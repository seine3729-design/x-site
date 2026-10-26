package playtoken

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	secretA = "0123456789abcdef0123456789abcdef-a"
	secretB = "0123456789abcdef0123456789abcdef-b"
)

func mustKeyring(t *testing.T, spec string) *Keyring {
	t.Helper()
	kr, err := ParseKeyring(spec)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

var now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func sample() Claims {
	return Claims{EpisodeID: 42, LineID: 12, Device: DeviceHash("dev-1"), TrialSec: 30, ExpiresAt: now.Add(10 * time.Minute)}
}

func TestSignVerify_RoundTrip(t *testing.T) {
	kr := mustKeyring(t, "k1:"+secretA)
	for _, c := range []Claims{
		sample(),
		{EpisodeID: 1, LineID: 2, ExpiresAt: now.Add(time.Minute)}, // 不绑定设备、完整播放
	} {
		tok, err := kr.Sign(c)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(tok, "k1.") || strings.Count(tok, ".") != 2 {
			t.Fatalf("令牌应为 kid.载荷.签名：%s", tok)
		}
		got, err := kr.Verify(tok, now)
		if err != nil {
			t.Fatalf("签发的令牌应能校验通过：%v", err)
		}
		if got.EpisodeID != c.EpisodeID || got.LineID != c.LineID || got.Device != c.Device ||
			got.TrialSec != c.TrialSec || !got.ExpiresAt.Equal(c.ExpiresAt) {
			t.Fatalf("往返结果不一致：%+v != %+v", got, c)
		}
	}
}

func TestVerify_RejectsTampering(t *testing.T) {
	kr := mustKeyring(t, "k1:"+secretA)
	tok, err := kr.Sign(sample())
	if err != nil {
		t.Fatal(err)
	}
	// 逐个字符改动：任何一处改动都不能通过校验
	for i := range len(tok) {
		if tok[i] == '.' {
			continue
		}
		b := []byte(tok)
		if b[i] == 'A' {
			b[i] = 'B'
		} else {
			b[i] = 'A'
		}
		if _, err := kr.Verify(string(b), now); !errors.Is(err, ErrInvalid) {
			t.Fatalf("改动第 %d 个字符后应无效，得到 %v", i, err)
		}
	}

	parts := strings.Split(tok, ".")
	// 把载荷换成另一个合法载荷但保留原签名：签名不匹配
	other, _ := kr.Sign(Claims{EpisodeID: 43, LineID: 12, ExpiresAt: now.Add(time.Minute)})
	swapped := parts[0] + "." + strings.Split(other, ".")[1] + "." + parts[2]
	for name, bad := range map[string]string{
		"换载荷":   swapped,
		"少一段":   parts[0] + "." + parts[1],
		"多一段":   tok + ".x",
		"空令牌":   "",
		"超长":    strings.Repeat("a", maxTokenLen+1),
		"签名带填充": tok + "=",
	} {
		if _, err := kr.Verify(bad, now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s：应无效，得到 %v", name, err)
		}
	}
}

func TestVerify_Expiry(t *testing.T) {
	kr := mustKeyring(t, "k1:"+secretA)
	tok, _ := kr.Sign(sample())
	exp := sample().ExpiresAt
	if _, err := kr.Verify(tok, exp.Add(-time.Second)); err != nil {
		t.Fatalf("过期前一秒应有效：%v", err)
	}
	for _, at := range []time.Time{exp, exp.Add(time.Hour)} {
		if _, err := kr.Verify(tok, at); !errors.Is(err, ErrExpired) {
			t.Fatalf("%v 时应已过期，得到 %v", at, err)
		}
	}
}

func TestKeyRotation(t *testing.T) {
	old := mustKeyring(t, "k1:"+secretA)
	tok, _ := old.Sign(sample())

	// 轮换：新密钥放第一把签发，旧密钥留着校验
	rotated := mustKeyring(t, "k2:"+secretB+",k1:"+secretA)
	if rotated.CurrentKID() != "k2" {
		t.Fatalf("第一把应为当前密钥：%s", rotated.CurrentKID())
	}
	if _, err := rotated.Verify(tok, now); err != nil {
		t.Fatalf("轮换过渡期内旧密钥签发的令牌应仍有效：%v", err)
	}
	newTok, _ := rotated.Sign(sample())
	if !strings.HasPrefix(newTok, "k2.") {
		t.Fatalf("轮换后应用新密钥签发：%s", newTok)
	}
	if _, err := old.Verify(newTok, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("没有新密钥的实例不认识新令牌，得到 %v", err)
	}

	// 旧密钥删除后不再有效；同一个 kid 换了 secret 也不行
	if _, err := mustKeyring(t, "k2:"+secretB).Verify(tok, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("旧密钥删除后令牌应无效，得到 %v", err)
	}
	if _, err := mustKeyring(t, "k1:"+secretB).Verify(tok, now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("同一个 kid 换了 secret 后令牌应无效，得到 %v", err)
	}
}

// 签名正确但载荷不合法（伪造者拿到了密钥之外的情况不会发生，这里确认解析本身是严格的）。
func TestVerify_StrictPayload(t *testing.T) {
	kr := mustKeyring(t, "k1:"+secretA)
	forge := func(v any) string {
		raw, _ := json.Marshal(v)
		body := b64.EncodeToString(raw)
		return "k1." + body + "." + b64.EncodeToString(mac(kr.purpose.macDomain, kr.keys["k1"], "k1", body))
	}
	exp := now.Add(time.Minute).Unix()
	for name, v := range map[string]any{
		"未知字段":   map[string]any{"e": 1, "l": 1, "x": exp, "u": 9},
		"缺分集":    map[string]any{"l": 1, "x": exp},
		"试看为负":   map[string]any{"e": 1, "l": 1, "t": -1, "x": exp},
		"不是对象":   []int{1, 2},
		"字段类型不对": map[string]any{"e": "1", "l": 1, "x": exp},
	} {
		if _, err := kr.Verify(forge(v), now); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s：应无效，得到 %v", name, err)
		}
	}
	// 载荷后跟多余内容
	body := b64.EncodeToString([]byte(`{"e":1,"l":1,"x":` + itoa(exp) + `} {}`))
	trailing := "k1." + body + "." + b64.EncodeToString(mac(kr.purpose.macDomain, kr.keys["k1"], "k1", body))
	if _, err := kr.Verify(trailing, now); !errors.Is(err, ErrInvalid) {
		t.Errorf("载荷后有多余内容应无效，得到 %v", err)
	}
	// 对照：同样构造的合法载荷能通过，说明上面的失败来自载荷本身
	if _, err := kr.Verify(forge(map[string]any{"e": 1, "l": 1, "x": exp}), now); err != nil {
		t.Fatalf("合法载荷应通过：%v", err)
	}
}

func TestSign_RejectsIncompleteClaims(t *testing.T) {
	kr := mustKeyring(t, "k1:"+secretA)
	for _, c := range []Claims{
		{LineID: 1, ExpiresAt: now},
		{EpisodeID: 1, ExpiresAt: now},
		{EpisodeID: 1, LineID: 1},
		{EpisodeID: 1, LineID: 1, TrialSec: -1, ExpiresAt: now},
	} {
		if _, err := kr.Sign(c); err == nil {
			t.Errorf("%+v 应签发失败", c)
		}
	}
}

func TestDeviceBinding(t *testing.T) {
	if DeviceHash("") != "" {
		t.Fatal("没有设备标识时不绑定设备")
	}
	h := DeviceHash("dev-1")
	if h == "" || h != DeviceHash("dev-1") || h == DeviceHash("dev-2") || strings.Contains(h, "dev-1") {
		t.Fatalf("设备哈希应稳定、区分设备、不含原文：%q", h)
	}
	bound := Claims{Device: h}
	if !bound.BoundTo("dev-1") || bound.BoundTo("dev-2") || bound.BoundTo("") {
		t.Fatal("绑定设备的令牌只能给该设备使用")
	}
	if !(Claims{}).BoundTo("any") || !(Claims{}).BoundTo("") {
		t.Fatal("没有绑定设备的令牌不限设备")
	}
}

func TestParseKeyring(t *testing.T) {
	kr := mustKeyring(t, " k1:"+secretA+" , k2:"+secretB+" ,")
	if kr.CurrentKID() != "k1" || len(kr.keys) != 2 {
		t.Fatalf("解析结果不对：%s %d", kr.CurrentKID(), len(kr.keys))
	}
	for name, spec := range map[string]string{
		"空":         "",
		"只有逗号":      " , ",
		"缺冒号":       "k1" + secretA,
		"kid 非法字符":  "k-1:" + secretA,
		"kid 太长":    strings.Repeat("k", maxKidLen+1) + ":" + secretA,
		"secret 太短": "k1:short",
		"kid 重复":    "k1:" + secretA + ",k1:" + secretB,
	} {
		if _, err := ParseKeyring(spec); err == nil {
			t.Errorf("%s：应解析失败", name)
		}
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// 前台令牌与后台试播令牌互不通用（docs/06 第 37 节：后台试播要与前台不通用的预览令牌）。
func TestPurposes_AreNotInterchangeable(t *testing.T) {
	spec := "k1:" + secretA
	front, err := ParseKeyringFor(spec, Playback)
	if err != nil {
		t.Fatalf("前台密钥环：%v", err)
	}
	preview, err := ParseKeyringFor(spec, AdminPreview)
	if err != nil {
		t.Fatalf("后台密钥环：%v", err)
	}
	// 同一份 secret、同一个 kid，派生出的密钥必须不同
	if bytes.Equal(front.keys["k1"], preview.keys["k1"]) {
		t.Fatal("两种用途不能派生出同一把密钥")
	}
	claims := Claims{EpisodeID: 7, LineID: 3, ExpiresAt: now.Add(time.Minute)}
	frontTok, err := front.Sign(claims)
	if err != nil {
		t.Fatalf("前台签发：%v", err)
	}
	previewTok, err := preview.Sign(claims)
	if err != nil {
		t.Fatalf("后台签发：%v", err)
	}
	if frontTok == previewTok {
		t.Fatal("两种用途不能签出同一个令牌")
	}
	if _, err := preview.Verify(frontTok, now); !errors.Is(err, ErrInvalid) {
		t.Errorf("后台密钥环不该认前台令牌，得到 %v", err)
	}
	if _, err := front.Verify(previewTok, now); !errors.Is(err, ErrInvalid) {
		t.Errorf("前台密钥环不该认后台令牌，得到 %v", err)
	}
	// 各自仍能验自己的
	if _, err := front.Verify(frontTok, now); err != nil {
		t.Errorf("前台令牌应通过前台校验：%v", err)
	}
	if _, err := preview.Verify(previewTok, now); err != nil {
		t.Errorf("后台令牌应通过后台校验：%v", err)
	}
}

// 没指定用途的零值 Purpose 要被拒，避免派生出意外的密钥。
func TestParseKeyringFor_RejectsZeroPurpose(t *testing.T) {
	if _, err := ParseKeyringFor("k1:"+secretA, Purpose{}); err == nil {
		t.Fatal("零值用途应报错")
	}
}
