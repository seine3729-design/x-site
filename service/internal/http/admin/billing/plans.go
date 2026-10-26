package billing

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/billing"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type vipPlanRequest struct {
	DurationDays   int      `json:"duration_days"`
	PriceFen       int64    `json:"price_fen"`
	ListPriceFen   int64    `json:"list_price_fen"`
	PriceMicroUSDT int64    `json:"price_micro_usdt"`
	BadgeKey       string   `json:"badge_key"`
	Recommended    bool     `json:"recommended"`
	Channels       []string `json:"channels"`
	Clients        []string `json:"clients"`
	Sort           int      `json:"sort"`
	Status         string   `json:"status"`
}

type vipPlanDTO struct {
	ID             int64     `json:"id"`
	Code           string    `json:"code"`
	DurationDays   int       `json:"duration_days"`
	PriceFen       int64     `json:"price_fen"`
	ListPriceFen   int64     `json:"list_price_fen"`
	PriceMicroUSDT int64     `json:"price_micro_usdt"`
	BadgeKey       string    `json:"badge_key"`
	Recommended    bool      `json:"recommended"`
	Channels       []string  `json:"channels"`
	Clients        []string  `json:"clients"`
	Sort           int       `json:"sort"`
	Status         string    `json:"status"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func vipPlanOf(plan dom.AdminVIPPlan) vipPlanDTO {
	return vipPlanDTO{
		ID: plan.ID, Code: plan.Code, DurationDays: plan.DurationDays, PriceFen: plan.PriceFen,
		ListPriceFen: plan.ListPriceFen, PriceMicroUSDT: plan.PriceMicroUSDT, BadgeKey: plan.BadgeKey,
		Recommended: plan.Recommended, Channels: nonNil(plan.Channels), Clients: nonNil(plan.Clients),
		Sort: plan.Sort, Status: plan.Status, UpdatedAt: plan.UpdatedAt,
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// ListPlans 是 GET /api/admin/vip/plans：固定五种套餐，含下架项，按运营排序展示。
func (h *Handlers) ListPlans(c *gin.Context) {
	plans, err := h.Plans.List(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := make([]vipPlanDTO, len(plans))
	for i, plan := range plans {
		out[i] = vipPlanOf(plan)
	}
	respond.OK(c, gin.H{"list": out})
}

// UpdatePlan 是 PATCH /api/admin/vip/plans/:id：套餐编码不允许修改；保存后领域层失效所有客户端套餐缓存。
func (h *Handlers) UpdatePlan(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	var req vipPlanRequest
	if !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Plans.Update(c.Request.Context(), id, dom.AdminVIPPlanInput{
		DurationDays: req.DurationDays, PriceFen: req.PriceFen, ListPriceFen: req.ListPriceFen,
		PriceMicroUSDT: req.PriceMicroUSDT, BadgeKey: req.BadgeKey, Recommended: req.Recommended,
		Channels: req.Channels, Clients: req.Clients, Sort: req.Sort, Status: req.Status,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := vipPlanOf(after)
	admin.SetAudit(c, admin.AuditDetail{
		Action: "vip_plan.update", TargetType: "vip_plan", TargetID: strconv.FormatInt(id, 10),
		Before: vipPlanOf(before), After: out,
	})
	respond.OK(c, out)
}
