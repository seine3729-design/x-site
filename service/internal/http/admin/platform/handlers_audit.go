package platform

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// AuditService 是审计日志的查询与校验（domain/platform.AuditService）。
type AuditService interface {
	List(ctx context.Context, f dom.AuditFilter) ([]dom.AuditItem, int64, int, int, error)
	Detail(ctx context.Context, id int64) (dom.AuditDetail, error)
	Facets(ctx context.Context) (dom.AuditFacets, error)
	VerifyAuditChain(ctx context.Context, max int) (dom.AuditVerifyResult, error)
}

type auditListQuery struct {
	Q          string `form:"q"`
	Action     string `form:"action"`
	TargetType string `form:"target_type"`
	AdminID    int64  `form:"admin_id"`
	From       string `form:"from"`
	To         string `form:"to"`
	Page       int    `form:"page"`
	PageSize   int    `form:"page_size"`
}

// ListAudit 是 GET /api/admin/audit：搜操作人、动作或对象，按动作 / 对象类型 / 操作人 / 日期筛选。
func (h *Handlers) ListAudit(c *gin.Context) {
	var q auditListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	from, err := parseDayBound(q.From)
	if err != nil {
		respond.Error(c, err)
		return
	}
	to, err := parseDayBound(q.To)
	if err != nil {
		respond.Error(c, err)
		return
	}
	list, total, page, size, err := h.Audit.List(c.Request.Context(), dom.AuditFilter{
		Q: q.Q, Action: q.Action, TargetType: q.TargetType, AdminID: q.AdminID,
		From: from, To: to, Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"list": list, "total": total, "page": page, "page_size": size})
}

// GetAudit 是 GET /api/admin/audit/:id：单条详情，含改动前后对比。
func (h *Handlers) GetAudit(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	out, err := h.Audit.Detail(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

// AuditFacets 是 GET /api/admin/audit/facets：筛选下拉的可选项（出现过的动作、对象类型与操作人）。
func (h *Handlers) AuditFacets(c *gin.Context) {
	out, err := h.Audit.Facets(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

type auditVerifyQuery struct {
	Max int `form:"max"`
}

// VerifyAudit 是 GET /api/admin/audit/verify：校验哈希链，报告第一处被改动或删除的位置。
//
// 是读请求（只要 audit.view）：它不改数据。审计页没有 .edit 权限点（迁移 00003 把 audit 排除在外）。
func (h *Handlers) VerifyAudit(c *gin.Context) {
	var q auditVerifyQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	out, err := h.Audit.VerifyAuditChain(c.Request.Context(), q.Max)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

// parseDayBound 解析日期筛选的边界。空串返回 nil（不限）。
//
// 接受两种写法：
//   - 完整的 RFC3339（如 2026-10-25T00:00:00+08:00）：**后台页面用这种**。
//     时区由调用方给出，「今天」是用户所在时区的今天。
//   - 裸日期（如 2026-10-24）：按服务端时区的零点解释。容器里是 UTC，所以它表示 UTC 的那一天 ——
//     跨时区时和用户看到的「今天」会差几个小时，只适合脚本按 UTC 取整天。
func parseDayBound(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return &t, nil
	}
	if t, err := time.ParseInLocation(time.DateOnly, s, time.Local); err == nil {
		return &t, nil
	}
	return nil, errs.ErrBadRequest.WithMessage("日期格式应为 2026-10-24 或带时区的完整时间（如 2026-10-25T00:00:00+08:00）")
}
