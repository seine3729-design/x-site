package imageopt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeTinify 是外部优化服务的替身：记录收到的请求，按真实服务的协议回包。
type fakeTinify struct {
	server *httptest.Server
	// outputType 是 /shrink 回报的压缩结果格式。
	outputType string
	// outputW/H 是 /shrink 回报的结果尺寸。
	outputW, outputH int
	// outputURL 非空时覆盖结果地址，用来模拟指向别处的响应（SSRF）。
	outputURL string
	// noOutputURL 为 true 时回一个没有结果地址的响应。
	noOutputURL bool
	// shrinkStatus 非 0 时让 /shrink 返回该状态码。
	shrinkStatus int

	gotShrinkAuth  string
	gotShrinkMIME  string
	gotFetchMethod string
	gotFetchBody   string
}

func newFakeTinify(t *testing.T, f *fakeTinify) *fakeTinify {
	t.Helper()
	if f.outputType == "" {
		f.outputType = "image/png"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/shrink", func(w http.ResponseWriter, r *http.Request) {
		f.gotShrinkAuth = r.Header.Get("Authorization")
		f.gotShrinkMIME = r.Header.Get("Content-Type")
		if f.shrinkStatus != 0 {
			w.WriteHeader(f.shrinkStatus)
			return
		}
		url := f.outputURL
		if url == "" && !f.noOutputURL {
			url = f.server.URL + "/output/abc"
		}
		w.Header().Set("Compression-Count", "7")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"input":  map[string]any{"size": 1000, "type": "image/png"},
			"output": map[string]any{"size": 400, "type": f.outputType, "width": f.outputW, "height": f.outputH, "url": url},
		})
	})
	mux.HandleFunc("/output/abc", func(w http.ResponseWriter, r *http.Request) {
		f.gotFetchMethod = r.Method
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
			f.gotFetchBody = string(body)
		}
		w.Header().Set("Compression-Count", "8")
		w.Header().Set("Content-Type", OutputMIME)
		w.Header().Set("Image-Width", "120")
		w.Header().Set("Image-Height", "90")
		_, _ = w.Write([]byte("RIFF0000WEBPpayload"))
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func optimizerFor(t *testing.T, f *fakeTinify, maxSide int) *Optimizer {
	t.Helper()
	o, err := New(Options{Endpoint: f.server.URL, APIKey: "test-key", MaxSide: maxSide, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("New() err = %v", err)
	}
	return o
}

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := New(Options{Endpoint: "https://api.tinify.com"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("没有 API Key 时应返回 ErrNotConfigured，得到 %v", err)
	}
}

// 原图不是 WebP 时必须带上转换指令，否则写进对象存储的东西过不了 WebP 文件头校验。
func TestOptimizeConvertsNonWebPSource(t *testing.T) {
	fake := newFakeTinify(t, &fakeTinify{outputType: "image/png", outputW: 100, outputH: 80})
	result, err := optimizerFor(t, fake, 2400).Optimize(context.Background(), []byte("png-bytes"), "image/png")
	if err != nil {
		t.Fatalf("Optimize() err = %v", err)
	}
	if fake.gotFetchMethod != http.MethodPost {
		t.Fatalf("需要转格式时应 POST 结果地址，得到 %s", fake.gotFetchMethod)
	}
	if !strings.Contains(fake.gotFetchBody, OutputMIME) {
		t.Fatalf("转换指令里应指定 %s，得到 %s", OutputMIME, fake.gotFetchBody)
	}
	if strings.Contains(fake.gotFetchBody, "resize") {
		t.Fatalf("尺寸没超限时不该带 resize：%s", fake.gotFetchBody)
	}
	if result.ContentType != OutputMIME || result.Width != 120 || result.Height != 90 {
		t.Fatalf("结果元数据不对：%+v", result)
	}
	if result.InputBytes != 1000 || result.OutputBytes != int64(len(result.Data)) || result.UsedThisMonth != 8 {
		t.Fatalf("结果统计不对：%+v", result)
	}
	if !strings.HasPrefix(fake.gotShrinkAuth, "Basic ") || fake.gotShrinkMIME != "image/png" {
		t.Fatalf("上传原图的请求头不对：auth=%q mime=%q", fake.gotShrinkAuth, fake.gotShrinkMIME)
	}
}

// 已经是 WebP 且尺寸合规时直接取回，不触发额外的一次计费转换。
func TestOptimizeSkipsTransformWhenAlreadyWebP(t *testing.T) {
	fake := newFakeTinify(t, &fakeTinify{outputType: OutputMIME, outputW: 800, outputH: 600})
	if _, err := optimizerFor(t, fake, 2400).Optimize(context.Background(), []byte("webp"), OutputMIME); err != nil {
		t.Fatalf("Optimize() err = %v", err)
	}
	if fake.gotFetchMethod != http.MethodGet {
		t.Fatalf("无需转换时应 GET 结果地址，得到 %s", fake.gotFetchMethod)
	}
}

// 超过最长边时要带 resize，否则后台会把原始尺寸的大图发给访客。
func TestOptimizeResizesOversized(t *testing.T) {
	fake := newFakeTinify(t, &fakeTinify{outputType: OutputMIME, outputW: 4000, outputH: 3000})
	if _, err := optimizerFor(t, fake, 2400).Optimize(context.Background(), []byte("webp"), OutputMIME); err != nil {
		t.Fatalf("Optimize() err = %v", err)
	}
	if fake.gotFetchMethod != http.MethodPost || !strings.Contains(fake.gotFetchBody, "resize") {
		t.Fatalf("超尺寸时应 POST 并带 resize：method=%s body=%s", fake.gotFetchMethod, fake.gotFetchBody)
	}
	if !strings.Contains(fake.gotFetchBody, "2400") {
		t.Fatalf("resize 应用配置的最长边：%s", fake.gotFetchBody)
	}
}

// 外部响应是不可信输入：照抄它给的地址再发请求，等于把一次 SSRF 的目标交给对方决定。
func TestOptimizeRejectsResultURLOffService(t *testing.T) {
	for _, raw := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"https://evil.example.com/output/abc",
		"not-a-url",
	} {
		t.Run(raw, func(t *testing.T) {
			fake := newFakeTinify(t, &fakeTinify{outputURL: raw})
			_, err := optimizerFor(t, fake, 2400).Optimize(context.Background(), []byte("png"), "image/png")
			if err == nil {
				t.Fatal("结果地址不在服务域名下时应返回错误")
			}
			if fake.gotFetchMethod != "" {
				t.Fatalf("不应向该地址发请求，实际发了 %s", fake.gotFetchMethod)
			}
		})
	}
}

// 额度用尽与格式不支持要能被调用方区分：前者退回本地压缩，后者提示运营换图。
func TestOptimizeMapsServiceErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusTooManyRequests, ErrQuotaExceeded},
		{http.StatusUnsupportedMediaType, ErrUnsupported},
		{http.StatusBadRequest, ErrUnsupported},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			fake := newFakeTinify(t, &fakeTinify{shrinkStatus: tc.status})
			_, err := optimizerFor(t, fake, 2400).Optimize(context.Background(), []byte("png"), "image/png")
			if !errors.Is(err, tc.want) {
				t.Fatalf("HTTP %d 应映射为 %v，得到 %v", tc.status, tc.want, err)
			}
		})
	}
}

// 凭据错误与未知状态码都不能把外部服务的响应正文或结果地址回传出去。
func TestOptimizeErrorsDoNotLeakKeyOrURL(t *testing.T) {
	fake := newFakeTinify(t, &fakeTinify{shrinkStatus: http.StatusUnauthorized})
	_, err := optimizerFor(t, fake, 2400).Optimize(context.Background(), []byte("png"), "image/png")
	if err == nil {
		t.Fatal("凭据被拒时应返回错误")
	}
	if strings.Contains(err.Error(), "test-key") {
		t.Fatalf("错误里不能出现 API Key：%v", err)
	}
}

// 压缩服务没给结果地址时要明确失败，不能把空地址当成「没有东西要取」而返回空图。
func TestOptimizeRejectsResponseWithoutResultURL(t *testing.T) {
	fake := newFakeTinify(t, &fakeTinify{noOutputURL: true})
	if _, err := optimizerFor(t, fake, 2400).Optimize(context.Background(), []byte("png"), "image/png"); err == nil {
		t.Fatal("响应缺少结果地址时应返回错误")
	}
}

func TestOptimizeRejectsEmptySource(t *testing.T) {
	fake := newFakeTinify(t, &fakeTinify{})
	if _, err := optimizerFor(t, fake, 2400).Optimize(context.Background(), nil, "image/png"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("空图应返回 ErrUnsupported，得到 %v", err)
	}
}
