package community

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	usr "xsite/service/internal/domain/user"
	"xsite/service/internal/errs"
)

// fakeUsers 记录调用并返回可控结果（用户管理，§26）。
type fakeUsers struct {
	lastUserFilter  usr.AdminFilter
	lastGuestFilter usr.AdminFilter
	statusID        int64
	statusValue     string
	statusErr       error
	getErr          error
}

func newFakeUsers() *fakeUsers { return &fakeUsers{} }

// mustJSON 把审计的 Before / After 序列化出来做断言。
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return b
}

// doNoCSRF 发一个带会话但不带 CSRF 头的写请求（env.do 会自动补头，这里要的就是缺头）。
func (e *env) doNoCSRF(as, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("User-Agent", "Test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", origin)
	req.AddCookie(&http.Cookie{Name: "xs_admin", Value: as})
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func (f *fakeUsers) ListUsers(_ context.Context, filter usr.AdminFilter) (usr.Page[usr.AdminUser], error) {
	f.lastUserFilter = filter
	verified := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	return usr.Page[usr.AdminUser]{
		List: []usr.AdminUser{
			{ID: 7, Username: "alice", Nickname: "爱丽丝", Status: usr.StatusNormal, Locale: "zh-CN", VerifiedAt: &verified},
			{ID: 6, Username: "bob", Nickname: "Bob", Status: usr.StatusBanned},
		},
		Total: 2, Page: 1, PageSize: 20,
	}, nil
}

func (f *fakeUsers) ListGuests(_ context.Context, filter usr.AdminFilter) (usr.Page[usr.Guest], error) {
	f.lastGuestFilter = filter
	uid := int64(7)
	return usr.Page[usr.Guest]{
		List: []usr.Guest{
			{ID: "018f2a00-0000-7000-8000-000000000001", Platform: "web", SourceChannel: "tg", AttestLevel: "none"},
			{ID: "018f2a00-0000-7000-8000-000000000002", Platform: "ios", MergedUserID: &uid, MergedUsername: "alice"},
		},
		Total: 2, Page: 1, PageSize: 20,
	}, nil
}

func (f *fakeUsers) GetUser(_ context.Context, id int64) (usr.AdminUserDetail, error) {
	if f.getErr != nil {
		return usr.AdminUserDetail{}, f.getErr
	}
	return usr.AdminUserDetail{
		AdminUser:      usr.AdminUser{ID: id, Username: "alice", Nickname: "爱丽丝", Status: usr.StatusNormal},
		NicknameStatus: "ok", BioStatus: "pending", AvatarStatus: "ok", HistoryPaused: true,
		Stats: usr.AdminUserStats{CommentCount: 12, DanmakuCount: 3, ReportSent: 1, ReportAgainst: 4, FollowCount: 9, HistoryCount: 20, SessionCount: 2},
	}, nil
}

func (f *fakeUsers) SetStatus(_ context.Context, id int64, status string) (usr.AdminUser, error) {
	if f.statusErr != nil {
		return usr.AdminUser{}, f.statusErr
	}
	f.statusID, f.statusValue = id, status
	return usr.AdminUser{ID: id, Username: "alice", Status: usr.Status(status)}, nil
}

func TestUserPermissions(t *testing.T) {
	e := newEnv()
	for _, tt := range []struct {
		as, method, path, body string
		want                   int
	}{
		{"viewer", "GET", "/api/admin/users", "", 200},
		{"viewer", "GET", "/api/admin/users/7", "", 200},
		{"viewer", "GET", "/api/admin/guests", "", 200},
		// 只读角色不能改状态
		{"viewer", "POST", "/api/admin/users/7/status", `{"status":"banned"}`, 403},
		{"editor", "POST", "/api/admin/users/7/status", `{"status":"banned"}`, 200},
		{"root", "POST", "/api/admin/users/7/status", `{"status":"muted"}`, 200},
		// 游客列表共用 users 权限点
		{"nobody", "GET", "/api/admin/guests", "", 403},
		{"nobody", "GET", "/api/admin/users", "", 403},
		{"", "GET", "/api/admin/users", "", 401},
		{"", "POST", "/api/admin/users/7/status", `{"status":"banned"}`, 401},
	} {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}
}

func TestListUsersHandler(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("viewer", "GET", "/api/admin/users?q=%E7%88%B1&status=normal&page=2&page_size=10", ""), 200, "OK")
	f := e.usr.lastUserFilter
	if f.Q != "爱" || f.Status != "normal" || f.Page != 2 || f.PageSize != 10 {
		t.Fatalf("查询参数应交给服务：%+v", f)
	}
	if !strings.Contains(string(b.Data), `"username":"alice"`) || !strings.Contains(string(b.Data), `"status":"banned"`) {
		t.Fatalf("列表数据不对：%s", b.Data)
	}
	// 未验证的用户 verified_at 为 null，前端按可空处理
	if !strings.Contains(string(b.Data), `"verified_at":null`) {
		t.Fatalf("verified_at 应可空：%s", b.Data)
	}
	// 后台任何用户接口都不下发密码哈希
	if strings.Contains(string(b.Data), "password") {
		t.Fatalf("列表不应出现密码字段：%s", b.Data)
	}
	// 分页参数不是数字 → 400
	expect(t, e.do("viewer", "GET", "/api/admin/users?page=abc", ""), 400, "BAD_REQUEST")
}

func TestListGuestsHandler(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("viewer", "GET", "/api/admin/guests?q=alice&page=1&page_size=20", ""), 200, "OK")
	if e.usr.lastGuestFilter.Q != "alice" {
		t.Fatalf("搜索词应交给服务：%+v", e.usr.lastGuestFilter)
	}
	if !strings.Contains(string(b.Data), `"platform":"ios"`) || !strings.Contains(string(b.Data), `"merged_username":"alice"`) {
		t.Fatalf("游客列表数据不对：%s", b.Data)
	}
}

func TestGetUserHandler(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("viewer", "GET", "/api/admin/users/7", ""), 200, "OK")
	for _, want := range []string{`"bio_status":"pending"`, `"history_paused":true`, `"report_against":4`, `"session_count":2`} {
		if !strings.Contains(string(b.Data), want) {
			t.Fatalf("详情缺 %s：%s", want, b.Data)
		}
	}
	// 非正整数 id → 404
	expect(t, e.do("viewer", "GET", "/api/admin/users/abc", ""), 404, "NOT_FOUND")
	e.usr.getErr = errs.ErrNotFound
	expect(t, e.do("viewer", "GET", "/api/admin/users/404", ""), 404, "NOT_FOUND")
}

func TestSetUserStatusHandler(t *testing.T) {
	e := newEnv()
	expect(t, e.do("editor", "POST", "/api/admin/users/7/status", `{"status":"banned"}`), 200, "OK")
	if e.usr.statusID != 7 || e.usr.statusValue != "banned" {
		t.Fatalf("入参应交给服务：id=%d status=%q", e.usr.statusID, e.usr.statusValue)
	}
	// 审计要能看出改前改后，否则事后查不出谁在什么时候封了谁
	a := e.audit.last(t)
	if a.Action != "user.status" || a.TargetType != "user" || a.TargetID != "7" {
		t.Fatalf("改状态审计：%+v", a)
	}
	if !strings.Contains(string(mustJSON(t, a.Before)), "normal") || !strings.Contains(string(mustJSON(t, a.After)), "banned") {
		t.Fatalf("审计应含改前改后状态：before=%v after=%v", a.Before, a.After)
	}
	// 未知字段 400
	expect(t, e.do("editor", "POST", "/api/admin/users/7/status", `{"status":"banned","extra":1}`), 400, "BAD_REQUEST")
	// 非正整数 id → 404
	expect(t, e.do("editor", "POST", "/api/admin/users/abc/status", `{"status":"banned"}`), 404, "NOT_FOUND")
	// 领域层拒绝的状态原样返回 400
	e.usr.statusErr = errs.ErrBadRequest.WithMessage("账号状态只能是 normal、muted 或 banned")
	expect(t, e.do("editor", "POST", "/api/admin/users/7/status", `{"status":"deleted"}`), 400, "BAD_REQUEST")
}

func TestSetUserStatusNoCSRF(t *testing.T) {
	e := newEnv()
	// 不带 CSRF 头的写操作必须被挡下
	req := e.do("editor", "GET", "/api/admin/users", "") // 先确认角色本身可用
	if req.Code != 200 {
		t.Fatalf("准备步骤失败：%d", req.Code)
	}
	w := e.doNoCSRF("editor", "POST", "/api/admin/users/7/status", `{"status":"banned"}`)
	if w.Code != 403 {
		t.Fatalf("缺少 CSRF 头应 403，得到 %d %s", w.Code, w.Body.String())
	}
}
