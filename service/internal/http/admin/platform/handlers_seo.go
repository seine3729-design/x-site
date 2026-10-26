package platform

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/platform"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// SEOService 是 SEO 与统计代码（domain/platform.SEOService）。
type SEOService interface {
	Get(ctx context.Context) (dom.SEOConfig, error)
	Set(ctx context.Context, in dom.SEO) (dom.SEOConfig, error)
	Analytics(ctx context.Context) (dom.Analytics, error)
	SetAnalytics(ctx context.Context, in dom.Analytics) (dom.Analytics, error)
}

// GetSEO 是 GET /api/admin/seo：SEO 段 + 页面与可用变量清单（前端的「点击插入」照此渲染）。
// 统计代码不在这里返回：它另有权限点，没有权限的人连内容都不该读到。
func (h *Handlers) GetSEO(c *gin.Context) {
	out, err := h.SEO.Get(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

// SetSEO 是 PUT /api/admin/seo：整体保存 SEO 段。
func (h *Handlers) SetSEO(c *gin.Context) {
	var req dom.SEO
	if !admin.Bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	before, err := h.SEO.Get(ctx)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out, err := h.SEO.Set(ctx, req)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "site_config.seo", TargetType: "site_config", TargetID: "seo",
		Before: before.SEO, After: out.SEO,
	})
	respond.OK(c, out)
}

// GetAnalytics 是 GET /api/admin/analytics-code：统计代码（要 seo.analytics_code）。
func (h *Handlers) GetAnalytics(c *gin.Context) {
	out, err := h.SEO.Analytics(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

// SetAnalytics 是 PUT /api/admin/analytics-code：整体保存统计代码（要 seo.analytics_code）。
//
// 这段代码会原样注入每个页面，等于给站点注入任意脚本，所以审计里记下前后全文：
// 出问题时要能查到是谁放了什么进去。
func (h *Handlers) SetAnalytics(c *gin.Context) {
	var req dom.Analytics
	if !admin.Bind(c, &req) {
		return
	}
	ctx := c.Request.Context()
	before, err := h.SEO.Analytics(ctx)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out, err := h.SEO.SetAnalytics(ctx, req)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "site_config.analytics", TargetType: "site_config", TargetID: "analytics",
		Before: before, After: out,
	})
	respond.OK(c, out)
}
