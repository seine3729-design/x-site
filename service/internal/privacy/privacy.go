// Package privacy 提供数据最小化工具（docs/10 第 1 节）：IP 只保留网段前缀，User-Agent 只保存摘要。
// 写库、写日志前用这里的函数处理，原始 IP 与 UA 只在本次请求内使用。
package privacy

import (
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
)

// IPPrefix 返回 IP 所在网段：IPv4 保留前两段（/16），IPv6 保留前 32 位；无法解析时返回空串。
func IPPrefix(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	addr = addr.Unmap().WithZone("")
	bits := 32
	if addr.Is4() {
		bits = 16
	}
	p, err := addr.Prefix(bits)
	if err != nil {
		return ""
	}
	return p.String()
}

// UADigest 返回 User-Agent 的 SHA-256 摘要前 16 字节（十六进制），用于会话绑定与登录记录，不保存原文。
func UADigest(ua string) string {
	sum := sha256.Sum256([]byte(ua))
	return hex.EncodeToString(sum[:16])
}
