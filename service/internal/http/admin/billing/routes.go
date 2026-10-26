// Package billing 提供运营后台的会员套餐配置接口（S3-15 / A3-5）。
package billing

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/billing"
	"xsite/service/internal/http/admin"
)

// PlanService 是后台套餐配置服务。
type PlanService interface {
	List(ctx context.Context) ([]dom.AdminVIPPlan, error)
	Update(ctx context.Context, id int64, in dom.AdminVIPPlanInput) (before, after dom.AdminVIPPlan, err error)
}

// Handlers 聚合会员套餐后台接口。支付渠道凭据配置在独立 PAYMENT_KEKS 就绪前不得接入本包。
type Handlers struct {
	Plans PlanService
}

func (h *Handlers) Routes(authed gin.IRouter) {
	plans := authed.Group("/vip/plans", admin.RequirePage("vip_plans"))
	plans.GET("", h.ListPlans)
	plans.PATCH("/:id", h.UpdatePlan)
}
