package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/domain/identity"
	"xsite/service/internal/errs"
)

// 身份中间件（S2-1）：解析 Authorization: Bearer <access>，把注册用户 / 游客身份放进 context，
// 用户端接口的鉴权由它统一完成。Access Token 无状态，校验只验签不查库（见 identity.ResolveAccess）。

const ctxAccess = "xsite_access"

// AccessResolver 校验 Access Token（identity.Service 实现）。
type AccessResolver interface {
	ResolveAccess(token string) (identity.Access, error)
}

// SetIdentity 解析 Bearer 令牌：无 Authorization 头作为匿名放行；带令牌但无效 / 过期时中止并返回对应错误
// （401 TOKEN_EXPIRED / NEED_LOGIN），客户端据此刷新或重新登录。
func SetIdentity(resolver AccessResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		tok := BearerToken(c)
		if tok == "" {
			c.Next()
			return
		}
		access, err := resolver.ResolveAccess(tok)
		if err != nil {
			abortError(c, err)
			return
		}
		c.Set(ctxAccess, access)
		c.Next()
	}
}

// RequireUser 要求已登录的注册用户（role=user）；否则 401 NEED_LOGIN。放在 SetIdentity 之后。
func RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		a, ok := CurrentAccess(c)
		if !ok || !a.IsUser() {
			abortError(c, errs.ErrNeedLogin)
			return
		}
		c.Next()
	}
}

// CurrentAccess 返回当前请求解析出的身份（SetIdentity 已解析）；未登录时 ok 为 false。
func CurrentAccess(c *gin.Context) (identity.Access, bool) {
	v, ok := c.Get(ctxAccess)
	if !ok {
		return identity.Access{}, false
	}
	a, ok := v.(identity.Access)
	return a, ok
}

// BearerToken 取出 Authorization: Bearer <token> 里的令牌；没有则返回空串。
func BearerToken(c *gin.Context) string {
	const prefix = "bearer "
	h := c.GetHeader("Authorization")
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// abortError 以统一响应包返回业务错误并中止。中间件不引用 respond 包（respond 依赖 middleware，会形成循环）。
func abortError(c *gin.Context, err error) {
	e := errs.From(err)
	c.AbortWithStatusJSON(e.Status, gin.H{
		"code": e.Code, "message": e.Message, "request_id": GetRequestID(c),
	})
}
