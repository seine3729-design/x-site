// Package envelope 提供字段级信封加密（docs/10 第 3.2 节）：用 KEK 对任意字段做 AES-256-GCM 加密。
// 主要给后台「只写不读」的凭据使用（存储源 S1-6a、支付渠道等）：明文加密入库，读出时解密，密文随行存储。
//
// S2-8 接入 KMS（Tink）之前，KEK 由环境变量提供：`kid:secret[,kid2:secret2]`，第一把用于加密新数据，
// 其余只用于解开旧密文（轮换过渡）；每把 KEK = HKDF-SHA256(secret, info)，info 区分用途（不同用途即使 secret 相同也派生出不同密钥）。
//
// 密文格式：kid ‖ ':' ‖ nonce(12) ‖ 密文与认证标签。AAD 由调用方给出（通常是 表名|列名[|主键]，docs/10 第 3.2 节），
// 绑定后密文无法被挪到其他列或记录上解开。
package envelope

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
)

const (
	minSecret = 32
	maxKidLen = 16
	nonceSize = 12
)

var (
	// ErrUnknownKEK 表示密文的 kid 不在当前密钥环中（KEK 已删除或配置错误）。
	ErrUnknownKEK = errors.New("envelope: 未知的 KEK")
	// ErrCorrupt 表示密文格式错误或认证失败（被篡改、AAD 不一致）。
	ErrCorrupt = errors.New("envelope: 密文无效")
)

// Keyring 是一组 KEK，第一把为当前密钥。并发安全（只读）。
type Keyring struct {
	current string
	aeads   map[string]cipher.AEAD
}

// ParseKeyring 解析 `kid:secret[,kid2:secret2]`：kid 为 1–16 位字母或数字，secret 至少 32 个字符。
// info 区分 KEK 用途（如 "xsite-storage-kek/v1"），不能为空。
func ParseKeyring(spec, info string) (*Keyring, error) {
	if info == "" {
		return nil, errors.New("envelope: 必须指定 info（区分 KEK 用途）")
	}
	kr := &Keyring{aeads: map[string]cipher.AEAD{}}
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kid, secret, ok := strings.Cut(part, ":")
		if !ok || !validKid(kid) {
			return nil, fmt.Errorf("KEK 编号只能是 1–%d 位字母或数字: %q", maxKidLen, kid)
		}
		if len(secret) < minSecret {
			return nil, fmt.Errorf("KEK %s 的 secret 至少 %d 个字符", kid, minSecret)
		}
		if _, dup := kr.aeads[kid]; dup {
			return nil, fmt.Errorf("KEK 编号重复: %s", kid)
		}
		key, err := hkdf.Key(sha256.New, []byte(secret), nil, info, 32)
		if err != nil {
			return nil, fmt.Errorf("派生 KEK %s: %w", kid, err)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, err
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, err
		}
		kr.aeads[kid] = aead
		if kr.current == "" {
			kr.current = kid
		}
	}
	if kr.current == "" {
		return nil, errors.New("至少需要一把 KEK（kid:secret）")
	}
	return kr, nil
}

// Seal 用当前 KEK 加密 plaintext，aad 绑定到密文（解密时必须一致）。plaintext 可以为空。
func (k *Keyring) Seal(aad, plaintext []byte) []byte {
	aead := k.aeads[k.current]
	nonce := make([]byte, nonceSize)
	rand.Read(nonce) // crypto/rand.Read 不会失败（Go 1.24 起失败时直接终止进程）
	out := make([]byte, 0, len(k.current)+1+nonceSize+len(plaintext)+aead.Overhead())
	out = append(out, k.current...)
	out = append(out, ':')
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, aad)
}

// Open 解开 Seal 的结果；kid 不在密钥环中返回 ErrUnknownKEK，被篡改或 aad 不一致返回 ErrCorrupt。
func (k *Keyring) Open(aad, ciphertext []byte) ([]byte, error) {
	i := bytes.IndexByte(ciphertext, ':')
	if i <= 0 || i > maxKidLen {
		return nil, ErrCorrupt
	}
	aead, ok := k.aeads[string(ciphertext[:i])]
	if !ok {
		return nil, ErrUnknownKEK
	}
	rest := ciphertext[i+1:]
	if len(rest) < nonceSize+aead.Overhead() {
		return nil, ErrCorrupt
	}
	plain, err := aead.Open(nil, rest[:nonceSize], rest[nonceSize:], aad)
	if err != nil {
		return nil, ErrCorrupt
	}
	return plain, nil
}

// CurrentKID 返回用于加密新数据的 KEK 编号。
func (k *Keyring) CurrentKID() string { return k.current }

func validKid(kid string) bool {
	if kid == "" || len(kid) > maxKidLen {
		return false
	}
	for i := 0; i < len(kid); i++ {
		c := kid[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}
