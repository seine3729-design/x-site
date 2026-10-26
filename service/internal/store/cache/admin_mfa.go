package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// AdminMFAStore 存后台二次验证的临时数据（Valkey，实现 admin.TOTPPendingStore，S2-6）。
// 目前只存「待确认的 TOTP 种子」：setup 时生成、短期有效，verify 校验通过后删除并落库。
type AdminMFAStore struct {
	client valkey.Client
	prefix string
}

func NewAdminMFAStore(client valkey.Client, prefix string) *AdminMFAStore {
	return &AdminMFAStore{client: client, prefix: prefix}
}

func (s *AdminMFAStore) pendingKey(adminID int64) string {
	return s.prefix + "totp_setup:" + strconv.FormatInt(adminID, 10)
}

// SetPendingTOTP 存待确认的 TOTP 种子（覆盖旧的；短 TTL）。
func (s *AdminMFAStore) SetPendingTOTP(ctx context.Context, adminID int64, secret string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	cmd := s.client.B().Set().Key(s.pendingKey(adminID)).Value(secret).Px(ttl).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("暂存 TOTP 种子: %w", err)
	}
	return nil
}

// GetPendingTOTP 取待确认的 TOTP 种子；不存在返回 ("", false, nil)。
func (s *AdminMFAStore) GetPendingTOTP(ctx context.Context, adminID int64) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	v, err := s.client.Do(ctx, s.client.B().Get().Key(s.pendingKey(adminID)).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("读取待确认 TOTP 种子: %w", err)
	}
	return v, true, nil
}

// DelPendingTOTP 删除待确认的 TOTP 种子。
func (s *AdminMFAStore) DelPendingTOTP(ctx context.Context, adminID int64) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if err := s.client.Do(ctx, s.client.B().Del().Key(s.pendingKey(adminID)).Build()).Error(); err != nil {
		return fmt.Errorf("清除待确认 TOTP 种子: %w", err)
	}
	return nil
}

func (s *AdminMFAStore) ticketKey(ticket string) string {
	return s.prefix + "login_ticket:" + ticket
}

// CreateLoginTicket 存登录二次验证票据（票据 → 管理员 ID，短 TTL，实现 admin.MFATicketStore）。
func (s *AdminMFAStore) CreateLoginTicket(ctx context.Context, ticket string, adminID int64, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	cmd := s.client.B().Set().Key(s.ticketKey(ticket)).Value(strconv.FormatInt(adminID, 10)).Px(ttl).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("暂存二次验证票据: %w", err)
	}
	return nil
}

// LookupLoginTicket 用票据换管理员 ID；不存在返回 (0, false, nil)。
func (s *AdminMFAStore) LookupLoginTicket(ctx context.Context, ticket string) (int64, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	v, err := s.client.Do(ctx, s.client.B().Get().Key(s.ticketKey(ticket)).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("读取二次验证票据: %w", err)
	}
	adminID, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("解析二次验证票据: %w", err)
	}
	return adminID, true, nil
}

// DeleteLoginTicket 删除登录二次验证票据（通过或失效时）。
func (s *AdminMFAStore) DeleteLoginTicket(ctx context.Context, ticket string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if err := s.client.Do(ctx, s.client.B().Del().Key(s.ticketKey(ticket)).Build()).Error(); err != nil {
		return fmt.Errorf("删除二次验证票据: %w", err)
	}
	return nil
}

func (s *AdminMFAStore) reauthKey(sessionKey string) string {
	return s.prefix + "reauth:" + sessionKey
}

// MarkReauthed 标记某会话刚在 ipPrefix 网段通过高敏重新验证（实现 admin.MFAStore）；ttl 为有效窗口，到期自动失效。
// 存网段而非布尔，供「IP 段变化时要求重新验证」比较（docs/10 第 6.3 节）。
func (s *AdminMFAStore) MarkReauthed(ctx context.Context, sessionKey, ipPrefix string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	cmd := s.client.B().Set().Key(s.reauthKey(sessionKey)).Value(ipPrefix).Px(ttl).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("标记重新验证: %w", err)
	}
	return nil
}

func (s *AdminMFAStore) waKey(key string) string { return s.prefix + "wa:" + key }

// SaveWebAuthnSession 暂存 WebAuthn 挑战会话（begin 与 finish 之间，实现 admin.WebAuthnSessionStore）。
func (s *AdminMFAStore) SaveWebAuthnSession(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	cmd := s.client.B().Set().Key(s.waKey(key)).Value(valkey.BinaryString(data)).Px(ttl).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("暂存 WebAuthn 会话: %w", err)
	}
	return nil
}

// LoadWebAuthnSession 读挑战会话；不存在返回 (nil, false, nil)。
func (s *AdminMFAStore) LoadWebAuthnSession(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	b, err := s.client.Do(ctx, s.client.B().Get().Key(s.waKey(key)).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("读取 WebAuthn 会话: %w", err)
	}
	return b, true, nil
}

// DelWebAuthnSession 删除挑战会话。
func (s *AdminMFAStore) DelWebAuthnSession(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if err := s.client.Do(ctx, s.client.B().Del().Key(s.waKey(key)).Build()).Error(); err != nil {
		return fmt.Errorf("删除 WebAuthn 会话: %w", err)
	}
	return nil
}

// IsReauthed 查某会话是否在重新验证窗口内、且仍在当初重新验证的 IP 网段（实现 http/admin.ReauthChecker）。
// 窗口过期（键不存在）或网段变化（值不符）都返回 false。
func (s *AdminMFAStore) IsReauthed(ctx context.Context, sessionKey, ipPrefix string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	v, err := s.client.Do(ctx, s.client.B().Get().Key(s.reauthKey(sessionKey)).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取重新验证标记: %w", err)
	}
	return v == ipPrefix, nil
}
