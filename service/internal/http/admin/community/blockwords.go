package community

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type blockwordListQuery struct {
	Q        string `form:"q"`
	Group    string `form:"group"`
	Action   string `form:"action"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

type blockwordCreateRequest struct {
	Word   string `json:"word"`
	Group  string `json:"group"`
	Action string `json:"action"`
}

type blockwordDTO struct {
	ID        int64     `json:"id"`
	Word      string    `json:"word"`
	Group     string    `json:"group"`
	Action    string    `json:"action"`
	CreatedAt time.Time `json:"created_at"`
}

func blockwordOf(b dom.Blockword) blockwordDTO {
	return blockwordDTO{ID: b.ID, Word: b.Word, Group: b.Group, Action: string(b.Action), CreatedAt: b.CreatedAt}
}

// ListBlockwords 是 GET /api/admin/blockwords：按分组、处理办法、词筛选，按加入时间倒序。
func (h *Handlers) ListBlockwords(c *gin.Context) {
	var q blockwordListQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Blockwords.List(c.Request.Context(), dom.BlockwordFilter{
		Q: q.Q, Group: q.Group, Action: q.Action,
		PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page, blockwordOf))
}

// ListBlockwordGroups 是 GET /api/admin/blockwords/groups：已有分组（供筛选下拉）。
func (h *Handlers) ListBlockwordGroups(c *gin.Context) {
	groups, err := h.Blockwords.Groups(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	if groups == nil {
		groups = []string{}
	}
	respond.OK(c, gin.H{"groups": groups})
}

// CreateBlockword 是 POST /api/admin/blockwords：加词（规范化后保存），前台立即生效；重复词 409。
func (h *Handlers) CreateBlockword(c *gin.Context) {
	var req blockwordCreateRequest
	if !admin.Bind(c, &req) {
		return
	}
	bw, err := h.Blockwords.Create(c.Request.Context(), req.Word, req.Group, req.Action)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := blockwordOf(bw)
	admin.SetAudit(c, admin.AuditDetail{Action: "blockword.create", TargetType: "blockword", TargetID: itoa(bw.ID), After: out})
	respond.OK(c, out)
}

// DeleteBlockword 是 DELETE /api/admin/blockwords/:id：删词（改处理办法请先删后加），前台立即生效。
func (h *Handlers) DeleteBlockword(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	bw, err := h.Blockwords.Delete(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "blockword.delete", TargetType: "blockword", TargetID: itoa(id), Before: blockwordOf(bw)})
	respond.OK(c, nil)
}
