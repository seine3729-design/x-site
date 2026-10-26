package apienc

import (
	"bytes"
	"crypto/hpke"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

const testSecret = "0123456789abcdef0123456789abcdef-test"

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RFC 9180 官方测试向量：mode_base, DHKEM(X25519, HKDF-SHA256), HKDF-SHA256, AES-256-GCM。
// 用来确认本包选用的套件与标准一致（Web / APP 客户端的测试使用同一组向量）。
func TestRFC9180Vector(t *testing.T) {
	priv, err := kem.NewPrivateKey(mustHex(t, "497b4502664cfea5d5af0b39934dac72242a74f8480451e1aee7d6a53320333d"))
	if err != nil {
		t.Fatal(err)
	}
	enc := mustHex(t, "6c93e09869df3402d7bf231bf540fadd35cd56be14f97178f0954db94b7fc256")
	info := mustHex(t, "4f6465206f6e2061204772656369616e2055726e")
	r, err := hpke.NewRecipient(enc, priv, kdf, aead, info)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := r.Open(mustHex(t, "436f756e742d30"), mustHex(t, "e5d84cd531cfb583096e7cfa9641bd3079cf3a91cda813c52deb5f512be9931980a41de125a925cdad859d5b7a"))
	if err != nil {
		t.Fatalf("打开 RFC 向量失败: %v", err)
	}
	if want := mustHex(t, "4265617574792069732074727574682c20747275746820626561757479"); !bytes.Equal(pt, want) {
		t.Fatalf("明文不一致: %x", pt)
	}
	exp, err := r.Export("", 32)
	if err != nil {
		t.Fatal(err)
	}
	if want := mustHex(t, "ded6cffafaea6b812cbf3e241e88332adbc077aca81512914213810ee291770a"); !bytes.Equal(exp, want) {
		t.Fatalf("导出值不一致: %x", exp)
	}
}

func testKeyring(t *testing.T) *Keyring {
	t.Helper()
	kr, err := ParseKeyring("k2:" + testSecret + "-new, k1:" + testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

func TestParseKeyring(t *testing.T) {
	kr := testKeyring(t)
	if kr.Current() != "k2" {
		t.Fatalf("第一把应为当前密钥，得到 %s", kr.Current())
	}
	pubs := kr.PublicKeys()
	if len(pubs) != 2 || pubs[0].Kid != "k2" || len(pubs[0].PublicKey) != encodedEncN {
		t.Fatalf("公钥列表不符合预期: %+v", pubs)
	}
	// 同一 secret 必须派生出同一把密钥，多实例部署才能互相解密
	again := testKeyring(t)
	if again.PublicKeys()[0].PublicKey != pubs[0].PublicKey {
		t.Fatal("同一 secret 派生的公钥应一致")
	}

	bad := []string{"", "k1", "k1:short", "k-1:" + testSecret, "k1:" + testSecret + ",k1:" + testSecret, strings.Repeat("a", 17) + ":" + testSecret}
	for _, spec := range bad {
		if _, err := ParseKeyring(spec); err == nil {
			t.Errorf("期望拒绝 %q", spec)
		}
	}
}

func TestParseHeader(t *testing.T) {
	enc := bytes.Repeat([]byte{7}, 32)
	tag := bytes.Repeat([]byte{9}, 16)
	h, err := ParseHeader(FormatHeader("k1", 1792575000, enc, tag))
	if err != nil || h.Kid != "k1" || h.TS != 1792575000 || !bytes.Equal(h.Enc, enc) || !bytes.Equal(h.Tag, tag) {
		t.Fatalf("解析失败: %+v %v", h, err)
	}
	e, g := "."+b64(enc), "."+b64(tag)
	for _, v := range []string{"", "2.k1.1" + e + g, "1..1" + e + g, "1.k1.abc" + e + g, "1.k1.-5" + e + g,
		"1.k1.1.short" + g, "1.k1.1" + e, "1.k1.1" + e + ".short", "1.k1.1" + e + g + ".extra"} {
		if _, err := ParseHeader(v); !errors.Is(err, ErrHeader) {
			t.Errorf("期望拒绝 %q，得到 %v", v, err)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	kr := testKeyring(t)
	for _, pk := range kr.PublicKeys() { // 当前密钥与过渡期旧密钥都要能解
		req, err := NewClientRequest(pk.Kid, pk.PublicKey, 100, "POST", "/api/me/history?x=1", []byte(`{"a":1}`))
		if err != nil {
			t.Fatal(err)
		}
		h, err := ParseHeader(req.Header)
		if err != nil {
			t.Fatal(err)
		}
		pt, s, err := kr.Open(h, RequestAAD("POST", "/api/me/history?x=1", 100), req.Body)
		if err != nil || string(pt) != `{"a":1}` {
			t.Fatalf("%s: 解密失败 %q %v", pk.Kid, pt, err)
		}
		sealed, err := s.SealResponse([]byte(`{"code":"OK"}`))
		if err != nil {
			t.Fatal(err)
		}
		got, err := req.Session.OpenResponse(sealed)
		if err != nil || string(got) != `{"code":"OK"}` {
			t.Fatalf("%s: 客户端解不开响应 %q %v", pk.Kid, got, err)
		}
	}
}

func TestOpen_RejectsTampering(t *testing.T) {
	kr := testKeyring(t)
	pk := kr.PublicKeys()[0]
	req, err := NewClientRequest(pk.Kid, pk.PublicKey, 100, "POST", "/api/a?x=1", []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	h, _ := ParseHeader(req.Header)

	cases := map[string]func() error{
		"改了查询串": func() error {
			_, _, err := kr.Open(h, RequestAAD("POST", "/api/a?x=2", 100), req.Body)
			return err
		},
		"改了方法": func() error {
			_, _, err := kr.Open(h, RequestAAD("PUT", "/api/a?x=1", 100), req.Body)
			return err
		},
		"改了时间戳": func() error {
			_, _, err := kr.Open(h, RequestAAD("POST", "/api/a?x=1", 101), req.Body)
			return err
		},
		"GET 请求改了查询串（只有 tag）": func() error {
			get, err := NewClientRequest(pk.Kid, pk.PublicKey, 100, "GET", "/api/a?x=1", nil)
			if err != nil {
				return err
			}
			gh, _ := ParseHeader(get.Header)
			_, _, err = kr.Open(gh, RequestAAD("GET", "/api/a?x=2", 100), nil)
			return err
		},
		"改了密文": func() error {
			body := bytes.Clone(req.Body)
			body[0] ^= 1
			_, _, err := kr.Open(h, RequestAAD("POST", "/api/a?x=1", 100), body)
			return err
		},
	}
	for name, run := range cases {
		if err := run(); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%s: 期望 ErrDecrypt，得到 %v", name, err)
		}
	}

	h.Kid = "nope"
	if _, _, err := kr.Open(h, nil, req.Body); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("未知 kid 应返回 ErrUnknownKey，得到 %v", err)
	}
}

func TestOpenResponse_RejectsShortOrTampered(t *testing.T) {
	s := &Session{respKey: bytes.Repeat([]byte{1}, 32)}
	sealed, err := s.SealResponse([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := s.OpenResponse(sealed); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("篡改后应失败，得到 %v", err)
	}
	if _, err := s.OpenResponse([]byte("short")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("过短应失败，得到 %v", err)
	}
}
