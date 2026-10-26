package play

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/domain/content"
	dom "xsite/service/internal/domain/play"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
)

// 选源接口的 HTTP 层测试：参数解析、观看者识别、限流、缓存头与错误映射。

type fakeService struct {
	err   error
	calls []dom.SourcesInput
}

func (f *fakeService) Sources(_ context.Context, in dom.SourcesInput) (dom.Sources, error) {
	f.calls = append(f.calls, in)
	if f.err != nil {
		return dom.Sources{}, f.err
	}
	return dom.Sources{EpisodeID: in.EpisodeID, Region: "XX", Access: content.AccessFree, Lines: []dom.SourceLine{
		{LineID: 12, Name: "线路 1", RegionTag: "通用", Recommended: true, URL: "/api/v1/hls/t/index.m3u8",
			ExpiresAt: time.Date(2026, 10, 23, 12, 10, 0, 0, time.UTC)},
	}}, nil
}

// fakeLimiter 按 key 累加计数；err 不为空时模拟 Valkey 故障。
type fakeLimiter struct {
	counts map[string]int64
	err    error
}

func (l *fakeLimiter) Add(_ context.Context, key string, delta int64, _ time.Duration) (int64, error) {
	if l.err != nil {
		return 0, l.err
	}
	l.counts[key] += delta
	return l.counts[key], nil
}

func newHandlers(svc *fakeService) (*Handlers, *fakeLimiter) {
	lim := &fakeLimiter{counts: map[string]int64{}}
	return &Handlers{Svc: svc, Limiter: lim}, lim
}

func do(t *testing.T, h *Handlers, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	h.Routes(r.Group("/api/v1"))
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "203.0.113.9:4321"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func codeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	return body.Code
}

func TestSources_OK(t *testing.T) {
	svc := &fakeService{}
	h, _ := newHandlers(svc)
	w := do(t, h, "/api/v1/episodes/42/sources?line_id=7", map[string]string{"X-Device-Id": "0192f1a2-7c3d-7e4f-8a9b-0c1d2e3f4a5b"})
	if w.Code != http.StatusOK || codeOf(t, w) != "OK" {
		t.Fatalf("期望 200 OK，得到 %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("选源结果含签名，缓存头应为 private, no-store：%q", got)
	}
	in := svc.calls[0]
	if in.EpisodeID != 42 || in.LineID != 7 || in.Viewer.DeviceID != "0192f1a2-7c3d-7e4f-8a9b-0c1d2e3f4a5b" || in.Viewer.UserID != 0 {
		t.Fatalf("参数未正确传入：%+v", in)
	}
	if in.IP != "203.0.113.9" || in.Geo != (storage.Geo{}) {
		t.Fatalf("非 dev 环境应按客户端 IP 解析地区：%+v", in)
	}
	var body struct {
		Data struct {
			EpisodeID int64  `json:"episode_id"`
			TrialSec  *int32 `json:"trial_sec"`
			Lines     []struct {
				URL       string `json:"url"`
				Manual    *bool  `json:"manual"`
				ExpiresAt string `json:"expires_at"`
			} `json:"lines"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	d := body.Data
	if d.EpisodeID != 42 || d.TrialSec == nil || len(d.Lines) != 1 || d.Lines[0].Manual == nil ||
		d.Lines[0].ExpiresAt != "2026-10-23T12:10:00Z" {
		t.Fatalf("响应字段不对：%s", w.Body.String())
	}
}

func TestSources_BadRequests(t *testing.T) {
	for _, tt := range []struct {
		name    string
		path    string
		headers map[string]string
		status  int
		code    string
	}{
		{"分集编号不是数字", "/api/v1/episodes/abc/sources", nil, 404, "NOT_FOUND"},
		{"分集编号不是正数", "/api/v1/episodes/0/sources", nil, 404, "NOT_FOUND"},
		{"手选线路不是数字", "/api/v1/episodes/1/sources?line_id=x", nil, 400, "BAD_REQUEST"},
		{"手选线路为负", "/api/v1/episodes/1/sources?line_id=-3", nil, 400, "BAD_REQUEST"},
		{"设备标识含非法字符", "/api/v1/episodes/1/sources", map[string]string{"X-Device-Id": "a b"}, 400, "BAD_REQUEST"},
		{"设备标识过长", "/api/v1/episodes/1/sources", map[string]string{"X-Device-Id": strings.Repeat("a", 65)}, 400, "BAD_REQUEST"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{}
			h, _ := newHandlers(svc)
			w := do(t, h, tt.path, tt.headers)
			if w.Code != tt.status || codeOf(t, w) != tt.code {
				t.Fatalf("期望 %d %s，得到 %d %s", tt.status, tt.code, w.Code, w.Body.String())
			}
			if len(svc.calls) != 0 {
				t.Fatal("参数不对时不应调用选源服务")
			}
		})
	}
}

func TestSources_ErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		code   string
	}{
		{errs.ErrNotFound, 404, "NOT_FOUND"},
		{errs.ErrNeedLogin, 401, "NEED_LOGIN"},
		{errs.ErrNeedVIP, 402, "NEED_VIP"},
		{dom.ErrNoLines, 503, "UNAVAILABLE"},
		{errors.New("db down"), 500, "INTERNAL"},
	} {
		h, _ := newHandlers(&fakeService{err: tt.err})
		w := do(t, h, "/api/v1/episodes/1/sources", nil)
		if w.Code != tt.status || codeOf(t, w) != tt.code {
			t.Errorf("%v：期望 %d %s，得到 %d %s", tt.err, tt.status, tt.code, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != "private, no-store" {
			t.Errorf("%v：错误响应同样不能缓存：%q", tt.err, got)
		}
	}
}

func TestSources_RateLimit(t *testing.T) {
	svc := &fakeService{}
	h, lim := newHandlers(svc)
	dev := map[string]string{"X-Device-Id": "dev-1"}
	for i := range sourcesPerMinute {
		if w := do(t, h, "/api/v1/episodes/1/sources", dev); w.Code != http.StatusOK {
			t.Fatalf("第 %d 次应放行，得到 %d", i+1, w.Code)
		}
	}
	w := do(t, h, "/api/v1/episodes/2/sources", dev)
	if w.Code != http.StatusTooManyRequests || codeOf(t, w) != "RATE_LIMITED" {
		t.Fatalf("每分钟第 61 次应 429，得到 %d %s", w.Code, w.Body.String())
	}
	after, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || after < 1 || after > 60 {
		t.Fatalf("Retry-After 应为到下一分钟的秒数：%q", w.Header().Get("Retry-After"))
	}
	if len(svc.calls) != sourcesPerMinute {
		t.Fatalf("被限流的请求不应调用选源服务：%d", len(svc.calls))
	}
	// 其他设备不受影响
	if w := do(t, h, "/api/v1/episodes/1/sources", map[string]string{"X-Device-Id": "dev-2"}); w.Code != http.StatusOK {
		t.Fatalf("其他设备应放行，得到 %d", w.Code)
	}
	// 没有设备标识时按 IP 计数
	do(t, h, "/api/v1/episodes/1/sources", nil)
	found := false
	for k := range lim.counts {
		if strings.HasPrefix(k, "ip:203.0.113.9:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("没有设备标识时应按 IP 计数：%v", lim.counts)
	}

	// 计数器故障时放行
	h, lim = newHandlers(&fakeService{})
	lim.err = errors.New("valkey down")
	if w := do(t, h, "/api/v1/episodes/1/sources", dev); w.Code != http.StatusOK {
		t.Fatalf("限流器不可用时应放行，得到 %d", w.Code)
	}
}

func TestLimiterIP(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9":                "203.0.113.9",
		"::ffff:203.0.113.9":         "203.0.113.9",
		"2001:db8:1:2:3:4:5:6":       "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff:ffff:0:1": "2001:db8:1:2::/64",
		"not-an-ip":                  "not-an-ip",
	} {
		if got := limiterIP(in); got != want {
			t.Errorf("limiterIP(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestSources_IdentifyAndDebugGeo(t *testing.T) {
	svc := &fakeService{}
	h, _ := newHandlers(svc)
	// 身份中间件接入后（S1-10 / S2-1）由 Identify 提供观看者
	h.Identify = func(*gin.Context) (dom.Viewer, error) { return dom.Viewer{UserID: 7, DeviceID: "dev-9"}, nil }
	h.Dev = true
	do(t, h, "/api/v1/episodes/1/sources", map[string]string{"X-Debug-Geo": "CN/广东/电信", "X-Device-Id": "ignored"})
	in := svc.calls[0]
	if in.Viewer != (dom.Viewer{UserID: 7, DeviceID: "dev-9"}) {
		t.Fatalf("应使用身份中间件给出的观看者：%+v", in.Viewer)
	}
	if in.IP != "" || in.Geo != (storage.Geo{Country: "CN", Province: "广东", ISP: "电信"}) {
		t.Fatalf("dev 环境的 X-Debug-Geo 应直接给定地区：%+v", in)
	}

	// 身份解析失败（如登录过期）原样返回
	h.Identify = func(*gin.Context) (dom.Viewer, error) { return dom.Viewer{}, errs.ErrTokenExpired }
	if w := do(t, h, "/api/v1/episodes/1/sources", nil); w.Code != http.StatusUnauthorized || codeOf(t, w) != "TOKEN_EXPIRED" {
		t.Fatalf("身份解析失败应原样返回，得到 %d %s", w.Code, w.Body.String())
	}

	// 非 dev 环境忽略 X-Debug-Geo
	svc = &fakeService{}
	h, _ = newHandlers(svc)
	do(t, h, "/api/v1/episodes/1/sources", map[string]string{"X-Debug-Geo": "CN/广东/电信"})
	if svc.calls[0].IP != "203.0.113.9" || svc.calls[0].Geo != (storage.Geo{}) {
		t.Fatalf("非 dev 环境不能模拟地区：%+v", svc.calls[0])
	}
}
