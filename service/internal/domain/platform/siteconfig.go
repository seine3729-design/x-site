// Package platform 是平台域：站点配置、缓存、白标等（docs/03 第 9 节）。
package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// SiteConfig 是下发给前台与 APP 的站点配置（不含统计代码等后台专用段）。
type SiteConfig struct {
	Version int64           `json:"version"`
	Site    json.RawMessage `json:"site"`
	Theme   json.RawMessage `json:"theme"`
	Layout  json.RawMessage `json:"layout"`
	SEO     json.RawMessage `json:"seo"`
	// Analytics 是统计代码：前台要把它原样注入页面，所以随配置一起下发（docs/specs/A3-3.md 决定 3）。
	Analytics json.RawMessage `json:"analytics"`
	AgeGate   json.RawMessage `json:"age_gate"`
	Features  json.RawMessage `json:"features"`
	Addresses json.RawMessage `json:"addresses"`
	// Media 是图片与视频线路的域名池，来自后台存储线路表（domain/storage 的 PoolService）。
	// 客户端用它拼图片相对 key，Web 还用它按请求生成 CSP（docs/04 F、docs/12 第 6 节）。
	Media     MediaDomains `json:"media"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// MediaDomains 是随站点配置下发的线路域名池（docs/04 F）。
// 字段与 domain/storage.DomainPool 一对一，适配在 internal/app（platform 不依赖 storage）。
type MediaDomains struct {
	// ImageBases 是图片线路的访问前缀（含路径前缀），按优先级排列：图片相对 key 拼在其后，首选失败换下一条。
	ImageBases []string `json:"image_bases"`
	// VideoOrigins 是视频线路的 scheme://host[:port]，只用于把域名登记进 CSP 的 connect-src。
	VideoOrigins []string `json:"video_origins"`
}

// MediaDomainReader 读取线路域名池。由 internal/app 把 domain/storage 的实现适配进来；
// 为 nil 时下发空池（前台退回部署配置里的域名，见 apps 的 IMAGE_BASE_URLS）。
type MediaDomainReader interface {
	MediaDomains(ctx context.Context) (MediaDomains, error)
}

// ETag 由版本号生成，版本不变则客户端可用缓存。
func (s SiteConfig) ETag() string { return fmt.Sprintf(`W/"site-%d"`, s.Version) }

// SiteConfigRepo 读取站点配置并维护全局版本号。
type SiteConfigRepo interface {
	GetPublicSiteConfig(ctx context.Context) (SiteConfig, error)
	// BumpVersion 令 site_config.version +1，返回新版本。配置任一段变更后调用，作废 ETag 旧值。
	BumpVersion(ctx context.Context) (int64, error)
}

// Cache 是 Valkey 读穿缓存（二级），实现见 internal/store/cache。
type Cache interface {
	GetOrLoadJSON(ctx context.Context, key string, ttl time.Duration, dst any, load func(context.Context) (any, error)) error
	Delete(ctx context.Context, key string) error
}

// Invalidator 向所有实例广播缓存失效（实现见 internal/store/cache.Invalidator）。
type Invalidator interface {
	Publish(ctx context.Context, topic string) error
}

// SiteConfigTopic 是站点配置失效消息的 topic：各实例收到后清掉本地的站点配置缓存。
const SiteConfigTopic = "site_config"

const siteConfigCacheKey = "site_config:public"

// 三级缓存的兜底 TTL（docs/08 第 9.2 节）。正常走 BumpVersion 的即时失效（删 Valkey + Pub/Sub 广播清本地），
// 两个 TTL 只是广播不可用时的兜底：一级远小于验收要求的「5 秒内各实例生效」。
const (
	siteConfigL1TTL = 3 * time.Second
	siteConfigL2TTL = 30 * time.Second
)

// cachedConfig 是进程内一级缓存的一条快照。
type cachedConfig struct {
	cfg SiteConfig
	at  time.Time
}

// SiteConfigService 提供站点配置读取与「变更后各实例秒级生效」的失效机制（S1-9）。
//
// 读路径三级缓存：进程内一级（local，短 TTL）→ Valkey 二级（读穿缓存）→ PG。
// 写路径（版本 +1）清掉本地与 Valkey 缓存，并经 Pub/Sub 让其它实例清各自的一级缓存。
type SiteConfigService struct {
	repo  SiteConfigRepo
	cache Cache
	inval Invalidator
	media MediaDomainReader
	local atomic.Pointer[cachedConfig]
	now   func() time.Time
}

// NewSiteConfigService 装配站点配置读取。media 提供线路域名池（docs/04 F），可为 nil（下发空池）。
// 线路表变更必须同样走 BumpVersion，否则前台的 CSP 与图片前缀会停在旧域名上。
func NewSiteConfigService(repo SiteConfigRepo, cache Cache, inval Invalidator, media MediaDomainReader) *SiteConfigService {
	return &SiteConfigService{repo: repo, cache: cache, inval: inval, media: media, now: time.Now}
}

// Get 返回当前站点配置：先查进程内一级缓存（短 TTL），未命中走 Valkey 读穿缓存，再回源 PG。
func (s *SiteConfigService) Get(ctx context.Context) (SiteConfig, error) {
	if c := s.local.Load(); c != nil && s.now().Sub(c.at) < siteConfigL1TTL {
		return c.cfg, nil
	}
	var cfg SiteConfig
	err := s.cache.GetOrLoadJSON(ctx, siteConfigCacheKey, siteConfigL2TTL, &cfg, func(ctx context.Context) (any, error) {
		loaded, err := s.repo.GetPublicSiteConfig(ctx)
		if err != nil {
			return nil, err
		}
		// 域名池来自另一张表（storage_line），和配置各段一起进缓存、一起失效。
		// 读失败就让整个配置读失败：域名池为空会让前台一张封面都加载不出来，
		// 静默下发空池比报错更难排查（这也是原先靠部署环境变量时踩过的坑）。
		if loaded.Media, err = s.mediaDomains(ctx); err != nil {
			return nil, err
		}
		return loaded, nil
	})
	if err != nil {
		return SiteConfig{}, err
	}
	s.local.Store(&cachedConfig{cfg: cfg, at: s.now()})
	return cfg, nil
}

// mediaDomains 读线路域名池；未注入读取器时返回空池（切片用空数组，JSON 里不出现 null）。
func (s *SiteConfigService) mediaDomains(ctx context.Context) (MediaDomains, error) {
	empty := MediaDomains{ImageBases: []string{}, VideoOrigins: []string{}}
	if s.media == nil {
		return empty, nil
	}
	m, err := s.media.MediaDomains(ctx)
	if err != nil {
		return MediaDomains{}, err
	}
	if m.ImageBases == nil {
		m.ImageBases = []string{}
	}
	if m.VideoOrigins == nil {
		m.VideoOrigins = []string{}
	}
	return m, nil
}

// BumpVersion 在站点配置变更后调用：version +1，清本地与 Valkey 缓存，并广播失效让各实例秒级拿到新版本。
// 返回新版本号。S3-3 白标分段保存、S4-4 手动清「站点设置」都应走这里，以便一次调用即完成跨实例失效。
func (s *SiteConfigService) BumpVersion(ctx context.Context) (int64, error) {
	version, err := s.repo.BumpVersion(ctx)
	if err != nil {
		return 0, err
	}
	s.invalidateLocal(ctx)
	if err := s.inval.Publish(ctx, SiteConfigTopic); err != nil {
		// 广播失败不影响本次写入：其它实例靠一级缓存的短 TTL 兜底，最多 siteConfigL1TTL 后自然生效。
		slog.WarnContext(ctx, "广播站点配置失效失败", "err", err)
	}
	return version, nil
}

// OnInvalidate 收到失效广播后调用：清掉本实例的本地与 Valkey 缓存，下次读回源最新配置。
func (s *SiteConfigService) OnInvalidate(ctx context.Context) { s.invalidateLocal(ctx) }

// invalidateLocal 清本进程的一级缓存并删除 Valkey 二级缓存键（幂等，多个实例同时删也安全）。
func (s *SiteConfigService) invalidateLocal(ctx context.Context) {
	s.local.Store(nil)
	if err := s.cache.Delete(ctx, siteConfigCacheKey); err != nil {
		slog.WarnContext(ctx, "清除站点配置缓存失败", "err", err)
	}
}
