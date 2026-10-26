package billing

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

const (
	PendingOrderLimit = 3
	OrderTTL          = 30 * time.Minute
)

// ProductType 是订单商品类型。当前仅 vip_plan 可创建，coin_package 预留给福利体系。
type ProductType string

const (
	ProductVIPPlan     ProductType = "vip_plan"
	ProductCoinPackage ProductType = "coin_package"
)

// Channel 是支付或商店结算渠道。
type Channel string

const (
	ChannelAlipay    Channel = "alipay"
	ChannelWechat    Channel = "wechat"
	ChannelUSDT      Channel = "usdt"
	ChannelIAPApple  Channel = "iap_apple"
	ChannelIAPGoogle Channel = "iap_google"
)

// Currency 是订单计价币种；金额一律为最小整数单位。
type Currency string

const (
	CurrencyCNY  Currency = "CNY"
	CurrencyUSDT Currency = "USDT"
)

// OrderStatus 是订单状态。付款成功或关闭之后都不允许回到 pending。
type OrderStatus string

const (
	OrderPending  OrderStatus = "pending"
	OrderPaid     OrderStatus = "paid"
	OrderClosed   OrderStatus = "closed"
	OrderRefunded OrderStatus = "refunded"
)

// VIPOrderPlan 是仓储在事务中锁定并读取的套餐快照源。
type VIPOrderPlan struct {
	ID             int64
	Code           string
	DurationDays   int
	PriceFen       int64
	PriceMicroUSDT int64
	Channels       []string
	Clients        []string
	Status         string
}

// ChannelConfig 是仓储在事务中读取的支付渠道开关；credential_enc 永远不进入领域对象。
type ChannelConfig struct {
	Channel      Channel
	Enabled      bool
	Clients      []string
	RegionsAllow []string
	RegionsDeny  []string
}

// VIPOrderSnapshot 固化在 pay_order.product_snapshot，避免后续套餐改价、改时长影响已创建订单。
type VIPOrderSnapshot struct {
	Type         ProductType `json:"type"`
	PlanID       int64       `json:"plan_id"`
	PlanCode     string      `json:"plan_code"`
	DurationDays int         `json:"duration_days"`
	Channel      Channel     `json:"channel"`
	Currency     Currency    `json:"currency"`
	Amount       int64       `json:"amount"`
}

// Order 是订单领域视图。渠道支付参数不会持久化在这里，由 S3-12～S3-14 的适配器按需生成。
type Order struct {
	ID             int64            `json:"id"`
	OrderNo        string           `json:"order_no"`
	UserID         int64            `json:"user_id"`
	ProductType    ProductType      `json:"product_type"`
	ProductID      int64            `json:"product_id"`
	Snapshot       VIPOrderSnapshot `json:"product_snapshot"`
	Channel        Channel          `json:"channel"`
	Client         string           `json:"client"`
	Currency       Currency         `json:"currency"`
	Amount         int64            `json:"amount"`
	Status         OrderStatus      `json:"status"`
	ChannelTradeNo string           `json:"channel_trade_no"`
	PaidAt         *time.Time       `json:"paid_at"`
	ExpireAt       time.Time        `json:"expire_at"`
	Late           bool             `json:"late"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`
}

// OrderPage 是我的订单的游标分页结果。
type OrderPage struct {
	List       []Order `json:"list"`
	NextCursor string  `json:"next_cursor"`
}

// CreateVIPOrderInput 是用户端下单意图。Country 由可信 GeoIP 解析结果提供，不能信任客户端自报。
type CreateVIPOrderInput struct {
	PlanID         int64
	Channel        Channel
	Client         string
	Country        string
	IdempotencyKey string
}

// CreateVIPOrderCommand 是传给仓储的已校验下单命令。仓储必须在一个事务中重新锁定套餐/渠道，
// 检查待支付数量和幂等键，再写入固定快照，不能只信任调用前读到的数据。
type CreateVIPOrderCommand struct {
	UserID         int64
	PlanID         int64
	Channel        Channel
	Client         string
	Country        string
	IdempotencyKey string
	OrderNo        string
	ExpireAt       time.Time
	Mock           bool
}

// OrderRepo 的方法必须在事务中实现。模拟支付仍通过相同订单与会员流水表，不得在 handler 里直接改库。
type OrderRepo interface {
	CreateVIPOrder(ctx context.Context, in CreateVIPOrderCommand) (Order, error)
	GetPayOrder(ctx context.Context, userID int64, orderNo string) (Order, error)
	ListPayOrders(ctx context.Context, userID, beforeID int64, limit int) ([]Order, error)
	CancelPayOrder(ctx context.Context, userID int64, orderNo string) (Order, error)
	CompleteMockPayOrder(ctx context.Context, userID int64, orderNo string, paidAt time.Time) (Order, error)
}

// MembershipInvalidator 在订单入账后失效播放链路使用的会员缓存。
type MembershipInvalidator interface {
	InvalidateMembership(ctx context.Context, userID int64)
}

// OrderService 是支付适配器无关的订单入口。真实 pay_action 由后续 S3-12～S3-14 适配器生成。
type OrderService struct {
	repo        OrderRepo
	membership  MembershipInvalidator
	mockEnabled bool
	now         func() time.Time
	newOrderNo  func() (string, error)
}

func NewOrderService(repo OrderRepo, membership MembershipInvalidator, mockEnabled bool) *OrderService {
	return &OrderService{repo: repo, membership: membership, mockEnabled: mockEnabled, now: time.Now, newOrderNo: NewOrderNo}
}

func (s *OrderService) MockEnabled() bool { return s.mockEnabled }

// CreateVIPOrder 生成不可预测订单号和过期时间，再交由事务仓储保证订单创建原子性。
// 当前没有真实渠道适配器时，只有 development 模拟模式可创建订单；production 一律拒绝。
func (s *OrderService) CreateVIPOrder(ctx context.Context, userID int64, in CreateVIPOrderInput) (Order, error) {
	if userID <= 0 {
		return Order{}, errs.ErrNeedLogin
	}
	if !s.mockEnabled {
		return Order{}, errs.ErrUnavailable.WithMessage("支付渠道暂未开放")
	}
	if in.PlanID <= 0 || !validChannel(in.Channel) || !validClient(in.Client) || !validCountry(in.Country) || !validIdempotencyKey(in.IdempotencyKey) {
		return Order{}, errs.ErrBadRequest.WithMessage("订单参数无效")
	}
	orderNo, err := s.newOrderNo()
	if err != nil {
		return Order{}, err
	}
	now := s.now().UTC()
	return s.repo.CreateVIPOrder(ctx, CreateVIPOrderCommand{
		UserID: userID, PlanID: in.PlanID, Channel: in.Channel, Client: in.Client,
		Country: strings.ToUpper(strings.TrimSpace(in.Country)), IdempotencyKey: in.IdempotencyKey,
		OrderNo: orderNo, ExpireAt: now.Add(OrderTTL), Mock: true,
	})
}

func (s *OrderService) Get(ctx context.Context, userID int64, orderNo string) (Order, error) {
	if userID <= 0 || strings.TrimSpace(orderNo) == "" {
		return Order{}, errs.ErrNotFound
	}
	return s.repo.GetPayOrder(ctx, userID, orderNo)
}

func (s *OrderService) List(ctx context.Context, userID, beforeID int64, limit int) (OrderPage, error) {
	if userID <= 0 {
		return OrderPage{}, errs.ErrNeedLogin
	}
	if beforeID < 0 || limit < 1 || limit > 50 {
		return OrderPage{}, errs.ErrBadRequest.WithMessage("订单分页参数无效")
	}
	list, err := s.repo.ListPayOrders(ctx, userID, beforeID, limit)
	if err != nil {
		return OrderPage{}, err
	}
	page := OrderPage{List: nonNilOrders(list)}
	if len(list) == limit {
		page.NextCursor = formatOrderCursor(list[len(list)-1].ID)
	}
	return page, nil
}

func (s *OrderService) Cancel(ctx context.Context, userID int64, orderNo string) (Order, error) {
	if userID <= 0 || strings.TrimSpace(orderNo) == "" {
		return Order{}, errs.ErrNotFound
	}
	return s.repo.CancelPayOrder(ctx, userID, orderNo)
}

// CompleteMock 仅在 development 模拟渠道开启。它复用真实的订单状态机和会员流水事务，禁止在 production 注册。
func (s *OrderService) CompleteMock(ctx context.Context, userID int64, orderNo string) (Order, error) {
	if !s.mockEnabled {
		return Order{}, errs.ErrNotFound
	}
	order, err := s.repo.CompleteMockPayOrder(ctx, userID, orderNo, s.now().UTC())
	if err != nil {
		return Order{}, err
	}
	if s.membership != nil && order.Status == OrderPaid {
		s.membership.InvalidateMembership(ctx, userID)
	}
	return order, nil
}

// BuildVIPOrderSnapshot 在事务中读取到套餐后固定商品内容与金额。关闭、渠道不匹配或金额为零的套餐不可下单。
func BuildVIPOrderSnapshot(plan VIPOrderPlan, channel Channel, client string) (VIPOrderSnapshot, error) {
	if plan.ID <= 0 || plan.DurationDays <= 0 || plan.Status != "on" || !slices.Contains(plan.Channels, string(channel)) || !slices.Contains(plan.Clients, client) {
		return VIPOrderSnapshot{}, errs.ErrUnavailable.WithMessage("套餐当前不可购买")
	}
	currency := currencyForChannel(channel)
	amount := plan.PriceFen
	if currency == CurrencyUSDT {
		amount = plan.PriceMicroUSDT
	}
	if amount <= 0 {
		return VIPOrderSnapshot{}, errs.ErrUnavailable.WithMessage("套餐价格尚未配置")
	}
	return VIPOrderSnapshot{
		Type: ProductVIPPlan, PlanID: plan.ID, PlanCode: plan.Code, DurationDays: plan.DurationDays,
		Channel: channel, Currency: currency, Amount: amount,
	}, nil
}

// ChannelAvailable 按启用状态、客户端和地区白黑名单判定。allow 非空时，未知地区不能放行。
func ChannelAvailable(channel ChannelConfig, client, country string) bool {
	if !channel.Enabled || channel.Channel == "" || !slices.Contains(channel.Clients, client) {
		return false
	}
	country = strings.ToUpper(strings.TrimSpace(country))
	if country != "" && slices.Contains(channel.RegionsDeny, country) {
		return false
	}
	return len(channel.RegionsAllow) == 0 || (country != "" && slices.Contains(channel.RegionsAllow, country))
}

// CanOrderTransition 定义订单状态机；同状态用于幂等回放，视为合法。
// closed → paid 表示渠道在本地关闭后才确认到账，入账实现必须同时标记 late=true。
func CanOrderTransition(from, to OrderStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case OrderPending:
		return to == OrderPaid || to == OrderClosed
	case OrderClosed:
		return to == OrderPaid
	case OrderPaid:
		return to == OrderRefunded
	default:
		return false
	}
}

// ExtendVIPUntil 计算购买叠加后的到期时间。并发安全由仓储在锁住 user_vip 行后调用它保证。
func ExtendVIPUntil(from, now time.Time, days int) (base, to time.Time, err error) {
	if days <= 0 {
		return time.Time{}, time.Time{}, errs.ErrBadRequest.WithMessage("会员天数必须大于 0")
	}
	from = from.UTC()
	now = now.UTC()
	base = now
	if from.After(now) {
		base = from
	}
	return base, base.AddDate(0, 0, days), nil
}

// NewOrderNo 生成 16 字节随机数的无填充 base32 文本（26 位），适合公开给客户端而不可预测。
func NewOrderNo() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

func formatOrderCursor(id int64) string { return strconv.FormatInt(id, 10) }

func parseOrderCursor(cursor string) (int64, error) {
	if cursor == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(cursor, 10, 64)
	if err != nil || id <= 0 {
		return 0, errs.ErrBadRequest.WithMessage("订单游标无效")
	}
	return id, nil
}

func nonNilOrders(in []Order) []Order {
	if in == nil {
		return []Order{}
	}
	return in
}

func currencyForChannel(channel Channel) Currency {
	if channel == ChannelUSDT {
		return CurrencyUSDT
	}
	return CurrencyCNY
}

func validChannel(channel Channel) bool {
	switch channel {
	case ChannelAlipay, ChannelWechat, ChannelUSDT, ChannelIAPApple, ChannelIAPGoogle:
		return true
	default:
		return false
	}
}

func validCountry(country string) bool {
	country = strings.TrimSpace(country)
	if country == "" {
		return true
	}
	if len(country) != 2 {
		return false
	}
	for _, c := range country {
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}

func validIdempotencyKey(key string) bool {
	if key == "" || len(key) > 128 || !utf8.ValidString(key) {
		return false
	}
	for _, c := range key {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}
