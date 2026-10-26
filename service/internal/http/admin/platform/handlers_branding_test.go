package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	adm "xsite/service/internal/domain/admin"
	dom "xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/middleware"
)

// 白标接口的 HTTP 层测试：权限点、CSRF 与 Origin、分段保存的审计、领域层错误透传。

func init() {
	gin.SetMode(gin.TestMode)
	binding.EnableDecoderDisallowUnknownFields = true // 与 NewRouter 一致
}

const (
	origin = "http://localhost:5173"
	testUA = "Mozilla/5.0 Test"
)

// fakeAuth：会话令牌即管理员用户名。
type fakeAuth struct{ ids map[string]adm.Identity }

func (f fakeAuth) Login(context.Context, adm.LoginInput) (adm.LoginResult, error) {
	return adm.LoginResult{}, errs.ErrNeedLogin
}

func (f fakeAuth) LoginMFA(context.Context, adm.LoginMFAInput) (adm.LoginResult, error) {
	return adm.LoginResult{}, errs.ErrNeedLogin
}

func (f fakeAuth) Reauth(context.Context, adm.Identity, string, string, []byte, string) error {
	return nil
}

func (f fakeAuth) MFATicketAdmin(context.Context, string) (int64, error) { return 0, errs.ErrNeedLogin }

func (f fakeAuth) Authenticate(_ context.Context, token, _ string) (adm.Identity, error) {
	id, ok := f.ids[token]
	if !ok {
		return adm.Identity{}, errs.ErrNeedLogin
	}
	return id, nil
}

func (f fakeAuth) Logout(context.Context, string) error { return nil }

func who(name string, perms ...string) adm.Identity {
	id := adm.NewIdentity(int64(len(name)), name, adm.RoleRef{ID: 2, Code: "ops", Name: "运营", Active: true}, perms)
	id.CSRFToken = "csrf-" + name
	id.MFAEnabled = true
	return id
}

type memRecorder struct {
	mu      sync.Mutex
	entries []dom.AuditEntry
}

func (m *memRecorder) Record(e dom.AuditEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
}

func (m *memRecorder) last(t *testing.T) dom.AuditEntry {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) == 0 {
		t.Fatal("没有审计记录")
	}
	return m.entries[len(m.entries)-1]
}

// fakeBranding 记录收到的入参，可指定返回的错误。
type fakeBranding struct {
	mu     sync.Mutex
	brand  dom.Brand
	theme  dom.Theme
	layout dom.Layout
	saved  []string
	err    error
}

func (f *fakeBranding) Get(context.Context) (dom.BrandingConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return dom.BrandingConfig{Brand: f.brand, Theme: f.theme, Layout: f.layout}, nil
}

func (f *fakeBranding) SetBrand(_ context.Context, in dom.Brand) (dom.BrandingConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, "brand")
	if f.err != nil {
		return dom.BrandingConfig{}, f.err
	}
	f.brand = in
	return dom.BrandingConfig{Brand: f.brand, Theme: f.theme, Layout: f.layout}, nil
}

func (f *fakeBranding) SetTheme(_ context.Context, in dom.Theme) (dom.BrandingConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, "theme")
	if f.err != nil {
		return dom.BrandingConfig{}, f.err
	}
	f.theme = in
	return dom.BrandingConfig{Brand: f.brand, Theme: f.theme, Layout: f.layout}, nil
}

func (f *fakeBranding) SetLayout(_ context.Context, in dom.Layout) (dom.BrandingConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, "layout")
	if f.err != nil {
		return dom.BrandingConfig{}, f.err
	}
	f.layout = in
	return dom.BrandingConfig{Brand: f.brand, Theme: f.theme, Layout: f.layout}, nil
}

type env struct {
	r        *gin.Engine
	branding *fakeBranding
	audit    *memRecorder
	ids      map[string]adm.Identity
}

func newEnv() *env {
	e := &env{branding: &fakeBranding{}, audit: &memRecorder{}, ids: map[string]adm.Identity{
		"viewer": who("viewer", "brand.view"),
		"editor": who("editor", "brand.view", "brand.edit"),
		"other":  who("other", "seo.view", "seo.edit"),
		"nobody": who("nobody"),
	}}
	h := &admin.Handlers{Auth: fakeAuth{ids: e.ids}, Cookie: admin.NewSessionCookie(false)}
	e.r = gin.New()
	e.r.Use(middleware.RequestID())
	authed := h.Routes(e.r, []string{origin}, e.audit)
	(&Handlers{Branding: e.branding}).Routes(authed)
	return e
}

// do 以 as 的身份请求；写请求带上正确的 Origin 与 CSRF 令牌。
func (e *env) do(as, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("User-Agent", testUA)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if as != "" {
		req.AddCookie(&http.Cookie{Name: "xs_admin", Value: as})
		if method != http.MethodGet {
			req.Header.Set("Origin", origin)
			req.Header.Set(admin.HeaderCSRFToken, e.ids[as].CSRFToken)
		}
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

type envelope struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func expect(t *testing.T, w *httptest.ResponseRecorder, status int, code string) envelope {
	t.Helper()
	var b envelope
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("响应不是 JSON：%q", w.Body.String())
	}
	if w.Code != status || b.Code != code {
		t.Fatalf("期望 %d %s，得到 %d %s", status, code, w.Code, w.Body.String())
	}
	return b
}

const (
	brandBody  = `{"site_name":"短剧站","slogan":"每天追好剧","copyright":"© 2026","support_url":"https://t.me/support","logo_light":"img/logo.webp","favicon":"img/favicon.png"}`
	themeBody  = `{"preset":"midnight","color_scheme":"light","allow_scheme_toggle":true,"colors":{"primary":"#ff0033"},"radius":8,"cover_ratio":"16:9","font":"serif"}`
	layoutBody = `{"header_style":"pill","card_style":"overlay","play_mode":"all_immersive","mobile_tabbar":false}`
)

func TestBrandingPermissions(t *testing.T) {
	e := newEnv()
	expect(t, e.do("", http.MethodGet, "/api/admin/brand", ""), http.StatusUnauthorized, "NEED_LOGIN")
	expect(t, e.do("nobody", http.MethodGet, "/api/admin/brand", ""), http.StatusForbidden, "FORBIDDEN")
	// 只有 SEO 权限的角色读不了白标
	expect(t, e.do("other", http.MethodGet, "/api/admin/brand", ""), http.StatusForbidden, "FORBIDDEN")
	expect(t, e.do("viewer", http.MethodGet, "/api/admin/brand", ""), http.StatusOK, "OK")
	// 只有查看权限：三个保存接口一律 403
	for _, c := range []struct{ path, body string }{
		{"/api/admin/brand", brandBody},
		{"/api/admin/theme", themeBody},
		{"/api/admin/layout", layoutBody},
	} {
		expect(t, e.do("viewer", http.MethodPut, c.path, c.body), http.StatusForbidden, "FORBIDDEN")
	}
	expect(t, e.do("editor", http.MethodPut, "/api/admin/brand", brandBody), http.StatusOK, "OK")
}

func TestBrandingCSRFAndOrigin(t *testing.T) {
	e := newEnv()
	req := httptest.NewRequest(http.MethodPut, "/api/admin/theme", strings.NewReader(themeBody))
	req.Header.Set("User-Agent", testUA)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.AddCookie(&http.Cookie{Name: "xs_admin", Value: "editor"})
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req) // 缺 CSRF 令牌
	expect(t, w, http.StatusForbidden, "FORBIDDEN")

	req = httptest.NewRequest(http.MethodPut, "/api/admin/theme", strings.NewReader(themeBody))
	req.Header.Set("User-Agent", testUA)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set(admin.HeaderCSRFToken, e.ids["editor"].CSRFToken)
	req.AddCookie(&http.Cookie{Name: "xs_admin", Value: "editor"})
	w = httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	expect(t, w, http.StatusForbidden, "FORBIDDEN")
}

func TestBrandingSaveAndAudit(t *testing.T) {
	e := newEnv()

	body := expect(t, e.do("editor", http.MethodPut, "/api/admin/brand", brandBody), http.StatusOK, "OK")
	var cfg dom.BrandingConfig
	if err := json.Unmarshal(body.Data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Brand.SiteName != "短剧站" || cfg.Brand.LogoLight != "img/logo.webp" {
		t.Fatalf("请求体透传：%+v", cfg.Brand)
	}
	if a := e.audit.last(t); a.Action != "site_config.site" || a.TargetType != "site_config" || a.TargetID != "site" || a.After == nil {
		t.Fatalf("品牌审计：%+v", a)
	}

	expect(t, e.do("editor", http.MethodPut, "/api/admin/theme", themeBody), http.StatusOK, "OK")
	a := e.audit.last(t)
	if a.Action != "site_config.theme" || a.TargetID != "theme" || a.Before == nil || a.After == nil {
		t.Fatalf("配色审计带前后值：%+v", a)
	}
	// 审计里只放改动的那一段，不把另外两段也塞进去
	raw, err := json.Marshal(a.After)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "site_name") || strings.Contains(string(raw), "header_style") {
		t.Fatalf("配色审计不该含别的段：%s", raw)
	}

	expect(t, e.do("editor", http.MethodPut, "/api/admin/layout", layoutBody), http.StatusOK, "OK")
	if a := e.audit.last(t); a.Action != "site_config.layout" || a.TargetID != "layout" {
		t.Fatalf("布局审计：%+v", a)
	}

	// 三次保存各自只写一段
	e.branding.mu.Lock()
	saved := strings.Join(e.branding.saved, ",")
	e.branding.mu.Unlock()
	if saved != "brand,theme,layout" {
		t.Fatalf("分段保存：%s", saved)
	}

	// 未知字段 400（防止前端把别的段的字段塞进来却以为保存成功）
	expect(t, e.do("editor", http.MethodPut, "/api/admin/brand", `{"site_name":"站","bogus":1}`), http.StatusBadRequest, "BAD_REQUEST")
}

func TestBrandingDomainErrorPassthrough(t *testing.T) {
	e := newEnv()
	e.branding.err = errs.ErrBadRequest.WithMessage("卡片圆角为 0–32 像素")
	body := expect(t, e.do("editor", http.MethodPut, "/api/admin/theme", themeBody), http.StatusBadRequest, "BAD_REQUEST")
	if !strings.Contains(body.Message, "圆角") {
		t.Fatalf("应透传领域层的提示：%s", body.Message)
	}
}

// ---------- SEO 与统计代码 ----------

// fakeSEO 记录收到的入参。
type fakeSEO struct {
	mu        sync.Mutex
	seo       dom.SEO
	analytics dom.Analytics
	saved     []string
	err       error
}

func (f *fakeSEO) Get(context.Context) (dom.SEOConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return dom.SEOConfig{SEO: f.seo}, nil
}

func (f *fakeSEO) Set(_ context.Context, in dom.SEO) (dom.SEOConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, "seo")
	if f.err != nil {
		return dom.SEOConfig{}, f.err
	}
	f.seo = in
	return dom.SEOConfig{SEO: f.seo}, nil
}

func (f *fakeSEO) Analytics(context.Context) (dom.Analytics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.analytics, nil
}

func (f *fakeSEO) SetAnalytics(_ context.Context, in dom.Analytics) (dom.Analytics, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, "analytics")
	if f.err != nil {
		return dom.Analytics{}, f.err
	}
	f.analytics = in
	return f.analytics, nil
}

// seoEnv 在 newEnv 的基础上加上 SEO 服务与三种身份。
type seoEnv struct {
	*env
	seo *fakeSEO
}

func newSEOEnv() *seoEnv {
	e := &env{branding: &fakeBranding{}, audit: &memRecorder{}, ids: map[string]adm.Identity{
		"viewer": who("viewer", "seo.view"),
		"editor": who("editor", "seo.view", "seo.edit"),
		// 统计代码另要 seo.analytics_code（默认只有超管）
		"coder":  who("coder", "seo.view", "seo.edit", "seo.analytics_code"),
		"nobody": who("nobody"),
	}}
	seo := &fakeSEO{}
	h := &admin.Handlers{Auth: fakeAuth{ids: e.ids}, Cookie: admin.NewSessionCookie(false)}
	e.r = gin.New()
	e.r.Use(middleware.RequestID())
	authed := h.Routes(e.r, []string{origin}, e.audit)
	(&Handlers{Branding: e.branding, SEO: seo}).Routes(authed)
	return &seoEnv{env: e, seo: seo}
}

const seoBody = `{"title_templates":{"home":"{site_name} - {slogan}"},"desc_templates":{"drama":"{summary}"},` +
	`"og_image":"img/og.webp","robots_allow":true,"robots_extra":"Disallow: /admin","sitemap":true,"structured_data":true,` +
	`"verifications":[{"name":"google-site-verification","content":"abc123"}]}`

const analyticsBody = `{"head_snippet":"<script>a=1</script>","body_snippet":"<noscript>x</noscript>"}`

func TestSEOPermissions(t *testing.T) {
	e := newSEOEnv()
	expect(t, e.do("", http.MethodGet, "/api/admin/seo", ""), http.StatusUnauthorized, "NEED_LOGIN")
	expect(t, e.do("nobody", http.MethodGet, "/api/admin/seo", ""), http.StatusForbidden, "FORBIDDEN")
	expect(t, e.do("viewer", http.MethodGet, "/api/admin/seo", ""), http.StatusOK, "OK")
	expect(t, e.do("viewer", http.MethodPut, "/api/admin/seo", seoBody), http.StatusForbidden, "FORBIDDEN")
	expect(t, e.do("editor", http.MethodPut, "/api/admin/seo", seoBody), http.StatusOK, "OK")
}

// TestAnalyticsCodeNeedsOwnPermission 守住「统计代码要单独权限，读也要」：
// 这段代码会被原样注入页面，等于任意脚本注入，有 seo.edit 还不够。
func TestAnalyticsCodeNeedsOwnPermission(t *testing.T) {
	e := newSEOEnv()
	expect(t, e.do("editor", http.MethodGet, "/api/admin/analytics-code", ""), http.StatusForbidden, "FORBIDDEN")
	expect(t, e.do("editor", http.MethodPut, "/api/admin/analytics-code", analyticsBody), http.StatusForbidden, "FORBIDDEN")
	expect(t, e.do("coder", http.MethodGet, "/api/admin/analytics-code", ""), http.StatusOK, "OK")

	body := expect(t, e.do("coder", http.MethodPut, "/api/admin/analytics-code", analyticsBody), http.StatusOK, "OK")
	var got dom.Analytics
	if err := json.Unmarshal(body.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.HeadSnippet != "<script>a=1</script>" {
		t.Fatalf("统计代码原样保存：%q", got.HeadSnippet)
	}
	// 审计记下前后全文：出问题时要能查到是谁放了什么进去
	if a := e.audit.last(t); a.Action != "site_config.analytics" || a.TargetID != "analytics" || a.After == nil {
		t.Fatalf("统计代码审计：%+v", a)
	}

	// SEO 的响应里不带统计代码（没有权限的人连内容都不该读到）
	raw := expect(t, e.do("viewer", http.MethodGet, "/api/admin/seo", ""), http.StatusOK, "OK")
	if strings.Contains(string(raw.Data), "head_snippet") {
		t.Fatalf("GET /seo 不该返回统计代码：%s", raw.Data)
	}
}

func TestSEOSaveAndAudit(t *testing.T) {
	e := newSEOEnv()
	body := expect(t, e.do("editor", http.MethodPut, "/api/admin/seo", seoBody), http.StatusOK, "OK")
	var cfg dom.SEOConfig
	if err := json.Unmarshal(body.Data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SEO.TitleTemplates["home"] != "{site_name} - {slogan}" || len(cfg.SEO.Verifications) != 1 {
		t.Fatalf("请求体透传：%+v", cfg.SEO)
	}
	if a := e.audit.last(t); a.Action != "site_config.seo" || a.TargetID != "seo" || a.After == nil {
		t.Fatalf("SEO 审计：%+v", a)
	}
	// 未知字段 400
	expect(t, e.do("editor", http.MethodPut, "/api/admin/seo", `{"robots_allow":true,"bogus":1}`), http.StatusBadRequest, "BAD_REQUEST")
	// 领域层校验错误透传
	e.seo.err = errs.ErrBadRequest.WithMessage("「首页」的标题模板里有未知变量 {x}")
	msg := expect(t, e.do("editor", http.MethodPut, "/api/admin/seo", seoBody), http.StatusBadRequest, "BAD_REQUEST")
	if !strings.Contains(msg.Message, "未知变量") {
		t.Fatalf("应透传领域层的提示：%s", msg.Message)
	}
}
