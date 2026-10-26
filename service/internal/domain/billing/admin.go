package billing

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

const (
	VIPPlanStatusOn  = "on"
	VIPPlanStatusOff = "off"
)

// AdminVIPPlan 是运营后台使用的完整套餐配置。code 固定且只读，名称由用户端按 code 本地化。
type AdminVIPPlan struct {
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

// AdminVIPPlanInput 是后台整体保存套餐的可编辑字段。code、ID 均由服务端固定，不接受客户端修改。
type AdminVIPPlanInput struct {
	DurationDays   int
	PriceFen       int64
	ListPriceFen   int64
	PriceMicroUSDT int64
	BadgeKey       string
	Recommended    bool
	Channels       []string
	Clients        []string
	Sort           int
	Status         string
}

// PlanAdminRepo 是会员套餐后台配置的持久化边界。
type PlanAdminRepo interface {
	ListAdminVIPPlans(ctx context.Context) ([]AdminVIPPlan, error)
	GetAdminVIPPlan(ctx context.Context, id int64) (AdminVIPPlan, error)
	UpdateAdminVIPPlan(ctx context.Context, id int64, in AdminVIPPlanInput) (AdminVIPPlan, error)
}

// PlanInvalidator 清理套餐读缓存。缓存失败不回滚已成功保存的数据库配置。
type PlanInvalidator interface {
	InvalidatePlans(ctx context.Context)
}

// PlanAdminService 管理固定五种会员套餐；支付渠道凭据不属于本服务。
type PlanAdminService struct {
	repo  PlanAdminRepo
	cache PlanInvalidator
}

func NewPlanAdminService(repo PlanAdminRepo, cache PlanInvalidator) *PlanAdminService {
	return &PlanAdminService{repo: repo, cache: cache}
}

func (s *PlanAdminService) List(ctx context.Context) ([]AdminVIPPlan, error) {
	return s.repo.ListAdminVIPPlans(ctx)
}

// Update 保存套餐。套餐上架前必须具备双币种正价、客户端和渠道，避免前台出现无法购买的半成品卡片。
func (s *PlanAdminService) Update(ctx context.Context, id int64, in AdminVIPPlanInput) (before, after AdminVIPPlan, err error) {
	if id <= 0 {
		return AdminVIPPlan{}, AdminVIPPlan{}, errs.ErrBadRequest.WithMessage("套餐编号无效")
	}
	if in, err = normalizeAdminVIPPlan(in); err != nil {
		return AdminVIPPlan{}, AdminVIPPlan{}, err
	}
	before, err = s.repo.GetAdminVIPPlan(ctx, id)
	if err != nil {
		return AdminVIPPlan{}, AdminVIPPlan{}, err
	}
	after, err = s.repo.UpdateAdminVIPPlan(ctx, id, in)
	if err != nil {
		return AdminVIPPlan{}, AdminVIPPlan{}, err
	}
	if s.cache != nil {
		s.cache.InvalidatePlans(ctx)
	}
	return before, after, nil
}

func normalizeAdminVIPPlan(in AdminVIPPlanInput) (AdminVIPPlanInput, error) {
	if in.DurationDays < 1 || in.DurationDays > 3660 {
		return AdminVIPPlanInput{}, errs.ErrBadRequest.WithMessage("套餐天数必须在 1–3660 之间")
	}
	if in.PriceFen < 0 || in.ListPriceFen < 0 || in.PriceMicroUSDT < 0 {
		return AdminVIPPlanInput{}, errs.ErrBadRequest.WithMessage("套餐价格不能为负数")
	}
	if in.ListPriceFen > 0 && in.ListPriceFen < in.PriceFen {
		return AdminVIPPlanInput{}, errs.ErrBadRequest.WithMessage("划线价不能低于人民币售价")
	}
	if in.Sort < 0 || in.Sort > 9999 {
		return AdminVIPPlanInput{}, errs.ErrBadRequest.WithMessage("排序必须在 0–9999 之间")
	}
	if in.Status != VIPPlanStatusOn && in.Status != VIPPlanStatusOff {
		return AdminVIPPlanInput{}, errs.ErrBadRequest.WithMessage("套餐状态无效")
	}
	channels, err := normalizeChannels(in.Channels)
	if err != nil {
		return AdminVIPPlanInput{}, err
	}
	clients, err := normalizeClients(in.Clients)
	if err != nil {
		return AdminVIPPlanInput{}, err
	}
	badge := strings.TrimSpace(in.BadgeKey)
	if utf8.RuneCountInString(badge) > 64 || (badge != "" && !validBadgeKey(badge)) {
		return AdminVIPPlanInput{}, errs.ErrBadRequest.WithMessage("角标文案键格式无效")
	}
	if in.Status == VIPPlanStatusOn {
		if in.PriceFen <= 0 || in.PriceMicroUSDT <= 0 {
			return AdminVIPPlanInput{}, errs.ErrBadRequest.WithMessage("上架套餐必须配置正的人民币和 USDT 价格")
		}
		if len(channels) == 0 || len(clients) == 0 {
			return AdminVIPPlanInput{}, errs.ErrBadRequest.WithMessage("上架套餐至少选择一个支付渠道和客户端")
		}
	}
	in.BadgeKey = badge
	in.Channels = channels
	in.Clients = clients
	return in, nil
}

func normalizeChannels(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if !validChannel(Channel(v)) {
			return nil, errs.ErrBadRequest.WithMessage("套餐支付渠道无效")
		}
		if slices.Contains(out, v) {
			return nil, errs.ErrBadRequest.WithMessage("套餐支付渠道不能重复")
		}
		out = append(out, v)
	}
	return out, nil
}

func normalizeClients(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if !validClient(v) {
			return nil, errs.ErrBadRequest.WithMessage("套餐客户端范围无效")
		}
		if slices.Contains(out, v) {
			return nil, errs.ErrBadRequest.WithMessage("套餐客户端范围不能重复")
		}
		out = append(out, v)
	}
	return out, nil
}

func validBadgeKey(key string) bool {
	if !strings.HasPrefix(key, "vip.") {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
