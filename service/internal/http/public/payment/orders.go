package payment

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/billing"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
	"xsite/service/internal/http/respond"
)

const noStore = "private, no-store"

type createOrderRequest struct {
	Product struct {
		Type string `json:"type"`
		ID   int64  `json:"id"`
	} `json:"product"`
	Channel    string `json:"channel"`
	Client     string `json:"client"`
	ReturnPath string `json:"return_path"`
}

type orderDTO struct {
	OrderNo     string     `json:"order_no"`
	ProductType string     `json:"product_type"`
	ProductID   int64      `json:"product_id"`
	Product     any        `json:"product_snapshot"`
	Channel     string     `json:"channel"`
	Client      string     `json:"client"`
	Currency    string     `json:"currency"`
	Amount      int64      `json:"amount"`
	Status      string     `json:"status"`
	ExpireAt    time.Time  `json:"expire_at"`
	PaidAt      *time.Time `json:"paid_at"`
	Late        bool       `json:"late"`
	CreatedAt   time.Time  `json:"created_at"`
}

type mockAction struct {
	Type             string `json:"type"`
	CompleteEndpoint string `json:"complete_endpoint"`
}

func orderOf(order dom.Order) orderDTO {
	return orderDTO{
		OrderNo: order.OrderNo, ProductType: string(order.ProductType), ProductID: order.ProductID,
		Product: order.Snapshot, Channel: string(order.Channel), Client: order.Client, Currency: string(order.Currency),
		Amount: order.Amount, Status: string(order.Status), ExpireAt: order.ExpireAt, PaidAt: order.PaidAt,
		Late: order.Late, CreatedAt: order.CreatedAt,
	}
}

func currentUserID(c *gin.Context) int64 {
	access, _ := middleware.CurrentAccess(c)
	return access.UserID()
}

// Create 是 POST /api/v1/pay/orders。当前仅在 development 模拟模式下创建订单；production 会安全返回 UNAVAILABLE。
func (h *Handlers) Create(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req createOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	if req.Product.Type != string(dom.ProductVIPPlan) {
		respond.Error(c, errs.ErrBadRequest.WithMessage("当前仅支持会员套餐"))
		return
	}
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	order, err := h.Orders.CreateVIPOrder(c.Request.Context(), currentUserID(c), dom.CreateVIPOrderInput{
		PlanID: req.Product.ID, Channel: dom.Channel(req.Channel), Client: req.Client, IdempotencyKey: key,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	data := gin.H{"order": orderOf(order)}
	if h.Orders.MockEnabled() {
		data["pay_action"] = mockAction{Type: "mock", CompleteEndpoint: "/api/v1/pay/orders/" + order.OrderNo + "/mock-complete"}
	}
	respond.OK(c, data)
}

// Get 是 GET /api/v1/pay/orders/:order_no：只能读取当前用户自己的订单。
func (h *Handlers) Get(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	order, err := h.Orders.Get(c.Request.Context(), currentUserID(c), c.Param("order_no"))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, orderOf(order))
}

// List 是 GET /api/v1/me/orders：游标使用内部订单 ID，不向客户端暴露实现细节以外的可推测编号。
func (h *Handlers) List(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	before := int64(0)
	if raw := c.Query("cursor"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			respond.Error(c, errs.ErrBadRequest.WithMessage("订单游标无效"))
			return
		}
		before = parsed
	}
	page, err := h.Orders.List(c.Request.Context(), currentUserID(c), before, 20)
	if err != nil {
		respond.Error(c, err)
		return
	}
	list := make([]orderDTO, len(page.List))
	for i, order := range page.List {
		list[i] = orderOf(order)
	}
	respond.OK(c, gin.H{"list": list, "next_cursor": page.NextCursor})
}

// Cancel 是 POST /api/v1/pay/orders/:order_no/cancel：只关闭本人尚未过期的待支付订单。
func (h *Handlers) Cancel(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	order, err := h.Orders.Cancel(c.Request.Context(), currentUserID(c), c.Param("order_no"))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, orderOf(order))
}

// CompleteMock 是 development 专用的模拟完成。它不是支付回调，也不接受第三方提交的金额或流水号。
func (h *Handlers) CompleteMock(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	order, err := h.Orders.CompleteMock(c.Request.Context(), currentUserID(c), c.Param("order_no"))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, orderOf(order))
}
