package content

import (
	"context"
	"time"
)

// PublicDramaFilter 是前台剧列表的筛选，各项之间是「与」。
// TermIDs 里的每个取值都要命中（与自动专题规则一致）；零值表示不限。
type PublicDramaFilter struct {
	TermIDs  []int64
	TagID    int64
	Finished *bool
	Since    *time.Time // 上新窗口的起点，按 coalesce(publish_at, created_at) 比较
	// Pattern 是剧名或别名的 LIKE 模式（搜索用，已由领域层转义并加 %）；空表示不按文本搜。
	Pattern string
}

// DramaCursor 是前台列表的游标位置。ID 为 0 表示第一页；其余字段按排序取用：
// hot / recommend 用 Num、latest 用 At、random 用 Text。
type DramaCursor struct {
	ID   int64
	Num  int64
	At   time.Time
	Text string
}

// PublicDramaQuery 是一次前台剧列表查询。Offset 只有频道页（页码分页）使用。
type PublicDramaQuery struct {
	PublicDramaFilter
	Sort   DramaSort
	Seed   int64 // random 排序的 10 分钟窗口种子
	Cursor DramaCursor
	Limit  int
	Offset int
}

// PublicRepo 是前台只读查询。查不到（含不可见）时返回 errs.ErrNotFound。
// 实现见 internal/store/pg/content_public.go。
type PublicRepo interface {
	// ---------- 剧详情 ----------
	PublicDrama(ctx context.Context, id int64) (Drama, error)
	PublicDramaTags(ctx context.Context, dramaID int64) ([]Tag, error)
	PublicDramaTerms(ctx context.Context, dramaID int64) ([]TermView, error)
	PublicEpisodes(ctx context.Context, dramaID int64) ([]EpisodeView, error)
	// RelatedDramas 返回与给定标签或取值有交集的其他可见剧，按共同项数 → 热度 → 编号排序。
	RelatedDramas(ctx context.Context, excludeID int64, tagIDs, termIDs []int64, limit int) ([]DramaCard, error)

	// ---------- 列表 ----------
	PublicDramas(ctx context.Context, q PublicDramaQuery) ([]DramaCard, error)
	CountPublicDramas(ctx context.Context, f PublicDramaFilter) (int64, error)

	// ---------- 频道、标签、分面 ----------
	PublicChannel(ctx context.Context, slug string) (ChannelView, error)
	PublicTagBySlug(ctx context.Context, slug string) (Tag, error)
	PublicTagByID(ctx context.Context, id int64) (Tag, error)
	FacetTaxonomies(ctx context.Context) ([]FacetTaxonomy, error)
	// ResolveTermIDs 把「维度slug:取值slug」解析为取值编号，解析不到的不返回。
	ResolveTermIDs(ctx context.Context, pairs []string) ([]int64, error)
	// ResolveTermIDsByCodes 把取值编号（code）解析为取值 ID，供自动专题规则使用。
	ResolveTermIDsByCodes(ctx context.Context, codes []string) ([]int64, error)

	// ---------- 专题 ----------
	PublicTopics(ctx context.Context, cursorID int64, limit int) ([]Topic, int64, error)
	PublicTopicBySlug(ctx context.Context, slug string) (Topic, error)
	// PublicTopicDramas 返回手选专题里可见的剧，按后台保存的顺序。
	PublicTopicDramas(ctx context.Context, topicID int64, limit int) ([]DramaCard, error)
}

// Cache 是读穿缓存，实现见 internal/store/cache（与 platform.Cache 同形）。
type Cache interface {
	GetOrLoadJSON(ctx context.Context, key string, ttl time.Duration, dst any, load func(context.Context) (any, error)) error
}
