// Package vip 提供会员套餐与我的会员状态接口（S3-10）。支付下单与渠道配置在后续切片接入。
package vip

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/domain/billing"
	"xsite/service/internal/http/middleware"
	"xsite/service/internal/http/respond"
)

const noStore = "private, no-store"

// Service 是会员核心服务。
type Service interface {
	Plans(ctx context.Context, client string) ([]billing.Plan, error)
	Status(ctx context.Context, userID int64) (billing.Status, error)
}

// Handlers 聚合会员前台读接口。
type Handlers struct {
	Svc         Service
	Resolver    middleware.AccessResolver
	MockEnabled bool
}

// Routes 注册套餐列表与我的会员状态。两者均在 /api/v1 报文加密分组中；只有后者要求注册用户身份。
func (h *Handlers) Routes(v1 gin.IRouter) {
	v1.GET("/vip/plans", h.ListPlans)
	me := v1.Group("/me", middleware.SetIdentity(h.Resolver), middleware.RequireUser())
	me.GET("/vip", h.GetStatus)
}

// ListPlans 是 GET /api/v1/vip/plans。S3-10 策略 A 仅按客户端过滤；地区和实时渠道开关在 S3-11 / S3-15 接入。
func (h *Handlers) ListPlans(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	client := planClient(c)
	plans, err := h.Svc.Plans(c.Request.Context(), client)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"client": client, "plans": plans, "mock_enabled": h.MockEnabled})
}

// GetStatus 是 GET /api/v1/me/vip：只允许注册用户读取自己的会员状态。
func (h *Handlers) GetStatus(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	access, _ := middleware.CurrentAccess(c)
	status, err := h.Svc.Status(c.Request.Context(), access.UserID())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, status)
}

// planClient 复用已存在的 X-Platform 语义：web 再按 UA 区分桌面和手机；ios / android 当前映射官网包。
// 商店包在后续接入时可显式传 app_store，不能依赖 UA 猜测分发渠道。
func planClient(c *gin.Context) string {
	switch strings.ToLower(strings.TrimSpace(c.GetHeader("X-Platform"))) {
	case billing.ClientWebPC:
		return billing.ClientWebPC
	case billing.ClientWebMobile:
		return billing.ClientWebMobile
	case billing.ClientAppOfficial, "ios", "android":
		return billing.ClientAppOfficial
	case billing.ClientAppStore:
		return billing.ClientAppStore
	}
	ua := strings.ToLower(c.GetHeader("User-Agent"))
	if strings.Contains(ua, "mobile") || strings.Contains(ua, "android") || strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") {
		return billing.ClientWebMobile
	}
	return billing.ClientWebPC
}
