package netguard

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"testing"
)

func TestPolicy_Check(t *testing.T) {
	tests := []struct {
		ip           string
		strict, open bool // 期望允许：严格策略 / 开发环境放开内网
	}{
		{"8.8.8.8", true, true},
		{"2606:4700:4700::1111", true, true},
		{"127.0.0.1", false, true},
		{"::1", false, true},
		{"10.1.2.3", false, true},
		{"172.16.0.1", false, true},
		{"172.31.255.255", false, true},
		{"172.32.0.1", true, true},
		{"192.168.1.1", false, true},
		{"169.254.169.254", false, true}, // 云厂商元数据服务
		{"fe80::1", false, true},
		{"fe80::1%eth0", false, true},
		{"fc00::1", false, true},
		{"fd12:3456::1", false, true},
		{"100.64.0.1", false, true},
		{"0.1.2.3", false, true},
		{"192.0.2.10", false, true},
		{"198.18.0.1", false, true},
		{"240.0.0.1", false, true},
		{"2001:db8::1", false, true},
		{"::ffff:10.0.0.1", false, true},  // IPv4 映射地址按 IPv4 判断
		{"::ffff:8.8.8.8", true, true},    // 映射的公网地址
		{"64:ff9b::a00:1", false, true},   // NAT64 内嵌 10.0.0.1
		{"64:ff9b::808:808", true, true},  // NAT64 内嵌 8.8.8.8
		{"2002:c0a8:101::1", false, true}, // 6to4 内嵌 192.168.1.1
		{"0.0.0.0", false, false},         // 未指定地址始终禁止
		{"::", false, false},              //
		{"224.0.0.1", false, false},       // 组播始终禁止
		{"ff02::1", false, false},         //
		{"255.255.255.255", false, false}, // 广播始终禁止
	}
	for _, tt := range tests {
		ip := netip.MustParseAddr(tt.ip)
		if got := (Policy{}).Check(ip) == nil; got != tt.strict {
			t.Errorf("严格策略 Check(%s) 允许=%v，期望 %v", tt.ip, got, tt.strict)
		}
		if got := (Policy{AllowPrivate: true}).Check(ip) == nil; got != tt.open {
			t.Errorf("放开内网 Check(%s) 允许=%v，期望 %v", tt.ip, got, tt.open)
		}
	}
	if err := (Policy{}).Check(netip.Addr{}); !IsBlocked(err) {
		t.Fatalf("无效地址应拒绝，得到 %v", err)
	}
}

type fakeResolver struct {
	ips []netip.Addr
	err error
	got []string
}

func (f *fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	f.got = append(f.got, host)
	return f.ips, f.err
}

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, len(s))
	for i, v := range s {
		out[i] = netip.MustParseAddr(v)
	}
	return out
}

func TestPolicy_Resolve(t *testing.T) {
	ctx := context.Background()

	// IP 字面量不查 DNS；IPv6 可以带方括号
	r := &fakeResolver{}
	if got, err := (Policy{}).Resolve(ctx, r, "[2606:4700::1]"); err != nil || !slices.Equal(got, addrs("2606:4700::1")) || len(r.got) != 0 {
		t.Fatalf("IP 字面量应直接返回：%v %v %v", got, err, r.got)
	}
	if _, err := (Policy{}).Resolve(ctx, r, "10.0.0.8"); !IsBlocked(err) {
		t.Fatalf("内网 IP 字面量应拒绝，得到 %v", err)
	}

	// 任一解析结果是内网地址就整体拒绝
	r = &fakeResolver{ips: addrs("8.8.8.8", "10.0.0.8")}
	_, err := (Policy{}).Resolve(ctx, r, "mixed.example.com")
	var blocked *BlockedError
	if !errors.As(err, &blocked) || blocked.Addr != netip.MustParseAddr("10.0.0.8") {
		t.Fatalf("混有内网地址应拒绝并指出该地址，得到 %v", err)
	}
	if got, err := (Policy{AllowPrivate: true}).Resolve(ctx, r, "mixed.example.com"); err != nil || len(got) != 2 {
		t.Fatalf("开发环境放开内网后应返回全部地址：%v %v", got, err)
	}

	r = &fakeResolver{ips: addrs("203.0.114.9", "2606:4700::1")}
	if got, err := (Policy{}).Resolve(ctx, r, "ftp.example.com"); err != nil || len(got) != 2 || r.got[0] != "ftp.example.com" {
		t.Fatalf("公网域名应返回全部地址：%v %v", got, err)
	}

	// macOS 的系统解析器会把 IPv4 结果返回成 ::ffff:a.b.c.d：按 IPv4 返回（连接与显示），重复的去掉
	r = &fakeResolver{ips: addrs("::ffff:203.0.114.9", "203.0.114.9", "2606:4700::1")}
	if got, err := (Policy{}).Resolve(ctx, r, "ftp.example.com"); err != nil || !slices.Equal(got, addrs("203.0.114.9", "2606:4700::1")) {
		t.Fatalf("IPv4 映射地址应还原为 IPv4 并去重：%v %v", got, err)
	}
	if got, err := (Policy{}).Resolve(ctx, nil, "::ffff:203.0.114.9"); err != nil || !slices.Equal(got, addrs("203.0.114.9")) {
		t.Fatalf("IPv4 映射的字面量也还原为 IPv4：%v %v", got, err)
	}

	// 解析失败与没有结果
	r = &fakeResolver{err: &net.DNSError{Err: "no such host", Name: "x", IsNotFound: true}}
	if _, err := (Policy{}).Resolve(ctx, r, "missing.example.com"); err == nil || IsBlocked(err) {
		t.Fatalf("解析失败应原样返回，得到 %v", err)
	}
	var dnsErr *net.DNSError
	if _, err := (Policy{}).Resolve(ctx, &fakeResolver{}, "empty.example.com"); !errors.As(err, &dnsErr) || !dnsErr.IsNotFound {
		t.Fatalf("没有解析结果应视为域名不存在，得到 %v", err)
	}
}
