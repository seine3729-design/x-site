package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"xsite/service/internal/crypto/authtoken"
	"xsite/service/internal/domain/identity"
	"xsite/service/internal/domain/user"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"

	"github.com/gin-gonic/gin"
)

// 注册 / 登录 HTTP 层测试：请求解析、错误映射、注册限流、响应字段。

type fakeUsers struct {
	registerErr  error
	loginErr     error
	challengeErr error
	registered   []user.RegisterInput
	loggedIn     []user.LoginInput
	result       user.AuthResult
}

func (f *fakeUsers) Register(_ context.Context, in user.RegisterInput, _ identity.IssueInput) (user.AuthResult, error) {
	f.registered = append(f.registered, in)
	if f.registerErr != nil {
		return user.AuthResult{}, f.registerErr
	}
	return f.result, nil
}

func (f *fakeUsers) Login(_ context.Context, in user.LoginInput, _ identity.IssueInput) (user.AuthResult, error) {
	f.loggedIn = append(f.loggedIn, in)
	if f.loginErr != nil {
		return user.AuthResult{}, f.loginErr
	}
	return f.result, nil
}

func (f *fakeUsers) IssueChallenge(_ context.Context) (user.Challenge, error) {
	if f.challengeErr != nil {
		return user.Challenge{}, f.challengeErr
	}
	return user.Challenge{Nonce: "nonce-x", Difficulty: 18, ExpiresIn: 120}, nil
}

func okResult() user.AuthResult {
	return user.AuthResult{
		User:   user.User{ID: 7, Username: "alice", Nickname: "小爱", Status: user.StatusNormal},
		Tokens: identity.Tokens{AccessToken: "access", RefreshToken: "refresh", Role: authtoken.RoleUser, ExpiresIn: 900},
	}
}

func newAccountHandlers(u *fakeUsers) (*Handlers, *fakeLimiter) {
	lim := &fakeLimiter{counts: map[string]int64{}}
	return &Handlers{Users: u, RegisterLimiter: lim}, lim
}

func decodeAuth(t *testing.T, w *httptest.ResponseRecorder) authResponse {
	t.Helper()
	var body struct {
		Data authResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	return body.Data
}

func TestRegister_OK(t *testing.T) {
	u := &fakeUsers{result: okResult()}
	h, _ := newAccountHandlers(u)
	w := do(t, h, "/api/v1/auth/register", `{"username":"Alice","password":"s3cret-pass","nickname":"小爱","ack_no_recovery":true}`,
		map[string]string{"User-Agent": "Mozilla/5.0", "X-Platform": "ios"})
	if w.Code != http.StatusOK || codeOf(t, w) != "OK" {
		t.Fatalf("期望 200 OK，得到 %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != noStore {
		t.Fatalf("含令牌的响应应禁止缓存：%q", got)
	}
	d := decodeAuth(t, w)
	if d.User.ID != 7 || d.AccessToken != "access" || d.RefreshToken != "refresh" || d.TokenType != "Bearer" ||
		d.ExpiresIn != 900 || d.Role != "user" {
		t.Fatalf("响应字段不对：%s", w.Body.String())
	}
	if len(u.registered) != 1 || u.registered[0].Username != "Alice" || !u.registered[0].AckNoRecovery {
		t.Fatalf("注册入参未正确传入：%+v", u.registered)
	}
}

func TestRegister_ErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		code   string
	}{
		{errs.ErrConflict.WithMessage("用户名已被占用"), 409, "CONFLICT"},
		{errs.ErrBadRequest.WithMessage("密码过于常见"), 400, "BAD_REQUEST"},
		{errors.New("db down"), 500, "INTERNAL"},
	} {
		u := &fakeUsers{registerErr: tt.err}
		h, _ := newAccountHandlers(u)
		w := do(t, h, "/api/v1/auth/register", `{"username":"bob123","password":"s3cret-pass","ack_no_recovery":true}`, nil)
		if w.Code != tt.status || codeOf(t, w) != tt.code {
			t.Errorf("%v：期望 %d %s，得到 %d %s", tt.err, tt.status, tt.code, w.Code, w.Body.String())
		}
	}
}

func TestRegister_RateLimit(t *testing.T) {
	u := &fakeUsers{result: okResult()}
	h, _ := newAccountHandlers(u)
	body := `{"username":"bob123","password":"s3cret-pass","ack_no_recovery":true}`
	for i := range registerPerMinute {
		if w := do(t, h, "/api/v1/auth/register", body, nil); w.Code != http.StatusOK {
			t.Fatalf("第 %d 次应放行，得到 %d", i+1, w.Code)
		}
	}
	w := do(t, h, "/api/v1/auth/register", body, nil)
	if w.Code != http.StatusTooManyRequests || codeOf(t, w) != "RATE_LIMITED" {
		t.Fatalf("超限应 429 RATE_LIMITED，得到 %d %s", w.Code, w.Body.String())
	}
	after, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || after < 1 || after > 60 {
		t.Fatalf("Retry-After 应为到下一分钟的秒数：%q", w.Header().Get("Retry-After"))
	}
	if len(u.registered) != registerPerMinute {
		t.Fatalf("被限流的请求不应调用服务：%d", len(u.registered))
	}
}

func TestLogin_OK(t *testing.T) {
	u := &fakeUsers{result: okResult()}
	h, _ := newAccountHandlers(u)
	w := do(t, h, "/api/v1/auth/login", `{"username":"alice","password":"s3cret-pass"}`, nil)
	if w.Code != http.StatusOK || codeOf(t, w) != "OK" {
		t.Fatalf("期望 200 OK，得到 %d %s", w.Code, w.Body.String())
	}
	d := decodeAuth(t, w)
	if d.User.ID != 7 || d.AccessToken != "access" || d.Role != "user" {
		t.Fatalf("响应字段不对：%s", w.Body.String())
	}
	if len(u.loggedIn) != 1 || u.loggedIn[0].Username != "alice" {
		t.Fatalf("登录入参未正确传入：%+v", u.loggedIn)
	}
}

func TestLogin_ErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		code   string
	}{
		{errs.ErrNeedLogin.WithMessage("用户名或密码不正确"), 401, "NEED_LOGIN"},
		{errs.ErrForbidden.WithMessage("账号已被封禁"), 403, "FORBIDDEN"},
		{errs.ErrChallengeRequired, 428, "CHALLENGE_REQUIRED"},
	} {
		u := &fakeUsers{loginErr: tt.err}
		h, _ := newAccountHandlers(u)
		w := do(t, h, "/api/v1/auth/login", `{"username":"alice","password":"x"}`, nil)
		if w.Code != tt.status || codeOf(t, w) != tt.code {
			t.Errorf("%v：期望 %d %s，得到 %d %s", tt.err, tt.status, tt.code, w.Code, w.Body.String())
		}
	}
}

func doGet(t *testing.T, h *Handlers, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	h.Routes(r.Group("/api/v1"))
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestChallenge(t *testing.T) {
	h, _ := newAccountHandlers(&fakeUsers{})
	w := doGet(t, h, "/api/v1/auth/challenge")
	if w.Code != http.StatusOK || codeOf(t, w) != "OK" {
		t.Fatalf("期望 200 OK，得到 %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != noStore {
		t.Fatalf("挑战响应应禁止缓存：%q", got)
	}
	var body struct {
		Data challengeResponse `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Nonce == "" || body.Data.Difficulty <= 0 || body.Data.Algorithm == "" {
		t.Fatalf("挑战响应字段不对：%s", w.Body.String())
	}
}

// 登录带挑战答案时，答案应透传给服务。
func TestLogin_WithChallenge(t *testing.T) {
	u := &fakeUsers{result: okResult()}
	h, _ := newAccountHandlers(u)
	w := do(t, h, "/api/v1/auth/login", `{"username":"alice","password":"s3cret-pass","challenge":{"nonce":"n1","solution":"42"}}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d %s", w.Code, w.Body.String())
	}
	if len(u.loggedIn) != 1 || u.loggedIn[0].Challenge == nil ||
		u.loggedIn[0].Challenge.Nonce != "n1" || u.loggedIn[0].Challenge.Solution != "42" {
		t.Fatalf("挑战答案未透传：%+v", u.loggedIn)
	}
}
