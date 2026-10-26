package storage

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/crypto/envelope"
	"xsite/service/internal/errs"
	"xsite/service/internal/netguard"
)

// ---------- 测试替身 ----------

type fakeRepo struct {
	rows map[int64]Row
	next int64
	// 媒体副本（S1-6f）：键为 (object_id, provider_id)
	replicas map[[2]int64]Replica
	objects  map[int64]MediaObject
	settings Settings
	// referenced 模拟 episode.video_object_id 的引用；clock 为零值时用 time.Now
	referenced map[int64]bool
	clock      time.Time
}

func newFakeRepo() *fakeRepo { return &fakeRepo{rows: map[int64]Row{}} }

func (r *fakeRepo) List(_ context.Context, _ Query) ([]Row, int64, error) {
	out := make([]Row, 0, len(r.rows))
	for _, v := range r.rows {
		out = append(out, v)
	}
	return out, int64(len(out)), nil
}

func (r *fakeRepo) Get(_ context.Context, id int64) (Row, error) {
	row, ok := r.rows[id]
	if !ok {
		return Row{}, errs.ErrNotFound
	}
	return row, nil
}

func (r *fakeRepo) Create(_ context.Context, row Row) (Row, error) {
	r.next++
	row.ID = r.next
	r.rows[row.ID] = row
	return row, nil
}

func (r *fakeRepo) Update(_ context.Context, id int64, row Row) (Row, error) {
	if _, ok := r.rows[id]; !ok {
		return Row{}, errs.ErrNotFound
	}
	row.ID = id
	r.rows[id] = row
	return row, nil
}

func (r *fakeRepo) Delete(_ context.Context, id int64) (Row, error) {
	row, ok := r.rows[id]
	if !ok {
		return Row{}, errs.ErrNotFound
	}
	delete(r.rows, id)
	return row, nil
}

type fakeProber struct{ got ProbeRequest }

func (p *fakeProber) Probe(_ context.Context, req ProbeRequest) ProbeResult {
	p.got = req
	return ProbeResult{OK: true}
}

func newTestService(t *testing.T) (*Service, *fakeRepo, *fakeProber) {
	t.Helper()
	kr, err := envelope.ParseKeyring("k1:0123456789abcdef0123456789abcdef", "xsite-storage-test/v1")
	if err != nil {
		t.Fatal(err)
	}
	repo, prober := newFakeRepo(), &fakeProber{}
	// AllowPrivate=false 以便校验内网端点被拒；HTTPSOnly=false 方便用 http 构造用例
	svc := NewService(repo, kr, prober, Policy{HTTPSOnly: false, Egress: netguard.Policy{AllowPrivate: false}})
	return svc, repo, prober
}

func s3Input() Input {
	return Input{
		Name: "S3 主存储", Type: TypeS3, Purpose: PurposeVideo, Role: RolePrimary,
		Region: "us-east-1", Bucket: "xsite-media",
		WriteCred: &Credential{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "secretvaluezzzz"},
	}
}

// ---------- 创建与凭据加密 ----------

func TestCreateEncryptsCredential(t *testing.T) {
	svc, repo, _ := newTestService(t)
	prov, err := svc.Create(context.Background(), s3Input())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !prov.HasWriteCred || prov.CredTail != "zzzz" {
		t.Fatalf("展示应标记已配置且显示末 4 位，得到 has=%v tail=%q", prov.HasWriteCred, prov.CredTail)
	}
	row := repo.rows[prov.ID]
	if len(row.CredentialEnc) == 0 {
		t.Fatal("凭据应加密入库")
	}
	if bytes.Contains(row.CredentialEnc, []byte("secretvaluezzzz")) || bytes.Contains(row.CredentialEnc, []byte("AKIDEXAMPLE")) {
		t.Fatal("密文中不应出现明文凭据")
	}
	// 能用同一密钥环解回
	got, err := svc.open(writeAAD, row.CredentialEnc)
	if err != nil || got.AccessKeyID != "AKIDEXAMPLE" || got.SecretAccessKey != "secretvaluezzzz" {
		t.Fatalf("应能解回原凭据：%+v %v", got, err)
	}
}

func TestCreateR2DerivesEndpoint(t *testing.T) {
	svc, _, _ := newTestService(t)
	in := Input{
		Name: "R2", Type: TypeR2, Purpose: PurposeBoth, Role: RolePrimary,
		AccountID: "38cf99d986ef8a3c19ea71e9fd2099c9", Bucket: "duanju666",
		WriteCred: &Credential{AccessKeyID: "ak", SecretAccessKey: "0123456789ab"},
	}
	prov, err := svc.Create(context.Background(), in)
	if err != nil {
		t.Fatalf("Create R2: %v", err)
	}
	if prov.Endpoint != "https://38cf99d986ef8a3c19ea71e9fd2099c9.r2.cloudflarestorage.com" {
		t.Fatalf("R2 端点应自动生成，得到 %q", prov.Endpoint)
	}
	if prov.Region != "auto" {
		t.Fatalf("R2 region 应为 auto，得到 %q", prov.Region)
	}
}

func TestCreateHTTPOriginNoCredential(t *testing.T) {
	svc, _, _ := newTestService(t)
	in := Input{
		Name: "HTTP 源", Type: TypeCustom, Subtype: SubtypeHTTPOrigin, Purpose: PurposeImage, Role: RoleReadonly,
		Endpoint: "https://img.example.com/media",
	}
	if _, err := svc.Create(context.Background(), in); err != nil {
		t.Fatalf("HTTP 源允许无凭据创建，得到 %v", err)
	}
}

func TestCreateRequiresCredentialExceptHTTPOrigin(t *testing.T) {
	svc, _, _ := newTestService(t)
	in := s3Input()
	in.WriteCred = nil
	if _, err := svc.Create(context.Background(), in); err == nil {
		t.Fatal("S3 新建缺少读写凭据应报错")
	}
}

// ---------- 校验 ----------

func TestCreateValidation(t *testing.T) {
	svc, _, _ := newTestService(t)
	cases := map[string]Input{
		"类型非法":                   func() Input { in := s3Input(); in.Type = "oss"; return in }(),
		"S3 缺 region":            func() Input { in := s3Input(); in.Region = ""; return in }(),
		"S3 缺 bucket":            func() Input { in := s3Input(); in.Bucket = ""; return in }(),
		"R2 account 非法":          {Name: "r2", Type: TypeR2, Purpose: PurposeBoth, Role: RolePrimary, AccountID: "xyz", Bucket: "b1b1", WriteCred: &Credential{"a", "0123456789ab"}},
		"custom 缺子类型":            {Name: "c", Type: TypeCustom, Purpose: PurposeImage, Role: RoleMirror, Endpoint: "https://x.example.com", Bucket: "b1b1", WriteCred: &Credential{"a", "0123456789ab"}},
		"s3_compatible 缺端点":      {Name: "c", Type: TypeCustom, Subtype: SubtypeS3Compatible, Purpose: PurposeImage, Role: RoleMirror, Bucket: "b1b1", WriteCred: &Credential{"a", "0123456789ab"}},
		"http_origin 缺 base_url": {Name: "c", Type: TypeCustom, Subtype: SubtypeHTTPOrigin, Purpose: PurposeImage, Role: RoleReadonly},
		"端点为内网地址":                {Name: "c", Type: TypeCustom, Subtype: SubtypeS3Compatible, Purpose: PurposeImage, Role: RoleMirror, Endpoint: "http://10.1.2.3:9000", Bucket: "xsite-media", WriteCred: &Credential{"a", "0123456789ab"}},
		"预签名有效期越界":               func() Input { in := s3Input(); in.MaxPresignTTL = 10; return in }(),
		"凭据只填一半":                 func() Input { in := s3Input(); in.WriteCred = &Credential{AccessKeyID: "ak"}; return in }(),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.Create(context.Background(), in); err == nil {
				t.Fatalf("用例 %q 应校验失败", name)
			}
		})
	}
}

// ---------- 修改时凭据的保留与替换 ----------

func TestUpdateKeepsCredentialWhenOmitted(t *testing.T) {
	svc, repo, _ := newTestService(t)
	prov, err := svc.Create(context.Background(), s3Input())
	if err != nil {
		t.Fatal(err)
	}
	encBefore := append([]byte(nil), repo.rows[prov.ID].CredentialEnc...)

	in := s3Input()
	in.WriteCred = nil // 不重新输入凭据
	in.Name = "改名后的 S3"
	_, after, err := svc.Update(context.Background(), prov.ID, in)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if after.Name != "改名后的 S3" {
		t.Fatalf("名称应更新，得到 %q", after.Name)
	}
	if !bytes.Equal(repo.rows[prov.ID].CredentialEnc, encBefore) {
		t.Fatal("未重新输入凭据时密文应保持不变")
	}
	if after.CredTail != "zzzz" {
		t.Fatalf("凭据提示应保留，得到 %q", after.CredTail)
	}
}

func TestUpdateReplacesCredentialWhenProvided(t *testing.T) {
	svc, repo, _ := newTestService(t)
	prov, err := svc.Create(context.Background(), s3Input())
	if err != nil {
		t.Fatal(err)
	}
	encBefore := append([]byte(nil), repo.rows[prov.ID].CredentialEnc...)

	in := s3Input()
	in.WriteCred = &Credential{AccessKeyID: "AKNEW", SecretAccessKey: "brandnewsecret1234"}
	_, after, err := svc.Update(context.Background(), prov.ID, in)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if bytes.Equal(repo.rows[prov.ID].CredentialEnc, encBefore) {
		t.Fatal("重新输入凭据后密文应改变")
	}
	if after.CredTail != "1234" {
		t.Fatalf("凭据提示应更新为新 secret 末 4 位，得到 %q", after.CredTail)
	}
}

// ---------- 连通性测试解密 ----------

func TestTestDecryptsAndPassesCredential(t *testing.T) {
	svc, _, prober := newTestService(t)
	prov, err := svc.Create(context.Background(), s3Input())
	if err != nil {
		t.Fatal(err)
	}
	res, err := svc.Test(context.Background(), prov.ID)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if !res.OK {
		t.Fatal("假探测器应返回 OK")
	}
	if prober.got.Write.AccessKeyID != "AKIDEXAMPLE" || prober.got.Write.SecretAccessKey != "secretvaluezzzz" {
		t.Fatalf("应把解密后的读写凭据传给探测器，得到 %+v", prober.got.Write)
	}
	// 未单独配置只读凭据时，只读等于读写
	if prober.got.Read != prober.got.Write {
		t.Fatalf("未配置只读凭据时应回退到读写凭据，得到 %+v", prober.got.Read)
	}
}

func TestTestWithoutCredentialFails(t *testing.T) {
	svc, repo, _ := newTestService(t)
	// 直接塞一行没有凭据的 S3 存储源（绕过 Create 的必填校验）
	repo.rows[1] = Row{ID: 1, Name: "x", Type: TypeS3, Purpose: PurposeVideo, Role: RolePrimary, Region: "us-east-1", Bucket: "xsite-media", Status: StatusActive}
	if _, err := svc.Test(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "凭据") {
		t.Fatalf("未配置凭据的 S3 测试应报错，得到 %v", err)
	}
}

// 线路方法存根：provider 的测试用例不触及线路（线路走 pg 集成测试 storage_line_test.go）。
func (r *fakeRepo) ListLines(context.Context, LineQuery) ([]Line, error)       { return nil, nil }
func (r *fakeRepo) GetLine(context.Context, int64) (Line, error)               { return Line{}, errs.ErrNotFound }
func (r *fakeRepo) CreateLine(context.Context, LineInput) (Line, error)        { return Line{}, nil }
func (r *fakeRepo) UpdateLine(context.Context, int64, LineInput) (Line, error) { return Line{}, nil }
func (r *fakeRepo) DeleteLine(context.Context, int64) (Line, error)            { return Line{}, nil }
func (r *fakeRepo) ListRulesByProvider(context.Context, int64) ([]Rule, error) { return nil, nil }
func (r *fakeRepo) ListRulesByLine(context.Context, int64) ([]Rule, error)     { return nil, nil }
func (r *fakeRepo) ReplaceLineRules(context.Context, int64, []RuleInput) ([]Rule, error) {
	return nil, nil
}

// ---------- 媒体副本存根（S1-6f）----------

func (r *fakeRepo) SaveReplica(_ context.Context, objectID, providerID int64, status ReplicaStatus, errMsg string) (Replica, error) {
	if r.replicas == nil {
		r.replicas = map[[2]int64]Replica{}
	}
	rep := Replica{ObjectID: objectID, ProviderID: providerID, Status: status, Error: errMsg}
	r.replicas[[2]int64{objectID, providerID}] = rep
	return rep, nil
}

func (r *fakeRepo) ListReplicas(_ context.Context, objectID int64) ([]Replica, error) {
	var out []Replica
	for k, v := range r.replicas {
		if k[0] == objectID {
			out = append(out, v)
		}
	}
	return out, nil
}

func (r *fakeRepo) ReadyProviders(_ context.Context, objectID int64) ([]int64, error) {
	var out []int64
	for k, v := range r.replicas {
		if k[0] == objectID && v.Status == ReplicaReady {
			out = append(out, k[1])
		}
	}
	return out, nil
}

func (r *fakeRepo) CountReplicasByProvider(_ context.Context, providerID int64) (int64, error) {
	var n int64
	for k := range r.replicas {
		if k[1] == providerID {
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) GetObject(_ context.Context, objectID int64) (MediaObject, error) {
	if obj, ok := r.objects[objectID]; ok {
		return obj, nil
	}
	return MediaObject{}, errs.ErrNotFound
}

func (r *fakeRepo) DeleteReplica(_ context.Context, objectID, providerID int64) error {
	delete(r.replicas, [2]int64{objectID, providerID})
	return nil
}

// ---------- 孤立产物的标记与清理存根（S1-6f）----------

// refs 记录「对象被哪个分集引用」，存根里用 referenced 集合模拟。
func (r *fakeRepo) MarkOrphans(context.Context) (int64, error) {
	var n int64
	for id, obj := range r.objects {
		if r.referenced[id] || !obj.OrphanedAt.IsZero() {
			continue
		}
		obj.OrphanedAt = r.now()
		r.objects[id] = obj
		n++
	}
	return n, nil
}

func (r *fakeRepo) UnmarkOrphans(context.Context) (int64, error) {
	var n int64
	for id, obj := range r.objects {
		if r.referenced[id] && !obj.OrphanedAt.IsZero() {
			obj.OrphanedAt = time.Time{}
			r.objects[id] = obj
			n++
		}
	}
	return n, nil
}

func (r *fakeRepo) ListExpiredOrphans(_ context.Context, before time.Time, cursorID int64, limit int) ([]OrphanObject, error) {
	var out []OrphanObject
	for id, obj := range r.objects {
		if id <= cursorID || obj.OrphanedAt.IsZero() || !obj.OrphanedAt.Before(before) || r.referenced[id] {
			continue
		}
		out = append(out, OrphanObject{
			ID: id, Kind: obj.Kind, Key: obj.Key, SizeBytes: obj.SizeBytes, OrphanedAt: obj.OrphanedAt,
		})
	}
	slices.SortFunc(out, func(a, b OrphanObject) int { return int(a.ID - b.ID) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeRepo) OrphanStats(_ context.Context, before time.Time) (OrphanStats, error) {
	var s OrphanStats
	for _, obj := range r.objects {
		if obj.OrphanedAt.IsZero() {
			continue
		}
		s.Total++
		s.Bytes += obj.SizeBytes
		if obj.OrphanedAt.Before(before) {
			s.Expired++
		}
	}
	return s, nil
}

func (r *fakeRepo) DeleteObject(_ context.Context, objectID int64) (bool, error) {
	// 仍被分集引用时不删（与 SQL 的 NOT EXISTS 一致）
	if r.referenced[objectID] {
		return false, nil
	}
	if _, ok := r.objects[objectID]; !ok {
		return false, nil
	}
	delete(r.objects, objectID)
	for k := range r.replicas {
		if k[0] == objectID {
			delete(r.replicas, k) // 模拟外键级联
		}
	}
	return true, nil
}

func (r *fakeRepo) now() time.Time {
	if r.clock.IsZero() {
		return time.Now()
	}
	return r.clock
}

func (r *fakeRepo) ListOrphanObjectIDs(_ context.Context, cursorID int64, limit int) ([]int64, error) {
	var out []int64
	for id := range r.objects {
		if id <= cursorID {
			continue
		}
		// 没有任何副本记录的对象
		orphan := true
		for k := range r.replicas {
			if k[0] == id {
				orphan = false
				break
			}
		}
		if orphan {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeRepo) ListObjectIDs(_ context.Context, q ObjectQuery) ([]int64, error) {
	var out []int64
	for id := range r.objects {
		if id <= q.CursorID {
			continue
		}
		if q.ProviderID > 0 {
			if _, ok := r.replicas[[2]int64{id, q.ProviderID}]; !ok {
				continue
			}
		}
		out = append(out, id)
	}
	slices.Sort(out)
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (r *fakeRepo) GetSettings(context.Context) (Settings, error) { return r.settings, nil }
func (r *fakeRepo) UpdateSettings(context.Context, SettingsInput) (Settings, error) {
	return Settings{}, nil
}
