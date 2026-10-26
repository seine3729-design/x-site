package ops

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
)

// RankBoard 是榜单（docs/06 第 21 节）。
type RankBoard string

const (
	BoardDramaWeek  RankBoard = "drama_week"  // 剧集周榜
	BoardDramaMonth RankBoard = "drama_month" // 剧集月榜
)

// RankBoards 是全部榜单。
var RankBoards = []RankBoard{BoardDramaWeek, BoardDramaMonth}

const (
	rankKeep         = 100 // 每张榜在 Valkey 里存前 100 名
	rankPublicTop    = 20  // 前台取前 20 名
	rankReadExtra    = 10  // 前台多取几名，补上次重算后下架的剧
	maxRankOverrides = 100 // 每张榜最多调整几部剧
	maxRankBoost     = 1_000_000_000
	maxRankWeight    = 1000
	rankCacheTTL     = 30 * time.Second // 前台榜单（含剧卡片）的缓存，重算后立即清除
	rankFeature      = "ranks"          // site_config.features 的开关
)

func rankCacheKey(b RankBoard) string { return "ops:rank:" + string(b) }

// RankWeights 是计分系数（site_config.ranking）。
type RankWeights struct {
	Play   float64 `json:"play"`
	Follow float64 `json:"follow"`
	Like   float64 `json:"like"`
	Finish float64 `json:"finish"`
}

// DefaultRankWeights：播放 1、追剧 3、点赞 2、看完 2（docs/specs/A3-4.md 假设 3）。与迁移 00021 的默认值一致。
var DefaultRankWeights = RankWeights{Play: 1, Follow: 3, Like: 2, Finish: 2}

// RankSignals 是一部前台可见的剧在统计窗口内的计分数据。
type RankSignals struct {
	DramaID  int64
	Plays    int64
	Follows  int64
	Likes    int64
	Finishes int64
}

// RankOverride 是一部剧在某张榜上的人工调整：置顶、不让上榜（互斥），加分（负数为减分）。
type RankOverride struct {
	DramaID int64
	Pinned  bool
	Blocked bool
	Boost   int64
}

// RankOverrideView 是后台显示的调整（带剧名、剧的状态）。
type RankOverrideView struct {
	RankOverride
	Title       string
	DramaStatus string
	InTrash     bool
	UpdatedAt   time.Time
}

// RankEntry 是算出的一个名次。线上榜单（Valkey）不存置顶标记。
type RankEntry struct {
	DramaID int64
	Score   float64
	Pinned  bool
}

// RankRow 是后台显示的一个名次（带剧名）。
type RankRow struct {
	Rank    int
	DramaID int64
	Title   string
	Heat    float64
	Pinned  bool
}

// RankAdminView 是后台一张榜的页面数据：调整、线上榜单（上次重算的结果）与按当前设置的预览。
type RankAdminView struct {
	Board      RankBoard
	Overrides  []RankOverrideView
	ComputedAt *time.Time // 线上榜单的计算时间；还没算过为 nil
	Live       []RankRow
	Preview    []RankRow
}

// PublicRankItem 是前台榜单的一项：名次、热度值（算出的分数）与剧卡片。
type PublicRankItem struct {
	Rank  int               `json:"rank"`
	Heat  float64           `json:"heat"`
	Drama content.DramaCard `json:"drama"`
}

// PublicRankBoard 是前台的一张榜（GET /api/v1/rank）。
type PublicRankBoard struct {
	Board      RankBoard        `json:"board"`
	ComputedAt *time.Time       `json:"computed_at"`
	List       []PublicRankItem `json:"list"`
}

// RankRepo 读写人工调整、计分系数与榜单要显示的剧。
type RankRepo interface {
	RankingWeights(ctx context.Context) (RankWeights, error)
	SetRankingWeights(ctx context.Context, w RankWeights) (RankWeights, error)
	// RankOverrideRules 返回计算用的调整。
	RankOverrideRules(ctx context.Context, b RankBoard) ([]RankOverride, error)
	ListRankOverrides(ctx context.Context, b RankBoard) ([]RankOverrideView, error)
	// ReplaceRankOverrides 整体替换一张榜的调整。
	ReplaceRankOverrides(ctx context.Context, b RankBoard, list []RankOverride) error
	// MissingDramas 返回 ids 中不存在或在回收站的剧。
	MissingDramas(ctx context.Context, ids []int64) ([]int64, error)
	DramaTitles(ctx context.Context, ids []int64) (map[int64]string, error)
	// RankCards 按 ids 的顺序返回前台可见的剧卡片（不可见的跳过）。
	RankCards(ctx context.Context, ids []int64) ([]content.DramaCard, error)
}

// RankSource 提供计分数据。埋点（S4-1 / S4-2）与追剧、点赞（S2-3）就绪前，播放 = 剧的热度（初始播放量 + 真实播放量），
// 其余为 0，周榜与月榜相同（docs/specs/A3-4.md 决定 3）；就绪后换成按统计窗口汇总的实现。
type RankSource interface {
	RankSignals(ctx context.Context, b RankBoard) ([]RankSignals, error)
}

// RankStore 是 Valkey 里的线上榜单（实现见 store/cache.RankStore）。
type RankStore interface {
	// Replace 用新的名次原子替换线上榜单（先写临时键再 RENAME，读的一方没有空窗），并记下计算时间；entries 为空时清空榜单。
	Replace(ctx context.Context, b RankBoard, entries []RankEntry, at time.Time) error
	// Top 返回线上榜单的前 n 名与计算时间；还没算过时 ok 为 false。
	Top(ctx context.Context, b RankBoard, n int) (entries []RankEntry, at time.Time, ok bool, err error)
}

// FeatureFlags 读站点功能开关（site_config.features）；没设置的开关视为打开。
type FeatureFlags interface {
	Enabled(ctx context.Context, name string) (bool, error)
}

// RecomputeQueue 把「马上重新排名」交给 worker（River 任务），done 表示在等待时间内已经算完。
type RecomputeQueue interface {
	RecomputeNow(ctx context.Context) (done bool, err error)
}

// ComputeRanking 按系数计分、应用人工调整，返回排好的前 limit 名（纯函数）：
// 不让上榜的去掉；加分直接加到分数上；置顶的排在最前，多部置顶之间按分数；其余按分数倒序，同分按剧集编号升序。
// 只有 signals 里的剧（前台可见）能上榜，调整了但不可见的剧忽略。
func ComputeRanking(signals []RankSignals, overrides []RankOverride, w RankWeights, limit int) []RankEntry {
	rules := make(map[int64]RankOverride, len(overrides))
	for _, o := range overrides {
		rules[o.DramaID] = o
	}
	out := make([]RankEntry, 0, len(signals))
	for _, sg := range signals {
		o := rules[sg.DramaID]
		if o.Blocked {
			continue
		}
		score := float64(sg.Plays)*w.Play + float64(sg.Follows)*w.Follow + float64(sg.Likes)*w.Like +
			float64(sg.Finishes)*w.Finish + float64(o.Boost)
		out = append(out, RankEntry{DramaID: sg.DramaID, Score: round2(score), Pinned: o.Pinned})
	}
	slices.SortFunc(out, func(a, b RankEntry) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		return cmp.Compare(a.DramaID, b.DramaID)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// round2 保留两位小数（系数最多两位小数），避免浮点误差让同分的剧排序抖动。
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// RankService 是榜单的计分、重算、人工调整与前台读取。
type RankService struct {
	repo     RankRepo
	source   RankSource
	store    RankStore
	cache    Cache
	features FeatureFlags
	queue    RecomputeQueue
	now      func() time.Time
	group    singleflight.Group
}

// NewRankService 创建榜单服务。worker 进程只用 Recompute，features 与 queue 可为 nil。
func NewRankService(repo RankRepo, source RankSource, store RankStore, cache Cache, features FeatureFlags, queue RecomputeQueue) *RankService {
	return &RankService{repo: repo, source: source, store: store, cache: cache, features: features, queue: queue, now: time.Now}
}

func validBoard(b RankBoard) bool { return slices.Contains(RankBoards, b) }

// Weights 返回计分系数。
func (s *RankService) Weights(ctx context.Context) (RankWeights, error) {
	return s.repo.RankingWeights(ctx)
}

// SetWeights 保存计分系数（0–1000，保留两位小数），返回修改前后的值。要重新排名才生效。
func (s *RankService) SetWeights(ctx context.Context, w RankWeights) (before, after RankWeights, err error) {
	for _, v := range []*float64{&w.Play, &w.Follow, &w.Like, &w.Finish} {
		if math.IsNaN(*v) || *v < 0 || *v > maxRankWeight {
			return before, after, badRequest("系数为 0–%d 的数字", maxRankWeight)
		}
		*v = round2(*v)
	}
	if before, err = s.repo.RankingWeights(ctx); err != nil {
		return
	}
	after, err = s.repo.SetRankingWeights(ctx, w)
	return
}

// SetOverrides 整体保存一张榜的人工调整，返回修改前后的列表。没有任何调整的行不保存；要重新排名才生效。
func (s *RankService) SetOverrides(ctx context.Context, b RankBoard, list []RankOverride) (before, after []RankOverrideView, err error) {
	if !validBoard(b) {
		return nil, nil, errs.ErrNotFound
	}
	clean, err := cleanOverrides(list)
	if err != nil {
		return nil, nil, err
	}
	ids := make([]int64, len(clean))
	for i, o := range clean {
		ids[i] = o.DramaID
	}
	missing, err := s.repo.MissingDramas(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	if len(missing) > 0 {
		names := make([]string, len(missing))
		for i, id := range missing {
			names[i] = fmt.Sprintf("#%d", id)
		}
		return nil, nil, badRequest("剧集 %s 不存在或在回收站", strings.Join(names, "、"))
	}
	if before, err = s.repo.ListRankOverrides(ctx, b); err != nil {
		return
	}
	if err = s.repo.ReplaceRankOverrides(ctx, b, clean); err != nil {
		return
	}
	after, err = s.repo.ListRankOverrides(ctx, b)
	return
}

func cleanOverrides(list []RankOverride) ([]RankOverride, error) {
	if len(list) > maxRankOverrides {
		return nil, badRequest("每张榜最多调整 %d 部剧", maxRankOverrides)
	}
	seen := make(map[int64]bool, len(list))
	out := make([]RankOverride, 0, len(list))
	for _, o := range list {
		switch {
		case o.DramaID <= 0:
			return nil, badRequest("剧集编号无效：%d", o.DramaID)
		case seen[o.DramaID]:
			return nil, badRequest("剧集 #%d 重复，每部剧只能有一行调整", o.DramaID)
		case o.Pinned && o.Blocked:
			return nil, badRequest("剧集 #%d 不能同时置顶和不让上榜", o.DramaID)
		case o.Boost < -maxRankBoost || o.Boost > maxRankBoost:
			return nil, badRequest("加分为 -%d 到 %d", maxRankBoost, maxRankBoost)
		}
		seen[o.DramaID] = true
		if !o.Pinned && !o.Blocked && o.Boost == 0 {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

// Recompute 重算全部榜单并原子替换线上榜单（worker 每 5 分钟一次，后台「马上重新排名」也走这里）。
func (s *RankService) Recompute(ctx context.Context) error {
	w, err := s.repo.RankingWeights(ctx)
	if err != nil {
		return fmt.Errorf("读取计分系数: %w", err)
	}
	for _, b := range RankBoards {
		if err := s.recomputeBoard(ctx, b, w); err != nil {
			return err
		}
	}
	return nil
}

func (s *RankService) recomputeBoard(ctx context.Context, b RankBoard, w RankWeights) error {
	entries, err := s.compute(ctx, b, w, rankKeep)
	if err != nil {
		return err
	}
	if err := s.store.Replace(ctx, b, entries, s.now().UTC()); err != nil {
		return fmt.Errorf("写入榜单 %s: %w", b, err)
	}
	if err := s.cache.Delete(ctx, rankCacheKey(b)); err != nil {
		slog.WarnContext(ctx, "清除前台榜单缓存失败，最多 30 秒后生效", "board", b, "err", err)
	}
	return nil
}

func (s *RankService) compute(ctx context.Context, b RankBoard, w RankWeights, limit int) ([]RankEntry, error) {
	signals, err := s.source.RankSignals(ctx, b)
	if err != nil {
		return nil, fmt.Errorf("读取计分数据: %w", err)
	}
	overrides, err := s.repo.RankOverrideRules(ctx, b)
	if err != nil {
		return nil, fmt.Errorf("读取人工调整: %w", err)
	}
	return ComputeRanking(signals, overrides, w, limit), nil
}

// RecomputeNow 是后台「马上重新排名」：交给 worker 执行，done 表示在等待时间内已经算完。
func (s *RankService) RecomputeNow(ctx context.Context) (bool, error) {
	return s.queue.RecomputeNow(ctx)
}

// live 读线上榜单；还没算过（新环境、Valkey 被清空）时当场算一次并写入，同一时刻同一张榜只算一次。
func (s *RankService) live(ctx context.Context, b RankBoard, n int) ([]RankEntry, time.Time, error) {
	entries, at, ok, err := s.store.Top(ctx, b, n)
	if err != nil || ok {
		return entries, at, err
	}
	if _, err, _ := s.group.Do(string(b), func() (any, error) {
		w, err := s.repo.RankingWeights(ctx)
		if err != nil {
			return nil, err
		}
		return nil, s.recomputeBoard(ctx, b, w)
	}); err != nil {
		return nil, time.Time{}, err
	}
	entries, at, _, err = s.store.Top(ctx, b, n)
	return entries, at, err
}

// PublicBoard 返回前台榜单的前 20 名（GET /api/v1/rank）。features.ranks 关闭时 404。
// 上次重算后下架的剧在读时滤掉，名次重新连续编号。
func (s *RankService) PublicBoard(ctx context.Context, b RankBoard) (PublicRankBoard, error) {
	if !validBoard(b) {
		return PublicRankBoard{}, badRequest("board 只能是 drama_week 或 drama_month")
	}
	on, err := s.features.Enabled(ctx, rankFeature)
	if err != nil {
		return PublicRankBoard{}, err
	}
	if !on {
		return PublicRankBoard{}, errs.ErrNotFound
	}
	var out PublicRankBoard
	err = s.cache.GetOrLoadJSON(ctx, rankCacheKey(b), rankCacheTTL, &out, func(ctx context.Context) (any, error) {
		return s.buildPublic(ctx, b)
	})
	return out, err
}

func (s *RankService) buildPublic(ctx context.Context, b RankBoard) (PublicRankBoard, error) {
	entries, at, err := s.live(ctx, b, rankPublicTop+rankReadExtra)
	if err != nil {
		return PublicRankBoard{}, err
	}
	ids := make([]int64, len(entries))
	heat := make(map[int64]float64, len(entries))
	for i, e := range entries {
		ids[i], heat[e.DramaID] = e.DramaID, e.Score
	}
	cards, err := s.repo.RankCards(ctx, ids)
	if err != nil {
		return PublicRankBoard{}, err
	}
	out := PublicRankBoard{Board: b, List: make([]PublicRankItem, 0, rankPublicTop)}
	if !at.IsZero() {
		out.ComputedAt = &at
	}
	for _, c := range cards {
		if len(out.List) == rankPublicTop {
			break
		}
		out.List = append(out.List, PublicRankItem{Rank: len(out.List) + 1, Heat: heat[c.ID], Drama: c})
	}
	return out, nil
}

// AdminBoard 返回后台一张榜的页面数据。预览按当前的系数与调整现算，重新排名后线上就是这个结果。
func (s *RankService) AdminBoard(ctx context.Context, b RankBoard) (RankAdminView, error) {
	if !validBoard(b) {
		return RankAdminView{}, errs.ErrNotFound
	}
	overrides, err := s.repo.ListRankOverrides(ctx, b)
	if err != nil {
		return RankAdminView{}, err
	}
	live, at, ok, err := s.store.Top(ctx, b, rankPublicTop)
	if err != nil {
		return RankAdminView{}, err
	}
	w, err := s.repo.RankingWeights(ctx)
	if err != nil {
		return RankAdminView{}, err
	}
	preview, err := s.compute(ctx, b, w, rankPublicTop)
	if err != nil {
		return RankAdminView{}, err
	}
	ids := make([]int64, 0, len(live)+len(preview))
	for _, e := range slices.Concat(live, preview) {
		ids = append(ids, e.DramaID)
	}
	titles, err := s.repo.DramaTitles(ctx, ids)
	if err != nil {
		return RankAdminView{}, err
	}
	v := RankAdminView{Board: b, Overrides: overrides, Live: rankRows(live, titles), Preview: rankRows(preview, titles)}
	if ok {
		v.ComputedAt = &at
	}
	return v, nil
}

func rankRows(entries []RankEntry, titles map[int64]string) []RankRow {
	out := make([]RankRow, len(entries))
	for i, e := range entries {
		out[i] = RankRow{Rank: i + 1, DramaID: e.DramaID, Title: titles[e.DramaID], Heat: e.Score, Pinned: e.Pinned}
	}
	return out
}
