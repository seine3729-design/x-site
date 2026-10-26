// Package admin 是后台管理员域：账号、登录与会话、可配置的权限（角色 → 权限点 → 菜单，docs/adr/0006）。
// 二次验证（TOTP / Passkey）与高敏操作重新验证在 S2-6 接入。
package admin

import (
	"context"
	"strings"
	"time"
)

// Status 是管理员账号状态（docs/06 第 33 节：可登录 / 停用 / 未登录过）。
type Status string

const (
	StatusActive   Status = "active"
	StatusInactive Status = "inactive"
	// StatusUnused 是新建后从未登录过的账号，可以登录，首次登录后变为 active。
	StatusUnused Status = "unused"
)

// Valid 表示是否为已知状态。
func (s Status) Valid() bool { return s == StatusActive || s == StatusInactive || s == StatusUnused }

// RoleRef 是管理员所属角色的摘要。
type RoleRef struct {
	ID      int64
	Code    string
	Name    string
	IsSuper bool
	// Active 为 false 时角色已停用：其成员不能登录，已有会话立即失效。
	Active bool
}

// Account 是管理员账号。PasswordHash 只用于登录校验，不离开本域。
// Permissions 只在 GetByID 时加载（超级角色为空，由 Role.IsSuper 表示拥有全部权限）。
type Account struct {
	ID             int64
	Username       string
	PasswordHash   string
	Status         Status
	SessionVersion int32
	// MFAEnabled 为是否已绑定任一二次验证因子（TOTP 或 Passkey）。HasTOTP / HasPasskey 只在登录（GetByUsername）
	// 时填充，供第二步决定可用方式；GetByID 只填 MFAEnabled。
	MFAEnabled  bool
	HasTOTP     bool
	HasPasskey  bool
	Role        RoleRef
	Permissions []string
}

// Identity 是通过会话校验的当前管理员，由会话中间件写入请求上下文。
type Identity struct {
	AdminID    int64
	Username   string
	Role       RoleRef
	MFAEnabled bool
	CSRFToken  string
	// SessionKey 是会话令牌的摘要（Valkey 中的键），用于登出；不是令牌本身。
	SessionKey string
	perms      map[string]bool
}

// Can 表示当前管理员是否拥有某个权限点。超级角色拥有全部权限点。
func (id Identity) Can(code string) bool { return id.Role.IsSuper || id.perms[code] }

// Permissions 返回当前角色被授予的权限点（超级角色返回空，调用方用 Role.IsSuper 判断）。
func (id Identity) Permissions() []string {
	out := make([]string, 0, len(id.perms))
	for c := range id.perms {
		out = append(out, c)
	}
	return out
}

// NewIdentity 构造身份，供本域与测试使用。
func NewIdentity(adminID int64, username string, role RoleRef, perms []string) Identity {
	id := Identity{AdminID: adminID, Username: username, Role: role, perms: make(map[string]bool, len(perms))}
	for _, p := range perms {
		id.perms[p] = true
	}
	return id
}

// Session 是保存在 Valkey 中的后台会话。键为令牌的 SHA-256，不保存令牌本身。
// SessionVersion 与账号当前值不一致时会话作废（改密码、强制下线时账号的值加一）。
type Session struct {
	AdminID        int64
	SessionVersion int32
	CSRFToken      string
	UADigest       string
	CreatedAt      time.Time
}

// LoginInput 是一次登录尝试。ClientIP 只用于失败计数，写库前转为网段前缀。
type LoginInput struct {
	Username  string
	Password  string
	ClientIP  string
	UserAgent string
}

// LoginResult 是登录的结果。Token 写入 HttpOnly Cookie，只在登录响应中出现一次。
// 账号已启用二次验证时第一步（账号密码）返回 MFARequired=true + 一次性票据，不含会话；
// 第二步 LoginMFA 校验验证码后才返回 Token 与 Identity。
type LoginResult struct {
	Token    string
	Identity Identity

	// 二次验证（S2-6）。MFARequired 为 true 时 Token/Identity 为空，改用 MFATicket + MFAMethods。
	MFARequired bool
	MFATicket   string
	MFAMethods  []string
}

// LoginMFAInput 是二次验证登录的第二步：用第一步发的票据 + 验证码（TOTP）或断言（Passkey）换会话。
// ClientIP 用于失败计数与登录记录；UserAgent 绑定到新会话（与后续请求一致才有效）。
type LoginMFAInput struct {
	Ticket    string
	Method    string // totp | passkey
	Code      string // method=totp
	Response  []byte // method=passkey：WebAuthn 断言响应 JSON
	ClientIP  string
	UserAgent string
}

// LoginOutcome 是登录记录的结果（admin_login_log.result）。
type LoginOutcome string

const (
	LoginSuccess     LoginOutcome = "success"
	LoginBadPassword LoginOutcome = "bad_password"
	LoginInactive    LoginOutcome = "inactive"
	LoginLocked      LoginOutcome = "locked"
	// LoginMFAFailed：账号密码正确但二次验证码错误（docs/02 第 6.1 节 admin_login_log.result）。
	LoginMFAFailed LoginOutcome = "mfa_failed"
)

// LoginLog 是一条登录记录（docs/02 第 6.1 节 admin_login_log）。用户名不存在时 AdminID 为空。
type LoginLog struct {
	AdminID       *int64
	UsernameTried string
	Result        LoginOutcome
	IPPrefix      string
	UADigest      string
}

// AccountRepo 读写管理员账号与登录记录。查不到时返回 errs.ErrNotFound。
type AccountRepo interface {
	GetByUsername(ctx context.Context, username string) (Account, error)
	// GetByID 同时加载角色的权限点。
	GetByID(ctx context.Context, id int64) (Account, error)
	// MarkLogin 记录登录时间，并把 unused 状态改为 active。
	MarkLogin(ctx context.Context, id int64) error
	UpdatePasswordHash(ctx context.Context, id int64, hash string) error
	InsertLoginLog(ctx context.Context, log LoginLog) error
}

// SessionStore 保存后台会话，key 为令牌摘要。不存在时返回 errs.ErrNotFound。
type SessionStore interface {
	Create(ctx context.Context, key string, s Session, ttl time.Duration) error
	// GetAndTouch 读取会话，并把空闲过期时间重置为 ttl（滑动过期）。
	GetAndTouch(ctx context.Context, key string, ttl time.Duration) (Session, error)
	Delete(ctx context.Context, key string) error
}

// FailureCounter 记录登录失败次数，每次失败后有效期重置为 window。
type FailureCounter interface {
	// Get 返回当前计数与剩余有效期；键不存在时返回 0。
	Get(ctx context.Context, key string) (int64, time.Duration, error)
	Incr(ctx context.Context, key string, window time.Duration) (int64, error)
	Reset(ctx context.Context, key string) error
}

// TOTPVerifier 校验某管理员已绑定的 TOTP 验证码（S2-6，登录第二步与高敏重验共用，由 MFAService 实现）。
// 账号未绑定时返回 (false, nil)；密钥环未配置或密文损坏时返回错误（登录按失败处理）。
type TOTPVerifier interface {
	VerifyTOTP(ctx context.Context, adminID int64, code string) (bool, error)
}

// PasskeyAuthenticator 校验某管理员的 Passkey 断言（登录第二步与高敏重验共用，由 PasskeyService 实现）。
// challengeKey 定位 begin 时暂存的 WebAuthn 挑战会话（登录按票据、reauth 按会话键）。校验不过返回 400。
type PasskeyAuthenticator interface {
	FinishAssertion(ctx context.Context, adminID int64, challengeKey string, response []byte) error
}

// MFAStore 暂存后台二次验证的会话级临时状态（Valkey）：登录票据（账号密码已过、待二次验证，一次性短 TTL）
// 与高敏操作「最近重新验证」标记（按会话键，5 分钟）。
type MFAStore interface {
	CreateLoginTicket(ctx context.Context, ticket string, adminID int64, ttl time.Duration) error
	// LookupLoginTicket 用票据换管理员 ID；不存在返回 (0, false, nil)。
	LookupLoginTicket(ctx context.Context, ticket string) (adminID int64, found bool, err error)
	DeleteLoginTicket(ctx context.Context, ticket string) error
	// MarkReauthed 记某会话刚通过高敏重新验证（登录过二次验证也算）的 IP 网段与有效窗口。
	// 存网段用于「IP 段变化时要求重新验证」：之后请求网段不同即视为未重新验证（docs/10 第 6.3 节）。
	MarkReauthed(ctx context.Context, sessionKey, ipPrefix string, ttl time.Duration) error
}

// PasswordHasher 由 internal/crypto/password 实现。
type PasswordHasher interface {
	Hash(ctx context.Context, plain string) (string, error)
	Verify(ctx context.Context, plain, encoded string) (ok, needsRehash bool, err error)
	Equalize(ctx context.Context, plain string)
}

// NormalizeUsername 统一用户名大小写与首尾空白：管理员用户名不区分大小写，库中保存小写。
func NormalizeUsername(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ValidUsername：3–32 位，只含小写字母、数字、下划线、点、连字符（先经 NormalizeUsername）。
func ValidUsername(s string) bool {
	if len(s) < 3 || len(s) > 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		ok := ch == '_' || ch == '.' || ch == '-' || (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z')
		if !ok {
			return false
		}
	}
	return true
}
