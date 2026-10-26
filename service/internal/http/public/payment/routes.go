// Package payment 提供用户端订单接口。真实渠道适配完成前，只有 development 模拟路径可完成支付。
package payment

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/billing"
	"xsite/service/internal/http/middleware"
)

// OrderService 是订单领域服务。
type OrderService interface {
	CreateVIPOrder(ctx context.Context, userID int64, in dom.CreateVIPOrderInput) (dom.Order, error)
	Get(ctx context.Context, userID int64, orderNo string) (dom.Order, error)
	List(ctx context.Context, userID, beforeID int64, limit int) (dom.OrderPage, error)
	Cancel(ctx context.Context, userID int64, orderNo string) (dom.Order, error)
	CompleteMock(ctx context.Context, userID int64, orderNo string) (dom.Order, error)
	MockEnabled() bool
}

// Handlers 聚合订单 API；Resolver 仅解析注册用户身份，令牌仍由 BFF HttpOnly Cookie 透传。
type Handlers struct {
	Orders   OrderService
	Resolver middleware.AccessResolver
}

func (h *Handlers) Routes(v1 gin.IRouter) {
	pay := v1.Group("/pay", middleware.SetIdentity(h.Resolver), middleware.RequireUser())
	pay.POST("/orders", h.Create)
	pay.GET("/orders/:order_no", h.Get)
	pay.POST("/orders/:order_no/cancel", h.Cancel)
	if h.Orders != nil && h.Orders.MockEnabled() {
		// 只在 development 注册：production 没有模拟完成路径，不能借此绕过真实渠道验签。
		pay.POST("/orders/:order_no/mock-complete", h.CompleteMock)
	}
	me := v1.Group("/me", middleware.SetIdentity(h.Resolver), middleware.RequireUser())
	me.GET("/orders", h.List)
}
