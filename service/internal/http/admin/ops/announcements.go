package ops

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/ops"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type announcementListQuery struct {
	Q        string `form:"q"`
	Kind     string `form:"kind"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// announcementRequest 是新建与整体保存的请求体（未传的可选字段按默认值保存）。
type announcementRequest struct {
	Kind      string     `json:"kind"`
	Content   string     `json:"content"`
	ClickURL  string     `json:"click_url"`
	Frequency string     `json:"frequency"`
	Pages     []string   `json:"pages"`
	Locales   []string   `json:"locales"`
	Image     string     `json:"image"`
	StartAt   *time.Time `json:"start_at"`
	EndAt     *time.Time `json:"end_at"`
	Status    string     `json:"status"`
}

func (r announcementRequest) input() dom.AnnouncementInput {
	return dom.AnnouncementInput{
		Kind: dom.AnnouncementKind(r.Kind), Content: r.Content, ClickURL: r.ClickURL, Frequency: dom.Frequency(r.Frequency),
		Pages: r.Pages, Locales: r.Locales, Image: r.Image, StartAt: r.StartAt, EndAt: r.EndAt, Status: dom.Status(r.Status),
	}
}

type announcementDTO struct {
	ID            int64      `json:"id"`
	Kind          string     `json:"kind"`
	Content       string     `json:"content"`
	ClickURL      string     `json:"click_url"`
	Frequency     string     `json:"frequency"`
	Pages         []string   `json:"pages"`
	Locales       []string   `json:"locales"`
	Image         string     `json:"image"`
	StartAt       *time.Time `json:"start_at"`
	EndAt         *time.Time `json:"end_at"`
	Status        string     `json:"status"`
	DisplayStatus string     `json:"display_status"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func announcementOf(now time.Time) func(dom.Announcement) announcementDTO {
	return func(a dom.Announcement) announcementDTO {
		return announcementDTO{
			ID: a.ID, Kind: string(a.Kind), Content: a.Content, ClickURL: a.ClickURL, Frequency: string(a.Frequency),
			Pages: strs(a.Pages), Locales: strs(a.Locales), Image: a.Image, StartAt: a.StartAt, EndAt: a.EndAt,
			Status: string(a.Status), DisplayStatus: string(a.DisplayStatus(now)), CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		}
	}
}

// ListAnnouncements 是 GET /api/admin/announcements：搜内容或跳转网址，按方式、状态筛选，页码分页（新的在前）。
func (h *Handlers) ListAnnouncements(c *gin.Context) {
	var q announcementListQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Announcements.List(c.Request.Context(), dom.AnnouncementFilter{
		Q: q.Q, Kind: dom.AnnouncementKind(q.Kind), Status: dom.Status(q.Status),
		PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page, announcementOf(h.now())))
}

// CreateAnnouncement 是 POST /api/admin/announcements：新建后前台立即生效。
func (h *Handlers) CreateAnnouncement(c *gin.Context) {
	var req announcementRequest
	if !admin.Bind(c, &req) {
		return
	}
	a, err := h.Announcements.Create(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := announcementOf(h.now())(a)
	admin.SetAudit(c, admin.AuditDetail{Action: "announcement.create", TargetType: "announcement", TargetID: itoa(a.ID), After: out})
	respond.OK(c, out)
}

// UpdateAnnouncement 是 PATCH /api/admin/announcements/:id：整体保存。
func (h *Handlers) UpdateAnnouncement(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req announcementRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Announcements.Update(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	dto := announcementOf(h.now())
	out := dto(after)
	admin.SetAudit(c, admin.AuditDetail{Action: "announcement.update", TargetType: "announcement", TargetID: itoa(id), Before: dto(before), After: out})
	respond.OK(c, out)
}

// DeleteAnnouncement 是 DELETE /api/admin/announcements/:id：不可恢复（设置类数据不进回收站）。
func (h *Handlers) DeleteAnnouncement(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Announcements.Delete(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "announcement.delete", TargetType: "announcement", TargetID: itoa(id), Before: announcementOf(h.now())(before)})
	respond.OK(c, nil)
}
