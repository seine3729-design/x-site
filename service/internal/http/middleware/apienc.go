package middleware

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/crypto/apienc"
	"xsite/service/internal/errs"
)

const (
	HeaderEnc      = "X-Enc"
	HeaderEncType  = "X-Enc-Type"
	HeaderBFFProxy = "X-Bff-Proxy"
	ctxEncrypted   = "api_encrypted"
	// 客户端与服务端时间允许的最大偏差。
	maxEncSkew = 5 * time.Minute
)

// APIEncryptionOptions 配置用户端接口报文加密。
type APIEncryptionOptions struct {
	Keyring *apienc.Keyring
	// Required 为 true 时，浏览器与 APP 的请求必须加密；SSR 阶段 BFF 直接调用（带服务令牌、
	// 且不是代为转发的浏览器请求）走内网 mTLS，允许明文。
	Required bool
	// Exempt 中的路径始终允许明文（公钥下发、存活检查）。
	Exempt map[string]bool
	Now    func() time.Time
}

// APIEncryption 解密请求体、加密响应体（协议见 internal/crypto/apienc）。
//
// 解密之前的失败（缺少或损坏的 X-Enc、未知 kid、时间偏差）返回明文错误；
// 一旦解密成功，handler 的所有响应（包括业务错误）都加密返回，且不允许任何共享缓存。
func APIEncryption(opt APIEncryptionOptions) gin.HandlerFunc {
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	return func(c *gin.Context) {
		value := c.GetHeader(HeaderEnc)
		if value == "" {
			if opt.Required && !opt.Exempt[c.Request.URL.Path] && !isInternalSSR(c) {
				abortPlain(c, errs.ErrEncRequired)
				return
			}
			c.Next()
			return
		}

		h, err := apienc.ParseHeader(value)
		if err != nil {
			abortPlain(c, errs.ErrEncInvalid)
			return
		}
		if skew := now().Sub(time.Unix(h.TS, 0)); skew > maxEncSkew || skew < -maxEncSkew {
			abortPlain(c, errs.ErrEncTimeSkew)
			return
		}

		var body []byte
		if c.Request.Body != nil {
			body, err = io.ReadAll(c.Request.Body)
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					abortPlain(c, errs.ErrBadRequest.WithMessage("请求体过大"))
					return
				}
				abortPlain(c, errs.ErrEncInvalid)
				return
			}
		}
		// RequestURI 是客户端发出的原始请求目标（路径 + 查询串），与客户端计算 AAD 用的字符串一致
		aad := apienc.RequestAAD(c.Request.Method, c.Request.RequestURI, h.TS)
		plaintext, session, err := opt.Keyring.Open(h, aad, body)
		switch {
		case errors.Is(err, apienc.ErrUnknownKey):
			abortPlain(c, errs.ErrEncKeyUnknown)
			return
		case err != nil:
			abortPlain(c, errs.ErrEncInvalid)
			return
		}

		c.Request.Body = io.NopCloser(bytes.NewReader(plaintext))
		c.Request.ContentLength = int64(len(plaintext))
		if len(plaintext) > 0 {
			ct := c.GetHeader(HeaderEncType)
			if ct == "" {
				ct = "application/json"
			}
			c.Request.Header.Set("Content-Type", ct)
		}
		c.Set(ctxEncrypted, true)

		orig := c.Writer
		w := &bufferedWriter{ResponseWriter: orig}
		c.Writer = w
		defer func() { c.Writer = orig }()

		c.Next()

		c.Writer = orig
		w.flushEncrypted(c, session)
	}
}

// IsEncrypted 表示当前请求经过了报文解密（响应会加密返回）。下发密钥等接口据此拒绝明文请求。
func IsEncrypted(c *gin.Context) bool { return c.GetBool(ctxEncrypted) }

// isInternalSSR：带有效服务令牌、且不是 BFF 代为转发的浏览器请求。
func isInternalSSR(c *gin.Context) bool {
	return IsFromBFF(c) && c.GetHeader(HeaderBFFProxy) == ""
}

func abortPlain(c *gin.Context, e *errs.Error) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(e.Status, gin.H{"code": e.Code, "message": e.Message, "request_id": GetRequestID(c)})
}

// bufferedWriter 缓存 handler 写出的状态码与响应体，等 handler 结束后整体加密。
type bufferedWriter struct {
	gin.ResponseWriter
	status int
	buf    bytes.Buffer
}

func (w *bufferedWriter) WriteHeader(code int) {
	if code > 0 {
		w.status = code
	}
}
func (w *bufferedWriter) WriteHeaderNow()                   {}
func (w *bufferedWriter) Write(b []byte) (int, error)       { return w.buf.Write(b) }
func (w *bufferedWriter) WriteString(s string) (int, error) { return w.buf.WriteString(s) }
func (w *bufferedWriter) Written() bool                     { return w.status != 0 || w.buf.Len() > 0 }
func (w *bufferedWriter) Size() int                         { return w.buf.Len() }
func (w *bufferedWriter) Flush()                            {}
func (w *bufferedWriter) Status() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *bufferedWriter) flushEncrypted(c *gin.Context, s *apienc.Session) {
	h := w.ResponseWriter.Header()
	// 每个响应都用本次请求独有的密钥加密，共享缓存与浏览器缓存都不能复用
	h.Set("Cache-Control", "no-store")
	h.Del("Content-Length")
	status := w.Status()
	if w.buf.Len() == 0 { // 204 / 304 等无响应体
		w.ResponseWriter.WriteHeader(status)
		w.ResponseWriter.WriteHeaderNow()
		return
	}
	sealed, err := s.SealResponse(w.buf.Bytes())
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "响应加密失败", "err", err, "request_id", GetRequestID(c))
		h.Set("Content-Type", "application/json; charset=utf-8")
		w.ResponseWriter.WriteHeader(http.StatusInternalServerError)
		_, _ = w.ResponseWriter.WriteString(`{"code":"INTERNAL","message":"` + errs.ErrInternal.Message + `"}`)
		return
	}
	if ct := h.Get("Content-Type"); ct != "" {
		h.Set(HeaderEncType, ct)
	}
	h.Set("Content-Type", apienc.ContentType)
	h.Set("Content-Length", strconv.Itoa(len(sealed)))
	w.ResponseWriter.WriteHeader(status)
	_, _ = w.ResponseWriter.Write(sealed)
}
