package user

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

// 后台用户管理（S2-7 / A2-2）单测：筛选条件归一、分页边界、改状态的会话吊销。
// 重点盯住封禁的即时生效 —— 这是 docs/11 S2-7 的验收标准，也是本次要修的缺陷：
// 改状态前封禁只挡登录，手上的 Access Token 与 Refresh 族都还活着。

type fakeAdminRepo struct {
	users     map[int64]*AdminUser
	lastQuery AdminQuery
	listErr   error
	setErr    error
}

func newFakeAdminRepo() *fakeAdminRepo {
	return &fakeAdminRepo{users: map[int64]*AdminUser{
		1: {ID: 1, Username: "alice", Nickname: "爱丽丝", Status: StatusNormal, CreatedAt: time.Now()},
		2: {ID: 2, Username: "bob", Nickname: "Bob", Status: StatusMuted, CreatedAt: time.Now()},
	}}
}

func (r *fakeAdminRepo) ListUsers(_ context.Context, q AdminQuery) ([]AdminUser, int64, error) {
	r.lastQuery = q
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	// 故意返回 nil 切片：领域层要把它规范成空数组，否则 JSON 出来是 null
	return nil, 2, nil
}

func (r *fakeAdminRepo) UserDetail(_ context.Context, id int64) (AdminUserDetail, error) {
	u, ok := r.users[id]
	if !ok {
		return AdminUserDetail{}, errs.ErrNotFound
	}
	return AdminUserDetail{AdminUser: *u}, nil
}

func (r *fakeAdminRepo) SetUserStatus(_ context.Context, id int64, status Status) (AdminUser, error) {
	if r.setErr != nil {
		return AdminUser{}, r.setErr
	}
	u, ok := r.users[id]
	if !ok {
		return AdminUser{}, errs.ErrNotFound
	}
	u.Status = status
	return *u, nil
}

func (r *fakeAdminRepo) ListGuests(_ context.Context, q AdminQuery) ([]Guest, int64, error) {
	r.lastQuery = q
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	return nil, 0, nil
}

// fakeRevoker 记录吊销调用，用来断言封禁确实踢人、禁言确实不踢人。
type fakeRevoker struct {
	calls []string // "userID:reason"
	err   error
}

func (f *fakeRevoker) RevokeAllSessions(_ context.Context, userID int64, reason string) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, strconv.FormatInt(userID, 10)+":"+reason)
	return nil
}

func newAdminFixture() (*AdminService, *fakeAdminRepo, *fakeRevoker) {
	repo := newFakeAdminRepo()
	rev := &fakeRevoker{}
	return NewAdminService(repo, rev), repo, rev
}

// ---------- 改状态 ----------

func TestSetStatusBannedRevokesSessions(t *testing.T) {
	svc, repo, rev := newAdminFixture()
	u, err := svc.SetStatus(context.Background(), 1, "banned")
	if err != nil {
		t.Fatalf("封禁失败: %v", err)
	}
	if u.Status != StatusBanned {
		t.Fatalf("状态应为 banned，得到 %q", u.Status)
	}
	if repo.users[1].Status != StatusBanned {
		t.Fatalf("状态未落库")
	}
	// 封禁必须吊销全部会话：Refresh 族 30 天滑动有效，不吊销等于封禁只挡了登录入口
	if len(rev.calls) != 1 || rev.calls[0] != "1:banned_by_admin" {
		t.Fatalf("封禁应吊销全部会话并带原因，实际调用 %v", rev.calls)
	}
}

func TestSetStatusMutedAndNormalKeepSessions(t *testing.T) {
	for _, st := range []string{"muted", "normal"} {
		svc, _, rev := newAdminFixture()
		if _, err := svc.SetStatus(context.Background(), 1, st); err != nil {
			t.Fatalf("改为 %s 失败: %v", st, err)
		}
		// 禁言只限制发言，不该顺带把人踢下线
		if len(rev.calls) != 0 {
			t.Fatalf("改为 %s 不应吊销会话，实际 %v", st, rev.calls)
		}
	}
}

func TestSetStatusRejectsUnknownStatus(t *testing.T) {
	svc, _, rev := newAdminFixture()
	for _, st := range []string{"", "deleted", "BANNED", "normal "} {
		_, err := svc.SetStatus(context.Background(), 1, st)
		if !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("状态 %q 应返回 400，得到 %v", st, err)
		}
	}
	if len(rev.calls) != 0 {
		t.Fatalf("校验失败时不应吊销会话")
	}
}

func TestSetStatusRevokeFailureSurfaces(t *testing.T) {
	repo := newFakeAdminRepo()
	rev := &fakeRevoker{err: errors.New("valkey 挂了")}
	svc := NewAdminService(repo, rev)
	// 状态已落库但没踢下线时必须报错：当成功返回就回到了「封禁半失效」
	if _, err := svc.SetStatus(context.Background(), 1, "banned"); err == nil {
		t.Fatal("吊销会话失败时应返回错误")
	}
}

func TestSetStatusMissingUser(t *testing.T) {
	svc, _, _ := newAdminFixture()
	if _, err := svc.SetStatus(context.Background(), 99, "banned"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("用户不存在应返回 404，得到 %v", err)
	}
	if _, err := svc.SetStatus(context.Background(), 0, "banned"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("非法 ID 应返回 404，得到 %v", err)
	}
}

func TestGetUserRejectsBadID(t *testing.T) {
	svc, _, _ := newAdminFixture()
	if _, err := svc.GetUser(context.Background(), -1); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("非法 ID 应返回 404，得到 %v", err)
	}
}

// ---------- 筛选与搜索归一 ----------

func TestListUsersNormalizesQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},             // 不搜
		{"Alice", "%alice%"}, // 转小写，与 lower(username) 索引对齐
		{"  爱丽丝 ", "%爱丽丝%"},  // 去首尾空白
		{"ＡＬＩＣＥ", "%alice%"}, // 全角转半角
		{"100%", `%100\%%`},  // LIKE 元字符要转义，否则 % 变成通配
		{"a_b", `%a\_b%`},    // _ 同理
		{`c\d`, `%c\\d%`},    // 反斜杠先转义自身
	}
	for _, c := range cases {
		svc, repo, _ := newAdminFixture()
		if _, err := svc.ListUsers(context.Background(), AdminFilter{Q: c.in}); err != nil {
			t.Fatalf("搜 %q 失败: %v", c.in, err)
		}
		if repo.lastQuery.Pattern != c.want {
			t.Fatalf("搜 %q 应生成模式 %q，得到 %q", c.in, c.want, repo.lastQuery.Pattern)
		}
	}
}

func TestListUsersQueryTooLong(t *testing.T) {
	svc, _, _ := newAdminFixture()
	_, err := svc.ListUsers(context.Background(), AdminFilter{Q: strings.Repeat("词", adminMaxQueryRunes+1)})
	if !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("超长搜索词应返回 400，得到 %v", err)
	}
}

func TestListUsersStatusFilter(t *testing.T) {
	svc, repo, _ := newAdminFixture()
	if _, err := svc.ListUsers(context.Background(), AdminFilter{Status: "muted"}); err != nil {
		t.Fatalf("按禁言筛选失败: %v", err)
	}
	if repo.lastQuery.Status != "muted" {
		t.Fatalf("状态筛选未传到仓储，得到 %q", repo.lastQuery.Status)
	}
	if _, err := svc.ListUsers(context.Background(), AdminFilter{Status: "pending"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("未知状态应返回 400")
	}
}

func TestListGuestsRejectsStatusFilter(t *testing.T) {
	svc, _, _ := newAdminFixture()
	// guest_device 没有账号状态列；接受这个参数会让前端以为筛生效了
	if _, err := svc.ListGuests(context.Background(), AdminFilter{Status: "banned"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("游客列表不该接受状态筛选，得到 %v", err)
	}
	if _, err := svc.ListGuests(context.Background(), AdminFilter{Q: "WEB"}); err != nil {
		t.Fatalf("游客列表搜索失败: %v", err)
	}
}

// ---------- 分页 ----------

func TestListUsersPaging(t *testing.T) {
	svc, repo, _ := newAdminFixture()
	// 不传页码时取默认值，并原样回填到响应里
	p, err := svc.ListUsers(context.Background(), AdminFilter{})
	if err != nil {
		t.Fatalf("默认分页失败: %v", err)
	}
	if p.Page != 1 || p.PageSize != adminDefaultPageSize {
		t.Fatalf("默认分页应为 1/%d，得到 %d/%d", adminDefaultPageSize, p.Page, p.PageSize)
	}
	if repo.lastQuery.Limit != adminDefaultPageSize || repo.lastQuery.Offset != 0 {
		t.Fatalf("默认 limit/offset 应为 %d/0，得到 %d/%d", adminDefaultPageSize, repo.lastQuery.Limit, repo.lastQuery.Offset)
	}
	if _, err := svc.ListUsers(context.Background(), AdminFilter{PageRequest: PageRequest{Page: 3, PageSize: 10}}); err != nil {
		t.Fatalf("第 3 页失败: %v", err)
	}
	if repo.lastQuery.Offset != 20 {
		t.Fatalf("第 3 页 offset 应为 20，得到 %d", repo.lastQuery.Offset)
	}
	for _, f := range []AdminFilter{
		{PageRequest: PageRequest{Page: -1}},
		{PageRequest: PageRequest{Page: adminMaxPage + 1}},
		{PageRequest: PageRequest{PageSize: adminMaxPageSize + 1}},
		{PageRequest: PageRequest{PageSize: -5}},
	} {
		if _, err := svc.ListUsers(context.Background(), f); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("越界分页 %+v 应返回 400", f.PageRequest)
		}
	}
}

func TestListSerializesEmptyListAsArray(t *testing.T) {
	svc, _, _ := newAdminFixture()
	p, err := svc.ListUsers(context.Background(), AdminFilter{})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	// 契约声明 list 是数组；nil 切片会出 null，前端对它做 .map 会当场崩页
	if strings.Contains(string(b), "null") {
		t.Fatalf("空列表不应序列化出 null：%s", b)
	}
	if !strings.Contains(string(b), `"list":[]`) {
		t.Fatalf("空列表应为 []：%s", b)
	}
	g, err := svc.ListGuests(context.Background(), AdminFilter{})
	if err != nil {
		t.Fatalf("游客列表失败: %v", err)
	}
	if gb, _ := json.Marshal(g); !strings.Contains(string(gb), `"list":[]`) {
		t.Fatalf("游客空列表应为 []：%s", gb)
	}
}

func TestListUsersPropagatesRepoError(t *testing.T) {
	repo := newFakeAdminRepo()
	repo.listErr = errors.New("db 挂了")
	svc := NewAdminService(repo, &fakeRevoker{})
	if _, err := svc.ListUsers(context.Background(), AdminFilter{}); err == nil {
		t.Fatal("仓储报错应往上传")
	}
	if _, err := svc.ListGuests(context.Background(), AdminFilter{}); err == nil {
		t.Fatal("游客列表的仓储报错应往上传")
	}
}

// ---------- 状态取值 ----------

func TestParseStatus(t *testing.T) {
	for _, s := range []string{"normal", "muted", "banned"} {
		if got, err := ParseStatus(s); err != nil || string(got) != s {
			t.Fatalf("%q 应解析成功，得到 %q / %v", s, got, err)
		}
	}
	for _, s := range []string{"", "ok", "Banned", "mute"} {
		if _, err := ParseStatus(s); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("%q 应返回 400", s)
		}
	}
}
