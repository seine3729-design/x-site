// Package apienc 实现用户端接口报文加密（docs/10 第 5.3 节）。
//
// 协议 v1：
//   - 客户端每个请求用 HPKE（RFC 9180，base 模式，DHKEM(X25519, HKDF-SHA256) + HKDF-SHA256 + AES-256-GCM）
//     向服务端公钥封装一次，请求头 `X-Enc: 1.<kid>.<unix秒>.<enc>.<tag>`。
//   - tag = Seal(aad=RequestAAD, 空明文)（序号 0）：没有请求体的 GET 也能校验方法、路径、查询串与时间戳。
//   - 请求体（如有）= Seal(aad=RequestAAD, 明文 JSON)（序号 1）。
//   - 响应密钥 = Export("xsite-api/v1 response", 32)；响应体 = nonce(12) || AES-256-GCM(明文, aad="xsite-api/v1 response")。
//
// 路径与查询串不加密（路由、限流、日志需要），但通过 AAD 与 tag 绑定，改动即校验失败。
// 本包不依赖 Gin，HTTP 接入见 internal/http/middleware/apienc.go。
package apienc

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hpke"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// Version 是 X-Enc 头中的协议版本号。
	Version = "1"
	// Info 是 HPKE 上下文的 info，两端必须一致。
	Info = "xsite-api/v1"
	// ResponseLabel 同时用作响应密钥的导出标签与响应 AES-GCM 的 AAD。
	ResponseLabel = "xsite-api/v1 response"
	// ContentType 是加密报文的媒体类型（请求与响应相同）。
	ContentType = "application/vnd.xsite.enc"

	nonceSize   = 12
	keySize     = 32
	minSecret   = 32
	maxKidLen   = 16
	encodedEncN = 43 // 32 字节 X25519 公钥的 base64url（无填充）长度
	encodedTagN = 22 // 16 字节 GCM tag 的 base64url（无填充）长度
)

var (
	// ErrHeader 表示 X-Enc 头格式错误。
	ErrHeader = errors.New("apienc: X-Enc 头格式错误")
	// ErrUnknownKey 表示 kid 不在当前密钥环中（客户端应刷新公钥后重试）。
	ErrUnknownKey = errors.New("apienc: 未知的密钥 ID")
	// ErrDecrypt 表示解密或认证失败。
	ErrDecrypt = errors.New("apienc: 解密失败")
)

var (
	kem  = hpke.DHKEM(ecdh.X25519())
	kdf  = hpke.HKDFSHA256()
	aead = hpke.AES256GCM()
)

// Keyring 是服务端持有的 HPKE 私钥集合。第一把为当前密钥，其余用于轮换过渡期解密。
type Keyring struct {
	current string
	order   []string
	keys    map[string]hpke.PrivateKey
}

// PublicKey 是下发给客户端的公钥描述。
type PublicKey struct {
	Kid       string `json:"kid"`
	KEM       string `json:"kem"`
	KDF       string `json:"kdf"`
	AEAD      string `json:"aead"`
	PublicKey string `json:"public_key"` // base64url（无填充）
}

// ParseKeyring 解析 `kid:secret,kid2:secret2`。私钥由 secret 经 HPKE DeriveKeyPair 派生，
// 因此配置里可以放任意高熵字符串（≥32 字符），不需要单独生成 X25519 私钥文件。
func ParseKeyring(spec string) (*Keyring, error) {
	kr := &Keyring{keys: map[string]hpke.PrivateKey{}}
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kid, secret, ok := strings.Cut(part, ":")
		if !ok || !validKid(kid) {
			return nil, fmt.Errorf("密钥 ID 只能是 1–%d 位字母或数字: %q", maxKidLen, kid)
		}
		if len(secret) < minSecret {
			return nil, fmt.Errorf("密钥 %s 的 secret 至少 %d 个字符", kid, minSecret)
		}
		if _, dup := kr.keys[kid]; dup {
			return nil, fmt.Errorf("密钥 ID 重复: %s", kid)
		}
		priv, err := kem.DeriveKeyPair([]byte(secret))
		if err != nil {
			return nil, fmt.Errorf("派生密钥 %s 失败: %w", kid, err)
		}
		kr.keys[kid] = priv
		kr.order = append(kr.order, kid)
	}
	if len(kr.order) == 0 {
		return nil, errors.New("至少需要一把密钥")
	}
	kr.current = kr.order[0]
	return kr, nil
}

// Current 返回当前密钥 ID。
func (k *Keyring) Current() string { return k.current }

// PublicKeys 按配置顺序返回全部公钥（第一把为当前密钥）。
func (k *Keyring) PublicKeys() []PublicKey {
	out := make([]PublicKey, 0, len(k.order))
	for _, kid := range k.order {
		out = append(out, PublicKey{
			Kid:       kid,
			KEM:       "DHKEM(X25519, HKDF-SHA256)",
			KDF:       "HKDF-SHA256",
			AEAD:      "AES-256-GCM",
			PublicKey: b64(k.keys[kid].PublicKey().Bytes()),
		})
	}
	return out
}

// Header 是解析后的 X-Enc 请求头。
type Header struct {
	Kid string
	TS  int64
	Enc []byte
	Tag []byte
}

// ParseHeader 解析 `1.<kid>.<unix秒>.<enc base64url>.<tag base64url>`。
func ParseHeader(v string) (Header, error) {
	parts := strings.Split(v, ".")
	if len(parts) != 5 || parts[0] != Version || !validKid(parts[1]) ||
		len(parts[3]) != encodedEncN || len(parts[4]) != encodedTagN {
		return Header{}, ErrHeader
	}
	ts, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || ts <= 0 {
		return Header{}, ErrHeader
	}
	enc, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return Header{}, ErrHeader
	}
	tag, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return Header{}, ErrHeader
	}
	return Header{Kid: parts[1], TS: ts, Enc: enc, Tag: tag}, nil
}

// FormatHeader 生成 X-Enc 头（服务端测试与工具使用）。
func FormatHeader(kid string, ts int64, enc, tag []byte) string {
	return Version + "." + kid + "." + strconv.FormatInt(ts, 10) + "." + b64(enc) + "." + b64(tag)
}

// RequestAAD 把方法、请求目标（路径 + 查询串，按客户端发出的原样）与时间戳绑定到密文。
func RequestAAD(method, requestURI string, ts int64) []byte {
	return []byte(Info + "\n" + strings.ToUpper(method) + "\n" + requestURI + "\n" + strconv.FormatInt(ts, 10))
}

// Session 是一次请求-响应的解密结果。
type Session struct {
	respKey []byte
}

// Open 用 kid 对应的私钥打开请求：先校验 tag（绑定方法、请求目标与时间戳），再解密请求体。
// body 为空表示没有请求体（GET 等）。
func (k *Keyring) Open(h Header, aad, body []byte) (plaintext []byte, s *Session, err error) {
	priv, ok := k.keys[h.Kid]
	if !ok {
		return nil, nil, ErrUnknownKey
	}
	r, err := hpke.NewRecipient(h.Enc, priv, kdf, aead, []byte(Info))
	if err != nil {
		return nil, nil, ErrDecrypt
	}
	if empty, err := r.Open(aad, h.Tag); err != nil || len(empty) != 0 {
		return nil, nil, ErrDecrypt
	}
	if len(body) > 0 {
		plaintext, err = r.Open(aad, body)
		if err != nil {
			return nil, nil, ErrDecrypt
		}
	}
	key, err := r.Export(ResponseLabel, keySize)
	if err != nil {
		return nil, nil, fmt.Errorf("apienc: 导出响应密钥: %w", err)
	}
	return plaintext, &Session{respKey: key}, nil
}

// SealResponse 加密响应体：nonce(12) || ciphertext || tag。
func (s *Session) SealResponse(plaintext []byte) ([]byte, error) {
	gcm, err := newGCM(s.respKey)
	if err != nil {
		return nil, err
	}
	out := make([]byte, nonceSize, nonceSize+len(plaintext)+gcm.Overhead())
	if _, err := rand.Read(out); err != nil {
		return nil, err
	}
	return gcm.Seal(out, out[:nonceSize], plaintext, []byte(ResponseLabel)), nil
}

// OpenResponse 解密响应体（客户端行为，服务端用于测试与互通校验）。
func (s *Session) OpenResponse(data []byte) ([]byte, error) {
	return openResponse(s.respKey, data)
}

func openResponse(key, data []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(data) < nonceSize+gcm.Overhead() {
		return nil, ErrDecrypt
	}
	pt, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], []byte(ResponseLabel))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
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

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
