package ops

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
)

func TestComputeRanking(t *testing.T) {
	signals := []RankSignals{
		{DramaID: 1, Plays: 100},
		{DramaID: 2, Plays: 300},
		{DramaID: 3, Plays: 200, Follows: 10}, // 200 + 10×3 = 230
		{DramaID: 4, Plays: 50, Likes: 5},     // 50 + 5×2 = 60
		{DramaID: 5, Plays: 10, Finishes: 1},  // 10 + 1×2 = 12，置顶
		{DramaID: 6, Plays: 5},                // 置顶，加 10 分 = 15
		{DramaID: 7, Plays: 1000},             // 不让上榜
		{DramaID: 8, Plays: 300},              // 与 2 号同分：编号小的在前
		{DramaID: 9, Plays: 400},              // 减 350 分 = 50
	}
	overrides := []RankOverride{
		{DramaID: 5, Pinned: true},
		{DramaID: 6, Pinned: true, Boost: 10},
		{DramaID: 7, Blocked: true},
		{DramaID: 9, Boost: -350},
		{DramaID: 404, Pinned: true}, // 不在可见的剧里：忽略
	}
	got := ComputeRanking(signals, overrides, DefaultRankWeights, 100)
	ids := []int64{}
	for _, e := range got {
		ids = append(ids, e.DramaID)
	}
	if want := []int64{6, 5, 2, 8, 3, 1, 4, 9}; !slices.Equal(ids, want) {
		t.Fatalf("置顶的在前（之间按分数），其余按分数倒序、同分按编号；不让上榜与不可见的不出现：%v，期望 %v", ids, want)
	}
	if got[0].Score != 15 || !got[0].Pinned || got[1].Score != 12 || got[4].Score != 230 || got[6].Score != 60 || got[7].Score != 50 || got[2].Pinned {
		t.Fatalf("分数与置顶标记：%+v", got)
	}
	if top := ComputeRanking(signals, overrides, DefaultRankWeights, 3); len(top) != 3 || top[2].DramaID != 2 {
		t.Fatalf("只保留前 limit 名：%+v", top)
	}
	// 系数为 0 时该项不计分；小数系数
	w := RankWeights{Play: 0.5, Follow: 0, Like: 0, Finish: 0}
	if s := ComputeRanking([]RankSignals{{DramaID: 1, Plays: 3, Follows: 100}}, nil, w, 10); s[0].Score != 1.5 {
		t.Fatalf("按系数计分：%+v", s)
	}
	if len(ComputeRanking(nil, overrides, DefaultRankWeights, 10)) != 0 {
		t.Fatal("没有可见的剧时为空榜")
	}
}

// ---------- 测试替身 ----------

type fakeRankRepo struct {
	mu        sync.Mutex
	weights   RankWeights
	overrides map[RankBoard][]RankOverride
	signals   []RankSignals
	live      map[int64]bool // 存在且不在回收站的剧
	titles    map[int64]string
	cards     map[int64]content.DramaCard // 前台可见的剧
	cardCalls [][]int64
}

func newFakeRankRepo() *fakeRankRepo {
	return &fakeRankRepo{
		weights:   DefaultRankWeights,
		overrides: map[RankBoard][]RankOverride{},
		live:      map[int64]bool{},
		titles:    map[int64]string{},
		cards:     map[int64]content.DramaCard{},
	}
}

func (r *fakeRankRepo) addDrama(id int64, title string, plays int64, visible bool) {
	r.live[id], r.titles[id] = true, title
	if visible {
		r.signals = append(r.signals, RankSignals{DramaID: id, Plays: plays})
		r.cards[id] = content.DramaCard{ID: id, Title: title, PlayCount: plays}
	}
}

func (r *fakeRankRepo) RankingWeights(context.Context) (RankWeights, error) { return r.weights, nil }

func (r *fakeRankRepo) SetRankingWeights(_ context.Context, w RankWeights) (RankWeights, error) {
	r.weights = w
	return w, nil
}

func (r *fakeRankRepo) RankSignals(context.Context, RankBoard) ([]RankSignals, error) {
	return slices.Clone(r.signals), nil
}

func (r *fakeRankRepo) RankOverrideRules(_ context.Context, b RankBoard) ([]RankOverride, error) {
	return slices.Clone(r.overrides[b]), nil
}

func (r *fakeRankRepo) ListRankOverrides(_ context.Context, b RankBoard) ([]RankOverrideView, error) {
	out := []RankOverrideView{}
	for _, o := range r.overrides[b] {
		out = append(out, RankOverrideView{RankOverride: o, Title: r.titles[o.DramaID], DramaStatus: "published"})
	}
	return out, nil
}

func (r *fakeRankRepo) ReplaceRankOverrides(_ context.Context, b RankBoard, list []RankOverride) error {
	r.overrides[b] = slices.Clone(list)
	return nil
}

func (r *fakeRankRepo) MissingDramas(_ context.Context, ids []int64) ([]int64, error) {
	var out []int64
	for _, id := range ids {
		if !r.live[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

func (r *fakeRankRepo) DramaTitles(_ context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	for _, id := range ids {
		if t, ok := r.titles[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}

func (r *fakeRankRepo) RankCards(_ context.Context, ids []int64) ([]content.DramaCard, error) {
	r.mu.Lock()
	r.cardCalls = append(r.cardCalls, slices.Clone(ids))
	r.mu.Unlock()
	out := []content.DramaCard{}
	for _, id := range ids {
		if c, ok := r.cards[id]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

type fakeRankStore struct {
	mu       sync.Mutex
	boards   map[RankBoard][]RankEntry
	at       map[RankBoard]time.Time
	replaces int
}

func newFakeRankStore() *fakeRankStore {
	return &fakeRankStore{boards: map[RankBoard][]RankEntry{}, at: map[RankBoard]time.Time{}}
}

func (s *fakeRankStore) Replace(_ context.Context, b RankBoard, entries []RankEntry, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaces++
	list := make([]RankEntry, len(entries))
	for i, e := range entries {
		list[i] = RankEntry{DramaID: e.DramaID, Score: e.Score} // 线上榜单不存置顶标记
	}
	s.boards[b], s.at[b] = list, at
	return nil
}

func (s *fakeRankStore) Top(_ context.Context, b RankBoard, n int) ([]RankEntry, time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.at[b]
	if !ok {
		return nil, time.Time{}, false, nil
	}
	list := s.boards[b]
	return slices.Clone(list[:min(n, len(list))]), at, true, nil
}

type fakeFeatures struct{ off map[string]bool }

func (f fakeFeatures) Enabled(_ context.Context, name string) (bool, error) { return !f.off[name], nil }

type fakeRecomputeQueue struct {
	calls int
	done  bool
	err   error
}

func (q *fakeRecomputeQueue) RecomputeNow(context.Context) (bool, error) {
	q.calls++
	return q.done, q.err
}

var rankNow = time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)

func newTestRankService() (*RankService, *fakeRankRepo, *fakeRankStore, *memCache, *fakeRecomputeQueue) {
	repo, store, c, q := newFakeRankRepo(), newFakeRankStore(), newMemCache(), &fakeRecomputeQueue{done: true}
	s := NewRankService(repo, repo, store, c, fakeFeatures{}, q)
	s.now = func() time.Time { return rankNow }
	return s, repo, store, c, q
}

// ---------- 服务 ----------

func TestRankWeights(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _, _ := newTestRankService()
	if w, _ := s.Weights(ctx); w != DefaultRankWeights {
		t.Fatalf("默认系数：%+v", w)
	}
	before, after, err := s.SetWeights(ctx, RankWeights{Play: 1.234, Follow: 0, Like: 2, Finish: 1000})
	if err != nil || before != DefaultRankWeights || after != (RankWeights{Play: 1.23, Follow: 0, Like: 2, Finish: 1000}) || repo.weights != after {
		t.Fatalf("保存系数（保留两位小数）：%+v → %+v %v", before, after, err)
	}
	for _, bad := range []RankWeights{{Play: -1}, {Like: 1000.01}, {Finish: math.NaN()}, {Follow: math.Inf(1)}} {
		_, _, err := s.SetWeights(ctx, bad)
		wantErr(t, err, errs.ErrBadRequest, "系数为 0–1000")
	}
}

func TestRankOverridesValidation(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _, _ := newTestRankService()
	repo.addDrama(1, "霸道总裁", 10, true)
	repo.addDrama(2, "草稿剧", 0, false)

	_, after, err := s.SetOverrides(ctx, BoardDramaWeek, []RankOverride{
		{DramaID: 1, Pinned: true},
		{DramaID: 2, Boost: -5}, // 不可见的剧也可以先调整，上架后生效
		{DramaID: 3, Boost: 7},  // 3 号剧不存在
	})
	if err == nil || !strings.Contains(errs.From(err).Message, "#3") || len(repo.overrides[BoardDramaWeek]) != 0 {
		t.Fatalf("不存在的剧应报出编号且整批不保存：%v", err)
	}
	if _, _, err := s.SetOverrides(ctx, BoardDramaWeek, []RankOverride{{DramaID: 404, Boost: 0}}); err != nil {
		t.Fatalf("什么都没调的行先去掉，不再检查剧是否存在：%v", err)
	}
	_, after, err = s.SetOverrides(ctx, BoardDramaWeek, []RankOverride{{DramaID: 1, Pinned: true}, {DramaID: 2, Boost: -5}, {DramaID: 1}})
	wantErr(t, err, errs.ErrBadRequest, "重复")
	_, after, err = s.SetOverrides(ctx, BoardDramaWeek, []RankOverride{{DramaID: 1, Pinned: true}, {DramaID: 2, Boost: -5}})
	if err != nil || len(after) != 2 || !after[0].Pinned || after[1].Boost != -5 || len(repo.overrides[BoardDramaMonth]) != 0 {
		t.Fatalf("保存后返回新的调整，只影响该榜：%+v %v", after, err)
	}
	before, after, err := s.SetOverrides(ctx, BoardDramaWeek, []RankOverride{{DramaID: 1, Boost: 0}})
	if err != nil || len(before) != 2 || len(after) != 0 {
		t.Fatalf("没有任何调整的行不保存（等于清空）：%+v → %+v %v", before, after, err)
	}

	for name, tt := range map[string]struct {
		board RankBoard
		list  []RankOverride
		code  *errs.Error
		msg   string
	}{
		"未知榜单":    {"tag_week", nil, errs.ErrNotFound, ""},
		"置顶又不让上榜": {BoardDramaWeek, []RankOverride{{DramaID: 1, Pinned: true, Blocked: true}}, errs.ErrBadRequest, "不能同时"},
		"加分太大":    {BoardDramaWeek, []RankOverride{{DramaID: 1, Boost: 1_000_000_001}}, errs.ErrBadRequest, "加分"},
		"编号无效":    {BoardDramaWeek, []RankOverride{{DramaID: 0, Pinned: true}}, errs.ErrBadRequest, "剧集编号"},
		"太多":      {BoardDramaWeek, slices.Repeat([]RankOverride{{DramaID: 1, Pinned: true}}, maxRankOverrides+1), errs.ErrBadRequest, "最多"},
	} {
		_, _, err := s.SetOverrides(ctx, tt.board, tt.list)
		if err == nil {
			t.Errorf("%s：应拒绝", name)
			continue
		}
		if e := errs.From(err); e.Code != tt.code.Code || !strings.Contains(e.Message, tt.msg) {
			t.Errorf("%s：期望 %s「%s」，得到 %s「%s」", name, tt.code.Code, tt.msg, e.Code, e.Message)
		}
	}
}

func TestRankRecomputeAndPublicBoard(t *testing.T) {
	ctx := context.Background()
	s, repo, store, c, _ := newTestRankService()
	for i := int64(1); i <= 25; i++ {
		repo.addDrama(i, "剧"+string(rune('A'+i-1)), i*10, true)
	}
	repo.addDrama(99, "没上架", 9999, false)
	repo.overrides[BoardDramaWeek] = []RankOverride{{DramaID: 3, Pinned: true}, {DramaID: 25, Blocked: true}}

	// 还没算过：前台读时当场算一次并写入线上榜单
	b, err := s.PublicBoard(ctx, BoardDramaWeek)
	if err != nil {
		t.Fatal(err)
	}
	if store.replaces != 1 || len(b.List) != rankPublicTop || b.List[0].Drama.ID != 3 || b.List[0].Rank != 1 || b.List[1].Drama.ID != 24 || b.ComputedAt == nil {
		t.Fatalf("没算过时当场算一次：replaces=%d %+v", store.replaces, b.List[:2])
	}
	for _, it := range b.List {
		if it.Drama.ID == 25 || it.Drama.ID == 99 {
			t.Fatalf("不让上榜与不可见的剧不出现：%+v", it)
		}
	}
	if b.List[1].Heat != 240 {
		t.Fatalf("热度值是计算出的分数：%v", b.List[1].Heat)
	}
	// 月榜没有调整：按分数
	m, _ := s.PublicBoard(ctx, BoardDramaMonth)
	if m.List[0].Drama.ID != 25 {
		t.Fatalf("月榜不受周榜的调整影响：%+v", m.List[0])
	}

	// 重算：两张榜都写入，并清掉前台缓存；之后读到新结果
	repo.overrides[BoardDramaWeek] = nil
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	if b, _ := s.PublicBoard(ctx, BoardDramaWeek); b.List[0].Drama.ID != 25 {
		t.Fatalf("重算后去掉了调整：%+v", b.List[0])
	}
	if store.replaces != 4 {
		t.Fatalf("重算两张榜：%d", store.replaces)
	}
	// 线上榜单里的剧下架后，读时滤掉并重新编号
	delete(repo.cards, 25)
	_ = c.Delete(ctx, rankCacheKey(BoardDramaWeek))
	b, _ = s.PublicBoard(ctx, BoardDramaWeek)
	if b.List[0].Drama.ID != 24 || b.List[0].Rank != 1 || len(b.List) != rankPublicTop {
		t.Fatalf("下架的剧滤掉、名次连续、仍补满前 20：%+v", b.List[0])
	}
	if last := repo.cardCalls[len(repo.cardCalls)-1]; len(last) <= rankPublicTop {
		t.Fatalf("多取几名留作补位：%d", len(last))
	}

	// 开关关闭时 404；榜单名无效 400
	s.features = fakeFeatures{off: map[string]bool{"ranks": true}}
	if _, err := s.PublicBoard(ctx, BoardDramaWeek); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("features.ranks 关闭时 404：%v", err)
	}
	s.features = fakeFeatures{}
	if _, err := s.PublicBoard(ctx, "tag_week"); errs.From(err).Code != errs.ErrBadRequest.Code {
		t.Fatalf("未知榜单 400：%v", err)
	}
}

func TestRankAdminBoard(t *testing.T) {
	ctx := context.Background()
	s, repo, _, _, q := newTestRankService()
	repo.addDrama(1, "一", 100, true)
	repo.addDrama(2, "二", 200, true)
	repo.addDrama(3, "三", 300, true)
	if err := s.Recompute(ctx); err != nil {
		t.Fatal(err)
	}
	// 改了调整还没重算：线上不变，预览按新设置
	if _, _, err := s.SetOverrides(ctx, BoardDramaWeek, []RankOverride{{DramaID: 1, Pinned: true}}); err != nil {
		t.Fatal(err)
	}
	v, err := s.AdminBoard(ctx, BoardDramaWeek)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Overrides) != 1 || v.ComputedAt == nil || !v.ComputedAt.Equal(rankNow) {
		t.Fatalf("调整与计算时间：%+v", v)
	}
	if v.Live[0].DramaID != 3 || v.Live[0].Title != "三" || v.Live[0].Heat != 300 || v.Live[0].Rank != 1 {
		t.Fatalf("线上榜单是上次重算的结果：%+v", v.Live)
	}
	if v.Preview[0].DramaID != 1 || !v.Preview[0].Pinned || v.Preview[0].Title != "一" || v.Preview[1].DramaID != 3 {
		t.Fatalf("预览按当前的系数与调整：%+v", v.Preview)
	}
	if _, err := s.AdminBoard(ctx, "x"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("未知榜单 404：%v", err)
	}

	// 马上重新排名：交给任务队列
	done, err := s.RecomputeNow(ctx)
	if err != nil || !done || q.calls != 1 {
		t.Fatalf("马上重新排名：%v %v %d", done, err, q.calls)
	}
}
