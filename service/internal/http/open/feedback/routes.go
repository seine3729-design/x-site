// Package feedback 是反馈社区的开放提交接口（/open-api/feedback，feedback/docs/07 第 0.5 节）。
//
// 这组接口供站外网页直接调用，所以与 /api/v1 有三点不同：
//  1. 不走报文加密：调用方是第三方站点，不可能持有前台的 HPKE 客户端（docs/10 第 5.3 节）。
//  2. 免回源鉴权：请求不经过 CDN 回源也没有 BFF 服务令牌，豁免登记在 http.NewRouter 的 Exempt 表里。
//  3. 身份只认 X-Api-Key，不读 Cookie；因此跨域回显任意 Origin 也不会产生 CSRF 风险，
//     真正的来源限制由 Key 上的 Origin 白名单在领域层判定。
package feedback

import (
	"context"
	nethttp "net/http"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/feedback"
)

// HeaderAPIKey 是开放提交接口的密钥头。
const HeaderAPIKey = "X-Api-Key"

// Service 是开放提交服务（domain/feedback.IntakeService）。
type Service interface {
	Submit(ctx context.Context, rawKey string, in dom.IntakeInput) (dom.IntakeResult, error)
}

// Limiter 是按 key 的固定窗口计数器（cache.Counter）。
type Limiter interface {
	Incr(ctx context.Context, key string, window time.Duration) (int64, error)
}

// Handlers 聚合开放提交接口的 handler。Limiter 为 nil 时跳过按 IP 的粗粒度限流。
type Handlers struct {
	Svc     Service
	Limiter Limiter
}

// Routes 注册到 /open-api 分组上（该分组不挂报文加密，也不走后台会话中间件）。
func (h *Handlers) Routes(g gin.IRouter) {
	fb := g.Group("/feedback", cors())
	// 预检不带 X-Api-Key，直接放过；没有显式注册 OPTIONS 时 Gin 会按 405 处理
	fb.OPTIONS("", func(c *gin.Context) { c.Status(nethttp.StatusNoContent) })
	fb.POST("", h.Submit)
}

// cors 允许任意站点以「不带凭据」的方式跨域提交。
// 这里刻意不输出 Access-Control-Allow-Credentials：接口不读 Cookie，浏览器也就不会带上任何会话，
// 于是回显 Origin 只是让第三方页面能读到响应，不构成代用户发起请求的通道。
func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		if origin := c.GetHeader("Origin"); origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			// 用 Add 而不是 Set：避免覆盖其他中间件已经写入的 Vary
			c.Writer.Header().Add("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, "+HeaderAPIKey)
		c.Header("Access-Control-Max-Age", "600")
		c.Next()
	}
}
