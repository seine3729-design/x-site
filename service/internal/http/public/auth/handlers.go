// Package auth 是用户端身份接口（docs/03 第 2 节，S1-10）：游客签发、令牌刷新与登出。
// 注册在 /api/v1 分组上，因此自动走报文加密中间件；handler 只读写明文。
package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/domain/identity"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

const (
	// 游客签发按 IP 限流：每个 IP 每分钟最多签发这么多次游客身份，防止刷库。
	guestPerMinute = 30
	limitWindow    = 2 * time.Minute
	tokenType      = "Bearer"
	noStore        = "private, no-store"
)

// Service 是身份服务（domain/identity.Service）。
type Service interface {
	IssueGuest(ctx context.Context, in identity.IssueInput) (identity.Tokens, error)
	Refresh(ctx context.Context, in identity.RefreshInput) (identity.Tokens, error)
	Logout(ctx context.Context, refreshToken string) error
}

// Limiter 是按 key 的固定窗口计数器（cache.Counter）。
type Limiter interface {
	Add(ctx context.Context, key string, delta int64, window time.Duration) (int64, error)
}

// Handlers 聚合用户端身份接口。
type Handlers struct {
	Svc   Service     // 游客签发、令牌刷新与登出（domain/identity）
	Users UserService // 注册 / 登录（domain/user，S2-1）
	// Limiter 是游客签发限流，RegisterLimiter 是注册限流（都按 IP 前缀，前缀不同）。
	Limiter         Limiter
	RegisterLimiter Limiter
}

// Routes 注册身份接口到 /api/v1 分组。
func (h *Handlers) Routes(v1 gin.IRouter) {
	g := v1.Group("/auth")
	g.POST("/guest", h.Guest)
	g.POST("/register", h.Register)
	g.POST("/login", h.Login)
	g.GET("/challenge", h.Challenge)
	g.POST("/refresh", h.Refresh)
	g.POST("/logout", h.Logout)
}

type guestRequest struct {
	Platform      string `json:"platform"`
	SourceChannel string `json:"source_channel"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	DeviceID     string `json:"device_id,omitempty"`
	Role         string `json:"role"`
}

// Guest 是 POST /api/v1/auth/guest：签发游客身份（请求体可空）。
func (h *Handlers) Guest(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req guestRequest
	if err := bindOptionalJSON(c, &req); err != nil {
		respond.Error(c, err)
		return
	}
	if wait := h.overGuestLimit(c); wait > 0 {
		respond.Error(c, errs.WithRetryAfter(errs.ErrRateLimited, wait))
		return
	}
	tok, err := h.Svc.IssueGuest(c.Request.Context(), identity.IssueInput{
		Platform: req.Platform, SourceChannel: req.SourceChannel,
		UADigest: uaDigest(c), IPPrefix: ipPrefix(c.ClientIP()),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, toResponse(tok))
}

// Refresh 是 POST /api/v1/auth/refresh：轮换 Refresh Token，返回新的 Access/Refresh。
func (h *Handlers) Refresh(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req refreshRequest
	if err := bindOptionalJSON(c, &req); err != nil {
		respond.Error(c, err)
		return
	}
	tok, err := h.Svc.Refresh(c.Request.Context(), identity.RefreshInput{
		RefreshToken: strings.TrimSpace(req.RefreshToken), UADigest: uaDigest(c), IPPrefix: ipPrefix(c.ClientIP()),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, toResponse(tok))
}

// Logout 是 POST /api/v1/auth/logout：吊销当前刷新令牌所在的会话族。令牌缺失也返回 204（幂等）。
func (h *Handlers) Logout(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req refreshRequest
	if err := bindOptionalJSON(c, &req); err != nil {
		respond.Error(c, err)
		return
	}
	if err := h.Svc.Logout(c.Request.Context(), strings.TrimSpace(req.RefreshToken)); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func toResponse(t identity.Tokens) tokenResponse {
	return tokenResponse{
		AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, TokenType: tokenType,
		ExpiresIn: t.ExpiresIn, DeviceID: t.DeviceID, Role: string(t.Role),
	}
}

// overGuestLimit 按自然分钟的固定窗口限制游客签发（按 IP）；计数器故障时放行。
func (h *Handlers) overGuestLimit(c *gin.Context) time.Duration {
	if h.Limiter == nil {
		return 0
	}
	now := time.Now()
	key := "ip:" + ipPrefix(c.ClientIP()) + ":" + strconv.FormatInt(now.Unix()/60, 10)
	n, err := h.Limiter.Add(c.Request.Context(), key, 1, limitWindow)
	if err != nil || n <= guestPerMinute {
		return 0
	}
	return time.Duration(60-now.Unix()%60) * time.Second
}

// bindOptionalJSON 解析请求体；空体按默认值（不报错），非法 JSON 或未知字段返回 400。
func bindOptionalJSON(c *gin.Context, v any) error {
	if c.Request.Body == nil {
		return nil
	}
	b, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return errs.ErrBadRequest.WithMessage("请求体读取失败")
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errs.ErrBadRequest.WithMessage("请求体格式错误")
	}
	return nil
}

// uaDigest 返回 User-Agent 的摘要（SHA-256 前 8 字节十六进制）：用于「我的设备」展示，不记录原始 UA。
func uaDigest(c *gin.Context) string {
	ua := c.GetHeader("User-Agent")
	if ua == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(ua))
	return hex.EncodeToString(sum[:8])
}

// ipPrefix 返回保护隐私的 IP 前缀（IPv4 /24，IPv6 /48），不存完整 IP。
func ipPrefix(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	bits := 24
	if addr.Is6() {
		bits = 48
	}
	p, err := addr.Prefix(bits)
	if err != nil {
		return ""
	}
	return p.String()
}
