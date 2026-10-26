package site

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/site"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type pageListQuery struct {
	Q        string `form:"q"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// staticPageRequest 是新建与整体保存的请求体（未传的可选字段按默认值保存）。
type staticPageRequest struct {
	Title      string `json:"title"`
	Slug       string `json:"slug"`
	Body       string `json:"body"`
	BodyFormat string `json:"body_format"`
	Status     string `json:"status"`
	SEOTitle   string `json:"seo_title"`
	SEODesc    string `json:"seo_desc"`
	IsFooter   bool   `json:"is_footer"`
}

func (r staticPageRequest) input() dom.PageInput {
	return dom.PageInput{
		Title: r.Title, Slug: r.Slug, Body: r.Body, BodyFormat: dom.BodyFormat(r.BodyFormat), Status: dom.PageStatus(r.Status),
		SEOTitle: r.SEOTitle, SEODesc: r.SEODesc, IsFooter: r.IsFooter,
	}
}

type staticPageDTO struct {
	ID         int64     `json:"id"`
	Title      string    `json:"title"`
	Slug       string    `json:"slug"`
	Body       string    `json:"body"`
	BodyFormat string    `json:"body_format"`
	Status     string    `json:"status"`
	SEOTitle   string    `json:"seo_title"`
	SEODesc    string    `json:"seo_desc"`
	IsFooter   bool      `json:"is_footer"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func staticPageOf(p dom.StaticPage) staticPageDTO {
	return staticPageDTO{
		ID: p.ID, Title: p.Title, Slug: p.Slug, Body: p.Body, BodyFormat: string(p.BodyFormat), Status: string(p.Status),
		SEOTitle: p.SEOTitle, SEODesc: p.SEODesc, IsFooter: p.IsFooter, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
}

// ListPages 是 GET /api/admin/pages：搜标题或 slug，按状态筛选，页码分页（新的在前）。
func (h *Handlers) ListPages(c *gin.Context) {
	var q pageListQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Pages.List(c.Request.Context(), dom.PageFilter{
		Q: q.Q, Status: dom.PageStatus(q.Status), PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, listOf(page, staticPageOf))
}

// CreatePage 是 POST /api/admin/pages：新建静态页（正文保存时按白名单清洗）。
func (h *Handlers) CreatePage(c *gin.Context) {
	var req staticPageRequest
	if !admin.Bind(c, &req) {
		return
	}
	p, err := h.Pages.Create(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := staticPageOf(p)
	admin.SetAudit(c, admin.AuditDetail{Action: "page.create", TargetType: "page", TargetID: itoa(p.ID), After: out})
	respond.OK(c, out)
}

// UpdatePage 是 PATCH /api/admin/pages/:id：整体保存。
func (h *Handlers) UpdatePage(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req staticPageRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Pages.Update(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := staticPageOf(after)
	admin.SetAudit(c, admin.AuditDetail{
		Action: "page.update", TargetType: "page", TargetID: itoa(id), Before: staticPageOf(before), After: out,
	})
	respond.OK(c, out)
}

// DeletePage 是 DELETE /api/admin/pages/:id：删除不可恢复；指向它的前台菜单项随后自动隐藏。
func (h *Handlers) DeletePage(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	p, err := h.Pages.Delete(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "page.delete", TargetType: "page", TargetID: itoa(id), Before: staticPageOf(p)})
	respond.OK(c, gin.H{"id": id})
}
