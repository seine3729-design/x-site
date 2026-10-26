// Package middleware 包含 HTTP 中间件，执行顺序见 docs/03 第 1 节。
package middleware

import (
	"github.com/gin-gonic/gin"
	"uuid"
)

const (
	HeaderRequestID = "X-Request-Id"
	ctxRequestID    = "request_id"
	maxRequestIDLen = 64
)

// RequestID 采用上游传入的合法 request id，否则生成 UUIDv7；写入上下文与响应头。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if !validRequestID(id) {
			id = uuid.NewV7().String()
		}
		c.Set(ctxRequestID, id)
		c.Header(HeaderRequestID, id)
		c.Next()
	}
}

// GetRequestID 读取当前请求的 request id。
func GetRequestID(c *gin.Context) string {
	return c.GetString(ctxRequestID)
}

// validRequestID 只接受短的 [A-Za-z0-9-]，防止日志注入与超长头。
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		ch := id[i]
		ok := ch == '-' || (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}
