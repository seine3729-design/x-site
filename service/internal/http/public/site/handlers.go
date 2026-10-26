// Package site 是前台的站点编排接口：静态页，以及后续的首页 / 频道页区块（docs/specs/A3-1.md）。
// 路由注册在 /api/v1 分组上，因此自动走报文加密中间件；handler 只读写明文。
package site

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/site"
	"xsite/service/internal/http/respond"
)

// PageService 是静态页下发（domain/site.PageService）。
type PageService interface {
	Public(ctx context.Context, slug string) (dom.PublicPage, error)
}

// HomeService 是页面区块下发（domain/site.BlockService）。
type HomeService interface {
	Home(ctx context.Context, page string) (dom.PublicHome, error)
}

// Handlers 聚合前台站点编排接口。
type Handlers struct {
	Pages PageService
	Home  HomeService
}

// 静态页改动不频繁，共享缓存可以放久一些；区块里有剧集数据，放短一些。
// 后台改动后各自的读穿缓存会立即清除。
const (
	pageMaxAge = 300
	homeMaxAge = 60
)

// Routes 注册前台站点编排接口。
func (h *Handlers) Routes(v1 gin.IRouter) {
	v1.GET("/pages/:slug", h.Page)
	v1.GET("/home", h.HomeBlocks)
}

type homeQuery struct {
	Page string `form:"page"`
}

// HomeBlocks 是 GET /api/v1/home?page=home|channel:{slug}：该页面的区块编排，一次返回全部启用区块及其数据。
func (h *Handlers) HomeBlocks(c *gin.Context) {
	var q homeQuery
	_ = c.ShouldBindQuery(&q) // 只有字符串参数，不会失败；页面名由领域层校验
	out, err := h.Home.Home(c.Request.Context(), q.Page)
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeCached(c, out, "home", homeMaxAge)
}

// Page 是 GET /api/v1/pages/:slug：静态页正文（草稿与不存在的 slug 都是 404）。
func (h *Handlers) Page(c *gin.Context) {
	p, err := h.Pages.Public(c.Request.Context(), c.Param("slug"))
	if err != nil {
		respond.Error(c, err)
		return
	}
	writeCached(c, p, "page", pageMaxAge)
}

// writeCached 输出带 ETag 与缓存头的响应（与 public/ops 的同名函数一致）。加密响应会被中间件改为 no-store
// （每个响应密钥不同），s-maxage 只对 BFF 明文直连生效；ETag 两种情况都有效。
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
