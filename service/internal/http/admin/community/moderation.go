package community

import (
	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type moderationListQuery struct {
	Kind      string `form:"kind"`
	Status    string `form:"status"`
	Reported  bool   `form:"reported"`
	Blockword bool   `form:"blockword"`
	Q         string `form:"q"`
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
}

type moderationActionRequest struct {
	Kind   string  `json:"kind"`
	Action string  `json:"action"`
	IDs    []int64 `json:"ids"`
}

type commentPolicyRequest struct {
	PreModeration bool `json:"pre_moderation"`
}

func moderationItemOf(v dom.ModerationItem) dom.ModerationItem { return v }

// ListModeration 是 GET /api/admin/moderation：评论 / 弹幕审核列表（§24）。
// kind=comment（默认）/ danmaku；筛选状态 / 只看被举报 / 只看含屏蔽词；Q 搜内容或用户名。
func (h *Handlers) ListModeration(c *gin.Context) {
	var q moderationListQuery
	if !bindQuery(c, &q) {
		return
	}
	kind := q.Kind
	if kind == "" {
		kind = dom.KindComment
	}
	page, err := h.Moderation.List(c.Request.Context(), kind, dom.ModerationFilter{
		Status: q.Status, Reported: q.Reported, Blockword: q.Blockword, Q: q.Q,
		PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page, moderationItemOf))
}

// ActModeration 是 POST /api/admin/moderation/actions：对一批评论 / 弹幕执行通过 / 隐藏 / 删除（§24，支持批量）。
func (h *Handlers) ActModeration(c *gin.Context) {
	var req moderationActionRequest
	if !admin.Bind(c, &req) {
		return
	}
	affected, err := h.Moderation.Act(c.Request.Context(), req.Kind, req.Action, req.IDs)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "moderation." + req.Action, TargetType: req.Kind,
		After: gin.H{"ids": req.IDs, "affected": affected},
	})
	respond.OK(c, gin.H{"affected": affected})
}

// GetCommentPolicy 是 GET /api/admin/comment-policy：读先审后显开关。
func (h *Handlers) GetCommentPolicy(c *gin.Context) {
	p, err := h.Policy.Get(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, p)
}

// SetCommentPolicy 是 PUT /api/admin/comment-policy：保存先审后显开关。
func (h *Handlers) SetCommentPolicy(c *gin.Context) {
	var req commentPolicyRequest
	if !admin.Bind(c, &req) {
		return
	}
	p, err := h.Policy.Set(c.Request.Context(), dom.CommentPolicy{PreModeration: req.PreModeration})
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "comment_policy.update", TargetType: "comment_policy", After: p})
	respond.OK(c, p)
}
