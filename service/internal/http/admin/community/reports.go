package community

import (
	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type reportListQuery struct {
	Q          string `form:"q"`
	Status     string `form:"status"`
	TargetType string `form:"target_type"`
	Reason     string `form:"reason"`
	Page       int    `form:"page"`
	PageSize   int    `form:"page_size"`
}

type reportHandleRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func reportOf(v dom.ReportView) dom.ReportView { return v }

// ListReports 是 GET /api/admin/reports：举报队列（§25），按状态 / 对象类型 / 原因筛选，Q 搜说明或举报人用户名。
func (h *Handlers) ListReports(c *gin.Context) {
	var q reportListQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Reports.List(c.Request.Context(), dom.ReportFilter{
		Q: q.Q, Status: q.Status, TargetType: q.TargetType, Reason: q.Reason,
		PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page, reportOf))
}

// HandleReport 是 POST /api/admin/reports/:id/handle：保存处理结果（已处理 / 已驳回 + 处理说明）。
// 处罚举报人 / 被举报人（改账号状态）依赖用户管理（§26），本接口不含。
func (h *Handlers) HandleReport(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	var req reportHandleRequest
	if !admin.Bind(c, &req) {
		return
	}
	v, err := h.Reports.Handle(c.Request.Context(), id, req.Status, req.Note)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "report.handle", TargetType: "report", TargetID: itoa(id),
		After: gin.H{"status": v.Status, "note": v.HandleNote},
	})
	respond.OK(c, v)
}
