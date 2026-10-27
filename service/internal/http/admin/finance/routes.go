// Package finance 是运营后台「收入与成本」接口（docs/06 第 23 节，A4-1 / S4-3）。
// 按月读写手填收支 + 会员收入自动汇总；会话 / CSRF / 审计由 admin.Routes 提供，按 finance 页声明权限。
package finance

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/finance"
	"xsite/service/internal/http/admin"
)

// Service 是收入与成本服务（domain/finance.Service）。
type Service interface {
	Report(ctx context.Context, month string) (dom.Report, error)
	Save(ctx context.Context, month string, in dom.Input) (dom.Report, error)
}

// Handlers 聚合收入与成本 handler。
type Handlers struct {
	Svc Service
}

// Routes 注册收入与成本接口：读要 finance.view，写要 finance.edit（非高敏）。
func (h *Handlers) Routes(authed gin.IRouter) {
	page := admin.RequirePage("finance")
	g := authed.Group("/finance")
	g.GET("", page, h.Get)
	g.PUT("", page, h.Save)
}
