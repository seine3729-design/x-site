package me

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	meDom "xsite/service/internal/domain/me"
	"xsite/service/internal/domain/user"
	"xsite/service/internal/errs"
)

type fakeMe struct {
	followed, unfollowed []int64
	liked, unliked       []string
	reported             []meDom.HistoryEntry
	history              []meDom.HistoryView
	deleted              [][]int64
	cleared              int
	notifs               []meDom.Notification
	notifBefore          int64
	notifLimit           int
	markedRead           int
	unread               int
	follows              []meDom.FollowView
	err                  error
}

func (f *fakeMe) Follow(_ context.Context, _, dramaID int64) error {
	f.followed = append(f.followed, dramaID)
	return f.err
}
func (f *fakeMe) ListFollows(context.Context, int64) ([]meDom.FollowView, error) {
	return f.follows, f.err
}
func (f *fakeMe) Unfollow(_ context.Context, _, dramaID int64) error {
	f.unfollowed = append(f.unfollowed, dramaID)
	return f.err
}
func (f *fakeMe) Like(_ context.Context, _ int64, typ string, id int64) error {
	f.liked = append(f.liked, fmt.Sprintf("%s:%d", typ, id))
	return f.err
}
func (f *fakeMe) Unlike(_ context.Context, _ int64, typ string, id int64) error {
	f.unliked = append(f.unliked, fmt.Sprintf("%s:%d", typ, id))
	return f.err
}
func (f *fakeMe) ReportHistory(_ context.Context, _ int64, e []meDom.HistoryEntry) error {
	f.reported = e
	return f.err
}
func (f *fakeMe) ListHistory(context.Context, int64) ([]meDom.HistoryView, error) {
	return f.history, f.err
}
func (f *fakeMe) DeleteHistory(_ context.Context, _ int64, ids []int64) error {
	f.deleted = append(f.deleted, ids)
	return f.err
}
func (f *fakeMe) ClearHistory(context.Context, int64) error { f.cleared++; return f.err }
func (f *fakeMe) ListNotifications(_ context.Context, _, before int64, limit int) ([]meDom.Notification, error) {
	f.notifBefore, f.notifLimit = before, limit
	return f.notifs, f.err
}
func (f *fakeMe) MarkAllNotificationsRead(context.Context, int64) (int, error) {
	f.markedRead++
	return f.markedRead, f.err
}
func (f *fakeMe) UnreadCount(context.Context, int64) (int, error) { return f.unread, f.err }

type fakeProfiles struct {
	profile user.Profile
	gotUpd  user.ProfileUpdate
	err     error
}

func (f *fakeProfiles) GetProfile(context.Context, int64) (user.Profile, error) {
	return f.profile, f.err
}
func (f *fakeProfiles) UpdateProfile(_ context.Context, _ int64, upd user.ProfileUpdate) (user.Profile, error) {
	f.gotUpd = upd
	if f.err != nil {
		return user.Profile{}, f.err
	}
	return f.profile, nil
}

func newS23Handlers(m *fakeMe, p *fakeProfiles) *Handlers {
	return &Handlers{
		Accounts: &fakeAccounts{}, Sessions: &fakeSessions{}, Me: m, Profiles: p,
		Resolver: fakeResolver{access: userAccess("s")},
	}
}

func TestFollowLike_HTTP(t *testing.T) {
	m := &fakeMe{}
	h := newS23Handlers(m, &fakeProfiles{})

	if w := do(t, h, http.MethodPost, "/api/v1/me/follow", `{"drama_id":42}`, "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("收藏应 204，得到 %d %s", w.Code, w.Body.String())
	}
	if len(m.followed) != 1 || m.followed[0] != 42 {
		t.Fatalf("应收藏剧 42，得到 %v", m.followed)
	}
	if w := do(t, h, http.MethodDelete, "/api/v1/me/follow?drama_id=42", "", "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("取消收藏应 204，得到 %d", w.Code)
	}
	if len(m.unfollowed) != 1 || m.unfollowed[0] != 42 {
		t.Fatalf("应取消收藏剧 42，得到 %v", m.unfollowed)
	}
	// 缺 drama_id → 400
	if w := do(t, h, http.MethodDelete, "/api/v1/me/follow", "", "tok"); w.Code != http.StatusBadRequest {
		t.Fatalf("缺 drama_id 应 400，得到 %d", w.Code)
	}

	if w := do(t, h, http.MethodPost, "/api/v1/me/like", `{"target_type":"drama","target_id":7}`, "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("点赞应 204，得到 %d %s", w.Code, w.Body.String())
	}
	if len(m.liked) != 1 || m.liked[0] != "drama:7" {
		t.Fatalf("应点赞 drama:7，得到 %v", m.liked)
	}
	if w := do(t, h, http.MethodDelete, "/api/v1/me/like?type=episode&id=9", "", "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("取消点赞应 204，得到 %d", w.Code)
	}
	if len(m.unliked) != 1 || m.unliked[0] != "episode:9" {
		t.Fatalf("应取消点赞 episode:9，得到 %v", m.unliked)
	}
}

func TestHistory_HTTP(t *testing.T) {
	m := &fakeMe{history: []meDom.HistoryView{{DramaID: 1, Title: "剧一"}}}
	h := newS23Handlers(m, &fakeProfiles{})

	// 列表
	w := do(t, h, http.MethodGet, "/api/v1/me/history", "", "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("历史列表应 200，得到 %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Items []struct {
				DramaID int64  `json:"drama_id"`
				Title   string `json:"title"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].Title != "剧一" {
		t.Fatalf("历史列表内容不对：%s", w.Body.String())
	}

	// 批量上报
	if w := do(t, h, http.MethodPost, "/api/v1/me/history",
		`{"items":[{"drama_id":1,"episode_id":10,"episode_number":2,"position_sec":30,"duration_sec":90}]}`, "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("上报应 204，得到 %d %s", w.Code, w.Body.String())
	}
	if len(m.reported) != 1 || m.reported[0].EpisodeID != 10 || m.reported[0].PositionSec != 30 {
		t.Fatalf("上报映射不对：%+v", m.reported)
	}

	// 删除指定
	if w := do(t, h, http.MethodDelete, "/api/v1/me/history?drama_ids=1,2,3", "", "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("删除应 204，得到 %d", w.Code)
	}
	if len(m.deleted) != 1 || len(m.deleted[0]) != 3 {
		t.Fatalf("应删 3 部，得到 %v", m.deleted)
	}
	// 清空
	if w := do(t, h, http.MethodDelete, "/api/v1/me/history?all=1", "", "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("清空应 204，得到 %d", w.Code)
	}
	if m.cleared != 1 {
		t.Fatalf("应清空一次，得到 %d", m.cleared)
	}
	// 既无 all 也无 drama_ids → 400
	if w := do(t, h, http.MethodDelete, "/api/v1/me/history", "", "tok"); w.Code != http.StatusBadRequest {
		t.Fatalf("缺参数应 400，得到 %d", w.Code)
	}
}

func TestNotifications_HTTP(t *testing.T) {
	m := &fakeMe{notifs: []meDom.Notification{{ID: 5, Type: "reply"}}}
	h := newS23Handlers(m, &fakeProfiles{})

	w := do(t, h, http.MethodGet, "/api/v1/me/notifications?before=100&limit=20", "", "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("消息列表应 200，得到 %d %s", w.Code, w.Body.String())
	}
	if m.notifBefore != 100 || m.notifLimit != 20 {
		t.Fatalf("游标透传不对：before=%d limit=%d", m.notifBefore, m.notifLimit)
	}
	if w := do(t, h, http.MethodPost, "/api/v1/me/notifications", "", "tok"); w.Code != http.StatusNoContent {
		t.Fatalf("全部已读应 204，得到 %d", w.Code)
	}
	if m.markedRead != 1 {
		t.Fatalf("应标记已读一次，得到 %d", m.markedRead)
	}
}

func TestProfileAndState_HTTP(t *testing.T) {
	p := &fakeProfiles{profile: user.Profile{Nickname: "小明", Avatar: "a.png", Bio: "你好", NicknameStatus: "ok"}}
	m := &fakeMe{unread: 3}
	h := newS23Handlers(m, p)

	// GET 资料
	w := do(t, h, http.MethodGet, "/api/v1/me/profile", "", "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("读资料应 200，得到 %d %s", w.Code, w.Body.String())
	}
	var pb struct {
		Data user.Profile `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &pb); err != nil {
		t.Fatal(err)
	}
	if pb.Data.Nickname != "小明" || pb.Data.NicknameStatus != "ok" {
		t.Fatalf("资料内容不对：%s", w.Body.String())
	}

	// PATCH 资料：只传 bio，指针语义
	if w := do(t, h, http.MethodPatch, "/api/v1/me/profile", `{"bio":"新简介"}`, "tok"); w.Code != http.StatusOK {
		t.Fatalf("改资料应 200，得到 %d %s", w.Code, w.Body.String())
	}
	if p.gotUpd.Bio == nil || *p.gotUpd.Bio != "新简介" || p.gotUpd.Nickname != nil || p.gotUpd.Avatar != nil {
		t.Fatalf("PATCH 指针语义不对：%+v", p.gotUpd)
	}

	// state：身份 + 未读数
	w = do(t, h, http.MethodGet, "/api/v1/me/state", "", "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("state 应 200，得到 %d %s", w.Code, w.Body.String())
	}
	var sb struct {
		Data struct {
			User struct {
				ID       int64  `json:"id"`
				Nickname string `json:"nickname"`
			} `json:"user"`
			Unread int `json:"unread"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &sb); err != nil {
		t.Fatal(err)
	}
	if sb.Data.User.ID != 7 || sb.Data.User.Nickname != "小明" || sb.Data.Unread != 3 {
		t.Fatalf("state 内容不对：%s", w.Body.String())
	}
}

// 领域校验错误（如点赞对象无效）映射为 400。
func TestInteract_DomainError(t *testing.T) {
	m := &fakeMe{err: errs.ErrBadRequest.WithMessage("点赞对象无效")}
	h := newS23Handlers(m, &fakeProfiles{})
	if w := do(t, h, http.MethodPost, "/api/v1/me/like", `{"target_type":"comment","target_id":1}`, "tok"); w.Code != http.StatusBadRequest || codeOf(t, w) != "BAD_REQUEST" {
		t.Fatalf("领域 400 应透传，得到 %d %s", w.Code, w.Body.String())
	}
}

// 新增 /me/* 路由同样受 RequireUser 保护。
func TestS23_RequireUser(t *testing.T) {
	h := &Handlers{Accounts: &fakeAccounts{}, Sessions: &fakeSessions{}, Me: &fakeMe{}, Profiles: &fakeProfiles{}, Resolver: fakeResolver{}}
	for _, path := range []string{"/api/v1/me/state", "/api/v1/me/profile", "/api/v1/me/history", "/api/v1/me/notifications"} {
		if w := do(t, h, http.MethodGet, path, "", ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("%s 未登录应 401，得到 %d", path, w.Code)
		}
	}
}

func TestListFollows_HTTP(t *testing.T) {
	score := 9.1
	year := int32(2025)
	m := &fakeMe{follows: []meDom.FollowView{
		{ID: 7, Title: "剧甲", Slug: "ju-jia", CoverPortrait: "img/a.jpg", LatestEpisode: 20, WatchedEpisode: 3, Score: &score, Year: &year},
		{ID: 8, Title: "剧乙", LatestEpisode: 5},
	}}
	h := newS23Handlers(m, &fakeProfiles{})

	w := do(t, h, http.MethodGet, "/api/v1/me/follows", "", "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("追剧列表应 200，得到 %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != noStore {
		t.Fatalf("个人数据不能被缓存，Cache-Control=%q", got)
	}
	var body struct {
		Code string `json:"code"`
		Data struct {
			List []struct {
				ID             int64    `json:"id"`
				Title          string   `json:"title"`
				Slug           string   `json:"slug"`
				CoverPortrait  string   `json:"cover_portrait"`
				LatestEpisode  int32    `json:"latest_episode"`
				WatchedEpisode int32    `json:"watched_episode"`
				Score          *float64 `json:"score"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "OK" || len(body.Data.List) != 2 {
		t.Fatalf("业务码与条数不对：%s", w.Body.String())
	}
	first := body.Data.List[0]
	if first.ID != 7 || first.Title != "剧甲" || first.Slug != "ju-jia" || first.CoverPortrait != "img/a.jpg" {
		t.Fatalf("卡片字段不对：%+v", first)
	}
	if first.LatestEpisode != 20 || first.WatchedEpisode != 3 || first.Score == nil || *first.Score != 9.1 {
		t.Fatalf("进度与评分字段不对：%+v", first)
	}
	if body.Data.List[1].WatchedEpisode != 0 {
		t.Fatalf("没看过的剧应为 0：%+v", body.Data.List[1])
	}
}

func TestListFollows_HTTP_Empty(t *testing.T) {
	h := newS23Handlers(&fakeMe{}, &fakeProfiles{})
	w := do(t, h, http.MethodGet, "/api/v1/me/follows", "", "tok")
	if w.Code != http.StatusOK {
		t.Fatalf("空列表也应 200，得到 %d", w.Code)
	}
	// 空列表下发 []，不是 null（前台直接 .map）
	if !strings.Contains(w.Body.String(), `"list":[]`) {
		t.Fatalf("空列表应为 []：%s", w.Body.String())
	}
}
