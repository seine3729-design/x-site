// Package playtoken 签发与校验播放令牌（docs/10 第 4 节、docs/specs/S1-7.md）：选源接口（S1-7）签发，
// 清单与密钥接口（S1-8）校验。令牌只在本服务内流转，不交给 CDN 边缘，所以用服务端专用的密钥，而不是线路签名密钥。
//
// 格式：<kid>.<base64url(载荷 JSON)>.<base64url(HMAC-SHA256)>，HMAC 覆盖 kid 与载荷段，kid 用于密钥轮换。
// 载荷：{"e":分集,"l":线路,"d":设备哈希,"t":试看秒数,"x":过期时间（Unix 秒）}，d 为空、t 为 0 时省略。
//
// 密钥来自 PLAY_TOKEN_KEYS=kid:secret[,kid2:secret2]：第一把签发，全部用于校验（轮换时新密钥放第一把，
// 旧密钥保留到它签发的令牌全部过期）；每把密钥 = HKDF-SHA256(secret, info)，与其他用途的密钥隔离。
//
// 同一份 secret 按 Purpose 派生出互不通用的密钥环：前台播放（Playback）与运营后台试播（AdminPreview）
// 的令牌不能互相使用，后台令牌因此不需要另一个环境变量（docs/06 第 37 节）。
package playtoken

import (
	"bytes"
	"crypto/hkdf"
	"crypto/hmac"
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
	// maxTokenLen 是可接受的令牌长度上限：正常令牌约 150 字节，超长的直接拒绝，不做解码。
	maxTokenLen = 512

	// deviceDomain 让设备哈希只用于播放令牌，与其他地方对设备标识做的摘要不可互相比对。
	deviceDomain  = "xsite-play-device/v1\n"
	deviceHashLen = 16
)

// Purpose 区分令牌用途。同一份 PLAY_TOKEN_KEYS 按用途派生出互不通用的密钥与 HMAC 域，
// 所以后台试播令牌（S1-8 要求与前台令牌不通用）不需要再加一个环境变量：
// 用前台密钥环校验后台令牌会因为 HMAC 不符而失败，反之亦然。
type Purpose struct {
	keyInfo   string
	macDomain string
}

var (
	// Playback 是前台播放令牌（S1-7 选源签发、S1-8 清单与密钥校验）。
	// 这两个值定义了线上已签发令牌的签名，不能修改。
	Playback = Purpose{keyInfo: "xsite-play-token/v1", macDomain: "xsite-play/v1."}
	// AdminPreview 是运营后台试播令牌（docs/06 第 37 节）：只在后台会话保护的接口上使用。
	AdminPreview = Purpose{keyInfo: "xsite-admin-preview-token/v1", macDomain: "xsite-admin-preview/v1."}
)

var (
	// ErrInvalid 表示令牌格式错误、签名不对、kid 未知或载荷不合法。
	ErrInvalid = errors.New("playtoken: 令牌无效")
	// ErrExpired 表示令牌签名正确但已过期（客户端应重新选源）。
	ErrExpired = errors.New("playtoken: 令牌已过期")
)

// b64 是令牌各段的编码：无填充 base64url，且拒绝非规范编码（同一个令牌只有一种写法）。
var b64 = base64.RawURLEncoding.Strict()

// Claims 是令牌内容。
type Claims struct {
	EpisodeID int64
	LineID    int64
	// Device 是 DeviceHash(设备标识)；空串表示签发时没有设备标识，不绑定设备。
	Device string
	// TrialSec 为 0 表示完整播放，大于 0 表示只能试看这么多秒（S1-8 据此裁剪清单）。
	TrialSec  int32
	ExpiresAt time.Time // 秒精度
}

// BoundTo 判断令牌能否给这个设备使用：签发时没有设备标识的令牌不限设备。
func (c Claims) BoundTo(deviceID string) bool {
	if c.Device == "" {
		return true
	}
	return hmac.Equal([]byte(c.Device), []byte(DeviceHash(deviceID)))
}

// DeviceHash 返回写进令牌的设备哈希（SHA-256 前 16 字节，base64url），令牌里不出现设备标识原文。空标识返回空串。
func DeviceHash(deviceID string) string {
	if deviceID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(deviceDomain + deviceID))
	return b64.EncodeToString(sum[:deviceHashLen])
}

// payload 是载荷的 JSON 形态，字段名用单字母缩短令牌（令牌出现在每个清单与密钥请求的路径里）。
type payload struct {
	E int64  `json:"e"`
	L int64  `json:"l"`
	D string `json:"d,omitempty"`
	T int32  `json:"t,omitempty"`
	X int64  `json:"x"`
}

// Keyring 是播放令牌的签名密钥，第一把为当前密钥。并发安全（只读）。
type Keyring struct {
	purpose Purpose
	current string
	keys    map[string][]byte
}

// ParseKeyring 解析前台播放令牌的密钥（等同 ParseKeyringFor(spec, Playback)）。
func ParseKeyring(spec string) (*Keyring, error) { return ParseKeyringFor(spec, Playback) }

// ParseKeyringFor 解析 `kid:secret[,kid2:secret2]`：kid 为 1–16 位字母或数字，secret 至少 32 个字符，kid 不能重复。
// purpose 决定密钥派生与 HMAC 域，不同用途的密钥环互不通用。
func ParseKeyringFor(spec string, purpose Purpose) (*Keyring, error) {
	if purpose.keyInfo == "" || purpose.macDomain == "" {
		return nil, errors.New("令牌用途未指定（用 playtoken.Playback 或 playtoken.AdminPreview）")
	}
	kr := &Keyring{purpose: purpose, keys: map[string][]byte{}}
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
		key, err := hkdf.Key(sha256.New, []byte(secret), nil, purpose.keyInfo, 32)
		if err != nil {
			return nil, fmt.Errorf("派生密钥 %s: %w", kid, err)
		}
		kr.keys[kid] = key
		if kr.current == "" {
			kr.current = kid
		}
	}
	if kr.current == "" {
		return nil, errors.New("至少需要一把密钥（kid:secret）")
	}
	return kr, nil
}

// CurrentKID 返回签发新令牌用的密钥编号。
func (k *Keyring) CurrentKID() string { return k.current }

// Sign 用当前密钥签发令牌。
func (k *Keyring) Sign(c Claims) (string, error) {
	if c.EpisodeID <= 0 || c.LineID <= 0 || c.TrialSec < 0 || c.ExpiresAt.IsZero() {
		return "", fmt.Errorf("playtoken: 令牌内容不完整: %+v", c)
	}
	raw, err := json.Marshal(payload{E: c.EpisodeID, L: c.LineID, D: c.Device, T: c.TrialSec, X: c.ExpiresAt.Unix()})
	if err != nil {
		return "", fmt.Errorf("playtoken: 编码载荷: %w", err)
	}
	body := b64.EncodeToString(raw)
	sig := mac(k.purpose.macDomain, k.keys[k.current], k.current, body)
	return k.current + "." + body + "." + b64.EncodeToString(sig), nil
}

// Verify 校验令牌并返回内容：先核对签名再解析载荷，签名不对的内容一律不解析。
// 签名正确但已过期时返回 ErrExpired，其余问题都是 ErrInvalid（不区分原因，避免给伪造者提供线索）。
func (k *Keyring) Verify(token string, now time.Time) (Claims, error) {
	if len(token) > maxTokenLen {
		return Claims{}, ErrInvalid
	}
	kid, rest, ok := strings.Cut(token, ".")
	if !ok {
		return Claims{}, ErrInvalid
	}
	body, sigPart, ok := strings.Cut(rest, ".")
	if !ok || strings.Contains(sigPart, ".") {
		return Claims{}, ErrInvalid
	}
	key, ok := k.keys[kid]
	if !ok {
		return Claims{}, ErrInvalid
	}
	sig, err := b64.DecodeString(sigPart)
	if err != nil || !hmac.Equal(sig, mac(k.purpose.macDomain, key, kid, body)) {
		return Claims{}, ErrInvalid
	}

	raw, err := b64.DecodeString(body)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	p, err := decodePayload(raw)
	if err != nil {
		return Claims{}, ErrInvalid
	}
	c := Claims{EpisodeID: p.E, LineID: p.L, Device: p.D, TrialSec: p.T, ExpiresAt: time.Unix(p.X, 0).UTC()}
	if !now.Before(c.ExpiresAt) {
		return Claims{}, ErrExpired
	}
	return c, nil
}

// decodePayload 严格解析载荷：不允许未知字段与多余内容，数值必须在合法范围。
func decodePayload(raw []byte) (payload, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p payload
	if err := dec.Decode(&p); err != nil {
		return payload{}, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return payload{}, errors.New("载荷后有多余内容")
	}
	if p.E <= 0 || p.L <= 0 || p.T < 0 || p.X <= 0 {
		return payload{}, errors.New("载荷字段不合法")
	}
	return p, nil
}

func mac(macDomain string, key []byte, kid, body string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(macDomain))
	h.Write([]byte(kid))
	h.Write([]byte{'.'})
	h.Write([]byte(body))
	return h.Sum(nil)
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
