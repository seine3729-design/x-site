package config

import (
	"errors"
	"strings"
	"time"

	"xsite/service/internal/crypto/envelope"
)

// StorageKEKInfo 是存储源凭据 KEK 的 HKDF info（区分用途，docs/10 第 3.2 节）。
const StorageKEKInfo = "xsite-storage-kek/v1"

// Storage 是存储源（S1-6a，docs/12）的配置。存储源的桶、端点、凭据都在后台配置并入库，
// 这里只保留加密凭据用的包装密钥（S2-8 接入 KMS 前的过渡方案）与线路健康探测参数。
type Storage struct {
	// KEKs 是存储源凭据的包装密钥 `kid:secret[,kid2:secret2]`（docs/10 第 3.2 节、第 7 节）。
	KEKs string `env:"STORAGE_KEKS"`

	// SignKeys 是线路的媒体签名密钥 `引用:密钥[,引用2:密钥2]`（docs/10 第 8 节：密钥管理下发到 service 与 CDN 边缘，
	// 不入库）。线路的 sign_key_ref 指向其中一个引用；轮换时新增一个引用、边缘同时配置两把，线路切换后再删旧的。
	SignKeys string `env:"LINE_SIGN_KEYS"`

	// GeoIPXDBPath 是 ip2region xdb 数据库的路径（S1-6c，docs/12 第 4.2 节）。
	// 留空时地区解析一律为未知（XX），只有 region_scope=global 的兜底线路会命中；
	// 配了路径但文件损坏时启动失败（部署配置问题，不该静默降级）。
	// 文件更新后由 worker 周期检查 mtime 并原子热替换，不需要重启。
	GeoIPXDBPath string `env:"GEOIP_XDB_PATH"`
	// GeoIPReloadInterval 是检查数据文件是否更新的间隔。
	GeoIPReloadInterval time.Duration `env:"GEOIP_RELOAD_INTERVAL" envDefault:"10m"`

	// 孤立产物的保留期与清理周期（S1-6f，docs/12 第 5 节第 6 条）：分集删除或重新转封装后产物不再被引用，
	// 过保留期才连同各存储源上的文件一起删除（回收站语义）。production 下保留期不建议短于 7 天。
	OrphanRetention time.Duration `env:"MEDIA_ORPHAN_RETENTION" envDefault:"720h"`
	CleanupInterval time.Duration `env:"MEDIA_CLEANUP_INTERVAL" envDefault:"24h"`

	// 线路健康探测（S1-6d，docs/12 第 4.3 节）：worker 周期性拉每条线路的 probe_path，
	// 把成功率与耗时换算成健康分（0–100）写入 Valkey，供选线算法与后台健康看板使用。
	// Enabled 关闭时 worker 不注册探测任务；健康分缺失时选线按满分处理（不因无数据过滤线路）。
	HealthProbeEnabled  bool          `env:"STORAGE_HEALTH_PROBE_ENABLED" envDefault:"true"`
	HealthProbeInterval time.Duration `env:"STORAGE_HEALTH_PROBE_INTERVAL" envDefault:"1m"`
	HealthProbeTimeout  time.Duration `env:"STORAGE_HEALTH_PROBE_TIMEOUT" envDefault:"5s"`
	HealthTTL           time.Duration `env:"STORAGE_HEALTH_TTL" envDefault:"10m"`
}

// Keyring 解析存储源凭据的 KEK 密钥环；KEKs 为空或格式错误时返回错误。
func (s Storage) Keyring() (*envelope.Keyring, error) {
	return envelope.ParseKeyring(s.KEKs, StorageKEKInfo)
}

// ValidateCleanup 校验孤立产物清理的配置（worker 启动时调用）。清理会删文件，配置写错的代价高，所以单独校验。
func (s Storage) ValidateCleanup(appEnv string) error {
	var problems []string
	if s.OrphanRetention < time.Hour {
		problems = append(problems, "MEDIA_ORPHAN_RETENTION 至少 1h（删掉的产物不可恢复）")
	}
	if appEnv == "production" && s.OrphanRetention < 7*24*time.Hour {
		problems = append(problems, "production 环境 MEDIA_ORPHAN_RETENTION 不应短于 168h（7 天）")
	}
	if s.CleanupInterval < time.Minute || s.CleanupInterval > 7*24*time.Hour {
		problems = append(problems, "MEDIA_CLEANUP_INTERVAL 应在 1m–168h 之间")
	}
	if len(problems) > 0 {
		return errors.New("孤立产物清理配置校验失败: " + strings.Join(problems, "；"))
	}
	return nil
}

// ValidateHealthProbe 校验线路健康探测配置，worker 在启用探测时调用；问题一次全部列出。
func (s Storage) ValidateHealthProbe() error {
	var problems []string
	if s.HealthProbeInterval < 15*time.Second || s.HealthProbeInterval > time.Hour {
		problems = append(problems, "STORAGE_HEALTH_PROBE_INTERVAL 应在 15s–1h 之间")
	}
	if s.HealthProbeTimeout < time.Second || s.HealthProbeTimeout > 30*time.Second {
		problems = append(problems, "STORAGE_HEALTH_PROBE_TIMEOUT 应在 1s–30s 之间")
	}
	if s.HealthTTL <= s.HealthProbeInterval {
		problems = append(problems, "STORAGE_HEALTH_TTL 应大于 STORAGE_HEALTH_PROBE_INTERVAL（否则探测间隔内健康分会过期）")
	}
	if len(problems) > 0 {
		return errors.New("线路健康探测配置校验失败: " + strings.Join(problems, "；"))
	}
	return nil
}
