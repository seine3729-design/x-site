package ops

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"xsite/service/internal/errs"
)

// fakeHotRepo 是内存版热搜词仓储，规则与表约束、SQL 一致（置顶一定显示、新置顶排最后、置顶上限）。
type fakeHotRepo struct {
	mu       sync.Mutex
	words    map[int64]HotWord
	nextID   int64
	counts   []map[string]HotCounts
	countErr error
	boardN   []int
}

func newFakeHotRepo(words ...HotWord) *fakeHotRepo {
	r := &fakeHotRepo{words: map[int64]HotWord{}, nextID: 100}
	for _, w := range words {
		r.words[w.ID] = w
	}
	return r
}

func (r *fakeHotRepo) ListHotWords(context.Context, HotQuery) ([]HotWord, int64, error) {
	return nil, 0, nil
}

func (r *fakeHotRepo) GetHotWord(_ context.Context, id int64) (HotWord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.words[id]
	if !ok {
		return HotWord{}, errs.ErrNotFound
	}
	return w, nil
}

func (r *fakeHotRepo) CreateHotWord(_ context.Context, word string) (HotWord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range r.words {
		if w.Word == word {
			return HotWord{}, errs.ErrConflict
		}
	}
	r.nextID++
	w := HotWord{ID: r.nextID, Word: word, Visible: true, Source: HotManual}
	r.words[w.ID] = w
	return w, nil
}

func (r *fakeHotRepo) SetHotWordFlags(_ context.Context, id int64, visible, pinned bool, maxPinned int) (HotWord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.words[id]
	if !ok {
		return HotWord{}, errs.ErrNotFound
	}
	others, maxSort := 0, 0
	for _, x := range r.words {
		if x.Pinned && x.ID != id {
			others++
			maxSort = max(maxSort, x.PinSort)
		}
	}
	if pinned && others >= maxPinned {
		return HotWord{}, ErrTooManyPinned
	}
	switch {
	case !pinned:
		w.PinSort = 0
	case !w.Pinned:
		w.PinSort = maxSort + 1
	}
	w.Visible, w.Pinned = visible, pinned
	r.words[id] = w
	return w, nil
}

func (r *fakeHotRepo) ReorderPinned(_ context.Context, ids []int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var pinned []int64
	for _, w := range r.words {
		if w.Pinned {
			pinned = append(pinned, w.ID)
		}
	}
	got := slices.Sorted(slices.Values(ids))
	slices.Sort(pinned)
	if !slices.Equal(got, pinned) {
		return ErrPinsChanged
	}
	for i, id := range ids {
		w := r.words[id]
		w.PinSort = i + 1
		r.words[id] = w
	}
	return nil
}

func (r *fakeHotRepo) DeleteHotWord(_ context.Context, id int64) (HotWord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.words[id]
	if !ok {
		return HotWord{}, errs.ErrNotFound
	}
	delete(r.words, id)
	return w, nil
}

func (r *fakeHotRepo) ListHotBoard(_ context.Context, n int) ([]HotWord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.boardN = append(r.boardN, n)
	var out []HotWord
	for _, w := range r.words {
		if w.Visible {
			out = append(out, w)
		}
	}
	slices.SortFunc(out, func(a, b HotWord) int {
		switch {
		case a.Pinned != b.Pinned:
			if a.Pinned {
				return -1
			}
			return 1
		case a.PinSort != b.PinSort:
			return a.PinSort - b.PinSort
		case a.SearchCount != b.SearchCount:
			return int(b.SearchCount - a.SearchCount)
		}
		return int(a.ID - b.ID)
	})
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}

func (r *fakeHotRepo) AddHotCounts(_ context.Context, counts map[string]HotCounts) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.countErr != nil {
		return r.countErr
	}
	r.counts = append(r.counts, counts)
	return nil
}

// fakeHotCounter 是内存版 Valkey 计数。
type fakeHotCounter struct {
	mu      sync.Mutex
	added   []string
	pending map[string]HotCounts
	taken   map[string]map[string]HotCounts
	done    []string
}

func newFakeHotCounter() *fakeHotCounter {
	return &fakeHotCounter{pending: map[string]HotCounts{}, taken: map[string]map[string]HotCounts{}}
}

func (f *fakeHotCounter) AddSearch(_ context.Context, word string, miss bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, word)
	c := f.pending[word]
	c.Searches++
	if miss {
		c.Misses++
	}
	f.pending[word] = c
	return nil
}

func (f *fakeHotCounter) TakePending(_ context.Context, token string) (map[string]HotCounts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.taken[token]; ok {
		return m, nil
	}
	m := f.pending
	f.pending = map[string]HotCounts{}
	if len(m) > 0 {
		f.taken[token] = m
	}
	return m, nil
}

func (f *fakeHotCounter) DonePending(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.taken, token)
	f.done = append(f.done, token)
	return nil
}

func newTestHotService(repo *fakeHotRepo, counter *fakeHotCounter) (*HotsearchService, *memCache) {
	c := newMemCache()
	return NewHotsearchService(repo, counter, c), c
}

func ptr[T any](v T) *T { return &v }

func TestNormalizeSearch(t *testing.T) {
	for in, want := range map[string]string{
		"  霸道总裁  ":             "霸道总裁",
		"ＡＢＣ　１２３":              "abc 123",
		"Love   Story":         "love story",
		"重生\t之\n逆袭":            "重生 之 逆袭",
		"ｈｅｌｌｏ，ｗｏｒｌｄ！":         "hello,world!",
		"\u200b\x00闪婚\x7f":     "闪婚",
		"   ":                  "",
		"MiXeD ＣＡＳＥ 剧":         "mixed case 剧",
		"（全角括号）":               "(全角括号)",
		strings.Repeat("长", 3): "长长长",
	} {
		if got := NormalizeSearch(in); got != want {
			t.Errorf("NormalizeSearch(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestHotCreate(t *testing.T) {
	ctx := context.Background()
	repo := newFakeHotRepo(HotWord{ID: 1, Word: "霸道总裁", Source: HotAuto})
	s, _ := newTestHotService(repo, newFakeHotCounter())

	w, err := s.Create(ctx, "  ＣＥＯ 甜宠  ")
	if err != nil || w.Word != "ceo 甜宠" || !w.Visible || w.Source != HotManual {
		t.Fatalf("手动加词：规范化后保存、默认显示：%+v %v", w, err)
	}
	_, err = s.Create(ctx, "霸道总裁")
	wantErr(t, err, errs.ErrConflict, "已在热搜词里")
	_, err = s.Create(ctx, " \t ")
	wantErr(t, err, errs.ErrBadRequest, "请输入热搜词")
	_, err = s.Create(ctx, strings.Repeat("词", 51))
	wantErr(t, err, errs.ErrBadRequest, "不能超过 50 个字")
}

func TestHotUpdateFlags(t *testing.T) {
	ctx := context.Background()
	repo := newFakeHotRepo(
		HotWord{ID: 1, Word: "a", Source: HotAuto},
		HotWord{ID: 2, Word: "b", Visible: true, Pinned: true, PinSort: 1, Source: HotManual},
	)
	s, _ := newTestHotService(repo, newFakeHotCounter())

	// 置顶时自动显示，排在已有置顶之后
	before, after, err := s.Update(ctx, 1, HotPatch{Pinned: ptr(true)})
	if err != nil || before.Pinned || !after.Pinned || !after.Visible || after.PinSort != 2 {
		t.Fatalf("置顶：%+v → %+v %v", before, after, err)
	}
	// 不显示时同时取消置顶
	if _, after, err := s.Update(ctx, 2, HotPatch{Visible: ptr(false)}); err != nil || after.Visible || after.Pinned || after.PinSort != 0 {
		t.Fatalf("隐藏：%+v %v", after, err)
	}
	// 只改显示
	if _, after, err := s.Update(ctx, 2, HotPatch{Visible: ptr(true)}); err != nil || !after.Visible || after.Pinned {
		t.Fatalf("显示：%+v %v", after, err)
	}
	_, _, err = s.Update(ctx, 1, HotPatch{Visible: ptr(false), Pinned: ptr(true)})
	wantErr(t, err, errs.ErrBadRequest, "置顶的词一定显示")
	_, _, err = s.Update(ctx, 1, HotPatch{})
	wantErr(t, err, errs.ErrBadRequest, "没有要修改的内容")
	if _, _, err := s.Update(ctx, 404, HotPatch{Visible: ptr(true)}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不存在的词 404：%v", err)
	}

	// 置顶上限 = 热搜榜条数
	for id := int64(10); id < 10+hotBoardSize; id++ {
		repo.words[id] = HotWord{ID: id, Word: string(rune('a' + id)), Source: HotAuto}
	}
	pinned := 1 // 1 号已置顶
	for id := int64(10); pinned < hotBoardSize; id++ {
		if _, _, err := s.Update(ctx, id, HotPatch{Pinned: ptr(true)}); err != nil {
			t.Fatal(err)
		}
		pinned++
	}
	_, _, err = s.Update(ctx, 2, HotPatch{Pinned: ptr(true)})
	wantErr(t, err, errs.ErrBadRequest, "最多置顶 10 个")
	// 已经置顶的词可以照常改（不算新置顶）
	if _, _, err := s.Update(ctx, 1, HotPatch{Pinned: ptr(true)}); err != nil {
		t.Fatalf("重复置顶不受上限影响：%v", err)
	}
}

func TestHotReorder(t *testing.T) {
	ctx := context.Background()
	repo := newFakeHotRepo(
		HotWord{ID: 1, Word: "a", Visible: true, Pinned: true, PinSort: 1},
		HotWord{ID: 2, Word: "b", Visible: true, Pinned: true, PinSort: 2},
		HotWord{ID: 3, Word: "c", Visible: true},
	)
	s, _ := newTestHotService(repo, newFakeHotCounter())
	if err := s.Reorder(ctx, []int64{2, 1}); err != nil {
		t.Fatal(err)
	}
	if repo.words[2].PinSort != 1 || repo.words[1].PinSort != 2 {
		t.Fatalf("按给定顺序重排：%+v %+v", repo.words[1], repo.words[2])
	}
	if err := s.Reorder(ctx, []int64{2, 3}); !errors.Is(err, ErrPinsChanged) {
		t.Fatalf("与当前置顶词不一致时 409：%v", err)
	}
	wantErr(t, s.Reorder(ctx, []int64{1, 1}), errs.ErrBadRequest, "重复")
	wantErr(t, s.Reorder(ctx, nil), errs.ErrBadRequest, "没有置顶词")
}

func TestHotBoardAndCache(t *testing.T) {
	ctx := context.Background()
	repo := newFakeHotRepo(
		HotWord{ID: 1, Word: "置顶二", Visible: true, Pinned: true, PinSort: 2, SearchCount: 1},
		HotWord{ID: 2, Word: "置顶一", Visible: true, Pinned: true, PinSort: 1, SearchCount: 5},
		HotWord{ID: 3, Word: "最热", Visible: true, SearchCount: 900},
		HotWord{ID: 4, Word: "次热", Visible: true, SearchCount: 800},
		HotWord{ID: 5, Word: "没显示", SearchCount: 10000, Source: HotAuto},
	)
	s, c := newTestHotService(repo, newFakeHotCounter())
	board, err := s.Board(ctx)
	if err != nil {
		t.Fatal(err)
	}
	words := []string{}
	for _, w := range board {
		words = append(words, w.Word)
	}
	if !slices.Equal(words, []string{"置顶一", "置顶二", "最热", "次热"}) || !board[0].Pinned || board[2].Pinned {
		t.Fatalf("置顶的按顺序在前，其余显示中的按搜索次数，不显示的不上榜：%v %+v", words, board)
	}
	if !slices.Equal(repo.boardN, []int{hotBoardSize}) {
		t.Fatalf("取前 %d 个：%v", hotBoardSize, repo.boardN)
	}
	_, _ = s.Board(ctx)
	if c.loads != 1 {
		t.Fatalf("第二次读命中缓存：%d", c.loads)
	}
	// 后台写操作都清缓存
	if _, err := s.Create(ctx, "新词"); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Board(ctx)
	if _, _, err := s.Update(ctx, 5, HotPatch{Visible: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Board(ctx)
	if err := s.Reorder(ctx, []int64{1, 2}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Board(ctx)
	if _, err := s.Delete(ctx, 3); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Board(ctx)
	if c.loads != 5 {
		t.Fatalf("新建、修改、排序、删除后都应清缓存：%d", c.loads)
	}
	empty, _ := NewHotsearchService(newFakeHotRepo(), newFakeHotCounter(), newMemCache()).Board(ctx)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("没有热搜词时为空列表：%#v", empty)
	}
}

func TestHotRecordAndFlush(t *testing.T) {
	ctx := context.Background()
	repo := newFakeHotRepo()
	counter := newFakeHotCounter()
	s, c := newTestHotService(repo, counter)

	for _, tt := range []struct {
		q    string
		hits int
	}{
		{"霸道总裁", 12}, {" 霸道总裁 ", 3}, {"ＣＥＯ", 0}, {"ceo", 0}, {"   ", 0}, {strings.Repeat("长", 51), 0},
	} {
		if err := s.Record(ctx, tt.q, tt.hits); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(counter.added, []string{"霸道总裁", "霸道总裁", "ceo", "ceo"}) {
		t.Fatalf("按规范化后的词计数，空词与超长的不计：%v", counter.added)
	}
	_, _ = s.Board(ctx) // 先把热搜榜读进缓存
	n, err := s.FlushCounts(ctx, "job-1")
	if err != nil || n != 2 || len(repo.counts) != 1 {
		t.Fatalf("落库：%d %v %v", n, err, repo.counts)
	}
	if got := repo.counts[0]; got["霸道总裁"] != (HotCounts{Searches: 2}) || got["ceo"] != (HotCounts{Searches: 2, Misses: 2}) {
		t.Fatalf("搜索次数与片库没有的次数：%v", got)
	}
	if !slices.Equal(counter.done, []string{"job-1"}) {
		t.Fatalf("写库成功后清理：%v", counter.done)
	}
	_, _ = s.Board(ctx)
	if c.loads != 2 {
		t.Fatalf("搜索次数变了，热搜榜缓存应重新读取：%d", c.loads)
	}

	// 写库失败保留，重试沿用同一个任务标识
	_ = s.Record(ctx, "重生", 1)
	repo.countErr = errors.New("db down")
	if _, err := s.FlushCounts(ctx, "job-2"); err == nil {
		t.Fatal("写库失败应返回错误，由任务重试")
	}
	repo.countErr = nil
	if n, err := s.FlushCounts(ctx, "job-2"); err != nil || n != 1 || repo.counts[1]["重生"] != (HotCounts{Searches: 1}) {
		t.Fatalf("重试写入上次取出的计数：%d %v %v", n, err, repo.counts)
	}
	if n, err := s.FlushCounts(ctx, "job-3"); err != nil || n != 0 || len(repo.counts) != 2 {
		t.Fatalf("没有新计数时不写库：%d %v", n, err)
	}
}

func TestHotListFilters(t *testing.T) {
	s, _ := newTestHotService(newFakeHotRepo(), newFakeHotCounter())
	if _, err := s.List(context.Background(), HotFilter{Q: strings.Repeat("长", 51)}); err == nil {
		t.Fatal("搜索词过长应 400")
	}
	if _, err := s.List(context.Background(), HotFilter{PageRequest: PageRequest{PageSize: 101}}); err == nil {
		t.Fatal("每页条数过大应 400")
	}
}
