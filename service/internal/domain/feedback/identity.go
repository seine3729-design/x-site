package feedback

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"xsite/service/internal/errs"
)

// SessionStore 保存社区用户会话（由 store/cache 实现）。
type SessionStore interface {
	Create(ctx context.Context, key string, s Session, ttl time.Duration) error
	// GetAndTouch 读取会话并把空闲过期时间重置为 ttl（滑动过期）。
	GetAndTouch(ctx context.Context, key string, ttl time.Duration) (Session, error)
	Delete(ctx context.Context, key string) error
}

// Session 是社区用户会话。
type Session struct {
	UserID         int64
	SessionVersion int32
	CreatedAt      time.Time
}

// FailureCounter 记录登录失败次数。
type FailureCounter interface {
	Get(ctx context.Context, key string) (int64, time.Duration, error)
	Incr(ctx context.Context, key string, window time.Duration) (int64, error)
	Reset(ctx context.Context, key string) error
}

// PasswordHasher 由 internal/crypto/password 实现。
type PasswordHasher interface {
	Hash(ctx context.Context, plain string) (string, error)
	Verify(ctx context.Context, plain, encoded string) (ok, needsRehash bool, err error)
	// Equalize 在账号不存在时也消耗一次哈希时间，让响应耗时相近（防用户名枚举）。
	Equalize(ctx context.Context, plain string)
}

const (
	// SessionTTL 是会话空闲过期时间（滑动）。
	SessionTTL = 30 * 24 * time.Hour
	// 登录失败阈值与窗口：按用户名计数，超过后要求稍后重试。
	maxLoginFailures = 8
	loginFailWindow  = 15 * time.Minute
)

// IdentityService 负责注册、登录、会话与账号安全（feedback/docs/adr/0011）。
//
// 这里**没有任何账号找回途径**，是刻意的：不收邮箱和手机号，也不发恢复码。
// 用户忘记密码就永久失去账号，运营也没有手段代为重置（adr/0011 已记录这个决定
// 与它的代价）。改动这块之前先读那份 ADR。
type IdentityService struct {
	repo     Repo
	sessions SessionStore
	failures FailureCounter
	hasher   PasswordHasher
	now      func() time.Time
}

func NewIdentityService(repo Repo, sessions SessionStore, failures FailureCounter, hasher PasswordHasher) *IdentityService {
	return &IdentityService{repo: repo, sessions: sessions, failures: failures, hasher: hasher, now: time.Now}
}

// 统一的登录失败提示：不区分「用户名不存在」与「密码错误」，防止枚举账号。
var errBadCredentials = errs.ErrNeedLogin.WithMessage("用户名或密码不正确")

// Register 用邀请码注册：校验邀请码 → 建用户 → 建会话。
func (s *IdentityService) Register(ctx context.Context, in RegisterInput) (AuthResult, error) {
	handle, err := validateHandle(in.Handle)
	if err != nil {
		return AuthResult{}, err
	}
	if err := validatePassword(in.Password); err != nil {
		return AuthResult{}, err
	}
	nickname, err := validateNickname(in.Nickname)
	if err != nil {
		return AuthResult{}, err
	}
	code := strings.TrimSpace(in.InviteCode)
	if code == "" {
		return AuthResult{}, badRequest("请填写邀请码")
	}

	pwHash, err := s.hasher.Hash(ctx, in.Password)
	if err != nil {
		return AuthResult{}, fmt.Errorf("计算密码哈希: %w", err)
	}

	now := s.now()
	user, err := s.repo.Register(ctx, NewUser{
		Handle:       handle,
		Nickname:     nickname,
		PasswordHash: pwHash,
		InviteHash:   HashInviteCode(code),
	}, func(inv Invite) error {
		if ok, reason := inv.Usable(now); !ok {
			return badRequest("%s", reason)
		}
		return nil
	})
	if err != nil {
		return AuthResult{}, err
	}

	token, err := s.issue(ctx, user)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: user, Token: token, ExpiresIn: int64(SessionTTL / time.Second)}, nil
}

// Login 用用户名与密码登录。
func (s *IdentityService) Login(ctx context.Context, in LoginInput) (AuthResult, error) {
	handle := strings.TrimSpace(in.Handle)
	if handle == "" || in.Password == "" {
		return AuthResult{}, errBadCredentials
	}
	key := strings.ToLower(handle)
	if n, left, err := s.failures.Get(ctx, key); err == nil && n >= maxLoginFailures {
		if left <= 0 {
			left = time.Minute
		}
		return AuthResult{}, errs.WithRetryAfter(errs.ErrRateLimited.WithMessage("登录失败次数过多，请稍后再试"), left)
	}

	user, err := s.repo.UserByHandle(ctx, handle)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			// 账号不存在时也消耗一次哈希时间，让两种失败耗时相近
			s.hasher.Equalize(ctx, in.Password)
			_, _ = s.failures.Incr(ctx, key, loginFailWindow)
			return AuthResult{}, errBadCredentials
		}
		return AuthResult{}, err
	}

	ok, needsRehash, err := s.hasher.Verify(ctx, in.Password, user.PasswordHash)
	if err != nil {
		return AuthResult{}, fmt.Errorf("校验密码: %w", err)
	}
	if !ok {
		_, _ = s.failures.Incr(ctx, key, loginFailWindow)
		return AuthResult{}, errBadCredentials
	}
	if user.Status == UserSuspended {
		return AuthResult{}, errs.ErrForbidden.WithMessage("账号已被封禁，如有疑问请联系管理员")
	}
	_ = s.failures.Reset(ctx, key)

	if needsRehash {
		if h, err := s.hasher.Hash(ctx, in.Password); err == nil {
			if u, err := s.repo.UpdateSecret(ctx, user.ID, h); err == nil {
				user = u
			}
		}
	}

	token, err := s.issue(ctx, user)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: user, Token: token, ExpiresIn: int64(SessionTTL / time.Second)}, nil
}

// Authenticate 用会话令牌换取当前用户；令牌失效或账号被封禁时返回 401。
func (s *IdentityService) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, errs.ErrNeedLogin
	}
	sess, err := s.sessions.GetAndTouch(ctx, sessionKey(token), SessionTTL)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			return User{}, errs.ErrNeedLogin
		}
		return User{}, err
	}
	user, err := s.repo.User(ctx, sess.UserID)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			_ = s.sessions.Delete(ctx, sessionKey(token))
			return User{}, errs.ErrNeedLogin
		}
		return User{}, err
	}
	// 改密、重置、封禁都会递增 session_version，让旧令牌立即失效
	if user.SessionVersion != sess.SessionVersion || user.Status == UserSuspended {
		_ = s.sessions.Delete(ctx, sessionKey(token))
		return User{}, errs.ErrTokenExpired.WithMessage("登录状态已失效，请重新登录")
	}
	return user, nil
}

// Logout 删除当前会话。
func (s *IdentityService) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.Delete(ctx, sessionKey(token))
}

// ChangePassword 用当前密码改密：成功后让其他会话失效。
//
// 这是**唯一**的改密路径，而且必须提供正确的当前密码。没有「忘记密码」这条路
// （adr/0011）：忘记密码的账号无法恢复，运营也不能代为重置。
// identity_test.go 的 TestNoPasswordResetWithoutOldPassword 守着这件事。
func (s *IdentityService) ChangePassword(ctx context.Context, userID int64, in ChangePasswordInput) (AuthResult, error) {
	if err := validatePassword(in.NewPassword); err != nil {
		return AuthResult{}, err
	}
	user, err := s.repo.User(ctx, userID)
	if err != nil {
		return AuthResult{}, err
	}
	ok, _, err := s.hasher.Verify(ctx, in.OldPassword, user.PasswordHash)
	if err != nil {
		return AuthResult{}, fmt.Errorf("校验密码: %w", err)
	}
	if !ok {
		return AuthResult{}, errs.ErrBadRequest.WithMessage("当前密码不正确")
	}
	pwHash, err := s.hasher.Hash(ctx, in.NewPassword)
	if err != nil {
		return AuthResult{}, fmt.Errorf("计算密码哈希: %w", err)
	}
	updated, err := s.repo.UpdateSecret(ctx, user.ID, pwHash)
	if err != nil {
		return AuthResult{}, err
	}
	token, err := s.issue(ctx, updated)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: updated, Token: token, ExpiresIn: int64(SessionTTL / time.Second)}, nil
}

// UpdateProfile 修改公开资料。
func (s *IdentityService) UpdateProfile(ctx context.Context, userID int64, in ProfileInput) (User, error) {
	nickname, err := validateNickname(in.Nickname)
	if err != nil {
		return User{}, err
	}
	in.Nickname = nickname
	in.Bio = cleanBody(in.Bio)
	if runeLen(in.Bio) > 300 {
		return User{}, badRequest("个人简介最多 300 个字")
	}
	return s.repo.UpdateProfile(ctx, userID, in)
}

// issue 生成会话令牌并写入会话存储。
func (s *IdentityService) issue(ctx context.Context, u User) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成会话令牌: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sess := Session{UserID: u.ID, SessionVersion: u.SessionVersion, CreatedAt: s.now()}
	if err := s.sessions.Create(ctx, sessionKey(token), sess, SessionTTL); err != nil {
		return "", err
	}
	return token, nil
}

// sessionKey 用令牌摘要作为存储键，Valkey 中不出现原始令牌。
func sessionKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// HashInviteCode 返回邀请码的存储哈希。邀请码是高熵随机串，用 SHA-256 即可（无需加盐慢哈希）。
func HashInviteCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeInviteCode(code)))
	return hex.EncodeToString(sum[:])
}

// normalizeInviteCode 统一邀请码大小写与分隔符。
func normalizeInviteCode(s string) string {
	r := strings.NewReplacer("-", "", " ", "", "\t", "")
	return strings.ToUpper(r.Replace(strings.TrimSpace(s)))
}

// 邀请码用 base32（去掉填充），分 3 组展示，便于口头转述与手抄。
//
// 这个编码器原来叫 recoveryEnc、由恢复码和邀请码共用。恢复码删掉之后
// （adr/0011）只剩邀请码在用，所以跟着改名归位 —— 留着 recovery 的名字
// 会让人以为恢复码还在。
var inviteEnc = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewInviteCode 生成邀请码明文（12 位 base32，分 3 组展示）。
func NewInviteCode() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("生成邀请码: %w", err)
	}
	enc := inviteEnc.EncodeToString(raw)[:12]
	return enc[:4] + "-" + enc[4:8] + "-" + enc[8:12], nil
}
