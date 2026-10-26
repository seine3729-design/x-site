// Package password 用 Argon2id 哈希与校验密码（docs/10 第 3.3 节）。
//
// 哈希串为 PHC 格式 `$argon2id$v=19$m=<KiB>,t=<迭代>,p=<并行度>$<盐>$<哈希>`（base64 无填充），
// 参数随哈希串保存：调高参数后旧哈希仍能校验，并在登录成功时透明重哈希。
package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params 是 Argon2id 参数，Memory 单位为 KiB。
type Params struct {
	Memory  uint32
	Time    uint32
	Threads uint8
	SaltLen uint32
	KeyLen  uint32
}

// DefaultParams：m=64MiB、t=3、p=2，盐 16 字节，输出 32 字节。
var DefaultParams = Params{Memory: 64 * 1024, Time: 3, Threads: 2, SaltLen: 16, KeyLen: 32}

// ErrMalformed 表示哈希串无法识别（不是 Argon2id、数据损坏或参数超出合理范围）。
var ErrMalformed = errors.New("password: 哈希串格式无效")

// 解析库中哈希串时的参数上限，防止损坏的数据让单次校验耗尽内存或 CPU。
const (
	maxMemoryKiB = 1 << 20 // 1 GiB
	maxTime      = 16
	minSaltLen   = 8
	minKeyLen    = 16
	maxKeyLen    = 64
)

var b64 = base64.RawStdEncoding

// Hasher 计算与校验密码哈希。每次计算占用 Params.Memory 的内存，
// 用信号量限制同时进行的计算数量，避免登录洪泛耗尽内存。
type Hasher struct {
	params Params
	slots  chan struct{}
}

// NewHasher 创建 Hasher，maxConcurrent 为同时进行的哈希计算上限（至少 1）。
func NewHasher(p Params, maxConcurrent int) *Hasher {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Hasher{params: p, slots: make(chan struct{}, maxConcurrent)}
}

// Hash 生成新密码的哈希串。
func (h *Hasher) Hash(ctx context.Context, plain string) (string, error) {
	salt := make([]byte, h.params.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成盐失败: %w", err)
	}
	key, err := h.derive(ctx, plain, salt, h.params)
	if err != nil {
		return "", err
	}
	p := h.params
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify 校验密码。needsRehash 为 true 表示该哈希的参数与当前配置不同，调用方应在校验成功后重新哈希保存。
func (h *Hasher) Verify(ctx context.Context, plain, encoded string) (ok, needsRehash bool, err error) {
	p, salt, want, err := decode(encoded)
	if err != nil {
		return false, false, err
	}
	got, err := h.derive(ctx, plain, salt, p)
	if err != nil {
		return false, false, err
	}
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	return true, p != h.params, nil
}

// Equalize 执行一次与 Verify 同等代价的计算并丢弃结果。
// 用户名不存在时调用，使响应耗时与「密码错误」相近，避免据此枚举用户名。
func (h *Hasher) Equalize(ctx context.Context, plain string) {
	_, _ = h.derive(ctx, plain, make([]byte, h.params.SaltLen), h.params)
}

func (h *Hasher) derive(ctx context.Context, plain string, salt []byte, p Params) ([]byte, error) {
	select {
	case h.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-h.slots }()
	return argon2.IDKey([]byte(plain), salt, p.Time, p.Memory, p.Threads, p.KeyLen), nil
}

// decode 解析 PHC 格式的哈希串，返回其中的参数、盐与哈希值。
func decode(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrMalformed
	}
	if parts[2] != fmt.Sprintf("v=%d", argon2.Version) {
		return Params{}, nil, nil, ErrMalformed
	}
	var p Params
	// Sscanf 不检查尾部多余字符，解析后再格式化一次比对，确保整段严格匹配
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Threads); err != nil ||
		parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", p.Memory, p.Time, p.Threads) {
		return Params{}, nil, nil, ErrMalformed
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, ErrMalformed
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, ErrMalformed
	}
	p.SaltLen, p.KeyLen = uint32(len(salt)), uint32(len(key))
	if p.Time < 1 || p.Time > maxTime || p.Threads < 1 || p.Memory < 8*uint32(p.Threads) || p.Memory > maxMemoryKiB ||
		p.SaltLen < minSaltLen || p.KeyLen < minKeyLen || p.KeyLen > maxKeyLen {
		return Params{}, nil, nil, ErrMalformed
	}
	return p, salt, key, nil
}
