package envelope

import (
	"bytes"
	"errors"
	"testing"
)

const (
	secret1 = "0123456789abcdef0123456789abcdef" // 32 字符
	secret2 = "fedcba9876543210fedcba9876543210"
	testKEK = "k1:" + secret1
	testAAD = "storage_provider|credential_enc|1"
)

func mustKeyring(t *testing.T, spec string) *Keyring {
	t.Helper()
	kr, err := ParseKeyring(spec, "xsite-test/v1")
	if err != nil {
		t.Fatalf("ParseKeyring(%q): %v", spec, err)
	}
	return kr
}

func TestSealOpenRoundTrip(t *testing.T) {
	kr := mustKeyring(t, testKEK)
	aad := []byte(testAAD)
	cases := map[string][]byte{
		"空明文":  {},
		"短明文":  []byte("sk-abcd"),
		"长明文":  bytes.Repeat([]byte("x"), 4096),
		"含零字节": {0, 1, 2, 0, 3},
	}
	for name, plain := range cases {
		t.Run(name, func(t *testing.T) {
			ct := kr.Seal(aad, plain)
			if bytes.Contains(ct, plain) && len(plain) > 4 {
				t.Fatal("密文中不应出现明文")
			}
			got, err := kr.Open(aad, ct)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if !bytes.Equal(got, plain) {
				t.Fatalf("round-trip 不一致: got %x want %x", got, plain)
			}
		})
	}
}

func TestOpenWrongAAD(t *testing.T) {
	kr := mustKeyring(t, testKEK)
	ct := kr.Seal([]byte(testAAD), []byte("secret"))
	if _, err := kr.Open([]byte("storage_provider|credential_enc|2"), ct); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("AAD 不一致应返回 ErrCorrupt，得到 %v", err)
	}
}

func TestOpenTampered(t *testing.T) {
	kr := mustKeyring(t, testKEK)
	ct := kr.Seal([]byte(testAAD), []byte("secret"))
	ct[len(ct)-1] ^= 0xff // 篡改认证标签
	if _, err := kr.Open([]byte(testAAD), ct); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("篡改后应返回 ErrCorrupt，得到 %v", err)
	}
}

func TestOpenUnknownKEK(t *testing.T) {
	enc := mustKeyring(t, "k1:"+secret1)
	ct := enc.Seal([]byte(testAAD), []byte("secret"))
	other := mustKeyring(t, "k2:"+secret2)
	if _, err := other.Open([]byte(testAAD), ct); !errors.Is(err, ErrUnknownKEK) {
		t.Fatalf("未知 kid 应返回 ErrUnknownKEK，得到 %v", err)
	}
}

func TestOpenMalformed(t *testing.T) {
	kr := mustKeyring(t, testKEK)
	for _, ct := range [][]byte{nil, []byte("nocolon"), []byte(":abc"), []byte("k1:short")} {
		if _, err := kr.Open([]byte(testAAD), ct); !errors.Is(err, ErrCorrupt) && !errors.Is(err, ErrUnknownKEK) {
			t.Fatalf("畸形密文 %q 应返回 ErrCorrupt/ErrUnknownKEK，得到 %v", ct, err)
		}
	}
}

// TestRotation：新密钥环把旧 KEK 留在后面，仍能解开旧密文；加密用的是第一把。
func TestRotation(t *testing.T) {
	old := mustKeyring(t, "k1:"+secret1)
	ctOld := old.Seal([]byte(testAAD), []byte("old-secret"))

	rotated := mustKeyring(t, "k2:"+secret2+",k1:"+secret1)
	if rotated.CurrentKID() != "k2" {
		t.Fatalf("当前 KEK 应为 k2，得到 %s", rotated.CurrentKID())
	}
	got, err := rotated.Open([]byte(testAAD), ctOld)
	if err != nil || string(got) != "old-secret" {
		t.Fatalf("轮换后应能解开旧密文: got %q err %v", got, err)
	}
	// 新密文用 k2 加密
	ctNew := rotated.Seal([]byte(testAAD), []byte("new-secret"))
	if i := bytes.IndexByte(ctNew, ':'); string(ctNew[:i]) != "k2" {
		t.Fatalf("新密文应由 k2 加密，kid=%s", ctNew[:i])
	}
}

func TestParseKeyringErrors(t *testing.T) {
	cases := map[string]string{
		"空":         "",
		"没有冒号":      "k1",
		"secret 过短": "k1:short",
		"kid 过长":    "0123456789abcdefg:" + secret1,
		"kid 非法":    "k-1:" + secret1,
		"重复 kid":    "k1:" + secret1 + ",k1:" + secret2,
	}
	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseKeyring(spec, "xsite-test/v1"); err == nil {
				t.Fatalf("spec %q 应报错", spec)
			}
		})
	}
	if _, err := ParseKeyring(testKEK, ""); err == nil {
		t.Fatal("info 为空应报错")
	}
}
