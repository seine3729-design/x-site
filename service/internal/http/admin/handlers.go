package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"

	"github.com/gin-gonic/gin"

	adm "xsite/service/internal/domain/admin"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// Authenticator 是 handler 依赖的登录服务（domain/admin.Service）。
type Authenticator interface {
	Login(ctx context.Context, in adm.LoginInput) (adm.LoginResult, error)
	LoginMFA(ctx context.Context, in adm.LoginMFAInput) (adm.LoginResult, error)
	Reauth(ctx context.Context, id adm.Identity, method, code string, response []byte, clientIP string) error
	// MFATicketAdmin 用登录二次验证票据换管理员 ID（不消费），供 Passkey 登录第二步先取断言选项。
	MFATicketAdmin(ctx context.Context, ticket string) (int64, error)
	Authenticate(ctx context.Context, token, userAgent string) (adm.Identity, error)
	Logout(ctx context.Context, sessionKey string) error
}

// SessionCookie 描述后台会话 Cookie：HttpOnly、SameSite=Strict、Path=/，不设过期时间（关闭浏览器即失效）。
// 非开发环境加 Secure 并使用 __Host- 前缀（禁止子域写入同名 Cookie）。后台独占域名，因此 Path 用 /。
type SessionCookie struct {
	Name   string
	Secure bool
}

// NewSessionCookie 按是否启用 HTTPS 生成 Cookie 配置。本地开发走 http://localhost，不能设 Secure。
func NewSessionCookie(secure bool) SessionCookie {
	if secure {
		return SessionCookie{Name: "__Host-xs_admin", Secure: true}
	}
	return SessionCookie{Name: "xs_admin"}
}

func (sc SessionCookie) set(c *gin.Context, token string) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sc.Name, Value: token, Path: "/",
		HttpOnly: true, Secure: sc.Secure, SameSite: http.SameSiteStrictMode,
	})
}

func (sc SessionCookie) clear(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sc.Name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: sc.Secure, SameSite: http.SameSiteStrictMode,
	})
}

// Handlers 聚合后台登录、会话与权限管理 handler。
type Handlers struct {
	Auth    Authenticator
	Manage  Manager
	MFA     MFAService
	Passkey PasskeyService
	Cookie  SessionCookie
}

type loginRequest struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=128"`
}

type adminUserDTO struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Role       string `json:"role"` // 角色编码
	RoleName   string `json:"role_name"`
	IsSuper    bool   `json:"is_super"`
	MFAEnabled bool   `json:"mfa_enabled"`
}

// sessionDTO 是 /me 的响应：当前管理员、CSRF 令牌（前端只存内存）与已授予的权限点。
// 超级管理员拥有全部权限点，permissions 为空，前端以 is_super 判断。
type sessionDTO struct {
	User        adminUserDTO `json:"user"`
	CSRFToken   string       `json:"csrf_token"`
	Permissions []string     `json:"permissions"`
}

// loginDTO 是登录成功（无需二次验证或已通过）的响应：mfa_required=false + 会话信息。
type loginDTO struct {
	MFARequired bool         `json:"mfa_required"`
	User        adminUserDTO `json:"user"`
	CSRFToken   string       `json:"csrf_token"`
	Permissions []string     `json:"permissions"`
}

// loginChallengeDTO 是账号密码正确但账号已启用二次验证时的响应：带一次性票据，不含会话。
type loginChallengeDTO struct {
	MFARequired bool     `json:"mfa_required"` // 恒为 true
	MFATicket   string   `json:"mfa_ticket"`
	MFAMethods  []string `json:"mfa_methods"`
}

// loginMFARequest 是二次验证登录第二步的入参。method=totp 用 code；method=passkey 用 response（断言 JSON）。
type loginMFARequest struct {
	MFATicket string          `json:"mfa_ticket" binding:"required,max=64"`
	Method    string          `json:"method" binding:"required,max=16"`
	Code      string          `json:"code" binding:"max=16"`
	Response  json.RawMessage `json:"response"`
}

// reauthRequest 是高敏操作重新验证的入参。method=totp 用 code；method=passkey 用 response（断言 JSON）。
type reauthRequest struct {
	Method   string          `json:"method" binding:"required,max=16"`
	Code     string          `json:"code" binding:"max=16"`
	Response json.RawMessage `json:"response"`
}

func sessionOf(id adm.Identity) sessionDTO {
	perms := id.Permissions()
	slices.Sort(perms)
	return sessionDTO{
		User: adminUserDTO{
			ID: id.AdminID, Username: id.Username, Role: id.Role.Code, RoleName: id.Role.Name,
			IsSuper: id.Role.IsSuper, MFAEnabled: id.MFAEnabled,
		},
		CSRFToken:   id.CSRFToken,
		Permissions: perms,
	}
}

// Login 是 POST /api/admin/login：账号密码登录，成功后下发 HttpOnly 会话 Cookie。
func (h *Handlers) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest)
		return
	}
	res, err := h.Auth.Login(c.Request.Context(), adm.LoginInput{
		Username:  req.Username,
		Password:  req.Password,
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	if res.MFARequired {
		// 账号密码已过，但账号启用了二次验证：不下发会话，返回一次性票据等第二步
		respond.OK(c, loginChallengeDTO{MFARequired: true, MFATicket: res.MFATicket, MFAMethods: res.MFAMethods})
		return
	}
	h.Cookie.set(c, res.Token)
	s := sessionOf(res.Identity)
	respond.OK(c, loginDTO{MFARequired: false, User: s.User, CSRFToken: s.CSRFToken, Permissions: s.Permissions})
}

// LoginMFA 是 POST /api/admin/login/mfa：二次验证登录第二步，用票据 + 验证码换会话。
func (h *Handlers) LoginMFA(c *gin.Context) {
	var req loginMFARequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest)
		return
	}
	res, err := h.Auth.LoginMFA(c.Request.Context(), adm.LoginMFAInput{
		Ticket:    req.MFATicket,
		Method:    req.Method,
		Code:      req.Code,
		Response:  req.Response,
		ClientIP:  c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	h.Cookie.set(c, res.Token)
	respond.OK(c, sessionOf(res.Identity))
}

// Reauth 是 POST /api/admin/reauth：高敏操作前重新验证二次因子（docs/03 第 2.3 节）。
// 通过后本会话在 5 分钟内可执行高敏写操作（标 sensitive 的权限点）。
func (h *Handlers) Reauth(c *gin.Context) {
	id, _ := CurrentAdmin(c)
	var req reauthRequest
	if !Bind(c, &req) {
		return
	}
	if err := h.Auth.Reauth(c.Request.Context(), id, req.Method, req.Code, req.Response, c.ClientIP()); err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin.reauth", TargetType: "admin", TargetID: strconv.FormatInt(id.AdminID, 10)})
	respond.OK(c, nil)
}

// Me 是 GET /api/admin/me：当前管理员、CSRF 令牌与权限。页面刷新后前端靠它恢复会话状态。
func (h *Handlers) Me(c *gin.Context) {
	id, _ := CurrentAdmin(c)
	respond.OK(c, sessionOf(id))
}

// Logout 是 POST /api/admin/logout：吊销当前会话并清除 Cookie。
func (h *Handlers) Logout(c *gin.Context) {
	id, _ := CurrentAdmin(c)
	if err := h.Auth.Logout(c.Request.Context(), id.SessionKey); err != nil {
		respond.Error(c, err)
		return
	}
	h.Cookie.clear(c)
	SetAudit(c, AuditDetail{Action: "admin.logout", TargetType: "admin", TargetID: strconv.FormatInt(id.AdminID, 10)})
	respond.OK(c, nil)
}
