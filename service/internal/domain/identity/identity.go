// Package identity 是用户端身份域（docs/03 第 2 节，S1-10）：游客身份签发、令牌轮换与复用检测。
//
// Access Token 是无状态 JWT（authtoken，EdDSA，15 分钟），校验只验签不查库；短有效期限制了泄露窗口。
// Refresh Token 是 256 bit 随机串，服务端只存 SHA-256（user_session.refresh_hash）；每次刷新轮换：
// 新建会话行、吊销旧行；旧令牌再被使用（命中已吊销的行）即判定泄露，吊销整个会话族（docs/02 第 5.1.1 节）。
// 注册用户登录（S2-1）会复用同一套令牌与会话族，届时 role=user、sub=user_id。
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"uuid"

	"xsite/service/internal/crypto/authtoken"
	"xsite/service/internal/errs"
)

const (
	// AccessTTL 是 Access Token 有效期（docs/10 第 4 节）。
	AccessTTL = 15 * time.Minute
	// GuestRefreshTTL 是游客 Refresh Token 有效期（docs/10 第 4 节：游客令牌 180 天）。
	GuestRefreshTTL = 180 * 24 * time.Hour
	// UserRefreshTTL 是注册用户 Refresh Token 的滑动有效期（docs/10 第 4 节、docs/03 第 2.2 节：30 天）。
	UserRefreshTTL = 30 * 24 * time.Hour
)

// validPlatform 限定来源平台（与迁移的 CHECK 一致）。
func validPlatform(p string) bool { return p == "web" || p == "ios" || p == "android" }

// Tokens 是一次签发或刷新的结果：明文令牌只在这里返回给客户端，不落库也不入日志。
type Tokens struct {
	AccessToken  string
	RefreshToken string
	DeviceID     string // 游客设备 ID（guest_device.id）；供客户端作为后续请求的设备标识
	Role         authtoken.Role
	ExpiresIn    int64 // Access Token 有效秒数
}

// Access 是校验 Access Token 得到的身份（无状态，不查库）。
type Access struct {
	Subject   string // 游客为 guest_device_id，注册用户为 user_id
	SessionID string
	Role      authtoken.Role
}

// IsUser 表示这是注册用户的身份（而非游客）。
func (a Access) IsUser() bool { return a.Role == authtoken.RoleUser }

// UserID 把 Subject 解析为注册用户 ID；非注册用户或解析失败返回 0。
func (a Access) UserID() int64 {
	if a.Role != authtoken.RoleUser {
		return 0
	}
	id, _ := strconv.ParseInt(a.Subject, 10, 64)
	return id
}

// SessionInfo 是「我的设备」列表中的一个会话（docs/03 第 2.2 节）。只含可展示摘要，不含令牌。
type SessionInfo struct {
	ID         uuid.UUID
	UADigest   string
	IPPrefix   string
	Platform   string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// ---------- 仓储数据结构 ----------

// GuestDevice 是要创建的游客设备。
type GuestDevice struct {
	ID            uuid.UUID
	SourceChannel string
	Platform      string
	AttestLevel   string
}

// NewSession 是要插入的新会话行。UserID 为 0 表示游客（用 GuestDeviceID）。
type NewSession struct {
	ID            uuid.UUID
	FamilyID      uuid.UUID
	GuestDeviceID uuid.UUID
	UserID        int64
	RefreshHash   []byte
	UADigest      string
	IPPrefix      string
	Platform      string
	ExpiresAt     time.Time
}

// RotateInput 是轮换时新会话行所需、无法从旧行推出的字段（族与主体由旧行继承）。
type RotateInput struct {
	NewSessionID   uuid.UUID
	NewRefreshHash []byte
	// 新行过期时间按旧行主体选：游客续 GuestExpiresAt，注册用户续 UserExpiresAt（两者滑动 TTL 不同）。
	GuestExpiresAt time.Time
	UserExpiresAt  time.Time
	UADigest       string
	IPPrefix       string
}

// RotateStatus 是一次轮换的结果分类。
type RotateStatus int

const (
	RotateOK       RotateStatus = iota // 轮换成功
	RotateNotFound                     // 刷新令牌不存在
	RotateExpired                      // 会话已过期
	RotateReuse                        // 旧令牌复用（已吊销整族）
)

// RotateResult 是轮换成功时旧行的主体信息（用于签发新 Access Token）。
type RotateResult struct {
	Status        RotateStatus
	GuestDeviceID uuid.UUID
	UserID        int64
	Platform      string
}

// SessionRepo 读写游客设备与会话族（实现见 internal/store/pg）。
type SessionRepo interface {
	CreateGuest(ctx context.Context, d GuestDevice) error
	CreateSession(ctx context.Context, s NewSession) error
	// Rotate 在一个事务内：按旧刷新令牌哈希锁住会话行；已吊销 → 吊销整族并返回 RotateReuse；
	// 过期 → RotateExpired；不存在 → RotateNotFound；否则吊销旧行、插入新行（同族同主体）并返回 RotateOK。
	Rotate(ctx context.Context, oldHash []byte, in RotateInput, now time.Time) (RotateResult, error)
	// RevokeFamilyByRefresh 按刷新令牌吊销其所在会话族（登出）。
	RevokeFamilyByRefresh(ctx context.Context, hash []byte, reason string) error
	// ListUserSessions 返回注册用户所有未吊销且未过期的会话（「我的设备」，按最近活跃倒序）。
	ListUserSessions(ctx context.Context, userID int64, now time.Time) ([]SessionInfo, error)
	// RevokeSessionByID 吊销该用户名下的指定会话（设备下线）。返回受影响行数：0 表示会话不存在或不属于该用户。
	RevokeSessionByID(ctx context.Context, userID int64, sessionID uuid.UUID, reason string) (int64, error)
	// RevokeAllForUser 吊销该用户的全部会话（改密 / 封禁）。
	RevokeAllForUser(ctx context.Context, userID int64, reason string) error
}

// Service 是身份服务。
type Service struct {
	repo SessionRepo
	keys *authtoken.Keyring
	now  func() time.Time
}

func NewService(repo SessionRepo, keys *authtoken.Keyring) *Service {
	return &Service{repo: repo, keys: keys, now: time.Now}
}

// IssueInput / RefreshInput 是请求参数；UADigest / IPPrefix 由 HTTP 层从请求算出（用于「我的设备」展示，不参与鉴权）。
type IssueInput struct {
	Platform      string
	SourceChannel string
	UADigest      string
	IPPrefix      string
}

type RefreshInput struct {
	RefreshToken string
	UADigest     string
	IPPrefix     string
}

// errExpiredSession 是刷新失败的统一提示：不存在、过期、复用都返回它，不向调用方泄露是哪一种（尤其是复用）。
var errExpiredSession = errs.ErrTokenExpired.WithMessage("登录状态已失效，请重新获取")

// IssueGuest 新建游客设备、开一个会话族并签发 Access/Refresh 令牌。
func (s *Service) IssueGuest(ctx context.Context, in IssueInput) (Tokens, error) {
	platform := in.Platform
	if !validPlatform(platform) {
		platform = "web"
	}
	device := uuid.NewV7()
	if err := s.repo.CreateGuest(ctx, GuestDevice{ID: device, SourceChannel: in.SourceChannel, Platform: platform, AttestLevel: "none"}); err != nil {
		return Tokens{}, err
	}
	refresh, hash, err := newRefresh()
	if err != nil {
		return Tokens{}, err
	}
	now := s.now()
	sid := uuid.NewV7()
	if err := s.repo.CreateSession(ctx, NewSession{
		ID: sid, FamilyID: uuid.NewV7(), GuestDeviceID: device, RefreshHash: hash,
		UADigest: in.UADigest, IPPrefix: in.IPPrefix, Platform: platform, ExpiresAt: now.Add(GuestRefreshTTL),
	}); err != nil {
		return Tokens{}, err
	}
	return s.sign(device.String(), sid.String(), authtoken.RoleGuest, device.String(), refresh, now)
}

// IssueForUser 为注册用户新开一个会话族并签发 Access/Refresh 令牌（登录 / 注册后调用，S2-1）。
// 注册用户没有游客设备 ID，sub=user_id、role=user，Refresh 用 UserRefreshTTL（30 天）。
func (s *Service) IssueForUser(ctx context.Context, userID int64, in IssueInput) (Tokens, error) {
	if userID <= 0 {
		return Tokens{}, fmt.Errorf("identity: 无效用户 ID %d", userID)
	}
	platform := in.Platform
	if !validPlatform(platform) {
		platform = "web"
	}
	refresh, hash, err := newRefresh()
	if err != nil {
		return Tokens{}, err
	}
	now := s.now()
	sid := uuid.NewV7()
	if err := s.repo.CreateSession(ctx, NewSession{
		ID: sid, FamilyID: uuid.NewV7(), UserID: userID, RefreshHash: hash,
		UADigest: in.UADigest, IPPrefix: in.IPPrefix, Platform: platform, ExpiresAt: now.Add(UserRefreshTTL),
	}); err != nil {
		return Tokens{}, err
	}
	return s.sign(strconv.FormatInt(userID, 10), sid.String(), authtoken.RoleUser, "", refresh, now)
}

// Refresh 校验并轮换刷新令牌，复用时吊销会话族。不存在 / 过期 / 复用一律返回同一提示。
func (s *Service) Refresh(ctx context.Context, in RefreshInput) (Tokens, error) {
	if in.RefreshToken == "" {
		return Tokens{}, errExpiredSession
	}
	refresh, newHash, err := newRefresh()
	if err != nil {
		return Tokens{}, err
	}
	now := s.now()
	sid := uuid.NewV7()
	res, err := s.repo.Rotate(ctx, hashToken(in.RefreshToken), RotateInput{
		NewSessionID: sid, NewRefreshHash: newHash,
		GuestExpiresAt: now.Add(GuestRefreshTTL), UserExpiresAt: now.Add(UserRefreshTTL),
		UADigest: in.UADigest, IPPrefix: in.IPPrefix,
	}, now)
	if err != nil {
		return Tokens{}, err
	}
	if res.Status != RotateOK {
		return Tokens{}, errExpiredSession
	}
	sub, role := subjectOf(res)
	deviceID := ""
	if role == authtoken.RoleGuest {
		deviceID = res.GuestDeviceID.String()
	}
	return s.sign(sub, sid.String(), role, deviceID, refresh, now)
}

// Logout 吊销当前刷新令牌所在的会话族。令牌为空或已失效时作为幂等成功处理。
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return nil
	}
	return s.repo.RevokeFamilyByRefresh(ctx, hashToken(refreshToken), "logout")
}

// ListSessions 返回注册用户的「我的设备」列表（docs/03 第 2.2 节）。
func (s *Service) ListSessions(ctx context.Context, userID int64) ([]SessionInfo, error) {
	return s.repo.ListUserSessions(ctx, userID, s.now())
}

// RevokeSession 让注册用户下线自己的某个会话（设备下线）。会话不存在或不属于该用户时返回 ErrNotFound。
func (s *Service) RevokeSession(ctx context.Context, userID int64, sessionID uuid.UUID) error {
	n, err := s.repo.RevokeSessionByID(ctx, userID, sessionID, "user_logout")
	if err != nil {
		return err
	}
	if n == 0 {
		return errs.ErrNotFound
	}
	return nil
}

// RevokeAllSessions 吊销注册用户的全部会话（改密后调用，docs/03 第 2.2 节：改密吊销全部会话）。
func (s *Service) RevokeAllSessions(ctx context.Context, userID int64, reason string) error {
	return s.repo.RevokeAllForUser(ctx, userID, reason)
}

// ResolveAccess 校验 Access Token（无状态）并返回身份：过期返回 TOKEN_EXPIRED，其余无效返回 NEED_LOGIN。
func (s *Service) ResolveAccess(token string) (Access, error) {
	c, err := s.keys.Verify(token, s.now())
	switch {
	case errors.Is(err, authtoken.ErrExpired):
		return Access{}, errs.ErrTokenExpired
	case err != nil:
		return Access{}, errs.ErrNeedLogin
	}
	return Access{Subject: c.Subject, SessionID: c.SessionID, Role: c.Role}, nil
}

// sign 签发 Access Token 并组装返回。
func (s *Service) sign(sub, sid string, role authtoken.Role, deviceID, refresh string, now time.Time) (Tokens, error) {
	access, err := s.keys.Sign(authtoken.Claims{
		Subject: sub, SessionID: sid, Role: role, IssuedAt: now, ExpiresAt: now.Add(AccessTTL),
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("签发 access token: %w", err)
	}
	return Tokens{
		AccessToken: access, RefreshToken: refresh, DeviceID: deviceID, Role: role,
		ExpiresIn: int64(AccessTTL / time.Second),
	}, nil
}

// subjectOf 从轮换结果取主体与角色：有 UserID 为注册用户，否则游客。
func subjectOf(res RotateResult) (string, authtoken.Role) {
	if res.UserID > 0 {
		return strconv.FormatInt(res.UserID, 10), authtoken.RoleUser
	}
	return res.GuestDeviceID.String(), authtoken.RoleGuest
}

// newRefresh 生成 256 bit 刷新令牌与它的 SHA-256（服务端只存后者）。
func newRefresh() (token string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("生成刷新令牌: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
