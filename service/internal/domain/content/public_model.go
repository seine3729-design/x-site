package content

import "time"

// 前台内容读模型（S1-4，docs/03 第 3.2 节、docs/specs/S1-4.md）。
//
// 这些类型即接口响应体（与 openapi.yaml 的 schema 一一对应），所以带 json tag：
// 它们同时是缓存里存的文档，HTTP 层直接原样下发，不再做一层 DTO 映射。
// 只有前台可见的内容会出现在这里，因此不带 status、deleted_at 等后台字段。

// SortRandom 是前台特有的随机排序：按 10 分钟稳定的种子打乱（后台列表不支持）。
const SortRandom DramaSort = "random"

// 上新窗口（docs/03 第 3.2 节）。
const (
	WindowAll = "all"
)

// DramaCard 是列表与推荐位的剧卡片。
type DramaCard struct {
	ID             int64      `json:"id"`
	Title          string     `json:"title"`
	Slug           string     `json:"slug"`
	CoverPortrait  string     `json:"cover_portrait"`
	CoverLandscape string     `json:"cover_landscape"`
	Badge          string     `json:"badge"`
	Score          *float64   `json:"score"`
	Year           *int32     `json:"year"`
	IsFinished     bool       `json:"is_finished"`
	LatestEpisode  int32      `json:"latest_episode"`
	PlayCount      int64      `json:"play_count"`
	PublishAt      *time.Time `json:"publish_at"`

	// 下面三项不下发，只用于按排序生成下一页的游标（缓存命中时不会再用到）。
	CreatedAt  time.Time `json:"-"`
	HotScore   int64     `json:"-"`
	SortWeight int32     `json:"-"`
}

// TagView 是剧上的标签。IsSensitive 对应 tag.is_hidden：过年龄门才显示、所在页面输出 noindex。
type TagView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	IsSensitive bool   `json:"is_sensitive"`
}

// TermView 是剧选中的维度取值（带所属维度）。
type TermView struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Code         string `json:"code"`
	IsChannel    bool   `json:"is_channel"`
	TaxonomyID   int64  `json:"taxonomy_id"`
	TaxonomyName string `json:"taxonomy_name"`
	TaxonomySlug string `json:"taxonomy_slug"`
}

// EpisodeView 是选集里的一集。Playable 为 false 表示还没转封装完成，前台显示「敬请期待」。
type EpisodeView struct {
	ID          int64  `json:"id"`
	Number      int32  `json:"number"`
	Title       string `json:"title"`
	Cover       string `json:"cover"`
	DurationSec int32  `json:"duration_sec"`
	Access      Access `json:"access"`
	TrialSec    int32  `json:"trial_sec"`
	Playable    bool   `json:"playable"`
}

// DramaDetailView 是剧详情页的数据。
type DramaDetailView struct {
	DramaCard
	Aliases      []string      `json:"aliases"`
	Summary      string        `json:"summary"`
	SEOTitle     string        `json:"seo_title"`
	SEODesc      string        `json:"seo_desc"`
	EpisodeCount int           `json:"episode_count"`
	Tags         []TagView     `json:"tags"`
	Terms        []TermView    `json:"terms"`
	Episodes     []EpisodeView `json:"episodes"`
	Related      []DramaCard   `json:"related"`
}

// ChannelView 是频道（设为频道的维度取值）。
type ChannelView struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Code         string `json:"code"`
	TaxonomyID   int64  `json:"taxonomy_id"`
	TaxonomyName string `json:"taxonomy_name"`
	TaxonomySlug string `json:"taxonomy_slug"`
}

// ChannelPage 是频道页的一页（前台只有频道页用页码分页，docs/07 第 0.1 节）。
type ChannelPage struct {
	Channel  ChannelView `json:"channel"`
	List     []DramaCard `json:"list"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

// TagInfo 是标签页头部。标签已被合并时 CanonicalSlug 为目标标签的 slug，供前台做规范链接。
type TagInfo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	CanonicalSlug string `json:"canonical_slug"`
	IsSensitive   bool   `json:"is_sensitive"`
}

// TagPage 是标签页的一页。
type TagPage struct {
	Tag        TagInfo     `json:"tag"`
	List       []DramaCard `json:"list"`
	NextCursor *string     `json:"next_cursor"`
	Total      int64       `json:"total"`
}

// FacetTerm 是分面里的一个取值。
type FacetTerm struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Code      string `json:"code"`
	IsChannel bool   `json:"is_channel"`
}

// FacetTaxonomy 是分面里的一个维度。筛选参数为 `<Slug>:<取值 Slug>`。
type FacetTaxonomy struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	Slug        string      `json:"slug"`
	MultiSelect bool        `json:"multi_select"`
	Terms       []FacetTerm `json:"terms"`
}

// ExploreFacets 是探索页的筛选条。
type ExploreFacets struct {
	Taxonomies []FacetTaxonomy `json:"taxonomies"`
}

// ExplorePage 是探索页的一页。Facets 只在第一页返回（无限滚动的后续页不重复下发）。
type ExplorePage struct {
	Facets     *ExploreFacets `json:"facets,omitempty"`
	List       []DramaCard    `json:"list"`
	NextCursor *string        `json:"next_cursor"`
	Total      int64          `json:"total"`
}

// TopicCard 是专题列表项。
type TopicCard struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Slug    string `json:"slug"`
	Summary string `json:"summary"`
}

// TopicListPage 是专题列表的一页。
type TopicListPage struct {
	List       []TopicCard `json:"list"`
	NextCursor *string     `json:"next_cursor"`
	Total      int64       `json:"total"`
}

// FAQView 是专题页的一组问答（纯文本，前台输出 FAQPage 结构化数据）。
type FAQView struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// TopicDetailView 是专题详情。
type TopicDetailView struct {
	TopicCard
	SourceType SourceType  `json:"source_type"`
	FAQ        []FAQView   `json:"faq"`
	Dramas     []DramaCard `json:"dramas"`
}
