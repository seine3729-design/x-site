// Package analytics 是运营后台「数据看板 / 剧集分析」接口（docs/07、docs/03 第 8 节，S4-3 / A4-1）。
// 只读 S4-2 预聚合表；会话、CSRF、审计中间件由 admin.Routes 返回的分组提供，这里按页面声明权限点。
package analytics

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/analytics"
	"xsite/service/internal/http/admin"
)

// Service 是看板与剧集分析读服务（domain/analytics.QueryService）。
type Service interface {
	Overview(ctx context.Context, days int) (dom.Overview, error)
	Dramas(ctx context.Context, in dom.DramasInput) (dom.DramasResult, error)
	DramaDetail(ctx context.Context, id int64, days int) (dom.DramaDetail, bool, error)
	Export(ctx context.Context, days int) (dom.ExportPackage, error)
}

// Handlers 聚合看板与剧集分析 handler。
type Handlers struct {
	Svc Service
}

// Routes 把数据看板与剧集分析接口注册到 admin.Routes 返回的分组上。
//
// 都是只读页面：数据看板（含导出给 Agent）要 dashboard.view，剧集分析（列表 + 单剧）要 analytics_dramas.view。
// analytics_dramas 无 .edit 权限点（00003 已排除），故用 RequirePermission 而非 RequirePage。
func (h *Handlers) Routes(authed gin.IRouter) {
	g := authed.Group("/analytics")
	g.GET("/overview", admin.RequirePermission("dashboard.view"), h.Overview)
	g.GET("/export", admin.RequirePermission("dashboard.view"), h.Export)
	g.GET("/dramas", admin.RequirePermission("analytics_dramas.view"), h.Dramas)
	g.GET("/dramas/:id", admin.RequirePermission("analytics_dramas.view"), h.DramaDetail)
}
