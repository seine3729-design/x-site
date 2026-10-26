package ops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
	"xsite/service/internal/textnorm"
)

// HotSource 是热搜词的来源。
type HotSource string

const (
	HotManual HotSource = "manual" // 后台手动加，默认显示
	HotAuto   HotSource = "auto"   // 用户搜过自动收录，默认不显示
)

const (
	maxHotWordRunes = 50 // 与搜索框 q 的上限一致（docs/03 第 3.3 节）
	hotBoardSize    = 10 // 热搜榜条数，也是置顶上限
	hotCacheKey     = "ops:hotsearch"
	hotCacheTTL     = 5 * time.Minute
)

var (
	// ErrTooManyPinned 是置顶数量超过热搜榜条数时的错误。
	ErrTooManyPinned = errs.ErrBadRequest.WithMessage(fmt.Sprintf("最多置顶 %d 个热搜词，请先取消别的置顶", hotBoardSize))
	// ErrPinsChanged 是排序时提交的置顶词与当前不一致（别人刚改过）的错误。
	ErrPinsChanged = errs.ErrConflict.WithMessage("置顶词有变化，请刷新后重新排序")
)

// HotWord 是一个热搜词。Word 是规范化后的词；置顶的词一定显示。
type HotWord struct {
	ID             int64
	Word           string
	SearchCount    int64
	MissCount      int64 // 搜了但片库没有结果的次数
	Pinned         bool
	PinSort        int
	Visible        bool
	Source         HotSource
	LastSearchedAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// HotFilter 是后台列表的筛选：Q 匹配词；Missing 只看「片库没有」的词。
type HotFilter struct {
	Q       string
	Visible *bool
	Pinned  *bool
	Missing bool
	PageRequest
}

// HotQuery 是校验后交给仓储的查询。Pattern 是已转义的 LIKE 模式，空表示不按文本搜。
type HotQuery struct {
	Pattern string
	Visible *bool
	Pinned  *bool
	Missing bool
	Limit   int
	Offset  int
}

// HotPatch 是后台对显示、置顶的修改；为 nil 的项不改。
type HotPatch struct {
	Visible *bool
	Pinned  *bool
}

// HotCounts 是一个词待落库的搜索次数与「片库没有」次数。
type HotCounts struct {
	Searches int64
	Misses   int64
}

// PublicHotWord 是热搜榜上的一项。
type PublicHotWord struct {
	Word   string `json:"word"`
	Pinned bool   `json:"pinned"`
}

// HotRepo 读写热搜词。查不到时返回 errs.ErrNotFound。
type HotRepo interface {
	ListHotWords(ctx context.Context, q HotQuery) ([]HotWord, int64, error)
	GetHotWord(ctx context.Context, id int64) (HotWord, error)
	// CreateHotWord 手动加词（显示中）；词已存在时返回 errs.ErrConflict。
	CreateHotWord(ctx context.Context, word string) (HotWord, error)
	// SetHotWordFlags 保存显示与置顶，新置顶的排到最后；置顶后会超过 maxPinned 个时返回 ErrTooManyPinned。与其他置顶修改串行。
	SetHotWordFlags(ctx context.Context, id int64, visible, pinned bool, maxPinned int) (HotWord, error)
	// ReorderPinned 按 ids 的顺序重排置顶词；ids 与当前置顶词不一致时返回 ErrPinsChanged。
	ReorderPinned(ctx context.Context, ids []int64) error
	// DeleteHotWord 删除词（连同搜索次数），返回删除前的记录。
	DeleteHotWord(ctx context.Context, id int64) (HotWord, error)
	// ListHotBoard 返回显示中的前 n 个词：置顶的按顺序在前，其余按搜索次数倒序。
	ListHotBoard(ctx context.Context, n int) ([]HotWord, error)
	// AddHotCounts 把增量加到累计上；没有的词自动收录（来源 auto、不显示）。
	AddHotCounts(ctx context.Context, counts map[string]HotCounts) error
}

// HotCounter 是 Valkey 里待落库的搜索计数（实现见 store/cache.HotCounter）。
type HotCounter interface {
	// AddSearch 记一次搜索；miss 表示片库没有结果。
	AddSearch(ctx context.Context, word string, miss bool) error
	// TakePending 把待落库的计数原子地移到 token 名下并返回；token 名下已有数据（上次落库失败）时直接返回那一份。
	TakePending(ctx context.Context, token string) (map[string]HotCounts, error)
	// DonePending 删除 token 名下已落库的计数。
	DonePending(ctx context.Context, token string) error
}

// NormalizeSearch 规范化搜索词（docs/03 第 3.3 节）：全角转半角、去掉控制与格式字符、转小写、去首尾并合并连续空白。
// 实现放在 internal/textnorm：搜索接口（domain/content，S3-4）也要用同一套规范化，两边分开写必然漂移。
func NormalizeSearch(s string) string { return textnorm.Search(s) }

// HotsearchService 是热搜词的后台管理、热搜榜与搜索计数。
type HotsearchService struct {
	repo    HotRepo
	counter HotCounter
	cache   Cache
}

// NewHotsearchService 创建热搜词服务。
func NewHotsearchService(repo HotRepo, counter HotCounter, cache Cache) *HotsearchService {
	return &HotsearchService{repo: repo, counter: counter, cache: cache}
}

// List 返回后台热搜词列表的一页：置顶的在前，其余按搜索次数倒序。
func (s *HotsearchService) List(ctx context.Context, f HotFilter) (Page[HotWord], error) {
	q, err := cleanQuery(f.Q)
	if err != nil {
		return Page[HotWord]{}, err
	}
	p, limit, offset, err := limitOffset(f.PageRequest)
	if err != nil {
		return Page[HotWord]{}, err
	}
	// 词库里存的是规范化后的词，搜索词也按同样的方式规范化
	list, total, err := s.repo.ListHotWords(ctx, HotQuery{
		Pattern: likePattern(NormalizeSearch(q)), Visible: f.Visible, Pinned: f.Pinned, Missing: f.Missing, Limit: limit, Offset: offset,
	})
	if err != nil {
		return Page[HotWord]{}, err
	}
	return Page[HotWord]{List: list, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// Create 手动加词：规范化后保存，默认显示。
func (s *HotsearchService) Create(ctx context.Context, raw string) (HotWord, error) {
	word := NormalizeSearch(raw)
	if word == "" {
		return HotWord{}, badRequest("请输入热搜词")
	}
	if utf8.RuneCountInString(word) > maxHotWordRunes {
		return HotWord{}, badRequest("热搜词不能超过 %d 个字", maxHotWordRunes)
	}
	w, err := s.repo.CreateHotWord(ctx, word)
	if errors.Is(err, errs.ErrConflict) {
		return HotWord{}, errs.ErrConflict.WithMessage(fmt.Sprintf("「%s」已在热搜词里，可以在列表里直接设为显示或置顶", word))
	}
	if err == nil {
		s.invalidate(ctx)
	}
	return w, err
}

// Update 修改显示与置顶，返回修改前后的记录。置顶的词一定显示：置顶时自动显示，不显示时同时取消置顶。
func (s *HotsearchService) Update(ctx context.Context, id int64, p HotPatch) (before, after HotWord, err error) {
	switch {
	case p.Visible == nil && p.Pinned == nil:
		return HotWord{}, HotWord{}, badRequest("没有要修改的内容")
	case p.Visible != nil && p.Pinned != nil && !*p.Visible && *p.Pinned:
		return HotWord{}, HotWord{}, badRequest("置顶的词一定显示，不能同时设为不显示")
	}
	if before, err = s.repo.GetHotWord(ctx, id); err != nil {
		return
	}
	visible, pinned := before.Visible, before.Pinned
	if p.Visible != nil {
		visible = *p.Visible
		pinned = pinned && visible
	}
	if p.Pinned != nil {
		pinned = *p.Pinned
		visible = visible || pinned
	}
	if after, err = s.repo.SetHotWordFlags(ctx, id, visible, pinned, hotBoardSize); err == nil {
		s.invalidate(ctx)
	}
	return
}

// Reorder 按 ids 的顺序重排置顶词（ids 必须恰好是当前全部置顶词）。
func (s *HotsearchService) Reorder(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return badRequest("没有置顶词可以排序")
	}
	if len(ids) > hotBoardSize {
		return badRequest("最多置顶 %d 个热搜词", hotBoardSize)
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return badRequest("排序里有重复或无效的编号")
		}
		seen[id] = true
	}
	err := s.repo.ReorderPinned(ctx, ids)
	if err == nil {
		s.invalidate(ctx)
	}
	return err
}

// Delete 删除热搜词（连同搜索次数，之后再被搜到会重新收录），返回删除前的记录。
func (s *HotsearchService) Delete(ctx context.Context, id int64) (HotWord, error) {
	w, err := s.repo.DeleteHotWord(ctx, id)
	if err == nil {
		s.invalidate(ctx)
	}
	return w, err
}

// Board 返回热搜榜（前台 GET /api/v1/search/hot）。
func (s *HotsearchService) Board(ctx context.Context) ([]PublicHotWord, error) {
	var out []PublicHotWord
	err := s.cache.GetOrLoadJSON(ctx, hotCacheKey, hotCacheTTL, &out, func(ctx context.Context) (any, error) {
		words, err := s.repo.ListHotBoard(ctx, hotBoardSize)
		if err != nil {
			return nil, err
		}
		list := make([]PublicHotWord, len(words))
		for i, w := range words {
			list[i] = PublicHotWord{Word: w.Word, Pinned: w.Pinned}
		}
		return list, nil
	})
	if out == nil && err == nil {
		out = []PublicHotWord{}
	}
	return out, err
}

// Record 记一次搜索（搜索接口 S3-4 调用）：按规范化后的词计数，hits 为 0 表示片库没有结果。空词与超长的词不计。
func (s *HotsearchService) Record(ctx context.Context, query string, hits int) error {
	word := NormalizeSearch(query)
	if word == "" || utf8.RuneCountInString(word) > maxHotWordRunes {
		return nil
	}
	if err := s.counter.AddSearch(ctx, word, hits == 0); err != nil {
		return fmt.Errorf("热搜计数: %w", err)
	}
	return nil
}

// FlushCounts 把 Valkey 里累计的搜索次数写入数据库，返回写入了几个词（worker 每小时执行）。
// token 是本次任务的标识：写库失败时取出的计数留在 token 名下，任务重试时沿用同一个 token 再写一次。
func (s *HotsearchService) FlushCounts(ctx context.Context, token string) (int, error) {
	counts, err := s.counter.TakePending(ctx, token)
	if err != nil {
		return 0, fmt.Errorf("取出待落库的热搜计数: %w", err)
	}
	// 只写合规的词：坏数据不能让整批因表约束失败
	maps.DeleteFunc(counts, func(w string, c HotCounts) bool {
		n := utf8.RuneCountInString(w)
		return n == 0 || n > maxHotWordRunes || w != NormalizeSearch(w) || c.Searches <= 0
	})
	if len(counts) == 0 {
		return 0, nil
	}
	if err := s.repo.AddHotCounts(ctx, counts); err != nil {
		return 0, fmt.Errorf("热搜计数落库: %w", err)
	}
	s.invalidate(ctx) // 搜索次数变了，热搜榜的顺序可能跟着变
	// 已经写进数据库：清理失败只记日志，不能让任务重试（会重复累加），留下的那一份到期自动删除
	if err := s.counter.DonePending(ctx, token); err != nil {
		slog.WarnContext(ctx, "清理已落库的热搜计数失败", "token", token, "err", err)
	}
	return len(counts), nil
}

// invalidate 删除热搜榜缓存。删除失败只记日志：缓存最多 5 分钟后自然过期。
func (s *HotsearchService) invalidate(ctx context.Context) {
	if err := s.cache.Delete(ctx, hotCacheKey); err != nil {
		slog.WarnContext(ctx, "清除热搜榜缓存失败，最多 5 分钟后生效", "err", err)
	}
}
