package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	HeaderEdgeSecret   = "X-Edge-Secret"
	HeaderServiceToken = "X-Service-Token"
	ctxFromBFF         = "from_bff"
)

// EdgeAuthOptions 配置回源鉴权。
type EdgeAuthOptions struct {
	Enforced     bool
	EdgeSecret   string
	ServiceToken string
	// Exempt 中的路径不校验（容器健康检查）。
	Exempt map[string]bool
}

// EdgeAuth 校验请求来自 CDN 回源（x-edge-secret）或 BFF（x-service-token），防止绕过 CDN 直连源站。
// 未强制时仍会识别 BFF 身份，供后续中间件决定是否采信转发的客户端信息。
func EdgeAuth(opt EdgeAuthOptions) gin.HandlerFunc {
	return func(c *gin.Context) {
		fromBFF := secretEqual(c.GetHeader(HeaderServiceToken), opt.ServiceToken)
		fromEdge := secretEqual(c.GetHeader(HeaderEdgeSecret), opt.EdgeSecret)
		c.Set(ctxFromBFF, fromBFF)

		if opt.Enforced && !fromBFF && !fromEdge && !opt.Exempt[c.Request.URL.Path] {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}

// IsFromBFF 表示请求携带了有效的 BFF 服务令牌。
func IsFromBFF(c *gin.Context) bool { return c.GetBool(ctxFromBFF) }

func secretEqual(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
