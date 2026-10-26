package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"xsite/service/internal/errs"
)

// 后台二次验证 Passkey / WebAuthn（S2-6，docs/06 第 33 节）。自助注册、登录第二步、高敏重验共用断言校验。
// WebAuthn 的挑战会话（begin/finish 之间）暂存 Valkey；凭据按 docs/02 的列落库，登录时重建 go-webauthn 凭据校验。

const (
	// WebAuthn 挑战会话有效期（begin 生成 → finish 校验之间）。
	webauthnSessionTTL = 5 * time.Minute
	passkeyMaxNameLen  = 64
	maxPasskeys        = 10 // 每个管理员最多注册几把，避免无限增长
)

var (
	errPasskeyUnavailable = errs.ErrUnavailable.WithMessage("后台未启用 Passkey")
	errPasskeyNoChallenge = errs.ErrBadRequest.WithMessage("请先开始再提交，或操作已超时，请重试")
	errPasskeyVerify      = errs.ErrBadRequest.WithMessage("Passkey 校验未通过，请重试")
	errPasskeyTooMany     = errs.ErrConflict.WithMessage("Passkey 数量已达上限")
	errPasskeyNotFound    = errs.ErrNotFound.WithMessage("Passkey 不存在")
)

// PasskeyCredential 是一把已注册的 Passkey（存储层形态，对应 admin_passkey 一行）。
type PasskeyCredential struct {
	ID           int64
	CredentialID []byte
	PublicKey    []byte
	SignCount    uint32
	Transports   []string
	Name         string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

// PasskeyInfo 是自助管理列表里的一把 Passkey（不含公钥等内部字段）。
type PasskeyInfo struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// PasskeyRepo 读写 admin_passkey。
type PasskeyRepo interface {
	Insert(ctx context.Context, adminID int64, c PasskeyCredential) (int64, time.Time, error)
	List(ctx context.Context, adminID int64) ([]PasskeyCredential, error)
	UpdateOnLogin(ctx context.Context, credentialID []byte, signCount uint32) error
	Delete(ctx context.Context, adminID, id int64) (bool, error)
	Count(ctx context.Context, adminID int64) (int, error)
	DeleteByAdmin(ctx context.Context, adminID int64) error
}

// WebAuthnSessionStore 暂存 WebAuthn 挑战会话（begin 与 finish 之间，Valkey）。
type WebAuthnSessionStore interface {
	SaveWebAuthnSession(ctx context.Context, key string, data []byte, ttl time.Duration) error
	LoadWebAuthnSession(ctx context.Context, key string) (data []byte, found bool, err error)
	DelWebAuthnSession(ctx context.Context, key string) error
}

// PasskeyConfig 是 WebAuthn 依赖方（Relying Party）配置。
type PasskeyConfig struct {
	RPID        string   // 后台域名，如 admin.example.com（本地 localhost）
	DisplayName string   // 展示名
	Origins     []string // 允许的来源（ADMIN_ORIGINS）
}

// PasskeyService 处理 Passkey 的注册与断言校验。
type PasskeyService struct {
	wa    *webauthn.WebAuthn
	repo  PasskeyRepo
	store WebAuthnSessionStore
}

// NewPasskeyService 创建 Passkey 服务。Passkey 用公钥密码，不需要 KEK，ADMIN_ORIGINS 齐备即可用。
func NewPasskeyService(cfg PasskeyConfig, repo PasskeyRepo, store WebAuthnSessionStore) (*PasskeyService, error) {
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: cfg.DisplayName,
		RPOrigins:     cfg.Origins,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 WebAuthn: %w", err)
	}
	return &PasskeyService{wa: wa, repo: repo, store: store}, nil
}

// passkeyUser 把一个管理员适配成 webauthn.User。
type passkeyUser struct {
	id    int64
	name  string
	creds []webauthn.Credential
}

// WebAuthnID 是用户句柄：用管理员 ID 的十进制字符串字节（稳定、≤64 字节），注册与断言间必须一致。
func (u *passkeyUser) WebAuthnID() []byte                         { return []byte(strconv.FormatInt(u.id, 10)) }
func (u *passkeyUser) WebAuthnName() string                       { return u.name }
func (u *passkeyUser) WebAuthnDisplayName() string                { return u.name }
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func toWebauthnCredential(p PasskeyCredential) webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, len(p.Transports))
	for i, t := range p.Transports {
		transports[i] = protocol.AuthenticatorTransport(t)
	}
	return webauthn.Credential{
		ID:            p.CredentialID,
		PublicKey:     p.PublicKey,
		Transport:     transports,
		Authenticator: webauthn.Authenticator{SignCount: p.SignCount},
	}
}

func (s *PasskeyService) user(ctx context.Context, adminID int64, name string) (*passkeyUser, error) {
	creds, err := s.repo.List(ctx, adminID)
	if err != nil {
		return nil, err
	}
	u := &passkeyUser{id: adminID, name: name, creds: make([]webauthn.Credential, len(creds))}
	for i, c := range creds {
		u.creds[i] = toWebauthnCredential(c)
	}
	return u, nil
}

// ---------- 自助注册 ----------

// BeginRegistration 为当前管理员生成 Passkey 注册选项并暂存挑战会话（key 绑定到管理员）。
func (s *PasskeyService) BeginRegistration(ctx context.Context, adminID int64, username string) (json.RawMessage, error) {
	n, err := s.repo.Count(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if n >= maxPasskeys {
		return nil, errPasskeyTooMany
	}
	u, err := s.user(ctx, adminID, username)
	if err != nil {
		return nil, err
	}
	// 2FA 场景：平台无偏好、不要求驻留密钥，用户验证设为 preferred（跟随认证器能力）
	options, session, err := s.wa.BeginRegistration(u, webauthn.WithExclusions(excludeList(u.creds)))
	if err != nil {
		return nil, fmt.Errorf("生成注册选项: %w", err)
	}
	if err := s.saveSession(ctx, regKey(adminID), session); err != nil {
		return nil, err
	}
	// 只回内层创建选项（PublicKeyCredentialCreationOptions），@simplewebauthn/browser 直接吃
	return json.Marshal(options.Response)
}

// FinishRegistration 校验注册响应并把新 Passkey 落库。
func (s *PasskeyService) FinishRegistration(ctx context.Context, adminID int64, username, name string, response []byte) error {
	session, ok, err := s.loadSession(ctx, regKey(adminID))
	if err != nil {
		return err
	}
	if !ok {
		return errPasskeyNoChallenge
	}
	u, err := s.user(ctx, adminID, username)
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(response))
	if err != nil {
		return errPasskeyVerify
	}
	cred, err := s.wa.CreateCredential(u, session, parsed)
	if err != nil {
		return errPasskeyVerify
	}
	_ = s.store.DelWebAuthnSession(ctx, regKey(adminID))

	transports := make([]string, len(cred.Transport))
	for i, t := range cred.Transport {
		transports[i] = string(t)
	}
	_, _, err = s.repo.Insert(ctx, adminID, PasskeyCredential{
		CredentialID: cred.ID,
		PublicKey:    cred.PublicKey,
		SignCount:    cred.Authenticator.SignCount,
		Transports:   transports,
		Name:         trimName(name),
	})
	return err
}

// ---------- 列表 / 删除 ----------

func (s *PasskeyService) ListPasskeys(ctx context.Context, adminID int64) ([]PasskeyInfo, error) {
	creds, err := s.repo.List(ctx, adminID)
	if err != nil {
		return nil, err
	}
	out := make([]PasskeyInfo, len(creds))
	for i, c := range creds {
		out[i] = PasskeyInfo{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt, LastUsedAt: c.LastUsedAt}
	}
	return out, nil
}

func (s *PasskeyService) DeletePasskey(ctx context.Context, adminID, id int64) error {
	ok, err := s.repo.Delete(ctx, adminID, id)
	if err != nil {
		return err
	}
	if !ok {
		return errPasskeyNotFound
	}
	return nil
}

// ---------- 断言（登录第二步、高敏重验共用）----------

// BeginAssertion 为某管理员生成断言选项并把挑战会话暂存到 challengeKey。
func (s *PasskeyService) BeginAssertion(ctx context.Context, adminID int64, challengeKey string) (json.RawMessage, error) {
	u, err := s.user(ctx, adminID, strconv.FormatInt(adminID, 10))
	if err != nil {
		return nil, err
	}
	if len(u.creds) == 0 {
		return nil, errPasskeyNotFound
	}
	options, session, err := s.wa.BeginLogin(u)
	if err != nil {
		return nil, fmt.Errorf("生成断言选项: %w", err)
	}
	if err := s.saveSession(ctx, challengeKey, session); err != nil {
		return nil, err
	}
	// 只回内层断言选项（PublicKeyCredentialRequestOptions）
	return json.Marshal(options.Response)
}

// FinishAssertion 校验断言响应（实现 admin.PasskeyAuthenticator）；通过后写回计数、清理挑战会话。
func (s *PasskeyService) FinishAssertion(ctx context.Context, adminID int64, challengeKey string, response []byte) error {
	session, ok, err := s.loadSession(ctx, challengeKey)
	if err != nil {
		return err
	}
	if !ok {
		return errPasskeyNoChallenge
	}
	u, err := s.user(ctx, adminID, strconv.FormatInt(adminID, 10))
	if err != nil {
		return err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(response))
	if err != nil {
		return errPasskeyVerify
	}
	cred, err := s.wa.ValidateLogin(u, session, parsed)
	if err != nil {
		return errPasskeyVerify
	}
	_ = s.store.DelWebAuthnSession(ctx, challengeKey)
	// 写回计数（防重放：计数回退由库在 ValidateLogin 中按 CloneWarning 标记，这里只更新存储）
	if err := s.repo.UpdateOnLogin(ctx, cred.ID, cred.Authenticator.SignCount); err != nil {
		return err
	}
	return nil
}

// ---------- 内部 ----------

func (s *PasskeyService) saveSession(ctx context.Context, key string, session *webauthn.SessionData) error {
	b, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("编码 WebAuthn 会话: %w", err)
	}
	return s.store.SaveWebAuthnSession(ctx, key, b, webauthnSessionTTL)
}

func (s *PasskeyService) loadSession(ctx context.Context, key string) (webauthn.SessionData, bool, error) {
	b, ok, err := s.store.LoadWebAuthnSession(ctx, key)
	if err != nil || !ok {
		return webauthn.SessionData{}, false, err
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(b, &session); err != nil {
		return webauthn.SessionData{}, false, nil // 结构不认识当作过期
	}
	return session, true, nil
}

func excludeList(creds []webauthn.Credential) []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, len(creds))
	for i, c := range creds {
		out[i] = c.Descriptor()
	}
	return out
}

func trimName(name string) string {
	return printable(name, passkeyMaxNameLen)
}

// 挑战会话的键（Valkey）：注册按管理员，登录按票据，reauth 按会话键。
func regKey(adminID int64) string          { return "reg:" + strconv.FormatInt(adminID, 10) }
func PasskeyLoginKey(ticket string) string { return "login:" + ticket }
func PasskeyReauthKey(sk string) string    { return "reauth:" + sk }
