// Package imageopt 把后台上传的原图交给外部图片优化服务压缩并转成 WebP。
//
// 为什么不在浏览器里压：canvas 要先把图片解码再有损编码一遍，对已经优化过的 JPEG 常常既掉画质又涨体积
// （运营看到的就是「压缩后比原图还大」）。Tinify 的引擎按图片内容选量化参数，压缩比和画质都稳定得多。
//
// API Key 只在服务端使用，不出现在日志、响应与前端产物里；外部服务返回的错误正文也不回显给调用方。
package imageopt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNotConfigured 表示没有配置 API Key，调用方应退回浏览器端压缩。
var ErrNotConfigured = errors.New("imageopt: 图片优化服务未配置")

// ErrQuotaExceeded 表示外部服务的本月额度已用尽。
var ErrQuotaExceeded = errors.New("imageopt: 图片优化服务本月额度已用尽")

// ErrUnsupported 表示外部服务不接受这张图（格式不支持或文件已损坏）。
var ErrUnsupported = errors.New("imageopt: 图片格式不受支持或文件已损坏")

// OutputMIME 是优化结果的统一格式。前台的 AVIF 由 CDN 分发层实时转（docs/08）。
const OutputMIME = "image/webp"

// maxResultBytes 是结果读取上限。优化只会让图片变小，这里只是防止替身服务返回超大响应把内存吃满。
const maxResultBytes = 32 << 20

// Options 是优化器的构造参数。
type Options struct {
	Endpoint string
	APIKey   string
	// MaxSide 是结果的最长边（像素）；0 表示不限制尺寸。
	MaxSide int
	Timeout time.Duration
}

// Optimizer 调用 Tinify 的 REST 接口。可并发使用。
type Optimizer struct {
	client   *http.Client
	endpoint *url.URL
	apiKey   string
	maxSide  int
	timeout  time.Duration
}

// Result 是一次优化的结果。
type Result struct {
	Data        []byte
	ContentType string
	Width       int
	Height      int
	InputBytes  int64
	OutputBytes int64
	// UsedThisMonth 是外部服务回报的本月已用次数（Compression-Count 响应头），用于在后台提示余量。
	UsedThisMonth int
}

// New 构造优化器。APIKey 为空时返回 ErrNotConfigured，让调用方把功能标为不可用而不是启动失败。
func New(opt Options) (*Optimizer, error) {
	if strings.TrimSpace(opt.APIKey) == "" {
		return nil, ErrNotConfigured
	}
	endpoint, err := url.Parse(strings.TrimRight(strings.TrimSpace(opt.Endpoint), "/"))
	if err != nil || endpoint.Host == "" {
		return nil, fmt.Errorf("imageopt: 服务地址无效")
	}
	timeout := opt.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil // 代理会让请求绕过这里对结果地址的同源校验
	transport.TLSHandshakeTimeout = 10 * time.Second
	transport.ResponseHeaderTimeout = timeout
	transport.IdleConnTimeout = 30 * time.Second
	return &Optimizer{
		endpoint: endpoint,
		apiKey:   strings.TrimSpace(opt.APIKey),
		maxSide:  opt.MaxSide,
		timeout:  timeout,
		client: &http.Client{
			Transport: transport,
			// 结果地址必须由响应体给出并通过同源校验，不接受跳转。
			CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("imageopt: 不跟随重定向") },
		},
	}, nil
}

// shrinkResponse 是 POST /shrink 的响应体。
type shrinkResponse struct {
	Input struct {
		Size int64  `json:"size"`
		Type string `json:"type"`
	} `json:"input"`
	Output struct {
		Size   int64  `json:"size"`
		Type   string `json:"type"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		URL    string `json:"url"`
	} `json:"output"`
}

// Optimize 压缩原图并按需转成 WebP、限制最长边，返回最终字节。
//
// 两段式：先 POST /shrink 上传原图拿到结果地址，再取回结果。只有确实需要换格式或缩小尺寸时才
// POST 结果地址（那会多消耗一次外部服务的额度），否则直接 GET。
func (o *Optimizer) Optimize(ctx context.Context, src []byte, srcMIME string) (Result, error) {
	if len(src) == 0 {
		return Result{}, ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	shrunk, err := o.shrink(ctx, src, srcMIME)
	if err != nil {
		return Result{}, err
	}
	target, err := o.resultURL(shrunk.body.Output.URL)
	if err != nil {
		return Result{}, err
	}

	convert := shrunk.body.Output.Type != OutputMIME
	oversized := o.maxSide > 0 && (shrunk.body.Output.Width > o.maxSide || shrunk.body.Output.Height > o.maxSide)
	fetched, err := o.fetchResult(ctx, target, convert, oversized)
	if err != nil {
		return Result{}, err
	}
	if !strings.HasPrefix(fetched.contentType, "image/") {
		return Result{}, fmt.Errorf("imageopt: 优化结果不是图片")
	}
	width, height := fetched.width, fetched.height
	if width == 0 || height == 0 {
		width, height = shrunk.body.Output.Width, shrunk.body.Output.Height
	}
	return Result{
		Data: fetched.data, ContentType: fetched.contentType, Width: width, Height: height,
		InputBytes: shrunk.body.Input.Size, OutputBytes: int64(len(fetched.data)),
		UsedThisMonth: max(shrunk.used, fetched.used),
	}, nil
}

// shrunk 是 /shrink 的解析结果。
type shrunk struct {
	body shrinkResponse
	used int
}

// shrink 上传原图。原图本身不受信任，这里只透传字节，由外部服务判定格式。
func (o *Optimizer) shrink(ctx context.Context, src []byte, srcMIME string) (shrunk, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint.String()+"/shrink", bytes.NewReader(src))
	if err != nil {
		return shrunk{}, fmt.Errorf("imageopt: 构造压缩请求失败")
	}
	if srcMIME != "" {
		req.Header.Set("Content-Type", srcMIME)
	}
	req.ContentLength = int64(len(src))
	got, err := o.do(req)
	if err != nil {
		return shrunk{}, err
	}
	var parsed shrinkResponse
	if err := json.Unmarshal(got.data, &parsed); err != nil || parsed.Output.URL == "" {
		return shrunk{}, fmt.Errorf("imageopt: 压缩服务返回了无法解析的结果")
	}
	return shrunk{body: parsed, used: got.used}, nil
}

// fetchResult 取回结果。需要换格式或缩小尺寸时用 POST 带上指令（会多消耗一次额度），否则直接 GET。
func (o *Optimizer) fetchResult(ctx context.Context, target *url.URL, convert, resize bool) (fetched, error) {
	method, payload := http.MethodGet, io.Reader(nil)
	if convert || resize {
		instructions := map[string]any{}
		if convert {
			instructions["convert"] = map[string]any{"type": OutputMIME}
		}
		if resize {
			instructions["resize"] = map[string]any{"method": "fit", "width": o.maxSide, "height": o.maxSide}
		}
		encoded, err := json.Marshal(instructions)
		if err != nil {
			return fetched{}, fmt.Errorf("imageopt: 构造转换指令失败")
		}
		method, payload = http.MethodPost, bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), payload)
	if err != nil {
		return fetched{}, fmt.Errorf("imageopt: 构造取回请求失败")
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	return o.do(req)
}

// fetched 是一次请求的响应体与相关元数据。
type fetched struct {
	data        []byte
	contentType string
	width       int
	height      int
	used        int
}

// do 发一次请求并读完响应。
func (o *Optimizer) do(req *http.Request) (fetched, error) {
	req.SetBasicAuth("api", o.apiKey)
	response, err := o.client.Do(req)
	if err != nil {
		// 不回传底层错误：它会带上结果地址里的一次性令牌。
		return fetched{}, fmt.Errorf("imageopt: 调用图片优化服务失败")
	}
	defer response.Body.Close()
	out := fetched{used: headerInt(response.Header, "Compression-Count")}

	switch {
	case response.StatusCode == http.StatusOK || response.StatusCode == http.StatusCreated:
	case response.StatusCode == http.StatusUnauthorized:
		return out, fmt.Errorf("imageopt: 图片优化服务拒绝了凭据")
	case response.StatusCode == http.StatusTooManyRequests:
		return out, ErrQuotaExceeded
	case response.StatusCode == http.StatusUnsupportedMediaType || response.StatusCode == http.StatusBadRequest:
		return out, ErrUnsupported
	default:
		return out, fmt.Errorf("imageopt: 图片优化服务返回 HTTP %d", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxResultBytes+1))
	if err != nil {
		return out, fmt.Errorf("imageopt: 读取优化结果失败")
	}
	if len(data) > maxResultBytes {
		return out, fmt.Errorf("imageopt: 优化结果超过允许大小")
	}
	out.data = data
	out.contentType, _, _ = strings.Cut(strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Type"))), ";")
	out.contentType = strings.TrimSpace(out.contentType)
	out.width = headerInt(response.Header, "Image-Width")
	out.height = headerInt(response.Header, "Image-Height")
	return out, nil
}

func headerInt(h http.Header, name string) int {
	value, err := strconv.Atoi(strings.TrimSpace(h.Get(name)))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

// resultURL 校验结果地址与配置的服务地址同源。
// 外部响应是不可信输入：照抄它给的地址再发请求，等于把一次 SSRF 的目标交给对方决定。
func (o *Optimizer) resultURL(raw string) (*url.URL, error) {
	target, err := url.Parse(raw)
	if err != nil || target.Host == "" {
		return nil, fmt.Errorf("imageopt: 优化结果地址无效")
	}
	if target.Scheme != o.endpoint.Scheme || target.Host != o.endpoint.Host {
		return nil, fmt.Errorf("imageopt: 优化结果地址不在服务域名下")
	}
	return target, nil
}
