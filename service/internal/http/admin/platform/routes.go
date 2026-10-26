// Package platform 是运营后台的平台接口（docs/07 第 2 节「系统」）：缓存管理（S4-4）。
// 会话、CSRF、审计中间件由 admin.Routes 返回的分组提供，这里按页面声明权限点。
package platform

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/platform"
	"xsite/service/internal/http/admin"
)

// Service 是缓存管理服务（domain/platform.CacheService）。
type Service interface {
	Overview(ctx context.Context) (dom.Overview, error)
	Purge(ctx context.Context, scope dom.PurgeScope, dramaID int64) (dom.PurgeResult, error)
}

// Handlers 聚合平台 handler。
type Handlers struct {
	Cache    Service
	Config   ConfigService
	Audit    AuditService
	Trash    TrashService
	Features FeatureService
	APIKeys  APIKeyService
	Branding BrandingService
	SEO      SEOService
}

// Routes 注册缓存与配置导出导入接口。
//
// RequirePage 按方法分流：读请求要 <页面>.view，写请求要 <页面>.edit。
// 导出是 GET（只要 config.view）、导入是 POST（要 config.edit，迁移 00003 已标为高敏）。
func (h *Handlers) Routes(authed gin.IRouter) {
	cachePage := admin.RequirePage("cache")
	cache := authed.Group("/cache")
	cache.GET("", cachePage, h.CacheOverview)
	cache.POST("/purge", cachePage, h.PurgeCache)

	configPage := admin.RequirePage("config")
	cfg := authed.Group("/config", configPage)
	cfg.GET("/export", h.ExportConfig)
	cfg.GET("/backups", h.ListConfigBackups)
	cfg.GET("/backups/:id", h.GetConfigBackup)
	cfg.POST("/import", h.ImportConfig)
	cfg.POST("/backups/:id/restore", h.RestoreConfigBackup)

	// 审计日志只读：迁移 00003 没有给 audit 页面生成 .edit 权限点，所以用 RequirePermission 而不是 RequirePage。
	// 哈希链校验也是读操作（不改数据），同样只要 audit.view。
	auditView := admin.RequirePermission("audit.view")
	au := authed.Group("/audit", auditView)
	au.GET("", h.ListAudit)
	au.GET("/facets", h.AuditFacets)
	au.GET("/verify", h.VerifyAudit)
	au.GET("/:id", h.GetAudit)

	// 回收站：查看要 trash.view，还原与彻底删除要 trash.edit（RequirePage 按方法分流）
	trashPage := admin.RequirePage("trash")
	tr := authed.Group("/trash", trashPage)
	tr.GET("", h.TrashOverview)
	tr.GET("/items", h.ListTrash)
	tr.POST("/items/:id/restore", h.RestoreTrash)
	tr.POST("/items/:id/purge", h.PurgeTrash)

	// 功能开关与年龄门（docs/06 第 36 节）：两段配置分开保存，改开关不影响年龄门
	featuresPage := admin.RequirePage("features")
	ft := authed.Group("/features", featuresPage)
	ft.GET("", h.GetFeatures)
	ft.PUT("/flags", h.SetFeatureFlags)
	ft.PUT("/age-gate", h.SetAgeGate)

	// 品牌、配色与布局（docs/06 第 16 节）：一次 GET 给三段，三个 PUT 分别保存，
	// 这样改配色不会把别人正在编辑的布局一起写回去
	brandPage := admin.RequirePage("brand")
	br := authed.Group("", brandPage)
	br.GET("/brand", h.GetBranding)
	br.PUT("/brand", h.SetBrand)
	br.PUT("/theme", h.SetTheme)
	br.PUT("/layout", h.SetLayout)

	// SEO 与统计代码（docs/06 第 17 节）：SEO 段走 seo.view / seo.edit；
	// 统计代码会被原样注入页面，等于给站点注入任意脚本，所以单独要 seo.analytics_code（迁移 00003 已标为高敏，默认只有超管），
	// 读也要这个权限——没有权限的人连现在埋了什么都不该看到
	seoPage := admin.RequirePage("seo")
	seo := authed.Group("/seo", seoPage)
	seo.GET("", h.GetSEO)
	seo.PUT("", h.SetSEO)
	analyticsPerm := admin.RequirePermission("seo.analytics_code")
	ac := authed.Group("/analytics-code", analyticsPerm)
	ac.GET("", h.GetAnalytics)
	ac.PUT("", h.SetAnalytics)

	// 开放平台 API Key（docs/06 第 32 节）：生成要 apikeys.edit（高敏）；没有删除接口，
	// 停用即时失效就够了，留着记录才能回答「这把密钥曾经存在、被谁用过」
	keysPage := admin.RequirePage("apikeys")
	ak := authed.Group("/apikeys", keysPage)
	ak.GET("", h.ListAPIKeys)
	ak.POST("", h.CreateAPIKey)
	ak.PATCH("/:id", h.UpdateAPIKey)
	ak.POST("/:id/disable", h.DisableAPIKey)
	ak.POST("/:id/enable", h.EnableAPIKey)
}
