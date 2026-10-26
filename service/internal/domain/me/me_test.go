package me

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"xsite/service/internal/errs"
)

type fakeRepo struct {
	addFollow, removeFollow, addLike, removeLike int
	paused                                       bool
	upserted                                     []HistoryRow
	listed                                       []HistoryView
	deleted                                      map[int64][]int64
	deletedAll                                   []int64
	notifs                                       []Notification
	notifBefore                                  int64
	notifLimit                                   int
	markedRead                                   int
	unread                                       int
	follows                                      []FollowView
	followLimit                                  int
	err                                          error
}

func (f *fakeRepo) AddFollow(context.Context, int64, int64) error    { f.addFollow++; return f.err }
func (f *fakeRepo) RemoveFollow(context.Context, int64, int64) error { f.removeFollow++; return f.err }
func (f *fakeRepo) AddLike(context.Context, int64, string, int64) error {
	f.addLike++
	return f.err
}
func (f *fakeRepo) RemoveLike(context.Context, int64, string, int64) error {
	f.removeLike++
	return f.err
}
func (f *fakeRepo) HistoryPaused(context.Context, int64) (bool, error) { return f.paused, f.err }
func (f *fakeRepo) UpsertHistory(_ context.Context, rows []HistoryRow) error {
	f.upserted = append(f.upserted, rows...)
	return f.err
}
func (f *fakeRepo) ListHistory(context.Context, int64, int) ([]HistoryView, error) {
	return f.listed, f.err
}
func (f *fakeRepo) ListFollows(_ context.Context, _ int64, limit int) ([]FollowView, error) {
	f.followLimit = limit
	return f.follows, f.err
}
func (f *fakeRepo) DeleteHistory(_ context.Context, uid int64, dramaIDs []int64) error {
	if f.deleted == nil {
		f.deleted = map[int64][]int64{}
	}
	f.deleted[uid] = append(f.deleted[uid], dramaIDs...)
	return f.err
}
func (f *fakeRepo) DeleteAllHistory(_ context.Context, uid int64) error {
	f.deletedAll = append(f.deletedAll, uid)
	return f.err
}

func (f *fakeRepo) ListNotifications(_ context.Context, uid, before int64, limit int) ([]Notification, error) {
	f.notifBefore, f.notifLimit = before, limit
	return f.notifs, f.err
}
func (f *fakeRepo) MarkAllNotificationsRead(context.Context, int64) (int, error) {
	f.markedRead++
	return f.markedRead, f.err
}
func (f *fakeRepo) CountUnreadNotifications(context.Context, int64) (int, error) {
	return f.unread, f.err
}

type fakeHistory struct {
	reports    []PendingHistory
	taken      []PendingHistory
	doneToken  string
	cleared    map[int64][]int64
	clearedAll []int64
	err        error
}

func (f *fakeHistory) Report(_ context.Context, uid, dramaID int64, payload string) error {
	if f.err != nil {
		return f.err
	}
	f.reports = append(f.reports, PendingHistory{UserID: uid, DramaID: dramaID, Payload: payload})
	return nil
}
func (f *fakeHistory) TakePending(context.Context, string) ([]PendingHistory, error) {
	return f.taken, f.err
}
func (f *fakeHistory) DonePending(_ context.Context, token string) error {
	f.doneToken = token
	return nil
}
func (f *fakeHistory) Clear(_ context.Context, uid int64, dramaIDs []int64) error {
	if f.cleared == nil {
		f.cleared = map[int64][]int64{}
	}
	f.cleared[uid] = append(f.cleared[uid], dramaIDs...)
	return f.err
}
func (f *fakeHistory) ClearAll(_ context.Context, uid int64) error {
	f.clearedAll = append(f.clearedAll, uid)
	return f.err
}

func TestFollow_Validation(t *testing.T) {
	f := &fakeRepo{}
	svc := NewService(f, &fakeHistory{})
	ctx := context.Background()
	if err := svc.Follow(ctx, 1, 0); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("剧 ID 0 应 400，得到 %v", err)
	}
	if err := svc.Follow(ctx, 1, 5); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unfollow(ctx, 1, 5); err != nil {
		t.Fatal(err)
	}
	if f.addFollow != 1 || f.removeFollow != 1 {
		t.Fatalf("应各调用一次：add=%d remove=%d", f.addFollow, f.removeFollow)
	}
}

func TestLike_Validation(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeHistory{})
	ctx := context.Background()
	for _, tt := range []struct {
		typ string
		id  int64
	}{
		{"comment", 5}, // 非法类型
		{"drama", 0},   // 非法 ID
		{"", 5},
	} {
		if err := svc.Like(ctx, 1, tt.typ, tt.id); !errors.Is(err, errs.ErrBadRequest) {
			t.Errorf("Like(%q,%d) 应 400，得到 %v", tt.typ, tt.id, err)
		}
	}
	if err := svc.Like(ctx, 1, "drama", 5); err != nil {
		t.Fatal(err)
	}
	if err := svc.Like(ctx, 1, "episode", 9); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unlike(ctx, 1, "drama", 5); err != nil {
		t.Fatal(err)
	}
	if err := svc.Unlike(ctx, 1, "bad", 5); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("取消点赞的非法类型也应 400")
	}
}

func TestReportHistory_PausedSkips(t *testing.T) {
	repo := &fakeRepo{paused: true}
	hist := &fakeHistory{}
	svc := NewService(repo, hist)
	err := svc.ReportHistory(context.Background(), 1, []HistoryEntry{{DramaID: 2, EpisodeID: 3, PositionSec: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hist.reports) != 0 {
		t.Fatalf("暂停记录时不应写入，得到 %d 条", len(hist.reports))
	}
}

func TestReportHistory_BatchLimit(t *testing.T) {
	svc := NewService(&fakeRepo{}, &fakeHistory{})
	entries := make([]HistoryEntry, maxHistoryBatch+1)
	if err := svc.ReportHistory(context.Background(), 1, entries); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("超量上报应 400，得到 %v", err)
	}
}

func TestReportHistory_SkipsInvalid(t *testing.T) {
	hist := &fakeHistory{}
	svc := NewService(&fakeRepo{}, hist)
	entries := []HistoryEntry{
		{DramaID: 0, EpisodeID: 3, PositionSec: 1},                                     // 剧 ID 非法
		{DramaID: 2, EpisodeID: 0, PositionSec: 1},                                     // 分集 ID 非法
		{DramaID: 2, EpisodeID: 3, PositionSec: -1},                                    // 进度非法
		{DramaID: 5, EpisodeID: 7, EpisodeNumber: 2, PositionSec: 30, DurationSec: 90}, // 有效
	}
	if err := svc.ReportHistory(context.Background(), 1, entries); err != nil {
		t.Fatal(err)
	}
	if len(hist.reports) != 1 {
		t.Fatalf("只应写入 1 条有效记录，得到 %d", len(hist.reports))
	}
	r := hist.reports[0]
	if r.UserID != 1 || r.DramaID != 5 {
		t.Fatalf("写入的记录字段不对：%+v", r)
	}
	var hp historyPayload
	if err := json.Unmarshal([]byte(r.Payload), &hp); err != nil {
		t.Fatal(err)
	}
	if hp.EpisodeID != 7 || hp.EpisodeNumber != 2 || hp.PositionSec != 30 || hp.DurationSec != 90 || hp.TS == 0 {
		t.Fatalf("payload 解析不对：%+v", hp)
	}
}

func TestFlushHistory_ParsesAndUpserts(t *testing.T) {
	good, _ := json.Marshal(historyPayload{EpisodeID: 7, EpisodeNumber: 2, PositionSec: 30, DurationSec: 90, TS: 1700000000})
	repo := &fakeRepo{}
	hist := &fakeHistory{taken: []PendingHistory{
		{UserID: 1, DramaID: 5, Payload: string(good)},
		{UserID: 1, DramaID: 6, Payload: "{bad json"}, // 坏 payload 跳过
	}}
	svc := NewService(repo, hist)
	n, err := svc.FlushHistory(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("应落库 1 行，得到 %d", n)
	}
	if len(repo.upserted) != 1 {
		t.Fatalf("UpsertHistory 应收到 1 行，得到 %d", len(repo.upserted))
	}
	row := repo.upserted[0]
	if row.UserID != 1 || row.DramaID != 5 || row.EpisodeID != 7 || row.PositionSec != 30 {
		t.Fatalf("落库行字段不对：%+v", row)
	}
	if hist.doneToken != "job-1" {
		t.Fatalf("落库成功后应 DonePending(job-1)，得到 %q", hist.doneToken)
	}
}

func TestFlushHistory_UpsertFailKeepsTaken(t *testing.T) {
	good, _ := json.Marshal(historyPayload{EpisodeID: 7, PositionSec: 30, TS: 1700000000})
	repo := &fakeRepo{err: errors.New("db down")}
	hist := &fakeHistory{taken: []PendingHistory{{UserID: 1, DramaID: 5, Payload: string(good)}}}
	svc := NewService(repo, hist)
	if _, err := svc.FlushHistory(context.Background(), "job-2"); err == nil {
		t.Fatal("UPSERT 失败应返回错误")
	}
	if hist.doneToken != "" {
		t.Fatalf("失败时不应 DonePending，得到 %q", hist.doneToken)
	}
}

func TestDeleteHistory_ClearsCacheAndDB(t *testing.T) {
	repo := &fakeRepo{}
	hist := &fakeHistory{}
	svc := NewService(repo, hist)
	if err := svc.DeleteHistory(context.Background(), 1, []int64{5, 6}); err != nil {
		t.Fatal(err)
	}
	if got := hist.cleared[1]; len(got) != 2 {
		t.Fatalf("应清 Valkey 2 条，得到 %v", got)
	}
	if got := repo.deleted[1]; len(got) != 2 {
		t.Fatalf("应删 DB 2 条，得到 %v", got)
	}
	// 空列表应直接返回，不触碰 Valkey / DB。
	hist2, repo2 := &fakeHistory{}, &fakeRepo{}
	svc2 := NewService(repo2, hist2)
	if err := svc2.DeleteHistory(context.Background(), 1, nil); err != nil {
		t.Fatal(err)
	}
	if len(hist2.cleared) != 0 || len(repo2.deleted) != 0 {
		t.Fatal("空列表删历史应为空操作")
	}
}

func TestListNotifications_ClampsLimit(t *testing.T) {
	f := &fakeRepo{}
	svc := NewService(f, &fakeHistory{})
	ctx := context.Background()
	// 超上限或非正数都收敛到 notificationListLimit
	if _, err := svc.ListNotifications(ctx, 1, 0, 9999); err != nil {
		t.Fatal(err)
	}
	if f.notifLimit != notificationListLimit {
		t.Fatalf("超上限应收敛为 %d，得到 %d", notificationListLimit, f.notifLimit)
	}
	if _, err := svc.ListNotifications(ctx, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	if f.notifLimit != notificationListLimit {
		t.Fatalf("非正数应收敛为 %d，得到 %d", notificationListLimit, f.notifLimit)
	}
	// 负游标归零
	if _, err := svc.ListNotifications(ctx, 1, -5, 10); err != nil {
		t.Fatal(err)
	}
	if f.notifBefore != 0 || f.notifLimit != 10 {
		t.Fatalf("负游标应归零、正常上限透传：before=%d limit=%d", f.notifBefore, f.notifLimit)
	}
}

func TestClearHistory_ClearsAll(t *testing.T) {
	repo := &fakeRepo{}
	hist := &fakeHistory{}
	svc := NewService(repo, hist)
	if err := svc.ClearHistory(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if len(hist.clearedAll) != 1 || hist.clearedAll[0] != 1 {
		t.Fatalf("应 ClearAll(1)，得到 %v", hist.clearedAll)
	}
	if len(repo.deletedAll) != 1 || repo.deletedAll[0] != 1 {
		t.Fatalf("应 DeleteAllHistory(1)，得到 %v", repo.deletedAll)
	}
}

func TestListFollows(t *testing.T) {
	t.Parallel()
	score := 9.1
	repo := &fakeRepo{follows: []FollowView{
		{ID: 7, Title: "甲", LatestEpisode: 20, WatchedEpisode: 3, Score: &score},
		{ID: 8, Title: "乙", LatestEpisode: 5},
	}}
	list, err := NewService(repo, &fakeHistory{}).ListFollows(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != 7 || list[0].WatchedEpisode != 3 {
		t.Fatalf("按仓储顺序原样返回：%+v", list)
	}
	// 没看过的剧 watched_episode 为 0，前台据此显示「还没看」
	if list[1].WatchedEpisode != 0 {
		t.Fatalf("未观看应为 0：%+v", list[1])
	}
	if repo.followLimit != followListLimit {
		t.Fatalf("应带上条数上限 %d，得到 %d", followListLimit, repo.followLimit)
	}
}

func TestListFollows_RepoError(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{err: errors.New("boom")}
	if _, err := NewService(repo, &fakeHistory{}).ListFollows(context.Background(), 1); err == nil {
		t.Fatal("仓储出错应把错误带出去")
	}
}
