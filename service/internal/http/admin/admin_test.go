package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	adm "xsite/service/internal/domain/admin"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
	binding.EnableDecoderDisallowUnknownFields = true // 与 NewRouter 一致
}

const (
	allowedOrigin = "http://localhost:5173"
	testUA        = "Mozilla/5.0 Test"
)

var (
	superRole    = adm.RoleRef{ID: 1, Code: "superadmin", Name: "超级管理员", IsSuper: true, Active: true}
	operatorRole = adm.RoleRef{ID: 2, Code: "operator", Name: "运营", Active: true}
)

// identity 默认已开启二次验证（MFAEnabled=true）：后台绝大多数测试不关心二次验证，
// 而 S2-6 切片 7 要求超管必须开启；个别需要「未开启」的用例显式置 false。
func identity(id int64, name string, role adm.RoleRef, csrf string, perms ...string) adm.Identity {
	x := adm.NewIdentity(id, name, role, perms)
	x.CSRFToken, x.SessionKey = csrf, "key-"+name
	x.MFAEnabled = true
	return x
}

// fakeAuth：会话令牌 → 身份；登录只接受 alice / right-password。
type fakeAuth struct {
	mu          sync.Mutex
	sessions    map[string]adm.Identity
	loginErr    error
	mfaRequired bool     // 为 true 时 Login 返回二次验证挑战而非会话
	loginMFAErr error    // 非空时 LoginMFA 直接返回该错误
	reauthErr   error    // 非空时 Reauth 直接返回该错误
	reauthed    []string // Reauth 成功过的会话键
}

func newFakeAuth() *fakeAuth { return &fakeAuth{sessions: map[string]adm.Identity{}} }

func (f *fakeAuth) aliceIdentity() adm.Identity {
	return identity(7, "alice", operatorRole, "csrf-alice", "dramas.view", "dramas.edit")
}

func (f *fakeAuth) Login(_ context.Context, in adm.LoginInput) (adm.LoginResult, error) {
	if f.loginErr != nil {
		return adm.LoginResult{}, f.loginErr
	}
	if in.Username != "alice" || in.Password != "right-password" {
		return adm.LoginResult{}, errs.ErrNeedLogin.WithMessage("用户名或密码错误")
	}
	if f.mfaRequired {
		return adm.LoginResult{MFARequired: true, MFATicket: "ticket-xyz", MFAMethods: []string{"totp"}}, nil
	}
	id := f.aliceIdentity()
	f.add("tok-alice", id)
	return adm.LoginResult{Token: "tok-alice", Identity: id}, nil
}

func (f *fakeAuth) LoginMFA(_ context.Context, in adm.LoginMFAInput) (adm.LoginResult, error) {
	if f.loginMFAErr != nil {
		return adm.LoginResult{}, f.loginMFAErr
	}
	if in.Ticket != "ticket-xyz" {
		return adm.LoginResult{}, errs.ErrNeedLogin.WithMessage("二次验证已超时，请重新登录")
	}
	if in.Method != "totp" || in.Code != "123456" {
		return adm.LoginResult{}, errs.ErrBadRequest.WithMessage("验证码不正确")
	}
	id := f.aliceIdentity()
	f.add("tok-alice", id)
	return adm.LoginResult{Token: "tok-alice", Identity: id}, nil
}

func (f *fakeAuth) Reauth(_ context.Context, id adm.Identity, method, code string, _ []byte, _ string) error {
	if f.reauthErr != nil {
		return f.reauthErr
	}
	if method != "totp" || code != "123456" {
		return errs.ErrBadRequest.WithMessage("验证码不正确")
	}
	f.mu.Lock()
	f.reauthed = append(f.reauthed, id.SessionKey)
	f.mu.Unlock()
	return nil
}

func (f *fakeAuth) MFATicketAdmin(_ context.Context, ticket string) (int64, error) {
	if ticket == "ticket-xyz" {
		return 7, nil
	}
	return 0, errs.ErrNeedLogin
}

func (f *fakeAuth) Authenticate(_ context.Context, token, ua string) (adm.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.sessions[token]
	if !ok || ua != testUA {
		return adm.Identity{}, errs.ErrNeedLogin
	}
	return id, nil
}

func (f *fakeAuth) Logout(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for tok, id := range f.sessions {
		if id.SessionKey == key {
			delete(f.sessions, tok)
		}
	}
	return nil
}

func (f *fakeAuth) add(token string, id adm.Identity) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[token] = id
}

// fakeManager 只记录调用；业务规则在 domain/admin 中测试。
type fakeManager struct {
	created  []adm.NewAccount
	setPerms []string
	err      error
}

func (f *fakeManager) ListAdmins(context.Context) ([]adm.AdminSummary, error) {
	login := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	return []adm.AdminSummary{{ID: 1, Username: "root", Status: adm.StatusActive, Role: superRole, LastLoginAt: &login}}, nil
}
func (f *fakeManager) CreateAdmin(_ context.Context, _ adm.Identity, in adm.NewAccount) (int64, error) {
	f.created = append(f.created, in)
	return 42, f.err
}
func (f *fakeManager) UpdateAdmin(context.Context, adm.Identity, int64, adm.AdminUpdate) (adm.AdminSummary, error) {
	return adm.AdminSummary{Role: operatorRole, Status: adm.StatusActive}, f.err
}
func (f *fakeManager) ResetAdminPassword(context.Context, adm.Identity, int64, string) error {
	return f.err
}
func (f *fakeManager) RevokeAdminSessions(context.Context, adm.Identity, int64) error { return f.err }
func (f *fakeManager) ResetAdminMFA(context.Context, adm.Identity, int64) error       { return f.err }
func (f *fakeManager) ListRoles(context.Context) ([]adm.RoleInfo, error) {
	return []adm.RoleInfo{{ID: 2, Code: "operator", Name: "运营", Active: true, Builtin: true, MemberCount: 3, Permissions: []string{"dramas.view"}}}, nil
}
func (f *fakeManager) CreateRole(context.Context, adm.RoleInput) (int64, error) { return 9, f.err }
func (f *fakeManager) UpdateRole(context.Context, int64, adm.RoleInput) (adm.RoleInfo, error) {
	return adm.RoleInfo{}, f.err
}
func (f *fakeManager) DeleteRole(context.Context, int64) (adm.RoleInfo, error) {
	return adm.RoleInfo{}, f.err
}
func (f *fakeManager) SetRolePermissions(_ context.Context, _ adm.Identity, _ int64, codes []string) ([]string, []string, error) {
	f.setPerms = codes
	return []string{"dramas.view"}, []string{"dramas.edit", "dramas.view"}, f.err
}
func (f *fakeManager) PermissionTree(context.Context) ([]adm.PermissionNode, error) {
	return []adm.PermissionNode{
		{Code: "dashboard", Kind: adm.MenuPage, Title: "数据看板", Permissions: []adm.Permission{{Code: "dashboard.view", Name: "查看"}}},
		{Code: "content", Kind: adm.MenuGroup, Title: "内容", Children: []adm.PermissionNode{
			{Code: "dramas", Kind: adm.MenuPage, Title: "剧集", Permissions: []adm.Permission{{Code: "dramas.view", Name: "查看"}}},
		}},
	}, nil
}
func (f *fakeManager) ListMenus(context.Context) ([]adm.Menu, error) { return nil, nil }
func (f *fakeManager) MyMenus(_ context.Context, actor adm.Identity) ([]adm.MenuNode, error) {
	out := []adm.MenuNode{{Menu: adm.Menu{Code: "dashboard", Kind: adm.MenuPage, Title: "数据看板", Path: "/", Icon: "LayoutDashboard"}}}
	g := adm.MenuNode{Menu: adm.Menu{Code: "content", Kind: adm.MenuGroup, Title: "内容"}}
	if actor.Can("dramas.view") {
		g.Children = append(g.Children, adm.Menu{Code: "dramas", Kind: adm.MenuPage, Title: "剧集", Path: "/dramas", Icon: "Clapperboard"})
	}
	return append(out, g), nil
}
func (f *fakeManager) CreateMenu(context.Context, adm.MenuInput) (int64, error) { return 5, f.err }
func (f *fakeManager) UpdateMenu(context.Context, int64, adm.MenuInput) (adm.Menu, error) {
	return adm.Menu{}, f.err
}
func (f *fakeManager) DeleteMenu(context.Context, int64) (adm.Menu, error) { return adm.Menu{}, f.err }

type memRecorder struct {
	mu      sync.Mutex
	entries []platform.AuditEntry
}

func (m *memRecorder) Record(e platform.AuditEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
}

type env struct {
	r      *gin.Engine
	auth   *fakeAuth
	manage *fakeManager
	audit  *memRecorder
	h      *Handlers
}

// newEnv 用与线上相同的 Routes 装配，另挂几条测试路由验证权限点与审计。
func newEnv(secureCookie bool) *env {
	e := &env{auth: newFakeAuth(), manage: &fakeManager{}, audit: &memRecorder{}}
	e.h = &Handlers{Auth: e.auth, Manage: e.manage, Cookie: NewSessionCookie(secureCookie)}
	e.r = gin.New()
	e.r.Use(middleware.RequestID())
	authed := e.h.Routes(e.r, []string{allowedOrigin}, e.audit)

	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": "OK", "message": "ok"}) }
	page := authed.Group("/test/dramas", RequirePage("dramas"))
	page.GET("", ok)
	page.POST("", ok)
	authed.POST("/test/custom-html", RequirePermission("ads.custom_html"), ok)
	authed.POST("/test/detailed", func(c *gin.Context) {
		SetAudit(c, AuditDetail{
			Action: "storage_provider.update", TargetType: "storage_provider", TargetID: "3",
			Before: map[string]any{"name": "旧", "secret_access_key": "old-sk"},
			After:  map[string]any{"name": "新", "secret_access_key": "new-sk"},
		})
		ok(c)
	})
	authed.POST("/test/plain", ok)
	authed.POST("/test/fail", func(c *gin.Context) { c.AbortWithStatus(http.StatusConflict) })
	authed.POST("/test/skip-audit", func(c *gin.Context) { SkipAudit(c); ok(c) })
	// 高敏写路由，用于验证重新验证门槛（S2-6）：写需要 sensitive.edit
	authed.POST("/test/sensitive", RequirePage("sensitive"), ok)
	return e
}

// fakePasskey 只记录调用，WebAuthn 加密流程在 domain/admin 用 virtualwebauthn 验证。
type fakePasskey struct {
	options json.RawMessage
	list    []adm.PasskeyInfo
	err     error
	deleted []int64
}

func (f *fakePasskey) BeginRegistration(context.Context, int64, string) (json.RawMessage, error) {
	return f.options, f.err
}
func (f *fakePasskey) FinishRegistration(context.Context, int64, string, string, []byte) error {
	return f.err
}
func (f *fakePasskey) ListPasskeys(context.Context, int64) ([]adm.PasskeyInfo, error) {
	return f.list, f.err
}
func (f *fakePasskey) DeletePasskey(_ context.Context, _, id int64) error {
	f.deleted = append(f.deleted, id)
	return f.err
}
func (f *fakePasskey) BeginAssertion(context.Context, int64, string) (json.RawMessage, error) {
	return f.options, f.err
}

// fakeReauthChecker 模拟会话重新验证窗口状态。ok 直接决定结果（门槛测试只关心放行/拦截，
// 不耦合测试环境的 c.ClientIP()）；IP 网段比较语义由 store/cache 集成测试与领域层覆盖。
type fakeReauthChecker struct{ ok bool }

func (f *fakeReauthChecker) IsReauthed(_ context.Context, _ string, _ string) (bool, error) {
	return f.ok, nil
}

type req struct {
	method, path, body string
	cookie, csrf       string
	origin             string
}

func (e *env) do(r req) *httptest.ResponseRecorder {
	hr := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	if r.body != "" {
		hr.Header.Set("Content-Type", "application/json")
	}
	hr.Header.Set("User-Agent", testUA)
	if r.cookie != "" {
		hr.AddCookie(&http.Cookie{Name: e.h.Cookie.Name, Value: r.cookie})
	}
	if r.csrf != "" {
		hr.Header.Set(HeaderCSRFToken, r.csrf)
	}
	if r.origin != "" {
		hr.Header.Set("Origin", r.origin)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, hr)
	return w
}

// write 以已登录身份发写请求（带正确的 Origin 与 CSRF）。
func (e *env) write(method, path, body string, as adm.Identity, token string) *httptest.ResponseRecorder {
	return e.do(req{method: method, path: path, body: body, cookie: token, csrf: as.CSRFToken, origin: allowedOrigin})
}

type envelope struct {
	Code      string          `json:"code"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"request_id"`
}

func decode(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var b envelope
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("响应不是 JSON：%q", w.Body.String())
	}
	return b
}

func sessionCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("响应中没有 Cookie %s：%v", name, w.Header().Values("Set-Cookie"))
	return nil
}

// ---------- 登录 ----------

func TestLogin_SetsHttpOnlySessionCookie(t *testing.T) {
	for _, secure := range []bool{false, true} {
		e := newEnv(secure)
		w := e.do(req{method: http.MethodPost, path: "/api/admin/login", origin: allowedOrigin,
			body: `{"username":"alice","password":"right-password"}`})
		if w.Code != http.StatusOK {
			t.Fatalf("登录应成功，得到 %d %s", w.Code, w.Body.String())
		}
		name := map[bool]string{false: "xs_admin", true: "__Host-xs_admin"}[secure]
		c := sessionCookie(t, w, name)
		if c.Value != "tok-alice" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.Secure != secure {
			t.Fatalf("Cookie 属性不符合要求（secure=%v）：%+v", secure, c)
		}
		if c.MaxAge != 0 || !c.Expires.IsZero() {
			t.Fatal("会话 Cookie 不应设置过期时间")
		}

		var data struct {
			MFARequired bool         `json:"mfa_required"`
			User        adminUserDTO `json:"user"`
			CSRFToken   string       `json:"csrf_token"`
			Permissions []string     `json:"permissions"`
		}
		if err := json.Unmarshal(decode(t, w).Data, &data); err != nil {
			t.Fatal(err)
		}
		if data.MFARequired || data.User.ID != 7 || data.User.Role != "operator" || data.User.RoleName != "运营" ||
			data.User.IsSuper || data.CSRFToken != "csrf-alice" {
			t.Fatalf("登录响应不符合预期：%+v", data)
		}
		if !slices.Equal(data.Permissions, []string{"dramas.edit", "dramas.view"}) {
			t.Fatalf("应下发排序后的权限点：%v", data.Permissions)
		}
		if strings.Contains(w.Body.String(), "tok-alice") {
			t.Fatal("会话令牌只能出现在 Cookie 中，不能出现在响应体")
		}
	}
}

func TestLogin_Errors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		origin     string
		loginErr   error
		status     int
		code       string
		retryAfter string
	}{
		{"缺少密码", `{"username":"alice"}`, allowedOrigin, nil, 400, "BAD_REQUEST", ""},
		{"未知字段被拒绝", `{"username":"alice","password":"right-password","role":"superadmin"}`, allowedOrigin, nil, 400, "BAD_REQUEST", ""},
		{"不是 JSON", `username=alice`, allowedOrigin, nil, 400, "BAD_REQUEST", ""},
		{"密码错误", `{"username":"alice","password":"nope-nope"}`, allowedOrigin, nil, 401, "NEED_LOGIN", ""},
		{"锁定时带 Retry-After", `{"username":"alice","password":"right-password"}`, allowedOrigin,
			errs.WithRetryAfter(errs.ErrRateLimited, 90*time.Second), 429, "RATE_LIMITED", "90"},
		{"来源不在白名单", `{"username":"alice","password":"right-password"}`, "https://evil.example", nil, 403, "FORBIDDEN", ""},
		{"来源为 null", `{"username":"alice","password":"right-password"}`, "null", nil, 403, "FORBIDDEN", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(false)
			e.auth.loginErr = tt.loginErr
			w := e.do(req{method: http.MethodPost, path: "/api/admin/login", origin: tt.origin, body: tt.body})
			b := decode(t, w)
			if w.Code != tt.status || b.Code != tt.code || b.RequestID == "" {
				t.Fatalf("期望 %d %s，得到 %d %s", tt.status, tt.code, w.Code, w.Body.String())
			}
			if got := w.Header().Get("Retry-After"); got != tt.retryAfter {
				t.Fatalf("Retry-After 期望 %q，得到 %q", tt.retryAfter, got)
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("登录失败不能下发 Cookie")
			}
		})
	}
}

func TestLogin_NoOriginAllowedForNonBrowserClients(t *testing.T) {
	e := newEnv(false)
	w := e.do(req{method: http.MethodPost, path: "/api/admin/login", body: `{"username":"alice","password":"right-password"}`})
	if w.Code != http.StatusOK {
		t.Fatalf("不带 Origin 的请求（curl 等）应放行，得到 %d", w.Code)
	}
}

// ---------- 二次验证登录（S2-6）----------

func TestLogin_MFAChallengeOmitsCookie(t *testing.T) {
	e := newEnv(false)
	e.auth.mfaRequired = true
	w := e.do(req{method: http.MethodPost, path: "/api/admin/login", origin: allowedOrigin,
		body: `{"username":"alice","password":"right-password"}`})
	if w.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d %s", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("需要二次验证时不能下发会话 Cookie")
	}
	var data struct {
		MFARequired bool     `json:"mfa_required"`
		MFATicket   string   `json:"mfa_ticket"`
		MFAMethods  []string `json:"mfa_methods"`
	}
	if err := json.Unmarshal(decode(t, w).Data, &data); err != nil {
		t.Fatal(err)
	}
	if !data.MFARequired || data.MFATicket != "ticket-xyz" || !slices.Equal(data.MFAMethods, []string{"totp"}) {
		t.Fatalf("应返回二次验证挑战：%+v", data)
	}
	if strings.Contains(w.Body.String(), `"user"`) || strings.Contains(w.Body.String(), "csrf") {
		t.Fatalf("挑战响应不应含会话信息：%s", w.Body.String())
	}
}

func TestLoginMFA_Success(t *testing.T) {
	e := newEnv(false)
	w := e.do(req{method: http.MethodPost, path: "/api/admin/login/mfa", origin: allowedOrigin,
		body: `{"mfa_ticket":"ticket-xyz","method":"totp","code":"123456"}`})
	if w.Code != http.StatusOK {
		t.Fatalf("应 200，得到 %d %s", w.Code, w.Body.String())
	}
	c := sessionCookie(t, w, "xs_admin")
	if c.Value != "tok-alice" || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
		t.Fatalf("应下发会话 Cookie：%+v", c)
	}
	var data sessionDTO
	if err := json.Unmarshal(decode(t, w).Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.User.ID != 7 || data.CSRFToken != "csrf-alice" {
		t.Fatalf("应返回会话信息：%+v", data)
	}
	if strings.Contains(w.Body.String(), "tok-alice") {
		t.Fatal("会话令牌只能在 Cookie，不能出现在响应体")
	}
}

func TestLoginMFA_Errors(t *testing.T) {
	tests := []struct {
		name, body, origin string
		status             int
		code               string
	}{
		{"验证码错误", `{"mfa_ticket":"ticket-xyz","method":"totp","code":"000000"}`, allowedOrigin, 400, "BAD_REQUEST"},
		{"票据失效", `{"mfa_ticket":"bad","method":"totp","code":"123456"}`, allowedOrigin, 401, "NEED_LOGIN"},
		{"缺少验证码", `{"mfa_ticket":"ticket-xyz","method":"totp"}`, allowedOrigin, 400, "BAD_REQUEST"},
		{"未知字段被拒绝", `{"mfa_ticket":"ticket-xyz","method":"totp","code":"123456","x":1}`, allowedOrigin, 400, "BAD_REQUEST"},
		{"来源不在白名单", `{"mfa_ticket":"ticket-xyz","method":"totp","code":"123456"}`, "https://evil.example", 403, "FORBIDDEN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(false)
			w := e.do(req{method: http.MethodPost, path: "/api/admin/login/mfa", origin: tt.origin, body: tt.body})
			b := decode(t, w)
			if w.Code != tt.status || b.Code != tt.code {
				t.Fatalf("期望 %d %s，得到 %d %s", tt.status, tt.code, w.Code, w.Body.String())
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("失败不能下发 Cookie")
			}
		})
	}
}

// ---------- 高敏操作重新验证（S2-6）----------

func TestReauth_SucceedsAndAudits(t *testing.T) {
	e := newEnv(false)
	alice := identity(7, "alice", operatorRole, "csrf-alice")
	e.auth.add("tok-alice", alice)

	w := e.write(http.MethodPost, "/api/admin/reauth", `{"method":"totp","code":"123456"}`, alice, "tok-alice")
	if w.Code != http.StatusOK || decode(t, w).Code != "OK" {
		t.Fatalf("重新验证应 200 OK，得到 %d %s", w.Code, w.Body.String())
	}
	if len(e.auth.reauthed) != 1 || e.auth.reauthed[0] != alice.SessionKey {
		t.Fatalf("应对当前会话重新验证：%v", e.auth.reauthed)
	}
	if len(e.audit.entries) != 1 || e.audit.entries[0].Action != "admin.reauth" || e.audit.entries[0].TargetID != "7" {
		t.Fatalf("应记一条 admin.reauth 审计：%+v", e.audit.entries)
	}
}

func TestReauth_Errors(t *testing.T) {
	e := newEnv(false)
	alice := identity(7, "alice", operatorRole, "csrf-alice")
	e.auth.add("tok-alice", alice)

	// 验证码错误 → 400
	if w := e.write(http.MethodPost, "/api/admin/reauth", `{"method":"totp","code":"000000"}`, alice, "tok-alice"); w.Code != http.StatusBadRequest {
		t.Fatalf("验证码错误应 400，得到 %d", w.Code)
	}
	// 缺字段 → 400
	if w := e.write(http.MethodPost, "/api/admin/reauth", `{"method":"totp"}`, alice, "tok-alice"); w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 code 应 400，得到 %d", w.Code)
	}
	// 缺 CSRF → 403
	if w := e.do(req{method: http.MethodPost, path: "/api/admin/reauth", cookie: "tok-alice", origin: allowedOrigin, body: `{"method":"totp","code":"123456"}`}); w.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 应 403，得到 %d", w.Code)
	}
	// 未登录 → 401
	if w := e.do(req{method: http.MethodPost, path: "/api/admin/reauth", origin: allowedOrigin, body: `{"method":"totp","code":"123456"}`}); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，得到 %d", w.Code)
	}
}

// ---------- Passkey（S2-6）----------

func TestPasskey_SelfServiceRequiresSession(t *testing.T) {
	e := newEnv(false)
	e.h.Passkey = &fakePasskey{options: json.RawMessage(`{"challenge":"x"}`)}
	if w := e.do(req{method: http.MethodGet, path: "/api/admin/mfa/passkeys"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录列出 Passkey 应 401，得到 %d", w.Code)
	}
	if w := e.do(req{method: http.MethodPost, path: "/api/admin/mfa/passkey/register/begin", origin: allowedOrigin}); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录注册 begin 应 401，得到 %d", w.Code)
	}
}

func TestPasskey_RegisterBeginAndList(t *testing.T) {
	e := newEnv(false)
	pk := &fakePasskey{options: json.RawMessage(`{"challenge":"abc"}`), list: []adm.PasskeyInfo{{ID: 3, Name: "办公室"}}}
	e.h.Passkey = pk
	alice := identity(7, "alice", operatorRole, "csrf-alice")
	e.auth.add("tok-alice", alice)

	w := e.write(http.MethodPost, "/api/admin/mfa/passkey/register/begin", "", alice, "tok-alice")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"challenge":"abc"`) {
		t.Fatalf("注册 begin 应回选项，得到 %d %s", w.Code, w.Body.String())
	}

	w = e.do(req{method: http.MethodGet, path: "/api/admin/mfa/passkeys", cookie: "tok-alice"})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "办公室") {
		t.Fatalf("列表应回 Passkey，得到 %d %s", w.Code, w.Body.String())
	}
}

func TestPasskey_DeleteRequiresCSRFAndAudits(t *testing.T) {
	e := newEnv(false)
	pk := &fakePasskey{}
	e.h.Passkey = pk
	alice := identity(7, "alice", operatorRole, "csrf-alice")
	e.auth.add("tok-alice", alice)

	// 缺 CSRF → 403
	if w := e.do(req{method: http.MethodDelete, path: "/api/admin/mfa/passkeys/3", cookie: "tok-alice", origin: allowedOrigin}); w.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 应 403，得到 %d", w.Code)
	}
	// 正常删除 → 200 + 审计
	w := e.write(http.MethodDelete, "/api/admin/mfa/passkeys/3", "", alice, "tok-alice")
	if w.Code != http.StatusOK || len(pk.deleted) != 1 || pk.deleted[0] != 3 {
		t.Fatalf("删除应成功并透传 id，得到 %d deleted=%v", w.Code, pk.deleted)
	}
	if len(e.audit.entries) != 1 || e.audit.entries[0].Action != "admin.mfa_passkey_delete" {
		t.Fatalf("应记审计 admin.mfa_passkey_delete：%+v", e.audit.entries)
	}
}

func TestPasskey_LoginBeginPublicWithTicket(t *testing.T) {
	e := newEnv(false)
	e.h.Passkey = &fakePasskey{options: json.RawMessage(`{"challenge":"login"}`)}

	// 有效票据（fakeAuth.MFATicketAdmin 认 ticket-xyz）→ 200 选项
	w := e.do(req{method: http.MethodPost, path: "/api/admin/login/passkey/begin", origin: allowedOrigin, body: `{"mfa_ticket":"ticket-xyz"}`})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"challenge":"login"`) {
		t.Fatalf("凭有效票据应回断言选项，得到 %d %s", w.Code, w.Body.String())
	}
	// 无效票据 → 401
	w = e.do(req{method: http.MethodPost, path: "/api/admin/login/passkey/begin", origin: allowedOrigin, body: `{"mfa_ticket":"bad"}`})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无效票据应 401，得到 %d", w.Code)
	}
}

func TestRequireSuperMFA(t *testing.T) {
	e := newEnv(false)
	e.h.Passkey = &fakePasskey{list: []adm.PasskeyInfo{}}
	noMFA := identity(1, "root", superRole, "csrf-root")
	noMFA.MFAEnabled = false
	e.auth.add("tok-nomfa", noMFA)
	withMFA := identity(2, "root2", superRole, "csrf-root2") // 默认 MFAEnabled=true
	e.auth.add("tok-mfa", withMFA)

	// 无二次验证的超管：业务接口一律 403
	if w := e.do(req{method: http.MethodGet, path: "/api/admin/admins", cookie: "tok-nomfa"}); w.Code != http.StatusForbidden {
		t.Fatalf("无二次验证超管访问业务接口应 403，得到 %d %s", w.Code, w.Body.String())
	}
	// 但可查看自己、访问开启二次验证相关接口
	if w := e.do(req{method: http.MethodGet, path: "/api/admin/me", cookie: "tok-nomfa"}); w.Code != http.StatusOK {
		t.Fatalf("应允许 /me，得到 %d", w.Code)
	}
	if w := e.do(req{method: http.MethodGet, path: "/api/admin/mfa/passkeys", cookie: "tok-nomfa"}); w.Code != http.StatusOK {
		t.Fatalf("应允许开启二次验证相关接口，得到 %d %s", w.Code, w.Body.String())
	}
	// 已开启二次验证的超管：业务接口正常
	if w := e.do(req{method: http.MethodGet, path: "/api/admin/admins", cookie: "tok-mfa"}); w.Code != http.StatusOK {
		t.Fatalf("已开启二次验证超管应放行，得到 %d %s", w.Code, w.Body.String())
	}
}

func TestReauthGate_SensitiveWriteRequiresRecentReauth(t *testing.T) {
	e := newEnv(false)
	SetSensitivePermissions([]string{"sensitive.edit"})
	defer SetSensitivePermissions(nil)
	rc := &fakeReauthChecker{}
	SetReauthChecker(rc)
	defer SetReauthChecker(nil)

	// 已绑定二次验证、但不在重新验证窗口（或网段已变）→ 412
	mfaID := identity(8, "mfauser", superRole, "csrf-m")
	mfaID.MFAEnabled = true
	e.auth.add("tok-m", mfaID)
	if w := e.write(http.MethodPost, "/api/admin/test/sensitive", "", mfaID, "tok-m"); w.Code != http.StatusPreconditionFailed || decode(t, w).Code != "REAUTH_REQUIRED" {
		t.Fatalf("高敏写应要求重新验证 412，得到 %d %s", w.Code, w.Body.String())
	}

	// 当前网段内重新验证过 → 放行
	rc.ok = true
	if w := e.write(http.MethodPost, "/api/admin/test/sensitive", "", mfaID, "tok-m"); w.Code != http.StatusOK {
		t.Fatalf("重新验证后应放行，得到 %d %s", w.Code, w.Body.String())
	}

	// 未绑定二次验证的账号不被重验门槛拦（没有可重新验证的二次因子）。用非超管避免被「超管强制开启」拦。
	plain := identity(9, "plain", operatorRole, "csrf-p", "sensitive.edit")
	plain.MFAEnabled = false
	e.auth.add("tok-p", plain)
	if w := e.write(http.MethodPost, "/api/admin/test/sensitive", "", plain, "tok-p"); w.Code != http.StatusOK {
		t.Fatalf("未绑定二次验证的账号不应被门槛拦，得到 %d %s", w.Code, w.Body.String())
	}
}

// ---------- 会话 ----------

func TestMe(t *testing.T) {
	e := newEnv(false)
	e.auth.add("tok-r", identity(1, "root", superRole, "csrf-r"))

	w := e.do(req{method: http.MethodGet, path: "/api/admin/me"})
	if w.Code != http.StatusUnauthorized || decode(t, w).Code != "NEED_LOGIN" {
		t.Fatalf("没有 Cookie 应 401 NEED_LOGIN，得到 %d %s", w.Code, w.Body.String())
	}

	w = e.do(req{method: http.MethodGet, path: "/api/admin/me", cookie: "tok-r"})
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d %s", w.Code, w.Body.String())
	}
	var data sessionDTO
	if err := json.Unmarshal(decode(t, w).Data, &data); err != nil {
		t.Fatal(err)
	}
	if !data.User.IsSuper || data.User.RoleName != "超级管理员" || data.CSRFToken != "csrf-r" || data.Permissions == nil || len(data.Permissions) != 0 {
		t.Fatalf("超管的 permissions 为空数组、以 is_super 表示全部：%+v", data)
	}
}

func TestMe_InvalidSessionClearsCookie(t *testing.T) {
	e := newEnv(false)
	w := e.do(req{method: http.MethodGet, path: "/api/admin/me", cookie: "expired"})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("期望 401，得到 %d", w.Code)
	}
	if c := sessionCookie(t, w, "xs_admin"); c.MaxAge >= 0 || c.Value != "" {
		t.Fatalf("失效会话应清除 Cookie：%+v", c)
	}
}

func TestLogout_RequiresCSRFAndIsAudited(t *testing.T) {
	e := newEnv(false)
	e.auth.add("tok-a", identity(7, "alice", operatorRole, "csrf-a"))

	for name, r := range map[string]req{
		"缺少 CSRF 令牌": {method: http.MethodPost, path: "/api/admin/logout", cookie: "tok-a", origin: allowedOrigin},
		"CSRF 令牌错误":  {method: http.MethodPost, path: "/api/admin/logout", cookie: "tok-a", origin: allowedOrigin, csrf: "csrf-x"},
		"来源不在白名单":    {method: http.MethodPost, path: "/api/admin/logout", cookie: "tok-a", origin: "https://evil.example", csrf: "csrf-a"},
	} {
		if w := e.do(r); w.Code != http.StatusForbidden {
			t.Fatalf("%s：期望 403，得到 %d", name, w.Code)
		}
	}
	if len(e.audit.entries) != 0 {
		t.Fatal("被拒绝的写请求不记审计")
	}

	w := e.do(req{method: http.MethodPost, path: "/api/admin/logout", cookie: "tok-a", origin: allowedOrigin, csrf: "csrf-a"})
	if w.Code != http.StatusOK || decode(t, w).Code != "OK" {
		t.Fatalf("登出应成功，得到 %d %s", w.Code, w.Body.String())
	}
	if c := sessionCookie(t, w, "xs_admin"); c.MaxAge >= 0 {
		t.Fatal("登出应清除 Cookie")
	}
	if w := e.do(req{method: http.MethodGet, path: "/api/admin/me", cookie: "tok-a"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("登出后会话应失效，得到 %d", w.Code)
	}
	if len(e.audit.entries) != 1 {
		t.Fatalf("登出应记一条审计，得到 %d", len(e.audit.entries))
	}
	got := e.audit.entries[0]
	if got.AdminID != 7 || got.Action != "admin.logout" || got.TargetType != "admin" || got.TargetID != "7" || got.RequestID == "" {
		t.Fatalf("审计记录不符合预期：%+v", got)
	}
}

// ---------- 权限点 ----------

func TestRequirePermission(t *testing.T) {
	e := newEnv(false)
	people := map[string]adm.Identity{
		"super":  identity(1, "root", superRole, "c1"),
		"editor": identity(2, "editor", operatorRole, "c2", "dramas.view", "dramas.edit"),
		"viewer": identity(3, "viewer", operatorRole, "c3", "dramas.view"),
		"html":   identity(4, "html", operatorRole, "c4", "ads.custom_html"),
		"none":   identity(5, "none", operatorRole, "c5"),
	}
	for name, id := range people {
		e.auth.add("tok-"+name, id)
	}
	tests := []struct {
		who, method, path string
		want              int
	}{
		{"super", "GET", "/api/admin/test/dramas", 200}, {"super", "POST", "/api/admin/test/dramas", 200},
		{"super", "POST", "/api/admin/test/custom-html", 200},
		{"editor", "GET", "/api/admin/test/dramas", 200}, {"editor", "POST", "/api/admin/test/dramas", 200},
		{"editor", "POST", "/api/admin/test/custom-html", 403},
		{"viewer", "GET", "/api/admin/test/dramas", 200}, {"viewer", "POST", "/api/admin/test/dramas", 403},
		{"html", "GET", "/api/admin/test/dramas", 403}, {"html", "POST", "/api/admin/test/custom-html", 200},
		{"none", "GET", "/api/admin/test/dramas", 403},
		// 管理接口同样按权限点判定
		{"viewer", "GET", "/api/admin/admins", 403}, {"super", "GET", "/api/admin/admins", 200},
		{"viewer", "GET", "/api/admin/roles", 403}, {"viewer", "GET", "/api/admin/permissions", 403},
		{"viewer", "GET", "/api/admin/me/menus", 200},
	}
	for _, tt := range tests {
		id := people[tt.who]
		var w *httptest.ResponseRecorder
		if tt.method == "GET" {
			w = e.do(req{method: "GET", path: tt.path, cookie: "tok-" + tt.who})
		} else {
			w = e.write(tt.method, tt.path, "", id, "tok-"+tt.who)
		}
		if w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d", tt.who, tt.method, tt.path, tt.want, w.Code)
		}
	}

	if w := e.do(req{method: "GET", path: "/api/admin/test/dramas"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录访问业务接口应 401，得到 %d", w.Code)
	}
}

func TestDeclaredPermissions(t *testing.T) {
	newEnv(false)
	got := DeclaredPermissions()
	for _, want := range []string{"admins.view", "admins.edit", "roles.edit", "role_permissions.edit", "admin_menus.edit", "dramas.edit"} {
		if !slices.Contains(got, want) {
			t.Errorf("路由声明的权限点应被登记：缺少 %s（%v）", want, got)
		}
	}
}

// ---------- 管理接口 ----------

func TestManageEndpoints(t *testing.T) {
	e := newEnv(false)
	root := identity(1, "root", superRole, "c1")
	e.auth.add("tok-root", root)

	w := e.write("POST", "/api/admin/admins", `{"username":"writer","password":"s3cure-pass"}`, root, "tok-root")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺少 role_id 应 400，得到 %d", w.Code)
	}
	w = e.write("POST", "/api/admin/admins", `{"username":"writer","password":"s3cure-pass","role_id":2}`, root, "tok-root")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"id":42`) {
		t.Fatalf("创建管理员应返回 id，得到 %d %s", w.Code, w.Body.String())
	}
	created := e.audit.entries[len(e.audit.entries)-1]
	if created.Action != "admin.create" || created.TargetID != "42" || strings.Contains(string(created.After), "s3cure-pass") {
		t.Fatalf("审计记录不应包含密码：%+v %s", created, created.After)
	}

	w = e.write("PATCH", "/api/admin/admins/abc", `{"role_id":2,"status":"active"}`, root, "tok-root")
	if w.Code != http.StatusNotFound {
		t.Fatalf("非法 id 应 404，得到 %d", w.Code)
	}
	w = e.write("PATCH", "/api/admin/admins/3", `{"role_id":2,"status":"unused"}`, root, "tok-root")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("状态只能是 active / inactive，得到 %d", w.Code)
	}

	w = e.write("PUT", "/api/admin/roles/3/permissions", `{"codes":["dramas.edit"]}`, root, "tok-root")
	if w.Code != http.StatusOK || !slices.Equal(e.manage.setPerms, []string{"dramas.edit"}) {
		t.Fatalf("设置角色权限应透传权限点，得到 %d %s", w.Code, w.Body.String())
	}
	last := e.audit.entries[len(e.audit.entries)-1]
	if last.Action != "role.set_permissions" || !strings.Contains(string(last.Before), "dramas.view") || !strings.Contains(string(last.After), "dramas.edit") {
		t.Fatalf("权限变更应记录前后差异：%+v", last)
	}

	w = e.write("PATCH", "/api/admin/admin-menus/5", `{"title":"剧集"}`, root, "tok-root")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("visible 必填，得到 %d", w.Code)
	}
	w = e.write("POST", "/api/admin/admin-menus", `{"title":"常用","visible":true}`, root, "tok-root")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("新建菜单必须指定类型，得到 %d", w.Code)
	}

	e.manage.err = errs.ErrConflict.WithMessage("内置角色不能删除")
	w = e.write("DELETE", "/api/admin/roles/2", "", root, "tok-root")
	if w.Code != http.StatusConflict || decode(t, w).Message != "内置角色不能删除" {
		t.Fatalf("业务错误应原样映射，得到 %d %s", w.Code, w.Body.String())
	}
}

func TestMyMenusAndLists(t *testing.T) {
	e := newEnv(false)
	e.auth.add("tok-v", identity(3, "viewer", operatorRole, "c3", "dramas.view"))
	e.auth.add("tok-r", identity(1, "root", superRole, "c1"))

	w := e.do(req{method: "GET", path: "/api/admin/me/menus", cookie: "tok-v"})
	var nodes []navNodeDTO
	if err := json.Unmarshal(decode(t, w).Data, &nodes); err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].Kind != "page" || nodes[0].Path != "/" || nodes[1].Kind != "group" ||
		len(nodes[1].Children) != 1 || nodes[1].Children[0].Path != "/dramas" || nodes[1].Children[0].Icon != "Clapperboard" {
		t.Fatalf("侧栏结构不符合预期：%s", w.Body.String())
	}
	// 没有子菜单时 children 是空数组，客户端不用判断 null
	if !strings.Contains(w.Body.String(), `"path":"/","icon":"LayoutDashboard","children":[]`) {
		t.Fatalf("一级页面的 children 应为空数组：%s", w.Body.String())
	}

	w = e.do(req{method: "GET", path: "/api/admin/roles", cookie: "tok-r"})
	var roles listDTO[roleDTO]
	if err := json.Unmarshal(decode(t, w).Data, &roles); err != nil {
		t.Fatal(err)
	}
	if roles.Total != 1 || roles.List[0].Status != "active" || roles.List[0].MemberCount != 3 {
		t.Fatalf("角色列表不符合预期：%s", w.Body.String())
	}

	w = e.do(req{method: "GET", path: "/api/admin/admins", cookie: "tok-r"})
	if !strings.Contains(w.Body.String(), `"last_login_at":"2026-10-21T08:00:00Z"`) || strings.Contains(w.Body.String(), "password") {
		t.Fatalf("管理员列表不符合预期：%s", w.Body.String())
	}

	w = e.do(req{method: "GET", path: "/api/admin/permissions", cookie: "tok-r"})
	var tree []permissionNodeDTO
	if err := json.Unmarshal(decode(t, w).Data, &tree); err != nil {
		t.Fatal(err)
	}
	if len(tree) != 2 || tree[0].Kind != "page" || len(tree[0].Permissions) != 1 || tree[0].Children == nil ||
		tree[1].Kind != "group" || tree[1].Permissions == nil || len(tree[1].Children) != 1 ||
		tree[1].Children[0].Permissions[0].Code != "dramas.view" {
		t.Fatalf("权限树不符合预期：%s", w.Body.String())
	}
}

// ---------- 审计 ----------

func TestAudit(t *testing.T) {
	e := newEnv(false)
	root := identity(1, "root", superRole, "csrf-s")
	e.auth.add("tok-s", root)

	if code := e.write("POST", "/api/admin/test/detailed", "", root, "tok-s").Code; code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d", code)
	}
	if code := e.write("POST", "/api/admin/test/plain", "", root, "tok-s").Code; code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d", code)
	}
	if code := e.write("POST", "/api/admin/test/fail", "", root, "tok-s").Code; code != http.StatusConflict {
		t.Fatalf("期望 409，得到 %d", code)
	}
	// 数据面高频写入（视频分片）显式跳过审计
	if code := e.write("POST", "/api/admin/test/skip-audit", "", root, "tok-s").Code; code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d", code)
	}
	e.do(req{method: http.MethodGet, path: "/api/admin/test/dramas", cookie: "tok-s"})

	if len(e.audit.entries) != 2 {
		t.Fatalf("只有成功、未声明跳过的写请求记审计（读请求、失败请求、SkipAudit 不记），得到 %d 条", len(e.audit.entries))
	}
	detailed, plain := e.audit.entries[0], e.audit.entries[1]
	if detailed.Action != "storage_provider.update" || detailed.TargetType != "storage_provider" || detailed.TargetID != "3" {
		t.Fatalf("应使用 handler 补充的审计内容：%+v", detailed)
	}
	for _, raw := range []json.RawMessage{detailed.Before, detailed.After} {
		if strings.Contains(string(raw), "-sk") || !strings.Contains(string(raw), `"***"`) {
			t.Fatalf("密钥字段应脱敏：%s", raw)
		}
	}
	if !strings.Contains(string(detailed.After), "新") {
		t.Fatalf("普通字段应保留：%s", detailed.After)
	}
	if plain.Action != "POST /api/admin/test/plain" || plain.Before != nil || plain.After != nil {
		t.Fatalf("未补充内容时动作为「方法 + 路由模板」：%+v", plain)
	}
}
