package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Recover 捕获 panic，记录堆栈后返回 500（不把细节返回客户端）。
func Recover() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		slog.ErrorContext(c.Request.Context(), "panic", "recovered", recovered,
			"route", c.FullPath(), "request_id", GetRequestID(c))
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
			"code": "INTERNAL", "message": "服务开小差了，请稍后再试", "request_id": GetRequestID(c),
		})
	})
}

// AccessLog 记录访问日志。按路由模板记录，不记录查询串与请求头，避免泄露令牌。
func AccessLog(skip map[string]bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		if skip[c.Request.URL.Path] {
			return
		}
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		slog.InfoContext(c.Request.Context(), "http",
			"method", c.Request.Method,
			"route", route,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"request_id", GetRequestID(c),
		)
	}
}

// SecureHeaders 设置通用安全响应头；API 默认不缓存，需要缓存的 handler 自行覆盖 Cache-Control。
func SecureHeaders(hsts bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		if hsts {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		c.Next()
	}
}

// BodyLimit 限制请求体大小。后台媒体上传的原始文件流使用独立上限：
// 不能把默认 1 MiB 限制放宽到所有 API，也必须在认证前拦住无界上传。
const adminUploadMaxBytes int64 = 4 << 30

// adminImageOptimizeMaxBytes 是后台图片优化接口接收的原图上限，与 upload.OptimizeMaxBytes 保持一致。
// 这里不 import 那个包：middleware 是底层包，反向依赖 handler 会形成循环。
const adminImageOptimizeMaxBytes int64 = 25 << 20

// adminUploadPartMaxBytes 是单个视频分片请求体的上限。只是外层闸门：handler 还会按会话推算出的
// 片长逐字节核对（upload.VideoPartSize = 5 MiB），这里留余量是为了以后调大分片时不必同步改这一处。
const adminUploadPartMaxBytes int64 = 64 << 20

// 长请求的连接读写期限。
const (
	// uploadDeadline 覆盖「浏览器把文件传完 + 服务端写入对象存储 + 返回响应」整个过程。
	uploadDeadline = 2 * time.Hour
	// uploadPartDeadline 覆盖一个视频分片的同样过程。5 MiB 的分片按 10 KB/s 的极差网络也只要 9 分钟，
	// 不需要整段上传那样的小时级期限；期限越短，被慢速连接占住的缓冲越早释放。
	uploadPartDeadline = 15 * time.Minute
	// streamDeadline 略大于 SSE 自己的单连接存活上限（10 分钟），让服务端主动收尾而不是被超时掐断。
	streamDeadline = 15 * time.Minute
	// externalDeadline 给「要等外部服务往返」的后台接口：图片优化要调两次外部 API，
	// 默认的 30 秒写超时会在响应写出前掐断连接。按它自己的 90 秒调用超时留足余量即可，不必放到小时级。
	externalDeadline = 3 * time.Minute
)

// LongRequest 放宽长请求（后台大文件上传、SSE 进度流）的连接读写期限。
//
// http.Server 的 ReadTimeout / WriteTimeout 覆盖的是整个请求：几百 MB 的视频在默认的十几秒里
// 根本读不完，连接会被服务端直接掐断，经 nginx 表现为 502 Bad Gateway；SSE 同理会在写超时到点时断流。
// 这里只按路由放宽这两类请求，其余接口保留默认的严格超时（防慢速读写攻击）。
func LongRequest() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch {
		case isAdminUploadContent(c.Request), isAdminUploadComplete(c.Request):
			extendDeadline(c, uploadDeadline)
		case isAdminUploadPart(c.Request):
			extendDeadline(c, uploadPartDeadline)
		case isAdminImageOptimize(c.Request):
			extendDeadline(c, externalDeadline)
		case isEventStream(c.Request):
			extendDeadline(c, streamDeadline)
		}
		c.Next()
	}
}

// extendDeadline 放宽当前连接的读写期限；底层 ResponseWriter 不支持时（如测试里的 recorder）忽略。
func extendDeadline(c *gin.Context, d time.Duration) {
	rc := http.NewResponseController(c.Writer)
	at := time.Now().Add(d)
	if err := rc.SetReadDeadline(at); err != nil && !errors.Is(err, http.ErrNotSupported) {
		slog.WarnContext(c.Request.Context(), "放宽读期限失败", "route", c.FullPath(), "err", err)
	}
	if err := rc.SetWriteDeadline(at); err != nil && !errors.Is(err, http.ErrNotSupported) {
		slog.WarnContext(c.Request.Context(), "放宽写期限失败", "route", c.FullPath(), "err", err)
	}
}

// isEventStream 识别 SSE 请求（后台进度流用 ?stream=1 开启，docs/06 第 10 节）。
func isEventStream(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Query().Get("stream") == "1"
}

func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			limit := maxBytes
			switch {
			case isAdminUploadContent(c.Request):
				limit = adminUploadMaxBytes
			case isAdminUploadPart(c.Request):
				limit = adminUploadPartMaxBytes
			case isAdminImageOptimize(c.Request):
				limit = adminImageOptimizeMaxBytes
			}
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}

func isAdminUploadContent(r *http.Request) bool {
	return r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/admin/uploads/") && strings.HasSuffix(r.URL.Path, "/content")
}

// isAdminUploadPart 识别视频分片请求：PUT /api/admin/uploads/:id/parts/:number。
func isAdminUploadPart(r *http.Request) bool {
	if r.Method != http.MethodPut {
		return false
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/api/admin/uploads/")
	if !ok {
		return false
	}
	id, number, ok := strings.Cut(rest, "/parts/")
	return ok && id != "" && !strings.Contains(id, "/") && number != "" && !strings.Contains(number, "/")
}

// isAdminUploadComplete 识别上传完成请求。它的请求体只有几十字节，但处理时间可能与文件大小成正比
// （暂存对象缺失时要从主存储把源视频复制一遍），默认的 30 秒写超时会在响应写出前掐断连接。
func isAdminUploadComplete(r *http.Request) bool {
	return r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/api/admin/uploads/") && strings.HasSuffix(r.URL.Path, "/complete")
}

// isAdminImageOptimize 识别后台图片优化请求：请求体是没压过的原图，处理要等两次外部服务往返。
func isAdminImageOptimize(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/admin/images/optimize"
}
