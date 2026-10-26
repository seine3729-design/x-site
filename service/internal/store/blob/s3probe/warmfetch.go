package s3probe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"xsite/service/internal/domain/resourcewarm"
	"xsite/service/internal/netguard"
)

const (
	// warmFetchTimeout 是拉一个切片的上限。
	//
	// 比体检（10 秒）宽松：体检只取一个字节，预热要完整拉下来让边缘缓存住，
	// 一个 2 秒切片几百 KB，慢线路上十几秒是正常的。
	warmFetchTimeout = 30 * time.Second
	// warmMaxBytes 是单个对象读取上限。切片与 init 段都是几百 KB 量级，
	// 超过这个数说明地址指错了（整片视频、日志文件之类），没必要为它付流量。
	warmMaxBytes = 32 << 20
)

// WarmFetcher 完整拉取一个地址，让边缘回源缓存住（resourcewarm.Fetcher）。
//
// 目标地址来自线路的 base_url 与签名，经 netguard 校验出站（防 DNS 重绑定 + 生产禁止内网）。
type WarmFetcher struct {
	Egress netguard.Policy
}

func (f *WarmFetcher) Fetch(ctx context.Context, rawURL string) resourcewarm.FetchResult {
	res := resourcewarm.FetchResult{}
	if rawURL == "" {
		res.Message = "地址为空"
		return res
	}
	if _, err := url.Parse(rawURL); err != nil {
		res.Message = "地址无效"
		return res
	}

	ctx, cancel := context.WithTimeout(ctx, warmFetchTimeout)
	defer cancel()

	r, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		res.Message = "地址无效"
		return res
	}
	resp, err := httpClient(f.Egress.DialContext(nil)).Do(r)
	if err != nil {
		res.Unreachable = true
		res.Message = publicMsg(err)
		return res
	}
	defer resp.Body.Close()

	res.HTTPStatus = resp.StatusCode
	// 必须把 body 读完：预热的全部意义就是让边缘把这个对象完整回源一次并缓存住。
	// 只读响应头（或提前关闭）的话，边缘可能根本没把对象拉全，下一个观众照样回源。
	n, readErr := io.Copy(io.Discard, io.LimitReader(resp.Body, warmMaxBytes))
	res.Bytes = n
	switch {
	case readErr != nil:
		// 响应头拿到了但读 body 断了：算没拿到（边缘那边也不会缓存半截对象）
		res.Unreachable = true
		res.Message = "读取内容中断：" + publicMsg(readErr)
	case resp.StatusCode >= 200 && resp.StatusCode < 400:
		res.Message = fmt.Sprintf("已拉取 %d 字节", n)
	default:
		res.Message = fmt.Sprintf("源站返回 HTTP %d", resp.StatusCode)
	}
	return res
}
