// Package respond 统一 HTTP 响应包：{ code, message, data }（docs/07 第 0.1 节）。
package respond

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
)

// Envelope 是所有 JSON 响应的外层结构。成功时 code 为 "OK"。
type Envelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// OK 返回 200 与数据。
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{Code: "OK", Message: "ok", Data: data})
}

// Error 把错误映射为业务码。非业务错误记录日志后统一返回 INTERNAL，不把内部细节返回客户端。
// 错误带等待时长（errs.WithRetryAfter）时输出 Retry-After 头（秒，向上取整）。
func Error(c *gin.Context, err error) {
	e := errs.From(err)
	if e == errs.ErrInternal {
		slog.ErrorContext(c.Request.Context(), "请求处理失败",
			"err", err, "route", c.FullPath(), "request_id", middleware.GetRequestID(c))
	}
	if after := errs.RetryAfter(err); after > 0 {
		c.Header("Retry-After", strconv.FormatInt(int64((after+time.Second-1)/time.Second), 10))
	}
	c.AbortWithStatusJSON(e.Status, Envelope{
		Code:      e.Code,
		Message:   e.Message,
		RequestID: middleware.GetRequestID(c),
	})
}

// NotModified 处理 If-None-Match：命中时返回 304 并返回 true。调用方随后应直接 return。
func NotModified(c *gin.Context, etag string) bool {
	c.Header("ETag", etag)
	if match := c.GetHeader("If-None-Match"); match != "" && match == etag {
		c.Status(http.StatusNotModified)
		c.Abort()
		return true
	}
	return false
}
