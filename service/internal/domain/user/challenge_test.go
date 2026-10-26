package user

import (
	"context"
	"strconv"
	"testing"
	"time"

	"xsite/service/internal/pow"
)

// memChallengeStore 是内存版 ChallengeStore；Take 取出即删（复刻 Valkey GETDEL 的一次性语义）。
type memChallengeStore struct{ m map[string]int }

func (s *memChallengeStore) Put(_ context.Context, nonce string, difficulty int, _ time.Duration) error {
	s.m[nonce] = difficulty
	return nil
}

func (s *memChallengeStore) Take(_ context.Context, nonce string) (int, bool, error) {
	d, ok := s.m[nonce]
	if ok {
		delete(s.m, nonce)
	}
	return d, ok, nil
}

func solve(challenge string, difficulty int) string {
	for i := 0; ; i++ {
		s := strconv.Itoa(i)
		if pow.Verify(challenge, s, difficulty) {
			return s
		}
	}
}

func TestGuard_IssueVerify(t *testing.T) {
	g := NewGuard(&memChallengeStore{m: map[string]int{}})
	ctx := context.Background()
	c, err := g.Issue(ctx)
	if err != nil || c.Nonce == "" || c.Difficulty <= 0 || c.ExpiresIn <= 0 {
		t.Fatalf("签发挑战不对：%+v / %v", c, err)
	}
	sol := solve(c.Nonce, c.Difficulty)
	if ok, err := g.Verify(ctx, ChallengeAnswer{Nonce: c.Nonce, Solution: sol}); err != nil || !ok {
		t.Fatalf("正确解应通过：%v / %v", ok, err)
	}
	// 一次性：同 nonce 再验失败（已被 Take 删除）
	if ok, _ := g.Verify(ctx, ChallengeAnswer{Nonce: c.Nonce, Solution: sol}); ok {
		t.Fatal("nonce 应一次性，重用应失败")
	}
}

func TestGuard_VerifyRejects(t *testing.T) {
	g := NewGuard(&memChallengeStore{m: map[string]int{}})
	ctx := context.Background()
	if ok, _ := g.Verify(ctx, ChallengeAnswer{}); ok {
		t.Fatal("空答案应拒绝")
	}
	if ok, _ := g.Verify(ctx, ChallengeAnswer{Nonce: "unknown", Solution: "x"}); ok {
		t.Fatal("未知 nonce 应拒绝")
	}
	c, _ := g.Issue(ctx)
	if ok, _ := g.Verify(ctx, ChallengeAnswer{Nonce: c.Nonce, Solution: "wrong-solution"}); ok {
		t.Fatal("错误解应拒绝")
	}
}
