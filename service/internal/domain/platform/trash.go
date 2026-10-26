package platform

import (
	"context"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// 回收站（docs/06 第 35 节，S4-7 / A4-3）。
//
// 哪些数据进回收站由 docs/06 第 35 节划定：**设置类不进**（菜单、广告、专题、线路、资源机等，
// 误删看审计日志重建），只有内容类进。目前只有剧集实现了软删（`drama.deleted_at`，迁移 00004），
// 所以回收站当下只有「剧集」一类；用户随 A2-2、其余随各自模块接入。
//
// 分类做成列表而不是写死剧集：页面要「按分类查看」，以后加一类只需在这里登记 + 实现一个仓储方法。

// TrashCategory 是回收站的分类。
type TrashCategory string

const (
	// TrashDramas 是剧集。彻底删除会级联删掉分集、标签与维度关联、专题关联与排行干预；
	// 分集引用的转码产物随之失去引用，由孤立产物清理任务（S1-6f）回收存储上的文件。
	TrashDramas TrashCategory = "dramas"
)

// TrashCategoryInfo 描述一个分类，供页面渲染标签页。
type TrashCategoryInfo struct {
	Category TrashCategory `json:"category"`
	Title    string        `json:"title"`
	Desc     string        `json:"desc"`
	Count    int           `json:"count"`
}

// trashCategories 按页面上的展示顺序排列。
var trashCategories = []TrashCategoryInfo{
	{
		Category: TrashDramas, Title: "剧集",
		Desc: "还原会把分集与标签、维度、专题关联一起带回来。彻底删除后分集与关联一并消失，转码产物由后台任务回收。",
	},
}

// TrashItem 是回收站里的一项。
type TrashItem struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Slug      string    `json:"slug"`
	Cover     string    `json:"cover"`
	Status    string    `json:"status"` // 删除前的状态（还原后恢复成它）
	Extra     int       `json:"extra"`  // 附带内容的条数，剧集为分集数
	DeletedAt time.Time `json:"deleted_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// ExpiresAt 是到期自动彻底删除的时间，DaysLeft 是剩余天数（0 表示今天就到期）。
	ExpiresAt time.Time `json:"expires_at"`
	DaysLeft  int       `json:"days_left"`
}

// TrashFilter 是回收站列表的筛选。
type TrashFilter struct {
	Category TrashCategory
	Q        string
	Page     int
	PageSize int
}

// TrashQuery 是校验后交给仓储的查询。
type TrashQuery struct {
	Pattern string
	Limit   int
	Offset  int
}

// TrashOverview 是回收站概览：各分类的条数与保留期。
type TrashOverview struct {
	RetentionDays int                 `json:"retention_days"`
	Categories    []TrashCategoryInfo `json:"categories"`
	// OldestExpiresAt 是最近一条到期的时间；回收站为空时为零值。
	OldestExpiresAt *time.Time `json:"oldest_expires_at"`
}

// TrashRepo 读写回收站。
type TrashRepo interface {
	ListTrashedDramas(ctx context.Context, q TrashQuery) ([]TrashItem, int64, error)
	RestoreDrama(ctx context.Context, id int64) (TrashItem, error)
	PurgeDrama(ctx context.Context, id int64) (TrashItem, error)
	TrashedDrama(ctx context.Context, id int64) (TrashItem, error)
	TrashCounts(ctx context.Context) (dramas int, oldestDeletedAt *time.Time, err error)
	PurgeExpiredDramas(ctx context.Context, before time.Time) ([]TrashItem, error)
}

// TrashService 是后台回收站（docs/06 第 35 节）。
type TrashService struct {
	repo      TrashRepo
	retention time.Duration
	now       func() time.Time
}

func NewTrashService(repo TrashRepo, retention time.Duration) *TrashService {
	if retention <= 0 {
		retention = DefaultTrashRetention
	}
	return &TrashService{repo: repo, retention: retention, now: time.Now}
}

// DefaultTrashRetention 是回收站的默认保留期（docs/06 第 35 节：30 天内可还原）。
const DefaultTrashRetention = 30 * 24 * time.Hour

const (
	trashDefaultPageSize = 20
	trashMaxPageSize     = 100
	trashMaxPage         = 10000
	trashMaxQueryRunes   = 100
)

var trashLikeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Retention 返回保留期。
func (s *TrashService) Retention() time.Duration { return s.retention }

// Overview 返回各分类的条数与保留期。
func (s *TrashService) Overview(ctx context.Context) (TrashOverview, error) {
	dramas, oldest, err := s.repo.TrashCounts(ctx)
	if err != nil {
		return TrashOverview{}, err
	}
	out := TrashOverview{
		RetentionDays: int(s.retention.Hours() / 24),
		Categories:    make([]TrashCategoryInfo, len(trashCategories)),
	}
	copy(out.Categories, trashCategories)
	for i := range out.Categories {
		if out.Categories[i].Category == TrashDramas {
			out.Categories[i].Count = dramas
		}
	}
	if oldest != nil {
		expires := oldest.Add(s.retention)
		out.OldestExpiresAt = &expires
	}
	return out, nil
}

// List 返回某个分类下的一页。
func (s *TrashService) List(ctx context.Context, f TrashFilter) ([]TrashItem, int64, int, int, error) {
	if err := checkCategory(f.Category); err != nil {
		return nil, 0, 0, 0, err
	}
	q, page, size, err := cleanTrashFilter(f)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	list, total, err := s.repo.ListTrashedDramas(ctx, q)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	now := s.now()
	for i := range list {
		s.fillExpiry(&list[i], now)
	}
	return list, total, page, size, nil
}

// Restore 还原一项。超过保留期的也允许还原：清理任务可能还没跑到，此时东西还在，能救就救。
func (s *TrashService) Restore(ctx context.Context, category TrashCategory, id int64) (TrashItem, error) {
	if err := checkCategory(category); err != nil {
		return TrashItem{}, err
	}
	item, err := s.repo.RestoreDrama(ctx, id)
	if err != nil {
		return TrashItem{}, err
	}
	return item, nil
}

// Purge 彻底删除一项（不可恢复）。
func (s *TrashService) Purge(ctx context.Context, category TrashCategory, id int64) (TrashItem, error) {
	if err := checkCategory(category); err != nil {
		return TrashItem{}, err
	}
	// 先读出来：删除后拿不到标题与分集数，而审计与提示都要用
	before, err := s.repo.TrashedDrama(ctx, id)
	if err != nil {
		return TrashItem{}, err
	}
	s.fillExpiry(&before, s.now())
	if _, err := s.repo.PurgeDrama(ctx, id); err != nil {
		return TrashItem{}, err
	}
	return before, nil
}

// PurgeExpired 删除所有已过保留期的项，返回被删的内容（清理任务调用）。
func (s *TrashService) PurgeExpired(ctx context.Context, now time.Time) ([]TrashItem, error) {
	return s.repo.PurgeExpiredDramas(ctx, now.Add(-s.retention))
}

// fillExpiry 算出到期时间与剩余天数。
//
// 剩余天数向上取整：还剩 0.5 天显示「1 天」而不是「今天到期」——
// 宁可让运维觉得时间比实际多一点点，也不要让他以为已经没救了。
func (s *TrashService) fillExpiry(it *TrashItem, now time.Time) {
	it.ExpiresAt = it.DeletedAt.Add(s.retention)
	left := it.ExpiresAt.Sub(now)
	if left <= 0 {
		it.DaysLeft = 0
		return
	}
	it.DaysLeft = int(math.Ceil(left.Hours() / 24))
}

func checkCategory(c TrashCategory) error {
	for _, info := range trashCategories {
		if info.Category == c {
			return nil
		}
	}
	names := make([]string, 0, len(trashCategories))
	for _, info := range trashCategories {
		names = append(names, string(info.Category))
	}
	return badRequest("回收站分类无效（可选：%s）", strings.Join(names, "、"))
}

func cleanTrashFilter(f TrashFilter) (TrashQuery, int, int, error) {
	var q TrashQuery
	word := strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, f.Q))
	if utf8.RuneCountInString(word) > trashMaxQueryRunes {
		return TrashQuery{}, 0, 0, badRequest("搜索词不能超过 %d 个字", trashMaxQueryRunes)
	}
	if word != "" {
		q.Pattern = "%" + trashLikeEscaper.Replace(word) + "%"
	}
	page, size := f.Page, f.PageSize
	if page == 0 {
		page = 1
	}
	if size == 0 {
		size = trashDefaultPageSize
	}
	if page < 1 || page > trashMaxPage || size < 1 || size > trashMaxPageSize {
		return TrashQuery{}, 0, 0, badRequest("分页参数错误：page 为 1–%d，page_size 为 1–%d", trashMaxPage, trashMaxPageSize)
	}
	q.Limit, q.Offset = size, (page-1)*size
	return q, page, size, nil
}
