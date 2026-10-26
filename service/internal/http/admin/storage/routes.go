// Package storage 是运营后台的存储源、线路、设置与选线预览接口（docs/07 第 2 节，docs/12 第 4.4、8 节，S1-6a/b/d）。
// 会话、CSRF、审计中间件由 admin.Routes 返回的分组提供，这里按页面声明权限点。
package storage

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/storage"
	"xsite/service/internal/http/admin"
)

// Service 是存储源服务（domain/storage.Service）。
type Service interface {
	List(ctx context.Context, f dom.Filter) (dom.Page[dom.Provider], error)
	Create(ctx context.Context, in dom.Input) (dom.Provider, error)
	Update(ctx context.Context, id int64, in dom.Input) (before, after dom.Provider, err error)
	Delete(ctx context.Context, id int64) (dom.Provider, error)
	Test(ctx context.Context, id int64) (dom.ProbeResult, error)
}

// LineService 是线路服务（domain/storage.LineService）。
type LineService interface {
	ListLines(ctx context.Context, f dom.LineFilter) ([]dom.Line, error)
	CreateLine(ctx context.Context, in dom.LineInput) (dom.Line, error)
	UpdateLine(ctx context.Context, id int64, in dom.LineInput) (before, after dom.Line, err error)
	DeleteLine(ctx context.Context, id int64) (dom.Line, error)
	SetRules(ctx context.Context, lineID int64, rules []dom.RuleInput) ([]dom.Rule, error)
	TestLine(ctx context.Context, id int64) (dom.LineProbeResult, error)
}

// SettingsService 是存储全局设置服务（domain/storage.SettingsService）。
type SettingsService interface {
	Get(ctx context.Context) (dom.Settings, error)
	// Overview 是设置 + 交付自检，供设置页提示「上传的资源前台取不到」。
	Overview(ctx context.Context) (dom.SettingsView, error)
	Update(ctx context.Context, in dom.SettingsInput) (dom.Settings, error)
}

// PreviewService 是选线预览服务（domain/storage.PreviewService）。
type PreviewService interface {
	Preview(ctx context.Context, in dom.PreviewInput) (dom.PreviewResult, error)
}

// HealthService 是线路健康看板服务（domain/storage.HealthService）。
type HealthService interface {
	Overview(ctx context.Context) (dom.HealthOverview, error)
}

// ManageService 是存储源的批量运维操作（domain/storage.ManageService，S1-6f）。
type ManageService interface {
	Replicate(ctx context.Context, req dom.ReplicateRequest) (dom.EnqueueResult, error)
	Migrate(ctx context.Context, req dom.MigrateRequest) (dom.EnqueueResult, error)
}

// AdoptService 把历史产物登记为存储源副本（domain/storage.AdoptService，S1-6f）。
type AdoptService interface {
	Adopt(ctx context.Context, req dom.AdoptRequest) (dom.EnqueueResult, error)
}

// CleanupService 清理孤立产物（domain/storage.CleanupService，S1-6f）。
type CleanupService interface {
	Cleanup(ctx context.Context, dryRun bool, now time.Time) (dom.CleanupResult, error)
	Stats(ctx context.Context, now time.Time) (dom.OrphanStats, error)
	Retention() time.Duration
}

// Handlers 聚合存储源、线路、设置、选线预览与健康看板 handler。
type Handlers struct {
	Svc         Service
	LineSvc     LineService
	SettingsSvc SettingsService
	PreviewSvc  PreviewService
	HealthSvc   HealthService
	ManageSvc   ManageService
	AdoptSvc    AdoptService
	CleanupSvc  CleanupService
}

// Routes 把存储源、线路、设置与选线预览接口注册到 admin.Routes 返回的分组上。
// 增删改要 *.edit（高敏，S2-6 起需重新验证）；测试 / 预览不改数据，只要 *.view（仍是写请求的要 CSRF 并记审计）。
func (h *Handlers) Routes(authed gin.IRouter) {
	page := admin.RequirePage("storage_providers")
	view := admin.RequirePermission("storage_providers.view")
	g := authed.Group("/storage/providers")
	g.GET("", page, h.List)
	g.POST("", page, h.Create)
	g.PATCH("/:id", page, h.Update)
	g.DELETE("/:id", page, h.Delete)
	g.POST("/:id/test", view, h.Test)

	linePage := admin.RequirePage("storage_lines")
	lineView := admin.RequirePermission("storage_lines.view")
	lg := authed.Group("/storage/lines")
	lg.GET("", linePage, h.ListLines)
	lg.POST("", linePage, h.CreateLine)
	lg.PATCH("/:id", linePage, h.UpdateLine)
	lg.DELETE("/:id", linePage, h.DeleteLine)
	lg.PUT("/:id/rules", linePage, h.SetLineRules)
	lg.POST("/:id/test", lineView, h.TestLine)

	settingsPage := admin.RequirePage("storage_settings")
	sg := authed.Group("/storage/settings")
	sg.GET("", settingsPage, h.GetSettings)
	sg.PUT("", settingsPage, h.UpdateSettings)

	// 批量运维（S1-6f）：手动补副本与迁移都要改数据，按存储设置页的修改权限判定
	edit := admin.RequirePermission("storage_settings.edit")
	authed.POST("/storage/replicate", edit, h.Replicate)
	authed.POST("/storage/migrate", edit, h.Migrate)
	authed.POST("/storage/adopt", edit, h.Adopt)
	authed.POST("/storage/cleanup", edit, h.Cleanup)
	authed.GET("/storage/orphans", admin.RequirePermission("storage_settings.view"), h.OrphanStats)

	// 选线预览与健康看板（只读）：storage_preview 页面只有查看权限
	authed.GET("/storage/preview", admin.RequirePermission("storage_preview.view"), h.Preview)
	authed.GET("/storage/health", admin.RequirePermission("storage_preview.view"), h.Health)
}
