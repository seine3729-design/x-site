// Package mediakey 生成、包装与解开 HLS 内容密钥（docs/10 第 7 节，表 media_key 见 docs/02 第 6.4 节）。
//
// 内容密钥是每集一把的 16 字节 AES-128 密钥，入库前用 KEK 做 AES-256-GCM 包装，只在使用方的内存中解开。
// S2-8 接入 KMS（Tink 信封加密）之前，KEK 由环境变量 MEDIA_KEKS 提供：`kid:secret[,kid2:secret2]`，
// 第一把用于包装新密钥，其余只用于解开旧密钥（轮换过渡）；每把 KEK = HKDF-SHA256(secret, info)。
//
// 包装结果：kid ‖ ':' ‖ nonce(12) ‖ 密文与认证标签(32)。AAD 绑定表名、列名与主键
// （media_key|wrapped_key|<episode_id>|<key_ver>，docs/10 第 3.2 节），密文挪到其他分集或版本上无法解开。
package mediakey

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// KeySize 是内容密钥长度（HLS METHOD=AES-128）。
const KeySize = 16

const (
	kekInfo   = "xsite-media-kek/v1"
	minSecret = 32
	maxKidLen = 16
	nonceSize = 12
)

var (
	// ErrUnknownKEK 表示密文的 kid 不在当前密钥环中（KEK 已删除或配置错误）。
	ErrUnknownKEK = errors.New("mediakey: 未知的 KEK")
	// ErrCorrupt 表示密文格式错误或认证失败（被篡改、AAD 不匹配）。
	ErrCorrupt = errors.New("mediakey: 密文无效")
)

// Keyring 是一组 KEK，第一把为当前密钥。并发安全（只读）。
type Keyring struct {
	current string
	aeads   map[string]cipher.AEAD
}

// ParseKeyring 解析 `kid:secret[,kid2:secret2]`：kid 为 1–16 位字母或数字，secret 至少 32 个字符。
func ParseKeyring(spec string) (*Keyring, error) {
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
		key, err := hkdf.Key(sha256.New, []byte(secret), nil, kekInfo, 32)
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

// NewContentKey 生成新的 16 字节内容密钥。
func NewContentKey() []byte {
	key := make([]byte, KeySize)
	rand.Read(key) // crypto/rand.Read 不会失败（Go 1.24 起失败时直接终止进程）
	return key
}

// Wrap 用当前 KEK 包装某集某版本的内容密钥。
func (k *Keyring) Wrap(episodeID int64, keyVer int32, key []byte) ([]byte, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("mediakey: 内容密钥应为 %d 字节，得到 %d", KeySize, len(key))
	}
	aead := k.aeads[k.current]
	nonce := make([]byte, nonceSize)
	rand.Read(nonce)
	out := make([]byte, 0, len(k.current)+1+nonceSize+KeySize+aead.Overhead())
	out = append(out, k.current...)
	out = append(out, ':')
	out = append(out, nonce...)
	return aead.Seal(out, nonce, key, aad(episodeID, keyVer)), nil
}

// Unwrap 解开 Wrap 的结果；kid 不在密钥环中返回 ErrUnknownKEK，被篡改或 AAD 不匹配返回 ErrCorrupt。
func (k *Keyring) Unwrap(episodeID int64, keyVer int32, wrapped []byte) ([]byte, error) {
	i := bytes.IndexByte(wrapped, ':')
	if i <= 0 || i > maxKidLen {
		return nil, ErrCorrupt
	}
	aead, ok := k.aeads[string(wrapped[:i])]
	if !ok {
		return nil, ErrUnknownKEK
	}
	rest := wrapped[i+1:]
	if len(rest) != nonceSize+KeySize+aead.Overhead() {
		return nil, ErrCorrupt
	}
	key, err := aead.Open(nil, rest[:nonceSize], rest[nonceSize:], aad(episodeID, keyVer))
	if err != nil {
		return nil, ErrCorrupt
	}
	return key, nil
}

// CurrentKID 返回用于包装新密钥的 KEK 编号。
func (k *Keyring) CurrentKID() string { return k.current }

func aad(episodeID int64, keyVer int32) []byte {
	return []byte("media_key|wrapped_key|" + strconv.FormatInt(episodeID, 10) + "|" + strconv.FormatInt(int64(keyVer), 10))
}

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
