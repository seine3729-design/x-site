// Package content 是运营后台的内容管理接口：剧集（含批量）、分集、标签、筛选维度与取值、专题
// （docs/07 第 2 节，S1-3）。会话、CSRF、审计中间件由 admin.Routes 返回的分组提供，这里按页面声明权限点。
package content

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/content"
	"xsite/service/internal/http/admin"
)

// Service 是内容管理服务（domain/content.Service）。
type Service interface {
	ListDramas(ctx context.Context, f dom.DramaFilter) (dom.Page[dom.Drama], error)
	GetDrama(ctx context.Context, id int64) (dom.DramaDetail, error)
	CreateDrama(ctx context.Context, in dom.DramaInput) (dom.Drama, error)
	UpdateDrama(ctx context.Context, id int64, in dom.DramaInput) (before, after dom.Drama, err error)
	DeleteDrama(ctx context.Context, id int64) (dom.Drama, error)
	SetDramaTags(ctx context.Context, id int64, tagIDs []int64) (before, after []dom.Tag, err error)
	SetDramaTerms(ctx context.Context, id int64, termIDs []int64) (before, after []dom.TermRef, err error)
	BulkDramas(ctx context.Context, in dom.BulkInput) (int64, error)

	ListEpisodes(ctx context.Context, dramaID int64) ([]dom.Episode, error)
	CreateEpisode(ctx context.Context, dramaID int64, in dom.EpisodeInput) (dom.Episode, error)
	UpdateEpisode(ctx context.Context, id int64, in dom.EpisodeInput) (before, after dom.Episode, err error)
	DeleteEpisode(ctx context.Context, id int64) (dom.Episode, error)

	ListTags(ctx context.Context, f dom.TagFilter) (dom.Page[dom.Tag], error)
	CreateTag(ctx context.Context, in dom.TagInput) (dom.Tag, error)
	UpdateTag(ctx context.Context, id int64, in dom.TagInput) (before, after dom.Tag, err error)
	MergeTag(ctx context.Context, sourceID, targetID int64) (int64, error)

	ListTaxonomies(ctx context.Context) ([]dom.Taxonomy, error)
	CreateTaxonomy(ctx context.Context, in dom.TaxonomyInput) (dom.Taxonomy, error)
	UpdateTaxonomy(ctx context.Context, id int64, in dom.TaxonomyInput) (before, after dom.Taxonomy, err error)
	DeleteTaxonomy(ctx context.Context, id int64) (dom.Taxonomy, error)
	ReorderTaxonomies(ctx context.Context, ids []int64) error
	CreateTerm(ctx context.Context, taxonomyID int64, in dom.TermInput) (dom.Term, error)
	UpdateTerm(ctx context.Context, taxonomyID, termID int64, in dom.TermInput) (before, after dom.Term, err error)
	DeleteTerm(ctx context.Context, taxonomyID, termID int64) (dom.Term, error)
	ReorderTerms(ctx context.Context, taxonomyID int64, ids []int64) error

	ListTopics(ctx context.Context, f dom.TopicFilter) (dom.Page[dom.Topic], error)
	GetTopic(ctx context.Context, id int64) (dom.TopicDetail, error)
	CreateTopic(ctx context.Context, in dom.TopicInput) (dom.Topic, error)
	UpdateTopic(ctx context.Context, id int64, in dom.TopicInput) (before, after dom.Topic, err error)
	DeleteTopic(ctx context.Context, id int64) (dom.Topic, error)
	SetTopicDramas(ctx context.Context, id int64, dramaIDs []int64) (before, after []int64, err error)
}

// Handlers 聚合内容管理 handler。
type Handlers struct {
	Svc Service
	// ResourceChecks 是资源体检（S4-5）。它跨内容与存储两域，所以是独立的 service。
	ResourceChecks ResourceCheckService
	// ResourceWarm 是资源预热（S4-5）。
	ResourceWarm ResourceWarmService
	// ContentImports 是 CSV 批量导入（S4-6）。它要用屏蔽词引擎，所以是独立的 service。
	ContentImports ContentImportService
	// Now 用于计算剧的显示状态（定时未到点）；为空时取当前时间。
	Now func() time.Time
}

func (h *Handlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Routes 把内容管理接口注册到 admin.Routes 返回的分组上。读接口要 <页面>.view，写接口要 <页面>.edit；分集属于剧集页面。
func (h *Handlers) Routes(authed gin.IRouter) {
	dramas := authed.Group("/dramas", admin.RequirePage("dramas"))
	dramas.GET("", h.ListDramas)
	dramas.POST("", h.CreateDrama)
	dramas.POST("/bulk", h.BulkDramas)
	dramas.GET("/:id", h.GetDrama)
	dramas.PATCH("/:id", h.UpdateDrama)
	dramas.DELETE("/:id", h.DeleteDrama)
	dramas.PUT("/:id/tags", h.SetDramaTags)
	dramas.PUT("/:id/terms", h.SetDramaTerms)
	dramas.GET("/:id/episodes", h.ListEpisodes)
	dramas.POST("/:id/episodes", h.CreateEpisode)

	episodes := authed.Group("/episodes", admin.RequirePage("dramas"))
	episodes.PATCH("/:id", h.UpdateEpisode)
	episodes.DELETE("/:id", h.DeleteEpisode)

	tags := authed.Group("/tags", admin.RequirePage("tags"))
	tags.GET("", h.ListTags)
	tags.POST("", h.CreateTag)
	tags.PATCH("/:id", h.UpdateTag)
	tags.POST("/:id/merge", h.MergeTag)

	tax := authed.Group("/taxonomies", admin.RequirePage("taxonomies"))
	tax.GET("", h.ListTaxonomies)
	tax.POST("", h.CreateTaxonomy)
	tax.POST("/reorder", h.ReorderTaxonomies)
	tax.PATCH("/:id", h.UpdateTaxonomy)
	tax.DELETE("/:id", h.DeleteTaxonomy)
	tax.POST("/:id/terms", h.CreateTerm)
	tax.POST("/:id/terms/reorder", h.ReorderTerms)
	tax.PATCH("/:id/terms/:term_id", h.UpdateTerm)
	tax.DELETE("/:id/terms/:term_id", h.DeleteTerm)

	topics := authed.Group("/topics", admin.RequirePage("topics"))
	topics.GET("", h.ListTopics)
	topics.POST("", h.CreateTopic)
	topics.GET("/:id", h.GetTopic)
	topics.PATCH("/:id", h.UpdateTopic)
	topics.DELETE("/:id", h.DeleteTopic)
	topics.PUT("/:id/dramas", h.SetTopicDramas)

	// 资源体检（S4-5，docs/06 第 9 节）。发起与清空都算修改（要 resource_checks.edit）：
	// 发起会对源站产生批量请求，清空会丢掉上一次的结论。
	checks := authed.Group("/resource-checks", admin.RequirePage("resource_checks"))
	checks.GET("", h.ListResourceChecks)
	checks.POST("/run", h.RunResourceCheck)
	checks.POST("/clear", h.ClearResourceChecks)

	// 资源预热（S4-5，docs/06 第 10 节）。开始与停止算修改（要 resource_warm.edit）：
	// 预热会对源站与边缘产生持续流量。进度与历史是只读。
	warm := authed.Group("/resource-warm", admin.RequirePage("resource_warm"))
	warm.GET("/progress", h.ResourceWarmProgress)
	warm.GET("/history", h.ListResourceWarmHistory)
	warm.POST("/start", h.StartResourceWarm)
	warm.POST("/stop", h.StopResourceWarm)

	// 批量导入（S4-6，docs/06 第 8 节）。
	// 预览只要 import.view —— 它不写库也不记记录，是个纯粹的「让我先看看会发生什么」。
	imports := authed.Group("/import", admin.RequirePage("import"))
	imports.GET("/logs", h.ListImportLogs)
	imports.POST("/preview", h.PreviewImport)
	imports.POST("/run", h.RunImport)
}
