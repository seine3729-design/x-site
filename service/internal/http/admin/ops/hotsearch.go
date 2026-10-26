package ops

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/ops"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type hotListQuery struct {
	Q        string `form:"q"`
	Visible  string `form:"visible"`
	Pinned   string `form:"pinned"`
	Missing  string `form:"missing"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

type hotCreateRequest struct {
	Word string `json:"word"`
}

// hotPatchRequest 是部分修改：只传要改的项。
type hotPatchRequest struct {
	Visible *bool `json:"visible"`
	Pinned  *bool `json:"pinned"`
}

type hotReorderRequest struct {
	IDs []int64 `json:"ids"`
}

type hotWordDTO struct {
	ID             int64      `json:"id"`
	Word           string     `json:"word"`
	SearchCount    int64      `json:"search_count"`
	MissCount      int64      `json:"miss_count"`
	Pinned         bool       `json:"pinned"`
	PinSort        int        `json:"pin_sort"`
	Visible        bool       `json:"visible"`
	Source         string     `json:"source"`
	LastSearchedAt *time.Time `json:"last_searched_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func hotWordOf(w dom.HotWord) hotWordDTO {
	return hotWordDTO{
		ID: w.ID, Word: w.Word, SearchCount: w.SearchCount, MissCount: w.MissCount, Pinned: w.Pinned, PinSort: w.PinSort,
		Visible: w.Visible, Source: string(w.Source), LastSearchedAt: w.LastSearchedAt, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt,
	}
}

// optionalBool 解析可选的 true / false 查询参数；不传返回 nil。
func optionalBool(name, v string) (*bool, error) {
	switch v {
	case "":
		return nil, nil
	case "true", "false":
		b := v == "true"
		return &b, nil
	}
	return nil, errs.ErrBadRequest.WithMessage(name + " 只能是 true 或 false")
}

// ListHotWords 是 GET /api/admin/hotsearch：搜词，按显示、置顶、片库没有筛选；置顶的在前，其余按搜索次数倒序。
func (h *Handlers) ListHotWords(c *gin.Context) {
	var q hotListQuery
	if !bindQuery(c, &q) {
		return
	}
	visible, err := optionalBool("visible", q.Visible)
	if err != nil {
		respond.Error(c, err)
		return
	}
	pinned, err := optionalBool("pinned", q.Pinned)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if q.Missing != "" && q.Missing != "true" {
		respond.Error(c, errs.ErrBadRequest.WithMessage("missing 只能是 true（只看片库没有的词）"))
		return
	}
	page, err := h.Hotsearch.List(c.Request.Context(), dom.HotFilter{
		Q: q.Q, Visible: visible, Pinned: pinned, Missing: q.Missing == "true",
		PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page, hotWordOf))
}

// CreateHotWord 是 POST /api/admin/hotsearch：手动加词（规范化后保存，默认显示），前台立即生效。
func (h *Handlers) CreateHotWord(c *gin.Context) {
	var req hotCreateRequest
	if !admin.Bind(c, &req) {
		return
	}
	w, err := h.Hotsearch.Create(c.Request.Context(), req.Word)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := hotWordOf(w)
	admin.SetAudit(c, admin.AuditDetail{Action: "hotsearch.create", TargetType: "hotsearch", TargetID: itoa(w.ID), After: out})
	respond.OK(c, out)
}

// UpdateHotWord 是 PATCH /api/admin/hotsearch/:id：改显示或置顶（置顶时自动显示，不显示时同时取消置顶）。
func (h *Handlers) UpdateHotWord(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req hotPatchRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Hotsearch.Update(c.Request.Context(), id, dom.HotPatch{Visible: req.Visible, Pinned: req.Pinned})
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := hotWordOf(after)
	admin.SetAudit(c, admin.AuditDetail{Action: "hotsearch.update", TargetType: "hotsearch", TargetID: itoa(id), Before: hotWordOf(before), After: out})
	respond.OK(c, out)
}

// ReorderHotWords 是 POST /api/admin/hotsearch/reorder：按 ids 的顺序重排置顶词（必须恰好是当前全部置顶词，否则 409）。
func (h *Handlers) ReorderHotWords(c *gin.Context) {
	var req hotReorderRequest
	if !admin.Bind(c, &req) {
		return
	}
	if err := h.Hotsearch.Reorder(c.Request.Context(), req.IDs); err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "hotsearch.reorder", TargetType: "hotsearch", After: gin.H{"ids": req.IDs}})
	respond.OK(c, nil)
}

// DeleteHotWord 是 DELETE /api/admin/hotsearch/:id：连同搜索次数删除，之后再被搜到会重新收录。
func (h *Handlers) DeleteHotWord(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Hotsearch.Delete(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "hotsearch.delete", TargetType: "hotsearch", TargetID: itoa(id), Before: hotWordOf(before)})
	respond.OK(c, nil)
}
