package domains

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// 证书生成的约束。
const (
	minCertDays = 1
	maxCertDays = 3650 // 10 年，自签够用；真正对外用的证书走 ACME
	maxSANs     = 64
	maxCertPEM  = 1 << 16 // 上传证书 PEM 的大小上限（64 KiB），再大不是正常证书
)

// KeyType 是生成证书用的密钥类型。
type KeyType string

const (
	KeyEC  KeyType = "ec_p256"  // ECDSA P-256（默认，短小高效）
	KeyRSA KeyType = "rsa_2048" // RSA 2048（兼容老客户端）
)

// GenCertInput 是生成自签证书的输入。
type GenCertInput struct {
	CommonName string
	SANs       []string
	Days       int
	KeyType    KeyType
}

// GeneratedCert 是生成的自签证书。KeyPEM 是私钥，只在生成响应里返回这一次，服务端不保存、不记日志、不记审计。
type GeneratedCert struct {
	CertPEM     string    `json:"cert_pem"`
	KeyPEM      string    `json:"key_pem"`
	KeyType     KeyType   `json:"key_type"`
	SANs        []string  `json:"sans"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	Fingerprint string    `json:"fingerprint"`
}

// CertInfo 是解析一张证书得到的信息（上传证书回填到期日时展示）。
type CertInfo struct {
	Subject     string    `json:"subject"`
	Issuer      string    `json:"issuer"`
	SANs        []string  `json:"sans"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	Fingerprint string    `json:"fingerprint"`
	IsCA        bool      `json:"is_ca"`
	SelfSigned  bool      `json:"self_signed"`
}

// GenerateSelfSigned 用标准库现场签出一张自签服务器证书。
//
// 用途：源站回源 mTLS、本地 / 测试 HTTPS、certbot 应急占位。浏览器不信任自签证书，
// 面向用户的公网入口证书仍由部署层 ACME 自动签发（docs/09 第 2.2 节）。
func GenerateSelfSigned(in GenCertInput, now time.Time) (GeneratedCert, error) {
	cn := strings.TrimSpace(strings.ToLower(in.CommonName))
	if !validHostOrWildcard(cn) {
		return GeneratedCert{}, badRequest("请填写正确的域名作为证书主题（Common Name）")
	}
	if in.Days < minCertDays || in.Days > maxCertDays {
		return GeneratedCert{}, badRequest("有效期为 %d–%d 天", minCertDays, maxCertDays)
	}
	sans, err := normalizeSANs(cn, in.SANs)
	if err != nil {
		return GeneratedCert{}, err
	}

	kt := in.KeyType
	if kt == "" {
		kt = KeyEC
	}
	var priv crypto.Signer
	keyUsage := x509.KeyUsageDigitalSignature
	switch kt {
	case KeyEC:
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return GeneratedCert{}, fmt.Errorf("生成 EC 私钥: %w", err)
		}
		priv = k
	case KeyRSA:
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return GeneratedCert{}, fmt.Errorf("生成 RSA 私钥: %w", err)
		}
		priv = k
		keyUsage |= x509.KeyUsageKeyEncipherment // RSA 可用于密钥交换，ECDSA 不适用
	default:
		return GeneratedCert{}, badRequest("密钥类型只能是 ec_p256 或 rsa_2048")
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return GeneratedCert{}, fmt.Errorf("生成序列号: %w", err)
	}
	notBefore := now.Add(-5 * time.Minute) // 容忍签发机与使用机的时钟偏差
	notAfter := now.AddDate(0, 0, in.Days)
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		DNSNames:              sans,
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              keyUsage,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, priv.Public(), priv)
	if err != nil {
		return GeneratedCert{}, fmt.Errorf("签发自签证书: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return GeneratedCert{}, fmt.Errorf("编码私钥: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return GeneratedCert{
		CertPEM:     string(certPEM),
		KeyPEM:      string(keyPEM),
		KeyType:     kt,
		SANs:        sans,
		NotBefore:   notBefore.UTC(),
		NotAfter:    notAfter.UTC(),
		Fingerprint: fingerprint(der),
	}, nil
}

// ParseCertificate 解析上传的 PEM 证书，取第一张（叶子）证书的信息。
func ParseCertificate(pemText string) (CertInfo, error) {
	if len(pemText) > maxCertPEM {
		return CertInfo{}, badRequest("证书内容过大，请确认粘贴的是单张证书")
	}
	rest := []byte(pemText)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue // 跳过私钥、参数等其它块
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return CertInfo{}, badRequest("证书无法解析，请确认是有效的 PEM 证书")
		}
		return CertInfo{
			Subject:     cert.Subject.String(),
			Issuer:      cert.Issuer.String(),
			SANs:        append([]string{}, cert.DNSNames...),
			NotBefore:   cert.NotBefore.UTC(),
			NotAfter:    cert.NotAfter.UTC(),
			Fingerprint: fingerprint(cert.Raw),
			IsCA:        cert.IsCA,
			SelfSigned:  cert.Subject.String() == cert.Issuer.String(),
		}, nil
	}
	return CertInfo{}, badRequest("没有找到证书，请粘贴以 -----BEGIN CERTIFICATE----- 开头的 PEM 内容")
}

// normalizeSANs 把 CN 并入 SAN 列表，去重并逐个校验。
func normalizeSANs(cn string, extra []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(extra)+1)
	add := func(h string) error {
		h = strings.TrimSpace(strings.ToLower(h))
		if h == "" {
			return nil
		}
		if !validHostOrWildcard(h) {
			return badRequest("SAN「%s」不是有效域名", h)
		}
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
		return nil
	}
	if err := add(cn); err != nil {
		return nil, err
	}
	for _, s := range extra {
		if err := add(s); err != nil {
			return nil, err
		}
		if len(out) > maxSANs {
			return nil, badRequest("SAN 最多 %d 个", maxSANs)
		}
	}
	return out, nil
}

// validHostOrWildcard 允许泛域名（*.example.com）作为证书主题 / SAN。
func validHostOrWildcard(h string) bool {
	if strings.HasPrefix(h, "*.") {
		return validHost(h[2:])
	}
	return validHost(h)
}

// fingerprint 返回证书 DER 的 SHA-256 指纹（大写十六进制、冒号分隔）。
func fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	var b strings.Builder
	for i, x := range sum {
		if i > 0 {
			b.WriteByte(':')
		}
		fmt.Fprintf(&b, "%02X", x)
	}
	return b.String()
}
