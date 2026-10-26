package community

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	adm "xsite/service/internal/domain/admin"
	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
	binding.EnableDecoderDisallowUnknownFields = true // 与 NewRouter 一致
}

const origin = "http://localhost:5173"

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
	role := adm.RoleRef{ID: 2, Code: "moderator", Name: "审核员", Active: true}
	if name == "root" {
		role = adm.RoleRef{ID: 1, Code: "superadmin", Name: "超级管理员", IsSuper: true, Active: true}
	}
	id := adm.NewIdentity(int64(len(name)+1), name, role, perms)
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

// fakeBlockwords 记录调用并返回可控结果。
type fakeBlockwords struct {
	created    []string // "word|group|action"
	deleted    []int64
	lastFilter dom.BlockwordFilter
	createErr  error
	deleteErr  error
}

func (f *fakeBlockwords) List(_ context.Context, filter dom.BlockwordFilter) (dom.Page[dom.Blockword], error) {
	f.lastFilter = filter
	return dom.Page[dom.Blockword]{List: []dom.Blockword{
		{ID: 3, Word: "vpn", Group: "引流", Action: dom.ActionBlock},
		{ID: 2, Word: "敏感", Group: "", Action: dom.ActionMask},
	}, Total: 2, Page: 1, PageSize: 20}, nil
}
func (f *fakeBlockwords) Groups(context.Context) ([]string, error) {
	return []string{"引流", "辱骂"}, nil
}
func (f *fakeBlockwords) Create(_ context.Context, word, group, action string) (dom.Blockword, error) {
	if f.createErr != nil {
		return dom.Blockword{}, f.createErr
	}
	f.created = append(f.created, word+"|"+group+"|"+action)
	return dom.Blockword{ID: 9, Word: dom.NormalizeWord(word), Group: group, Action: dom.Action(action)}, nil
}
func (f *fakeBlockwords) Delete(_ context.Context, id int64) (dom.Blockword, error) {
	if f.deleteErr != nil {
		return dom.Blockword{}, f.deleteErr
	}
	f.deleted = append(f.deleted, id)
	return dom.Blockword{ID: id, Word: "被删除", Action: dom.ActionBlock}, nil
}

type env struct {
	r     *gin.Engine
	bw    *fakeBlockwords
	rep   *fakeReports
	mod   *fakeModeration
	pol   *fakePolicy
	em    *fakeEmojis
	usr   *fakeUsers
	audit *memRecorder
	ids   map[string]adm.Identity
}

func newEnv() *env {
	e := &env{bw: &fakeBlockwords{}, rep: &fakeReports{}, mod: &fakeModeration{}, pol: &fakePolicy{}, em: &fakeEmojis{}, usr: newFakeUsers(), audit: &memRecorder{}, ids: map[string]adm.Identity{
		"root":   who("root"),
		"viewer": who("viewer", "blockwords.view", "emojis.view", "reports.view", "moderation.view", "users.view"),
		"editor": who("editor", "blockwords.view", "blockwords.edit", "emojis.view", "emojis.edit", "reports.view", "reports.edit", "moderation.view", "moderation.edit", "users.view", "users.edit"),
		"nobody": who("nobody"),
	}}
	h := &admin.Handlers{Auth: fakeAuth{ids: e.ids}, Cookie: admin.NewSessionCookie(false)}
	e.r = gin.New()
	e.r.Use(middleware.RequestID())
	authed := h.Routes(e.r, []string{origin}, e.audit)
	(&Handlers{Blockwords: e.bw, Reports: e.rep, Moderation: e.mod, Policy: e.pol, Emojis: e.em, Users: e.usr}).Routes(authed)
	return e
}

func (e *env) do(as, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("User-Agent", "Test")
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
	Code string          `json:"code"`
	Data json.RawMessage `json:"data"`
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

func TestBlockwordPermissions(t *testing.T) {
	e := newEnv()
	for _, tt := range []struct {
		as, method, path, body string
		want                   int
	}{
		{"viewer", "GET", "/api/admin/blockwords", "", 200},
		{"viewer", "GET", "/api/admin/blockwords/groups", "", 200},
		{"viewer", "POST", "/api/admin/blockwords", `{"word":"x","group":"g","action":"block"}`, 403},
		{"viewer", "DELETE", "/api/admin/blockwords/1", "", 403},
		{"editor", "POST", "/api/admin/blockwords", `{"word":"x","group":"g","action":"block"}`, 200},
		{"editor", "DELETE", "/api/admin/blockwords/1", "", 200},
		{"root", "POST", "/api/admin/blockwords", `{"word":"y","action":"mask"}`, 200},
		{"nobody", "GET", "/api/admin/blockwords", "", 403},
		{"", "GET", "/api/admin/blockwords", "", 401},
	} {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}
	got := admin.DeclaredPermissions()
	for _, p := range []string{"blockwords.view", "blockwords.edit"} {
		if !slices.Contains(got, p) {
			t.Errorf("应声明权限点 %s", p)
		}
	}
}

func TestListBlockwords(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("viewer", "GET", "/api/admin/blockwords?q=vp&group=%E5%BC%95%E6%B5%81&action=block&page=2&page_size=10", ""), 200, "OK")
	f := e.bw.lastFilter
	if f.Q != "vp" || f.Group != "引流" || f.Action != "block" || f.Page != 2 || f.PageSize != 10 {
		t.Fatalf("查询参数应交给服务：%+v", f)
	}
	if !strings.Contains(string(b.Data), `"word":"vpn"`) || !strings.Contains(string(b.Data), `"list"`) {
		t.Fatalf("列表数据不对：%s", b.Data)
	}
	// groups 端点
	g := expect(t, e.do("viewer", "GET", "/api/admin/blockwords/groups", ""), 200, "OK")
	if !strings.Contains(string(g.Data), "引流") {
		t.Fatalf("分组列表不对：%s", g.Data)
	}
}

func TestBlockwordWrites(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("editor", "POST", "/api/admin/blockwords", `{"word":"  ＶＰＮ ","group":"引流","action":"block"}`), 200, "OK")
	if len(e.bw.created) != 1 || e.bw.created[0] != "  ＶＰＮ |引流|block" {
		t.Fatalf("请求体原样交给服务：%v", e.bw.created)
	}
	if !strings.Contains(string(b.Data), `"word":"vpn"`) {
		t.Fatalf("应返回规范化后的词：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "blockword.create" || a.TargetType != "blockword" || a.TargetID != "9" {
		t.Fatalf("新建审计：%+v", a)
	}
	// 未知字段 400
	expect(t, e.do("editor", "POST", "/api/admin/blockwords", `{"word":"x","action":"block","extra":1}`), 400, "BAD_REQUEST")

	expect(t, e.do("editor", "DELETE", "/api/admin/blockwords/5", ""), 200, "OK")
	if len(e.bw.deleted) != 1 || e.bw.deleted[0] != 5 {
		t.Fatalf("删除 id 应交给服务：%v", e.bw.deleted)
	}
	if a := e.audit.last(t); a.Action != "blockword.delete" || a.TargetID != "5" {
		t.Fatalf("删除审计：%+v", a)
	}
	expect(t, e.do("editor", "DELETE", "/api/admin/blockwords/abc", ""), 404, "NOT_FOUND")
}

func TestBlockwordConflict(t *testing.T) {
	e := newEnv()
	e.bw.createErr = errs.ErrConflict.WithMessage("该屏蔽词已存在（改处理办法请先删后加）")
	expect(t, e.do("editor", "POST", "/api/admin/blockwords", `{"word":"vpn","action":"block"}`), 409, "CONFLICT")
}
