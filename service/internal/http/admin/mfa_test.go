package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	adm "xsite/service/internal/domain/admin"
	"xsite/service/internal/errs"
)

// fakeMFA 只记录调用，业务规则在 domain/admin 中测试。
type fakeMFA struct {
	setup       adm.TOTPSetup
	beginErr    error
	confirmErr  error
	beginFor    []int64
	confirmCode map[int64]string
}

func (f *fakeMFA) BeginTOTP(_ context.Context, adminID int64, _ string) (adm.TOTPSetup, error) {
	f.beginFor = append(f.beginFor, adminID)
	if f.beginErr != nil {
		return adm.TOTPSetup{}, f.beginErr
	}
	return f.setup, nil
}

func (f *fakeMFA) ConfirmTOTP(_ context.Context, adminID int64, code string) error {
	if f.confirmCode == nil {
		f.confirmCode = map[int64]string{}
	}
	f.confirmCode[adminID] = code
	return f.confirmErr
}

// mfaEnv 复用后台路由装配，并接上 fake 二次验证服务与已登录的 alice。
func mfaEnv(t *testing.T) (*env, *fakeMFA, adm.Identity) {
	t.Helper()
	e := newEnv(false)
	mfa := &fakeMFA{setup: adm.TOTPSetup{Secret: "JBSWY3DPEHPK3PXP", OtpauthURL: "otpauth://totp/x-site:alice?secret=JBSWY3DPEHPK3PXP&issuer=x-site"}}
	e.h.MFA = mfa // 路由里的 handler 在请求时才读取 h.MFA，装配后再设置即可
	alice := identity(7, "alice", operatorRole, "csrf-alice", "dramas.view")
	e.auth.add("tok-alice", alice)
	return e, mfa, alice
}

func TestMFATOTPSetup_RequiresSession(t *testing.T) {
	e, _, _ := mfaEnv(t)
	// 未登录
	if w := e.do(req{method: http.MethodPost, path: "/api/admin/mfa/totp/setup", origin: allowedOrigin}); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，得到 %d %s", w.Code, w.Body.String())
	}
	// 已登录但缺少 CSRF 令牌
	if w := e.do(req{method: http.MethodPost, path: "/api/admin/mfa/totp/setup", cookie: "tok-alice", origin: allowedOrigin}); w.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 应 403，得到 %d %s", w.Code, w.Body.String())
	}
}

func TestMFATOTPSetup_ReturnsOtpauth(t *testing.T) {
	e, mfa, alice := mfaEnv(t)
	w := e.write(http.MethodPost, "/api/admin/mfa/totp/setup", "", alice, "tok-alice")
	if w.Code != http.StatusOK {
		t.Fatalf("开始绑定应 200，得到 %d %s", w.Code, w.Body.String())
	}
	var data struct {
		Secret     string `json:"secret"`
		OtpauthURL string `json:"otpauth_url"`
	}
	if err := json.Unmarshal(decode(t, w).Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Secret != mfa.setup.Secret || data.OtpauthURL != mfa.setup.OtpauthURL {
		t.Fatalf("应返回种子与 otpauth URL：%+v", data)
	}
	if len(mfa.beginFor) != 1 || mfa.beginFor[0] != 7 {
		t.Fatalf("应为当前管理员生成，调用记录=%v", mfa.beginFor)
	}
	// setup 是写请求，审计中间件记一条通用条目（方法 + 路由模板）
	if len(e.audit.entries) != 1 || e.audit.entries[0].Action != "POST /api/admin/mfa/totp/setup" {
		t.Fatalf("开始绑定应记一条通用审计，得到 %+v", e.audit.entries)
	}
}

func TestMFATOTPVerify_EnablesAndAudits(t *testing.T) {
	e, mfa, alice := mfaEnv(t)
	w := e.write(http.MethodPost, "/api/admin/mfa/totp/verify", `{"code":"123456"}`, alice, "tok-alice")
	if w.Code != http.StatusOK || decode(t, w).Code != "OK" {
		t.Fatalf("验证应 200 OK，得到 %d %s", w.Code, w.Body.String())
	}
	if mfa.confirmCode[7] != "123456" {
		t.Fatalf("应把验证码透传给服务，得到 %q", mfa.confirmCode[7])
	}
	if len(e.audit.entries) != 1 {
		t.Fatalf("启用二次验证应记一条审计，得到 %d", len(e.audit.entries))
	}
	got := e.audit.entries[0]
	if got.AdminID != 7 || got.Action != "admin.mfa_totp_enable" || got.TargetType != "admin" || got.TargetID != "7" {
		t.Fatalf("审计记录不符合预期：%+v", got)
	}
}

func TestMFATOTPVerify_ErrorsMapAndSkipAudit(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"验证码错误", errs.ErrBadRequest.WithMessage("验证码不正确"), http.StatusBadRequest, "BAD_REQUEST"},
		{"未配置密钥", errs.ErrUnavailable.WithMessage("后台未配置二次验证密钥"), http.StatusServiceUnavailable, "UNAVAILABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, mfa, alice := mfaEnv(t)
			mfa.confirmErr = tt.err
			w := e.write(http.MethodPost, "/api/admin/mfa/totp/verify", `{"code":"000000"}`, alice, "tok-alice")
			b := decode(t, w)
			if w.Code != tt.status || b.Code != tt.code {
				t.Fatalf("期望 %d %s，得到 %d %s", tt.status, tt.code, w.Code, w.Body.String())
			}
			if len(e.audit.entries) != 0 {
				t.Fatalf("失败的写请求不记审计，得到 %d 条", len(e.audit.entries))
			}
		})
	}
}
