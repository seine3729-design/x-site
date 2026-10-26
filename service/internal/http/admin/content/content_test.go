package content

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
	dom "xsite/service/internal/domain/content"
	"xsite/service/internal/domain/platform"
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

var now = time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)

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
	if name == "root" {
		role = adm.RoleRef{ID: 1, Code: "superadmin", Name: "超级管理员", IsSuper: true, Active: true}
	}
	id := adm.NewIdentity(int64(len(name)), name, role, perms)
	id.CSRFToken = "csrf-" + name
	id.MFAEnabled = true
	return id
}

// fakeSvc 只实现测试用到的方法，并记录收到的参数；嵌入的 Service 为 nil，调用未实现的方法会 panic。
type fakeSvc struct {
	Service
	mu   sync.Mutex
	got  []any
	err  error
	page dom.Page[dom.Drama]
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

func (f *fakeSvc) ListDramas(_ context.Context, filter dom.DramaFilter) (dom.Page[dom.Drama], error) {
	f.record(filter)
	return f.page, f.err
}

func (f *fakeSvc) GetDrama(_ context.Context, id int64) (dom.DramaDetail, error) {
	return dom.DramaDetail{Drama: dom.Drama{ID: id, Title: "剧"}}, f.err
}

func (f *fakeSvc) CreateDrama(_ context.Context, in dom.DramaInput) (dom.Drama, error) {
	f.record(in)
	return dom.Drama{ID: 42, Title: in.Title, Status: dom.DramaDraft, CreatedAt: now, UpdatedAt: now}, f.err
}

func (f *fakeSvc) UpdateDrama(_ context.Context, id int64, in dom.DramaInput) (dom.Drama, dom.Drama, error) {
	f.record(in)
	return dom.Drama{ID: id, Title: "旧名", Status: dom.DramaDraft}, dom.Drama{ID: id, Title: in.Title, Status: in.Status, PublishAt: in.PublishAt}, f.err
}

func (f *fakeSvc) SetDramaTags(_ context.Context, _ int64, ids []int64) ([]dom.Tag, []dom.Tag, error) {
	f.record(ids)
	after := make([]dom.Tag, len(ids))
	for i, id := range ids {
		after[i] = dom.Tag{ID: id, Name: "标签", Status: dom.TagActive}
	}
	return []dom.Tag{{ID: 7, Name: "旧标签", Status: dom.TagActive}}, after, f.err
}

func (f *fakeSvc) BulkDramas(_ context.Context, in dom.BulkInput) (int64, error) {
	f.record(in)
	return 2, f.err
}

func (f *fakeSvc) CreateEpisode(_ context.Context, dramaID int64, in dom.EpisodeInput) (dom.Episode, error) {
	f.record(in)
	return dom.Episode{ID: 5, DramaID: dramaID, Number: in.Number, Access: dom.AccessFree, MediaStatus: "pending"}, f.err
}

func (f *fakeSvc) UpdateEpisode(_ context.Context, id int64, in dom.EpisodeInput) (dom.Episode, dom.Episode, error) {
	f.record(in)
	return dom.Episode{ID: id, Number: 1}, dom.Episode{ID: id, Number: in.Number}, f.err
}

func (f *fakeSvc) ListTags(_ context.Context, filter dom.TagFilter) (dom.Page[dom.Tag], error) {
	f.record(filter)
	return dom.Page[dom.Tag]{List: []dom.Tag{{ID: 3, Name: "甜宠", MergedIntoID: ptr(int64(9))}}, Total: 1, Page: 1, PageSize: 20}, f.err
}

func (f *fakeSvc) MergeTag(_ context.Context, source, target int64) (int64, error) {
	f.record([2]int64{source, target})
	return 4, f.err
}

func (f *fakeSvc) CreateTaxonomy(_ context.Context, in dom.TaxonomyInput) (dom.Taxonomy, error) {
	f.record(in)
	return dom.Taxonomy{ID: 8, Name: in.Name, ShowInExplore: in.ShowInExplore}, f.err
}

func (f *fakeSvc) UpdateTerm(_ context.Context, taxonomyID, termID int64, in dom.TermInput) (dom.Term, dom.Term, error) {
	f.record([2]int64{taxonomyID, termID})
	return dom.Term{ID: termID}, dom.Term{ID: termID, TaxonomyID: taxonomyID, Name: in.Name}, f.err
}

func (f *fakeSvc) CreateTopic(_ context.Context, in dom.TopicInput) (dom.Topic, error) {
	f.record(in)
	return dom.Topic{ID: 6, Title: in.Title, SourceType: dom.SourceManual, ListInIndex: in.ListInIndex}, f.err
}

func (f *fakeSvc) SetTopicDramas(_ context.Context, _ int64, ids []int64) ([]int64, []int64, error) {
	f.record(ids)
	return nil, ids, f.err
}

func ptr[T any](v T) *T { return &v }

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

type env struct {
	r     *gin.Engine
	svc   *fakeSvc
	audit *memRecorder
	ids   map[string]adm.Identity
}

// newEnv 与线上一样：admin.Routes 提供会话、CSRF、审计，内容接口注册在它返回的分组上。
func newEnv() *env {
	e := &env{svc: &fakeSvc{}, audit: &memRecorder{}, ids: map[string]adm.Identity{
		"root":   who("root"),
		"viewer": who("viewer", "dramas.view"),
		"editor": who("editor", "dramas.view", "dramas.edit"),
		"tagger": who("tagger", "tags.view", "tags.edit", "taxonomies.view", "taxonomies.edit"),
		"nobody": who("nobody"),
	}}
	h := &admin.Handlers{Auth: fakeAuth{ids: e.ids}, Cookie: admin.NewSessionCookie(false)}
	e.r = gin.New()
	e.r.Use(middleware.RequestID())
	authed := h.Routes(e.r, []string{origin}, e.audit)
	(&Handlers{Svc: e.svc, Now: func() time.Time { return now }}).Routes(authed)
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

func decode(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var b envelope
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("响应不是 JSON：%q", w.Body.String())
	}
	return b
}

func expect(t *testing.T, w *httptest.ResponseRecorder, status int, code string) envelope {
	t.Helper()
	b := decode(t, w)
	if w.Code != status || b.Code != code {
		t.Fatalf("期望 %d %s，得到 %d %s", status, code, w.Code, w.Body.String())
	}
	return b
}

// ---------- 权限 ----------

func TestPermissions(t *testing.T) {
	e := newEnv()
	e.svc.page = dom.Page[dom.Drama]{List: []dom.Drama{}, Page: 1, PageSize: 20}
	tests := []struct {
		as, method, path, body string
		want                   int
	}{
		{"viewer", "GET", "/api/admin/dramas", "", 200},
		{"viewer", "POST", "/api/admin/dramas", `{"title":"新剧"}`, 403},
		{"editor", "POST", "/api/admin/dramas", `{"title":"新剧"}`, 200},
		{"root", "POST", "/api/admin/dramas", `{"title":"新剧"}`, 200},
		{"nobody", "GET", "/api/admin/dramas", "", 403},
		{"tagger", "GET", "/api/admin/dramas", "", 403},
		// 分集属于剧集页面
		{"viewer", "POST", "/api/admin/dramas/1/episodes", `{"number":1}`, 403},
		{"editor", "POST", "/api/admin/dramas/1/episodes", `{"number":1}`, 200},
		{"tagger", "PATCH", "/api/admin/episodes/1", `{"number":1}`, 403},
		{"viewer", "PATCH", "/api/admin/episodes/1", `{"number":1}`, 403},
		{"editor", "PATCH", "/api/admin/episodes/1", `{"number":1}`, 200},
		// 其他页面各有自己的权限点
		{"editor", "GET", "/api/admin/tags", "", 403},
		{"tagger", "GET", "/api/admin/tags", "", 200},
		{"editor", "PATCH", "/api/admin/taxonomies/1/terms/2", `{"name":"都市"}`, 403},
		{"tagger", "PATCH", "/api/admin/taxonomies/1/terms/2", `{"name":"都市"}`, 200},
		{"editor", "GET", "/api/admin/topics", "", 403},
		{"", "GET", "/api/admin/dramas", "", 401},
	}
	for _, tt := range tests {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}

	// 写请求没有 CSRF 令牌时拒绝（由 admin.Routes 的分组提供）
	req := httptest.NewRequest("POST", "/api/admin/dramas", strings.NewReader(`{"title":"x"}`))
	req.Header.Set("User-Agent", testUA)
	req.AddCookie(&http.Cookie{Name: "xs_admin", Value: "root"})
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("缺少 CSRF 令牌应 403，得到 %d", w.Code)
	}
}

func TestDeclaredPermissions(t *testing.T) {
	newEnv()
	got := admin.DeclaredPermissions()
	for _, page := range []string{"dramas", "tags", "taxonomies", "topics"} {
		for _, p := range []string{page + ".view", page + ".edit"} {
			if !slices.Contains(got, p) {
				t.Errorf("内容接口声明的权限点应登记：缺少 %s", p)
			}
		}
	}
	// 演员模块已删除（S1-3a）：迁移 00010 删掉了这两个权限点，路由还声明的话启动校验会失败
	for _, p := range []string{"actors.view", "actors.edit"} {
		if slices.Contains(got, p) {
			t.Errorf("不应再声明 %s", p)
		}
	}
}

// 演员模块已删除（S1-3a）：演员接口与剧的演员表接口不再注册，超级管理员访问也是 404；剧集详情不再带 actors。
func TestActorRoutesRemoved(t *testing.T) {
	e := newEnv()
	for _, r := range []struct{ method, path, body string }{
		{"GET", "/api/admin/actors", ""},
		{"POST", "/api/admin/actors", `{"name":"张三"}`},
		{"PATCH", "/api/admin/actors/1", `{"name":"张三"}`},
		{"DELETE", "/api/admin/actors/1", ""},
		{"PUT", "/api/admin/dramas/1/actors", `{"actors":[]}`},
	} {
		if w := e.do("root", r.method, r.path, r.body); w.Code != http.StatusNotFound {
			t.Errorf("%s %s 应 404，得到 %d %s", r.method, r.path, w.Code, w.Body.String())
		}
	}
	b := expect(t, e.do("viewer", "GET", "/api/admin/dramas/1", ""), 200, "OK")
	var detail map[string]json.RawMessage
	if err := json.Unmarshal(b.Data, &detail); err != nil {
		t.Fatal(err)
	}
	if _, ok := detail["actors"]; ok {
		t.Fatalf("剧集详情不应再有 actors：%s", b.Data)
	}
	for _, k := range []string{"tags", "terms"} {
		if _, ok := detail[k]; !ok {
			t.Fatalf("剧集详情应有 %s：%s", k, b.Data)
		}
	}
}

// ---------- 剧集 ----------

func TestListDramas(t *testing.T) {
	e := newEnv()
	future := now.Add(time.Hour)
	e.svc.page = dom.Page[dom.Drama]{
		List:  []dom.Drama{{ID: 9, Title: "定时剧", Status: dom.DramaPublished, PublishAt: &future, CreatedAt: now, UpdatedAt: now}},
		Total: 31, Page: 2, PageSize: 30,
	}
	w := e.do("viewer", "GET", "/api/admin/dramas?q=%E6%80%BB%E8%A3%81&status=scheduled&tag_id=3&term_id=4&sort=hot&page=2&page_size=30", "")
	b := expect(t, w, 200, "OK")
	want := dom.DramaFilter{Q: "总裁", Status: dom.DramaScheduled, TagID: 3, TermID: 4, Sort: dom.SortHot, PageRequest: dom.PageRequest{Page: 2, PageSize: 30}}
	if got := e.svc.last(); got != want {
		t.Fatalf("查询参数应原样交给服务：%+v", got)
	}
	var page pageDTO[map[string]any]
	if err := json.Unmarshal(b.Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 31 || page.Page != 2 || page.PageSize != 30 || len(page.List) != 1 {
		t.Fatalf("分页信息不符合预期：%s", b.Data)
	}
	d := page.List[0]
	if d["display_status"] != "scheduled" || d["status"] != "published" || d["score"] != nil || d["year"] != nil || d["slug"] != "" {
		t.Fatalf("剧集字段不符合预期：%v", d)
	}
	if aliases, ok := d["aliases"].([]any); !ok || len(aliases) != 0 {
		t.Fatalf("没有别名时为空数组：%v", d["aliases"])
	}

	for _, q := range []string{"page=abc", "tag_id=x"} {
		expect(t, e.do("viewer", "GET", "/api/admin/dramas?"+q, ""), 400, "BAD_REQUEST")
	}
	e.svc.err = errs.ErrBadRequest.WithMessage("排序方式无效")
	if b := expect(t, e.do("viewer", "GET", "/api/admin/dramas?sort=random", ""), 400, "BAD_REQUEST"); b.Message != "排序方式无效" {
		t.Fatalf("领域层的提示原样返回：%s", b.Message)
	}
}

func TestCreateAndUpdateDrama(t *testing.T) {
	e := newEnv()

	// 新建不接受 status（一律草稿）；未知字段拒绝
	expect(t, e.do("editor", "POST", "/api/admin/dramas", `{"title":"新剧","status":"published"}`), 400, "BAD_REQUEST")
	b := expect(t, e.do("editor", "POST", "/api/admin/dramas",
		`{"title":"新剧","aliases":["别名"],"score":9.5,"year":2024,"publish_at":"2026-11-01T00:00:00Z","view_base":10}`), 200, "OK")
	in := e.svc.last().(dom.DramaInput)
	if in.Title != "新剧" || *in.Score != 9.5 || *in.Year != 2024 || !in.PublishAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) || in.Status != dom.DramaDraft {
		t.Fatalf("请求应转为剧资料：%+v", in)
	}
	if !strings.Contains(string(b.Data), `"id":42`) || !strings.Contains(string(b.Data), `"display_status":"draft"`) {
		t.Fatalf("新建应返回完整的剧：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "drama.create" || a.TargetType != "drama" || a.TargetID != "42" || !strings.Contains(string(a.After), "新剧") {
		t.Fatalf("新建应记审计：%+v", a)
	}

	expect(t, e.do("editor", "PATCH", "/api/admin/dramas/abc", `{"title":"x"}`), 404, "NOT_FOUND")
	b = expect(t, e.do("editor", "PATCH", "/api/admin/dramas/7", `{"title":"新名","status":"published"}`), 200, "OK")
	if in := e.svc.last().(dom.DramaInput); in.Status != dom.DramaPublished || in.Title != "新名" {
		t.Fatalf("保存应带状态：%+v", in)
	}
	a := e.audit.last(t)
	if a.Action != "drama.update" || a.TargetID != "7" || !strings.Contains(string(a.Before), "旧名") || !strings.Contains(string(a.After), "新名") {
		t.Fatalf("保存应记修改前后：%+v", a)
	}

	e.svc.err = dom.ErrSlugTaken
	if b := expect(t, e.do("editor", "POST", "/api/admin/dramas", `{"title":"新剧","slug":"taken"}`), 409, "CONFLICT"); b.Message != "slug 已被使用" {
		t.Fatalf("slug 冲突应 409 并说明：%s", b.Message)
	}
	n := len(e.audit.entries)
	e.do("editor", "PATCH", "/api/admin/dramas/7", `{"title":"x","status":"draft"}`)
	if len(e.audit.entries) != n {
		t.Fatal("失败的写请求不记审计")
	}
}

func TestDramaRelationsAndBulk(t *testing.T) {
	e := newEnv()

	expect(t, e.do("editor", "PUT", "/api/admin/dramas/1/tags", `{}`), 400, "BAD_REQUEST")
	b := expect(t, e.do("editor", "PUT", "/api/admin/dramas/1/tags", `{"tag_ids":[5,3]}`), 200, "OK")
	if ids := e.svc.last().([]int64); !slices.Equal(ids, []int64{5, 3}) {
		t.Fatalf("标签应按顺序交给服务：%v", ids)
	}
	if !strings.Contains(string(b.Data), `"tags":[{"id":5`) {
		t.Fatalf("应返回保存后的标签：%s", b.Data)
	}
	a := e.audit.last(t)
	if a.Action != "drama.set_tags" || !strings.Contains(string(a.Before), `"tag_ids":[7]`) || !strings.Contains(string(a.After), `"tag_ids":[5,3]`) {
		t.Fatalf("标签审计应带修改前后：%+v", a)
	}
	// 空数组表示清空
	expect(t, e.do("editor", "PUT", "/api/admin/dramas/1/tags", `{"tag_ids":[]}`), 200, "OK")

	b = expect(t, e.do("editor", "POST", "/api/admin/dramas/bulk", `{"ids":[1,2],"action":"add_tags","tag_ids":[5]}`), 200, "OK")
	if in := e.svc.last().(dom.BulkInput); in.Action != dom.BulkAddTags || !slices.Equal(in.IDs, []int64{1, 2}) || !slices.Equal(in.TagIDs, []int64{5}) {
		t.Fatalf("批量参数：%+v", in)
	}
	if string(b.Data) != `{"affected":2}` {
		t.Fatalf("批量应返回受影响的剧数：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "drama.bulk" || !strings.Contains(string(a.After), `"action":"add_tags"`) || !strings.Contains(string(a.After), `"affected":2`) {
		t.Fatalf("批量审计：%+v %s", a, a.After)
	}
	expect(t, e.do("editor", "POST", "/api/admin/dramas/bulk", `{"action":"publish"}`), 400, "BAD_REQUEST")
}

func TestEpisodes(t *testing.T) {
	e := newEnv()
	// S1-3 不能绑定视频：请求里出现视频字段视为未知字段
	expect(t, e.do("editor", "POST", "/api/admin/dramas/1/episodes", `{"number":1,"video_path":"/v/1.m3u8"}`), 400, "BAD_REQUEST")
	b := expect(t, e.do("editor", "POST", "/api/admin/dramas/1/episodes", `{"number":2,"access":"vip","trial_sec":30}`), 200, "OK")
	if in := e.svc.last().(dom.EpisodeInput); in.Number != 2 || in.Access != dom.AccessVIP || in.TrialSec != 30 {
		t.Fatalf("分集参数：%+v", in)
	}
	if !strings.Contains(string(b.Data), `"video_object_id":null`) || !strings.Contains(string(b.Data), `"media_status":"pending"`) {
		t.Fatalf("分集响应：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "episode.create" || a.TargetType != "episode" || a.TargetID != "5" {
		t.Fatalf("分集审计：%+v", a)
	}
	e.svc.err = errs.ErrConflict.WithMessage("第 2 集已存在")
	if b := expect(t, e.do("editor", "POST", "/api/admin/dramas/1/episodes", `{"number":2}`), 409, "CONFLICT"); b.Message != "第 2 集已存在" {
		t.Fatalf("集号重复：%s", b.Message)
	}
}

// ---------- 标签、维度、专题 ----------

func TestTagsAndTaxonomies(t *testing.T) {
	e := newEnv()

	expect(t, e.do("tagger", "GET", "/api/admin/tags?merged=yes", ""), 400, "BAD_REQUEST")
	b := expect(t, e.do("tagger", "GET", "/api/admin/tags?merged=false&q=%E7%94%9C", ""), 200, "OK")
	if f := e.svc.last().(dom.TagFilter); f.Merged == nil || *f.Merged || f.Q != "甜" {
		t.Fatalf("merged=false 只看未合并的：%+v", f)
	}
	if !strings.Contains(string(b.Data), `"merged_into_id":9`) {
		t.Fatalf("标签应带合并目标：%s", b.Data)
	}

	expect(t, e.do("tagger", "POST", "/api/admin/tags/3/merge", `{}`), 400, "BAD_REQUEST")
	b = expect(t, e.do("tagger", "POST", "/api/admin/tags/3/merge", `{"target_id":9}`), 200, "OK")
	if got := e.svc.last().([2]int64); got != [2]int64{3, 9} || string(b.Data) != `{"merged_into_id":9,"moved_dramas":4}` {
		t.Fatalf("合并：%v %s", got, b.Data)
	}
	if a := e.audit.last(t); a.Action != "tag.merge" || a.TargetID != "3" || !strings.Contains(string(a.After), `"moved_dramas":4`) {
		t.Fatalf("合并审计：%+v", a)
	}

	// show_in_explore 未传时默认显示在探索页
	expect(t, e.do("tagger", "POST", "/api/admin/taxonomies", `{"name":"地区"}`), 200, "OK")
	if in := e.svc.last().(dom.TaxonomyInput); !in.ShowInExplore {
		t.Fatalf("show_in_explore 默认 true：%+v", in)
	}
	expect(t, e.do("tagger", "POST", "/api/admin/taxonomies", `{"name":"地区","show_in_explore":false}`), 200, "OK")
	if in := e.svc.last().(dom.TaxonomyInput); in.ShowInExplore {
		t.Fatalf("show_in_explore=false 应保存：%+v", in)
	}

	expect(t, e.do("tagger", "PATCH", "/api/admin/taxonomies/3/terms/abc", `{"name":"x"}`), 404, "NOT_FOUND")
	expect(t, e.do("tagger", "PATCH", "/api/admin/taxonomies/3/terms/12", `{"name":"都市"}`), 200, "OK")
	if got := e.svc.last().([2]int64); got != [2]int64{3, 12} {
		t.Fatalf("应解析维度与取值 ID：%v", got)
	}
	if a := e.audit.last(t); a.Action != "taxonomy_term.update" || a.TargetID != "12" {
		t.Fatalf("取值审计：%+v", a)
	}
}

func TestTopics(t *testing.T) {
	e := newEnv()
	e.ids["root"] = who("root")

	b := expect(t, e.do("root", "POST", "/api/admin/topics", `{"title":"年度好剧","faq":[{"question":"在哪看？","answer":"本站"}]}`), 200, "OK")
	in := e.svc.last().(dom.TopicInput)
	if !in.ListInIndex || len(in.FAQ) != 1 || in.FAQ[0].Answer != "本站" {
		t.Fatalf("list_in_index 默认 true，FAQ 透传：%+v", in)
	}
	// 响应中的规则按默认值补齐，FAQ 为空数组
	if !strings.Contains(string(b.Data), `"rule":{"term_codes":[],"tag_id":null,"sort":"latest","window":"all","limit":24}`) ||
		!strings.Contains(string(b.Data), `"faq":[]`) {
		t.Fatalf("专题响应：%s", b.Data)
	}
	expect(t, e.do("root", "POST", "/api/admin/topics", `{"title":"x","rule":{"unknown":1}}`), 400, "BAD_REQUEST")

	expect(t, e.do("root", "PUT", "/api/admin/topics/6/dramas", `{"drama_ids":null}`), 400, "BAD_REQUEST")
	b = expect(t, e.do("root", "PUT", "/api/admin/topics/6/dramas", `{"drama_ids":[3,1,2]}`), 200, "OK")
	if string(b.Data) != `{"drama_ids":[3,1,2]}` {
		t.Fatalf("应返回保存后的顺序：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "topic.set_dramas" || string(a.Before) != `{"drama_ids":[]}` {
		t.Fatalf("手选剧审计：%+v %s", a, a.Before)
	}
}
