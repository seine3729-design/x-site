package mediakey

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const secretA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const secretB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestParseKeyring(t *testing.T) {
	kr, err := ParseKeyring(" k2:" + secretB + " , k1:" + secretA)
	if err != nil {
		t.Fatal(err)
	}
	if kr.CurrentKID() != "k2" {
		t.Fatalf("第一把为当前 KEK，得到 %s", kr.CurrentKID())
	}
	for _, bad := range []string{"", " , ", "k1", "k-1:" + secretA, "k1:short", "k1:" + secretA + ",k1:" + secretB,
		strings.Repeat("k", 17) + ":" + secretA} {
		if _, err := ParseKeyring(bad); err == nil {
			t.Errorf("MEDIA_KEKS=%q 应解析失败", bad)
		}
	}
}

func TestWrapUnwrap(t *testing.T) {
	kr, err := ParseKeyring("k1:" + secretA)
	if err != nil {
		t.Fatal(err)
	}
	key := NewContentKey()
	if len(key) != KeySize || bytes.Equal(key, NewContentKey()) {
		t.Fatal("内容密钥应为 16 字节随机数")
	}
	w, err := kr.Wrap(42, 3, key)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(w, []byte("k1:")) || bytes.Contains(w, key) {
		t.Fatal("密文应以 kid 开头且不含明文密钥")
	}
	got, err := kr.Unwrap(42, 3, w)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("解开失败：%v", err)
	}
	if w2, _ := kr.Wrap(42, 3, key); bytes.Equal(w, w2) {
		t.Fatal("每次包装使用随机 nonce，密文不应相同")
	}

	// AAD 绑定分集与版本：挪到其他分集或版本上无法解开
	if _, err := kr.Unwrap(43, 3, w); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("换分集应失败，得到 %v", err)
	}
	if _, err := kr.Unwrap(42, 4, w); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("换版本应失败，得到 %v", err)
	}
	tampered := bytes.Clone(w)
	tampered[len(tampered)-1] ^= 1
	if _, err := kr.Unwrap(42, 3, tampered); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("篡改后应失败，得到 %v", err)
	}
	if _, err := kr.Unwrap(42, 3, w[:len(w)-1]); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("截断后应失败，得到 %v", err)
	}
	if _, err := kr.Wrap(42, 3, key[:15]); err == nil {
		t.Fatal("内容密钥长度不对应拒绝")
	}
}

func TestUnwrap_Rotation(t *testing.T) {
	old, _ := ParseKeyring("k1:" + secretA)
	w, _ := old.Wrap(1, 1, NewContentKey())

	// 新 KEK 上线为第一把，旧 KEK 留在后面：旧密文仍能解开，新密文用新 KEK
	rotated, err := ParseKeyring("k2:" + secretB + ",k1:" + secretA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rotated.Unwrap(1, 1, w); err != nil {
		t.Fatalf("过渡期应能解开旧密文：%v", err)
	}
	w2, _ := rotated.Wrap(1, 2, NewContentKey())
	if !bytes.HasPrefix(w2, []byte("k2:")) {
		t.Fatal("新密钥应使用当前 KEK")
	}

	// 删除旧 KEK 后旧密文报 ErrUnknownKEK；同一 kid 换了 secret 报 ErrCorrupt
	onlyNew, _ := ParseKeyring("k2:" + secretB)
	if _, err := onlyNew.Unwrap(1, 1, w); !errors.Is(err, ErrUnknownKEK) {
		t.Fatalf("旧 KEK 已删除应为 ErrUnknownKEK，得到 %v", err)
	}
	wrongSecret, _ := ParseKeyring("k1:" + secretB)
	if _, err := wrongSecret.Unwrap(1, 1, w); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("secret 不同应为 ErrCorrupt，得到 %v", err)
	}
	if _, err := rotated.Unwrap(1, 1, []byte("garbage")); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("格式错误应为 ErrCorrupt，得到 %v", err)
	}
}

func TestKIDOf(t *testing.T) {
	kr, _ := ParseKeyring("dev1:" + secretA)
	w, _ := kr.Wrap(1, 1, NewContentKey())
	if got := KIDOf(w); got != "dev1" {
		t.Fatalf("KIDOf = %q，want dev1", got)
	}
	for _, bad := range [][]byte{nil, []byte("nocolon"), []byte(":abc"), []byte(strings.Repeat("k", 17) + ":x")} {
		if got := KIDOf(bad); got != "" {
			t.Errorf("KIDOf(%q) = %q，want 空串", bad, got)
		}
	}
}

// 本地数据导入线上：密文是本地 KEK（dev1）包装的，线上只有自己的 KEK（k1）。
// Rewrap 用 fallback 里的旧 KEK 解开、改用线上当前 KEK 包装，之后只靠线上密钥环就能解开。
func TestRewrap(t *testing.T) {
	const secretC = "cccccccccccccccccccccccccccccccc"
	local, _ := ParseKeyring("dev1:" + secretA)
	prod, _ := ParseKeyring("k1:" + secretB)
	prodWithOld, _ := ParseKeyring("k1:" + secretB + ",dev1:" + secretA)
	sameKidOtherSecret, _ := ParseKeyring("dev1:" + secretC) // 线上也叫 dev1，但 secret 不同

	key := NewContentKey()
	fromLocal, _ := local.Wrap(7, 2, key)
	fromProd, _ := prod.Wrap(7, 2, key)

	for _, tc := range []struct {
		name     string
		target   *Keyring
		fallback *Keyring
		wrapped  []byte
		changed  bool
		wantErr  error
	}{
		{"已是线上当前 KEK：不用改", prod, local, fromProd, false, nil},
		{"旧 KEK 从标准输入给（fallback）", prod, local, fromLocal, true, nil},
		{"旧 KEK 追加在 MEDIA_KEKS 后面", prodWithOld, nil, fromLocal, true, nil},
		{"两边 kid 同名、secret 不同：交给 fallback", sameKidOtherSecret, local, fromLocal, true, nil},
		{"没有旧 KEK：解不开", prod, nil, fromLocal, false, ErrUnknownKEK},
		{"给错旧 KEK：解不开", prod, sameKidOtherSecret, fromLocal, false, ErrCorrupt},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, changed, err := Rewrap(tc.target, tc.fallback, 7, 2, tc.wrapped)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) || changed || out != nil {
					t.Fatalf("应返回 %v 且不产出新密文，得到 changed=%v err=%v", tc.wantErr, changed, err)
				}
				return
			}
			if err != nil || changed != tc.changed {
				t.Fatalf("changed=%v err=%v，want changed=%v", changed, err, tc.changed)
			}
			if !changed {
				if out != nil {
					t.Fatal("不用改时不应产出新密文")
				}
				return
			}
			if KIDOf(out) != tc.target.CurrentKID() {
				t.Fatalf("新密文应使用当前 KEK %s，得到 %s", tc.target.CurrentKID(), KIDOf(out))
			}
			// 只留线上当前那一把，也能解开且明文不变
			onlyCurrent, _ := ParseKeyring(map[string]string{"k1": "k1:" + secretB, "dev1": "dev1:" + secretC}[tc.target.CurrentKID()])
			got, err := onlyCurrent.Unwrap(7, 2, out)
			if err != nil || !bytes.Equal(got, key) {
				t.Fatalf("只用线上当前 KEK 解开失败或明文变了：%v", err)
			}
			// AAD 仍绑定分集与版本
			if _, err := onlyCurrent.Unwrap(8, 2, out); !errors.Is(err, ErrCorrupt) {
				t.Fatalf("新密文挪到其他分集应解不开，得到 %v", err)
			}
		})
	}
}
