package main

import (
	"bytes"
	"strings"
	"testing"

	"xsite/service/internal/crypto/mediakey"
	"xsite/service/internal/store/pg"
)

const (
	rewrapSecretLocal = "llllllllllllllllllllllllllllllll"
	rewrapSecretProd  = "pppppppppppppppppppppppppppppppp"
)

func mustRewrapKeyring(t *testing.T, spec string) *mediakey.Keyring {
	t.Helper()
	kr, err := mediakey.ParseKeyring(spec)
	if err != nil {
		t.Fatal(err)
	}
	return kr
}

// 导入线上后的典型数据：两把本地 KEK（dev1）包装的旧密钥、一把线上新转码的（k1）、一把谁也解不开的。
func TestPlanRewrapCountsAndChanges(t *testing.T) {
	local := mustRewrapKeyring(t, "dev1:"+rewrapSecretLocal)
	prod := mustRewrapKeyring(t, "k1:"+rewrapSecretProd)
	stranger := mustRewrapKeyring(t, "old9:"+strings.Repeat("s", 32))
	wrap := func(kr *mediakey.Keyring, ep int64, ver int32) pg.WrappedMediaKey {
		w, err := kr.Wrap(ep, ver, mediakey.NewContentKey())
		if err != nil {
			t.Fatal(err)
		}
		return pg.WrappedMediaKey{EpisodeID: ep, KeyVer: ver, Wrapped: w}
	}
	rows := []pg.WrappedMediaKey{wrap(local, 1, 1), wrap(local, 1, 2), wrap(prod, 2, 1), wrap(stranger, 3, 1)}

	plan := planRewrap(rows, prod, local)
	if plan.Total != 4 || plan.Current != 1 || len(plan.Changes) != 2 || len(plan.Failures) != 1 || plan.ByKID["dev1"] != 2 {
		t.Fatalf("计划不对：%+v", plan)
	}
	if f := plan.Failures[0]; f.EpisodeID != 3 || f.KID != "old9" {
		t.Fatalf("解不开的应是第 3 集（kid old9）：%+v", f)
	}
	for _, c := range plan.Changes {
		if !bytes.Equal(c.Old, rows[c.KeyVer-1].Wrapped) || mediakey.KIDOf(c.New) != "k1" {
			t.Fatalf("替换项应带读出时的原密文、新密文用 k1：%+v", c)
		}
		if _, err := prod.Unwrap(c.EpisodeID, c.KeyVer, c.New); err != nil {
			t.Fatalf("新密文只用线上密钥环应能解开：%v", err)
		}
	}

	// 执行后的复查：不给旧 KEK，已替换的都算「当前」，剩下的只有原本就解不开的那一把
	after := []pg.WrappedMediaKey{
		{EpisodeID: 1, KeyVer: 1, Wrapped: plan.Changes[0].New}, {EpisodeID: 1, KeyVer: 2, Wrapped: plan.Changes[1].New}, rows[2], rows[3],
	}
	check := planRewrap(after, prod, nil)
	if check.Current != 3 || len(check.Changes) != 0 || len(check.Failures) != 1 {
		t.Fatalf("复查结果不对：%+v", check)
	}
}

func TestPrintRewrapPlanListsAtMostTenFailures(t *testing.T) {
	plan := rewrapPlan{Total: 12, ByKID: map[string]int{}}
	for i := range 12 {
		plan.Failures = append(plan.Failures, rewrapFailure{EpisodeID: int64(i + 1), KeyVer: 1, KID: "dev1", Err: mediakey.ErrUnknownKEK})
	}
	var out bytes.Buffer
	printRewrapPlan(&out, plan, "k1")
	text := out.String()
	if !strings.Contains(text, "解不开：12 把") || !strings.Contains(text, "第 10 集第 1 版") || strings.Contains(text, "第 11 集") ||
		!strings.Contains(text, "另有 2 把") {
		t.Fatalf("输出不符合预期：\n%s", text)
	}
}

// 参数与密钥环在连库之前就校验：格式不对直接报错，不会去碰数据库。
func TestRewrapMediaKeysRejectsBadKeyrings(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://invalid.invalid:1/none")
	for _, tc := range []struct {
		name, kek, stdin, want string
	}{
		{"没配 MEDIA_KEKS", "", "\n", "MEDIA_KEKS"},
		{"MEDIA_KEKS 太短", "k1:short", "\n", "MEDIA_KEKS"},
		{"旧 KEK 格式不对", "k1:" + rewrapSecretProd, "dev1\n", "旧 KEK"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MEDIA_KEKS", tc.kek)
			err := rewrapMediaKeys([]string{"rewrap-media-keys"}, strings.NewReader(tc.stdin), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("应报「%s」相关错误，得到 %v", tc.want, err)
			}
		})
	}
	if err := rewrapMediaKeys([]string{"rewrap-media-keys", "extra"}, strings.NewReader("\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("多余参数应报用法错误")
	}
}
