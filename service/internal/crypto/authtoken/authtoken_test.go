package authtoken

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	secret1 = "0123456789abcdef0123456789abcdef"  // 32 字符
	secret2 = "fedcba9876543210fedcba9876543210X" // 另一把
)

var testNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func mustKeyring(t *testing.T, spec string) *Keyring {
	t.Helper()
	kr, err := ParseKeyring(spec)
	if err != nil {
		t.Fatalf("解析密钥环：%v", err)
	}
	return kr
}

func sampleClaims() Claims {
	return Claims{
		Subject: "0192f1a2-7c3d-7e4f-8a9b-0c1d2e3f4a5b", SessionID: "0192f1a2-7c3d-7e4f-8a9b-0c1d2e3f4a5c",
		Role: RoleGuest, IssuedAt: testNow, ExpiresAt: testNow.Add(15 * time.Minute),
	}
}

func TestSignVerify_RoundTrip(t *testing.T) {
	kr := mustKeyring(t, "k1:"+secret1)
	tok, err := kr.Sign(sampleClaims())
	if err != nil {
		t.Fatal(err)
	}
	got, err := kr.Verify(tok, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != sampleClaims().Subject || got.SessionID != sampleClaims().SessionID ||
		got.Role != RoleGuest || !got.ExpiresAt.Equal(testNow.Add(15*time.Minute)) {
		t.Fatalf("载荷不一致：%+v", got)
	}
	// 是标准 JWT：三段、头部 alg=EdDSA/typ=JWT/kid=k1
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT 应为三段，得到 %d", len(parts))
	}
	hdrRaw, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var hdr map[string]any
	if err := json.Unmarshal(hdrRaw, &hdr); err != nil {
		t.Fatal(err)
	}
	if hdr["alg"] != "EdDSA" || hdr["typ"] != "JWT" || hdr["kid"] != "k1" {
		t.Fatalf("头部不对：%v", hdr)
	}
}

func TestVerify_Expired(t *testing.T) {
	kr := mustKeyring(t, "k1:"+secret1)
	tok, _ := kr.Sign(sampleClaims())
	if _, err := kr.Verify(tok, testNow.Add(15*time.Minute)); !errors.Is(err, ErrExpired) {
		t.Fatalf("到期时刻应过期，得到 %v", err)
	}
	if _, err := kr.Verify(tok, testNow.Add(15*time.Minute+time.Second)); !errors.Is(err, ErrExpired) {
		t.Fatalf("过期应 ErrExpired，得到 %v", err)
	}
	if _, err := kr.Verify(tok, testNow.Add(14*time.Minute)); err != nil {
		t.Fatalf("未过期应通过，得到 %v", err)
	}
}

func TestVerify_RejectsTamperAndBadInput(t *testing.T) {
	kr := mustKeyring(t, "k1:"+secret1)
	tok, _ := kr.Sign(sampleClaims())
	parts := strings.Split(tok, ".")

	// 改载荷但不改签名
	forgedPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"attacker","sid":"x","role":"user","iat":1,"exp":9999999999}`))
	tampered := parts[0] + "." + forgedPayload + "." + parts[2]

	// 改签名最后一字节
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sig[len(sig)-1] ^= 0x01
	badSig := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(sig)

	for name, bad := range map[string]string{
		"空":          "",
		"只有一段":       parts[0],
		"只有两段":       parts[0] + "." + parts[1],
		"四段":         tok + ".x",
		"篡改载荷":       tampered,
		"篡改签名":       badSig,
		"头部非法base64": "!!!." + parts[1] + "." + parts[2],
		"超长":         strings.Repeat("a", maxTokenLen+1),
	} {
		if _, err := kr.Verify(bad, testNow); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s：应 ErrInvalid，得到 %v", name, err)
		}
	}
}

// 拒绝 alg=none 与算法混淆：即使签名字段能构造，也不能绕过 EdDSA 验签。
func TestVerify_RejectsAlgConfusion(t *testing.T) {
	kr := mustKeyring(t, "k1:"+secret1)
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x","sid":"y","role":"guest","iat":1,"exp":9999999999}`))

	for name, hdr := range map[string]string{
		"alg=none":  `{"alg":"none","typ":"JWT","kid":"k1"}`,
		"alg=HS256": `{"alg":"HS256","typ":"JWT","kid":"k1"}`,
		"alg 空":     `{"alg":"","typ":"JWT","kid":"k1"}`,
		"未知 kid":    `{"alg":"EdDSA","typ":"JWT","kid":"zzz"}`,
	} {
		h := base64.RawURLEncoding.EncodeToString([]byte(hdr))
		// alg=none 攻击常用空签名
		tok := h + "." + payload + "."
		if _, err := kr.Verify(tok, testNow); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s（空签名）：应拒绝，得到 %v", name, err)
		}
	}
}

// 密钥轮换：新 kid 放第一把签发，旧 kid 签发的令牌仍可校验；去掉旧 kid 后旧令牌失效。
func TestKeyRotation(t *testing.T) {
	old := mustKeyring(t, "k1:"+secret1)
	oldTok, _ := old.Sign(sampleClaims())

	rotated := mustKeyring(t, "k2:"+secret2+",k1:"+secret1) // k2 当前，k1 过渡
	if rotated.CurrentKID() != "k2" {
		t.Fatalf("当前密钥应为 k2，得到 %s", rotated.CurrentKID())
	}
	if _, err := rotated.Verify(oldTok, testNow); err != nil {
		t.Fatalf("轮换过渡期旧 kid 的令牌应仍可校验：%v", err)
	}
	newTok, _ := rotated.Sign(sampleClaims())
	if !strings.HasPrefix(decodeKid(t, newTok), "k2") {
		t.Fatal("新令牌应由 k2 签发")
	}
	// 退役 k1 后，旧令牌失效
	onlyNew := mustKeyring(t, "k2:"+secret2)
	if _, err := onlyNew.Verify(oldTok, testNow); !errors.Is(err, ErrInvalid) {
		t.Fatalf("退役 kid 的令牌应失效，得到 %v", err)
	}
	// 同一 secret 在不同实例派生出同一密钥对：另一个实例签的令牌这里也能校验
	if _, err := mustKeyring(t, "k2:"+secret2).Verify(newTok, testNow); err != nil {
		t.Fatalf("同 secret 的另一实例应能校验：%v", err)
	}
}

// 不同 kid 的密钥对必须不同（HKDF 把 secret 绑定到用途，和其它子系统的密钥也不同）。
func TestDerivedKeysAreDeterministicAndDistinct(t *testing.T) {
	a := mustKeyring(t, "k1:"+secret1)
	b := mustKeyring(t, "k1:"+secret1)
	if !a.keys["k1"].priv.Equal(b.keys["k1"].priv) {
		t.Fatal("同 secret 应派生同一私钥")
	}
	c := mustKeyring(t, "k1:"+secret2)
	if a.keys["k1"].priv.Equal(c.keys["k1"].priv) {
		t.Fatal("不同 secret 应派生不同私钥")
	}
	if ed25519.Sign(a.keys["k1"].priv, []byte("x")) == nil {
		t.Fatal("应为可用的 Ed25519 私钥")
	}
}

func TestParseKeyring_Rejects(t *testing.T) {
	for name, spec := range map[string]string{
		"空":         "",
		"没有分隔符":     "k1" + secret1,
		"kid 为空":    ":" + secret1,
		"secret 太短": "k1:short",
		"kid 太长":    strings.Repeat("k", maxKidLen+1) + ":" + secret1,
		"kid 非法字符":  "k.1:" + secret1,
		"kid 重复":    "k1:" + secret1 + ",k1:" + secret2,
	} {
		if _, err := ParseKeyring(spec); err == nil {
			t.Errorf("%s：应解析失败", name)
		}
	}
	if _, err := ParseKeyring("k1:" + secret1 + ", k2:" + secret2); err != nil {
		t.Errorf("逗号后带空格应能解析：%v", err)
	}
}

func TestSign_RejectsIncompleteClaims(t *testing.T) {
	kr := mustKeyring(t, "k1:"+secret1)
	for name, c := range map[string]Claims{
		"缺 subject": {Role: RoleGuest, IssuedAt: testNow, ExpiresAt: testNow.Add(time.Minute)},
		"缺 role":    {Subject: "x", IssuedAt: testNow, ExpiresAt: testNow.Add(time.Minute)},
		"缺 exp":     {Subject: "x", Role: RoleGuest, IssuedAt: testNow},
	} {
		if _, err := kr.Sign(c); err == nil {
			t.Errorf("%s：应拒绝签发", name)
		}
	}
}

func decodeKid(t *testing.T, tok string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(strings.SplitN(tok, ".", 2)[0])
	if err != nil {
		t.Fatal(err)
	}
	var h header
	if err := json.Unmarshal(raw, &h); err != nil {
		t.Fatal(err)
	}
	return h.Kid
}
