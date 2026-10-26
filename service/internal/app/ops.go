package app

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/ops"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/jobs"
	"xsite/service/internal/store/cache"
	"xsite/service/internal/store/pg"
)

// 运营域在 Valkey 中的键前缀：API 进程与 worker 都要读写，两边必须一致（所以只在这里定义）。
const (
	adStatsPrefix    = "xsite:ad:"   // 广告去重、待落库计数、频控
	hotCounterPrefix = "xsite:hot:"  // 热搜词待落库的搜索次数
	rankStorePrefix  = "xsite:rank:" // 线上榜单（有序集合 + 热度 + 计算时间）
)

// newAdService 创建广告服务（API 进程与 worker 共用同一套装配）。
func newAdService(pool *pgxpool.Pool, vk valkey.Client) *ops.AdService {
	return ops.NewAdService(pg.NewOpsRepo(pool), cache.NewAdStats(vk, adStatsPrefix), cache.New(vk, "xsite:"))
}

// newHotsearchService 创建热搜词服务（API 进程与 worker 共用同一套装配）。
func newHotsearchService(pool *pgxpool.Pool, vk valkey.Client) *ops.HotsearchService {
	return ops.NewHotsearchService(pg.NewOpsRepo(pool), cache.NewHotCounter(vk, hotCounterPrefix), cache.New(vk, "xsite:"))
}

// newRankService 创建榜单服务。worker 只用来重新排名，features 与 queue 传 nil。
func newRankService(pool *pgxpool.Pool, vk valkey.Client, features ops.FeatureFlags, queue ops.RecomputeQueue) *ops.RankService {
	repo := pg.NewOpsRepo(pool)
	return ops.NewRankService(repo, repo, cache.NewRankStore(vk, rankStorePrefix), cache.New(vk, "xsite:"), features, queue)
}

// NewOpsWorkers 返回 worker 进程中运营域周期任务的依赖（docs/specs/A3-4.md）。
func NewOpsWorkers(pool *pgxpool.Pool, vk valkey.Client) jobs.OpsWorkers {
	return jobs.OpsWorkers{
		Ads:       newAdService(pool, vk),
		Hotsearch: newHotsearchService(pool, vk),
		Ranks:     newRankService(pool, vk, nil, nil),
	}
}

// siteConfigReader 是读站点配置的那一部分（platform.SiteConfigService）。
type siteConfigReader interface {
	Get(ctx context.Context) (platform.SiteConfig, error)
}

// siteFeatures 从站点配置的 features 段读功能开关（实现 ops.FeatureFlags）：只有明确设为 false 才算关闭。
type siteFeatures struct{ cfg siteConfigReader }

func (f siteFeatures) Enabled(ctx context.Context, name string) (bool, error) {
	c, err := f.cfg.Get(ctx)
	if err != nil {
		return false, err
	}
	var m map[string]any
	if len(c.Features) > 0 {
		if err := json.Unmarshal(c.Features, &m); err != nil {
			return false, fmt.Errorf("解析功能开关: %w", err)
		}
	}
	return m[name] != false, nil
}
