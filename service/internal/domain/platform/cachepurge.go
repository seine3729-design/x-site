package platform

import (
	"context"
	"fmt"

	"xsite/service/internal/errs"
)

// CacheKeyStore 是 Valkey 侧的扫描与删除能力（实现见 internal/store/cache.Purger）。
type CacheKeyStore interface {
	Stats(ctx context.Context) (keys int64, memoryUsed int64, memoryText string, err error)
	Count(ctx context.Context, pattern string) (int64, error)
	Purge(ctx context.Context, pattern string) (int64, error)
}

// ProductPrefixRepo 查一部剧的转码产物前缀，用于清播放用的清单原文缓存。
type ProductPrefixRepo interface {
	DramaObjectPrefixes(ctx context.Context, dramaID int64) ([]string, error)
}

// SiteConfigManager 是站点配置服务（实现见本包的 SiteConfigService）。
//
// 清「站点设置」必须走它的 BumpVersion 而不是直接改库：BumpVersion 在 version+1 之外
// 还会清掉本实例的一级缓存、删 Valkey 的二级缓存键、并经 Pub/Sub 让其它实例清各自的一级缓存。
// 只 +1 的话，各实例的进程内缓存最多还要等一个 L1 TTL 才生效。
type SiteConfigManager interface {
	Get(ctx context.Context) (SiteConfig, error)
	BumpVersion(ctx context.Context) (int64, error)
}

// CacheService 是后台的缓存管理（docs/06 第 28 节，S4-4）。
type CacheService struct {
	keys     CacheKeyStore
	site     SiteConfigManager
	products ProductPrefixRepo
}

func NewCacheService(keys CacheKeyStore, site SiteConfigManager, products ProductPrefixRepo) *CacheService {
	return &CacheService{keys: keys, site: site, products: products}
}

// Overview 是缓存页的概览数据。
type Overview struct {
	SiteVersion int64       `json:"site_version"`
	TotalKeys   int64       `json:"total_keys"`
	MemoryUsed  int64       `json:"memory_used"`
	MemoryText  string      `json:"memory_text"`
	Scopes      []ScopeInfo `json:"scopes"`
	Prefixes    []KeyPrefix `json:"prefixes"`
}

// PurgeResult 是一次清理的结果。
type PurgeResult struct {
	Scope PurgeScope `json:"scope"`
	Keys  int64      `json:"keys"` // 实际删除的键数
	// SiteVersion 只有清「站点设置」时有值：+1 之后的新版本。
	SiteVersion int64 `json:"site_version,omitempty"`
}

// Overview 返回站点版本、键数量与各前缀的占用情况。
//
// 键数量按前缀逐个 SCAN 得出，比 DBSIZE 更有用（能看出是哪类缓存涨起来的）；
// 有状态的键也会统计并标注为不可清，这样运维知道它们存在而不是以为漏了。
func (s *CacheService) Overview(ctx context.Context) (Overview, error) {
	total, memUsed, memText, err := s.keys.Stats(ctx)
	if err != nil {
		return Overview{}, err
	}
	out := Overview{
		TotalKeys: total, MemoryUsed: memUsed, MemoryText: memText,
		Scopes: Scopes(), Prefixes: make([]KeyPrefix, 0, len(statefulPrefixes)+5),
	}
	if cfg, err := s.site.Get(ctx); err == nil {
		out.SiteVersion = cfg.Version
	}

	// 可清的前缀：按用途分组统计
	for _, p := range []KeyPrefix{
		{Prefix: keySiteConfig, Title: "站点配置", Purgeable: true},
		{Prefix: keyContent + "*", Title: "前台内容（首页、列表、剧详情、专题）", Purgeable: true},
		{Prefix: keyOpsCache + "*", Title: "运营下发（公告、广告、热搜词、排行）", Purgeable: true},
		{Prefix: keyManifest + "*", Title: "HLS 清单原文", Purgeable: true},
		{Prefix: keyVIP + "*", Title: "会员状态与套餐", Purgeable: true},
	} {
		if p.Keys, err = s.keys.Count(ctx, p.Prefix); err != nil {
			return Overview{}, err
		}
		out.Prefixes = append(out.Prefixes, p)
	}
	for _, p := range statefulPrefixes {
		if p.Keys, err = s.keys.Count(ctx, p.Prefix+"*"); err != nil {
			return Overview{}, err
		}
		out.Prefixes = append(out.Prefixes, p)
	}
	return out, nil
}

// Purge 按范围清缓存。dramaID 只在剧详情与播放两个范围需要。
func (s *CacheService) Purge(ctx context.Context, scope PurgeScope, dramaID int64) (PurgeResult, error) {
	info, err := lookupScope(scope)
	if err != nil {
		return PurgeResult{}, err
	}
	if info.NeedsDrama && dramaID <= 0 {
		return PurgeResult{}, badRequest("清「%s」要填写剧集编号", info.Title)
	}

	var prefixes []string
	if info.Scope == ScopePlay {
		if prefixes, err = s.products.DramaObjectPrefixes(ctx, dramaID); err != nil {
			return PurgeResult{}, err
		}
		if len(prefixes) == 0 {
			// 这部剧还没有转码产物，没有清单原文可清。不当成错误：运维按编号清一遍是合理操作。
			return PurgeResult{Scope: scope}, nil
		}
	}

	out := PurgeResult{Scope: scope}
	for _, pattern := range patternsFor(info, dramaID, prefixes) {
		n, err := s.keys.Purge(ctx, pattern)
		if err != nil {
			return PurgeResult{}, err
		}
		out.Keys += n
	}

	// 「站点设置」与「全部」都要令版本 +1：前台配置的 ETag 由版本号派生，
	// 只清 Valkey 的话浏览器与 CDN 上的旧配置仍然有效（它们拿到的 304 还是老 ETag）。
	// 放在清键之后：BumpVersion 会再清一次并广播，确保广播发出时库里已是新版本。
	if info.Scope == ScopeSiteConfig || info.Scope == ScopeAll {
		version, err := s.site.BumpVersion(ctx)
		if err != nil {
			return PurgeResult{}, err
		}
		out.SiteVersion = version
	}
	return out, nil
}

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}
