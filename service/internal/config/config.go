// Package config 从环境变量加载服务配置，并在启动时校验。
// 缺少必填项或取值不合法时直接返回错误，进程不会带着危险默认值启动。
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	"xsite/service/internal/crypto/apienc"
	"xsite/service/internal/crypto/envelope"
)

// AdminTOTPKEKInfo 是后台 TOTP 种子 KEK 的 HKDF info（区分用途，docs/10 第 3.2 节）。
const AdminTOTPKEKInfo = "xsite-admin-totp/v1"

// Config 是 api 与 worker 进程共用的配置。
type Config struct {
	AppEnv   string `env:"APP_ENV" envDefault:"production"`
	HTTPAddr string `env:"HTTP_ADDR" envDefault:":8080"`
	LogLevel string `env:"LOG_LEVEL" envDefault:"info"`

	DatabaseURL   string `env:"DATABASE_URL,required"`
	DatabaseROURL string `env:"DATABASE_RO_URL"`
	ValkeyURL     string `env:"VALKEY_URL,required"`

	// AutoMigrate 为 true 时，进程启动会执行数据库迁移。只建议在本地开发开启，线上由发布流程执行。
	AutoMigrate bool `env:"AUTO_MIGRATE" envDefault:"false"`

	// BFFServiceToken 是 apps BFF 调用后端时携带的服务令牌。
	BFFServiceToken string `env:"BFF_SERVICE_TOKEN,required"`
	// EdgeSharedSecret 是 CDN 回源时携带的共享密钥（x-edge-secret）。
	EdgeSharedSecret string `env:"EDGE_SHARED_SECRET"`
	// EdgeAuthRequired 未设置时：development 不强制，其余环境强制。
	EdgeAuthRequired *bool `env:"EDGE_AUTH_REQUIRED"`
	// TrustedProxies 是可信代理网段（CDN 回源、BFF），只有来自这些网段的 X-Forwarded-For 才会被采信。
	TrustedProxies []string `env:"TRUSTED_PROXIES" envSeparator:","`

	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"20s"`

	// APIEncKeys 是用户端接口报文加密密钥 `kid:secret,...`（第一把为当前密钥，其余用于轮换过渡）。
	APIEncKeys string `env:"API_ENC_KEYS,required"`
	// APIEncRequired 未设置时：development 允许明文（方便 curl 调试），其余环境强制加密。
	APIEncRequired *bool `env:"API_ENCRYPTION_REQUIRED"`

	// AdminOrigins 是运营后台页面的来源（如 https://admin.example.com，逗号分隔），
	// /api/admin 的写请求带 Origin 时必须在其中（docs/03 第 2.3 节）。production 只允许 https。
	AdminOrigins []string `env:"ADMIN_ORIGINS,required" envSeparator:","`

	// AdminTOTPKEKs 是后台二次验证 TOTP 种子的包装密钥 `kid:secret[,...]`（docs/10 第 6.3 节，S2-6）。
	// 留空时后台 TOTP 绑定不可用（接口返回「未配置」）；production 必须配置（见 validate）。
	AdminTOTPKEKs string `env:"ADMIN_TOTP_KEKS"`

	// EgressAllowPrivate 控制连接后台可配置的地址（资源机 FTP 等）时能否访问内网、回环与保留地址段（docs/12 第 9 节）。
	// 未设置时 development 允许（本地资源机、SeaweedFS 在内网），其余环境禁止；production 不允许开启。
	EgressAllowPrivate *bool `env:"EGRESS_ALLOW_PRIVATE"`

	// FTPCredentials 是资源机账号密码，来自环境变量 FTP_<凭据标识>_USER / FTP_<凭据标识>_PASSWORD，只有 worker 使用。
	FTPCredentials FTPCredentials

	// Media 是对象存储与媒体转封装的配置（media.go）。
	Media Media

	// Storage 是存储源（S1-6a）的配置（storage.go）。
	Storage Storage
	// Play 是播放服务（S1-7）的配置（play.go）。只有 API 进程使用，在 app.NewAPI 中解析，缺失时 API 启动失败。
	Play Play
	// Auth 是用户端身份令牌（S1-10）的配置（auth.go）。只有 API 进程使用，在 app.NewAPI 中解析，缺失时 API 启动失败。
	Auth Auth
	// Platform 是系统功能（配置导出导入，S4-4）的配置（platform.go）。只有 API 进程使用。
	Platform Platform
}

// EgressPrivateAllowed 表示出站请求是否允许访问内网与保留地址段。
func (c Config) EgressPrivateAllowed() bool {
	if c.EgressAllowPrivate != nil {
		return *c.EgressAllowPrivate
	}
	return c.IsDev()
}

// APIEncEnforced 表示是否强制用户端接口加密。
func (c Config) APIEncEnforced() bool {
	if c.APIEncRequired != nil {
		return *c.APIEncRequired
	}
	return !c.IsDev()
}

// Load 读取并校验配置。
func Load() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("读取配置失败: %w", err)
	}
	cfg.AdminOrigins = trimAll(cfg.AdminOrigins)
	creds, credProblems := parseFTPCredentials(os.Environ())
	cfg.FTPCredentials = creds
	if err := cfg.validate(credProblems...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// IsDev 表示本地开发环境。
func (c Config) IsDev() bool { return c.AppEnv == "development" }

// AdminTOTPKeyring 解析后台 TOTP 种子的 KEK 密钥环；留空返回 (nil, nil)（功能不可用，但不阻止启动，便于本地）。
func (c Config) AdminTOTPKeyring() (*envelope.Keyring, error) {
	if strings.TrimSpace(c.AdminTOTPKEKs) == "" {
		return nil, nil
	}
	return envelope.ParseKeyring(c.AdminTOTPKEKs, AdminTOTPKEKInfo)
}

// EdgeAuthEnforced 表示是否强制要求回源鉴权。
func (c Config) EdgeAuthEnforced() bool {
	if c.EdgeAuthRequired != nil {
		return *c.EdgeAuthRequired
	}
	return !c.IsDev()
}

// validate 校验配置；extra 是加载阶段发现的问题（如资源机账号密码没有成对配置）。
func (c Config) validate(extra ...string) error {
	problems := append([]string(nil), extra...)
	switch c.AppEnv {
	case "development", "test", "production":
	default:
		problems = append(problems, "APP_ENV 必须是 development、test 或 production")
	}
	if len(c.BFFServiceToken) < 32 {
		problems = append(problems, "BFF_SERVICE_TOKEN 长度至少 32 个字符")
	}
	if c.EdgeAuthEnforced() && len(c.EdgeSharedSecret) < 32 {
		problems = append(problems, "强制回源鉴权时 EDGE_SHARED_SECRET 长度至少 32 个字符")
	}
	if c.AutoMigrate && c.AppEnv == "production" {
		problems = append(problems, "production 环境不允许 AUTO_MIGRATE，请在发布流程中执行迁移")
	}
	if c.AppEnv == "production" && c.APIEncRequired != nil && !*c.APIEncRequired {
		problems = append(problems, "production 环境不允许关闭 API_ENCRYPTION_REQUIRED")
	}
	if c.AppEnv == "production" && c.EgressPrivateAllowed() {
		problems = append(problems, "production 环境不允许开启 EGRESS_ALLOW_PRIVATE（出站请求禁止访问内网与保留地址段）")
	}
	if _, err := apienc.ParseKeyring(c.APIEncKeys); err != nil {
		problems = append(problems, "API_ENC_KEYS 无效："+err.Error())
	}
	if c.Storage.KEKs == "" {
		problems = append(problems, "STORAGE_KEKS 不能为空（存储源凭据的包装密钥 kid:secret，S1-6a）")
	} else if _, err := c.Storage.Keyring(); err != nil {
		problems = append(problems, "STORAGE_KEKS 无效："+err.Error())
	}
	if len(c.AdminOrigins) == 0 {
		problems = append(problems, "ADMIN_ORIGINS 不能为空（运营后台页面的来源，如 https://admin.example.com）")
	}
	for _, o := range c.AdminOrigins {
		if !validOrigin(o, c.AppEnv == "production") {
			problems = append(problems, fmt.Sprintf("ADMIN_ORIGINS 中的 %q 不是合法来源（scheme://host[:port]，production 只允许 https）", o))
		}
	}
	if c.AdminTOTPKEKs != "" {
		if _, err := c.AdminTOTPKeyring(); err != nil {
			problems = append(problems, "ADMIN_TOTP_KEKS 无效："+err.Error())
		}
	} else if c.AppEnv == "production" {
		problems = append(problems, "production 环境必须配置 ADMIN_TOTP_KEKS（后台二次验证 TOTP 种子的包装密钥，S2-6）")
	}
	if len(problems) > 0 {
		return errors.New("配置校验失败: " + strings.Join(problems, "；"))
	}
	return nil
}

// validOrigin 只接受浏览器 Origin 头的格式：scheme://host[:port]，不带路径、查询串与用户信息。
func validOrigin(o string, httpsOnly bool) bool {
	u, err := url.Parse(o)
	if err != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return false
	}
	return u.Scheme == "https" || (u.Scheme == "http" && !httpsOnly)
}

func trimAll(items []string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
