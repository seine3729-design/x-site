// Package billing 管理会员套餐与会员权益状态（S3-10）。订单、支付与人工调整在后续切片接入。
package billing

import (
	"context"
	"time"

	"xsite/service/internal/errs"
)

const (
	ClientWebPC       = "web_pc"
	ClientWebMobile   = "web_mobile"
	ClientAppOfficial = "app_official"
	ClientAppStore    = "app_store"
)

// Plan 是当前客户端可购买的会员套餐。名称由客户端按 code 查多语言文案，金额始终使用整数最小单位。
type Plan struct {
	ID             int64    `json:"id"`
	Code           string   `json:"code"`
	DurationDays   int      `json:"duration_days"`
	PriceFen       int64    `json:"price_fen"`
	ListPriceFen   int64    `json:"list_price_fen"`
	PriceMicroUSDT int64    `json:"price_micro_usdt"`
	BadgeKey       string   `json:"badge_key"`
	Recommended    bool     `json:"recommended"`
	Channels       []string `json:"channels"`
}

// Status 是注册用户当前会员状态。ExpireAt 为 nil 表示从未开通过会员。
type Status struct {
	IsVIP    bool       `json:"is_vip"`
	ExpireAt *time.Time `json:"expire_at"`
}

// Repo 是会员核心的持久化边界。
type Repo interface {
	ListAvailableVIPPlans(ctx context.Context, client string) ([]Plan, error)
	VIPUntil(ctx context.Context, userID int64) (time.Time, error)
}

// Cache 是会员状态与套餐读缓存。缓存故障必须降级回源，不得影响权益判定。
type Cache interface {
	GetVIPUntil(ctx context.Context, userID int64) (time.Time, bool)
	SetVIPUntil(ctx context.Context, userID int64, until time.Time)
	DeleteVIPUntil(ctx context.Context, userID int64)
	GetOrLoadPlans(ctx context.Context, client string, load func(context.Context) ([]Plan, error)) ([]Plan, error)
}

// Service 是会员核心服务。
type Service struct {
	repo  Repo
	cache Cache
	now   func() time.Time
}

func NewService(repo Repo, cache Cache) *Service {
	return &Service{repo: repo, cache: cache, now: time.Now}
}

// Plans 返回当前客户端可购买的已上架套餐。S3-10 的策略 A 暂不叠加渠道地区开关，后续由 S3-11 / S3-15 完整接入。
func (s *Service) Plans(ctx context.Context, client string) ([]Plan, error) {
	if !validClient(client) {
		return nil, errs.ErrBadRequest.WithMessage("客户端类型无效")
	}
	plans, err := s.cache.GetOrLoadPlans(ctx, client, func(ctx context.Context) ([]Plan, error) {
		return s.repo.ListAvailableVIPPlans(ctx, client)
	})
	if err != nil {
		return nil, err
	}
	return nonNilPlans(plans), nil
}

// VIPUntil 返回用户会员到期时间；零时间表示从未开通。缓存未命中或不可用时回源 PostgreSQL。
// 它实现 play.Membership，供选源链路判定 access=vip 的分集权益。
func (s *Service) VIPUntil(ctx context.Context, userID int64) (time.Time, error) {
	if userID <= 0 {
		return time.Time{}, nil
	}
	if until, ok := s.cache.GetVIPUntil(ctx, userID); ok {
		return until, nil
	}
	until, err := s.repo.VIPUntil(ctx, userID)
	if err != nil {
		return time.Time{}, err
	}
	until = until.UTC()
	s.cache.SetVIPUntil(ctx, userID, until)
	return until, nil
}

// Status 返回我的会员状态。到期会员仍返回实际到期时间，方便前台展示续费引导。
func (s *Service) Status(ctx context.Context, userID int64) (Status, error) {
	if userID <= 0 {
		return Status{}, errs.ErrNeedLogin
	}
	until, err := s.VIPUntil(ctx, userID)
	if err != nil {
		return Status{}, err
	}
	if until.IsZero() {
		return Status{}, nil
	}
	return Status{IsVIP: until.After(s.now()), ExpireAt: &until}, nil
}

// InvalidateMembership 供后续购买、退款、补偿等写路径在事务提交后失效会员状态缓存。
func (s *Service) InvalidateMembership(ctx context.Context, userID int64) {
	if userID > 0 {
		s.cache.DeleteVIPUntil(ctx, userID)
	}
}

func validClient(client string) bool {
	switch client {
	case ClientWebPC, ClientWebMobile, ClientAppOfficial, ClientAppStore:
		return true
	default:
		return false
	}
}

func nonNilPlans(in []Plan) []Plan {
	if in == nil {
		return []Plan{}
	}
	for i := range in {
		if in[i].Channels == nil {
			in[i].Channels = []string{}
		}
	}
	return in
}
