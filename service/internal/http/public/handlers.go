// Package public 是用户端 /api/v1/* 的 handler。
package public

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/crypto/apienc"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/domain/site"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// SiteConfigGetter 是 handler 依赖的最小接口，便于测试替换。
type SiteConfigGetter interface {
	Get(ctx context.Context) (platform.SiteConfig, error)
}

// Pinger 用于就绪检查。
type Pinger interface {
	Ping(ctx context.Context) error
}

// APIKeySet 提供报文加密公钥。
type APIKeySet interface {
	Current() string
	PublicKeys() []apienc.PublicKey
}

// SiteSections 提供随站点配置一起下发的站点编排内容（domain/site，docs/specs/A3-1.md）：
// 前台菜单与页脚页。改动它们会令配置版本 +1，所以条件请求仍然准确。
type SiteSections interface {
	Menus(ctx context.Context) ([]site.PublicMenu, error)
	FooterPages(ctx context.Context) ([]site.FooterPage, error)
}

// Handlers 聚合用户端 handler。
type Handlers struct {
	Env        string
	SiteConfig SiteConfigGetter
	Site       SiteSections
	APIKeys    APIKeySet
	Ready      map[string]Pinger
}

// Health 是存活检查：只表示进程能处理请求，不检查依赖。
func (h *Handlers) Health(c *gin.Context) {
	respond.OK(c, gin.H{"ok": true, "env": h.Env})
}

// ReadyCheck 是就绪检查：数据库、缓存都可用才返回 200。
func (h *Handlers) ReadyCheck(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	result := gin.H{}
	ok := true
	for name, p := range h.Ready {
		if err := p.Ping(ctx); err != nil {
			result[name] = "down"
			ok = false
			continue
		}
		result[name] = "up"
	}
	if !ok {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, respond.Envelope{
			Code: errs.ErrUnavailable.Code, Message: errs.ErrUnavailable.Message, Data: result,
		})
		return
	}
	respond.OK(c, result)
}

// GetCryptoKeys 下发报文加密公钥（明文、可缓存）。客户端遇到 ENC_KEY_UNKNOWN 时重新拉取。
func (h *Handlers) GetCryptoKeys(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=300")
	respond.OK(c, gin.H{"current": h.APIKeys.Current(), "keys": h.APIKeys.PublicKeys()})
}

// siteConfigDTO 是下发给前台的站点配置：platform 的各段 + 站点编排的菜单与页脚页（docs/07 第 1 节）。
type siteConfigDTO struct {
	platform.SiteConfig
	Menus       []site.PublicMenu `json:"menus"`
	FooterPages []site.FooterPage `json:"footer_pages"`
}

// GetSiteConfig 返回站点配置，支持 ETag 条件请求（docs/03 第 9.1 节）。
// ETag 由版本号与下发内容一起派生：菜单项的链接目标被别的模块改掉（静态页改回草稿、频道取值停用）时
// 版本号不变，但下发内容会变，只用版本号会让客户端一直拿到 304。
func (h *Handlers) GetSiteConfig(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, err := h.SiteConfig.Get(ctx)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := siteConfigDTO{SiteConfig: cfg, Menus: []site.PublicMenu{}, FooterPages: []site.FooterPage{}}
	if h.Site != nil {
		if out.Menus, err = h.Site.Menus(ctx); err != nil {
			respond.Error(c, err)
			return
		}
		if out.FooterPages, err = h.Site.FooterPages(ctx); err != nil {
			respond.Error(c, err)
			return
		}
	}
	body, err := json.Marshal(out)
	if err != nil {
		respond.Error(c, err)
		return
	}
	sum := sha256.Sum256(body)
	c.Header("Cache-Control", "public, max-age=0, s-maxage=30, stale-while-revalidate=300")
	if respond.NotModified(c, fmt.Sprintf(`W/"site-%d-%s"`, cfg.Version, hex.EncodeToString(sum[:8]))) {
		return
	}
	respond.OK(c, json.RawMessage(body))
}
