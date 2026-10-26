// Package content 是前台内容读接口的 handler（S1-4，docs/07 第 1 节「站点与内容读取」）。
// 路由注册在 /api/v1 分组上，因此自动走报文加密中间件；handler 只读写明文。
package content

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// Service 是 handler 依赖的最小接口，便于测试替换。
type Service interface {
	Drama(ctx context.Context, id int64) (dom.Doc[dom.DramaDetailView], error)
	Channel(ctx context.Context, slug string, in dom.ChannelRequest) (dom.Doc[dom.ChannelPage], error)
	Tag(ctx context.Context, slug string, in dom.ListRequest) (dom.Doc[dom.TagPage], error)
	Explore(ctx context.Context, in dom.ExploreRequest) (dom.Doc[dom.ExplorePage], error)
	Topics(ctx context.Context, in dom.ListRequest) (dom.Doc[dom.TopicListPage], error)
	Topic(ctx context.Context, slug string) (dom.Doc[dom.TopicDetailView], error)
}

// SearchService 是前台搜索与联想（domain/content.SearchService）。
type SearchService interface {
	Search(ctx context.Context, in dom.SearchRequest) (dom.Doc[dom.SearchPage], error)
	Suggest(ctx context.Context, q string, limit int) (dom.Doc[dom.SuggestPage], error)
}

// Limiter 是按 key 的固定窗口限流器（cache.Counter）。
type Limiter interface {
	Add(ctx context.Context, key string, delta int64, window time.Duration) (int64, error)
}

// Handlers 聚合前台内容读接口。
type Handlers struct {
	Svc Service
	// Search 为空时不注册搜索路由（S3-4）。
	Search SearchService
	// SuggestLimiter 给联想接口单独限流（docs/03 第 3.3 节）；为空时不限流。
	SuggestLimiter Limiter
}

// 共享缓存里的保留时长（秒），与 domain/content 的 TTL 一致（docs/03 第 3.2 节）。
const (
	dramaMaxAge   = 120
	channelMaxAge = 60
	tagMaxAge     = 60
	exploreMaxAge = 30
	topicMaxAge   = 300
	searchMaxAge  = 60
	suggestMaxAge = 300
)

const (
	headerDeviceID     = "X-Device-Id"
	suggestPerMinute   = 60              // 联想按设备每分钟 60 次：输入时每 300ms 防抖一次也够用
	suggestLimitWindow = 2 * time.Minute // 分钟桶 key 的存活时间（略长于 1 分钟）
)

// Routes 注册前台内容读接口。
func (h *Handlers) Routes(v1 gin.IRouter) {
	v1.GET("/dramas/:id", h.GetDrama)
	v1.GET("/channels/:slug", h.GetChannel)
	v1.GET("/tags/:slug", h.GetTag)
	v1.GET("/explore", h.Explore)
	v1.GET("/topics", h.ListTopics)
	v1.GET("/topics/:slug", h.GetTopic)
	if h.Search != nil {
		v1.GET("/search", h.SearchDramas)
		v1.GET("/search/suggest", h.Suggest)
	}
}

type listQuery struct {
	Sort   string `form:"sort"`
	Cursor string `form:"cursor"`
	Limit  int    `form:"limit"`
}

type channelQuery struct {
	Sort     string `form:"sort"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

type exploreQuery struct {
	Terms    string `form:"terms"`
	Tag      string `form:"tag"`
	Finished *bool  `form:"finished"`
	Window   string `form:"window"`
	Sort     string `form:"sort"`
	Cursor   string `form:"cursor"`
	Limit    int    `form:"limit"`
}

// GetDrama 是 GET /api/v1/dramas/:id。
func (h *Handlers) GetDrama(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	doc, err := h.Svc.Drama(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeDoc(c, doc, dramaMaxAge)
}

// GetChannel 是 GET /api/v1/channels/:slug。
func (h *Handlers) GetChannel(c *gin.Context) {
	var q channelQuery
	if !bindQuery(c, &q) {
		return
	}
	doc, err := h.Svc.Channel(c.Request.Context(), c.Param("slug"), dom.ChannelRequest{
		Sort: dom.DramaSort(q.Sort), Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeDoc(c, doc, channelMaxAge)
}

// GetTag 是 GET /api/v1/tags/:slug。
func (h *Handlers) GetTag(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	doc, err := h.Svc.Tag(c.Request.Context(), c.Param("slug"), q.request())
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeDoc(c, doc, tagMaxAge)
}

// Explore 是 GET /api/v1/explore。
func (h *Handlers) Explore(c *gin.Context) {
	var q exploreQuery
	if !bindQuery(c, &q) {
		return
	}
	doc, err := h.Svc.Explore(c.Request.Context(), dom.ExploreRequest{
		Terms: q.Terms, Tag: q.Tag, Finished: q.Finished, Window: q.Window,
		Sort: dom.DramaSort(q.Sort), Cursor: q.Cursor, Limit: q.Limit,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeDoc(c, doc, exploreMaxAge)
}

// ListTopics 是 GET /api/v1/topics。
func (h *Handlers) ListTopics(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	doc, err := h.Svc.Topics(c.Request.Context(), q.request())
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeDoc(c, doc, topicMaxAge)
}

// GetTopic 是 GET /api/v1/topics/:slug。
func (h *Handlers) GetTopic(c *gin.Context) {
	doc, err := h.Svc.Topic(c.Request.Context(), c.Param("slug"))
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeDoc(c, doc, topicMaxAge)
}

type searchQuery struct {
	Q      string `form:"q"`
	Sort   string `form:"sort"`
	Cursor string `form:"cursor"`
	Limit  int    `form:"limit"`
}

type suggestQuery struct {
	Q     string `form:"q"`
	Limit int    `form:"limit"`
}

// SearchDramas 是 GET /api/v1/search：搜索结果（只有剧集）。每次搜索计入热搜（领域层处理）。
func (h *Handlers) SearchDramas(c *gin.Context) {
	var q searchQuery
	if !bindQuery(c, &q) {
		return
	}
	doc, err := h.Search.Search(c.Request.Context(), dom.SearchRequest{
		Q: q.Q, Sort: dom.DramaSort(q.Sort), Cursor: q.Cursor, Limit: q.Limit,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeDoc(c, doc, searchMaxAge)
}

// Suggest 是 GET /api/v1/search/suggest：剧名联想。features.search_suggest 关闭时 404；按设备限流。
func (h *Handlers) Suggest(c *gin.Context) {
	var q suggestQuery
	if !bindQuery(c, &q) {
		return
	}
	if wait := h.overSuggestLimit(c); wait > 0 {
		respond.Error(c, errs.WithRetryAfter(errs.ErrRateLimited, wait))
		return
	}
	doc, err := h.Search.Suggest(c.Request.Context(), q.Q, q.Limit)
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeDoc(c, doc, suggestMaxAge)
}

// overSuggestLimit 按自然分钟的固定窗口计数，超出配额时返回到下一分钟的等待时长。
// 没有设备标识时按客户端 IP 计；计数器故障时放行（宁可放宽也不拒绝整个接口）。
func (h *Handlers) overSuggestLimit(c *gin.Context) time.Duration {
	if h.SuggestLimiter == nil {
		return 0
	}
	subject := strings.TrimSpace(c.GetHeader(headerDeviceID))
	if subject == "" {
		subject = "ip:" + c.ClientIP()
	}
	now := time.Now()
	key := subject + ":" + strconv.FormatInt(now.Unix()/60, 10)
	n, err := h.SuggestLimiter.Add(c.Request.Context(), key, 1, suggestLimitWindow)
	if err != nil || n <= suggestPerMinute {
		return 0
	}
	return time.Duration(60-now.Unix()%60) * time.Second
}

func (q listQuery) request() dom.ListRequest {
	return dom.ListRequest{Sort: dom.DramaSort(q.Sort), Cursor: q.Cursor, Limit: q.Limit}
}

// writeDoc 输出带 ETag 与缓存头的文档。加密响应会被中间件改为 no-store（每个响应密钥不同），
// s-maxage 只对 BFF 明文直连生效；ETag 两种情况都有效。
func writeDoc[T any](c *gin.Context, doc dom.Doc[T], maxAge int) {
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=0, s-maxage=%d, stale-while-revalidate=300", maxAge))
	if respond.NotModified(c, doc.ETag) {
		return
	}
	respond.OK(c, doc.Payload)
}

// bindQuery 解析查询参数；类型不对时已写出 400。
func bindQuery(c *gin.Context, dst any) bool {
	if err := c.ShouldBindQuery(dst); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return false
	}
	return true
}

// pathID 解析路径上的正整数编号；不是正整数时按「内容不存在」处理。
func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(c, errs.ErrNotFound)
		return 0, false
	}
	return id, true
}
