package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/admin"
	"xsite/service/internal/errs"
)

// AdminSessionStore 把后台会话存在 Valkey（实现 admin.SessionStore）。
// 键为「前缀 + 令牌摘要」，值只含管理员 ID、CSRF 令牌、UA 摘要与创建时间（docs/10 第 3.1 节）。
type AdminSessionStore struct {
	client valkey.Client
	prefix string
}

func NewAdminSessionStore(client valkey.Client, prefix string) *AdminSessionStore {
	return &AdminSessionStore{client: client, prefix: prefix}
}

type sessionRecord struct {
	AdminID        int64  `json:"admin_id"`
	SessionVersion int32  `json:"ver"`
	CSRFToken      string `json:"csrf"`
	UADigest       string `json:"ua"`
	CreatedAt      int64  `json:"created_at"` // Unix 秒
}

func (s *AdminSessionStore) Create(ctx context.Context, key string, sess admin.Session, ttl time.Duration) error {
	b, err := json.Marshal(sessionRecord{
		AdminID: sess.AdminID, SessionVersion: sess.SessionVersion, CSRFToken: sess.CSRFToken,
		UADigest: sess.UADigest, CreatedAt: sess.CreatedAt.Unix(),
	})
	if err != nil {
		return fmt.Errorf("会话编码失败: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	// 键由 256 bit 随机令牌派生，NX 只是防御：万一重复也不覆盖别人的会话
	cmd := s.client.B().Set().Key(s.prefix + key).Value(valkey.BinaryString(b)).Nx().Px(ttl).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("写入会话: %w", err)
	}
	return nil
}

func (s *AdminSessionStore) GetAndTouch(ctx context.Context, key string, ttl time.Duration) (admin.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	b, err := s.client.Do(ctx, s.client.B().Getex().Key(s.prefix+key).Px(ttl).Build()).AsBytes()
	if valkey.IsValkeyNil(err) {
		return admin.Session{}, errs.ErrNotFound
	}
	if err != nil {
		return admin.Session{}, fmt.Errorf("读取会话: %w", err)
	}
	var r sessionRecord
	if err := json.Unmarshal(b, &r); err != nil {
		// 结构不认识的会话视为不存在，用户重新登录即可
		return admin.Session{}, errs.ErrNotFound
	}
	return admin.Session{
		AdminID: r.AdminID, SessionVersion: r.SessionVersion, CSRFToken: r.CSRFToken,
		UADigest: r.UADigest, CreatedAt: time.Unix(r.CreatedAt, 0),
	}, nil
}

func (s *AdminSessionStore) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if err := s.client.Do(ctx, s.client.B().Del().Key(s.prefix+key).Build()).Error(); err != nil {
		return fmt.Errorf("删除会话: %w", err)
	}
	return nil
}

// Counter 是带滑动过期的计数器（实现 admin.FailureCounter），用于登录失败计数。
type Counter struct {
	client valkey.Client
	prefix string
}

func NewCounter(client valkey.Client, prefix string) *Counter {
	return &Counter{client: client, prefix: prefix}
}

func (c *Counter) Get(ctx context.Context, key string) (int64, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	k := c.prefix + key
	res := c.client.DoMulti(ctx, c.client.B().Get().Key(k).Build(), c.client.B().Pttl().Key(k).Build())
	raw, err := res[0].ToString()
	if valkey.IsValkeyNil(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("读取计数: %w", err)
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("计数格式错误: %w", err)
	}
	ms, err := res[1].AsInt64()
	if err != nil {
		return 0, 0, fmt.Errorf("读取计数有效期: %w", err)
	}
	// PTTL：-2 键不存在，-1 没有过期时间（不应出现），都按 0 处理
	return n, time.Duration(max(ms, 0)) * time.Millisecond, nil
}

func (c *Counter) Incr(ctx context.Context, key string, window time.Duration) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	k := c.prefix + key
	res := c.client.DoMulti(ctx,
		c.client.B().Incr().Key(k).Build(),
		c.client.B().Pexpire().Key(k).Milliseconds(window.Milliseconds()).Build(),
	)
	n, err := res[0].AsInt64()
	if err != nil {
		return 0, fmt.Errorf("计数加一: %w", err)
	}
	if err := res[1].Error(); err != nil {
		return 0, fmt.Errorf("设置计数有效期: %w", err)
	}
	return n, nil
}

func (c *Counter) Reset(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	if err := c.client.Do(ctx, c.client.B().Del().Key(c.prefix+key).Build()).Error(); err != nil {
		return fmt.Errorf("清除计数: %w", err)
	}
	return nil
}

// Add 把 key 增加 delta 并续期，返回累加后的值。配合按时间片轮换的 key（如「设备:分钟」）实现固定窗口限流。
func (c *Counter) Add(ctx context.Context, key string, delta int64, window time.Duration) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	k := c.prefix + key
	res := c.client.DoMulti(ctx,
		c.client.B().Incrby().Key(k).Increment(delta).Build(),
		c.client.B().Pexpire().Key(k).Milliseconds(window.Milliseconds()).Build(),
	)
	n, err := res[0].AsInt64()
	if err != nil {
		return 0, fmt.Errorf("计数累加: %w", err)
	}
	if err := res[1].Error(); err != nil {
		return 0, fmt.Errorf("设置计数有效期: %w", err)
	}
	return n, nil
}
