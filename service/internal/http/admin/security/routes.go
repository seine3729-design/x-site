package security

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/security"
	"xsite/service/internal/http/admin"
)

// Service 是安全中心服务（domain/security.Service）。
type Service interface {
	LoginLogs(ctx context.Context, f dom.LoginLogFilter) (dom.LoginLogPage, error)
	KeyRotations(ctx context.Context) []dom.KeyVersion
	Risk(ctx context.Context) dom.RiskOverview
}

// Handlers 聚合安全中心 handler。
type Handlers struct {
	Svc Service
}

// Routes 注册安全中心接口。只读页：整组要 security.view，没有写接口。
func (h *Handlers) Routes(authed gin.IRouter) {
	g := authed.Group("/security", admin.RequirePermission("security.view"))
	g.GET("/login-logs", h.LoginLogs)
	g.GET("/key-rotations", h.KeyRotations)
	g.GET("/risk", h.Risk)
}
