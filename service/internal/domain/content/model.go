// Package content 是内容域：剧集、分集、标签、筛选维度与取值、专题（docs/02 第 2 节、docs/03 第 3.1 节）。
// 本包是后台的增删改查规则（S1-3，docs/specs/S1-3.md）；前台读接口在 S1-4。
package content

import "time"

// DramaStatus 是剧集状态（drama.status）。
type DramaStatus string

const (
	DramaDraft     DramaStatus = "draft"
	DramaPublished DramaStatus = "published"
	DramaOffline   DramaStatus = "offline"
	// DramaScheduled 不入库，只用于列表筛选与显示：已上架但 publish_at 未到，前台不可见。
	DramaScheduled DramaStatus = "scheduled"
)

// Drama 是一部剧（不含回收站中的剧）。Slug 为空表示未填（库中为 NULL）；Score、Year、PublishAt 为 nil 表示未填。
type Drama struct {
	ID             int64
	Title          string
	Aliases        []string
	Slug           string
	Summary        string
	CoverPortrait  string
	CoverLandscape string
	Badge          string
	Score          *float64
	Year           *int32
	Status         DramaStatus
	PublishAt      *time.Time
	IsFinished     bool
	SortWeight     int32
	ViewBase       int64
	ViewCount      int64
	HotScore       int64
	LatestEpisode  int32
	SEOTitle       string
	SEODesc        string
	Source         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DisplayStatus 是列表显示用的状态：已上架但 publish_at 在 now 之后时为 scheduled。
func (d Drama) DisplayStatus(now time.Time) DramaStatus {
	if d.Status == DramaPublished && d.PublishAt != nil && d.PublishAt.After(now) {
		return DramaScheduled
	}
	return d.Status
}

// DramaInput 是剧资料（新建与整体保存）。新建时忽略 Status（固定为草稿）。
type DramaInput struct {
	Title          string
	Aliases        []string
	Slug           string
	Summary        string
	CoverPortrait  string
	CoverLandscape string
	Badge          string
	Score          *float64
	Year           *int32
	Status         DramaStatus
	PublishAt      *time.Time
	IsFinished     bool
	SortWeight     int32
	ViewBase       int64
	SEOTitle       string
	SEODesc        string
}

// DramaSort 是后台剧集列表与自动专题的排序。
type DramaSort string

const (
	SortDefault   DramaSort = ""          // 按编号倒序
	SortLatest    DramaSort = "latest"    // 上架时间倒序，未上架的在最后
	SortUpdated   DramaSort = "updated"   // 修改时间倒序
	SortHot       DramaSort = "hot"       // 热度（初始播放量 + 真实播放量）
	SortRecommend DramaSort = "recommend" // 排序值（sort_weight）越大越前
)

// PageRequest 是页码分页参数：Page 从 1 开始，PageSize ≤ 100；为 0 时取默认值（第 1 页、每页 20 条）。
type PageRequest struct {
	Page     int
	PageSize int
}

// Page 是一页结果。
type Page[T any] struct {
	List     []T
	Total    int64
	Page     int
	PageSize int
}

// DramaFilter 是后台剧集列表的筛选。Q 匹配剧名或别名，也可以是 #ID 或纯数字编号。
type DramaFilter struct {
	Q      string
	Status DramaStatus // 空为全部；可为 scheduled
	TagID  int64
	TermID int64
	Sort   DramaSort
	PageRequest
}

// DramaQuery 是校验后交给仓储的查询。Pattern 是已转义的 LIKE 模式，空表示不按文本搜；IDEq 为 0 表示不按编号。
type DramaQuery struct {
	Pattern string
	IDEq    int64
	Status  DramaStatus
	TagID   int64
	TermID  int64
	Sort    DramaSort
	Limit   int
	Offset  int
}

// TermRef 是剧选中的维度取值（带所属维度）。
type TermRef struct {
	ID           int64
	TaxonomyID   int64
	TaxonomyName string
	MultiSelect  bool
	Name         string
	Slug         string
	Status       ActiveStatus
}

// DramaDetail 是剧集编辑页的数据：剧资料与标签、维度取值。分集单独读取。
type DramaDetail struct {
	Drama
	Tags  []Tag
	Terms []TermRef
}

// BulkAction 是剧集列表的批量操作。
type BulkAction string

const (
	BulkPublish    BulkAction = "publish"
	BulkOffline    BulkAction = "offline"
	BulkDraft      BulkAction = "draft"
	BulkAddTags    BulkAction = "add_tags"     // 追加标签
	BulkAddToTopic BulkAction = "add_to_topic" // 加入手选专题（追加在末尾）
	BulkSetTerms   BulkAction = "set_terms"    // 多选维度追加，单选维度替换
)

// BulkInput 是一次批量操作：最多 200 部剧。
type BulkInput struct {
	IDs     []int64
	Action  BulkAction
	TagIDs  []int64
	TopicID int64
	TermIDs []int64
}

// ---------- 分集 ----------

// Access 是分集的观看权限。
type Access string

const (
	AccessFree  Access = "free"
	AccessLogin Access = "login"
	AccessVIP   Access = "vip"
)

// Episode 是一集。VideoObjectID 由 S1-6a 的「视频路径 → 媒体对象」绑定写入，S1-3 中只读。
type Episode struct {
	ID            int64
	DramaID       int64
	Number        int32
	Title         string
	VideoObjectID *int64
	Cover         string
	DurationSec   int32
	Access        Access
	TrialSec      int32
	MediaStatus   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// EpisodeInput 是分集的新建与整体保存。DurationSec 为 0 表示时长未知。
type EpisodeInput struct {
	Number      int32
	Title       string
	Cover       string
	DurationSec int32
	Access      Access
	TrialSec    int32
}

// ---------- 标签 ----------

// TagStatus 是标签状态：hidden 不在前台显示。
type TagStatus string

const (
	TagActive TagStatus = "active"
	TagHidden TagStatus = "hidden"
)

// Tag 是标签。MergedIntoID 不为空表示已并入该标签（软合并）。
type Tag struct {
	ID           int64
	Name         string
	Slug         string
	IsHidden     bool
	Status       TagStatus
	MergedIntoID *int64
	CreatedAt    time.Time
}

// TagInput 是标签的新建与整体保存。IsHidden 为敏感标签：过年龄门才显示、不被搜索引擎收录。
type TagInput struct {
	Name     string
	Slug     string
	IsHidden bool
	Status   TagStatus
}

// TagFilter 是标签列表的筛选：Q 匹配名称或 slug；Merged 为 nil 时不按是否已合并筛选。
type TagFilter struct {
	Q      string
	Status TagStatus
	Merged *bool
	PageRequest
}

// TagQuery 是校验后交给仓储的查询。
type TagQuery struct {
	Pattern string
	Status  TagStatus
	Merged  *bool
	Limit   int
	Offset  int
}

// ---------- 筛选维度与取值 ----------

// ActiveStatus 是维度、取值的启用状态。
type ActiveStatus string

const (
	StatusActive   ActiveStatus = "active"
	StatusInactive ActiveStatus = "inactive"
)

// Taxonomy 是筛选维度及其取值。
type Taxonomy struct {
	ID            int64
	Name          string
	Slug          string
	MultiSelect   bool
	ShowInExplore bool
	Status        ActiveStatus
	Sort          int32
	Terms         []Term
}

// TaxonomyInput 是维度的新建与整体保存。
type TaxonomyInput struct {
	Name          string
	Slug          string
	MultiSelect   bool
	ShowInExplore bool
	Status        ActiveStatus
}

// Term 是维度下的取值。IsChannel 为有独立频道页（/c/:slug）；Code 供区块与专题规则引用，空表示未设置。
type Term struct {
	ID         int64
	TaxonomyID int64
	Name       string
	Slug       string
	Status     ActiveStatus
	IsChannel  bool
	Code       string
	Sort       int32
}

// TermInput 是取值的新建与整体保存。
type TermInput struct {
	Name      string
	Slug      string
	Status    ActiveStatus
	IsChannel bool
	Code      string
}

// ---------- 专题 ----------

// TopicStatus 是专题状态。
type TopicStatus string

const (
	TopicDraft     TopicStatus = "draft"
	TopicPublished TopicStatus = "published"
)

// SourceType 是专题的剧来源：manual 手选、auto 按规则自动选（规则在 S1-4 执行）。
type SourceType string

const (
	SourceManual SourceType = "manual"
	SourceAuto   SourceType = "auto"
)

// TopicRule 是自动专题的选剧规则：同时满足全部取值编号与标签，按 Sort 排序取前 Limit 部；Window 为上新窗口。
type TopicRule struct {
	TermCodes []string
	TagID     *int64
	Sort      DramaSort // latest / hot / recommend
	Window    string    // all / 7d / 14d / 30d / 90d
	Limit     int       // 1–60
}

// 自动专题规则的默认值：按上架时间、不限上新时间、取 24 部。
const (
	defaultTopicLimit = 24
	maxTopicLimit     = 60
)

// WithDefaults 补齐未设置的规则项（库中的 {} 也按默认规则解释）。
func (r TopicRule) WithDefaults() TopicRule {
	if r.TermCodes == nil {
		r.TermCodes = []string{}
	}
	if r.Sort == SortDefault {
		r.Sort = SortLatest
	}
	if r.Window == "" {
		r.Window = "all"
	}
	if r.Limit == 0 {
		r.Limit = defaultTopicLimit
	}
	return r
}

// FAQ 是专题页的一组问答（纯文本）。
type FAQ struct {
	Question string
	Answer   string
}

// Topic 是专题。
type Topic struct {
	ID          int64
	Title       string
	Slug        string
	Summary     string
	Status      TopicStatus
	SourceType  SourceType
	Rule        TopicRule
	FAQ         []FAQ
	ListInIndex bool
	CreatedAt   time.Time
}

// TopicInput 是专题的新建与整体保存。
type TopicInput struct {
	Title       string
	Slug        string
	Summary     string
	Status      TopicStatus
	SourceType  SourceType
	Rule        TopicRule
	FAQ         []FAQ
	ListInIndex bool
}

// TopicDrama 是手选专题中的一部剧（按展示顺序）。
type TopicDrama struct {
	ID            int64
	Title         string
	Status        DramaStatus
	PublishAt     *time.Time
	CoverPortrait string
}

// TopicDetail 是专题编辑页的数据。
type TopicDetail struct {
	Topic
	Dramas []TopicDrama
}

// TopicFilter 是专题列表的筛选：Q 匹配标题、简介或 slug。
type TopicFilter struct {
	Q          string
	Status     TopicStatus
	SourceType SourceType
	PageRequest
}

// TopicQuery 是校验后交给仓储的查询。
type TopicQuery struct {
	Pattern    string
	Status     TopicStatus
	SourceType SourceType
	Limit      int
	Offset     int
}
