package auth

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

	"xsite/service/internal/crypto/authtoken"
	"xsite/service/internal/domain/identity"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
)

// 身份接口的 HTTP 层测试：请求体解析、游客限流、缓存头、错误映射与登出幂等。

// fakeService 实现 Service 接口，记录入参并按需返回错误。
type fakeService struct {
	issueErr   error
	refreshErr error
	logoutErr  error
	issued     []identity.IssueInput
	refreshed  []identity.RefreshInput
	loggedOut  []string
}

func (f *fakeService) IssueGuest(_ context.Context, in identity.IssueInput) (identity.Tokens, error) {
	f.issued = append(f.issued, in)
	if f.issueErr != nil {
		return identity.Tokens{}, f.issueErr
	}
	return identity.Tokens{
		AccessToken: "access-xxx", RefreshToken: "refresh-xxx", DeviceID: "0192f1a2-7c3d-7e4f-8a9b-0c1d2e3f4a5b",
		Role: authtoken.RoleGuest, ExpiresIn: 900,
	}, nil
}

func (f *fakeService) Refresh(_ context.Context, in identity.RefreshInput) (identity.Tokens, error) {
	f.refreshed = append(f.refreshed, in)
	if f.refreshErr != nil {
		return identity.Tokens{}, f.refreshErr
	}
	return identity.Tokens{
		AccessToken: "access-new", RefreshToken: "refresh-new", DeviceID: "0192f1a2-7c3d-7e4f-8a9b-0c1d2e3f4a5b",
		Role: authtoken.RoleGuest, ExpiresIn: 900,
	}, nil
}

func (f *fakeService) Logout(_ context.Context, refreshToken string) error {
	f.loggedOut = append(f.loggedOut, refreshToken)
	return f.logoutErr
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

func do(t *testing.T, h *Handlers, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	h.Routes(r.Group("/api/v1"))
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
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

func decodeTokens(t *testing.T, w *httptest.ResponseRecorder) tokenResponse {
	t.Helper()
	var body struct {
		Data tokenResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	return body.Data
}

func TestGuest_OK(t *testing.T) {
	svc := &fakeService{}
	h, _ := newHandlers(svc)
	w := do(t, h, "/api/v1/auth/guest", `{"platform":"web","source_channel":"landing"}`,
		map[string]string{"User-Agent": "Mozilla/5.0"})
	if w.Code != http.StatusOK || codeOf(t, w) != "OK" {
		t.Fatalf("期望 200 OK，得到 %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != noStore {
		t.Fatalf("含令牌的响应应禁止缓存：%q", got)
	}
	d := decodeTokens(t, w)
	if d.AccessToken != "access-xxx" || d.RefreshToken != "refresh-xxx" || d.TokenType != "Bearer" ||
		d.ExpiresIn != 900 || d.Role != "guest" || d.DeviceID == "" {
		t.Fatalf("响应字段不对：%s", w.Body.String())
	}
	if len(svc.issued) != 1 {
		t.Fatalf("应调用一次签发：%d", len(svc.issued))
	}
	in := svc.issued[0]
	if in.Platform != "web" || in.SourceChannel != "landing" {
		t.Fatalf("请求体未正确传入：%+v", in)
	}
	if in.IPPrefix != "203.0.113.0/24" {
		t.Fatalf("应传入 /24 的 IP 前缀：%q", in.IPPrefix)
	}
	if in.UADigest == "" || len(in.UADigest) != 16 {
		t.Fatalf("应传入 UA 摘要（16 位十六进制）：%q", in.UADigest)
	}
}

func TestGuest_EmptyBody(t *testing.T) {
	svc := &fakeService{}
	h, _ := newHandlers(svc)
	// 请求体可空：游客签发不要求任何参数
	w := do(t, h, "/api/v1/auth/guest", "", nil)
	if w.Code != http.StatusOK || codeOf(t, w) != "OK" {
		t.Fatalf("空请求体应放行，得到 %d %s", w.Code, w.Body.String())
	}
	if len(svc.issued) != 1 || svc.issued[0].Platform != "" {
		t.Fatalf("空请求体应按默认值签发：%+v", svc.issued)
	}
}

func TestGuest_BadJSON(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
	}{
		{"非法 JSON", `{"platform":`},
		{"未知字段", `{"nope":1}`},
		{"类型不符", `{"platform":123}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeService{}
			h, _ := newHandlers(svc)
			w := do(t, h, "/api/v1/auth/guest", tt.body, nil)
			if w.Code != http.StatusBadRequest || codeOf(t, w) != "BAD_REQUEST" {
				t.Fatalf("期望 400 BAD_REQUEST，得到 %d %s", w.Code, w.Body.String())
			}
			if len(svc.issued) != 0 {
				t.Fatal("请求体非法时不应调用服务")
			}
		})
	}
}

func TestGuest_ServiceError(t *testing.T) {
	h, _ := newHandlers(&fakeService{issueErr: errors.New("db down")})
	w := do(t, h, "/api/v1/auth/guest", "", nil)
	if w.Code != http.StatusInternalServerError || codeOf(t, w) != "INTERNAL" {
		t.Fatalf("内部错误应 500 INTERNAL，得到 %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != noStore {
		t.Fatalf("错误响应同样不能缓存：%q", got)
	}
}

func TestGuest_RateLimit(t *testing.T) {
	svc := &fakeService{}
	h, lim := newHandlers(svc)
	for i := range guestPerMinute {
		if w := do(t, h, "/api/v1/auth/guest", "", nil); w.Code != http.StatusOK {
			t.Fatalf("第 %d 次应放行，得到 %d", i+1, w.Code)
		}
	}
	w := do(t, h, "/api/v1/auth/guest", "", nil)
	if w.Code != http.StatusTooManyRequests || codeOf(t, w) != "RATE_LIMITED" {
		t.Fatalf("超限应 429 RATE_LIMITED，得到 %d %s", w.Code, w.Body.String())
	}
	after, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || after < 1 || after > 60 {
		t.Fatalf("Retry-After 应为到下一分钟的秒数：%q", w.Header().Get("Retry-After"))
	}
	if len(svc.issued) != guestPerMinute {
		t.Fatalf("被限流的请求不应调用服务：%d", len(svc.issued))
	}
	// 按 IP 前缀计数
	found := false
	for k := range lim.counts {
		if strings.HasPrefix(k, "ip:203.0.113.0/24:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("游客签发应按 IP 前缀计数：%v", lim.counts)
	}

	// 计数器故障时放行
	h, lim = newHandlers(&fakeService{})
	lim.err = errors.New("valkey down")
	if w := do(t, h, "/api/v1/auth/guest", "", nil); w.Code != http.StatusOK {
		t.Fatalf("限流器不可用时应放行，得到 %d", w.Code)
	}
}

func TestRefresh_OK(t *testing.T) {
	svc := &fakeService{}
	h, _ := newHandlers(svc)
	w := do(t, h, "/api/v1/auth/refresh", `{"refresh_token":"  abc123  "}`, nil)
	if w.Code != http.StatusOK || codeOf(t, w) != "OK" {
		t.Fatalf("期望 200 OK，得到 %d %s", w.Code, w.Body.String())
	}
	d := decodeTokens(t, w)
	if d.AccessToken != "access-new" || d.RefreshToken != "refresh-new" {
		t.Fatalf("应返回轮换后的新令牌：%s", w.Body.String())
	}
	if len(svc.refreshed) != 1 || svc.refreshed[0].RefreshToken != "abc123" {
		t.Fatalf("刷新令牌应去除首尾空白后传入：%+v", svc.refreshed)
	}
}

func TestRefresh_ErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		code   string
	}{
		{errs.ErrTokenExpired, 401, "TOKEN_EXPIRED"},
		{errors.New("db down"), 500, "INTERNAL"},
	} {
		h, _ := newHandlers(&fakeService{refreshErr: tt.err})
		w := do(t, h, "/api/v1/auth/refresh", `{"refresh_token":"x"}`, nil)
		if w.Code != tt.status || codeOf(t, w) != tt.code {
			t.Errorf("%v：期望 %d %s，得到 %d %s", tt.err, tt.status, tt.code, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != noStore {
			t.Errorf("%v：错误响应不能缓存：%q", tt.err, got)
		}
	}
}

func TestLogout_NoContent(t *testing.T) {
	svc := &fakeService{}
	h, _ := newHandlers(svc)
	w := do(t, h, "/api/v1/auth/logout", `{"refresh_token":"  tok  "}`, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("登出应 204，得到 %d %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("204 不应有响应体：%s", w.Body.String())
	}
	if len(svc.loggedOut) != 1 || svc.loggedOut[0] != "tok" {
		t.Fatalf("应去除空白后传入登出令牌：%+v", svc.loggedOut)
	}
}

func TestLogout_EmptyBodyIdempotent(t *testing.T) {
	svc := &fakeService{}
	h, _ := newHandlers(svc)
	// 没有令牌也应幂等成功（服务层把空令牌当作已登出）
	w := do(t, h, "/api/v1/auth/logout", "", nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("空请求体登出应 204，得到 %d %s", w.Code, w.Body.String())
	}
	if len(svc.loggedOut) != 1 || svc.loggedOut[0] != "" {
		t.Fatalf("应以空令牌调用登出：%+v", svc.loggedOut)
	}
}

func TestLogout_ServiceError(t *testing.T) {
	h, _ := newHandlers(&fakeService{logoutErr: errors.New("db down")})
	w := do(t, h, "/api/v1/auth/logout", `{"refresh_token":"x"}`, nil)
	if w.Code != http.StatusInternalServerError || codeOf(t, w) != "INTERNAL" {
		t.Fatalf("登出失败应 500 INTERNAL，得到 %d %s", w.Code, w.Body.String())
	}
}

func TestIPPrefix(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.9":                "203.0.113.0/24",
		"::ffff:203.0.113.9":         "203.0.113.0/24",
		"2001:db8:1:2:3:4:5:6":       "2001:db8:1::/48",
		"2001:db8:1:ffff:ffff:0:0:1": "2001:db8:1::/48",
		"not-an-ip":                  "",
		"":                           "",
	} {
		if got := ipPrefix(in); got != want {
			t.Errorf("ipPrefix(%q) = %q，期望 %q", in, got, want)
		}
	}
}
