package password

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 测试用小参数，避免每个用例耗时数百毫秒。
var fast = Params{Memory: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestHashAndVerify(t *testing.T) {
	ctx := context.Background()
	h := NewHasher(fast, 2)

	encoded, err := h.Hash(ctx, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatalf("哈希串格式不符合 PHC：%q", encoded)
	}

	ok, rehash, err := h.Verify(ctx, "correct horse", encoded)
	if err != nil || !ok || rehash {
		t.Fatalf("正确密码应通过且无需重哈希：ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	if ok, _, _ := h.Verify(ctx, "wrong horse", encoded); ok {
		t.Fatal("错误密码不应通过")
	}

	again, _ := h.Hash(ctx, "correct horse")
	if again == encoded {
		t.Fatal("同一密码两次哈希应因随机盐而不同")
	}
}

// 参考实现（phc-winner-argon2）的官方测试向量：password / somesalt，t=2，m=2^16 KiB，p=1。
func TestVerify_ReferenceVector(t *testing.T) {
	const vector = "$argon2id$v=19$m=65536,t=2,p=1$c29tZXNhbHQ$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8ArZWb2GRPPc"
	ok, _, err := NewHasher(fast, 1).Verify(context.Background(), "password", vector)
	if err != nil || !ok {
		t.Fatalf("应能校验参考实现生成的哈希：ok=%v err=%v", ok, err)
	}
}

func TestVerify_NeedsRehashWhenParamsChange(t *testing.T) {
	ctx := context.Background()
	old, err := NewHasher(fast, 1).Hash(ctx, "secret-pass")
	if err != nil {
		t.Fatal(err)
	}
	stronger := fast
	stronger.Time = 2
	ok, rehash, err := NewHasher(stronger, 1).Verify(ctx, "secret-pass", old)
	if err != nil || !ok || !rehash {
		t.Fatalf("旧参数的哈希应通过校验并提示重哈希：ok=%v rehash=%v err=%v", ok, rehash, err)
	}
}

func TestVerify_Malformed(t *testing.T) {
	h := NewHasher(fast, 1)
	cases := map[string]string{
		"空串":         "",
		"明文":         "plain-text-password",
		"不是 id 变体":   "$argon2i$v=19$m=64,t=1,p=1$c29tZXNhbHRzYWx0$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8ArZWb2GRPPc",
		"版本不对":       "$argon2id$v=16$m=64,t=1,p=1$c29tZXNhbHRzYWx0$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8ArZWb2GRPPc",
		"参数尾部多余":     "$argon2id$v=19$m=64,t=1,p=1x$c29tZXNhbHRzYWx0$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8ArZWb2GRPPc",
		"内存超出上限":     "$argon2id$v=19$m=4194304,t=1,p=1$c29tZXNhbHRzYWx0$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8ArZWb2GRPPc",
		"盐不是 base64": "$argon2id$v=19$m=64,t=1,p=1$***$CTFhFdXPJO1aFaMaO6Mm5c8y7cJHAph8ArZWb2GRPPc",
		"哈希过短":       "$argon2id$v=19$m=64,t=1,p=1$c29tZXNhbHRzYWx0$CTFh",
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := h.Verify(context.Background(), "x", encoded); !errors.Is(err, ErrMalformed) {
				t.Fatalf("期望 ErrMalformed，得到 %v", err)
			}
		})
	}
}

func TestDerive_RespectsContextWhenSlotsBusy(t *testing.T) {
	h := NewHasher(fast, 1)
	h.slots <- struct{}{} // 占满并发名额
	defer func() { <-h.slots }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.Hash(ctx, "secret-pass"); !errors.Is(err, context.Canceled) {
		t.Fatalf("名额被占满且 ctx 已取消时应立即返回，得到 %v", err)
	}
	h.Equalize(ctx, "secret-pass") // 不应阻塞
}
