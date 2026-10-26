package domains

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/domains"
	"xsite/service/internal/http/admin"
)

// Service 是域名与证书服务（domain/domains.Service）。
type Service interface {
	List(ctx context.Context, f dom.Filter) ([]dom.Entry, error)
	Create(ctx context.Context, in dom.EntryInput) (dom.Entry, error)
	Update(ctx context.Context, id int64, in dom.EntryInput) (before, after dom.Entry, err error)
	Delete(ctx context.Context, id int64) (dom.Entry, error)
	GenerateCert(ctx context.Context, in dom.GenCertInput) (dom.GeneratedCert, error)
	InspectCert(ctx context.Context, pemText string) (dom.CertInfo, error)
	UploadCertForDomain(ctx context.Context, id int64, pemText string) (dom.Entry, dom.CertInfo, error)
	LatestRelease(ctx context.Context) (dom.Release, bool, error)
	ListReleases(ctx context.Context, page, pageSize int) ([]dom.Release, int64, error)
	PreviewRelease(ctx context.Context, fromVersion *int64) (dom.ReleasePreview, error)
	PublishRelease(ctx context.Context, in dom.PublishInput) (dom.Release, error)
	Health(ctx context.Context) (dom.HealthOverview, error)
}

// Handlers 聚合域名与证书 handler。
type Handlers struct {
	Svc Service
}

// Routes 把域名与证书接口注册到 admin.Routes 返回的分组上。
//
// 读要 domains.view，写要 domains.edit（高敏，S2-6 起需重新验证）。证书生成与清单发布是高敏写操作。
// 健康看板与证书生成用独立顶层路径（/domain-health、/domain-certificates），
// 避免与 /domains/:id 的参数段在同一层冲突（gin 不允许静态段与参数段同层）。
func (h *Handlers) Routes(authed gin.IRouter) {
	page := admin.RequirePage("domains")
	view := admin.RequirePermission("domains.view")
	edit := admin.RequirePermission("domains.edit")

	d := authed.Group("/domains")
	d.GET("", page, h.List)
	d.POST("", page, h.Create)
	d.PATCH("/:id", page, h.Update)
	d.DELETE("/:id", page, h.Delete)
	d.POST("/:id/certificate", page, h.UploadCert)

	authed.GET("/domain-health", view, h.Health)

	certs := authed.Group("/domain-certificates")
	certs.POST("", edit, h.GenerateCert)
	certs.POST("/inspect", view, h.InspectCert)

	rel := authed.Group("/domain-releases")
	rel.GET("", view, h.ListReleases)
	rel.GET("/preview", view, h.PreviewRelease)
	rel.POST("", edit, h.PublishRelease)
}
