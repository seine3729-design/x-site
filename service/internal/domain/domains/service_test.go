package domains

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

func TestGenerateSelfSigned(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, kt := range []KeyType{KeyEC, KeyRSA, ""} {
		got, err := GenerateSelfSigned(GenCertInput{
			CommonName: "a.example.com",
			SANs:       []string{"b.example.com", "a.example.com", "*.a.example.com"},
			Days:       90, KeyType: kt,
		}, now)
		if err != nil {
			t.Fatalf("生成失败 key=%q: %v", kt, err)
		}
		if !strings.Contains(got.CertPEM, "BEGIN CERTIFICATE") {
			t.Errorf("key=%q 缺证书 PEM", kt)
		}
		if !strings.Contains(got.KeyPEM, "PRIVATE KEY") {
			t.Errorf("key=%q 缺私钥 PEM", kt)
		}
		// CN 并入、重复去掉、通配保留 → a / b / *.a 共 3 个
		if len(got.SANs) != 3 {
			t.Errorf("key=%q SAN 去重后应为 3，得 %v", kt, got.SANs)
		}
		info, err := ParseCertificate(got.CertPEM)
		if err != nil {
			t.Fatalf("key=%q 解析失败: %v", kt, err)
		}
		if !info.SelfSigned {
			t.Errorf("key=%q 应为自签", kt)
		}
		if info.NotAfter.Unix() != got.NotAfter.Unix() {
			t.Errorf("key=%q notAfter 不一致: %v vs %v", kt, info.NotAfter, got.NotAfter)
		}
		if info.Fingerprint != got.Fingerprint {
			t.Errorf("key=%q 指纹不一致", kt)
		}
	}
}

func TestGenerateSelfSigned_Invalid(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name string
		in   GenCertInput
	}{
		{"空CN", GenCertInput{Days: 90}},
		{"天数为0", GenCertInput{CommonName: "a.com", Days: 0}},
		{"天数过大", GenCertInput{CommonName: "a.com", Days: 99999}},
		{"未知密钥类型", GenCertInput{CommonName: "a.com", Days: 90, KeyType: "dsa"}},
		{"非法SAN", GenCertInput{CommonName: "a.com", Days: 90, SANs: []string{"bad host"}}},
		{"CN带协议", GenCertInput{CommonName: "https://a.com", Days: 90}},
	}
	for _, c := range cases {
		if _, err := GenerateSelfSigned(c.in, now); err == nil {
			t.Errorf("%s 应报错", c.name)
		}
	}
}

func TestParseCertificate_Bad(t *testing.T) {
	for _, s := range []string{
		"",
		"这不是 PEM",
		"-----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY-----",
	} {
		if _, err := ParseCertificate(s); err == nil {
			t.Errorf("应报错: %q", s)
		}
	}
}

func TestCertState(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	cases := []struct {
		exp  *time.Time
		want string
	}{
		{nil, CertUnknown},
		{at(-24 * time.Hour), CertExpired},
		{at(10 * 24 * time.Hour), CertExpiring},
		{at(100 * 24 * time.Hour), CertOK},
	}
	for _, c := range cases {
		if got, _ := certState(c.exp, now); got != c.want {
			t.Errorf("exp=%v 期望 %s 得 %s", c.exp, c.want, got)
		}
	}
}

func TestEntryInputNormalize(t *testing.T) {
	got, err := EntryInput{Host: "A.Example.com", Role: RoleAPI, Weight: 50}.normalize()
	if err != nil {
		t.Fatalf("合法输入报错: %v", err)
	}
	if got.Host != "a.example.com" {
		t.Errorf("host 应小写归一，得 %s", got.Host)
	}
	if got.Region != "*" || got.Status != StatusActive {
		t.Errorf("region/status 默认值不对: %+v", got)
	}
	bad := []EntryInput{
		{Host: "", Role: RoleAPI, Weight: 50},
		{Host: "http://a.com", Role: RoleAPI, Weight: 50},
		{Host: "localhost", Role: RoleAPI, Weight: 50}, // 无点
		{Host: "a.com", Role: "bad", Weight: 50},
		{Host: "a.com", Role: RoleAPI, Weight: 0},
		{Host: "a.com", Role: RoleAPI, Weight: 101},
	}
	for i, b := range bad {
		if _, err := b.normalize(); err == nil {
			t.Errorf("非法输入 %d 应报错: %+v", i, b)
		}
	}
}

func TestPreviewRelease(t *testing.T) {
	repo := &fakeRepo{nextVer: 5, entries: []Entry{
		{Host: "a1.net", Role: RoleAPI, Weight: 60, Region: "cn", Status: StatusActive},
		{Host: "a2.net", Role: RoleAPI, Weight: 40, Region: "*", Status: StatusWatch},
		{Host: "x.net", Role: RoleAPI, Status: StatusDisabled}, // 停用：排除
		{Host: "www.com", Role: RoleWebPrimary, Status: StatusActive},
		{Host: "m.net", Role: RoleWebBackup, Status: StatusActive},
		{Host: "go.dev", Role: RoleNav, Status: StatusActive},
	}}
	svc := NewService(repo)
	svc.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

	prev, err := svc.PreviewRelease(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if prev.Version != 5 {
		t.Errorf("预览版本应为 5，得 %d", prev.Version)
	}
	var p ReleasePayload
	if err := json.Unmarshal(prev.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.V != 5 {
		t.Errorf("payload.v 应为 5，得 %d", p.V)
	}
	if len(p.API) != 2 {
		t.Errorf("API 应为 2（含 active+watch、排除 disabled），得 %d", len(p.API))
	}
	if len(p.Web) != 2 {
		t.Errorf("Web 应为 2，得 %d", len(p.Web))
	}
	if p.Nav != "https://go.dev" {
		t.Errorf("nav 应为 https://go.dev，得 %s", p.Nav)
	}
	if p.ExpiresAt.Sub(p.IssuedAt) != releaseTTLDays*24*time.Hour {
		t.Errorf("清单有效期应为 %d 天", releaseTTLDays)
	}
}

func TestPublishRelease(t *testing.T) {
	repo := &fakeRepo{nextVer: 3}
	svc := NewService(repo)
	payload := func(v int64) json.RawMessage { b, _ := json.Marshal(ReleasePayload{V: v}); return b }

	if _, err := svc.PublishRelease(context.Background(), PublishInput{Payload: payload(2), Signature: "YWJj"}); !errors.Is(err, errs.ErrConflict) {
		t.Errorf("版本号不匹配应返回 CONFLICT，得 %v", err)
	}
	if _, err := svc.PublishRelease(context.Background(), PublishInput{Payload: payload(3), Signature: ""}); !errors.Is(err, errs.ErrBadRequest) {
		t.Errorf("空签名应返回 400，得 %v", err)
	}
	if _, err := svc.PublishRelease(context.Background(), PublishInput{Payload: payload(3), Signature: "bad sig!"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Errorf("非 base64 签名应返回 400，得 %v", err)
	}
	rel, err := svc.PublishRelease(context.Background(), PublishInput{Payload: payload(3), Signature: "YWJjZA=="})
	if err != nil {
		t.Fatalf("正常发布失败: %v", err)
	}
	if rel.Version != 3 || repo.inserted == nil || repo.inserted.Version != 3 {
		t.Errorf("应以版本 3 插入，得 %+v", repo.inserted)
	}
}

// fakeRepo 是最小假仓储：只实现预览 / 发布用到的方法返回有意义的值，其余返回零值。
type fakeRepo struct {
	entries  []Entry
	nextVer  int64
	releases map[int64]Release
	inserted *ReleaseRow
}

func (f *fakeRepo) List(context.Context, Filter) ([]Entry, error) { return f.entries, nil }
func (f *fakeRepo) Get(context.Context, int64) (Entry, error)     { return Entry{}, nil }
func (f *fakeRepo) Create(context.Context, EntryInput) (Entry, error) {
	return Entry{}, nil
}
func (f *fakeRepo) Update(context.Context, int64, EntryInput) (Entry, error) { return Entry{}, nil }
func (f *fakeRepo) Delete(context.Context, int64) (Entry, error)             { return Entry{}, nil }
func (f *fakeRepo) UpdateCertExpiry(context.Context, int64, *time.Time) (Entry, error) {
	return Entry{}, nil
}
func (f *fakeRepo) ListReleases(context.Context, int, int) ([]Release, int64, error) {
	return nil, 0, nil
}
func (f *fakeRepo) GetRelease(_ context.Context, v int64) (Release, error) {
	if r, ok := f.releases[v]; ok {
		return r, nil
	}
	return Release{}, errs.ErrNotFound
}
func (f *fakeRepo) LatestRelease(context.Context) (Release, error) {
	return Release{}, errs.ErrNotFound
}
func (f *fakeRepo) NextReleaseVersion(context.Context) (int64, error) { return f.nextVer, nil }
func (f *fakeRepo) InsertRelease(_ context.Context, row ReleaseRow) (Release, error) {
	f.inserted = &row
	return Release{Version: row.Version, Payload: row.Payload, Signature: row.Signature, Note: row.Note}, nil
}
func (f *fakeRepo) ListHealth(context.Context, time.Time) ([]HealthDaily, error) { return nil, nil }
