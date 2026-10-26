package s3probe

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
)

// LineProber 对线路的 base_url + probe_path 发起 HTTP 探测，检查状态码、Content-Type 与 CORS（storage.LineProber）。
// 自定义域名在连接前经 netguard 校验出站地址（防 DNS 重绑定 + 生产禁止内网）。
type LineProber struct {
	Egress netguard.Policy
}

func (p *LineProber) ProbeLine(ctx context.Context, req storage.LineProbeRequest) storage.LineProbeResult {
	res := storage.LineProbeResult{}
	target := strings.TrimRight(req.BaseURL, "/") + req.ProbePath
	res.URL = target
	if _, err := url.Parse(target); err != nil {
		res.Message = "探测地址无效"
		return res
	}
	// 用一个代表站点入口的 Origin 探测 CORS（站点域名配置接入前用占位域名）
	probeOrigin := "https://probe.xsite.local"
	if len(req.Origins) > 0 {
		probeOrigin = req.Origins[0]
	}

	start := time.Now()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		res.Message = "探测地址无效"
		return res
	}
	r.Header.Set("Range", "bytes=0-0") // 只取一个字节，避免拉大文件
	r.Header.Set("Origin", probeOrigin)
	resp, err := httpClient(p.Egress.DialContext(nil)).Do(r)
	res.ElapsedMs = time.Since(start).Milliseconds()
	if err != nil {
		res.Message = publicMsg(err)
		return res
	}
	defer resp.Body.Close()

	res.HTTPStatus = resp.StatusCode
	res.ContentType = resp.Header.Get("Content-Type")
	acao := resp.Header.Get("Access-Control-Allow-Origin")
	res.CORSOrigin = acao
	res.CORS = acao == "*" || acao == probeOrigin
	res.OK = resp.StatusCode >= 200 && resp.StatusCode < 400
	if res.OK {
		res.Message = fmt.Sprintf("线路可达（HTTP %d）", resp.StatusCode)
	} else {
		res.Message = fmt.Sprintf("线路返回 HTTP %d", resp.StatusCode)
	}
	return res
}
