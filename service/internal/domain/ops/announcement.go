package ops

import (
	"context"
	"log/slog"
	"time"
)

// AnnouncementKind 是公告方式。
type AnnouncementKind string

const (
	AnnouncementTopbar AnnouncementKind = "topbar" // 顶部滚动条
	AnnouncementPopup  AnnouncementKind = "popup"  // 弹窗
	AnnouncementInbox  AnnouncementKind = "inbox"  // 站内信，暂不启用（docs/06 第 19 节）
)

// Frequency 是公告的显示频率；「每人一次」「每人每天一次」的记录存在客户端本机。
type Frequency string

const (
	FrequencyAlways Frequency = "always"
	FrequencyOnce   Frequency = "once"
	FrequencyDaily  Frequency = "daily"
)

// AnnouncementPages 是公告可以出现的前台页面（docs/04 第 B 节）；「出现页面」为空表示全部页面。
var AnnouncementPages = []string{"home", "explore", "channel", "tag", "topic", "drama", "play", "shua", "rank", "search", "me"}

// Announcement 是一条公告。StartAt / EndAt 为空表示不限。
type Announcement struct {
	ID        int64
	Kind      AnnouncementKind
	Content   string
	ClickURL  string
	Frequency Frequency
	Pages     []string
	Locales   []string
	Image     string
	StartAt   *time.Time
	EndAt     *time.Time
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DisplayStatus 是后台列表显示的状态（由启用状态与起止时间算出，不入库）。
type DisplayStatus string

const (
	DisplayLive      DisplayStatus = "live"      // 显示中
	DisplayScheduled DisplayStatus = "scheduled" // 未到开始时间
	DisplayEnded     DisplayStatus = "ended"     // 已过结束时间
	DisplayInactive  DisplayStatus = "inactive"  // 已停用
)

// DisplayStatus 返回 now 时刻的显示状态。
func (a Announcement) DisplayStatus(now time.Time) DisplayStatus {
	switch {
	case a.Status != StatusActive:
		return DisplayInactive
	case a.EndAt != nil && !now.Before(*a.EndAt):
		return DisplayEnded
	case a.StartAt != nil && now.Before(*a.StartAt):
		return DisplayScheduled
	}
	return DisplayLive
}

// AnnouncementInput 是公告的新建与整体保存。
type AnnouncementInput struct {
	Kind      AnnouncementKind
	Content   string
	ClickURL  string
	Frequency Frequency
	Pages     []string
	Locales   []string
	Image     string
	StartAt   *time.Time
	EndAt     *time.Time
	Status    Status
}

// AnnouncementFilter 是后台列表的筛选：Q 匹配内容或跳转网址。
type AnnouncementFilter struct {
	Q      string
	Kind   AnnouncementKind
	Status Status
	PageRequest
}

// AnnouncementQuery 是校验后交给仓储的查询。Pattern 是已转义的 LIKE 模式，空表示不按文本搜。
type AnnouncementQuery struct {
	Pattern string
	Kind    AnnouncementKind
	Status  Status
	Limit   int
	Offset  int
}

// PublicAnnouncement 是下发给前台的公告（不含后台字段）。
type PublicAnnouncement struct {
	ID        int64            `json:"id"`
	Kind      AnnouncementKind `json:"kind"`
	Content   string           `json:"content"`
	ClickURL  string           `json:"click_url"`
	Frequency Frequency        `json:"frequency"`
	Pages     []string         `json:"pages"`
	Image     string           `json:"image"`
}

// AnnouncementRepo 读写公告。查不到时返回 errs.ErrNotFound。
type AnnouncementRepo interface {
	ListAnnouncements(ctx context.Context, q AnnouncementQuery) ([]Announcement, int64, error)
	GetAnnouncement(ctx context.Context, id int64) (Announcement, error)
	CreateAnnouncement(ctx context.Context, in AnnouncementInput) (Announcement, error)
	UpdateAnnouncement(ctx context.Context, id int64, in AnnouncementInput) (Announcement, error)
	// DeleteAnnouncement 删除公告（不进回收站），返回删除前的记录。
	DeleteAnnouncement(ctx context.Context, id int64) (Announcement, error)
	// ListAnnouncementCandidates 返回启用、未结束的顶部滚动条与弹窗（新的在前）。
	ListAnnouncementCandidates(ctx context.Context) ([]Announcement, error)
}

// 前台候选公告的缓存：后台增删改后立即删除，TTL 只是兜底；是否已开始、是否投给该语言在每次请求时判断。
const (
	announcementCacheKey = "ops:announcements"
	announcementCacheTTL = time.Minute
)

// AnnouncementService 是公告的后台管理与前台下发。
type AnnouncementService struct {
	repo  AnnouncementRepo
	cache Cache
	now   func() time.Time
}

// NewAnnouncementService 创建公告服务。
func NewAnnouncementService(repo AnnouncementRepo, cache Cache) *AnnouncementService {
	return &AnnouncementService{repo: repo, cache: cache, now: time.Now}
}

// List 返回后台公告列表的一页（按编号倒序）。
func (s *AnnouncementService) List(ctx context.Context, f AnnouncementFilter) (Page[Announcement], error) {
	q, err := cleanQuery(f.Q)
	if err != nil {
		return Page[Announcement]{}, err
	}
	switch f.Kind {
	case "", AnnouncementTopbar, AnnouncementPopup, AnnouncementInbox:
	default:
		return Page[Announcement]{}, badRequest("类型筛选无效")
	}
	if f.Status != "" && f.Status != StatusActive && f.Status != StatusInactive {
		return Page[Announcement]{}, badRequest("状态筛选无效")
	}
	p, limit, offset, err := limitOffset(f.PageRequest)
	if err != nil {
		return Page[Announcement]{}, err
	}
	list, total, err := s.repo.ListAnnouncements(ctx, AnnouncementQuery{
		Pattern: likePattern(q), Kind: f.Kind, Status: f.Status, Limit: limit, Offset: offset,
	})
	if err != nil {
		return Page[Announcement]{}, err
	}
	return Page[Announcement]{List: list, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// Create 新建公告，前台立即生效。
func (s *AnnouncementService) Create(ctx context.Context, in AnnouncementInput) (Announcement, error) {
	in, err := cleanAnnouncement(in)
	if err != nil {
		return Announcement{}, err
	}
	a, err := s.repo.CreateAnnouncement(ctx, in)
	if err == nil {
		s.invalidate(ctx)
	}
	return a, err
}

// Update 整体保存公告，返回修改前后的记录。
func (s *AnnouncementService) Update(ctx context.Context, id int64, in AnnouncementInput) (before, after Announcement, err error) {
	if in, err = cleanAnnouncement(in); err != nil {
		return
	}
	if before, err = s.repo.GetAnnouncement(ctx, id); err != nil {
		return
	}
	if after, err = s.repo.UpdateAnnouncement(ctx, id, in); err == nil {
		s.invalidate(ctx)
	}
	return
}

// Delete 删除公告（不可恢复），返回删除前的记录。
func (s *AnnouncementService) Delete(ctx context.Context, id int64) (Announcement, error) {
	a, err := s.repo.DeleteAnnouncement(ctx, id)
	if err == nil {
		s.invalidate(ctx)
	}
	return a, err
}

// Live 返回 locale 下当前显示中的公告（新的在前）。出现页面与频率由前台判断；同一页面有多条弹窗时前台只弹第一条。
func (s *AnnouncementService) Live(ctx context.Context, locale string) ([]PublicAnnouncement, error) {
	var candidates []Announcement
	err := s.cache.GetOrLoadJSON(ctx, announcementCacheKey, announcementCacheTTL, &candidates, func(ctx context.Context) (any, error) {
		return s.repo.ListAnnouncementCandidates(ctx)
	})
	if err != nil {
		return nil, err
	}
	locale = NormalizeLocale(locale)
	now := s.now()
	out := make([]PublicAnnouncement, 0, len(candidates))
	for _, a := range candidates {
		// 候选在缓存里最多停留一分钟，起止时间与状态每次重新判断
		if a.DisplayStatus(now) != DisplayLive || !targets(a.Locales, locale) {
			continue
		}
		pages := a.Pages
		if pages == nil {
			pages = []string{}
		}
		out = append(out, PublicAnnouncement{
			ID: a.ID, Kind: a.Kind, Content: a.Content, ClickURL: a.ClickURL, Frequency: a.Frequency, Pages: pages, Image: a.Image,
		})
	}
	return out, nil
}

// invalidate 删除前台候选缓存。删除失败只记日志：缓存最多一分钟后自然过期。
func (s *AnnouncementService) invalidate(ctx context.Context) {
	if err := s.cache.Delete(ctx, announcementCacheKey); err != nil {
		slog.WarnContext(ctx, "清除公告缓存失败，最多一分钟后生效", "err", err)
	}
}

func cleanAnnouncement(in AnnouncementInput) (AnnouncementInput, error) {
	var err error
	switch in.Kind {
	case AnnouncementTopbar, AnnouncementPopup:
	case AnnouncementInbox:
		return in, badRequest("站内信暂未开放，请选择顶部滚动条或弹窗")
	default:
		return in, badRequest("请选择公告方式：顶部滚动条或弹窗")
	}
	if in.Content, err = cleanText("公告内容", in.Content, 500, true); err != nil {
		return in, err
	}
	if in.ClickURL, err = cleanLink("跳转网址", in.ClickURL); err != nil {
		return in, err
	}
	switch in.Frequency {
	case "":
		in.Frequency = FrequencyAlways
	case FrequencyAlways, FrequencyOnce, FrequencyDaily:
	default:
		return in, badRequest("频率只能是每次、每人一次或每人每天一次")
	}
	if in.Pages, err = cleanSet("出现页面", in.Pages, AnnouncementPages); err != nil {
		return in, err
	}
	if in.Locales, err = cleanSet("投放语言", in.Locales, Locales); err != nil {
		return in, err
	}
	if in.Kind == AnnouncementPopup {
		if in.Image, err = cleanMediaRef("弹窗配图", in.Image); err != nil {
			return in, err
		}
	} else {
		in.Image = "" // 只有弹窗有配图
	}
	if in.StartAt != nil && in.EndAt != nil && !in.EndAt.After(*in.StartAt) {
		return in, badRequest("结束时间要晚于开始时间")
	}
	in.StartAt, in.EndAt = utc(in.StartAt), utc(in.EndAt)
	if in.Status, err = cleanStatus(in.Status); err != nil {
		return in, err
	}
	return in, nil
}

// utc 把时间转为 UTC（入库与输出一致）。
func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}
