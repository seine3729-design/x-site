package s3probe

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"xsite/service/internal/domain/resourcecheck"
	"xsite/service/internal/netguard"
)

// assetProbeTimeout 是单个资源的探测上限。
//
// 比存储源连通性测试（opTimeout 15 秒）短：体检是成百上千个资源的批量操作，
// 一个卡住的地址不该拖着整部剧的任务。连不上的真实原因在 10 秒内都能暴露。
const assetProbeTimeout = 10 * time.Second

// AssetProber 探测单个资源能不能打开（resourcecheck.Prober）。
//
// 目标地址来自运营填写的封面路径与线路的 base_url，都是不可信输入，
// 所以连接前经 netguard 校验出站地址（防 DNS 重绑定 + 生产禁止内网）。
type AssetProber struct {
	Egress netguard.Policy
}

func (p *AssetProber) ProbeAsset(ctx context.Context, req resourcecheck.ProbeRequest) resourcecheck.ProbeResult {
	res := resourcecheck.ProbeResult{}
	if _, err := url.Parse(req.URL); err != nil || req.URL == "" {
		// 地址本身不合法：连都不用连。算「打不开」而不是「连不上」—— 问题在配置，不在网络
		res.Message = "访问地址无效"
		return res
	}

	ctx, cancel := context.WithTimeout(ctx, assetProbeTimeout)
	defer cancel()

	start := time.Now()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		res.Message = "访问地址无效"
		return res
	}
	// 只取一个字节：体检要判断的是「在不在、能不能读」，没必要把视频切片或大图拉下来。
	// 用 GET 而不是 HEAD —— 部分存储与 CDN 对 HEAD 返回 405，那会被误判成「打不开」。
	r.Header.Set("Range", "bytes=0-0")

	resp, err := httpClient(p.Egress.DialContext(nil)).Do(r)
	res.ElapsedMs = time.Since(start).Milliseconds()
	if err != nil {
		res.Unreachable = true
		res.Message = publicMsg(err)
		return res
	}
	defer resp.Body.Close()

	res.HTTPStatus = resp.StatusCode
	res.Message = assetMsg(resp.StatusCode)
	return res
}

// assetMsg 给状态码配一句中文说明。运营看结果列时不必自己记状态码的含义。
func assetMsg(status int) string {
	switch {
	case status == http.StatusPartialContent:
		// 206：支持 Range 的正常响应，说明源站对分段请求的支持是好的（播放器就靠它拖进度）
		return "正常（支持分段请求）"
	case status >= 200 && status < 300:
		return fmt.Sprintf("正常（HTTP %d）", status)
	case status >= 300 && status < 400:
		return fmt.Sprintf("重定向到了别处（HTTP %d），播放器通常能跟上，但多一跳延迟", status)
	case status == http.StatusNotFound || status == http.StatusGone:
		return fmt.Sprintf("源站上没有这个文件（HTTP %d）", status)
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return fmt.Sprintf("源站拒绝访问（HTTP %d），检查桶的读权限与线路的签名方式", status)
	case status == http.StatusTooManyRequests:
		return "源站限流了（HTTP 429），降低体检并发或稍后重试"
	case status == http.StatusRequestedRangeNotSatisfiable:
		return "源站不接受分段请求（HTTP 416），文件可能是空的"
	case status >= 500:
		return fmt.Sprintf("源站出错（HTTP %d）", status)
	default:
		return fmt.Sprintf("源站返回 HTTP %d", status)
	}
}
