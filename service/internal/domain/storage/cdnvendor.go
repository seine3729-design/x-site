package storage

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// CDN 厂商 URL 鉴权（docs/12 第 3.3 节的 cdn_vendor，S1-6e）：每家 CDN 一个适配器。
// 算法由厂商规定，我们只负责按其规范生成；校验在厂商边缘完成。

// VendorInput 是生成厂商鉴权地址所需的参数。Prefix 非空时按目录前缀签名（一个令牌覆盖整集切片）。
type VendorInput struct {
	BaseURL   string
	Key       string
	Prefix    string
	Secret    []byte
	ExpiresAt time.Time
}

// CDNVendor 是一家 CDN 的 URL 鉴权算法。
type CDNVendor interface {
	// Name 是适配器名称，写在线路的 sign_key_ref 里（形如 vendor:名称/密钥引用）。
	Name() string
	// Sign 返回带鉴权参数的完整地址。
	Sign(in VendorInput) (string, error)
}

// VendorRegistry 是厂商适配器注册表。
type VendorRegistry struct {
	byName map[string]CDNVendor
}

func NewVendorRegistry(vendors ...CDNVendor) *VendorRegistry {
	r := &VendorRegistry{byName: make(map[string]CDNVendor, len(vendors))}
	for _, v := range vendors {
		r.byName[v.Name()] = v
	}
	return r
}

// DefaultVendors 是内置的厂商适配器。接入新厂商时在这里注册一个实现即可。
func DefaultVendors() *VendorRegistry {
	return NewVendorRegistry(TimestampMD5Vendor{})
}

// Lookup 按名称取适配器。
func (r *VendorRegistry) Lookup(name string) (CDNVendor, error) {
	if name == "" {
		return nil, badRequest("CDN 厂商鉴权的线路要在密钥引用里写明厂商，形如 vendor:厂商名/密钥引用")
	}
	v, ok := r.byName[name]
	if !ok {
		return nil, badRequest("不支持的 CDN 厂商：%s（已支持：%s）", name, strings.Join(r.Names(), "、"))
	}
	return v, nil
}

// Names 返回已注册的厂商名称。
func (r *VendorRegistry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	return out
}

// VendorRef 从线路的 sign_key_ref 中解析厂商名：格式为 `厂商名/密钥引用`，没有斜杠时视为未指定厂商。
func (l Line) VendorRef() string {
	name, _, ok := strings.Cut(l.SignKeyRef, "/")
	if !ok {
		return ""
	}
	return name
}

// SignKeyName 返回 sign_key_ref 中真正的密钥引用部分：cdn_vendor 的 `厂商名/密钥引用` 取后半段，其余取整体。
func (l Line) SignKeyName() string {
	if l.SignType == SignCDNVendor {
		if _, key, ok := strings.Cut(l.SignKeyRef, "/"); ok {
			return key
		}
		return ""
	}
	return l.SignKeyRef
}

// TimestampMD5Vendor 是「时间戳防盗链 A 型」适配器：阿里云 CDN、腾讯云 CDN 等多家通用的鉴权方式，
// 在查询串里带 auth_key=过期时间-随机串-uid-md5(路径-过期时间-随机串-uid-密钥)。
//
// 这里用 MD5 是厂商协议的硬性要求（边缘按同样算法校验），不是本项目的安全选择；
// 防护强度来自短有效期与密钥保密。能选的话优先用 hmac_token（自有边缘函数，HMAC-SHA256）。
type TimestampMD5Vendor struct{}

func (TimestampMD5Vendor) Name() string { return "timestamp_md5" }

func (TimestampMD5Vendor) Sign(in VendorInput) (string, error) {
	if len(in.Secret) == 0 {
		return "", badRequest("缺少 CDN 鉴权密钥")
	}
	// 签名针对目录前缀时，厂商按「URI 前缀」校验，此时 URI 取前缀；否则取对象自身
	uri := in.Key
	if in.Prefix != "" {
		uri = in.Prefix
	}
	uri = "/" + strings.TrimPrefix(uri, "/")

	ts := strconv.FormatInt(in.ExpiresAt.Unix(), 10)
	nonce, err := randomHex(8)
	if err != nil {
		return "", fmt.Errorf("生成随机串: %w", err)
	}
	const uid = "0" // 厂商协议保留字段，固定 0
	sum := md5.Sum([]byte(strings.Join([]string{uri, ts, nonce, uid, string(in.Secret)}, "-")))

	q := url.Values{}
	q.Set("auth_key", strings.Join([]string{ts, nonce, uid, hex.EncodeToString(sum[:])}, "-"))
	return joinURL(in.BaseURL, in.Key) + "?" + q.Encode(), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
