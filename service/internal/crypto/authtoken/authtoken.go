// Package authtoken 签发与校验用户端 Access Token（S1-10，docs/10 第 4 节）。
//
// Access Token 是标准 JWT（JWS 紧凑格式），EdDSA / Ed25519 签名，头部带 kid；载荷 sub/sid/role/iat/exp，不放任何个人数据。
// 校验时固定只接受 alg=EdDSA，拒绝 none 与算法混淆（docs/10 第 4 节）。
// 密钥来自 JWT_SIGNING_KEYS（`kid:secret[,kid2:secret2]`）：每把 secret 经 HKDF-SHA256 派生成 Ed25519 种子，
// 多实例用同一 secret 得到同一密钥对，不必分发私钥文件（与 apienc / playtoken 的做法一致）。
// 第一把为当前签发密钥，其余用于轮换过渡期校验；轮换时新 kid 放第一把，旧 kid 保留到它签发的令牌全部过期再删。
package authtoken

import (
	"bytes"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	minSecret = 32
	maxKidLen = 16
	// maxTokenLen 是可接受的令牌长度上限：正常令牌约 250 字节，超长的直接拒绝、不解析。
	maxTokenLen = 1024
	// seedInfo 把 JWT 签名密钥与其它用途（播放令牌、报文加密、KEK）分开，即使复用同一 secret 也得到不同密钥。
	seedInfo = "xsite-jwt/v1"
	// algEdDSA 是唯一接受的签名算法；拒绝 none 与其它算法，避免算法混淆攻击。
	algEdDSA = "EdDSA"
)

var (
	// ErrInvalid 表示令牌格式错误、签名不对、算法不符或 kid 未知。
	ErrInvalid = errors.New("authtoken: 令牌无效")
	// ErrExpired 表示令牌签名正确但已过期。
	ErrExpired = errors.New("authtoken: 令牌已过期")
)

// b64 是各段的编码：无填充 base64url，拒绝非规范编码。
var b64 = base64.RawURLEncoding.Strict()

// Role 是令牌主体的角色。
type Role string

const (
	RoleGuest Role = "guest" // 游客（sub 为 guest_device_id）
	RoleUser  Role = "user"  // 注册用户（sub 为 user_id，S2-1 起）
)

// Claims 是 Access Token 的载荷。
type Claims struct {
	// Subject 是主体标识：游客为 guest_device_id（UUID），注册用户为 user_id。
	Subject string
	// SessionID 是会话标识（user_session.id），把 Access Token 关联到刷新令牌所属的会话族。
	SessionID string
	Role      Role
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// header 是 JWS 头部（固定 EdDSA）。
type header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
	Kid string `json:"kid"`
}

// payload 是 JWT 标准载荷字段，缩短放不下敏感信息。
type payload struct {
	Sub  string `json:"sub"`
	Sid  string `json:"sid"`
	Role string `json:"role"`
	Iat  int64  `json:"iat"`
	Exp  int64  `json:"exp"`
}

// key 是一把签名密钥的私钥与公钥。
type key struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

// Keyring 是 Ed25519 签名密钥集合。第一把为当前签发密钥。并发安全（只读）。
type Keyring struct {
	current string
	order   []string
	keys    map[string]key
}

// ParseKeyring 解析 `kid:secret[,kid2:secret2]`：kid 为 1–16 位字母或数字，secret 至少 32 个字符，kid 不能重复。
func ParseKeyring(spec string) (*Keyring, error) {
	kr := &Keyring{keys: map[string]key{}}
	for part := range strings.SplitSeq(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kid, secret, ok := strings.Cut(part, ":")
		if !ok || !validKid(kid) {
			return nil, fmt.Errorf("密钥编号只能是 1–%d 位字母或数字: %q", maxKidLen, kid)
		}
		if len(secret) < minSecret {
			return nil, fmt.Errorf("密钥 %s 的 secret 至少 %d 个字符", kid, minSecret)
		}
		if _, dup := kr.keys[kid]; dup {
			return nil, fmt.Errorf("密钥编号重复: %s", kid)
		}
		seed, err := hkdf.Key(sha256.New, []byte(secret), nil, seedInfo, ed25519.SeedSize)
		if err != nil {
			return nil, fmt.Errorf("派生密钥 %s: %w", kid, err)
		}
		priv := ed25519.NewKeyFromSeed(seed)
		kr.keys[kid] = key{priv: priv, pub: priv.Public().(ed25519.PublicKey)}
		kr.order = append(kr.order, kid)
	}
	if len(kr.order) == 0 {
		return nil, errors.New("至少需要一把密钥（kid:secret）")
	}
	kr.current = kr.order[0]
	return kr, nil
}

// CurrentKID 返回签发新令牌用的密钥编号。
func (k *Keyring) CurrentKID() string { return k.current }

// Sign 用当前密钥签发 Access Token。
func (k *Keyring) Sign(c Claims) (string, error) {
	if c.Subject == "" || c.Role == "" || c.ExpiresAt.IsZero() || c.IssuedAt.IsZero() {
		return "", fmt.Errorf("authtoken: 令牌内容不完整: %+v", c)
	}
	hdr, err := json.Marshal(header{Alg: algEdDSA, Typ: "JWT", Kid: k.current})
	if err != nil {
		return "", fmt.Errorf("authtoken: 编码头部: %w", err)
	}
	pl, err := json.Marshal(payload{
		Sub: c.Subject, Sid: c.SessionID, Role: string(c.Role), Iat: c.IssuedAt.Unix(), Exp: c.ExpiresAt.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("authtoken: 编码载荷: %w", err)
	}
	signingInput := b64.EncodeToString(hdr) + "." + b64.EncodeToString(pl)
	sig := ed25519.Sign(k.keys[k.current].priv, []byte(signingInput))
	return signingInput + "." + b64.EncodeToString(sig), nil
}

// Verify 校验令牌并返回载荷：先按头部 kid 固定 EdDSA 验签，再解析载荷。
// 签名正确但已过期返回 ErrExpired，其余问题一律 ErrInvalid（不区分原因，避免给伪造者线索）。
func (k *Keyring) Verify(token string, now time.Time) (Claims, error) {
	if len(token) == 0 || len(token) > maxTokenLen {
		return Claims{}, ErrInvalid
	}
	h, rest, ok := strings.Cut(token, ".")
	if !ok {
		return Claims{}, ErrInvalid
	}
	p, sigPart, ok := strings.Cut(rest, ".")
	if !ok || strings.Contains(sigPart, ".") {
		return Claims{}, ErrInvalid
	}

	hdrRaw, err := b64.DecodeString(h)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	hdr, err := decodeStrict[header](hdrRaw)
	// 固定只接受 EdDSA（拒绝 none 与算法混淆），typ 若带必须是 JWT
	if err != nil || hdr.Alg != algEdDSA || (hdr.Typ != "" && hdr.Typ != "JWT") {
		return Claims{}, ErrInvalid
	}
	key, ok := k.keys[hdr.Kid]
	if !ok {
		return Claims{}, ErrInvalid
	}
	sig, err := b64.DecodeString(sigPart)
	if err != nil || !ed25519.Verify(key.pub, []byte(h+"."+p), sig) {
		return Claims{}, ErrInvalid
	}

	plRaw, err := b64.DecodeString(p)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	pl, err := decodeStrict[payload](plRaw)
	if err != nil || pl.Sub == "" || pl.Role == "" || pl.Iat <= 0 || pl.Exp <= 0 {
		return Claims{}, ErrInvalid
	}
	c := Claims{
		Subject: pl.Sub, SessionID: pl.Sid, Role: Role(pl.Role),
		IssuedAt: time.Unix(pl.Iat, 0).UTC(), ExpiresAt: time.Unix(pl.Exp, 0).UTC(),
	}
	if !now.Before(c.ExpiresAt) {
		return Claims{}, ErrExpired
	}
	return c, nil
}

// decodeStrict 严格解析 JSON：不允许未知字段与多余内容。
func decodeStrict[T any](raw []byte) (T, error) {
	var v T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return v, errors.New("JSON 后有多余内容")
	}
	return v, nil
}

func validKid(kid string) bool {
	if kid == "" || len(kid) > maxKidLen {
		return false
	}
	for _, r := range kid {
		if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9') {
			return false
		}
	}
	return true
}
