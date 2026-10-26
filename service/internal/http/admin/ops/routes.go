// Package ops 是运营后台「运营」目录的接口：公告、广告、热搜词、排行干预（docs/07 第 2 节，docs/specs/A3-4.md）。
// 会话、CSRF、审计中间件由 admin.Routes 返回的分组提供，这里按页面声明权限点。
package ops

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/ops"
	"xsite/service/internal/http/admin"
)

// AnnouncementService 是公告管理（domain/ops.AnnouncementService）。
type AnnouncementService interface {
	List(ctx context.Context, f dom.AnnouncementFilter) (dom.Page[dom.Announcement], error)
	Create(ctx context.Context, in dom.AnnouncementInput) (dom.Announcement, error)
	Update(ctx context.Context, id int64, in dom.AnnouncementInput) (before, after dom.Announcement, err error)
	Delete(ctx context.Context, id int64) (dom.Announcement, error)
}

// AdService 是广告管理（domain/ops.AdService）。customHTML 表示当前管理员有 ads.custom_html 权限。
type AdService interface {
	List(ctx context.Context, f dom.AdFilter) (dom.Page[dom.Ad], error)
	Create(ctx context.Context, in dom.AdInput, customHTML bool) (dom.Ad, error)
	Update(ctx context.Context, id int64, in dom.AdInput, customHTML bool) (before, after dom.Ad, err error)
	Delete(ctx context.Context, id int64) (dom.Ad, error)
}

// HotsearchService 是热搜词管理（domain/ops.HotsearchService）。
type HotsearchService interface {
	List(ctx context.Context, f dom.HotFilter) (dom.Page[dom.HotWord], error)
	Create(ctx context.Context, word string) (dom.HotWord, error)
	Update(ctx context.Context, id int64, p dom.HotPatch) (before, after dom.HotWord, err error)
	Reorder(ctx context.Context, ids []int64) error
	Delete(ctx context.Context, id int64) (dom.HotWord, error)
}

// RankService 是排行干预（domain/ops.RankService）。
type RankService interface {
	AdminBoard(ctx context.Context, b dom.RankBoard) (dom.RankAdminView, error)
	SetOverrides(ctx context.Context, b dom.RankBoard, list []dom.RankOverride) (before, after []dom.RankOverrideView, err error)
	RecomputeNow(ctx context.Context) (done bool, err error)
	Weights(ctx context.Context) (dom.RankWeights, error)
	SetWeights(ctx context.Context, w dom.RankWeights) (before, after dom.RankWeights, err error)
}

// Handlers 聚合运营目录的 handler。
type Handlers struct {
	Announcements AnnouncementService
	Ads           AdService
	Hotsearch     HotsearchService
	Ranks         RankService
	// Now 用于计算公告、广告的显示状态；为空时取当前时间。
	Now func() time.Time
}

func (h *Handlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Routes 把运营接口注册到 admin.Routes 返回的分组上。读接口要 <页面>.view，写接口要 <页面>.edit。
func (h *Handlers) Routes(authed gin.IRouter) {
	ann := authed.Group("/announcements", admin.RequirePage("announcements"))
	ann.GET("", h.ListAnnouncements)
	ann.POST("", h.CreateAnnouncement)
	ann.PATCH("/:id", h.UpdateAnnouncement)
	ann.DELETE("/:id", h.DeleteAnnouncement)

	// HTML 素材另要 ads.custom_html：是否需要取决于请求体与原记录，由 handler 交给领域层判断
	admin.DeclarePermissions(permCustomHTML)
	ads := authed.Group("/ads", admin.RequirePage("ads"))
	ads.GET("", h.ListAds)
	ads.POST("", h.CreateAd)
	ads.PATCH("/:id", h.UpdateAd)
	ads.DELETE("/:id", h.DeleteAd)

	hot := authed.Group("/hotsearch", admin.RequirePage("hotsearch"))
	hot.GET("", h.ListHotWords)
	hot.POST("", h.CreateHotWord)
	hot.POST("/reorder", h.ReorderHotWords)
	hot.PATCH("/:id", h.UpdateHotWord)
	hot.DELETE("/:id", h.DeleteHotWord)

	ranks := authed.Group("/ranks", admin.RequirePage("ranks"))
	ranks.POST("/recompute", h.RecomputeRanks)
	ranks.GET("/:board", h.GetRankBoard)
	ranks.PUT("/:board", h.SetRankOverrides)
	// 计分系数属于排行干预页面，与榜单用同一组权限点
	rankCfg := authed.Group("/ranking-config", admin.RequirePage("ranks"))
	rankCfg.GET("", h.GetRankingConfig)
	rankCfg.PUT("", h.SetRankingConfig)
}
