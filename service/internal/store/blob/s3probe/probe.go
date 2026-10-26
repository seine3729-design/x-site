// Package s3probe 实现 storage.Prober：在 API 进程内对存储源做连通性测试
// （写入 → 读取 → 预签名访问 → 删除；HTTP 源做可达性探测），自定义端点经 netguard 校验出站地址（docs/12 第 4.4、9 节）。
package s3probe

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
)

// opTimeout 是单步操作（写 / 读 / 预签名访问 / 删除）的超时。
const opTimeout = 15 * time.Second

// Prober 用解密后的凭据连接存储源执行连通性测试。Egress 决定允许连接哪些出站地址。
type Prober struct {
	Egress netguard.Policy
}

// Probe 执行连通性测试；任一步失败即停止后续步骤（结果里能看到走到哪一步）。
func (p *Prober) Probe(ctx context.Context, req storage.ProbeRequest) storage.ProbeResult {
	start := time.Now()
	var res storage.ProbeResult
	if req.Subtype == storage.SubtypeHTTPOrigin {
		p.probeHTTPOrigin(ctx, req, &res)
	} else {
		p.probeS3(ctx, req, &res)
	}
	res.ElapsedMs = time.Since(start).Milliseconds()
	res.OK = allOK(res.Steps)
	return res
}

func (p *Prober) probeS3(ctx context.Context, req storage.ProbeRequest, res *storage.ProbeResult) {
	dial := p.Egress.DialContext(nil)
	ttl := time.Duration(req.TTL) * time.Second

	// 自定义端点先解析并校验出站地址（AWS 默认端点由 SDK 内部构造，跳过）
	if req.Endpoint != "" {
		if !p.step(res, storage.StepResolve, func() (string, error) {
			u, err := url.Parse(req.Endpoint)
			if err != nil {
				return "", err
			}
			ips, err := p.Egress.Resolve(ctx, nil, u.Hostname())
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("端点解析到 %d 个地址并通过校验", len(ips)), nil
		}) {
			return
		}
	}

	writeClient, err := blob.New(blobConfig(req, req.Write, dial))
	if err != nil {
		p.addFail(res, storage.StepPut, err)
		return
	}
	bucket := writeClient.Bucket(req.Bucket)
	key := joinKey(req.PathPrefix, "_probe/conn-"+token()+".txt")
	content := []byte("xsite-storage-probe-" + token())

	if !p.step(res, storage.StepPut, func() (string, error) {
		cctx, cancel := context.WithTimeout(ctx, opTimeout)
		defer cancel()
		if err := bucket.PutBytes(cctx, key, content, blob.PutOptions{ContentType: "text/plain"}); err != nil {
			return "", err
		}
		return "写入探测文件成功", nil
	}) {
		return
	}
	// put 成功后无论后续结果如何都兜底清理（best-effort，不记入步骤）
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		_ = bucket.Delete(cctx, key)
	}()

	if !p.step(res, storage.StepGet, func() (string, error) {
		cctx, cancel := context.WithTimeout(ctx, opTimeout)
		defer cancel()
		rc, _, err := bucket.Get(cctx, key)
		if err != nil {
			return "", err
		}
		defer rc.Close()
		got, err := io.ReadAll(io.LimitReader(rc, int64(len(content))+16))
		if err != nil {
			return "", err
		}
		if !bytes.Equal(got, content) {
			return "", errors.New("读回的内容与写入不一致")
		}
		return "读取并校验内容成功", nil
	}) {
		return
	}

	if !p.step(res, storage.StepPresign, func() (string, error) {
		readClient, err := blob.New(blobConfig(req, req.Read, dial))
		if err != nil {
			return "", err
		}
		rawURL, err := readClient.Bucket(req.Bucket).PresignGet(ctx, key, ttl)
		if err != nil {
			return "", err
		}
		cctx, cancel := context.WithTimeout(ctx, opTimeout)
		defer cancel()
		if err := httpGetVerify(cctx, httpClient(dial), rawURL, content); err != nil {
			return "", err
		}
		return "预签名下载并校验成功", nil
	}) {
		return
	}

	p.step(res, storage.StepDelete, func() (string, error) {
		cctx, cancel := context.WithTimeout(ctx, opTimeout)
		defer cancel()
		if err := bucket.Delete(cctx, key); err != nil {
			return "", err
		}
		return "删除探测文件成功", nil
	})
}

func (p *Prober) probeHTTPOrigin(ctx context.Context, req storage.ProbeRequest, res *storage.ProbeResult) {
	dial := p.Egress.DialContext(nil)
	u, err := url.Parse(req.Endpoint)
	if err != nil {
		p.addFail(res, storage.StepResolve, err)
		return
	}
	if !p.step(res, storage.StepResolve, func() (string, error) {
		ips, err := p.Egress.Resolve(ctx, nil, u.Hostname())
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("解析到 %d 个地址并通过校验", len(ips)), nil
	}) {
		return
	}
	p.step(res, storage.StepHead, func() (string, error) {
		cctx, cancel := context.WithTimeout(ctx, opTimeout)
		defer cancel()
		r, err := http.NewRequestWithContext(cctx, http.MethodGet, req.Endpoint, nil)
		if err != nil {
			return "", err
		}
		r.Header.Set("Range", "bytes=0-0") // 只取一个字节，避免拉取大文件
		resp, err := httpClient(dial).Do(r)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("HTTP 源返回 %d", resp.StatusCode)
		}
		return fmt.Sprintf("HTTP 源可达（HTTP %d）", resp.StatusCode), nil
	})
}

// step 执行一步并记录结果；返回是否成功（决定是否继续后续步骤）。
func (p *Prober) step(res *storage.ProbeResult, name storage.ProbeStepName, fn func() (string, error)) bool {
	t := time.Now()
	msg, err := fn()
	s := storage.ProbeStep{Name: name, Ms: time.Since(t).Milliseconds()}
	if err != nil {
		s.OK, s.Message = false, publicMsg(err)
		res.Steps = append(res.Steps, s)
		return false
	}
	s.OK, s.Message = true, msg
	res.Steps = append(res.Steps, s)
	return true
}

func (p *Prober) addFail(res *storage.ProbeResult, name storage.ProbeStepName, err error) {
	res.Steps = append(res.Steps, storage.ProbeStep{Name: name, OK: false, Message: publicMsg(err)})
}

func blobConfig(req storage.ProbeRequest, cred storage.Credential, dial func(context.Context, string, string) (net.Conn, error)) blob.Config {
	return blob.Config{
		Endpoint: req.Endpoint, Region: req.Region, AccessKey: cred.AccessKeyID, SecretKey: cred.SecretAccessKey,
		PathStyle: req.PathStyle, DialContext: dial,
	}
}

func httpClient(dial func(context.Context, string, string) (net.Conn, error)) *http.Client {
	return &http.Client{
		Timeout:   opTimeout,
		Transport: &http.Transport{DialContext: dial, TLSHandshakeTimeout: 10 * time.Second, ForceAttemptHTTP2: true},
	}
}

func httpGetVerify(ctx context.Context, hc *http.Client, rawURL string, want []byte) error {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := hc.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("预签名地址返回 HTTP %d", resp.StatusCode)
	}
	got, err := io.ReadAll(io.LimitReader(resp.Body, int64(len(want))+16))
	if err != nil {
		return err
	}
	if !bytes.Equal(got, want) {
		return errors.New("预签名下载的内容与写入不一致")
	}
	return nil
}

// publicMsg 把错误转成给运营看的中文说明。S3 / HTTP 错误不含凭据，可直接展示；超长截断。
func publicMsg(err error) string {
	switch {
	case netguard.IsBlocked(err):
		return "目标地址属于内网或保留地址段，当前环境不允许连接"
	case errors.Is(err, context.DeadlineExceeded):
		return "连接超时，请检查端点地址与网络"
	}
	m := err.Error()
	if len(m) > 200 {
		m = m[:200]
	}
	return m
}

func allOK(steps []storage.ProbeStep) bool {
	if len(steps) == 0 {
		return false
	}
	for _, s := range steps {
		if !s.OK {
			return false
		}
	}
	return true
}

// joinKey 把路径前缀与键拼成桶内的对象键。
func joinKey(prefix, key string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return key
	}
	return prefix + "/" + key
}

// token 生成 10 位小写随机串，用于探测文件名与内容。
func token() string {
	return strings.ToLower(rand.Text()[:10])
}
