package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"
	"unicode"

	"xsite/service/internal/errs"
	"xsite/service/internal/privacy"
)

const (
	// 会话空闲 2 小时过期、绝对时长 7 天（docs/03 第 2.3 节）。
	SessionIdleTimeout     = 2 * time.Hour
	SessionAbsoluteTimeout = 7 * 24 * time.Hour

	// 登录失败计数：最后一次失败后 15 分钟清零。同一用户名连续失败 5 次、同一 IP 失败 20 次后锁定，
	// 直到计数过期；锁定期间不再校验密码。
	failureWindow    = 15 * time.Minute
	maxUserFailures  = 5
	maxIPFailures    = 20
	maxUsernameInLog = 64
	// 失败计数与登录记录的写入超时（不随请求取消）。
	accountingTimeout = 2 * time.Second

	tokenBytes = 32

	// 二次验证登录票据的有效期（S2-6）：账号密码过了之后，这段时间内必须完成第二步。
	mfaTicketTTL = 5 * time.Minute

	// ReauthWindow 是高敏操作「最近重新验证」的有效窗口（S2-6，docs/03 第 2.3 节）：
	// 过去这段时间内重新验证过（或刚登录并过了二次验证）才放行高敏写操作。
	ReauthWindow = 5 * time.Minute
)

var (
	// 用户名不存在与密码错误返回同一个错误（防枚举）。
	errBadCredentials = errs.ErrNeedLogin.WithMessage("用户名或密码错误")
	errLocked         = errs.ErrRateLimited.WithMessage("登录失败次数过多，请稍后再试")
	errInactive       = errs.ErrForbidden.WithMessage("账号已停用，请联系超级管理员")
	errRoleDisabled   = errs.ErrForbidden.WithMessage("所属角色已停用，请联系超级管理员")
	errSessionInvalid = errs.ErrNeedLogin.WithMessage("登录已失效，请重新登录")

	// 二次验证（S2-6）。票据失效按「重新登录」处理；验证码错误是 400。
	errMFATicketInvalid = errs.ErrNeedLogin.WithMessage("二次验证已超时，请重新登录")
	errMFACode          = errs.ErrBadRequest.WithMessage("验证码不正确，请检查认证器时间后重试")
	errMFAMethod        = errs.ErrBadRequest.WithMessage("不支持的二次验证方式")

	// 高敏操作重新验证（S2-6）。
	errReauthUnavailable = errs.ErrBadRequest.WithMessage("后台未启用二次验证，无法重新验证")
	errReauthNoFactor    = errs.ErrBadRequest.WithMessage("请先在账号安全绑定二次验证")
	errReauthCode        = errs.ErrBadRequest.WithMessage("验证码不正确，请检查认证器时间后重试")
)

// Service 处理后台登录与会话。
type Service struct {
	accounts AccountRepo
	sessions SessionStore
	failures FailureCounter
	hasher   PasswordHasher
	now      func() time.Time

	// 二次验证（S2-6，可选）：装配时经 WithMFA 注入。两者都为 nil 时不启用二次验证
	// （本地未配置 ADMIN_TOTP_KEKS 时即如此；此时账号也无法绑定 TOTP，不会出现 MFAEnabled 账号）。
	mfa      TOTPVerifier
	mfaStore MFAStore
	passkey  PasskeyAuthenticator
}

// Option 配置 Service 的可选依赖。
type Option func(*Service)

// WithMFA 启用后台二次验证（S2-6）：已绑定二次验证的账号登录要过第二步校验（LoginMFA），
// 高敏操作要 5 分钟内重新验证（Reauth）。
func WithMFA(verifier TOTPVerifier, store MFAStore) Option {
	return func(s *Service) {
		s.mfa = verifier
		s.mfaStore = store
	}
}

// WithPasskey 启用 Passkey 作为二次验证因子（登录第二步、高敏重验）。
func WithPasskey(pa PasskeyAuthenticator) Option {
	return func(s *Service) { s.passkey = pa }
}

// NewService 创建后台登录服务。opts 可注入二次验证等可选依赖。
func NewService(accounts AccountRepo, sessions SessionStore, failures FailureCounter, hasher PasswordHasher, opts ...Option) *Service {
	s := &Service{accounts: accounts, sessions: sessions, failures: failures, hasher: hasher, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Login 校验账号密码并创建会话。二次验证在 S2-6 接入，届时需要二次验证的账号先返回挑战而不是会话。
func (s *Service) Login(ctx context.Context, in LoginInput) (LoginResult, error) {
	username := NormalizeUsername(in.Username)
	record := LoginLog{
		UsernameTried: printable(username, maxUsernameInLog),
		IPPrefix:      privacy.IPPrefix(in.ClientIP),
		UADigest:      privacy.UADigest(in.UserAgent),
	}
	userKey, ipKey := "u:"+username, "ip:"+limiterIP(in.ClientIP)

	wait, err := s.lockedFor(ctx, userKey, ipKey)
	if err != nil {
		return LoginResult{}, err
	}
	if wait > 0 {
		s.writeLog(ctx, record, LoginLocked, nil)
		return LoginResult{}, errs.WithRetryAfter(errLocked, wait)
	}

	// 格式不合法的用户名不可能存在，不查库，按「不存在」处理
	acct, err := Account{}, error(errs.ErrNotFound)
	if ValidUsername(username) {
		acct, err = s.accounts.GetByUsername(ctx, username)
	}
	if errors.Is(err, errs.ErrNotFound) {
		// 做一次同等代价的哈希计算，让「用户名不存在」与「密码错误」耗时相近
		s.hasher.Equalize(ctx, in.Password)
		return LoginResult{}, s.fail(ctx, record, nil, userKey, ipKey)
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("读取管理员: %w", err)
	}

	ok, needsRehash, err := s.hasher.Verify(ctx, in.Password, acct.PasswordHash)
	if err != nil {
		return LoginResult{}, fmt.Errorf("校验密码: %w", err)
	}
	if !ok {
		return LoginResult{}, s.fail(ctx, record, &acct.ID, userKey, ipKey)
	}
	if acct.Status == StatusInactive || !acct.Role.Active {
		s.writeLog(ctx, record, LoginInactive, &acct.ID)
		if acct.Status == StatusInactive {
			return LoginResult{}, errInactive
		}
		return LoginResult{}, errRoleDisabled
	}
	if needsRehash {
		s.rehash(ctx, acct.ID, in.Password)
	}

	// 账号已启用二次验证：账号密码已过，但先不发会话，返回一次性票据等第二步（LoginMFA）。
	// 未配置二次验证（mfaStore 为 nil）时不会有 MFAEnabled 账号，直接完成登录。
	if s.mfaStore != nil && acct.MFAEnabled {
		methods := mfaMethods(acct)
		if len(methods) == 0 {
			// 理论不至于：MFAEnabled 为真必有至少一个因子
			return LoginResult{}, fmt.Errorf("账号 %d 标记已启用二次验证但无可用因子", acct.ID)
		}
		ticket, err := randomToken()
		if err != nil {
			return LoginResult{}, err
		}
		if err := s.mfaStore.CreateLoginTicket(ctx, ticket, acct.ID, mfaTicketTTL); err != nil {
			return LoginResult{}, fmt.Errorf("暂存二次验证票据: %w", err)
		}
		return LoginResult{MFARequired: true, MFATicket: ticket, MFAMethods: methods}, nil
	}

	return s.completeLogin(ctx, acct, record, userKey)
}

// completeLogin 创建会话并记登录成功，供账号密码登录（无二次验证）与二次验证第二步共用。
func (s *Service) completeLogin(ctx context.Context, acct Account, record LoginLog, userKey string) (LoginResult, error) {
	token, err := randomToken()
	if err != nil {
		return LoginResult{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return LoginResult{}, err
	}
	key := sessionKey(token)
	sess := Session{AdminID: acct.ID, SessionVersion: acct.SessionVersion, CSRFToken: csrf, UADigest: record.UADigest, CreatedAt: s.now()}
	if err := s.sessions.Create(ctx, key, sess, SessionIdleTimeout); err != nil {
		return LoginResult{}, fmt.Errorf("创建后台会话: %w", err)
	}

	// 以下失败不影响本次登录，只记日志
	if err := s.accounts.MarkLogin(ctx, acct.ID); err != nil {
		slog.WarnContext(ctx, "记录管理员登录时间失败", "admin_id", acct.ID, "err", err)
	}
	if err := s.failures.Reset(ctx, userKey); err != nil {
		slog.WarnContext(ctx, "清除登录失败计数失败", "admin_id", acct.ID, "err", err)
	}
	// 刚登录（已过账号密码；MFA 账号还过了二次验证）视为最近重新验证，给高敏操作 5 分钟宽限；
	// 记下登录网段，之后请求换了网段即需重新验证（docs/10 第 6.3 节）
	if acct.MFAEnabled && s.mfaStore != nil {
		if err := s.mfaStore.MarkReauthed(ctx, key, record.IPPrefix, ReauthWindow); err != nil {
			slog.WarnContext(ctx, "标记登录后重新验证窗口失败", "admin_id", acct.ID, "err", err)
		}
	}
	s.writeLog(ctx, record, LoginSuccess, &acct.ID)

	return LoginResult{Token: token, Identity: identityOf(acct, sess, key)}, nil
}

// LoginMFA 是二次验证登录的第二步：用第一步发的票据 + 验证码换会话。
// 票据失效 / 账号已被停用返回 NEED_LOGIN；验证码错误返回 400 并计入登录失败锁定（防爆破 6 位码）。
func (s *Service) LoginMFA(ctx context.Context, in LoginMFAInput) (LoginResult, error) {
	if s.mfaStore == nil {
		return LoginResult{}, errMFATicketInvalid // 未启用二次验证
	}

	adminID, found, err := s.mfaStore.LookupLoginTicket(ctx, in.Ticket)
	if err != nil {
		return LoginResult{}, fmt.Errorf("读取二次验证票据: %w", err)
	}
	if !found {
		return LoginResult{}, errMFATicketInvalid
	}

	acct, err := s.accounts.GetByID(ctx, adminID)
	if errors.Is(err, errs.ErrNotFound) {
		s.dropTicket(ctx, in.Ticket)
		return LoginResult{}, errMFATicketInvalid
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("读取管理员: %w", err)
	}

	record := LoginLog{
		UsernameTried: printable(acct.Username, maxUsernameInLog),
		IPPrefix:      privacy.IPPrefix(in.ClientIP),
		UADigest:      privacy.UADigest(in.UserAgent),
	}
	userKey, ipKey := "u:"+acct.Username, "ip:"+limiterIP(in.ClientIP)

	// 账号 / 角色在发起登录后被停用
	if acct.Status == StatusInactive || !acct.Role.Active {
		s.writeLog(ctx, record, LoginInactive, &acct.ID)
		s.dropTicket(ctx, in.Ticket)
		if acct.Status == StatusInactive {
			return LoginResult{}, errInactive
		}
		return LoginResult{}, errRoleDisabled
	}

	// 复用登录失败锁定，给 6 位验证码限流
	wait, err := s.lockedFor(ctx, userKey, ipKey)
	if err != nil {
		return LoginResult{}, err
	}
	if wait > 0 {
		s.writeLog(ctx, record, LoginLocked, &acct.ID)
		return LoginResult{}, errs.WithRetryAfter(errLocked, wait)
	}

	// 按方式校验第二因子。失败只计数不消费票据，TTL 内可重试直到锁定。
	switch in.Method {
	case "totp":
		if s.mfa == nil {
			return LoginResult{}, errMFAMethod
		}
		valid, err := s.mfa.VerifyTOTP(ctx, acct.ID, in.Code)
		if err != nil {
			return LoginResult{}, fmt.Errorf("校验二次验证码: %w", err)
		}
		if !valid {
			return LoginResult{}, s.failMFA(ctx, record, &acct.ID, userKey, ipKey)
		}
	case "passkey":
		if s.passkey == nil {
			return LoginResult{}, errMFAMethod
		}
		err := s.passkey.FinishAssertion(ctx, acct.ID, PasskeyLoginKey(in.Ticket), in.Response)
		if errors.Is(err, errs.ErrBadRequest) {
			return LoginResult{}, s.failMFA(ctx, record, &acct.ID, userKey, ipKey)
		}
		if err != nil {
			return LoginResult{}, fmt.Errorf("校验 Passkey: %w", err)
		}
	default:
		return LoginResult{}, errMFAMethod
	}

	s.dropTicket(ctx, in.Ticket) // 一次性
	return s.completeLogin(ctx, acct, record, userKey)
}

// MFATicketAdmin 用登录二次验证票据换管理员 ID（不消费票据），供 Passkey 登录第二步先取回当前用户生成断言选项。
func (s *Service) MFATicketAdmin(ctx context.Context, ticket string) (int64, error) {
	if s.mfaStore == nil {
		return 0, errMFATicketInvalid
	}
	adminID, found, err := s.mfaStore.LookupLoginTicket(ctx, ticket)
	if err != nil {
		return 0, fmt.Errorf("读取二次验证票据: %w", err)
	}
	if !found {
		return 0, errMFATicketInvalid
	}
	return adminID, nil
}

// mfaMethods 返回账号可用的二次验证方式（供登录第二步选择）。
func mfaMethods(acct Account) []string {
	methods := make([]string, 0, 2)
	if acct.HasTOTP {
		methods = append(methods, "totp")
	}
	if acct.HasPasskey {
		methods = append(methods, "passkey")
	}
	return methods
}

// Reauth 为高敏操作重新验证二次因子（S2-6，docs/03 第 2.3 节）：校验当前管理员的 TOTP 验证码或 Passkey 断言，
// 通过后把本会话标记为「在当前 IP 网段最近已重新验证」（ReauthWindow 内、同网段有效）。
// 只对已绑定二次验证的账号有意义。id 来自已通过会话中间件的当前管理员（可信）；clientIP 为本次请求来源。
func (s *Service) Reauth(ctx context.Context, id Identity, method, code string, response []byte, clientIP string) error {
	if s.mfaStore == nil {
		return errReauthUnavailable
	}
	if !id.MFAEnabled {
		return errReauthNoFactor
	}

	// 复用登录失败锁定，给验证码 / 断言限流（防会话被劫持后爆破）
	userKey := "u:" + id.Username
	n, ttl, err := s.failures.Get(ctx, userKey)
	if err != nil {
		return fmt.Errorf("读取失败计数: %w", err)
	}
	if n >= maxUserFailures && ttl > 0 {
		return errs.WithRetryAfter(errLocked, ttl)
	}

	switch method {
	case "", "totp":
		if s.mfa == nil {
			return errReauthNoFactor
		}
		ok, err := s.mfa.VerifyTOTP(ctx, id.AdminID, code)
		if err != nil {
			return fmt.Errorf("校验二次验证码: %w", err)
		}
		if !ok {
			return s.failReauth(ctx, userKey)
		}
	case "passkey":
		if s.passkey == nil {
			return errReauthNoFactor
		}
		err := s.passkey.FinishAssertion(ctx, id.AdminID, PasskeyReauthKey(id.SessionKey), response)
		if errors.Is(err, errs.ErrBadRequest) {
			return s.failReauth(ctx, userKey)
		}
		if err != nil {
			return fmt.Errorf("校验 Passkey: %w", err)
		}
	default:
		return errMFAMethod
	}

	if err := s.failures.Reset(ctx, userKey); err != nil {
		slog.WarnContext(ctx, "清除失败计数失败", "admin_id", id.AdminID, "err", err)
	}
	if err := s.mfaStore.MarkReauthed(ctx, id.SessionKey, privacy.IPPrefix(clientIP), ReauthWindow); err != nil {
		return fmt.Errorf("记录重新验证: %w", err)
	}
	return nil
}

// Authenticate 用 Cookie 中的会话令牌换取当前管理员。会话过期、浏览器变化、账号或角色停用、
// 被重置密码或强制下线（会话版本变化）、账号删除时返回 NEED_LOGIN。
func (s *Service) Authenticate(ctx context.Context, token, userAgent string) (Identity, error) {
	if !validToken(token) {
		return Identity{}, errSessionInvalid
	}
	key := sessionKey(token)
	sess, err := s.sessions.GetAndTouch(ctx, key, SessionIdleTimeout)
	if errors.Is(err, errs.ErrNotFound) {
		return Identity{}, errSessionInvalid
	}
	if err != nil {
		return Identity{}, fmt.Errorf("读取后台会话: %w", err)
	}

	// 会话绑定 UA 摘要（docs/10 第 6.3 节）：Cookie 被拿到其他浏览器使用时作废
	if s.now().Sub(sess.CreatedAt) > SessionAbsoluteTimeout || sess.UADigest != privacy.UADigest(userAgent) {
		s.dropSession(ctx, key)
		return Identity{}, errSessionInvalid
	}

	// 每次都读账号与角色权限：停用、改角色、改权限立即生效，不需要逐个清理会话
	acct, err := s.accounts.GetByID(ctx, sess.AdminID)
	if errors.Is(err, errs.ErrNotFound) ||
		(err == nil && (acct.Status == StatusInactive || !acct.Role.Active || acct.SessionVersion != sess.SessionVersion)) {
		s.dropSession(ctx, key)
		return Identity{}, errSessionInvalid
	}
	if err != nil {
		return Identity{}, fmt.Errorf("读取管理员: %w", err)
	}
	return identityOf(acct, sess, key), nil
}

// Logout 吊销当前会话。
func (s *Service) Logout(ctx context.Context, sessionKey string) error {
	if err := s.sessions.Delete(ctx, sessionKey); err != nil {
		return fmt.Errorf("删除后台会话: %w", err)
	}
	return nil
}

// lockedFor 返回还需等待多久才能再次尝试；未锁定时返回 0。
func (s *Service) lockedFor(ctx context.Context, userKey, ipKey string) (time.Duration, error) {
	var wait time.Duration
	for _, c := range []struct {
		key string
		max int64
	}{{userKey, maxUserFailures}, {ipKey, maxIPFailures}} {
		n, ttl, err := s.failures.Get(ctx, c.key)
		if err != nil {
			return 0, fmt.Errorf("读取登录失败计数: %w", err)
		}
		if n >= c.max && ttl > wait {
			wait = ttl
		}
	}
	return wait, nil
}

// fail 累计失败次数、写登录记录，并返回统一的「用户名或密码错误」。
func (s *Service) fail(ctx context.Context, record LoginLog, adminID *int64, keys ...string) error {
	ctx, cancel := detached(ctx)
	defer cancel()
	for _, k := range keys {
		if _, err := s.failures.Incr(ctx, k, failureWindow); err != nil {
			return fmt.Errorf("记录登录失败: %w", err)
		}
	}
	s.writeLog(ctx, record, LoginBadPassword, adminID)
	return errBadCredentials
}

// failMFA 累计失败次数、记 mfa_failed，并返回「验证码不正确」。与 fail 一样不随请求取消。
func (s *Service) failMFA(ctx context.Context, record LoginLog, adminID *int64, keys ...string) error {
	ctx, cancel := detached(ctx)
	defer cancel()
	for _, k := range keys {
		if _, err := s.failures.Incr(ctx, k, failureWindow); err != nil {
			return fmt.Errorf("记录二次验证失败: %w", err)
		}
	}
	s.writeLog(ctx, record, LoginMFAFailed, adminID)
	return errMFACode
}

// failReauth 累计失败次数并返回「验证码不正确」。与 fail 一样不随请求取消。
func (s *Service) failReauth(ctx context.Context, userKey string) error {
	ctx, cancel := detached(ctx)
	defer cancel()
	if _, err := s.failures.Incr(ctx, userKey, failureWindow); err != nil {
		return fmt.Errorf("记录重新验证失败: %w", err)
	}
	return errReauthCode
}

// dropTicket 删除登录二次验证票据；失败只记日志。
func (s *Service) dropTicket(ctx context.Context, ticket string) {
	if err := s.mfaStore.DeleteLoginTicket(ctx, ticket); err != nil {
		slog.WarnContext(ctx, "删除二次验证票据失败", "err", err)
	}
}

// writeLog 写登录记录；写失败只记日志，不影响登录结果。
func (s *Service) writeLog(ctx context.Context, record LoginLog, result LoginOutcome, adminID *int64) {
	ctx, cancel := detached(ctx)
	defer cancel()
	record.Result, record.AdminID = result, adminID
	if err := s.accounts.InsertLoginLog(ctx, record); err != nil {
		slog.WarnContext(ctx, "写入后台登录记录失败", "result", result, "err", err)
	}
}

// detached 用于失败计数与登录记录：客户端中途断开也要记账，所以不随请求取消，另设超时。
func detached(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), accountingTimeout)
}

// rehash 在参数调整后用新参数重新保存密码哈希；失败不影响登录。
func (s *Service) rehash(ctx context.Context, id int64, plain string) {
	hash, err := s.hasher.Hash(ctx, plain)
	if err == nil {
		err = s.accounts.UpdatePasswordHash(ctx, id, hash)
	}
	if err != nil {
		slog.WarnContext(ctx, "管理员密码重新哈希失败", "admin_id", id, "err", err)
	}
}

func (s *Service) dropSession(ctx context.Context, key string) {
	if err := s.sessions.Delete(ctx, key); err != nil {
		slog.WarnContext(ctx, "删除失效的后台会话失败", "err", err)
	}
}

func identityOf(a Account, s Session, key string) Identity {
	id := NewIdentity(a.ID, a.Username, a.Role, a.Permissions)
	id.MFAEnabled, id.CSRFToken, id.SessionKey = a.MFAEnabled, s.CSRFToken, key
	return id
}

// randomToken 生成 256 bit 随机串（base64url 无填充，43 个字符）。
func randomToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成随机令牌: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// validToken 只接受 randomToken 生成的格式，避免把任意长度的 Cookie 值拿去计算与查询。
func validToken(t string) bool {
	if len(t) != base64.RawURLEncoding.EncodedLen(tokenBytes) {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(t)
	return err == nil
}

// sessionKey 是会话在 Valkey 中的键：令牌的 SHA-256。Valkey 数据泄露时拿不到可用的 Cookie。
func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// limiterIP 是按 IP 计数用的键：IPv4 用完整地址，IPv6 用所在 /64（一个用户通常持有整个 /64，换地址不能绕过计数）。
func limiterIP(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap().WithZone("")
	if addr.Is4() {
		return addr.String()
	}
	p, err := addr.Prefix(64)
	if err != nil {
		return addr.String()
	}
	return p.String()
}

// printable 去掉控制字符（含 NUL，PostgreSQL text 不接受）并按字符截断，用于保存用户输入的用户名。
func printable(s string, maxRunes int) string {
	out := make([]rune, 0, min(len(s), maxRunes))
	for _, r := range strings.ToValidUTF8(s, "\uFFFD") {
		if len(out) == maxRunes {
			break
		}
		if !unicode.IsControl(r) {
			out = append(out, r)
		}
	}
	return string(out)
}
