// Package netguard 校验出站连接的目标地址（SSRF 防护，docs/12 第 9 节）。
// 连接后台可配置的地址（资源机 FTP，以后的自定义存储源、线路测试）时：先解析域名并校验全部 IP，
// 之后只按校验过的 IP 直连、不再重新解析，避免 DNS 重绑定把连接引到内网。
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// BlockedError 表示目标地址被策略禁止。
type BlockedError struct {
	Addr netip.Addr
}

func (e *BlockedError) Error() string {
	return fmt.Sprintf("地址 %s 属于内网或保留地址段", e.Addr)
}

// IsBlocked 判断错误链上是否有 *BlockedError。
func IsBlocked(err error) bool {
	var b *BlockedError
	return errors.As(err, &b)
}

// Policy 是出站地址策略。零值为最严格：只允许公网单播地址。
type Policy struct {
	// AllowPrivate 为 true 时允许内网、回环、链路本地与保留地址（只用于开发环境，EGRESS_ALLOW_PRIVATE）。
	// 未指定地址（0.0.0.0、::）与组播地址始终禁止。
	AllowPrivate bool
}

// 标准库判断之外仍不能作为出站目标的地址段（RFC 6890 等）。
var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // 本网络
	netip.MustParsePrefix("100.64.0.0/10"),   // 运营商级 NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF 协议分配
	netip.MustParsePrefix("192.0.2.0/24"),    // 文档示例
	netip.MustParsePrefix("198.18.0.0/15"),   // 基准测试
	netip.MustParsePrefix("198.51.100.0/24"), // 文档示例
	netip.MustParsePrefix("203.0.113.0/24"),  // 文档示例
	netip.MustParsePrefix("240.0.0.0/4"),     // 保留（含 255.255.255.255）
	netip.MustParsePrefix("64:ff9b:1::/48"),  // 本地 NAT64
	netip.MustParsePrefix("100::/64"),        // 丢弃
	netip.MustParsePrefix("2001::/32"),       // Teredo
	netip.MustParsePrefix("2001:db8::/32"),   // 文档示例
	netip.MustParsePrefix("fec0::/10"),       // 已废弃的站点本地
}

// 内嵌 IPv4 的 IPv6 地址段，按内嵌的 IPv4 判断。
var (
	nat64Prefix = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour   = netip.MustParsePrefix("2002::/16")
)

// Check 校验一个 IP；被禁止时返回 *BlockedError。
func (p Policy) Check(ip netip.Addr) error {
	if !ip.IsValid() {
		return &BlockedError{Addr: ip}
	}
	addr := ip.WithZone("").Unmap()
	if addr.IsUnspecified() || addr.IsMulticast() || addr == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return &BlockedError{Addr: ip}
	}
	if p.AllowPrivate {
		return nil
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() {
		return &BlockedError{Addr: ip}
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(addr) {
			return &BlockedError{Addr: ip}
		}
	}
	if embedded, ok := embeddedIPv4(addr); ok {
		if err := p.Check(embedded); err != nil {
			return &BlockedError{Addr: ip}
		}
	}
	return nil
}

// embeddedIPv4 取出 NAT64（64:ff9b::/96，末 32 位）与 6to4（2002::/16，第 16–48 位）地址中的 IPv4。
func embeddedIPv4(addr netip.Addr) (netip.Addr, bool) {
	b := addr.As16()
	switch {
	case nat64Prefix.Contains(addr):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case sixToFour.Contains(addr):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	}
	return netip.Addr{}, false
}

// Resolver 是 *net.Resolver 的子集，测试可替换。
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Resolve 解析 host 并校验全部结果：IP 字面量直接校验；域名的任一解析结果被禁止时整体拒绝
// （同时解析到公网与内网的域名多半是在试探）。r 为 nil 时用 net.DefaultResolver。
// IPv4 映射地址（::ffff:a.b.c.d，macOS 的系统解析器会这样返回 IPv4）还原为 IPv4，按 IPv4 连接；重复的地址去掉。
func (p Policy) Resolve(ctx context.Context, r Resolver, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		if err := p.Check(ip); err != nil {
			return nil, err
		}
		return []netip.Addr{ip.Unmap()}, nil
	}
	if r == nil {
		r = net.DefaultResolver
	}
	ips, err := r.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, &net.DNSError{Err: "没有解析结果", Name: host, IsNotFound: true}
	}
	out := make([]netip.Addr, 0, len(ips))
	for _, ip := range ips {
		if err := p.Check(ip); err != nil {
			return nil, err
		}
		if ip = ip.Unmap(); !slices.Contains(out, ip) {
			out = append(out, ip)
		}
	}
	return out, nil
}

// dialTimeout 是出站连接每个地址的拨号超时。
const dialTimeout = 10 * time.Second

// DialContext 返回一个拨号函数：解析 host 并校验全部 IP（按 Policy），只连校验过的 IP、不再重新解析
// （防 DNS 重绑定，docs/12 第 9 节）。用于给 S3 / HTTP 出站的 http.Transport.DialContext。
// r 为 nil 时用 net.DefaultResolver；TLS 的 SNI 与证书校验仍用原域名（由 http.Transport 负责）。
func (p Policy) DialContext(r Resolver) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := &net.Dialer{Timeout: dialTimeout}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips, err := p.Resolve(ctx, r, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, ip := range ips {
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = &net.AddrError{Err: "没有可用地址", Addr: host}
		}
		return nil, lastErr
	}
}
