package app

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/billing"
	"xsite/service/internal/store/cache"
	"xsite/service/internal/store/pg"
)

// 会员状态键格式为 xsite:vip:<uid>；套餐缓存使用同一命名空间下的 plans:<client>。
const vipCachePrefix = "xsite:vip:"

// newVIPService 创建会员核心服务。购买、退款等写路径在后续切片事务提交后调用 InvalidateMembership。
func newVIPService(pool *pgxpool.Pool, vk valkey.Client) *billing.Service {
	return billing.NewService(pg.NewBillingRepo(pool), cache.NewVIPStore(vk, vipCachePrefix))
}

// newVIPPlanAdminService 创建后台套餐配置服务，和前台读服务使用同一组 Valkey 套餐缓存键。
func newVIPPlanAdminService(pool *pgxpool.Pool, vk valkey.Client) *billing.PlanAdminService {
	return billing.NewPlanAdminService(pg.NewBillingRepo(pool), cache.NewVIPStore(vk, vipCachePrefix))
}

// newVIPOrderService 创建订单服务。mockEnabled 仅由 cfg.IsDev() 传入，production 不能获得模拟完成能力。
func newVIPOrderService(pool *pgxpool.Pool, membership *billing.Service, mockEnabled bool) *billing.OrderService {
	return billing.NewOrderService(pg.NewBillingRepo(pool), membership, mockEnabled)
}
