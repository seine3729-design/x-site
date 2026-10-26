package platform

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/platform"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// BrandingService 是品牌、配色与布局（domain/platform.BrandingService）。
type BrandingService interface {
	Get(ctx context.Context) (dom.BrandingConfig, error)
	SetBrand(ctx context.Context, in dom.Brand) (dom.BrandingConfig, error)
	SetTheme(ctx context.Context, in dom.Theme) (dom.BrandingConfig, error)
	SetLayout(ctx context.Context, in dom.Layout) (dom.BrandingConfig, error)
}

// GetBranding 是 GET /api/admin/brand：品牌、配色、布局三段 + 各下拉的可选值。
// 一个页面三张卡，所以一次全给，省掉三次请求；保存仍然分三个接口。
func (h *Handlers) GetBranding(c *gin.Context) {
	out, err := h.Branding.Get(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

// SetBrand 是 PUT /api/admin/brand：整体保存品牌段。
func (h *Handlers) SetBrand(c *gin.Context) {
	var req dom.Brand
	if !admin.Bind(c, &req) {
		return
	}
	h.saveBranding(c, "site_config.site", "site", func(ctx context.Context) (dom.BrandingConfig, error) {
		return h.Branding.SetBrand(ctx, req)
	}, func(cfg dom.BrandingConfig) any { return cfg.Brand })
}

// SetTheme 是 PUT /api/admin/theme：整体保存配色段。
func (h *Handlers) SetTheme(c *gin.Context) {
	var req dom.Theme
	if !admin.Bind(c, &req) {
		return
	}
	h.saveBranding(c, "site_config.theme", "theme", func(ctx context.Context) (dom.BrandingConfig, error) {
		return h.Branding.SetTheme(ctx, req)
	}, func(cfg dom.BrandingConfig) any { return cfg.Theme })
}

// SetLayout 是 PUT /api/admin/layout：整体保存布局段。
func (h *Handlers) SetLayout(c *gin.Context) {
	var req dom.Layout
	if !admin.Bind(c, &req) {
		return
	}
	h.saveBranding(c, "site_config.layout", "layout", func(ctx context.Context) (dom.BrandingConfig, error) {
		return h.Branding.SetLayout(ctx, req)
	}, func(cfg dom.BrandingConfig) any { return cfg.Layout })
}

// saveBranding 保存一段并记审计（只记这一段的前后值，不把另外两段也塞进审计）。
func (h *Handlers) saveBranding(
	c *gin.Context, action, target string,
	save func(context.Context) (dom.BrandingConfig, error),
	segment func(dom.BrandingConfig) any,
) {
	ctx := c.Request.Context()
	before, err := h.Branding.Get(ctx)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out, err := save(ctx)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: action, TargetType: "site_config", TargetID: target,
		Before: segment(before), After: segment(out),
	})
	respond.OK(c, out)
}
