package privacy

import "testing"

func TestIPPrefix(t *testing.T) {
	tests := []struct {
		ip, want string
	}{
		{"203.0.113.7", "203.0.0.0/16"},
		{"::ffff:203.0.113.7", "203.0.0.0/16"},
		{"2001:db8:1234:5678::1", "2001:db8::/32"},
		{"fe80::1%eth0", "fe80::/32"},
		{"", ""},
		{"not-an-ip", ""},
	}
	for _, tt := range tests {
		if got := IPPrefix(tt.ip); got != tt.want {
			t.Errorf("IPPrefix(%q) = %q，期望 %q", tt.ip, got, tt.want)
		}
	}
}

func TestUADigest(t *testing.T) {
	a := UADigest("Mozilla/5.0 A")
	if len(a) != 32 {
		t.Fatalf("摘要应为 32 个十六进制字符，得到 %q", a)
	}
	if a != UADigest("Mozilla/5.0 A") {
		t.Fatal("同一 UA 的摘要应稳定")
	}
	if a == UADigest("Mozilla/5.0 B") {
		t.Fatal("不同 UA 的摘要应不同")
	}
}
