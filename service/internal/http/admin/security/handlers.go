// Package security 是运营后台「安全中心」接口（docs/07 第 2 节，docs/06 第 39 节，A4-4 / S4-8）。
// 纯只读：登录记录、密钥轮换记录、风控概览，统一要 security.view。
package security

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/security"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

type loginLogsQuery struct {
	Result   string `form:"result"`
	Q        string `form:"q"`
	From     string `form:"from"`
	To       string `form:"to"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// LoginLogs 是 GET /api/admin/security/login-logs：管理员登录记录（页码分页，倒序）。
func (h *Handlers) LoginLogs(c *gin.Context) {
	var q loginLogsQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	f := dom.LoginLogFilter{Result: q.Result, Q: q.Q, Page: q.Page, PageSize: q.PageSize}
	since, err := parseTime(q.From)
	if err != nil {
		respond.Error(c, err)
		return
	}
	until, err := parseTime(q.To)
	if err != nil {
		respond.Error(c, err)
		return
	}
	f.Since, f.Until = since, until
	page, err := h.Svc.LoginLogs(c.Request.Context(), f)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, page)
}

// KeyRotations 是 GET /api/admin/security/key-rotations：当前启用的各密钥版本（只有版本号与用途）。
func (h *Handlers) KeyRotations(c *gin.Context) {
	respond.OK(c, gin.H{"list": h.Svc.KeyRotations(c.Request.Context())})
}

// Risk 是 GET /api/admin/security/risk：风控概览。
func (h *Handlers) Risk(c *gin.Context) {
	respond.OK(c, h.Svc.Risk(c.Request.Context()))
}

// parseTime 解析筛选时间：优先 RFC3339（带时区），其次按 UTC 解释 YYYY-MM-DD；空串表示不限。
func parseTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		u := t.UTC()
		return &u, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		u := t.UTC()
		return &u, nil
	}
	return nil, errs.ErrBadRequest.WithMessage("时间格式不正确，请用 RFC3339 或 YYYY-MM-DD")
}
