package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	adm "xsite/service/internal/domain/admin"
	"xsite/service/internal/domain/platform"
	dom "xsite/service/internal/domain/site"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
	binding.EnableDecoderDisallowUnknownFields = true // 与 NewRouter 一致
}

const (
	origin = "http://localhost:5173"
	testUA = "Mozilla/5.0 Test"
)

var now = time.Date(2026, 10, 25, 8, 0, 0, 0, time.UTC)

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
	role := adm.RoleRef{ID: 2, Code: "ops", Name: "运营", Active: true}
	id := adm.NewIdentity(int64(len(name)), name, role, perms)
	id.CSRFToken = "csrf-" + name
	id.MFAEnabled = true
	return id
}

type memRecorder struct {
	mu      sync.Mutex
	entries []platform.AuditEntry
}

func (m *memRecorder) Record(e platform.AuditEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
}

func (m *memRecorder) last(t *testing.T) platform.AuditEntry {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) == 0 {
		t.Fatal("没有审计记录")
	}
	return m.entries[len(m.entries)-1]
}

// fakePages 记录收到的参数，并可指定返回的错误。
type fakePages struct {
	mu     sync.Mutex
	filter dom.PageFilter
	input  dom.PageInput
	id     int64
	err    error
}

func (f *fakePages) List(_ context.Context, filter dom.PageFilter) (dom.Page[dom.StaticPage], error) {
	f.mu.Lock()
	f.filter = filter
	f.mu.Unlock()
	return dom.Page[dom.StaticPage]{List: []dom.StaticPage{
		{ID: 2, Title: "用户协议", Slug: "terms", Body: "<p>正文</p>", BodyFormat: dom.BodyRich, Status: dom.PagePublished,
			IsFooter: true, CreatedAt: now, UpdatedAt: now},
	}, Total: 1, Page: 1, PageSize: 20}, f.err
}

func (f *fakePages) Create(_ context.Context, in dom.PageInput) (dom.StaticPage, error) {
	f.mu.Lock()
	f.input = in
	f.mu.Unlock()
	return dom.StaticPage{ID: 9, Title: in.Title, Slug: in.Slug, Body: in.Body, BodyFormat: in.BodyFormat, Status: in.Status,
		IsFooter: in.IsFooter, CreatedAt: now, UpdatedAt: now}, f.err
}

func (f *fakePages) Update(_ context.Context, id int64, in dom.PageInput) (dom.StaticPage, dom.StaticPage, error) {
	f.mu.Lock()
	f.input, f.id = in, id
	f.mu.Unlock()
	before := dom.StaticPage{ID: id, Title: "旧标题", Slug: "old", Body: "<p>旧</p>", Status: dom.PageDraft}
	return before, dom.StaticPage{ID: id, Title: in.Title, Slug: in.Slug, Body: in.Body, Status: in.Status}, f.err
}

func (f *fakePages) Delete(_ context.Context, id int64) (dom.StaticPage, error) {
	f.mu.Lock()
	f.id = id
	f.mu.Unlock()
	return dom.StaticPage{ID: id, Title: "被删除", Slug: "gone"}, f.err
}

type env struct {
	r         *gin.Engine
	pages     *fakePages
	menus     *fakeMenus
	addresses *fakeAddresses
	blocks    *fakeBlocks
	audit     *memRecorder
	ids       map[string]adm.Identity
}

func newEnv() *env {
	e := &env{pages: &fakePages{}, menus: &fakeMenus{}, addresses: &fakeAddresses{}, blocks: &fakeBlocks{},
		audit: &memRecorder{}, ids: map[string]adm.Identity{
			"viewer":  who("viewer", "pages.view"),
			"editor":  who("editor", "pages.view", "pages.edit"),
			"mviewer": who("mviewer", "menus.view"),
			"meditor": who("meditor", "menus.view", "menus.edit"),
			"bviewer": who("bviewer", "blocks.view"),
			"beditor": who("beditor", "blocks.view", "blocks.edit"),
			"bhtml":   who("bhtml", "blocks.view", "blocks.edit", permBlockCustomHTML),
			"nobody":  who("nobody"),
		}}
	h := &admin.Handlers{Auth: fakeAuth{ids: e.ids}, Cookie: admin.NewSessionCookie(false)}
	e.r = gin.New()
	e.r.Use(middleware.RequestID())
	authed := h.Routes(e.r, []string{origin}, e.audit)
	(&Handlers{Pages: e.pages, Menus: e.menus, Addresses: e.addresses, Blocks: e.blocks}).Routes(authed)
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

const pageBody = `{"title":"用户协议","slug":"terms","body":"<p>正文</p>","body_format":"rich","status":"published","seo_title":"协议","seo_desc":"简介","is_footer":true}`

func TestPagePermissions(t *testing.T) {
	e := newEnv()
	// 未登录
	expect(t, e.do("", http.MethodGet, "/api/admin/pages", ""), http.StatusUnauthorized, "NEED_LOGIN")
	// 没有 pages.view
	expect(t, e.do("nobody", http.MethodGet, "/api/admin/pages", ""), http.StatusForbidden, "FORBIDDEN")
	// 只有查看权限：读可以，写一律 403
	expect(t, e.do("viewer", http.MethodGet, "/api/admin/pages", ""), http.StatusOK, "OK")
	for _, c := range []struct {
		method, path, body string
	}{
		{http.MethodPost, "/api/admin/pages", pageBody},
		{http.MethodPatch, "/api/admin/pages/2", pageBody},
		{http.MethodDelete, "/api/admin/pages/2", ""},
	} {
		expect(t, e.do("viewer", c.method, c.path, c.body), http.StatusForbidden, "FORBIDDEN")
	}
	// 有修改权限
	expect(t, e.do("editor", http.MethodPost, "/api/admin/pages", pageBody), http.StatusOK, "OK")
}

func TestPageCSRFAndOrigin(t *testing.T) {
	e := newEnv()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/pages", strings.NewReader(pageBody))
	req.Header.Set("User-Agent", testUA)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.AddCookie(&http.Cookie{Name: "xs_admin", Value: "editor"})
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req) // 缺 CSRF 令牌
	expect(t, w, http.StatusForbidden, "FORBIDDEN")

	req = httptest.NewRequest(http.MethodPost, "/api/admin/pages", strings.NewReader(pageBody))
	req.Header.Set("User-Agent", testUA)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example.com")
	req.Header.Set(admin.HeaderCSRFToken, e.ids["editor"].CSRFToken)
	req.AddCookie(&http.Cookie{Name: "xs_admin", Value: "editor"})
	w = httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	expect(t, w, http.StatusForbidden, "FORBIDDEN")
}

func TestPageListQuery(t *testing.T) {
	e := newEnv()
	body := expect(t, e.do("viewer", http.MethodGet, "/api/admin/pages?q=%E5%8D%8F%E8%AE%AE&status=published&page=2&page_size=10", ""), http.StatusOK, "OK")
	var got pageDTO[staticPageDTO]
	if err := json.Unmarshal(body.Data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.List) != 1 || got.List[0].Slug != "terms" || !got.List[0].IsFooter || got.Total != 1 {
		t.Fatalf("列表字段：%+v", got)
	}
	e.pages.mu.Lock()
	f := e.pages.filter
	e.pages.mu.Unlock()
	if f.Q != "协议" || f.Status != dom.PagePublished || f.Page != 2 || f.PageSize != 10 {
		t.Fatalf("筛选参数透传：%+v", f)
	}
	// 页码不是数字
	expect(t, e.do("viewer", http.MethodGet, "/api/admin/pages?page=abc", ""), http.StatusBadRequest, "BAD_REQUEST")
}

func TestPageWriteAndAudit(t *testing.T) {
	e := newEnv()
	body := expect(t, e.do("editor", http.MethodPost, "/api/admin/pages", pageBody), http.StatusOK, "OK")
	var created staticPageDTO
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatal(err)
	}
	if created.ID != 9 || created.Slug != "terms" || created.BodyFormat != "rich" || created.Status != "published" {
		t.Fatalf("新建返回保存结果：%+v", created)
	}
	if a := e.audit.last(t); a.Action != "page.create" || a.TargetType != "page" || a.TargetID != "9" || a.After == nil {
		t.Fatalf("新建审计：%+v", a)
	}
	e.pages.mu.Lock()
	in := e.pages.input
	e.pages.mu.Unlock()
	if in.Title != "用户协议" || in.Body != "<p>正文</p>" || in.SEODesc != "简介" || !in.IsFooter {
		t.Fatalf("请求体透传：%+v", in)
	}

	// 整体保存：审计带修改前后
	expect(t, e.do("editor", http.MethodPatch, "/api/admin/pages/2", pageBody), http.StatusOK, "OK")
	if a := e.audit.last(t); a.Action != "page.update" || a.TargetID != "2" || a.Before == nil || a.After == nil {
		t.Fatalf("保存审计：%+v", a)
	}

	// 删除：只返回编号，审计带删除前的记录
	body = expect(t, e.do("editor", http.MethodDelete, "/api/admin/pages/2", ""), http.StatusOK, "OK")
	if !strings.Contains(string(body.Data), `"id":2`) {
		t.Fatalf("删除返回编号：%s", body.Data)
	}
	if a := e.audit.last(t); a.Action != "page.delete" || a.TargetID != "2" || a.Before == nil {
		t.Fatalf("删除审计：%+v", a)
	}

	// 未知字段 400；路径编号不是数字按 404（与其他后台接口一致）
	expect(t, e.do("editor", http.MethodPost, "/api/admin/pages", `{"title":"x","slug":"y","body":"z","unknown":1}`), http.StatusBadRequest, "BAD_REQUEST")
	expect(t, e.do("editor", http.MethodPatch, "/api/admin/pages/abc", pageBody), http.StatusNotFound, "NOT_FOUND")
}

func TestPageDomainErrorPassthrough(t *testing.T) {
	e := newEnv()
	e.pages.err = errs.ErrConflict.WithMessage("这个 slug 已经被用了，请换一个")
	body := expect(t, e.do("editor", http.MethodPost, "/api/admin/pages", pageBody), http.StatusConflict, "CONFLICT")
	if !strings.Contains(body.Message, "slug") {
		t.Fatalf("应透传领域层的提示：%s", body.Message)
	}
}

// ---------- 前台菜单与防丢地址 ----------

type fakeMenus struct {
	mu       sync.Mutex
	input    dom.MenuInput
	id       int64
	pos      dom.MenuPosition
	ids      []int64
	err      error
	reordErr error
}

func (f *fakeMenus) List(context.Context) ([]dom.MenuView, error) {
	return []dom.MenuView{
		{Menu: dom.Menu{ID: 1, Position: dom.PositionTop, Label: "首页", LinkType: dom.LinkInternal, LinkValue: "home",
			VisibleTo: dom.VisibleAll, Device: dom.DeviceAll, Status: dom.MenuActive, Sort: 0, CreatedAt: now, UpdatedAt: now}},
		{Menu: dom.Menu{ID: 2, Position: dom.PositionFooter, Label: "条款", LinkType: dom.LinkPage, LinkValue: "terms",
			GroupTitle: "帮助与条款", Status: dom.MenuActive, Sort: 1, CreatedAt: now, UpdatedAt: now},
			TargetIssue: "静态页「terms」当前不对前台显示"},
	}, f.err
}

func (f *fakeMenus) Create(_ context.Context, in dom.MenuInput) (dom.Menu, error) {
	f.mu.Lock()
	f.input = in
	f.mu.Unlock()
	return dom.Menu{ID: 9, Position: in.Position, Label: in.Label, LinkType: in.LinkType, LinkValue: in.LinkValue,
		Status: in.Status, CreatedAt: now, UpdatedAt: now}, f.err
}

func (f *fakeMenus) Update(_ context.Context, id int64, in dom.MenuInput) (dom.Menu, dom.Menu, error) {
	f.mu.Lock()
	f.input, f.id = in, id
	f.mu.Unlock()
	before := dom.Menu{ID: id, Position: dom.PositionTop, Label: "旧名称", LinkType: dom.LinkInternal, LinkValue: "home"}
	return before, dom.Menu{ID: id, Position: in.Position, Label: in.Label, LinkType: in.LinkType, LinkValue: in.LinkValue}, f.err
}

func (f *fakeMenus) Delete(_ context.Context, id int64) (dom.Menu, error) {
	f.mu.Lock()
	f.id = id
	f.mu.Unlock()
	return dom.Menu{ID: id, Label: "被删除"}, f.err
}

func (f *fakeMenus) Reorder(_ context.Context, pos dom.MenuPosition, ids []int64) error {
	f.mu.Lock()
	f.pos, f.ids = pos, ids
	f.mu.Unlock()
	return f.reordErr
}

type fakeAddresses struct {
	mu      sync.Mutex
	in      dom.Addresses
	current dom.Addresses
	err     error
}

func (f *fakeAddresses) Get(context.Context) (dom.Addresses, error) { return f.current, f.err }

func (f *fakeAddresses) Set(_ context.Context, in dom.Addresses) (dom.Addresses, dom.Addresses, int64, error) {
	f.mu.Lock()
	f.in = in
	f.mu.Unlock()
	if f.err != nil {
		return dom.Addresses{}, dom.Addresses{}, 0, f.err
	}
	before := f.current
	f.current = in
	return before, in, 12, nil
}

const menuBody = `{"position":"footer","label":"用户协议","link_type":"page","link_value":"terms","group_title":"帮助与条款","icon":"file-text","tabbar_icon":"","badge":"","visible_to":"all","device":"all","new_tab":false,"status":"active"}`

const addressBody = `{"notice":"请收藏永久地址","latest_url":"https://new.example.com","permanent_url":"https://perm.example.com","backup_urls":["https://b1.example.com"],"publish_page":"https://pub.example.com","support_url":"https://t.me/support","telegram":"https://t.me/channel","socials":[{"name":"微博","url":"https://weibo.com/x"}]}`

func TestMenuPermissions(t *testing.T) {
	e := newEnv()
	expect(t, e.do("", http.MethodGet, "/api/admin/menus", ""), http.StatusUnauthorized, "NEED_LOGIN")
	expect(t, e.do("nobody", http.MethodGet, "/api/admin/menus", ""), http.StatusForbidden, "FORBIDDEN")
	// 只有静态页权限的角色读不了菜单
	expect(t, e.do("viewer", http.MethodGet, "/api/admin/menus", ""), http.StatusForbidden, "FORBIDDEN")
	expect(t, e.do("mviewer", http.MethodGet, "/api/admin/menus", ""), http.StatusOK, "OK")
	for _, c := range []struct {
		method, path, body string
	}{
		{http.MethodPost, "/api/admin/menus", menuBody},
		{http.MethodPatch, "/api/admin/menus/1", menuBody},
		{http.MethodDelete, "/api/admin/menus/1", ""},
		{http.MethodPost, "/api/admin/menus/reorder", `{"position":"top","ids":[1]}`},
		{http.MethodPut, "/api/admin/addresses", addressBody},
	} {
		expect(t, e.do("mviewer", c.method, c.path, c.body), http.StatusForbidden, "FORBIDDEN")
	}
	// 防丢地址与菜单同页：menus.view 能读，menus.edit 能写
	expect(t, e.do("mviewer", http.MethodGet, "/api/admin/addresses", ""), http.StatusOK, "OK")
	expect(t, e.do("meditor", http.MethodPost, "/api/admin/menus", menuBody), http.StatusOK, "OK")
	expect(t, e.do("meditor", http.MethodPut, "/api/admin/addresses", addressBody), http.StatusOK, "OK")
}

func TestMenuListAndWrite(t *testing.T) {
	e := newEnv()
	body := expect(t, e.do("mviewer", http.MethodGet, "/api/admin/menus", ""), http.StatusOK, "OK")
	var list struct {
		List []menuDTO `json:"list"`
	}
	if err := json.Unmarshal(body.Data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.List) != 2 || list.List[0].Label != "首页" || list.List[0].TargetIssue != "" {
		t.Fatalf("列表字段：%+v", list.List)
	}
	if !strings.Contains(list.List[1].TargetIssue, "不对前台显示") || list.List[1].GroupTitle != "帮助与条款" {
		t.Fatalf("目标不可用要带原因：%+v", list.List[1])
	}

	// 新建：请求体透传 + 审计
	body = expect(t, e.do("meditor", http.MethodPost, "/api/admin/menus", menuBody), http.StatusOK, "OK")
	var created menuDTO
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatal(err)
	}
	if created.ID != 9 || created.Position != "footer" || created.LinkType != "page" {
		t.Fatalf("新建返回保存结果：%+v", created)
	}
	e.menus.mu.Lock()
	in := e.menus.input
	e.menus.mu.Unlock()
	if in.Label != "用户协议" || in.LinkValue != "terms" || in.GroupTitle != "帮助与条款" || in.Icon != "file-text" {
		t.Fatalf("请求体透传：%+v", in)
	}
	if a := e.audit.last(t); a.Action != "menu.create" || a.TargetType != "menu" || a.TargetID != "9" {
		t.Fatalf("新建审计：%+v", a)
	}

	expect(t, e.do("meditor", http.MethodPatch, "/api/admin/menus/3", menuBody), http.StatusOK, "OK")
	if a := e.audit.last(t); a.Action != "menu.update" || a.TargetID != "3" || a.Before == nil || a.After == nil {
		t.Fatalf("保存审计：%+v", a)
	}
	expect(t, e.do("meditor", http.MethodDelete, "/api/admin/menus/3", ""), http.StatusOK, "OK")
	if a := e.audit.last(t); a.Action != "menu.delete" || a.TargetID != "3" || a.Before == nil {
		t.Fatalf("删除审计：%+v", a)
	}
	// 未知字段 400
	expect(t, e.do("meditor", http.MethodPost, "/api/admin/menus", `{"position":"top","label":"x","link_type":"internal","link_value":"home","bogus":1}`),
		http.StatusBadRequest, "BAD_REQUEST")
}

func TestMenuReorder(t *testing.T) {
	e := newEnv()
	expect(t, e.do("meditor", http.MethodPost, "/api/admin/menus/reorder", `{"position":"top","ids":[3,1,2]}`), http.StatusOK, "OK")
	e.menus.mu.Lock()
	pos, ids := e.menus.pos, e.menus.ids
	e.menus.mu.Unlock()
	if pos != dom.PositionTop || len(ids) != 3 || ids[0] != 3 {
		t.Fatalf("顺序透传：%v %v", pos, ids)
	}
	if a := e.audit.last(t); a.Action != "menu.reorder" || a.TargetID != "top" {
		t.Fatalf("排序审计：%+v", a)
	}
	// 没传 ids（nil）与空数组要区分：nil 报错
	expect(t, e.do("meditor", http.MethodPost, "/api/admin/menus/reorder", `{"position":"top"}`), http.StatusBadRequest, "BAD_REQUEST")
	// 领域层的 409 透传
	e.menus.reordErr = errs.ErrConflict.WithMessage("菜单已被改动，请刷新后重新排序")
	expect(t, e.do("meditor", http.MethodPost, "/api/admin/menus/reorder", `{"position":"top","ids":[1]}`), http.StatusConflict, "CONFLICT")
}

func TestAddresses(t *testing.T) {
	e := newEnv()
	e.addresses.current = dom.Addresses{LatestURL: "https://old.example.com"}
	body := expect(t, e.do("mviewer", http.MethodGet, "/api/admin/addresses", ""), http.StatusOK, "OK")
	var got addressesDTO
	if err := json.Unmarshal(body.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.LatestURL != "https://old.example.com" || got.BackupURLs == nil || got.Socials == nil {
		t.Fatalf("空列表要输出 [] 而不是 null：%s", body.Data)
	}

	body = expect(t, e.do("meditor", http.MethodPut, "/api/admin/addresses", addressBody), http.StatusOK, "OK")
	var saved struct {
		Addresses   addressesDTO `json:"addresses"`
		SiteVersion int64        `json:"site_version"`
	}
	if err := json.Unmarshal(body.Data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.SiteVersion != 12 {
		t.Fatalf("保存后返回新的站点配置版本：%+v", saved)
	}
	if saved.Addresses.Notice != "请收藏永久地址" || len(saved.Addresses.BackupURLs) != 1 || saved.Addresses.Socials[0].Name != "微博" {
		t.Fatalf("保存结果：%+v", saved.Addresses)
	}
	if a := e.audit.last(t); a.Action != "addresses.update" || a.TargetType != "site_config" || a.TargetID != "addresses" || a.Before == nil {
		t.Fatalf("防丢地址审计：%+v", a)
	}
	// 领域层的校验错误透传
	e.addresses.err = errs.ErrBadRequest.WithMessage("最新地址要填 http:// 或 https:// 开头的完整地址")
	body = expect(t, e.do("meditor", http.MethodPut, "/api/admin/addresses", addressBody), http.StatusBadRequest, "BAD_REQUEST")
	if !strings.Contains(body.Message, "最新地址") {
		t.Fatalf("应透传领域层的提示：%s", body.Message)
	}
}

// ---------- 页面区块 ----------

type fakeBlocks struct {
	mu       sync.Mutex
	page     string
	input    dom.BlockInput
	id       int64
	ids      []int64
	customOK bool // 最近一次写入时 handler 判定的 blocks.custom_html 权限
	err      error
	reordErr error
}

func (f *fakeBlocks) Pages(context.Context) ([]dom.BlockPage, error) {
	return []dom.BlockPage{{Value: "home", Label: "首页"}, {Value: "channel:city", Label: "都市"}}, f.err
}

func (f *fakeBlocks) List(_ context.Context, page string) ([]dom.Block, error) {
	f.mu.Lock()
	f.page = page
	f.mu.Unlock()
	return []dom.Block{
		{ID: 1, Page: "home", Type: dom.BlockCarousel, Title: "热播", ItemLimit: 8, Status: dom.MenuActive, ColsDesktop: 6,
			Device: dom.DeviceAll, SourceType: dom.SourceManual, Source: dom.BlockSource{DramaIDs: []int64{3, 1}},
			Sort: 0, CreatedAt: now, UpdatedAt: now},
		{ID: 2, Page: "home", Type: dom.BlockTagCloud, Title: "标签", ItemLimit: 20, Status: dom.MenuInactive, ColsDesktop: 6,
			Device: dom.DeviceMobile, SourceType: dom.SourceAuto, Sort: 1, CreatedAt: now, UpdatedAt: now},
	}, f.err
}

// block 按输入造一条记录（与领域层的默认值无关，默认值在领域层测）。
func (f *fakeBlocks) block(id int64, in dom.BlockInput) dom.Block {
	return dom.Block{
		ID: id, Page: in.Page, Type: in.Type, Title: in.Title, Subtitle: in.Subtitle, ItemLimit: in.ItemLimit,
		Status: in.Status, MoreLink: in.MoreLink, ColsDesktop: in.ColsDesktop, Device: in.Device,
		SourceType: in.SourceType, Source: in.Source, CustomHTML: in.CustomHTML, CreatedAt: now, UpdatedAt: now,
	}
}

func (f *fakeBlocks) Create(_ context.Context, in dom.BlockInput, customHTML bool) (dom.Block, error) {
	f.mu.Lock()
	f.input, f.customOK = in, customHTML
	f.mu.Unlock()
	if in.Type == dom.BlockCustomHTML && !customHTML {
		return dom.Block{}, dom.ErrBlockCustomHTMLForbidden // 与领域层一致
	}
	return f.block(9, in), f.err
}

func (f *fakeBlocks) Update(_ context.Context, id int64, in dom.BlockInput, customHTML bool) (dom.Block, dom.Block, error) {
	f.mu.Lock()
	f.input, f.id, f.customOK = in, id, customHTML
	f.mu.Unlock()
	if in.Type == dom.BlockCustomHTML && !customHTML {
		return dom.Block{}, dom.Block{}, dom.ErrBlockCustomHTMLForbidden
	}
	before := dom.Block{ID: id, Page: "home", Type: dom.BlockTagCloud, Title: "旧标题"}
	return before, f.block(id, in), f.err
}

func (f *fakeBlocks) Delete(_ context.Context, id int64) (dom.Block, error) {
	f.mu.Lock()
	f.id = id
	f.mu.Unlock()
	return dom.Block{ID: id, Page: "home", Title: "被删除"}, f.err
}

func (f *fakeBlocks) Reorder(_ context.Context, page string, ids []int64) error {
	f.mu.Lock()
	f.page, f.ids = page, ids
	f.mu.Unlock()
	return f.reordErr
}

const blockBody = `{"page":"home","type":"carousel","title":"热播","subtitle":"每日更新","item_limit":8,"status":"active",` +
	`"more_link":"/explore","cols_desktop":4,"device":"mobile","source_type":"manual",` +
	`"source":{"drama_ids":[3,1],"topic_id":0,"term_codes":[],"tag_id":null,"sort":"","window":"",` +
	`"rank_board":"","ad_slot":"","announce_kind":"","topic_ids":[],"tag_ids":[]},"custom_html":""}`

const htmlBlockBody = `{"page":"home","type":"custom_html","title":"活动","custom_html":"<p>活动说明</p>"}`

func TestBlockPermissions(t *testing.T) {
	e := newEnv()
	expect(t, e.do("", http.MethodGet, "/api/admin/blocks", ""), http.StatusUnauthorized, "NEED_LOGIN")
	expect(t, e.do("nobody", http.MethodGet, "/api/admin/blocks", ""), http.StatusForbidden, "FORBIDDEN")
	// 菜单权限读不了区块
	expect(t, e.do("mviewer", http.MethodGet, "/api/admin/blocks", ""), http.StatusForbidden, "FORBIDDEN")
	expect(t, e.do("bviewer", http.MethodGet, "/api/admin/blocks", ""), http.StatusOK, "OK")
	expect(t, e.do("bviewer", http.MethodGet, "/api/admin/blocks/pages", ""), http.StatusOK, "OK")
	for _, c := range []struct {
		method, path, body string
	}{
		{http.MethodPost, "/api/admin/blocks", blockBody},
		{http.MethodPatch, "/api/admin/blocks/1", blockBody},
		{http.MethodDelete, "/api/admin/blocks/1", ""},
		{http.MethodPost, "/api/admin/blocks/reorder", `{"page":"home","ids":[1]}`},
	} {
		expect(t, e.do("bviewer", c.method, c.path, c.body), http.StatusForbidden, "FORBIDDEN")
	}
	expect(t, e.do("beditor", http.MethodPost, "/api/admin/blocks", blockBody), http.StatusOK, "OK")
}

func TestBlockListAndPages(t *testing.T) {
	e := newEnv()
	body := expect(t, e.do("bviewer", http.MethodGet, "/api/admin/blocks?page=channel:city", ""), http.StatusOK, "OK")
	var list struct {
		List []blockDTO `json:"list"`
	}
	if err := json.Unmarshal(body.Data, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.List) != 2 || list.List[0].Type != "carousel" || list.List[0].SourceType != "manual" {
		t.Fatalf("列表字段：%+v", list.List)
	}
	if len(list.List[0].Source.DramaIDs) != 2 || list.List[0].Source.DramaIDs[0] != 3 {
		t.Fatalf("来源参数下发：%+v", list.List[0].Source)
	}
	// 空列表输出 [] 而不是 null
	if list.List[1].Source.DramaIDs == nil || list.List[1].Source.TermCodes == nil || list.List[1].Status != "inactive" {
		t.Fatalf("空列表要输出 []：%s", body.Data)
	}
	e.blocks.mu.Lock()
	page := e.blocks.page
	e.blocks.mu.Unlock()
	if page != "channel:city" {
		t.Fatalf("page 参数透传：%q", page)
	}
	// page 不传按首页（具体兜底在领域层）
	expect(t, e.do("bviewer", http.MethodGet, "/api/admin/blocks", ""), http.StatusOK, "OK")
	e.blocks.mu.Lock()
	page = e.blocks.page
	e.blocks.mu.Unlock()
	if page != "" {
		t.Fatalf("page 不传时原样透传空串：%q", page)
	}

	body = expect(t, e.do("bviewer", http.MethodGet, "/api/admin/blocks/pages", ""), http.StatusOK, "OK")
	if !strings.Contains(string(body.Data), `"value":"home"`) || !strings.Contains(string(body.Data), `"label":"都市"`) {
		t.Fatalf("可编排页面：%s", body.Data)
	}
}

func TestBlockWriteAndAudit(t *testing.T) {
	e := newEnv()
	body := expect(t, e.do("beditor", http.MethodPost, "/api/admin/blocks", blockBody), http.StatusOK, "OK")
	var created blockDTO
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatal(err)
	}
	if created.ID != 9 || created.Type != "carousel" || created.ItemLimit != 8 || created.ColsDesktop != 4 || created.Device != "mobile" {
		t.Fatalf("新建返回保存结果：%+v", created)
	}
	e.blocks.mu.Lock()
	in, customOK := e.blocks.input, e.blocks.customOK
	e.blocks.mu.Unlock()
	if in.Title != "热播" || in.Subtitle != "每日更新" || in.MoreLink != "/explore" || in.SourceType != dom.SourceManual {
		t.Fatalf("请求体透传：%+v", in)
	}
	if len(in.Source.DramaIDs) != 2 || in.Source.DramaIDs[0] != 3 {
		t.Fatalf("来源参数透传：%+v", in.Source)
	}
	if customOK {
		t.Fatal("没有 blocks.custom_html 的管理员应判定为无权限")
	}
	if a := e.audit.last(t); a.Action != "block.create" || a.TargetType != "block" || a.TargetID != "9" || a.After == nil {
		t.Fatalf("新建审计：%+v", a)
	}

	// 整体保存：审计带修改前后
	expect(t, e.do("beditor", http.MethodPatch, "/api/admin/blocks/4", blockBody), http.StatusOK, "OK")
	if a := e.audit.last(t); a.Action != "block.update" || a.TargetID != "4" || a.Before == nil || a.After == nil {
		t.Fatalf("保存审计：%+v", a)
	}

	// 删除：只返回编号
	body = expect(t, e.do("beditor", http.MethodDelete, "/api/admin/blocks/4", ""), http.StatusOK, "OK")
	if !strings.Contains(string(body.Data), `"id":4`) {
		t.Fatalf("删除返回编号：%s", body.Data)
	}
	if a := e.audit.last(t); a.Action != "block.delete" || a.TargetID != "4" || a.Before == nil {
		t.Fatalf("删除审计：%+v", a)
	}

	// 未知字段 400；路径编号不是数字按 404
	expect(t, e.do("beditor", http.MethodPost, "/api/admin/blocks", `{"page":"home","type":"rank","bogus":1}`), http.StatusBadRequest, "BAD_REQUEST")
	expect(t, e.do("beditor", http.MethodPatch, "/api/admin/blocks/abc", blockBody), http.StatusNotFound, "NOT_FOUND")

	// 领域层的校验错误透传
	e.blocks.err = errs.ErrBadRequest.WithMessage("剧集 #99 不存在或在回收站")
	body = expect(t, e.do("beditor", http.MethodPost, "/api/admin/blocks", blockBody), http.StatusBadRequest, "BAD_REQUEST")
	if !strings.Contains(body.Message, "#99") {
		t.Fatalf("应透传领域层的提示：%s", body.Message)
	}
}

func TestBlockCustomHTMLPermission(t *testing.T) {
	e := newEnv()
	// blocks.edit 不够：自定义 HTML 区块另要 blocks.custom_html
	expect(t, e.do("beditor", http.MethodPost, "/api/admin/blocks", htmlBlockBody), http.StatusForbidden, "FORBIDDEN")
	expect(t, e.do("beditor", http.MethodPatch, "/api/admin/blocks/1", htmlBlockBody), http.StatusForbidden, "FORBIDDEN")

	body := expect(t, e.do("bhtml", http.MethodPost, "/api/admin/blocks", htmlBlockBody), http.StatusOK, "OK")
	var created blockDTO
	if err := json.Unmarshal(body.Data, &created); err != nil {
		t.Fatal(err)
	}
	if created.Type != "custom_html" || created.CustomHTML != "<p>活动说明</p>" {
		t.Fatalf("有权限时保存 HTML：%+v", created)
	}
	e.blocks.mu.Lock()
	customOK := e.blocks.customOK
	e.blocks.mu.Unlock()
	if !customOK {
		t.Fatal("有 blocks.custom_html 的管理员应判定为有权限")
	}
}

func TestBlockReorder(t *testing.T) {
	e := newEnv()
	expect(t, e.do("beditor", http.MethodPost, "/api/admin/blocks/reorder", `{"page":"channel:city","ids":[3,1,2]}`), http.StatusOK, "OK")
	e.blocks.mu.Lock()
	page, ids := e.blocks.page, e.blocks.ids
	e.blocks.mu.Unlock()
	if page != "channel:city" || len(ids) != 3 || ids[0] != 3 {
		t.Fatalf("顺序透传：%q %v", page, ids)
	}
	if a := e.audit.last(t); a.Action != "block.reorder" || a.TargetID != "channel:city" {
		t.Fatalf("排序审计：%+v", a)
	}
	// 没传 ids（nil）报错，空数组交给领域层判
	expect(t, e.do("beditor", http.MethodPost, "/api/admin/blocks/reorder", `{"page":"home"}`), http.StatusBadRequest, "BAD_REQUEST")
	// 领域层的 409 与 404 透传
	e.blocks.reordErr = errs.ErrConflict.WithMessage("区块已被改动，请刷新后重新排序")
	expect(t, e.do("beditor", http.MethodPost, "/api/admin/blocks/reorder", `{"page":"home","ids":[1]}`), http.StatusConflict, "CONFLICT")
	e.blocks.reordErr = errs.ErrNotFound
	expect(t, e.do("beditor", http.MethodPost, "/api/admin/blocks/reorder", `{"page":"channel:gone","ids":[1]}`), http.StatusNotFound, "NOT_FOUND")
}
