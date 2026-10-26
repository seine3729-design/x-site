// Package ops 是前台运营类接口的 handler（docs/07 第 1 节，docs/specs/A3-4.md）：公告、广告与事件上报、热搜榜、榜单。
// 路由注册在 /api/v1 分组上，因此自动走报文加密中间件；handler 只读写明文。
package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/ops"
	"xsite/service/internal/http/respond"
)

// AnnouncementService 是公告下发（domain/ops.AnnouncementService）。
type AnnouncementService interface {
	Live(ctx context.Context, locale string) ([]dom.PublicAnnouncement, error)
}

// AdService 是广告投放与事件计数（domain/ops.AdService）。
type AdService interface {
	Serve(ctx context.Context, req dom.ServeRequest) ([]dom.PublicAd, error)
	Record(ctx context.Context, ev dom.AdEvent) error
}

// HotBoardService 是热搜榜（domain/ops.HotsearchService）。
type HotBoardService interface {
	Board(ctx context.Context) ([]dom.PublicHotWord, error)
}

// RankBoardService 是前台榜单（domain/ops.RankService）。
type RankBoardService interface {
	PublicBoard(ctx context.Context, b dom.RankBoard) (dom.PublicRankBoard, error)
}

// Handlers 聚合前台运营类接口。
type Handlers struct {
	Announcements AnnouncementService
	Ads           AdService
	Hotsearch     HotBoardService
	Ranks         RankBoardService
}

// 共享缓存里的保留时长（秒）：公告的后台改动要尽快生效，所以比内容页短；热搜榜每小时、榜单每 5 分钟才更新一次。
const (
	announcementMaxAge = 30
	hotBoardMaxAge     = 60
	rankBoardMaxAge    = 60
)

// Routes 注册前台运营类接口。
func (h *Handlers) Routes(v1 gin.IRouter) {
	v1.GET("/announcements", h.ListAnnouncements)
	v1.GET("/ads", h.ListAds)
	v1.POST("/ads/event", h.AdEvent)
	v1.GET("/search/hot", h.HotBoard)
	v1.GET("/rank", h.RankBoard)
}

type rankQuery struct {
	Board string `form:"board"`
}

// RankBoard 是 GET /api/v1/rank?board=drama_week|drama_month：榜单前 20 名（features.ranks 关闭时 404）。
func (h *Handlers) RankBoard(c *gin.Context) {
	var q rankQuery
	_ = c.ShouldBindQuery(&q) // 只有字符串参数，不会失败；榜单名由领域层校验
	b, err := h.Ranks.PublicBoard(c.Request.Context(), dom.RankBoard(q.Board))
	if err != nil {
		respond.Error(c, err)
		return
	}
	if b.List == nil {
		b.List = []dom.PublicRankItem{}
	}
	writeCached(c, b, "rank", rankBoardMaxAge)
}

// HotBoard 是 GET /api/v1/search/hot：热搜榜（置顶的按顺序在前，其余显示中的词按搜索次数倒序，共 10 个）。
func (h *Handlers) HotBoard(c *gin.Context) {
	list, err := h.Hotsearch.Board(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	if list == nil {
		list = []dom.PublicHotWord{}
	}
	writeCached(c, gin.H{"list": list}, "hot", hotBoardMaxAge)
}

type localeQuery struct {
	Locale string `form:"locale"`
}

// ListAnnouncements 是 GET /api/v1/announcements?locale=：当前显示中的公告（新的在前）。
func (h *Handlers) ListAnnouncements(c *gin.Context) {
	var q localeQuery
	_ = c.ShouldBindQuery(&q) // 只有字符串参数，不会失败；不认识的语言按简体中文
	list, err := h.Announcements.Live(c.Request.Context(), q.Locale)
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeCached(c, gin.H{"list": list}, "ann", announcementMaxAge)
}

// writeCached 输出带 ETag 与缓存头的响应。加密响应会被中间件改为 no-store（每个响应密钥不同），
// s-maxage 只对 BFF 明文直连生效；ETag 两种情况都有效。
func writeCached(c *gin.Context, payload any, kind string, maxAge int) {
	b, err := json.Marshal(payload)
	if err != nil {
		respond.Error(c, err)
		return
	}
	sum := sha256.Sum256(b)
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=0, s-maxage=%d, stale-while-revalidate=60", maxAge))
	if respond.NotModified(c, fmt.Sprintf(`W/"%s-%s"`, kind, hex.EncodeToString(sum[:8]))) {
		return
	}
	respond.OK(c, json.RawMessage(b))
}
