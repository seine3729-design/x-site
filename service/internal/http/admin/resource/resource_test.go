package resource

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
	dom "xsite/service/internal/domain/resource"
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

var now = time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)

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
	role := adm.RoleRef{ID: 2, Code: "ops", Name: "运维", Active: true}
	if name == "root" {
		role = adm.RoleRef{ID: 1, Code: "superadmin", Name: "超级管理员", IsSuper: true, Active: true}
	}
	id := adm.NewIdentity(int64(len(name)), name, role, perms)
	id.CSRFToken = "csrf-" + name
	id.MFAEnabled = true
	return id
}

// fakeSvc 记录收到的参数，返回固定结果。
type fakeSvc struct {
	mu  sync.Mutex
	got []any
	err error
}

func (f *fakeSvc) record(v any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, v)
}

func (f *fakeSvc) last() any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.got) == 0 {
		return nil
	}
	return f.got[len(f.got)-1]
}

func sample(id int64) dom.FTPServer {
	return dom.FTPServer{
		ID: id, Name: "香港资源机", Host: "ftp.example.com", Port: 21, TLS: dom.TLSExplicit, Passive: true,
		CredRef: "HK1", Status: dom.StatusActive, CreatedAt: now, UpdatedAt: now,
	}
}

func (f *fakeSvc) ListFTPServers(_ context.Context, filter dom.Filter) (dom.Page[dom.FTPServer], error) {
	f.record(filter)
	return dom.Page[dom.FTPServer]{List: []dom.FTPServer{sample(3)}, Total: 1, Page: 1, PageSize: 20}, f.err
}

func (f *fakeSvc) CreateFTPServer(_ context.Context, in dom.FTPServerInput) (dom.FTPServer, error) {
	f.record(in)
	s := sample(5)
	s.Name = in.Name
	return s, f.err
}

func (f *fakeSvc) UpdateFTPServer(_ context.Context, id int64, in dom.FTPServerInput) (dom.FTPServer, dom.FTPServer, error) {
	f.record(in)
	after := sample(id)
	after.Name = in.Name
	return sample(id), after, f.err
}

func (f *fakeSvc) DeleteFTPServer(_ context.Context, id int64) (dom.FTPServer, error) {
	f.record(id)
	return sample(id), f.err
}

func probeSample() dom.ProbeResult {
	at := now
	return dom.ProbeResult{
		OK: false, ElapsedMs: 120, Path: "/剧集",
		Steps: []dom.Step{
			{Name: dom.StepResolve, OK: true, Ms: 2, Message: "解析到 203.0.113.9"},
			{Name: dom.StepConnect, OK: true, Ms: 40, Message: "已连接 203.0.113.9:21，TLS 1.3"},
			{Name: dom.StepLogin, OK: false, Ms: 30, Message: "登录失败：530 Login incorrect."},
		},
		Entries: []dom.Entry{{Name: "霸道总裁", Type: dom.EntryDir, ModifiedAt: &at}},
	}
}

func (f *fakeSvc) TestFTPServer(_ context.Context, id int64) (dom.ProbeResult, error) {
	f.record(id)
	return probeSample(), f.err
}

func (f *fakeSvc) ListFTPDir(_ context.Context, id int64, path string) (dom.ProbeResult, error) {
	f.record(path)
	return probeSample(), f.err
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

func (m *memRecorder) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
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

type env struct {
	r     *gin.Engine
	svc   *fakeSvc
	audit *memRecorder
	ids   map[string]adm.Identity
}

// newEnv 与线上一样：admin.Routes 提供会话、CSRF、审计，资源机接口注册在它返回的分组上。
func newEnv() *env {
	e := &env{svc: &fakeSvc{}, audit: &memRecorder{}, ids: map[string]adm.Identity{
		"root":   who("root"),
		"viewer": who("viewer", "ftp.view"),
		"editor": who("editor", "ftp.view", "ftp.edit"),
		"other":  who("other", "dramas.view", "dramas.edit"),
	}}
	h := &admin.Handlers{Auth: fakeAuth{ids: e.ids}, Cookie: admin.NewSessionCookie(false)}
	e.r = gin.New()
	e.r.Use(middleware.RequestID())
	authed := h.Routes(e.r, []string{origin}, e.audit)
	(&Handlers{Svc: e.svc}).Routes(authed)
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

const validBody = `{"name":"香港资源机","host":"ftp.example.com"}`

func TestPermissions(t *testing.T) {
	e := newEnv()
	tests := []struct {
		as, method, path, body string
		want                   int
	}{
		{"viewer", "GET", "/api/admin/ftp", "", 200},
		{"other", "GET", "/api/admin/ftp", "", 403},
		{"", "GET", "/api/admin/ftp", "", 401},
		// 增删改要 ftp.edit
		{"viewer", "POST", "/api/admin/ftp", validBody, 403},
		{"viewer", "PATCH", "/api/admin/ftp/3", validBody, 403},
		{"viewer", "DELETE", "/api/admin/ftp/3", "", 403},
		{"editor", "POST", "/api/admin/ftp", validBody, 200},
		{"editor", "PATCH", "/api/admin/ftp/3", validBody, 200},
		{"editor", "DELETE", "/api/admin/ftp/3", "", 200},
		{"root", "POST", "/api/admin/ftp", validBody, 200},
		// 测连与列目录只要 ftp.view
		{"viewer", "POST", "/api/admin/ftp/3/test", "", 200},
		{"viewer", "POST", "/api/admin/ftp/3/list", `{"path":"/剧集"}`, 200},
		{"other", "POST", "/api/admin/ftp/3/test", "", 403},
		{"other", "POST", "/api/admin/ftp/3/list", "", 403},
		{"", "POST", "/api/admin/ftp/3/test", "", 401},
	}
	for _, tt := range tests {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}

	// 测连会让 worker 对外发起连接，同样要 CSRF 令牌，防止跨站触发
	req := httptest.NewRequest("POST", "/api/admin/ftp/3/test", nil)
	req.Header.Set("User-Agent", testUA)
	req.AddCookie(&http.Cookie{Name: "xs_admin", Value: "viewer"})
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 令牌应 403，得到 %d", w.Code)
	}
}

func TestDeclaredPermissions(t *testing.T) {
	newEnv()
	got := admin.DeclaredPermissions()
	for _, p := range []string{"ftp.view", "ftp.edit"} {
		if !slices.Contains(got, p) {
			t.Errorf("资源机接口声明的权限点应登记：缺少 %s", p)
		}
	}
}

func TestListFTPServers(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("viewer", "GET", "/api/admin/ftp?q=香港&status=active&page=2&page_size=10", ""), 200, "OK")
	if f := e.svc.last().(dom.Filter); f.Q != "香港" || f.Status != dom.StatusActive || f.Page != 2 || f.PageSize != 10 {
		t.Fatalf("查询参数应原样交给服务：%+v", f)
	}
	var page struct {
		List     []map[string]any `json:"list"`
		Total    int              `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	if err := json.Unmarshal(b.Data, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.List) != 1 || page.Total != 1 || page.Page != 1 || page.PageSize != 20 {
		t.Fatalf("应返回一页：%s", b.Data)
	}
	item := page.List[0]
	credEnv, _ := item["cred_env"].(map[string]any)
	if item["cred_ref"] != "HK1" || credEnv["user"] != "FTP_HK1_USER" || credEnv["password"] != "FTP_HK1_PASSWORD" ||
		item["tls"] != "explicit" || item["passive"] != true || item["created_at"] != "2026-10-22T08:00:00Z" {
		t.Fatalf("字段不符合契约：%v", item)
	}
	for _, k := range []string{"id", "name", "host", "port", "tls", "passive", "cred_ref", "cred_env", "status", "created_at", "updated_at"} {
		if _, ok := item[k]; !ok {
			t.Errorf("缺少字段 %s", k)
		}
	}
	expect(t, e.do("viewer", "GET", "/api/admin/ftp?page=x", ""), 400, "BAD_REQUEST")
}

func TestCreateUpdateDelete(t *testing.T) {
	e := newEnv()

	// passive 未传时为 true；其余字段原样交给服务
	b := expect(t, e.do("editor", "POST", "/api/admin/ftp", `{"name":"香港资源机","host":"ftp.example.com","port":990,"tls":"implicit","cred_ref":"hk1","status":"inactive"}`), 200, "OK")
	in := e.svc.last().(dom.FTPServerInput)
	if in != (dom.FTPServerInput{Name: "香港资源机", Host: "ftp.example.com", Port: 990, TLS: dom.TLSImplicit, Passive: true, CredRef: "hk1", Status: dom.StatusInactive}) {
		t.Fatalf("请求体应转换为输入：%+v", in)
	}
	if !strings.Contains(string(b.Data), `"id":5`) {
		t.Fatalf("应返回保存后的记录：%s", b.Data)
	}
	a := e.audit.last(t)
	if a.Action != "ftp_server.create" || a.TargetType != "ftp_server" || a.TargetID != "5" ||
		!strings.Contains(string(a.After), `"cred_ref":"HK1"`) || strings.Contains(string(a.After), "FTP_HK1_PASSWORD") {
		t.Fatalf("新建应记审计（变量名中的 password 字段被脱敏）：%+v %s", a, a.After)
	}

	expect(t, e.do("editor", "POST", "/api/admin/ftp", `{"name":"x","host":"h","passive":false}`), 200, "OK")
	if in := e.svc.last().(dom.FTPServerInput); in.Passive {
		t.Fatal("显式传 passive=false 时应交给服务校验")
	}
	// 未知字段（比如想把密码直接传进来）拒绝
	expect(t, e.do("editor", "POST", "/api/admin/ftp", `{"name":"x","host":"h","password":"p"}`), 400, "BAD_REQUEST")
	expect(t, e.do("editor", "POST", "/api/admin/ftp", `{"name":"x","host":"h","port":"21"}`), 400, "BAD_REQUEST")

	expect(t, e.do("editor", "PATCH", "/api/admin/ftp/3", `{"name":"新名字","host":"ftp.example.com"}`), 200, "OK")
	a = e.audit.last(t)
	if a.Action != "ftp_server.update" || a.TargetID != "3" || !strings.Contains(string(a.Before), "香港资源机") || !strings.Contains(string(a.After), "新名字") {
		t.Fatalf("保存应记修改前后：%+v %s %s", a, a.Before, a.After)
	}
	expect(t, e.do("editor", "PATCH", "/api/admin/ftp/abc", validBody), 404, "NOT_FOUND")

	expect(t, e.do("editor", "DELETE", "/api/admin/ftp/3", ""), 200, "OK")
	if a = e.audit.last(t); a.Action != "ftp_server.delete" || a.TargetID != "3" || !strings.Contains(string(a.Before), "ftp.example.com") {
		t.Fatalf("删除应记删除前的记录：%+v", a)
	}

	// 业务错误照常映射，失败的写请求不记审计
	n := e.audit.count()
	e.svc.err = errs.ErrNotFound
	expect(t, e.do("editor", "DELETE", "/api/admin/ftp/9", ""), 404, "NOT_FOUND")
	e.svc.err = errs.ErrBadRequest.WithMessage("当前环境只允许显式 TLS（FTPS）连接资源机，请把加密方式改为显式 TLS")
	b = expect(t, e.do("editor", "POST", "/api/admin/ftp", validBody), 400, "BAD_REQUEST")
	if !strings.Contains(b.Message, "显式 TLS") {
		t.Fatalf("应返回服务给出的提示：%q", b.Message)
	}
	if e.audit.count() != n {
		t.Fatal("失败的写请求不应记审计")
	}
}

func TestProbeEndpoints(t *testing.T) {
	e := newEnv()

	b := expect(t, e.do("viewer", "POST", "/api/admin/ftp/3/test", ""), 200, "OK")
	if id := e.svc.last().(int64); id != 3 {
		t.Fatalf("应测连 #3，得到 %d", id)
	}
	var res struct {
		OK        bool `json:"ok"`
		ElapsedMs int  `json:"elapsed_ms"`
		Steps     []struct {
			Name    string `json:"name"`
			OK      bool   `json:"ok"`
			Ms      int    `json:"ms"`
			Message string `json:"message"`
		} `json:"steps"`
		Path      string           `json:"path"`
		Entries   []map[string]any `json:"entries"`
		Truncated *bool            `json:"truncated"`
	}
	if err := json.Unmarshal(b.Data, &res); err != nil {
		t.Fatal(err)
	}
	if res.OK || res.ElapsedMs != 120 || len(res.Steps) != 3 || res.Steps[2].Name != "login" || res.Steps[2].OK ||
		res.Steps[2].Message != "登录失败：530 Login incorrect." || res.Path != "/剧集" || res.Truncated == nil ||
		len(res.Entries) != 1 || res.Entries[0]["type"] != "dir" || res.Entries[0]["modified_at"] != "2026-10-22T08:00:00Z" {
		t.Fatalf("测连失败也是 200，结果写在步骤里：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "ftp_server.test" || a.TargetID != "3" || string(a.After) != `{"ok":false,"path":"/剧集","step":"login"}` {
		t.Fatalf("测连应记审计（结果与失败的一步，不记目录内容）：%+v %s", a, a.After)
	}

	// 列目录：路径在请求体中，可以省略
	expect(t, e.do("viewer", "POST", "/api/admin/ftp/3/list", `{"path":"/剧集/2026"}`), 200, "OK")
	if p := e.svc.last().(string); p != "/剧集/2026" {
		t.Fatalf("路径应交给服务：%q", p)
	}
	expect(t, e.do("viewer", "POST", "/api/admin/ftp/3/list", ""), 200, "OK")
	if p := e.svc.last().(string); p != "" {
		t.Fatalf("省略请求体为登录目录：%q", p)
	}
	expect(t, e.do("viewer", "POST", "/api/admin/ftp/3/list", `{}`), 200, "OK")
	expect(t, e.do("viewer", "POST", "/api/admin/ftp/3/list", `{"path":1}`), 400, "BAD_REQUEST")
	expect(t, e.do("viewer", "POST", "/api/admin/ftp/3/list", `{"dir":"/"}`), 400, "BAD_REQUEST")
	if a := e.audit.last(t); a.Action != "ftp_server.list" {
		t.Fatalf("列目录应记审计：%+v", a)
	}

	// worker 没有返回结果：503
	e.svc.err = errs.ErrUnavailable.WithMessage("worker 没有在 25 秒内返回资源机的结果，请确认 worker 正在运行后重试")
	b = expect(t, e.do("viewer", "POST", "/api/admin/ftp/3/test", ""), 503, "UNAVAILABLE")
	if !strings.Contains(b.Message, "worker") {
		t.Fatalf("应提示检查 worker：%q", b.Message)
	}
	e.svc.err = errs.ErrNotFound
	expect(t, e.do("viewer", "POST", "/api/admin/ftp/9/list", ""), 404, "NOT_FOUND")
	expect(t, e.do("viewer", "POST", "/api/admin/ftp/0/test", ""), 404, "NOT_FOUND")
}
