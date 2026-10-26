// Package resource 是运营后台的资源机接口（docs/07 第 2 节「资源机」，S1-6，docs/specs/S1-6.md）。
// 会话、CSRF、审计中间件由 admin.Routes 返回的分组提供，这里按页面声明权限点。
package resource

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/resource"
	"xsite/service/internal/http/admin"
)

// Service 是资源机服务（domain/resource.Service）。
type Service interface {
	ListFTPServers(ctx context.Context, f dom.Filter) (dom.Page[dom.FTPServer], error)
	CreateFTPServer(ctx context.Context, in dom.FTPServerInput) (dom.FTPServer, error)
	UpdateFTPServer(ctx context.Context, id int64, in dom.FTPServerInput) (before, after dom.FTPServer, err error)
	DeleteFTPServer(ctx context.Context, id int64) (dom.FTPServer, error)
	TestFTPServer(ctx context.Context, id int64) (dom.ProbeResult, error)
	ListFTPDir(ctx context.Context, id int64, path string) (dom.ProbeResult, error)
}

// Handlers 聚合资源机 handler。
type Handlers struct {
	Svc Service
}

// Routes 把资源机接口注册到 admin.Routes 返回的分组上。增删改要 ftp.edit（高敏，S2-6 起需重新验证）；
// 测连与列目录不改数据，只要 ftp.view，但仍是写请求：要 CSRF 令牌，并记审计（谁让 worker 连了哪台资源机）。
func (h *Handlers) Routes(authed gin.IRouter) {
	page := admin.RequirePage("ftp")
	view := admin.RequirePermission("ftp.view")

	ftp := authed.Group("/ftp")
	ftp.GET("", page, h.ListFTPServers)
	ftp.POST("", page, h.CreateFTPServer)
	ftp.PATCH("/:id", page, h.UpdateFTPServer)
	ftp.DELETE("/:id", page, h.DeleteFTPServer)
	ftp.POST("/:id/test", view, h.TestFTPServer)
	ftp.POST("/:id/list", view, h.ListFTPDir)
}
