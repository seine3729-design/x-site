// Package user 是用户端注册用户域（docs/03 第 2 节，S2-1）：注册、登录、改密。
//
// 令牌与会话族复用 identity 域：Access Token 为无状态 JWT（15 分钟），Refresh Token 轮换并做复用检测、
// 注册用户 30 天滑动有效。账号只有用户名 + 密码，不收集邮箱 / 手机号，也不提供找回密码（adr/0001）：
// 忘记密码只能重新注册，原用户名不释放、数据不迁移。
package user

import (
	"context"
	"errors"
	"fmt"
	"time"

	"xsite/service/internal/domain/identity"
	"xsite/service/internal/errs"
)

// Status 是账号状态（docs/02 第 5.1 节）。
type Status string

const (
	StatusNormal Status = "normal"
	StatusMuted  Status = "muted"  // 禁止发评论 / 弹幕
	StatusBanned Status = "banned" // 封禁，不能登录
)

// User 是注册用户。PasswordHash 只在领域层内部使用，不出现在任何对外 DTO。
type User struct {
	ID           int64
	Username     string
	Nickname     string
	Avatar       string
	Bio          string
	Status       Status
	Locale       string
	CreatedAt    time.Time
	PasswordHash string
}

// DisplayName 返回昵称，没有昵称时退回用户名。
func (u User) DisplayName() string {
	if u.Nickname != "" {
		return u.Nickname
	}
	return u.Username
}

// NewUser 是要创建的用户。
type NewUser struct {
	Username     string
	Nickname     string
	PasswordHash string
}

// Repo 读写 user 表（实现见 internal/store/pg）。
type Repo interface {
	// Create 建新用户；用户名已存在返回 errs.ErrConflict。
	Create(ctx context.Context, u NewUser) (User, error)
	ByUsername(ctx context.Context, username string) (User, error)
	ByID(ctx context.Context, id int64) (User, error)
	UpdatePassword(ctx context.Context, id int64, passwordHash string) error

	// 资料（见 profile.go）。
	GetProfile(ctx context.Context, id int64) (Profile, error)
	UpdateProfile(ctx context.Context, id int64, f ProfileFields) (Profile, error)
}

// Hasher 由 internal/crypto/password 实现。
type Hasher interface {
	Hash(ctx context.Context, plain string) (string, error)
	Verify(ctx context.Context, plain, encoded string) (ok, needsRehash bool, err error)
	// Equalize 在账号不存在时也消耗一次哈希时间，使响应耗时相近（防用户名枚举）。
	Equalize(ctx context.Context, plain string)
}

// Issuer 由 identity.Service 实现：为注册用户签发令牌、吊销其全部会话。
type Issuer interface {
	IssueForUser(ctx context.Context, userID int64, in identity.IssueInput) (identity.Tokens, error)
	RevokeAllSessions(ctx context.Context, userID int64, reason string) error
}

// FailureCounter 记录并读取登录失败次数（cache.Counter 实现）。
type FailureCounter interface {
	Get(ctx context.Context, key string) (int64, time.Duration, error)
	Incr(ctx context.Context, key string, window time.Duration) (int64, error)
	Reset(ctx context.Context, key string) error
}

const (
	// userFailThreshold / ipFailThreshold：同一用户名 / 同一 IP 前缀连续失败到此次数后，登录要求通过人机验证
	// （docs/03 第 2.2 节）。IP 维度阈值更高，避免 NAT 下多个真人共用出口 IP 被频繁要求挑战。
	userFailThreshold = 5
	ipFailThreshold   = 20
	// failWindow 是失败计数的滑动窗口（最后一次失败后到期清零）。
	failWindow = 15 * time.Minute
)

// Service 是注册用户域服务。
type Service struct {
	repo      Repo
	issuer    Issuer
	hasher    Hasher
	failures  FailureCounter
	challenge ChallengeGuard
}

func NewService(repo Repo, issuer Issuer, hasher Hasher, failures FailureCounter, challenge ChallengeGuard) *Service {
	return &Service{repo: repo, issuer: issuer, hasher: hasher, failures: failures, challenge: challenge}
}

// RegisterInput 是注册请求。
type RegisterInput struct {
	Username      string
	Password      string
	Nickname      string
	AckNoRecovery bool // 必须为 true：用户确认知悉忘记密码无法找回（adr/0001）
}

// LoginInput 是登录请求。
type LoginInput struct {
	Username  string
	Password  string
	Challenge *ChallengeAnswer // 失败过多时要求人机验证；平时为 nil
}

// ChangePasswordInput 是用当前密码改密。
type ChangePasswordInput struct {
	OldPassword string
	NewPassword string
}

// AuthResult 是注册 / 登录成功后的结果。明文令牌只在这里返回给客户端。
type AuthResult struct {
	User   User
	Tokens identity.Tokens
}

// errBadCredentials 是统一的登录失败提示：不区分「用户名不存在」与「密码错误」，防止枚举账号。
var errBadCredentials = errs.ErrNeedLogin.WithMessage("用户名或密码不正确")

// Register 校验输入、建用户并签发令牌。用户名已存在返回 409 CONFLICT。
func (s *Service) Register(ctx context.Context, in RegisterInput, device identity.IssueInput) (AuthResult, error) {
	if !in.AckNoRecovery {
		return AuthResult{}, errs.ErrBadRequest.WithMessage("请先确认知悉：忘记密码将无法找回")
	}
	username, err := validateUsername(in.Username)
	if err != nil {
		return AuthResult{}, err
	}
	if err := validatePassword(in.Password, username); err != nil {
		return AuthResult{}, err
	}
	nickname, err := validateNickname(in.Nickname)
	if err != nil {
		return AuthResult{}, err
	}
	hash, err := s.hasher.Hash(ctx, in.Password)
	if err != nil {
		return AuthResult{}, fmt.Errorf("计算密码哈希: %w", err)
	}
	u, err := s.repo.Create(ctx, NewUser{Username: username, Nickname: nickname, PasswordHash: hash})
	if err != nil {
		if errors.Is(err, errs.ErrConflict) {
			return AuthResult{}, errs.ErrConflict.WithMessage("用户名已被占用")
		}
		return AuthResult{}, err
	}
	tokens, err := s.issuer.IssueForUser(ctx, u.ID, device)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{User: u, Tokens: tokens}, nil
}

// Login 校验用户名密码并签发令牌。用户名不存在与密码错误返回相同提示与相近耗时（防枚举）；
// 同一用户名 / IP 连续失败过多时要求通过人机验证（docs/03 第 2.2 节），通过挑战后仍可继续尝试、不锁定。
func (s *Service) Login(ctx context.Context, in LoginInput, device identity.IssueInput) (AuthResult, error) {
	username := normalizeUsername(in.Username)
	if username == "" || in.Password == "" {
		return AuthResult{}, errBadCredentials
	}
	userKey := "u:" + username
	ipKey := ""
	if device.IPPrefix != "" {
		ipKey = "ip:" + device.IPPrefix
	}

	// 失败过多先要求人机验证；通过了才继续校验密码
	if need, err := s.needChallenge(ctx, userKey, ipKey); err != nil {
		return AuthResult{}, err
	} else if need && !s.passedChallenge(ctx, in.Challenge) {
		return AuthResult{}, errs.ErrChallengeRequired
	}

	u, err := s.repo.ByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, errs.ErrNotFound) {
			// 账号不存在时也消耗一次哈希时间，让两种失败耗时相近
			s.hasher.Equalize(ctx, in.Password)
			s.recordFail(ctx, userKey, ipKey)
			return AuthResult{}, errBadCredentials
		}
		return AuthResult{}, err
	}
	ok, needsRehash, err := s.hasher.Verify(ctx, in.Password, u.PasswordHash)
	if err != nil {
		return AuthResult{}, fmt.Errorf("校验密码: %w", err)
	}
	if !ok {
		s.recordFail(ctx, userKey, ipKey)
		return AuthResult{}, errBadCredentials
	}
	if u.Status == StatusBanned {
		return AuthResult{}, errs.ErrForbidden.WithMessage("账号已被封禁，如有疑问请联系客服")
	}
	if needsRehash {
		// 参数过旧时透明重哈希；失败不影响本次登录
		if h, err := s.hasher.Hash(ctx, in.Password); err == nil {
			_ = s.repo.UpdatePassword(ctx, u.ID, h)
		}
	}
	tokens, err := s.issuer.IssueForUser(ctx, u.ID, device)
	if err != nil {
		return AuthResult{}, err
	}
	if s.failures != nil {
		_ = s.failures.Reset(ctx, userKey) // 成功清用户名失败计数（IP 计数自然过期）
	}
	return AuthResult{User: u, Tokens: tokens}, nil
}

// needChallenge 判断是否要求人机验证：用户名或 IP 失败计数达阈值。计数器故障时放行（不因 Valkey 抖动误挡所有人）。
func (s *Service) needChallenge(ctx context.Context, userKey, ipKey string) (bool, error) {
	if s.failures == nil {
		return false, nil
	}
	if n, _, err := s.failures.Get(ctx, userKey); err == nil && n >= userFailThreshold {
		return true, nil
	}
	if ipKey != "" {
		if n, _, err := s.failures.Get(ctx, ipKey); err == nil && n >= ipFailThreshold {
			return true, nil
		}
	}
	return false, nil
}

// passedChallenge 校验客户端提交的人机验证答案。
func (s *Service) passedChallenge(ctx context.Context, answer *ChallengeAnswer) bool {
	if answer == nil || s.challenge == nil {
		return false
	}
	ok, err := s.challenge.Verify(ctx, *answer)
	return err == nil && ok
}

// recordFail 记一次登录失败（用户名 + IP 两个维度）。
func (s *Service) recordFail(ctx context.Context, userKey, ipKey string) {
	if s.failures == nil {
		return
	}
	_, _ = s.failures.Incr(ctx, userKey, failWindow)
	if ipKey != "" {
		_, _ = s.failures.Incr(ctx, ipKey, failWindow)
	}
}

// IssueChallenge 签发一个人机验证挑战（GET /api/v1/auth/challenge）。
func (s *Service) IssueChallenge(ctx context.Context) (Challenge, error) {
	if s.challenge == nil {
		return Challenge{}, errs.ErrInternal
	}
	return s.challenge.Issue(ctx)
}

// ChangePassword 用当前密码改密：成功后吊销该用户的全部会话（docs/03 第 2.2 节：改密吊销全部会话）。
// Access Token 无状态，吊销只让 Refresh 族立即作废；已签发的 Access 最多再活 15 分钟。
func (s *Service) ChangePassword(ctx context.Context, userID int64, in ChangePasswordInput) error {
	if err := validatePassword(in.NewPassword, ""); err != nil {
		return err
	}
	u, err := s.repo.ByID(ctx, userID)
	if err != nil {
		return err
	}
	ok, _, err := s.hasher.Verify(ctx, in.OldPassword, u.PasswordHash)
	if err != nil {
		return fmt.Errorf("校验密码: %w", err)
	}
	if !ok {
		return errs.ErrBadRequest.WithMessage("当前密码不正确")
	}
	if in.NewPassword == in.OldPassword {
		return errs.ErrBadRequest.WithMessage("新密码不能与当前密码相同")
	}
	hash, err := s.hasher.Hash(ctx, in.NewPassword)
	if err != nil {
		return fmt.Errorf("计算密码哈希: %w", err)
	}
	if err := s.repo.UpdatePassword(ctx, userID, hash); err != nil {
		return err
	}
	return s.issuer.RevokeAllSessions(ctx, userID, "password_changed")
}
