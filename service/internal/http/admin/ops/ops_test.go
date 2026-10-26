package ops

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
	dom "xsite/service/internal/domain/ops"
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

var now = time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)

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

// recorder 记录服务收到的参数。
type recorder struct {
	mu  sync.Mutex
	got []any
}

func (r *recorder) record(v any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, v)
}

func (r *recorder) last() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) == 0 {
		return nil
	}
	return r.got[len(r.got)-1]
}

// fakeAnnouncements 只实现测试用到的方法。
type fakeAnnouncements struct {
	recorder
	err error
}

func (f *fakeAnnouncements) List(_ context.Context, filter dom.AnnouncementFilter) (dom.Page[dom.Announcement], error) {
	f.record(filter)
	end := now.Add(-time.Hour)
	return dom.Page[dom.Announcement]{List: []dom.Announcement{
		{ID: 3, Kind: dom.AnnouncementTopbar, Content: "滚动", Frequency: dom.FrequencyAlways, Status: dom.StatusActive},
		{ID: 2, Kind: dom.AnnouncementPopup, Content: "过期", Frequency: dom.FrequencyOnce, Status: dom.StatusActive, EndAt: &end, Pages: []string{"home"}},
	}, Total: 2, Page: 1, PageSize: 20}, f.err
}

func (f *fakeAnnouncements) Create(_ context.Context, in dom.AnnouncementInput) (dom.Announcement, error) {
	f.record(in)
	return dom.Announcement{ID: 9, Kind: in.Kind, Content: in.Content, Status: dom.StatusActive, CreatedAt: now, UpdatedAt: now}, f.err
}

func (f *fakeAnnouncements) Update(_ context.Context, id int64, in dom.AnnouncementInput) (dom.Announcement, dom.Announcement, error) {
	f.record(in)
	return dom.Announcement{ID: id, Kind: dom.AnnouncementTopbar, Content: "旧内容", Status: dom.StatusActive},
		dom.Announcement{ID: id, Kind: in.Kind, Content: in.Content, Status: in.Status}, f.err
}

func (f *fakeAnnouncements) Delete(_ context.Context, id int64) (dom.Announcement, error) {
	f.record(id)
	return dom.Announcement{ID: id, Kind: dom.AnnouncementTopbar, Content: "被删除"}, f.err
}

// adCall 是广告服务收到的写请求。
type adCall struct {
	in         dom.AdInput
	customHTML bool
}

// fakeAds 只实现测试用到的方法；HTML 素材没有权限时与领域层一样返回 403。
type fakeAds struct {
	recorder
	err error
}

func (f *fakeAds) List(_ context.Context, filter dom.AdFilter) (dom.Page[dom.Ad], error) {
	f.record(filter)
	end := now.Add(-time.Hour)
	return dom.Page[dom.Ad]{List: []dom.Ad{
		{ID: 5, Name: "顶部", Slot: dom.SlotHomeTop, CreativeType: dom.CreativeImage, Image: "/a.webp", Device: dom.AdDeviceAll, Weight: 10,
			Status: dom.StatusActive, ImpCount: 200, ClickCount: 3},
		{ID: 4, Name: "过期", Slot: dom.SlotTextLink, CreativeType: dom.CreativeText, Text: "文字", Device: dom.AdDeviceMobile, Weight: 1,
			Status: dom.StatusActive, EndAt: &end, Locales: []string{"en"}},
	}, Total: 2, Page: 1, PageSize: 20}, f.err
}

func (f *fakeAds) Create(_ context.Context, in dom.AdInput, customHTML bool) (dom.Ad, error) {
	f.record(adCall{in, customHTML})
	if in.CreativeType == dom.CreativeHTML && !customHTML {
		return dom.Ad{}, dom.ErrCustomHTMLForbidden
	}
	return dom.Ad{ID: 9, Name: in.Name, Slot: in.Slot, CreativeType: in.CreativeType, Status: dom.StatusActive, CreatedAt: now, UpdatedAt: now}, f.err
}

func (f *fakeAds) Update(_ context.Context, id int64, in dom.AdInput, customHTML bool) (dom.Ad, dom.Ad, error) {
	f.record(adCall{in, customHTML})
	if in.CreativeType == dom.CreativeHTML && !customHTML {
		return dom.Ad{}, dom.Ad{}, dom.ErrCustomHTMLForbidden
	}
	return dom.Ad{ID: id, Name: "旧广告名", Slot: dom.SlotHomeTop, CreativeType: dom.CreativeImage, Status: dom.StatusActive},
		dom.Ad{ID: id, Name: in.Name, Slot: in.Slot, CreativeType: in.CreativeType, Status: in.Status}, f.err
}

func (f *fakeAds) Delete(_ context.Context, id int64) (dom.Ad, error) {
	f.record(id)
	return dom.Ad{ID: id, Name: "被删除", Slot: dom.SlotHomeTop, CreativeType: dom.CreativeImage}, f.err
}

// fakeHot 只实现测试用到的方法。
type fakeHot struct {
	recorder
	err error
}

func (f *fakeHot) List(_ context.Context, filter dom.HotFilter) (dom.Page[dom.HotWord], error) {
	f.record(filter)
	last := now.Add(-time.Hour)
	return dom.Page[dom.HotWord]{List: []dom.HotWord{
		{ID: 3, Word: "霸道总裁", SearchCount: 900, Pinned: true, PinSort: 1, Visible: true, Source: dom.HotManual},
		{ID: 5, Word: "重生", SearchCount: 40, MissCount: 40, Source: dom.HotAuto, LastSearchedAt: &last},
	}, Total: 2, Page: 1, PageSize: 20}, f.err
}

func (f *fakeHot) Create(_ context.Context, word string) (dom.HotWord, error) {
	f.record(word)
	return dom.HotWord{ID: 9, Word: word, Visible: true, Source: dom.HotManual, CreatedAt: now, UpdatedAt: now}, f.err
}

func (f *fakeHot) Update(_ context.Context, id int64, p dom.HotPatch) (dom.HotWord, dom.HotWord, error) {
	f.record(p)
	after := dom.HotWord{ID: id, Word: "重生", Source: dom.HotAuto}
	if p.Visible != nil {
		after.Visible = *p.Visible
	}
	if p.Pinned != nil {
		after.Pinned, after.Visible = *p.Pinned, after.Visible || *p.Pinned
	}
	return dom.HotWord{ID: id, Word: "重生", Source: dom.HotAuto}, after, f.err
}

func (f *fakeHot) Reorder(_ context.Context, ids []int64) error {
	f.record(ids)
	return f.err
}

func (f *fakeHot) Delete(_ context.Context, id int64) (dom.HotWord, error) {
	f.record(id)
	return dom.HotWord{ID: id, Word: "被删除", Source: dom.HotAuto, SearchCount: 7}, f.err
}

// fakeRanks 只实现测试用到的方法。sets 单独记录 SetOverrides 收到的列表（保存后 handler 还会读一次整张榜）。
type fakeRanks struct {
	recorder
	err  error
	done bool
	sets [][]dom.RankOverride
}

func (f *fakeRanks) AdminBoard(_ context.Context, b dom.RankBoard) (dom.RankAdminView, error) {
	f.record(b)
	if b != dom.BoardDramaWeek && b != dom.BoardDramaMonth {
		return dom.RankAdminView{}, errs.ErrNotFound
	}
	at := now.Add(-time.Minute)
	return dom.RankAdminView{
		Board: b,
		Overrides: []dom.RankOverrideView{{
			RankOverride: dom.RankOverride{DramaID: 7, Pinned: true}, Title: "霸道总裁", DramaStatus: "published", UpdatedAt: now,
		}},
		ComputedAt: &at,
		Live:       []dom.RankRow{{Rank: 1, DramaID: 3, Title: "重生", Heat: 300}},
		Preview:    []dom.RankRow{{Rank: 1, DramaID: 7, Title: "霸道总裁", Heat: 12.5, Pinned: true}},
	}, f.err
}

func (f *fakeRanks) SetOverrides(_ context.Context, b dom.RankBoard, list []dom.RankOverride) ([]dom.RankOverrideView, []dom.RankOverrideView, error) {
	f.sets = append(f.sets, list)
	after := make([]dom.RankOverrideView, len(list))
	for i, o := range list {
		after[i] = dom.RankOverrideView{RankOverride: o, Title: "剧", DramaStatus: "published"}
	}
	return []dom.RankOverrideView{{RankOverride: dom.RankOverride{DramaID: 1, Blocked: true}, Title: "旧调整"}}, after, f.err
}

func (f *fakeRanks) RecomputeNow(context.Context) (bool, error) {
	f.record("recompute")
	return f.done, f.err
}

func (f *fakeRanks) Weights(context.Context) (dom.RankWeights, error) {
	return dom.DefaultRankWeights, f.err
}

func (f *fakeRanks) SetWeights(_ context.Context, w dom.RankWeights) (dom.RankWeights, dom.RankWeights, error) {
	f.record(w)
	return dom.DefaultRankWeights, w, f.err
}

type env struct {
	r     *gin.Engine
	ann   *fakeAnnouncements
	ads   *fakeAds
	hot   *fakeHot
	ranks *fakeRanks
	audit *memRecorder
	ids   map[string]adm.Identity
}

// newEnv 与线上一样：admin.Routes 提供会话、CSRF、审计，运营接口注册在它返回的分组上。
func newEnv() *env {
	e := &env{ann: &fakeAnnouncements{}, ads: &fakeAds{}, hot: &fakeHot{}, ranks: &fakeRanks{done: true}, audit: &memRecorder{}, ids: map[string]adm.Identity{
		"root":      who("root"),
		"viewer":    who("viewer", "announcements.view"),
		"editor":    who("editor", "announcements.view", "announcements.edit"),
		"adviewer":  who("adviewer", "ads.view"),
		"adeditor":  who("adeditor", "ads.view", "ads.edit"),
		"adhtml":    who("adhtml", "ads.view", "ads.edit", "ads.custom_html"),
		"hotviewer": who("hotviewer", "hotsearch.view"),
		"hoteditor": who("hoteditor", "hotsearch.view", "hotsearch.edit"),
		"rkviewer":  who("rkviewer", "ranks.view"),
		"rkeditor":  who("rkeditor", "ranks.view", "ranks.edit"),
		"nobody":    who("nobody"),
	}}
	h := &admin.Handlers{Auth: fakeAuth{ids: e.ids}, Cookie: admin.NewSessionCookie(false)}
	e.r = gin.New()
	e.r.Use(middleware.RequestID())
	authed := h.Routes(e.r, []string{origin}, e.audit)
	(&Handlers{Announcements: e.ann, Ads: e.ads, Hotsearch: e.hot, Ranks: e.ranks, Now: func() time.Time { return now }}).Routes(authed)
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

const annBody = `{"kind":"popup","content":"国庆会员 5 折","click_url":"/vip","frequency":"daily","pages":["home"],"locales":["en"],"image":"/a.webp","start_at":"2026-10-25T00:00:00Z","end_at":null,"status":"active"}`

func TestAnnouncementPermissions(t *testing.T) {
	e := newEnv()
	for _, tt := range []struct {
		as, method, path, body string
		want                   int
	}{
		{"viewer", "GET", "/api/admin/announcements", "", 200},
		{"viewer", "POST", "/api/admin/announcements", annBody, 403},
		{"viewer", "DELETE", "/api/admin/announcements/1", "", 403},
		{"editor", "POST", "/api/admin/announcements", annBody, 200},
		{"editor", "PATCH", "/api/admin/announcements/1", annBody, 200},
		{"editor", "DELETE", "/api/admin/announcements/1", "", 200},
		{"root", "POST", "/api/admin/announcements", annBody, 200},
		{"nobody", "GET", "/api/admin/announcements", "", 403},
		{"", "GET", "/api/admin/announcements", "", 401},
	} {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}
	got := admin.DeclaredPermissions()
	for _, p := range []string{"announcements.view", "announcements.edit"} {
		if !slices.Contains(got, p) {
			t.Errorf("应声明权限点 %s", p)
		}
	}
}

func TestListAnnouncements(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("viewer", "GET", "/api/admin/announcements?q=%E4%BC%9A%E5%91%98&kind=popup&status=active&page=2&page_size=50", ""), 200, "OK")
	want := dom.AnnouncementFilter{Q: "会员", Kind: dom.AnnouncementPopup, Status: dom.StatusActive, PageRequest: dom.PageRequest{Page: 2, PageSize: 50}}
	if got := e.ann.last(); got != any(want) {
		t.Fatalf("查询参数应原样交给服务：%+v", got)
	}
	var page pageDTO[map[string]any]
	if err := json.Unmarshal(b.Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.List) != 2 {
		t.Fatalf("分页信息：%s", b.Data)
	}
	if a := page.List[0]; a["display_status"] != "live" || a["start_at"] != nil {
		t.Fatalf("显示状态由服务端算出，不限时间为 null：%v", a)
	}
	if pages, ok := page.List[0]["pages"].([]any); !ok || len(pages) != 0 {
		t.Fatalf("不选页面输出空数组：%v", page.List[0]["pages"])
	}
	if page.List[1]["display_status"] != "ended" {
		t.Fatalf("已过结束时间：%v", page.List[1])
	}
	expect(t, e.do("viewer", "GET", "/api/admin/announcements?page=abc", ""), 400, "BAD_REQUEST")
	e.ann.err = errs.ErrBadRequest.WithMessage("类型筛选无效")
	if b := expect(t, e.do("viewer", "GET", "/api/admin/announcements?kind=x", ""), 400, "BAD_REQUEST"); b.Message != "类型筛选无效" {
		t.Fatalf("领域层的提示原样返回：%s", b.Message)
	}
}

func TestAnnouncementWrites(t *testing.T) {
	e := newEnv()

	// 新建：请求体转为领域输入；未知字段拒绝；记审计
	expect(t, e.do("editor", "POST", "/api/admin/announcements", `{"kind":"topbar","content":"x","color":"red"}`), 400, "BAD_REQUEST")
	b := expect(t, e.do("editor", "POST", "/api/admin/announcements", annBody), 200, "OK")
	in := e.ann.last().(dom.AnnouncementInput)
	if in.Kind != dom.AnnouncementPopup || in.Frequency != dom.FrequencyDaily || !slices.Equal(in.Pages, []string{"home"}) ||
		!slices.Equal(in.Locales, []string{"en"}) || !in.StartAt.Equal(time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)) || in.EndAt != nil {
		t.Fatalf("请求应转为公告输入：%+v", in)
	}
	if !strings.Contains(string(b.Data), `"id":9`) || !strings.Contains(string(b.Data), `"display_status":"live"`) {
		t.Fatalf("新建应返回完整记录：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "announcement.create" || a.TargetType != "announcement" || a.TargetID != "9" || !strings.Contains(string(a.After), "国庆会员") {
		t.Fatalf("新建审计：%+v", a)
	}

	// 保存：审计带修改前后；路径编号无效 404
	expect(t, e.do("editor", "PATCH", "/api/admin/announcements/abc", annBody), 404, "NOT_FOUND")
	expect(t, e.do("editor", "PATCH", "/api/admin/announcements/7", `{"kind":"topbar","content":"新内容","status":"inactive"}`), 200, "OK")
	if a := e.audit.last(t); a.Action != "announcement.update" || a.TargetID != "7" || !strings.Contains(string(a.Before), "旧内容") || !strings.Contains(string(a.After), "新内容") {
		t.Fatalf("保存审计：%+v", a)
	}

	// 删除：审计带删除前的记录
	expect(t, e.do("editor", "DELETE", "/api/admin/announcements/7", ""), 200, "OK")
	if a := e.audit.last(t); a.Action != "announcement.delete" || a.TargetID != "7" || !strings.Contains(string(a.Before), "被删除") {
		t.Fatalf("删除审计：%+v", a)
	}

	// 失败的写请求不记审计
	n := len(e.audit.entries)
	e.ann.err = errs.ErrNotFound
	expect(t, e.do("editor", "DELETE", "/api/admin/announcements/8", ""), 404, "NOT_FOUND")
	if len(e.audit.entries) != n {
		t.Fatal("失败的写请求不记审计")
	}
}

const adBody = `{"name":"首页顶部会员","slot":"home_top","creative_type":"image","image":"/ads/top.webp","video_url":"","text":"","html":"",` +
	`"alt":"会员 5 折","click_url":"/vip","device":"mobile","locales":["en"],"weight":30,"end_at":"2026-11-01T00:00:00Z","freq_cap":3,"status":"active"}`

const adHTMLBody = `{"name":"自定义","slot":"home_mid","creative_type":"html","html":"<p>x</p>"}`

func TestAdPermissions(t *testing.T) {
	e := newEnv()
	for _, tt := range []struct {
		as, method, path, body string
		want                   int
	}{
		{"adviewer", "GET", "/api/admin/ads", "", 200},
		{"adviewer", "POST", "/api/admin/ads", adBody, 403},
		{"adviewer", "PATCH", "/api/admin/ads/1", adBody, 403},
		{"adviewer", "DELETE", "/api/admin/ads/1", "", 403},
		{"adeditor", "POST", "/api/admin/ads", adBody, 200},
		{"adeditor", "PATCH", "/api/admin/ads/1", adBody, 200},
		{"adeditor", "DELETE", "/api/admin/ads/1", "", 200},
		{"adeditor", "POST", "/api/admin/ads", adHTMLBody, 403}, // 没有 ads.custom_html 不能建 HTML 素材
		{"adeditor", "PATCH", "/api/admin/ads/1", adHTMLBody, 403},
		{"adhtml", "POST", "/api/admin/ads", adHTMLBody, 200},
		{"root", "PATCH", "/api/admin/ads/1", adHTMLBody, 200},
		{"editor", "GET", "/api/admin/ads", "", 403}, // 公告的权限不能看广告
		{"", "GET", "/api/admin/ads", "", 401},
	} {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}
	// 「有没有自定义 HTML 权限」由 HTTP 层按当前管理员算出交给领域层
	for as, want := range map[string]bool{"adeditor": false, "adhtml": true, "root": true} {
		e.do(as, "POST", "/api/admin/ads", adBody)
		if got := e.ads.last().(adCall); got.customHTML != want {
			t.Errorf("%s：customHTML 应为 %v", as, want)
		}
	}
	got := admin.DeclaredPermissions()
	for _, p := range []string{"ads.view", "ads.edit", "ads.custom_html"} {
		if !slices.Contains(got, p) {
			t.Errorf("应声明权限点 %s（启动时与权限点目录比对）", p)
		}
	}
}

func TestListAds(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("adviewer", "GET", "/api/admin/ads?q=%E4%BC%9A%E5%91%98&slot=home_top&status=inactive&page=3&page_size=10", ""), 200, "OK")
	want := dom.AdFilter{Q: "会员", Slot: dom.SlotHomeTop, Status: dom.StatusInactive, PageRequest: dom.PageRequest{Page: 3, PageSize: 10}}
	if got := e.ads.last(); got != any(want) {
		t.Fatalf("查询参数应原样交给服务：%+v", got)
	}
	var page pageDTO[map[string]any]
	if err := json.Unmarshal(b.Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.List) != 2 {
		t.Fatalf("分页信息：%s", b.Data)
	}
	a := page.List[0]
	if a["display_status"] != "live" || a["imp_count"] != float64(200) || a["click_count"] != float64(3) || a["end_at"] != nil ||
		a["html"] != "" || a["slot"] != "home_top" || a["creative_type"] != "image" {
		t.Fatalf("列表字段：%v", a)
	}
	if locales, ok := a["locales"].([]any); !ok || len(locales) != 0 {
		t.Fatalf("不选语言输出空数组：%v", a["locales"])
	}
	if page.List[1]["display_status"] != "ended" {
		t.Fatalf("已过结束时间：%v", page.List[1])
	}
	expect(t, e.do("adviewer", "GET", "/api/admin/ads?page_size=x", ""), 400, "BAD_REQUEST")
}

func TestAdWrites(t *testing.T) {
	e := newEnv()

	expect(t, e.do("adeditor", "POST", "/api/admin/ads", `{"name":"x","slot":"home_top","creative_type":"text","text":"t","color":"red"}`), 400, "BAD_REQUEST")
	b := expect(t, e.do("adeditor", "POST", "/api/admin/ads", adBody), 200, "OK")
	in := e.ads.last().(adCall).in
	end := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if in.Name != "首页顶部会员" || in.Slot != dom.SlotHomeTop || in.CreativeType != dom.CreativeImage || in.Image != "/ads/top.webp" ||
		in.Alt != "会员 5 折" || in.ClickURL != "/vip" || in.Device != dom.AdDeviceMobile || !slices.Equal(in.Locales, []string{"en"}) ||
		in.Weight != 30 || !in.EndAt.Equal(end) || in.FreqCap != 3 || in.Status != dom.StatusActive {
		t.Fatalf("请求应转为广告输入：%+v", in)
	}
	if !strings.Contains(string(b.Data), `"id":9`) || !strings.Contains(string(b.Data), `"display_status":"live"`) {
		t.Fatalf("新建应返回完整记录：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "ad.create" || a.TargetType != "ad" || a.TargetID != "9" || !strings.Contains(string(a.After), "首页顶部会员") {
		t.Fatalf("新建审计：%+v", a)
	}

	expect(t, e.do("adeditor", "PATCH", "/api/admin/ads/abc", adBody), 404, "NOT_FOUND")
	expect(t, e.do("adeditor", "PATCH", "/api/admin/ads/7", `{"name":"新广告名","slot":"text_link","creative_type":"text","text":"t","status":"inactive"}`), 200, "OK")
	if a := e.audit.last(t); a.Action != "ad.update" || a.TargetID != "7" || !strings.Contains(string(a.Before), "旧广告名") || !strings.Contains(string(a.After), "新广告名") {
		t.Fatalf("保存审计：%+v", a)
	}

	expect(t, e.do("adeditor", "DELETE", "/api/admin/ads/7", ""), 200, "OK")
	if a := e.audit.last(t); a.Action != "ad.delete" || a.TargetID != "7" || !strings.Contains(string(a.Before), "被删除") {
		t.Fatalf("删除审计：%+v", a)
	}

	// 失败的写请求（含没有 HTML 权限）不记审计
	n := len(e.audit.entries)
	expect(t, e.do("adeditor", "POST", "/api/admin/ads", adHTMLBody), 403, "FORBIDDEN")
	e.ads.err = errs.ErrNotFound
	expect(t, e.do("adeditor", "DELETE", "/api/admin/ads/8", ""), 404, "NOT_FOUND")
	if len(e.audit.entries) != n {
		t.Fatal("失败的写请求不记审计")
	}
}

func TestHotsearchPermissions(t *testing.T) {
	e := newEnv()
	for _, tt := range []struct {
		as, method, path, body string
		want                   int
	}{
		{"hotviewer", "GET", "/api/admin/hotsearch", "", 200},
		{"hotviewer", "POST", "/api/admin/hotsearch", `{"word":"甜宠"}`, 403},
		{"hotviewer", "PATCH", "/api/admin/hotsearch/1", `{"visible":true}`, 403},
		{"hotviewer", "POST", "/api/admin/hotsearch/reorder", `{"ids":[1]}`, 403},
		{"hotviewer", "DELETE", "/api/admin/hotsearch/1", "", 403},
		{"hoteditor", "POST", "/api/admin/hotsearch", `{"word":"甜宠"}`, 200},
		{"hoteditor", "PATCH", "/api/admin/hotsearch/1", `{"visible":true}`, 200},
		{"hoteditor", "POST", "/api/admin/hotsearch/reorder", `{"ids":[1]}`, 200},
		{"hoteditor", "DELETE", "/api/admin/hotsearch/1", "", 200},
		{"adeditor", "GET", "/api/admin/hotsearch", "", 403},
		{"", "GET", "/api/admin/hotsearch", "", 401},
	} {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}
	got := admin.DeclaredPermissions()
	for _, p := range []string{"hotsearch.view", "hotsearch.edit"} {
		if !slices.Contains(got, p) {
			t.Errorf("应声明权限点 %s", p)
		}
	}
}

func TestListHotWords(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("hotviewer", "GET", "/api/admin/hotsearch?q=%E9%87%8D%E7%94%9F&visible=false&pinned=true&missing=true&page=2&page_size=10", ""), 200, "OK")
	f := e.hot.last().(dom.HotFilter)
	if f.Q != "重生" || f.Visible == nil || *f.Visible || f.Pinned == nil || !*f.Pinned || !f.Missing || f.Page != 2 || f.PageSize != 10 {
		t.Fatalf("查询参数应交给服务：%+v", f)
	}
	e.do("hotviewer", "GET", "/api/admin/hotsearch", "")
	if f := e.hot.last().(dom.HotFilter); f.Visible != nil || f.Pinned != nil || f.Missing {
		t.Fatalf("不传筛选时不筛：%+v", f)
	}
	var page pageDTO[map[string]any]
	if err := json.Unmarshal(b.Data, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.List) != 2 || page.List[0]["word"] != "霸道总裁" || page.List[0]["pinned"] != true || page.List[0]["last_searched_at"] != nil ||
		page.List[1]["miss_count"] != float64(40) || page.List[1]["source"] != "auto" || page.List[1]["last_searched_at"] == nil {
		t.Fatalf("列表字段：%s", b.Data)
	}
	for _, bad := range []string{"visible=yes", "pinned=1", "missing=false", "page=x"} {
		expect(t, e.do("hotviewer", "GET", "/api/admin/hotsearch?"+bad, ""), 400, "BAD_REQUEST")
	}
}

func TestHotsearchWrites(t *testing.T) {
	e := newEnv()

	expect(t, e.do("hoteditor", "POST", "/api/admin/hotsearch", `{"word":"甜宠","visible":false}`), 400, "BAD_REQUEST")
	b := expect(t, e.do("hoteditor", "POST", "/api/admin/hotsearch", `{"word":" 甜宠 "}`), 200, "OK")
	if e.hot.last() != " 甜宠 " || !strings.Contains(string(b.Data), `"id":9`) {
		t.Fatalf("原样交给服务规范化：%v %s", e.hot.last(), b.Data)
	}
	if a := e.audit.last(t); a.Action != "hotsearch.create" || a.TargetType != "hotsearch" || a.TargetID != "9" {
		t.Fatalf("新建审计：%+v", a)
	}

	// 部分修改：只传要改的项
	expect(t, e.do("hoteditor", "PATCH", "/api/admin/hotsearch/5", `{"pinned":true}`), 200, "OK")
	if p := e.hot.last().(dom.HotPatch); p.Visible != nil || p.Pinned == nil || !*p.Pinned {
		t.Fatalf("只交要改的项：%+v", p)
	}
	if a := e.audit.last(t); a.Action != "hotsearch.update" || a.TargetID != "5" || !strings.Contains(string(a.After), `"pinned":true`) {
		t.Fatalf("修改审计：%+v %s", a, a.After)
	}
	expect(t, e.do("hoteditor", "PATCH", "/api/admin/hotsearch/5", `{"visible":"yes"}`), 400, "BAD_REQUEST")
	expect(t, e.do("hoteditor", "PATCH", "/api/admin/hotsearch/abc", `{"visible":true}`), 404, "NOT_FOUND")

	expect(t, e.do("hoteditor", "POST", "/api/admin/hotsearch/reorder", `{"ids":[5,3]}`), 200, "OK")
	if ids := e.hot.last().([]int64); !slices.Equal(ids, []int64{5, 3}) {
		t.Fatalf("顺序原样交给服务：%v", ids)
	}
	if a := e.audit.last(t); a.Action != "hotsearch.reorder" || !strings.Contains(string(a.After), "5") {
		t.Fatalf("排序审计：%+v", a)
	}

	expect(t, e.do("hoteditor", "DELETE", "/api/admin/hotsearch/5", ""), 200, "OK")
	if a := e.audit.last(t); a.Action != "hotsearch.delete" || a.TargetID != "5" || !strings.Contains(string(a.Before), "被删除") {
		t.Fatalf("删除审计：%+v", a)
	}

	n := len(e.audit.entries)
	e.hot.err = dom.ErrPinsChanged
	expect(t, e.do("hoteditor", "POST", "/api/admin/hotsearch/reorder", `{"ids":[5]}`), 409, "CONFLICT")
	if len(e.audit.entries) != n {
		t.Fatal("失败的写请求不记审计")
	}
}

func TestRankPermissions(t *testing.T) {
	e := newEnv()
	for _, tt := range []struct {
		as, method, path, body string
		want                   int
	}{
		{"rkviewer", "GET", "/api/admin/ranks/drama_week", "", 200},
		{"rkviewer", "GET", "/api/admin/ranking-config", "", 200},
		{"rkviewer", "PUT", "/api/admin/ranks/drama_week", `{"overrides":[]}`, 403},
		{"rkviewer", "POST", "/api/admin/ranks/recompute", "", 403},
		{"rkviewer", "PUT", "/api/admin/ranking-config", `{"play":1,"follow":3,"like":2,"finish":2}`, 403},
		{"rkeditor", "PUT", "/api/admin/ranks/drama_week", `{"overrides":[]}`, 200},
		{"rkeditor", "POST", "/api/admin/ranks/recompute", "", 200},
		{"rkeditor", "PUT", "/api/admin/ranking-config", `{"play":1,"follow":3,"like":2,"finish":2}`, 200},
		{"hoteditor", "GET", "/api/admin/ranks/drama_week", "", 403},
		{"", "GET", "/api/admin/ranking-config", "", 401},
	} {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}
	got := admin.DeclaredPermissions()
	for _, p := range []string{"ranks.view", "ranks.edit"} {
		if !slices.Contains(got, p) {
			t.Errorf("应声明权限点 %s", p)
		}
	}
}

func TestGetRankBoard(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("rkviewer", "GET", "/api/admin/ranks/drama_month", ""), 200, "OK")
	if e.ranks.last() != any(dom.BoardDramaMonth) {
		t.Fatalf("榜单名交给服务：%v", e.ranks.last())
	}
	var v map[string]any
	if err := json.Unmarshal(b.Data, &v); err != nil {
		t.Fatal(err)
	}
	ov := v["overrides"].([]any)[0].(map[string]any)
	live := v["live"].([]any)[0].(map[string]any)
	pre := v["preview"].([]any)[0].(map[string]any)
	if v["board"] != "drama_month" || v["computed_at"] == nil || ov["drama_id"] != float64(7) || ov["title"] != "霸道总裁" ||
		ov["pinned"] != true || ov["drama_status"] != "published" || ov["in_trash"] != false ||
		live["rank"] != float64(1) || live["heat"] != float64(300) || pre["pinned"] != true || pre["heat"] != 12.5 {
		t.Fatalf("榜单页面数据：%s", b.Data)
	}
	expect(t, e.do("rkviewer", "GET", "/api/admin/ranks/tag_week", ""), 404, "NOT_FOUND")
}

func TestSetRankOverrides(t *testing.T) {
	e := newEnv()
	body := `{"overrides":[{"drama_id":7,"pinned":true,"blocked":false,"boost":0},{"drama_id":9,"pinned":false,"blocked":false,"boost":-20}]}`
	b := expect(t, e.do("rkeditor", "PUT", "/api/admin/ranks/drama_week", body), 200, "OK")
	list := e.ranks.sets[len(e.ranks.sets)-1]
	if len(list) != 2 || list[0] != (dom.RankOverride{DramaID: 7, Pinned: true}) || list[1] != (dom.RankOverride{DramaID: 9, Boost: -20}) {
		t.Fatalf("请求转为调整列表：%+v", list)
	}
	if e.ranks.last() != any(dom.BoardDramaWeek) {
		t.Fatalf("保存后读回同一张榜：%v", e.ranks.last())
	}
	if !strings.Contains(string(b.Data), `"preview"`) {
		t.Fatalf("保存后返回整张榜的页面数据（预览按新设置）：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "rank.update" || a.TargetType != "rank" || a.TargetID != "drama_week" ||
		!strings.Contains(string(a.Before), "旧调整") || !strings.Contains(string(a.After), `"drama_id":9`) {
		t.Fatalf("审计带修改前后：%+v", a)
	}
	// 没传 overrides 不当作清空，必须显式传空数组
	expect(t, e.do("rkeditor", "PUT", "/api/admin/ranks/drama_week", `{}`), 400, "BAD_REQUEST")
	expect(t, e.do("rkeditor", "PUT", "/api/admin/ranks/drama_week", `{"overrides":[{"drama_id":"7"}]}`), 400, "BAD_REQUEST")
	if len(e.ranks.sets) != 1 {
		t.Fatalf("格式错误时不调用服务：%d 次", len(e.ranks.sets))
	}
	expect(t, e.do("rkeditor", "PUT", "/api/admin/ranks/drama_week", `{"overrides":[]}`), 200, "OK")
	if list := e.ranks.sets[len(e.ranks.sets)-1]; list == nil || len(list) != 0 {
		t.Fatalf("空数组表示清空：%#v", list)
	}
}

func TestRecomputeRanks(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("rkeditor", "POST", "/api/admin/ranks/recompute", ""), 200, "OK")
	if !strings.Contains(string(b.Data), `"done":true`) {
		t.Fatalf("算完：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "rank.recompute" || a.TargetType != "rank" {
		t.Fatalf("审计：%+v", a)
	}
	e.ranks.done = false
	if b := expect(t, e.do("rkeditor", "POST", "/api/admin/ranks/recompute", ""), 200, "OK"); !strings.Contains(string(b.Data), `"done":false`) {
		t.Fatalf("还没算完：%s", b.Data)
	}
	e.ranks.err = errs.ErrUnavailable
	expect(t, e.do("rkeditor", "POST", "/api/admin/ranks/recompute", ""), 503, "UNAVAILABLE")
}

func TestRankingConfig(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("rkviewer", "GET", "/api/admin/ranking-config", ""), 200, "OK")
	if string(b.Data) != `{"play":1,"follow":3,"like":2,"finish":2}` {
		t.Fatalf("系数：%s", b.Data)
	}
	expect(t, e.do("rkeditor", "PUT", "/api/admin/ranking-config", `{"play":1.5,"follow":0,"like":2,"finish":0.25}`), 200, "OK")
	if w := e.ranks.last().(dom.RankWeights); w != (dom.RankWeights{Play: 1.5, Follow: 0, Like: 2, Finish: 0.25}) {
		t.Fatalf("请求转为系数：%+v", w)
	}
	if a := e.audit.last(t); a.Action != "ranking_config.update" || a.TargetType != "site_config" || a.TargetID != "ranking" ||
		!strings.Contains(string(a.Before), `"follow":3`) || !strings.Contains(string(a.After), `"finish":0.25`) {
		t.Fatalf("审计带修改前后：%+v", a)
	}
	// 四个系数都要传，缺了不当作 0
	expect(t, e.do("rkeditor", "PUT", "/api/admin/ranking-config", `{"play":1,"follow":3,"like":2}`), 400, "BAD_REQUEST")
	expect(t, e.do("rkeditor", "PUT", "/api/admin/ranking-config", `{"play":1,"follow":3,"like":2,"finish":2,"share":1}`), 400, "BAD_REQUEST")
}
