// Package errs 定义领域错误与业务码（docs/07 第 4 节）。
// 领域层返回这些哨兵错误（可用 %w 包装），HTTP 层统一映射为状态码与业务码。
package errs

import (
	"errors"
	"net/http"
	"time"
)

// Error 是可直接返回给客户端的业务错误。Message 面向用户，不包含内部细节。
type Error struct {
	Code    string
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Is 让带不同 Message 的同类错误也能用 errors.Is 匹配。
func (e *Error) Is(target error) bool {
	var t *Error
	return errors.As(target, &t) && t.Code == e.Code
}

// WithMessage 返回同一业务码、不同提示文案的副本。
func (e *Error) WithMessage(msg string) *Error {
	return &Error{Code: e.Code, Status: e.Status, Message: msg}
}

var (
	ErrBadRequest        = &Error{"BAD_REQUEST", http.StatusBadRequest, "请求参数错误"}
	ErrNeedLogin         = &Error{"NEED_LOGIN", http.StatusUnauthorized, "请先登录"}
	ErrNeedVIP           = &Error{"NEED_VIP", http.StatusPaymentRequired, "开通会员后即可观看"}
	ErrTokenExpired      = &Error{"TOKEN_EXPIRED", http.StatusUnauthorized, "登录已过期"}
	ErrSignatureInvalid  = &Error{"SIGNATURE_INVALID", http.StatusUnauthorized, "请求签名无效"}
	ErrForbidden         = &Error{"FORBIDDEN", http.StatusForbidden, "没有权限"}
	ErrNotFound          = &Error{"NOT_FOUND", http.StatusNotFound, "内容不存在或已下架"}
	ErrConflict          = &Error{"CONFLICT", http.StatusConflict, "数据冲突，请刷新后重试"}
	ErrReauthRequired    = &Error{"REAUTH_REQUIRED", http.StatusPreconditionFailed, "请重新验证身份"}
	ErrChallengeRequired = &Error{"CHALLENGE_REQUIRED", http.StatusPreconditionRequired, "请完成人机验证"}
	ErrRateLimited       = &Error{"RATE_LIMITED", http.StatusTooManyRequests, "操作太频繁，请稍后再试"}
	// 报文加密（docs/10 第 5.3 节）。这几类错误发生在解密之前，响应为明文。
	ErrEncRequired   = &Error{"ENC_REQUIRED", http.StatusBadRequest, "请求必须加密"}
	ErrEncInvalid    = &Error{"ENC_INVALID", http.StatusBadRequest, "加密报文无效"}
	ErrEncKeyUnknown = &Error{"ENC_KEY_UNKNOWN", http.StatusBadRequest, "加密公钥已更新，请刷新后重试"}
	ErrEncTimeSkew   = &Error{"ENC_TIME_SKEW", http.StatusBadRequest, "设备时间与服务器相差过大"}
	ErrUnavailable   = &Error{"UNAVAILABLE", http.StatusServiceUnavailable, "服务暂不可用"}
	ErrInternal      = &Error{"INTERNAL", http.StatusInternalServerError, "服务开小差了，请稍后再试"}
)

// From 把任意错误转换为业务错误；无法识别的错误统一视为 INTERNAL，避免泄露内部信息。
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return ErrInternal
}

// RetryAfterError 给业务错误附带客户端应等待的时长，HTTP 层据此输出 Retry-After 头（429 等）。
type RetryAfterError struct {
	Err   *Error
	After time.Duration
}

func (e *RetryAfterError) Error() string { return e.Err.Error() }
func (e *RetryAfterError) Unwrap() error { return e.Err }

// WithRetryAfter 包装业务错误并附带等待时长。
func WithRetryAfter(e *Error, after time.Duration) error {
	return &RetryAfterError{Err: e, After: after}
}

// RetryAfter 取出错误链上的等待时长；没有时返回 0。
func RetryAfter(err error) time.Duration {
	var ra *RetryAfterError
	if errors.As(err, &ra) {
		return ra.After
	}
	return 0
}
