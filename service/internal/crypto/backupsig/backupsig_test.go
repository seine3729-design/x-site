package backupsig

import (
	"errors"
	"strings"
	"testing"
)

const (
	secretA = "0123456789abcdef0123456789abcdef"
	secretB = "fedcba9876543210fedcba9876543210"
)

func TestParseKeyring(t *testing.T) {
	bad := []struct {
		spec, why string
	}{
		{"k1", "没有冒号"},
		{"k1:", "缺密钥"},
		{":secret0123456789abcdef0123456789", "缺标识"},
		{"k1:short", "密钥太短"},
		{"k 1:" + secretA, "标识含空格"},
		{"k1:" + secretA + ",k1:" + secretB, "标识重复"},
		{strings.Repeat("x", 17) + ":" + secretA, "标识超过 16 位"},
	}
	for _, c := range bad {
		if _, err := ParseKeyring(c.spec); err == nil {
			t.Errorf("%s 应当报错：%q", c.why, c.spec)
		}
	}

	// 空串是允许的：本地开发不配密钥
	kr, err := ParseKeyring("")
	if err != nil {
		t.Fatalf("空串应当返回空密钥环: %v", err)
	}
	if kr.Enabled() || kr.CurrentKID() != "" {
		t.Error("空密钥环的 Enabled 应为 false")
	}
	if _, err := kr.Sign(1, []byte("x")); !errors.Is(err, ErrNoKeys) {
		t.Errorf("空密钥环签名应返回 ErrNoKeys，得到 %v", err)
	}
	if err := kr.Verify(1, []byte("x"), "k1:sig"); !errors.Is(err, ErrNoKeys) {
		t.Errorf("空密钥环校验应返回 ErrNoKeys，得到 %v", err)
	}

	// 第一把是签名密钥
	kr, err = ParseKeyring("new:" + secretA + ", old:" + secretB)
	if err != nil {
		t.Fatal(err)
	}
	if kr.CurrentKID() != "new" {
		t.Errorf("签名密钥 = %q，期望 new", kr.CurrentKID())
	}
}

func TestSignVerify_RoundTrip(t *testing.T) {
	kr, _ := ParseKeyring("k1:" + secretA)
	payload := []byte(`{"taxonomies":[],"ads":[]}`)
	sig, err := kr.Sign(1, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sig, "k1:") {
		t.Errorf("签名应带 kid 前缀：%s", sig)
	}
	if err := kr.Verify(1, payload, sig); err != nil {
		t.Errorf("往返校验失败: %v", err)
	}
}

// 改一个字节就应该校验失败——这是「勿手改备份」的实际防线。
func TestVerify_TamperedPayload(t *testing.T) {
	kr, _ := ParseKeyring("k1:" + secretA)
	payload := []byte(`{"ads":[{"weight":1}]}`)
	sig, _ := kr.Sign(1, payload)

	for i := range payload {
		tampered := make([]byte, len(payload))
		copy(tampered, payload)
		tampered[i] ^= 0x01
		if err := kr.Verify(1, tampered, sig); err == nil {
			t.Fatalf("第 %d 字节被改动后仍校验通过", i)
		}
	}
}

// 只改格式版本号也要失败，否则可以把新备份伪装成旧版本去走旧的导入分支。
func TestVerify_TamperedFormat(t *testing.T) {
	kr, _ := ParseKeyring("k1:" + secretA)
	payload := []byte(`{"ads":[]}`)
	sig, _ := kr.Sign(2, payload)
	if err := kr.Verify(1, payload, sig); !errors.Is(err, ErrBadSignature) {
		t.Errorf("改格式版本后应当校验失败，得到 %v", err)
	}
}

// 版本与正文之间有分隔符：拼接歧义不能造成签名碰撞。
func TestSign_NoConcatenationAmbiguity(t *testing.T) {
	kr, _ := ParseKeyring("k1:" + secretA)
	a, _ := kr.Sign(1, []byte("23"))
	b, _ := kr.Sign(12, []byte("3"))
	if a == b {
		t.Error("「版本 1 + 正文 23」与「版本 12 + 正文 3」算出了同一个签名")
	}
}

func TestVerify_BadFormat(t *testing.T) {
	kr, _ := ParseKeyring("k1:" + secretA)
	payload := []byte(`{}`)
	for _, sig := range []string{"", "nosep", "k1:", ":abc"} {
		if err := kr.Verify(1, payload, sig); !errors.Is(err, ErrBadSignature) {
			t.Errorf("签名 %q 应当报 ErrBadSignature，得到 %v", sig, err)
		}
	}
}

// 密钥轮换：旧密钥签的备份在两把密钥都在时仍可校验；旧密钥被移除后拒绝。
func TestVerify_KeyRotation(t *testing.T) {
	payload := []byte(`{"ads":[]}`)
	oldKr, _ := ParseKeyring("old:" + secretB)
	sig, _ := oldKr.Sign(1, payload)

	both, _ := ParseKeyring("new:" + secretA + ",old:" + secretB)
	if err := both.Verify(1, payload, sig); err != nil {
		t.Errorf("轮换期间旧签名应当仍可校验: %v", err)
	}
	// 新签的签名用新 kid
	newSig, _ := both.Sign(1, payload)
	if !strings.HasPrefix(newSig, "new:") {
		t.Errorf("新签名应当用第一把密钥：%s", newSig)
	}

	onlyNew, _ := ParseKeyring("new:" + secretA)
	err := onlyNew.Verify(1, payload, sig)
	if !errors.Is(err, ErrBadSignature) {
		t.Errorf("移除旧密钥后应当拒绝旧签名，得到 %v", err)
	}
	if !strings.Contains(err.Error(), "old") {
		t.Errorf("报错应当指出是哪个密钥标识：%v", err)
	}
}

// 同一个密钥串在别处（如 LINE_SIGN_KEYS）派生出的子密钥与这里不同：HKDF 的域分隔。
func TestKeyring_DomainSeparation(t *testing.T) {
	kr, _ := ParseKeyring("k1:" + secretA)
	derived := kr.keys["k1"]
	if string(derived) == secretA {
		t.Error("密钥没有经过 HKDF 派生，直接用了原始密钥串")
	}
	if len(derived) != 32 {
		t.Errorf("派生密钥长度 = %d，期望 32", len(derived))
	}
}
