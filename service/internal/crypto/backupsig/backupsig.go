// Package backupsig 给配置备份文件签名（S4-4，docs/06 第 29 节「勿手改备份」）。
//
// 备份文件是明文 JSON（要能人工检视），所以用签名而不是加密：手改过的备份导入时会被拒绝。
// 算法与 crypto/playtoken 一致：每把密钥先经 HKDF 派生（域分隔，避免与其他地方的同一个密钥串混用），
// 再对「格式版本 + 正文」算 HMAC-SHA256，输出 `kid:base64url(mac)`。
//
// 格式版本一并进签名：否则可以只改版本号把一份新备份伪装成旧版本，诱导服务端走旧版导入分支。
package backupsig

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// keyInfo 是 HKDF 的域分隔信息：同一个密钥串用在别处派生出的子密钥与这里不同。
const keyInfo = "xsite-config-backup/v1"

// minSecretLen 是密钥的最小长度（与 LINE_SIGN_KEYS 等一致的下限）。
const minSecretLen = 32

var kidRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,16}$`)

// ErrNoKeys 表示没有配置签名密钥。
var ErrNoKeys = errors.New("没有配置配置备份的签名密钥（CONFIG_BACKUP_KEYS）")

// ErrBadSignature 表示签名缺失、格式不对或与正文不匹配。
var ErrBadSignature = errors.New("备份签名校验失败")

// Keyring 是备份签名密钥环：第一把用于签名，全部用于校验（支持轮换）。
type Keyring struct {
	order []string
	keys  map[string][]byte
}

// ParseKeyring 解析 `kid:密钥[,kid2:密钥2]`。
//
// 空串返回空密钥环而不是报错：本地开发不配密钥也要能导出导入，
// 此时导出的文件不带签名、导入也不验签（由调用方决定是否接受，见 Enabled）。
func ParseKeyring(spec string) (*Keyring, error) {
	kr := &Keyring{keys: map[string][]byte{}}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kid, secret, ok := strings.Cut(part, ":")
		kid, secret = strings.TrimSpace(kid), strings.TrimSpace(secret)
		if !ok || kid == "" || secret == "" {
			return nil, fmt.Errorf("格式应为 kid:密钥[,kid2:密钥2]")
		}
		if !kidRe.MatchString(kid) {
			return nil, fmt.Errorf("密钥标识 %q 只能是 1–16 位字母、数字、连字符或下划线", kid)
		}
		if _, dup := kr.keys[kid]; dup {
			return nil, fmt.Errorf("密钥标识 %q 重复", kid)
		}
		if len(secret) < minSecretLen {
			return nil, fmt.Errorf("密钥标识 %q 的密钥长度至少 %d 个字符", kid, minSecretLen)
		}
		derived, err := hkdf.Key(sha256.New, []byte(secret), nil, keyInfo, 32)
		if err != nil {
			return nil, fmt.Errorf("派生密钥 %q: %w", kid, err)
		}
		kr.order = append(kr.order, kid)
		kr.keys[kid] = derived
	}
	return kr, nil
}

// Enabled 表示配置了至少一把密钥。没有密钥时导出不签名、导入不验签。
func (k *Keyring) Enabled() bool { return k != nil && len(k.keys) > 0 }

// CurrentKID 返回签名用的密钥标识（第一把）。
func (k *Keyring) CurrentKID() string {
	if !k.Enabled() {
		return ""
	}
	return k.order[0]
}

// Sign 返回 `kid:base64url(HMAC-SHA256(派生密钥, 格式版本 || 正文))`。
func (k *Keyring) Sign(format int, payload []byte) (string, error) {
	if !k.Enabled() {
		return "", ErrNoKeys
	}
	kid := k.order[0]
	return kid + ":" + mac(k.keys[kid], format, payload), nil
}

// Verify 校验签名。签名由不认识的 kid 签发时也返回 ErrBadSignature（密钥已轮换掉或文件来自别的环境）。
func (k *Keyring) Verify(format int, payload []byte, signature string) error {
	if !k.Enabled() {
		return ErrNoKeys
	}
	kid, sig, ok := strings.Cut(signature, ":")
	if !ok || kid == "" || sig == "" {
		return fmt.Errorf("%w: 签名格式应为 kid:签名值", ErrBadSignature)
	}
	secret, known := k.keys[kid]
	if !known {
		return fmt.Errorf("%w: 签发签名的密钥 %q 不在当前配置中", ErrBadSignature, kid)
	}
	// 常量时间比较，避免按字节逐位试出签名
	if !hmac.Equal([]byte(mac(secret, format, payload)), []byte(sig)) {
		return fmt.Errorf("%w: 备份内容与签名不一致（文件被改动过？）", ErrBadSignature)
	}
	return nil
}

func mac(secret []byte, format int, payload []byte) string {
	h := hmac.New(sha256.New, secret)
	// 版本与正文之间加分隔符，避免「版本 1 + 正文 "2..."」和「版本 12 + 正文 "..."」算出同一个值
	h.Write([]byte(strconv.Itoa(format)))
	h.Write([]byte{0})
	h.Write(payload)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
