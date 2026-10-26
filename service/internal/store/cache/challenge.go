package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// ChallengeStore 存一次性人机验证挑战 nonce（实现 user.ChallengeStore，S2-2）。
// 值为难度，带 TTL；Take 用 GETDEL 原子取出并删除，保证每个 nonce 只能用一次（防重放）。
type ChallengeStore struct {
	client valkey.Client
	prefix string
}

func NewChallengeStore(client valkey.Client, prefix string) *ChallengeStore {
	return &ChallengeStore{client: client, prefix: prefix}
}

// Put 存入 nonce → difficulty，带 TTL。
func (s *ChallengeStore) Put(ctx context.Context, nonce string, difficulty int, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	err := s.client.Do(ctx, s.client.B().Set().
		Key(s.prefix+nonce).Value(strconv.Itoa(difficulty)).
		PxMilliseconds(ttl.Milliseconds()).Build()).Error()
	if err != nil {
		return fmt.Errorf("存挑战: %w", err)
	}
	return nil
}

// Take 原子取出并删除 nonce（GETDEL），返回其难度；不存在（含已用 / 过期）返回 ok=false。
func (s *ChallengeStore) Take(ctx context.Context, nonce string) (int, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, opTimeout)
	defer cancel()
	raw, err := s.client.Do(ctx, s.client.B().Getdel().Key(s.prefix+nonce).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("取挑战: %w", err)
	}
	d, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false, fmt.Errorf("挑战难度格式错误: %w", err)
	}
	return d, true, nil
}
