// Package admin 是运营后台 /api/admin/* 的 handler 与中间件（docs/07 第 2 节）。
// 后台接口不做报文加密，靠独立域名 + HttpOnly 会话 + Origin/CSRF 校验 + RBAC + 审计保护（docs/06 第 0.3 节）。
package admin

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	adm "xsite/service/internal/domain/admin"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
	"xsite/service/internal/http/respond"
	"xsite/service/internal/privacy"
)

const (
	HeaderCSRFToken = "X-CSRF-Token"
	ctxIdentity     = "admin_identity"
	ctxAudit        = "admin_audit"
)

var (
	errOrigin = errs.ErrForbidden.WithMessage("请求来源校验失败，请从后台页面操作")
	errCSRF   = errs.ErrForbidden.WithMessage("页面已过期，请刷新后重试")
)

// CurrentAdmin 返回会话中间件写入的当前管理员。
func CurrentAdmin(c *gin.Context) (adm.Identity, bool) {
	v, ok := c.Get(ctxIdentity)
	if !ok {
		return adm.Identity{}, false
	}
	id, ok := v.(adm.Identity)
	return id, ok
}

// RequireSession 用会话 Cookie 换取当前管理员；没有或失效时返回 401 并清除 Cookie。
func (h *Handlers) RequireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(h.Cookie.Name)
		if err != nil || token == "" {
			respond.Error(c, errs.ErrNeedLogin)
			return
		}
		id, err := h.Auth.Authenticate(c.Request.Context(), token, c.Request.UserAgent())
		if err != nil {
			if errs.From(err).Code == errs.ErrNeedLogin.Code {
				h.Cookie.clear(c)
			}
			respond.Error(c, err)
			return
		}
		c.Set(ctxIdentity, id)
		c.Next()
	}
}

// OriginCheck 拒绝 Origin 不在白名单中的写请求（含登录），防止跨站伪造请求。
// 浏览器发出的写请求总会带 Origin；不带 Origin 的是非浏览器客户端，由会话与 CSRF 令牌兜底。
func OriginCheck(allowed []string) gin.HandlerFunc {
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		set[strings.ToLower(o)] = true
	}
	return func(c *gin.Context) {
		if origin := c.GetHeader("Origin"); !isRead(c.Request.Method) && origin != "" && !set[strings.ToLower(origin)] {
			respond.Error(c, errOrigin)
			return
		}
		c.Next()
	}
}

// CSRFToken 要求写请求携带与会话一致的 X-CSRF-Token（常量时间比较）。挂在 RequireSession 之后。
func CSRFToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isRead(c.Request.Method) {
			c.Next()
			return
		}
		id, ok := CurrentAdmin(c)
		got := c.GetHeader(HeaderCSRFToken)
		if !ok || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(id.CSRFToken)) != 1 {
			respond.Error(c, errCSRF)
			return
		}
		c.Next()
	}
}

// declared 记录路由声明过的全部权限点，启动时与数据库中的权限点目录比对（拼错的编码会让接口永远 403）。
var (
	declaredMu sync.Mutex
	declared   = map[string]bool{}
)

func declare(codes ...string) {
	declaredMu.Lock()
	defer declaredMu.Unlock()
	for _, c := range codes {
		declared[c] = true
	}
}

// DeclarePermissions 登记 handler 内按条件判定的权限点（如素材类型为 HTML 时才要的 ads.custom_html），
// 让启动时的权限点目录校验也覆盖它们。在注册路由时调用。
func DeclarePermissions(codes ...string) { declare(codes...) }

// DeclaredPermissions 返回路由声明过的权限点（排序后）。
func DeclaredPermissions() []string {
	declaredMu.Lock()
	defer declaredMu.Unlock()
	out := make([]string, 0, len(declared))
	for c := range declared {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}

// 高敏权限点的重新验证（S2-6，docs/03 第 2.3 节）：标 sensitive 的权限点，其写操作要求 5 分钟内
// 重新验证过二次因子。集合与查询实现在启动时由 app 注入；未注入时不启用（本地未配置二次验证、单元测试）。
// 只对已绑定二次验证（MFAEnabled）的管理员生效——没有二次因子就没有可重新验证的。
var (
	reauthMu       sync.RWMutex
	sensitivePerms = map[string]bool{}
	reauthChecker  ReauthChecker
)

// ReauthChecker 查某会话是否在高敏重新验证窗口内、且仍在当初重新验证的 IP 网段
// （由 store/cache.AdminMFAStore 实现；网段变化也视为需重新验证，docs/10 第 6.3 节）。
type ReauthChecker interface {
	IsReauthed(ctx context.Context, sessionKey, ipPrefix string) (bool, error)
}

// SetSensitivePermissions 登记高敏权限点集合（启动时从 admin_permission.sensitive 载入）。
func SetSensitivePermissions(codes []string) {
	reauthMu.Lock()
	defer reauthMu.Unlock()
	sensitivePerms = make(map[string]bool, len(codes))
	for _, c := range codes {
		sensitivePerms[c] = true
	}
}

// SetReauthChecker 注入重新验证窗口的查询实现（nil 则不启用重新验证门槛）。
func SetReauthChecker(rc ReauthChecker) {
	reauthMu.Lock()
	defer reauthMu.Unlock()
	reauthChecker = rc
}

func isSensitivePerm(code string) bool {
	reauthMu.RLock()
	defer reauthMu.RUnlock()
	return sensitivePerms[code]
}

// reauthGate 对已判定为高敏的写请求检查会话是否在重新验证窗口内；不在则写出 412 并返回 false。
func reauthGate(c *gin.Context, id adm.Identity) bool {
	if !id.MFAEnabled { // 没绑定二次因子，无可重新验证（登录强制与超管强制开启负责这部分）
		return true
	}
	reauthMu.RLock()
	rc := reauthChecker
	reauthMu.RUnlock()
	if rc == nil { // 未启用（本地 / 测试）
		return true
	}
	ok, err := rc.IsReauthed(c.Request.Context(), id.SessionKey, privacy.IPPrefix(c.ClientIP()))
	if err != nil {
		respond.Error(c, err)
		return false
	}
	if !ok {
		respond.Error(c, errs.ErrReauthRequired)
		return false
	}
	return true
}

// RequirePermission 要求当前管理员拥有任一给定的权限点（docs/03 第 2.5 节），否则 403。挂在 RequireSession 之后。
// 权限只在后端判定，前端只做显示控制。高敏权限点的写操作另要求 5 分钟内重新验证（reauthGate）。
func RequirePermission(codes ...string) gin.HandlerFunc {
	declare(codes...)
	return func(c *gin.Context) {
		id, ok := CurrentAdmin(c)
		if !ok {
			respond.Error(c, errs.ErrNeedLogin)
			return
		}
		if !slices.ContainsFunc(codes, id.Can) {
			respond.Error(c, errs.ErrForbidden)
			return
		}
		if !isRead(c.Request.Method) && slices.ContainsFunc(codes, isSensitivePerm) && !reauthGate(c, id) {
			return
		}
		c.Next()
	}
}

// RequireSuperMFA 强制超级管理员开启二次验证（S2-6 切片 7，验收：超管无二次验证无法使用后台）。
// 未绑定任何二次因子的超管登录后，除「查看自己 / 登出 / 开启二次验证」相关接口外一律 403，
// 直到绑定 TOTP 或 Passkey。挂在 RequireSession 之后（需要当前管理员）。
func RequireSuperMFA() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := CurrentAdmin(c)
		if ok && id.Role.IsSuper && !id.MFAEnabled && !mfaEnrollPath(c) {
			respond.Error(c, errs.ErrForbidden.WithMessage("超级管理员必须先开启二次验证（账号安全）后才能操作"))
			return
		}
		c.Next()
	}
}

// mfaEnrollPath 判断当前路由是否为「允许未开启二次验证的超管访问」的开启 / 自查路径。
func mfaEnrollPath(c *gin.Context) bool {
	switch c.FullPath() {
	case "/api/admin/me", "/api/admin/me/menus", "/api/admin/logout":
		return true
	}
	return strings.HasPrefix(c.FullPath(), "/api/admin/mfa/")
}

// RequirePage 声明一组路由属于某个页面：读请求（GET/HEAD/OPTIONS）要 <page>.view，写请求要 <page>.edit。
// 只读页面（没有 .edit 权限点）请用 RequirePermission("<page>.view")。
func RequirePage(page string) gin.HandlerFunc {
	view, edit := page+".view", page+".edit"
	declare(view, edit)
	return func(c *gin.Context) {
		id, ok := CurrentAdmin(c)
		if !ok {
			respond.Error(c, errs.ErrNeedLogin)
			return
		}
		need := edit
		if isRead(c.Request.Method) {
			need = view
		}
		if !id.Can(need) {
			respond.Error(c, errs.ErrForbidden)
			return
		}
		if !isRead(c.Request.Method) && isSensitivePerm(need) && !reauthGate(c, id) {
			return
		}
		c.Next()
	}
}

// AuditDetail 是 handler 为本次写操作补充的审计内容。Before / After 中的密码、密钥类字段写入前会被替换为 ***。
type AuditDetail struct {
	Action     string // 如 drama.update；为空时记为「方法 + 路由模板」
	TargetType string // 如 drama、episode、admin
	TargetID   string
	Before     any
	After      any
}

// SetAudit 由 handler 调用，补充本次写操作的审计内容。
func SetAudit(c *gin.Context, d AuditDetail) { c.Set(ctxAudit, d) }

// AuditRecorder 由 platform.AuditRecorder 实现。
type AuditRecorder interface {
	Record(e platform.AuditEntry)
}

// Audit 在写请求成功（2xx）后提交审计记录（异步落库，docs/03 第 1 节）。挂在 RequireSession 之后。
func Audit(rec AuditRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		if isRead(c.Request.Method) {
			c.Next()
			return
		}
		c.Next()

		if s := c.Writer.Status(); s < http.StatusOK || s >= http.StatusMultipleChoices {
			return
		}
		id, ok := CurrentAdmin(c)
		if !ok {
			return
		}
		var d AuditDetail
		if v, ok := c.Get(ctxAudit); ok {
			d, _ = v.(AuditDetail)
		}
		if d.Action == "" {
			d.Action = c.Request.Method + " " + c.FullPath()
		}
		entry := platform.AuditEntry{
			AdminID:    id.AdminID,
			Action:     d.Action,
			TargetType: d.TargetType,
			TargetID:   d.TargetID,
			IPPrefix:   privacy.IPPrefix(c.ClientIP()),
			RequestID:  middleware.GetRequestID(c),
			CreatedAt:  time.Now(),
		}
		var err error
		if entry.Before, err = platform.RedactJSON(d.Before); err != nil {
			slog.ErrorContext(c.Request.Context(), "审计内容无法编码", "action", d.Action, "err", err)
		}
		if entry.After, err = platform.RedactJSON(d.After); err != nil {
			slog.ErrorContext(c.Request.Context(), "审计内容无法编码", "action", d.Action, "err", err)
		}
		rec.Record(entry)
	}
}

func isRead(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
