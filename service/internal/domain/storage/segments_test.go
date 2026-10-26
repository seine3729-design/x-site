package storage

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

// 清单批量签名（S1-8）：结果与逐个 SignFor 相同，能共用的只取一次。

const segPrefix = "hls/42/job1/s/"

func segKeys(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%032x.m4s", segPrefix, i)
	}
	return out
}

type countingSignRepo struct {
	*signRepo
	gets int
}

func (r *countingSignRepo) Get(ctx context.Context, id int64) (Row, error) {
	r.gets++
	return r.signRepo.Get(ctx, id)
}

type countingCreds struct {
	n    int
	cred Credential
}

func (c *countingCreds) ReadCredential(Row) (Credential, error) {
	c.n++
	return c.cred, nil
}

// batchPresigner 实现 BatchPresigner：批量签名时不应再逐个调用 PresignGet。
type batchPresigner struct {
	stubPresigner
	calls  int
	single int
	got    PresignManyParams
	short  bool // 模拟返回数量不符
}

func (b *batchPresigner) PresignGet(ctx context.Context, p PresignParams) (string, error) {
	b.single++
	return b.stubPresigner.PresignGet(ctx, p)
}

func (b *batchPresigner) PresignGetMany(_ context.Context, p PresignManyParams) ([]string, error) {
	b.calls++
	b.got = p
	out := make([]string, len(p.Keys))
	for i, k := range p.Keys {
		out[i] = "https://s3.example.com/" + k + "?X-Amz-Signature=batch"
	}
	if b.short {
		out = out[:len(out)-1]
	}
	return out, nil
}

func TestSignSegments_MatchesSignFor(t *testing.T) {
	svc := newSignService(t, &signRepo{})
	now := time.Unix(1_700_000_000, 0)
	keys := segKeys(5)
	for _, line := range []Line{
		{SignType: SignNone, BaseURL: "https://cdn.example.com/media"},
		{SignType: SignHMACToken, SignKeyRef: "cdn-main", BaseURL: "https://cdn.example.com", TokenTTL: 600},
		{SignType: SignHMACToken, SignKeyRef: "cdn-main", BaseURL: "https://cdn.example.com/"}, // token_ttl 为 0 时用默认有效期
	} {
		got, err := svc.SignSegments(context.Background(), line, segPrefix, keys, now)
		if err != nil {
			t.Fatalf("%s：%v", line.SignType, err)
		}
		if len(got) != len(keys) {
			t.Fatalf("%s：结果数量 %d", line.SignType, len(got))
		}
		for i, k := range keys {
			want, err := svc.SignFor(context.Background(), line, SignTarget{Key: k, Prefix: segPrefix}, 0, now)
			if err != nil {
				t.Fatal(err)
			}
			if got[i] != want.URL {
				t.Errorf("%s 第 %d 个：批量 %q，逐个 %q", line.SignType, i, got[i], want.URL)
			}
		}
	}

	// 目录前缀签名：同一个令牌覆盖整集切片，并且能通过边缘的校验逻辑
	line := Line{SignType: SignHMACToken, SignKeyRef: "cdn-main", BaseURL: "https://cdn.example.com", TokenTTL: 600}
	got, _ := svc.SignSegments(context.Background(), line, segPrefix, keys, now)
	for _, raw := range got {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		var exp int64
		fmt.Sscan(q.Get(SignParamExpire), &exp)
		if exp != now.Add(600*time.Second).Unix() || q.Get(SignParamPrefix) != segPrefix {
			t.Fatalf("有效期应为线路的 token_ttl、前缀为切片目录：%s", raw)
		}
		if err := VerifyHMACToken([]byte(testSecret), u.Path, q.Get(SignParamPrefix), q.Get(SignParamSig), exp, now); err != nil {
			t.Fatalf("签名应能通过校验：%v", err)
		}
	}
}

func TestSignSegments_CDNVendor(t *testing.T) {
	svc := newSignService(t, &signRepo{})
	now := time.Unix(1_700_000_000, 0)
	line := Line{SignType: SignCDNVendor, SignKeyRef: "timestamp_md5/cdn-main", BaseURL: "https://cdn.example.com", TokenTTL: 300}
	keys := segKeys(3)
	got, err := svc.SignSegments(context.Background(), line, segPrefix, keys, now)
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range got {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if u.Path != "/"+keys[i] {
			t.Fatalf("第 %d 个地址的路径 %q", i, u.Path)
		}
		// 厂商按目录前缀校验：md5(/前缀-过期时间-随机串-uid-密钥)
		parts := strings.Split(u.Query().Get("auth_key"), "-")
		if len(parts) != 4 || parts[0] != fmt.Sprint(now.Add(300*time.Second).Unix()) {
			t.Fatalf("auth_key 格式不对：%s", raw)
		}
		sum := md5.Sum([]byte(strings.Join([]string{"/" + segPrefix, parts[0], parts[1], parts[2], testSecret}, "-")))
		if parts[3] != hex.EncodeToString(sum[:]) {
			t.Fatalf("第 %d 个地址的鉴权串不对：%s", i, raw)
		}
	}

	// 未注册的厂商
	bad := line
	bad.SignKeyRef = "unknown/cdn-main"
	if _, err := svc.SignSegments(context.Background(), bad, segPrefix, keys, now); err == nil {
		t.Fatal("未注册的厂商应报错")
	}
}

func TestSignSegments_S3Presign(t *testing.T) {
	provider := Row{ID: 7, Name: "R2", Type: TypeR2, Bucket: "duanju666", Region: R2Region, PathPrefix: "media", MaxPresignTTL: 900}
	keys := segKeys(4)
	now := time.Now()
	line := Line{SignType: SignS3Presign, ProviderID: 7, TokenTTL: 3600}
	kr, _ := ParseSignKeyring("")

	// 预签名器不支持批量：逐个签，存储源与凭据只取一次
	repo := &countingSignRepo{signRepo: &signRepo{provider: provider}}
	creds := &countingCreds{cred: Credential{AccessKeyID: "ak", SecretAccessKey: "sk"}}
	ps := &stubPresigner{}
	svc := NewSignService(repo, creds, kr, ps, nil)
	got, err := svc.SignSegments(context.Background(), line, segPrefix, keys, now)
	if err != nil {
		t.Fatal(err)
	}
	if repo.gets != 1 || creds.n != 1 {
		t.Fatalf("存储源与凭据只取一次：%d / %d", repo.gets, creds.n)
	}
	for i, k := range keys {
		if !strings.HasPrefix(got[i], "https://s3.example.com/media/"+k+"?") {
			t.Fatalf("第 %d 个：对象键应拼上存储源的 path_prefix，得到 %q", i, got[i])
		}
	}
	if ps.gotTTL != 900*time.Second || ps.gotCfg.Bucket != "duanju666" {
		t.Fatalf("有效期不超过存储源上限、连接参数透传：%v %+v", ps.gotTTL, ps.gotCfg)
	}

	// 支持批量：一次调用，顺序一致
	repo = &countingSignRepo{signRepo: &signRepo{provider: provider}}
	bp := &batchPresigner{}
	svc = NewSignService(repo, creds, kr, bp, nil)
	got, err = svc.SignSegments(context.Background(), line, segPrefix, keys, now)
	if err != nil {
		t.Fatal(err)
	}
	if bp.calls != 1 || bp.single != 0 || len(bp.got.Keys) != 4 || bp.got.Keys[3] != "media/"+keys[3] || bp.got.TTL != 900*time.Second {
		t.Fatalf("应一次批量签名：%+v", bp)
	}
	if got[2] != "https://s3.example.com/media/"+keys[2]+"?X-Amz-Signature=batch" {
		t.Fatalf("结果顺序应与输入一致：%q", got[2])
	}
	bp.short = true
	if _, err := svc.SignSegments(context.Background(), line, segPrefix, keys, now); err == nil {
		t.Fatal("预签名结果数量不符时应报错")
	}

	// HTTP 源不支持预签名
	repo = &countingSignRepo{signRepo: &signRepo{provider: Row{Type: TypeCustom, Subtype: SubtypeHTTPOrigin}}}
	svc = NewSignService(repo, creds, kr, ps, nil)
	if _, err := svc.SignSegments(context.Background(), line, segPrefix, keys, now); err == nil {
		t.Fatal("HTTP 源应拒绝预签名")
	}
}

func TestSignSegments_Rejects(t *testing.T) {
	svc := newSignService(t, &signRepo{})
	now := time.Unix(1_700_000_000, 0)
	hmacLine := Line{SignType: SignHMACToken, SignKeyRef: "cdn-main", BaseURL: "https://cdn.example.com", TokenTTL: 600}
	tests := []struct {
		name   string
		line   Line
		prefix string
		keys   []string
	}{
		{"前缀不是目录", hmacLine, "hls/42/job1/s", segKeys(1)},
		{"前缀为空", hmacLine, "", segKeys(1)},
		{"对象不在前缀之下", hmacLine, segPrefix, []string{"hls/43/job9/s/a.m4s"}},
		{"对象就是前缀本身", hmacLine, segPrefix, []string{segPrefix}},
		{"签名方式无效", Line{SignType: "rot13", BaseURL: "https://cdn.example.com"}, segPrefix, segKeys(1)},
		{"不签名的线路没有访问域名", Line{SignType: SignNone}, segPrefix, segKeys(1)},
		{"HMAC 线路没有访问域名", Line{SignType: SignHMACToken, SignKeyRef: "cdn-main"}, segPrefix, segKeys(1)},
		{"厂商线路没有访问域名", Line{SignType: SignCDNVendor, SignKeyRef: "timestamp_md5/cdn-main"}, segPrefix, segKeys(1)},
	}
	for _, tt := range tests {
		if _, err := svc.SignSegments(context.Background(), tt.line, tt.prefix, tt.keys, now); err == nil {
			t.Errorf("%s：应报错", tt.name)
		}
	}
	missing := hmacLine
	missing.SignKeyRef = "absent"
	if _, err := svc.SignSegments(context.Background(), missing, segPrefix, segKeys(1), now); !errors.Is(err, ErrSignKeyMissing) {
		t.Errorf("密钥未注入时应返回 ErrSignKeyMissing，得到 %v", err)
	}
	if got, err := svc.SignSegments(context.Background(), hmacLine, segPrefix, nil, now); err != nil || len(got) != 0 {
		t.Errorf("没有对象时返回空结果：%v %v", got, err)
	}
}

// 一档清单几百个切片时签名的耗时（docs/specs/S1-8.md「预签名缓存先测量」）。
func BenchmarkSignSegments(b *testing.B) {
	kr, _ := ParseSignKeyring("cdn-main:" + testSecret)
	svc := NewSignService(&signRepo{}, stubCreds{}, kr, &stubPresigner{}, nil)
	line := Line{SignType: SignHMACToken, SignKeyRef: "cdn-main", BaseURL: "https://cdn.example.com", TokenTTL: 600}
	keys := segKeys(300)
	now := time.Now()
	for b.Loop() {
		if _, err := svc.SignSegments(context.Background(), line, segPrefix, keys, now); err != nil {
			b.Fatal(err)
		}
	}
}
