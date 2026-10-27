package storage

// 线路域名池（docs/04 F、docs/12 第 6 节）：随 GET /api/v1/site-config 下发给前台与 APP。
//
// 为什么要下发：
//   - 列表与详情接口返回的封面是**相对 key**（`img/drama_cover/xxx.webp`），客户端要用图片线路的
//     访问前缀拼成完整地址（docs/12 第 6 节），首选失败换下一条；
//   - 前台封面走 `fetch` + `blob:` 显示，受 CSP `connect-src` 约束而不是 `img-src`，
//     所以图片与视频线路的域名必须出现在 CSP 里（docs/04 F）。
//
// 这两件事原先靠部署期环境变量（apps 的 IMAGE_BASE_URLS / MEDIA_CONNECT_ORIGINS）手工与本表对齐，
// 漏同步的表现是「封面一张都不显示、且控制台没有任何报错」。改为随站点配置下发后跟着后台线路表自动变。

import (
	"context"
	"log/slog"
	"net/url"
	"slices"
	"sort"
	"strings"
)

// DomainPool 是下发给客户端的线路域名池。
type DomainPool struct {
	// ImageBases 是图片线路的访问前缀（含 base_url 里的路径前缀），按线路优先级排列、已去重。
	// 客户端把图片的相对 key 拼在其后，首选加载失败换下一条。
	ImageBases []string `json:"image_bases"`
	// VideoOrigins 是视频线路的 scheme://host[:port]，已去重。
	// 切片地址由清单给出、客户端不需要自己拼，所以这里只给域名（供 CSP connect-src 登记）。
	VideoOrigins []string `json:"video_origins"`
}

// PoolRepo 是读取域名池需要的仓储子集。
type PoolRepo interface {
	ListLines(ctx context.Context, q LineQuery) ([]Line, error)
}

// PoolService 组装线路域名池。它只读线路表，不涉及地区、健康分与签名：
// 域名池是「这个站点可能从哪些域名取资源」的全集，和按 IP 选线（selection.go）是两回事，
// 按地区裁剪过的列表当 CSP 用会把别的地区用户的域名漏掉。
type PoolService struct{ repo PoolRepo }

func NewPoolService(repo PoolRepo) *PoolService { return &PoolService{repo: repo} }

// DomainPool 返回当前会被下发的线路（active 与 watch，口径同 countUsable）的域名池。
func (s *PoolService) DomainPool(ctx context.Context) (DomainPool, error) {
	images, err := s.basesOf(ctx, LineImage)
	if err != nil {
		return DomainPool{}, err
	}
	videos, err := s.basesOf(ctx, LineVideo)
	if err != nil {
		return DomainPool{}, err
	}
	return DomainPool{ImageBases: images, VideoOrigins: originsOf(videos)}, nil
}

// basesOf 返回某类线路的访问前缀，按优先级倒序、同优先级按编号升序（与选线的排序基准一致），已去重。
// 直连存储的线路没有独立访问域名（base_url 为空），不进域名池。
func (s *PoolService) basesOf(ctx context.Context, kind LineKind) ([]string, error) {
	lines, err := s.repo.ListLines(ctx, LineQuery{Kind: kind})
	if err != nil {
		return nil, err
	}
	usable := make([]Line, 0, len(lines))
	for _, l := range lines {
		if l.Status == LineDisabled || strings.TrimSpace(l.BaseURL) == "" {
			continue
		}
		usable = append(usable, l)
	}
	// 仓储按 provider_id 排序在前，这里要的是全站范围内的优先级顺序，所以重新排一次。
	sort.SliceStable(usable, func(i, j int) bool {
		if usable[i].Priority != usable[j].Priority {
			return usable[i].Priority > usable[j].Priority
		}
		return usable[i].ID < usable[j].ID
	})
	out := make([]string, 0, len(usable))
	for _, l := range usable {
		base := strings.TrimRight(strings.TrimSpace(l.BaseURL), "/")
		if base != "" && !slices.Contains(out, base) {
			out = append(out, base)
		}
	}
	return out, nil
}

// originsOf 把访问前缀收敛成 scheme://host[:port]。base_url 允许带路径前缀，所以必须解析后重拼，
// 不能截字符串；解析不出来的地址直接丢掉（CSP 里写坏值会让整条策略失效）。
func originsOf(bases []string) []string {
	out := make([]string, 0, len(bases))
	for _, base := range bases {
		u, err := url.Parse(base)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		origin := u.Scheme + "://" + u.Host
		if !slices.Contains(out, origin) {
			out = append(out, origin)
		}
	}
	return out
}

// VersionBumper 令站点配置版本 +1 并广播失效（platform.SiteConfigService）。
// 线路域名池随站点配置下发，所以线路与地区规则的变更必须调用它（docs/12 第 4.4 节末），
// 否则前台与 CDN 会继续用旧的 ETag 与旧域名。不调用也不会永久不一致——
// 站点配置的一级 / 二级缓存 TTL（3 秒 / 30 秒）会兜底刷新，调用它是为了「秒级生效」。
type VersionBumper interface {
	BumpVersion(ctx context.Context) (int64, error)
}

// bumpSiteVersion 在线路变更后刷新站点配置版本。未注入时静默跳过（一次性脚本与测试不需要）。
// 失败只记日志、不回滚已写入的线路：线路本身已经生效，前台最多等一个缓存 TTL。
func bumpSiteVersion(ctx context.Context, v VersionBumper) {
	if v == nil {
		return
	}
	if _, err := v.BumpVersion(ctx); err != nil {
		slog.ErrorContext(ctx, "线路变更后更新站点配置版本失败，前台可能仍用旧的线路域名", "err", err)
	}
}
