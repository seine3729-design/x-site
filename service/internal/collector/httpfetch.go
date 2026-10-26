package collector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"xsite/service/internal/netguard"
)

const defaultCollectorUserAgent = "xsite-test-collector/1.0"

// Fetched 是尚未读取完的远端响应。调用方必须关闭 Body。
type Fetched struct {
	Body io.ReadCloser
	Size int64 // HTTP Content-Length；缺失时为 -1，媒体采集会失败关闭。
	MIME string
}

// Fetcher 允许测试替换实际 HTTP 出站。
type Fetcher interface {
	Fetch(ctx context.Context, rawURL string, maxBytes int64) (Fetched, error)
}

// HTTPFetcher 通过 netguard 发起不跟随重定向的受限 HTTP 请求。
type HTTPFetcher struct {
	client    *http.Client
	allowHTTP bool
	userAgent string
}

// HTTPFetcherOptions 是下载器的出站策略。AllowHTTP 仅供本地受控测试注入，正式命令始终为 false。
type HTTPFetcherOptions struct {
	Egress    netguard.Policy
	AllowHTTP bool
	UserAgent string
}

func NewHTTPFetcher(opt HTTPFetcherOptions) *HTTPFetcher {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // 代理可能绕过目标地址的 SSRF 校验。
	transport.DialContext = opt.Egress.DialContext(nil)
	transport.DisableCompression = true // Content-Length 必须对应写入对象的实际字节数。
	transport.ResponseHeaderTimeout = 15 * time.Second
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.IdleConnTimeout = 30 * time.Second
	userAgent := opt.UserAgent
	if userAgent == "" {
		userAgent = defaultCollectorUserAgent
	}
	return &HTTPFetcher{
		allowHTTP: opt.AllowHTTP,
		userAgent: userAgent,
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("采集器不跟随重定向")
			},
		},
	}
}

// Fetch 仅允许成功的 200 响应。它先用 Transport 的 DialContext 做 DNS 重绑定防护，
// 再由调用方校验 manifest 域名、robots、MIME 和文件头。
func (f *HTTPFetcher) Fetch(ctx context.Context, rawURL string, maxBytes int64) (Fetched, error) {
	if _, _, err := cleanRemoteURL(rawURL, f.allowHTTP); err != nil {
		return Fetched{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return Fetched{}, badRequest("创建远端媒体请求失败")
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	response, err := f.client.Do(req)
	if err != nil {
		// 不回传底层错误，避免签名查询串或供应商内部信息出现在命令输出和日志。
		return Fetched{}, fmt.Errorf("请求远端媒体失败")
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return Fetched{}, badRequest("远端媒体返回 HTTP %d", response.StatusCode)
	}
	if maxBytes > 0 && response.ContentLength > maxBytes {
		_ = response.Body.Close()
		return Fetched{}, badRequest("远端媒体超过允许大小")
	}
	return Fetched{Body: response.Body, Size: response.ContentLength, MIME: normalizeMIME(response.Header.Get("Content-Type"))}, nil
}
