package ops

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/ops"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// permCustomHTML 是新建、修改 HTML 素材所需的权限点（默认只有超级管理员），在 handler 内按素材类型判断。
const permCustomHTML = "ads.custom_html"

type adListQuery struct {
	Q        string `form:"q"`
	Slot     string `form:"slot"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// adRequest 是新建与整体保存的请求体（未传的可选字段按默认值保存）。
type adRequest struct {
	Name         string     `json:"name"`
	Slot         string     `json:"slot"`
	CreativeType string     `json:"creative_type"`
	Image        string     `json:"image"`
	VideoURL     string     `json:"video_url"`
	Text         string     `json:"text"`
	HTML         string     `json:"html"`
	Alt          string     `json:"alt"`
	ClickURL     string     `json:"click_url"`
	Device       string     `json:"device"`
	Locales      []string   `json:"locales"`
	Weight       int        `json:"weight"`
	EndAt        *time.Time `json:"end_at"`
	FreqCap      int        `json:"freq_cap"`
	Status       string     `json:"status"`
}

func (r adRequest) input() dom.AdInput {
	return dom.AdInput{
		Name: r.Name, Slot: dom.AdSlot(r.Slot), CreativeType: dom.CreativeType(r.CreativeType), Image: r.Image, VideoURL: r.VideoURL,
		Text: r.Text, HTML: r.HTML, Alt: r.Alt, ClickURL: r.ClickURL, Device: dom.AdDevice(r.Device), Locales: r.Locales,
		Weight: r.Weight, EndAt: r.EndAt, FreqCap: r.FreqCap, Status: dom.Status(r.Status),
	}
}

type adDTO struct {
	ID            int64      `json:"id"`
	Name          string     `json:"name"`
	Slot          string     `json:"slot"`
	CreativeType  string     `json:"creative_type"`
	Image         string     `json:"image"`
	VideoURL      string     `json:"video_url"`
	Text          string     `json:"text"`
	HTML          string     `json:"html"`
	Alt           string     `json:"alt"`
	ClickURL      string     `json:"click_url"`
	Device        string     `json:"device"`
	Locales       []string   `json:"locales"`
	Weight        int        `json:"weight"`
	EndAt         *time.Time `json:"end_at"`
	FreqCap       int        `json:"freq_cap"`
	Status        string     `json:"status"`
	DisplayStatus string     `json:"display_status"`
	ImpCount      int64      `json:"imp_count"`
	ClickCount    int64      `json:"click_count"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func adOf(now time.Time) func(dom.Ad) adDTO {
	return func(a dom.Ad) adDTO {
		return adDTO{
			ID: a.ID, Name: a.Name, Slot: string(a.Slot), CreativeType: string(a.CreativeType), Image: a.Image, VideoURL: a.VideoURL,
			Text: a.Text, HTML: a.HTML, Alt: a.Alt, ClickURL: a.ClickURL, Device: string(a.Device), Locales: strs(a.Locales),
			Weight: a.Weight, EndAt: a.EndAt, FreqCap: a.FreqCap, Status: string(a.Status), DisplayStatus: string(a.DisplayStatus(now)),
			ImpCount: a.ImpCount, ClickCount: a.ClickCount, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		}
	}
}

// canCustomHTML 表示当前管理员能否新建、修改 HTML 素材。
func canCustomHTML(c *gin.Context) bool {
	id, ok := admin.CurrentAdmin(c)
	return ok && id.Can(permCustomHTML)
}

// ListAds 是 GET /api/admin/ads：搜广告名或跳转网址，按位置、状态筛选，页码分页（新的在前）。
func (h *Handlers) ListAds(c *gin.Context) {
	var q adListQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Ads.List(c.Request.Context(), dom.AdFilter{
		Q: q.Q, Slot: dom.AdSlot(q.Slot), Status: dom.Status(q.Status),
		PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page, adOf(h.now())))
}

// CreateAd 是 POST /api/admin/ads：新建后前台立即生效；HTML 素材要 ads.custom_html。
func (h *Handlers) CreateAd(c *gin.Context) {
	var req adRequest
	if !admin.Bind(c, &req) {
		return
	}
	a, err := h.Ads.Create(c.Request.Context(), req.input(), canCustomHTML(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := adOf(h.now())(a)
	admin.SetAudit(c, admin.AuditDetail{Action: "ad.create", TargetType: "ad", TargetID: itoa(a.ID), After: out})
	respond.OK(c, out)
}

// UpdateAd 是 PATCH /api/admin/ads/:id：整体保存；改成或原本是 HTML 素材时要 ads.custom_html。
func (h *Handlers) UpdateAd(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req adRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Ads.Update(c.Request.Context(), id, req.input(), canCustomHTML(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	dto := adOf(h.now())
	out := dto(after)
	admin.SetAudit(c, admin.AuditDetail{Action: "ad.update", TargetType: "ad", TargetID: itoa(id), Before: dto(before), After: out})
	respond.OK(c, out)
}

// DeleteAd 是 DELETE /api/admin/ads/:id：不可恢复（设置类数据不进回收站）。
func (h *Handlers) DeleteAd(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Ads.Delete(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "ad.delete", TargetType: "ad", TargetID: itoa(id), Before: adOf(h.now())(before)})
	respond.OK(c, nil)
}
