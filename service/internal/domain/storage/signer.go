package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 线路签名（docs/12 第 3.3 节，S1-6e）：按线路的 sign_type 为对象生成可访问地址。
//
// HLS 清单里有大量切片，逐个预签名既费 CPU 又让清单变长，所以视频线路优先用**目录前缀签名**
// （hmac_token / cdn_vendor）：一个令牌覆盖整集切片；只有直连存储时才逐个 s3_presign。
// 签名绑定路径前缀，不能复用到其他剧集（docs/12 第 9 节）。
//
// 本文件只负责「生成」。hmac_token 与 cdn_vendor 的校验发生在 CDN 边缘（边缘函数 / 厂商鉴权），
// s3_presign 由存储服务自身校验。VerifyHMACToken 给出与生成对应的校验逻辑，供边缘函数实现参考与测试对照。

// 签名 URL 的查询参数名（hmac_token）。p 显式带上签名覆盖的前缀，边缘无需猜前缀深度，
// 并且能直接校验「请求路径必须在该前缀下」，避免令牌被挪用到其他目录。
const (
	SignParamPrefix = "p"
	SignParamExpire = "exp"
	SignParamSig    = "s"
)

// ErrSignKeyMissing 表示线路引用的签名密钥不在 LINE_SIGN_KEYS 中（部署时没注入，或引用写错）。
var ErrSignKeyMissing = errors.New("线路引用的签名密钥未配置")

// SignKeyring 是线路签名密钥环：引用（sign_key_ref）→ 密钥。来自 LINE_SIGN_KEYS，不入库。
type SignKeyring struct {
	keys map[string][]byte
}

// ParseSignKeyring 解析 `引用:密钥[,引用2:密钥2]`。空串返回空密钥环（没有配置签名密钥，签名时报错而不是启动失败：
// 本地开发常用 presign 直连，不需要签名密钥）。
func ParseSignKeyring(raw string) (*SignKeyring, error) {
	kr := &SignKeyring{keys: map[string][]byte{}}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ref, secret, ok := strings.Cut(part, ":")
		ref, secret = strings.TrimSpace(ref), strings.TrimSpace(secret)
		if !ok || ref == "" || secret == "" {
			return nil, fmt.Errorf("格式应为 引用:密钥[,引用2:密钥2]")
		}
		if !signKeyRefRe.MatchString(ref) {
			return nil, fmt.Errorf("密钥引用 %q 只能是字母、数字、连字符或下划线", ref)
		}
		if _, dup := kr.keys[ref]; dup {
			return nil, fmt.Errorf("密钥引用 %q 重复", ref)
		}
		if len(secret) < minSignSecretLen {
			return nil, fmt.Errorf("密钥引用 %q 的密钥长度至少 %d 个字符", ref, minSignSecretLen)
		}
		kr.keys[ref] = []byte(secret)
	}
	return kr, nil
}

// minSignSecretLen 是签名密钥的最小长度（与 BFF_SERVICE_TOKEN 等一致的下限）。
const minSignSecretLen = 32

// Key 返回某个引用对应的密钥；不存在时返回 ErrSignKeyMissing。
func (k *SignKeyring) Key(ref string) ([]byte, error) {
	if k == nil {
		return nil, ErrSignKeyMissing
	}
	secret, ok := k.keys[ref]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSignKeyMissing, ref)
	}
	return secret, nil
}

// Has 表示某个引用的密钥已配置。
func (k *SignKeyring) Has(ref string) bool {
	if k == nil {
		return false
	}
	_, ok := k.keys[ref]
	return ok
}

// Refs 返回已配置的密钥引用数量（供启动日志与健康检查，不暴露密钥本身）。
func (k *SignKeyring) Len() int {
	if k == nil {
		return 0
	}
	return len(k.keys)
}

// ---------- 签名输入与结果 ----------

// Conn 是连接一个存储源所需的参数（不含凭据），从 Row 取出，供预签名与后续的上传 / 复制复用。
type Conn struct {
	Type       Type
	Subtype    Subtype
	Endpoint   string
	Region     string
	Bucket     string
	AccountID  string
	PathStyle  bool
	PathPrefix string
}

// Conn 返回该存储源的连接参数。
func (r Row) Conn() Conn {
	return Conn{
		Type: r.Type, Subtype: r.Subtype, Endpoint: r.Endpoint, Region: r.Region, Bucket: r.Bucket,
		AccountID: r.AccountID, PathStyle: r.PathStyle, PathPrefix: r.PathPrefix,
	}
}

// SignTarget 是要签名的目标。Key 是对象在存储源内的相对路径（不含存储源的 path_prefix）。
// Prefix 非空时表示目录前缀签名：一个令牌覆盖该前缀下的全部对象（整集切片），Key 必须在 Prefix 之下。
type SignTarget struct {
	Key    string
	Prefix string
}

// SignedURL 是签名结果。ExpiresAt 为零值表示不过期（公开读）。
type SignedURL struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// PresignParams 是生成 S3 预签名所需的参数（凭据已解密，只在内存中流转）。
type PresignParams struct {
	Conn Conn
	Cred Credential
	Key  string // 已拼上存储源 path_prefix 的完整对象键
	TTL  time.Duration
}

// Presigner 生成 S3 兼容存储的预签名下载地址（实现在 store/blob，domain 不依赖 AWS SDK）。
type Presigner interface {
	PresignGet(ctx context.Context, p PresignParams) (string, error)
}

// ---------- 签名服务 ----------

// SignService 按线路的签名方式生成可访问地址。
// keyring 解密存储源的只读凭据（s3_presign 用），signKeys 提供 hmac_token / cdn_vendor 的签名密钥。
type SignService struct {
	repo      Repo
	creds     CredentialOpener
	signKeys  *SignKeyring
	presigner Presigner
	vendors   *VendorRegistry
}

// CredentialOpener 解密存储源凭据（由 Service 实现，避免把 KEK 密钥环扩散到多个服务）。
type CredentialOpener interface {
	// ReadCredential 返回该存储源的只读凭据；没有单独配置时回退到读写凭据。
	ReadCredential(row Row) (Credential, error)
}

func NewSignService(repo Repo, creds CredentialOpener, signKeys *SignKeyring, presigner Presigner, vendors *VendorRegistry) *SignService {
	if vendors == nil {
		vendors = DefaultVendors()
	}
	return &SignService{repo: repo, creds: creds, signKeys: signKeys, presigner: presigner, vendors: vendors}
}

// SignFor 为指定线路上的目标生成可访问地址。ttl 为 0 时用线路的 token_ttl。
func (s *SignService) SignFor(ctx context.Context, line Line, target SignTarget, ttl time.Duration, now time.Time) (SignedURL, error) {
	if target.Key == "" {
		return SignedURL{}, badRequest("缺少对象路径")
	}
	if target.Prefix != "" && !strings.HasPrefix(target.Key, target.Prefix) {
		return SignedURL{}, badRequest("对象路径不在签名前缀之下")
	}
	if ttl <= 0 {
		ttl = time.Duration(line.TokenTTL) * time.Second
	}
	if ttl <= 0 {
		ttl = DefaultPresignTTL * time.Second
	}

	switch line.SignType {
	case SignNone:
		return s.signNone(line, target)
	case SignHMACToken:
		return s.signHMAC(line, target, ttl, now)
	case SignCDNVendor:
		return s.signVendor(line, target, ttl, now)
	case SignS3Presign:
		return s.signS3(ctx, line, target, ttl, now)
	default:
		return SignedURL{}, badRequest("线路的签名方式无效：%s", line.SignType)
	}
}

// SignByLine 读出线路后签名，供后台线路测试与后续的选源接口使用。
func (s *SignService) SignByLine(ctx context.Context, lineID int64, target SignTarget, now time.Time) (SignedURL, error) {
	line, err := s.repo.GetLine(ctx, lineID)
	if err != nil {
		return SignedURL{}, err
	}
	return s.SignFor(ctx, line, target, 0, now)
}

// signNone 直接拼公开读地址（封面等公开资源）。
func (s *SignService) signNone(line Line, target SignTarget) (SignedURL, error) {
	if line.BaseURL == "" {
		return SignedURL{}, badRequest("不签名的线路必须配置访问域名（Base URL）")
	}
	return SignedURL{URL: joinURL(line.BaseURL, target.Key)}, nil
}

// signHMAC 生成目录前缀签名：s = base64url(HMAC-SHA256(密钥, 前缀 + exp))（docs/12 第 3.3 节）。
// 前缀显式写进查询串，边缘据此校验签名并确认请求路径在该前缀之下。
func (s *SignService) signHMAC(line Line, target SignTarget, ttl time.Duration, now time.Time) (SignedURL, error) {
	if line.BaseURL == "" {
		return SignedURL{}, badRequest("HMAC 令牌签名的线路必须配置访问域名（Base URL）")
	}
	secret, err := s.signKeys.Key(line.SignKeyName())
	if err != nil {
		return SignedURL{}, err
	}
	prefix := target.Prefix
	if prefix == "" {
		prefix = target.Key // 没给前缀时退化为单个对象签名
	}
	exp := now.Add(ttl).Unix()
	sig := HMACToken(secret, prefix, exp)

	q := url.Values{}
	q.Set(SignParamPrefix, prefix)
	q.Set(SignParamExpire, strconv.FormatInt(exp, 10))
	q.Set(SignParamSig, sig)
	return SignedURL{
		URL:       joinURL(line.BaseURL, target.Key) + "?" + q.Encode(),
		ExpiresAt: time.Unix(exp, 0).UTC(),
	}, nil
}

// signVendor 按 CDN 厂商的 URL 鉴权算法生成地址。
func (s *SignService) signVendor(line Line, target SignTarget, ttl time.Duration, now time.Time) (SignedURL, error) {
	if line.BaseURL == "" {
		return SignedURL{}, badRequest("CDN 厂商鉴权的线路必须配置访问域名（Base URL）")
	}
	secret, err := s.signKeys.Key(line.SignKeyName())
	if err != nil {
		return SignedURL{}, err
	}
	vendor, err := s.vendors.Lookup(line.VendorRef())
	if err != nil {
		return SignedURL{}, err
	}
	exp := now.Add(ttl)
	raw, err := vendor.Sign(VendorInput{
		BaseURL: line.BaseURL, Key: target.Key, Prefix: target.Prefix, Secret: secret, ExpiresAt: exp,
	})
	if err != nil {
		return SignedURL{}, err
	}
	return SignedURL{URL: raw, ExpiresAt: exp.UTC()}, nil
}

// signS3 用存储源的只读凭据对单个对象生成 SigV4 预签名（直连存储）。
func (s *SignService) signS3(ctx context.Context, line Line, target SignTarget, ttl time.Duration, now time.Time) (SignedURL, error) {
	row, err := s.repo.Get(ctx, line.ProviderID)
	if err != nil {
		return SignedURL{}, err
	}
	if isHTTPOrigin(row.Type, row.Subtype) {
		return SignedURL{}, badRequest("HTTP 源不支持预签名，请改用其他签名方式")
	}
	// 不超过存储源允许的预签名有效期上限
	if max := time.Duration(row.MaxPresignTTL) * time.Second; max > 0 && ttl > max {
		ttl = max
	}
	cred, err := s.creds.ReadCredential(row)
	if err != nil {
		return SignedURL{}, err
	}
	raw, err := s.presigner.PresignGet(ctx, PresignParams{
		Conn: row.Conn(), Cred: cred, Key: JoinPrefix(row.PathPrefix, target.Key), TTL: ttl,
	})
	if err != nil {
		return SignedURL{}, fmt.Errorf("生成预签名地址: %w", err)
	}
	return SignedURL{URL: raw, ExpiresAt: now.Add(ttl).UTC()}, nil
}

// ---------- 签名与校验 ----------

// HMACToken 计算目录前缀签名：base64url(HMAC-SHA256(密钥, 前缀 + exp))（docs/12 第 3.3 节）。
func HMACToken(secret []byte, prefix string, exp int64) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(prefix))
	mac.Write([]byte(strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyHMACToken 校验一次请求：签名正确、未过期，且请求路径在签名覆盖的前缀之下。
// 真正的校验在 CDN 边缘执行，这里给出等价实现供边缘函数参考与测试对照（用常量时间比较）。
func VerifyHMACToken(secret []byte, reqPath, prefix, sig string, exp int64, now time.Time) error {
	if exp <= now.Unix() {
		return errors.New("链接已过期")
	}
	if prefix == "" || !strings.HasPrefix(strings.TrimPrefix(reqPath, "/"), strings.TrimPrefix(prefix, "/")) {
		return errors.New("请求路径不在签名覆盖的前缀之下")
	}
	want := HMACToken(secret, prefix, exp)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return errors.New("签名不匹配")
	}
	return nil
}

// JoinPrefix 把存储源的 path_prefix 与对象相对路径拼成完整对象键。
func JoinPrefix(pathPrefix, key string) string {
	pathPrefix = strings.Trim(pathPrefix, "/")
	key = strings.TrimPrefix(key, "/")
	if pathPrefix == "" {
		return key
	}
	return pathPrefix + "/" + key
}

// joinURL 把访问域名与对象路径拼起来（域名可带路径前缀）。
func joinURL(baseURL, key string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimPrefix(key, "/")
}
