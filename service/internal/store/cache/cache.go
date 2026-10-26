// Package cache 提供基于 Valkey 的读穿缓存：同键并发只回源一次，TTL 带随机抖动，Valkey 故障时直接回源。
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/valkey-io/valkey-go"
	"golang.org/x/sync/singleflight"
)

// 单次缓存读写的超时，超时视为缓存不可用，直接回源。
const opTimeout = 200 * time.Millisecond

// Store 是读穿缓存。
type Store struct {
	client valkey.Client
	prefix string
	group  singleflight.Group
}

// NewClient 按 URL 创建 Valkey 客户端（redis:// 或 rediss://）。
func NewClient(url string) (valkey.Client, error) {
	opt, err := valkey.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("解析 VALKEY_URL 失败: %w", err)
	}
	client, err := valkey.NewClient(opt)
	if err != nil {
		return nil, fmt.Errorf("连接 Valkey 失败: %w", err)
	}
	return client, nil
}

// New 创建缓存，prefix 用于区分环境或用途。
func New(client valkey.Client, prefix string) *Store {
	return &Store{client: client, prefix: prefix}
}

// GetOrLoadJSON 先读缓存，未命中时调用 load 并写回；结果解码到 dst。
func (s *Store) GetOrLoadJSON(ctx context.Context, key string, ttl time.Duration, dst any, load func(context.Context) (any, error)) error {
	fullKey := s.prefix + key

	if raw, ok := s.get(ctx, fullKey); ok {
		if err := json.Unmarshal(raw, dst); err == nil {
			return nil
		}
		// 解码失败说明结构已变化，按未命中处理。
	}

	raw, err, _ := s.group.Do(fullKey, func() (any, error) {
		v, err := load(ctx)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("缓存编码失败: %w", err)
		}
		s.set(ctx, fullKey, b, jitter(ttl))
		return b, nil
	})
	if err != nil {
		return err
	}
	return json.Unmarshal(raw.([]byte), dst)
}

// Delete 删除缓存键（配置变更后调用）。
func (s *Store) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	return s.client.Do(ctx, s.client.B().Del().Key(s.prefix+key).Build()).Error()
}

func (s *Store) get(ctx context.Context, key string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	b, err := s.client.Do(ctx, s.client.B().Get().Key(key).Build()).AsBytes()
	if err != nil {
		if !valkey.IsValkeyNil(err) {
			slog.WarnContext(ctx, "读取缓存失败，直接回源", "key", key, "err", err)
		}
		return nil, false
	}
	return b, true
}

func (s *Store) set(ctx context.Context, key string, val []byte, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), opTimeout)
	defer cancel()
	cmd := s.client.B().Set().Key(key).Value(valkey.BinaryString(val)).Px(ttl).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		slog.WarnContext(ctx, "写入缓存失败", "key", key, "err", err)
	}
}

// jitter 给 TTL 加 ±10% 随机抖动，避免大量键同时过期。
func jitter(ttl time.Duration) time.Duration {
	delta := float64(ttl) * 0.1
	return ttl + time.Duration((rand.Float64()*2-1)*delta)
}
