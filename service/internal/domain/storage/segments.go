package storage

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 清单批量签名（S1-8，docs/specs/S1-8.md）：一档清单有几十到几百个切片，按线路的签名方式一次生成全部地址。
// 结果与对每个对象单独调用 SignFor（Prefix 为同一目录）相同，只是把能共用的计算提出来：
// hmac_token 对目录前缀只算一次签名；cdn_vendor 的密钥与适配器只取一次；s3_presign 的存储源与凭据只取一次，
// 预签名器支持批量时也只建一次客户端。

// PresignManyParams 是一批对象的预签名参数：同一存储源、同一有效期。Keys 是已拼上存储源 path_prefix 的完整对象键。
type PresignManyParams struct {
	Conn Conn
	Cred Credential
	Keys []string
	TTL  time.Duration
}

// BatchPresigner 一次为同一存储源上的多个对象生成预签名地址（只建一次客户端）。
// Presigner 实现了它时清单批量签名走这里，否则逐个调用 PresignGet。
type BatchPresigner interface {
	PresignGetMany(ctx context.Context, p PresignManyParams) ([]string, error)
}

// SignSegments 为同一集产物里的一批对象生成线路上的访问地址，顺序与 keys 一致。prefix 是这些对象共同所在的目录
// （以 / 结尾，形如 hls/{集}/{任务}/s/），keys 都必须在它之下：目录前缀签名的令牌只覆盖这个目录。
// 有效期为线路的 token_ttl（s3_presign 不超过存储源允许的上限）。
func (s *SignService) SignSegments(ctx context.Context, line Line, prefix string, keys []string, now time.Time) ([]string, error) {
	if prefix == "" || !strings.HasSuffix(prefix, "/") {
		return nil, badRequest("签名前缀必须是以 / 结尾的目录")
	}
	for _, k := range keys {
		if len(k) <= len(prefix) || !strings.HasPrefix(k, prefix) {
			return nil, badRequest("对象路径不在签名前缀之下")
		}
	}
	if len(keys) == 0 {
		return []string{}, nil
	}
	ttl := time.Duration(line.TokenTTL) * time.Second
	if ttl <= 0 {
		ttl = DefaultPresignTTL * time.Second
	}

	switch line.SignType {
	case SignNone:
		if line.BaseURL == "" {
			return nil, badRequest("不签名的线路必须配置访问域名（Base URL）")
		}
		out := make([]string, len(keys))
		for i, k := range keys {
			out[i] = joinURL(line.BaseURL, k)
		}
		return out, nil
	case SignHMACToken:
		return s.hmacSegments(line, prefix, keys, ttl, now)
	case SignCDNVendor:
		return s.vendorSegments(line, prefix, keys, ttl, now)
	case SignS3Presign:
		return s.presignSegments(ctx, line, keys, ttl)
	default:
		return nil, badRequest("线路的签名方式无效：%s", line.SignType)
	}
}

// hmacSegments 与 signHMAC 相同的目录前缀签名：同一前缀、同一过期时间的签名只算一次，查询串对所有切片相同。
func (s *SignService) hmacSegments(line Line, prefix string, keys []string, ttl time.Duration, now time.Time) ([]string, error) {
	if line.BaseURL == "" {
		return nil, badRequest("HMAC 令牌签名的线路必须配置访问域名（Base URL）")
	}
	secret, err := s.signKeys.Key(line.SignKeyName())
	if err != nil {
		return nil, err
	}
	exp := now.Add(ttl).Unix()
	q := url.Values{}
	q.Set(SignParamPrefix, prefix)
	q.Set(SignParamExpire, strconv.FormatInt(exp, 10))
	q.Set(SignParamSig, HMACToken(secret, prefix, exp))
	query := "?" + q.Encode()
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = joinURL(line.BaseURL, k) + query
	}
	return out, nil
}

// vendorSegments 按 CDN 厂商算法对目录前缀签名：密钥与适配器只取一次，地址仍由适配器逐个生成（厂商算法可能带随机串）。
func (s *SignService) vendorSegments(line Line, prefix string, keys []string, ttl time.Duration, now time.Time) ([]string, error) {
	if line.BaseURL == "" {
		return nil, badRequest("CDN 厂商鉴权的线路必须配置访问域名（Base URL）")
	}
	secret, err := s.signKeys.Key(line.SignKeyName())
	if err != nil {
		return nil, err
	}
	vendor, err := s.vendors.Lookup(line.VendorRef())
	if err != nil {
		return nil, err
	}
	exp := now.Add(ttl)
	out := make([]string, len(keys))
	for i, k := range keys {
		if out[i], err = vendor.Sign(VendorInput{BaseURL: line.BaseURL, Key: k, Prefix: prefix, Secret: secret, ExpiresAt: exp}); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// presignSegments 逐个预签名（直连存储）：存储源与只读凭据只取一次。
func (s *SignService) presignSegments(ctx context.Context, line Line, keys []string, ttl time.Duration) ([]string, error) {
	row, err := s.repo.Get(ctx, line.ProviderID)
	if err != nil {
		return nil, err
	}
	if isHTTPOrigin(row.Type, row.Subtype) {
		return nil, badRequest("HTTP 源不支持预签名，请改用其他签名方式")
	}
	if limit := time.Duration(row.MaxPresignTTL) * time.Second; limit > 0 && ttl > limit {
		ttl = limit
	}
	cred, err := s.creds.ReadCredential(row)
	if err != nil {
		return nil, err
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = JoinPrefix(row.PathPrefix, k)
	}
	if bp, ok := s.presigner.(BatchPresigner); ok {
		out, err := bp.PresignGetMany(ctx, PresignManyParams{Conn: row.Conn(), Cred: cred, Keys: full, TTL: ttl})
		if err != nil {
			return nil, fmt.Errorf("生成预签名地址: %w", err)
		}
		if len(out) != len(keys) {
			return nil, fmt.Errorf("预签名结果数量不符：%d / %d", len(out), len(keys))
		}
		return out, nil
	}
	out := make([]string, len(keys))
	for i, k := range full {
		if out[i], err = s.presigner.PresignGet(ctx, PresignParams{Conn: row.Conn(), Cred: cred, Key: k, TTL: ttl}); err != nil {
			return nil, fmt.Errorf("生成预签名地址: %w", err)
		}
	}
	return out, nil
}
