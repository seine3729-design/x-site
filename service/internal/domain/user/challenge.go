package user

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"xsite/service/internal/pow"
)

const (
	// challengeDifficulty 是 PoW 前导零 bit 数：约 2^18 次哈希，浏览器 / APP 通常 1 秒内解出，挡住自动化撞库。
	challengeDifficulty = 18
	// challengeTTL 是挑战有效期：取题到提交要在此时间内，且 nonce 一次性（取出即删）。
	challengeTTL = 2 * time.Minute
)

// Challenge 是一次人机验证挑战（下发给客户端求解）。
type Challenge struct {
	Nonce      string
	Difficulty int
	ExpiresIn  int64 // 秒
}

// ChallengeAnswer 是客户端提交的挑战答案。
type ChallengeAnswer struct {
	Nonce    string
	Solution string
}

// ChallengeStore 存一次性挑战 nonce（Valkey 实现）。Take 原子取出并删除（防重放）。
type ChallengeStore interface {
	Put(ctx context.Context, nonce string, difficulty int, ttl time.Duration) error
	Take(ctx context.Context, nonce string) (difficulty int, ok bool, err error)
}

// ChallengeGuard 签发与校验人机验证挑战（供登录流程在失败过多时要求）。
type ChallengeGuard interface {
	Issue(ctx context.Context) (Challenge, error)
	Verify(ctx context.Context, answer ChallengeAnswer) (bool, error)
}

// Guard 是基于 PoW + 一次性 nonce 的 ChallengeGuard 实现。
type Guard struct {
	store ChallengeStore
}

func NewGuard(store ChallengeStore) *Guard { return &Guard{store: store} }

// Issue 生成一个新挑战并存入 store（一次性、带 TTL）。
func (g *Guard) Issue(ctx context.Context) (Challenge, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return Challenge{}, fmt.Errorf("生成挑战: %w", err)
	}
	nonce := base64.RawURLEncoding.EncodeToString(raw)
	if err := g.store.Put(ctx, nonce, challengeDifficulty, challengeTTL); err != nil {
		return Challenge{}, err
	}
	return Challenge{Nonce: nonce, Difficulty: challengeDifficulty, ExpiresIn: int64(challengeTTL / time.Second)}, nil
}

// Verify 校验挑战答案：nonce 必须存在（一次性，取出即删）且未过期，solution 满足难度。
func (g *Guard) Verify(ctx context.Context, answer ChallengeAnswer) (bool, error) {
	if answer.Nonce == "" || answer.Solution == "" {
		return false, nil
	}
	difficulty, ok, err := g.store.Take(ctx, answer.Nonce)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil // 不存在 / 已用 / 过期
	}
	return pow.Verify(answer.Nonce, answer.Solution, difficulty), nil
}
