package cache

import (
	"context"
	"testing"
	"time"
)

// 集成测试：连本地 compose 的 Valkey；验证挑战 nonce 的存入、GETDEL 一次性取出与过期。
func TestChallengeStore(t *testing.T) {
	client, prefix := testClient(t)
	store := NewChallengeStore(client, prefix+"ch:")
	ctx := context.Background()

	if err := store.Put(ctx, "n1", 18, time.Minute); err != nil {
		t.Fatal(err)
	}
	d, ok, err := store.Take(ctx, "n1")
	if err != nil || !ok || d != 18 {
		t.Fatalf("取挑战应得 difficulty=18：d=%d ok=%v err=%v", d, ok, err)
	}
	// 一次性：GETDEL 后再取不存在
	if _, ok, _ := store.Take(ctx, "n1"); ok {
		t.Fatal("GETDEL 后应不存在")
	}
	// 未知 nonce
	if _, ok, _ := store.Take(ctx, "nope"); ok {
		t.Fatal("未知 nonce 应 ok=false")
	}
	// 过期
	if err := store.Put(ctx, "n2", 10, 60*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(140 * time.Millisecond)
	if _, ok, _ := store.Take(ctx, "n2"); ok {
		t.Fatal("过期后应不存在")
	}
}
