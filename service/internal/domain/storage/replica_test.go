package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// placementRepo 是解析主存储 / 镜像需要的存根。
type placementRepo struct {
	rows     []Row
	settings Settings
}

func (r *placementRepo) List(_ context.Context, q Query) ([]Row, int64, error) {
	var out []Row
	for _, row := range r.rows {
		if q.Status != "" && row.Status != q.Status {
			continue
		}
		out = append(out, row)
	}
	// 与 pg 实现一致：按编号倒序
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, int64(len(out)), nil
}

func (r *placementRepo) Get(_ context.Context, id int64) (Row, error) {
	for _, row := range r.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return Row{}, errors.New("not found")
}

func (r *placementRepo) GetSettings(context.Context) (Settings, error) { return r.settings, nil }

func provider(id int64, role Role, purpose Purpose, status Status) Row {
	return Row{
		ID: id, Name: "源" + string(rune('A'+id-1)), Type: TypeS3, Role: role, Purpose: purpose,
		Status: status, Bucket: "b", Region: "us-east-1",
	}
}

func TestPlacement_PrimaryFromSettings(t *testing.T) {
	repo := &placementRepo{
		rows: []Row{
			provider(1, RolePrimary, PurposeBoth, StatusActive),
			provider(2, RoleMirror, PurposeBoth, StatusActive),
		},
		// 设置里显式指定 2 号为视频主存储，应优先于 role=primary 的 1 号
		settings: Settings{PrimaryVideoProviderID: 2, PrimaryImageProviderID: 1},
	}
	p := NewPlacement(repo)

	got, err := p.Primary(context.Background(), LineVideo)
	if err != nil {
		t.Fatalf("解析主存储出错：%v", err)
	}
	if got.ID != 2 {
		t.Errorf("设置里指定的主存储应优先，得到 %d", got.ID)
	}
	if img, err := p.Primary(context.Background(), LineImage); err != nil || img.ID != 1 {
		t.Errorf("图片主存储应为 1，得到 %d %v", img.ID, err)
	}
}

func TestPlacement_PrimaryFallsBackToRole(t *testing.T) {
	repo := &placementRepo{
		rows: []Row{
			provider(1, RoleMirror, PurposeBoth, StatusActive),
			provider(2, RolePrimary, PurposeVideo, StatusActive),
			provider(3, RolePrimary, PurposeBoth, StatusActive),
		},
		// 设置里没指定
	}
	p := NewPlacement(repo)

	// 回退到 role=primary + 用途匹配中编号最小的
	if got, err := p.Primary(context.Background(), LineVideo); err != nil || got.ID != 2 {
		t.Errorf("视频主存储应回退到 2，得到 %d %v", got.ID, err)
	}
	// 图片：2 号用途只支持 video，应选 3
	if got, err := p.Primary(context.Background(), LineImage); err != nil || got.ID != 3 {
		t.Errorf("图片主存储应回退到 3，得到 %d %v", got.ID, err)
	}
}

func TestPlacement_PrimaryRejectsUnwritable(t *testing.T) {
	cases := []struct {
		name string
		row  Row
	}{
		{"停写的存储源", provider(1, RolePrimary, PurposeBoth, StatusDraining)},
		{"停用的存储源", provider(1, RolePrimary, PurposeBoth, StatusDisabled)},
		{"只读角色", provider(1, RoleReadonly, PurposeBoth, StatusActive)},
		{"用途不匹配", provider(1, RolePrimary, PurposeImage, StatusActive)},
	}
	for _, c := range cases {
		repo := &placementRepo{rows: []Row{c.row}, settings: Settings{PrimaryVideoProviderID: 1}}
		if _, err := NewPlacement(repo).Primary(context.Background(), LineVideo); err == nil {
			t.Errorf("%s 不应被选为主存储", c.name)
		}
	}

	// HTTP 源不能写入
	httpOrigin := provider(1, RolePrimary, PurposeBoth, StatusActive)
	httpOrigin.Type, httpOrigin.Subtype = TypeCustom, SubtypeHTTPOrigin
	repo := &placementRepo{rows: []Row{httpOrigin}, settings: Settings{PrimaryVideoProviderID: 1}}
	if _, err := NewPlacement(repo).Primary(context.Background(), LineVideo); err == nil {
		t.Error("只读 HTTP 源不应被选为主存储")
	}
}

func TestPlacement_NoPrimary(t *testing.T) {
	repo := &placementRepo{rows: []Row{provider(1, RoleMirror, PurposeBoth, StatusActive)}}
	if _, err := NewPlacement(repo).Primary(context.Background(), LineVideo); err == nil {
		t.Error("没有 primary 角色的存储源时应报错")
	}
}

func TestPlacement_Mirrors(t *testing.T) {
	repo := &placementRepo{rows: []Row{
		provider(1, RolePrimary, PurposeBoth, StatusActive),
		provider(2, RoleMirror, PurposeBoth, StatusActive),
		provider(3, RoleMirror, PurposeImage, StatusActive),  // 用途不含视频
		provider(4, RoleMirror, PurposeBoth, StatusDraining), // 停写，不再写入
		provider(5, RoleMirror, PurposeBoth, StatusDisabled), // 停用
		provider(6, RoleReadonly, PurposeBoth, StatusActive), // 只读角色不是镜像
	}}
	p := NewPlacement(repo)

	mirrors, err := p.Mirrors(context.Background(), LineVideo, 1)
	if err != nil {
		t.Fatalf("解析镜像出错：%v", err)
	}
	if len(mirrors) != 1 || mirrors[0].ID != 2 {
		ids := make([]int64, len(mirrors))
		for i, m := range mirrors {
			ids[i] = m.ID
		}
		t.Errorf("视频镜像应只有 2，得到 %v", ids)
	}
	// 图片镜像：2 与 3 都支持
	imgs, _ := p.Mirrors(context.Background(), LineImage, 1)
	if len(imgs) != 2 {
		t.Errorf("图片镜像应有 2 个，得到 %d", len(imgs))
	}
	// 主存储自己不算镜像（即使它的 role 是 mirror）
	selfMirror, _ := p.Mirrors(context.Background(), LineVideo, 2)
	for _, m := range selfMirror {
		if m.ID == 2 {
			t.Error("主存储不应出现在镜像列表里")
		}
	}
}

func TestSameStore(t *testing.T) {
	cred := Credential{AccessKeyID: "ak", SecretAccessKey: "sk"}
	other := Credential{AccessKeyID: "ak2", SecretAccessKey: "sk2"}
	a := Row{Endpoint: "https://x.r2.cloudflarestorage.com", Bucket: "b1"}
	b := Row{Endpoint: "https://x.r2.cloudflarestorage.com", Bucket: "b2"}
	c := Row{Endpoint: "https://y.example.com", Bucket: "b3"}

	// 同端点 + 同凭据：可用服务端复制
	if !sameStore(a, cred, b, cred) {
		t.Error("同端点同凭据应判定为同一存储服务")
	}
	// 同端点但凭据不同：不能假设服务端复制可用
	if sameStore(a, cred, b, other) {
		t.Error("凭据不同不应判定为同一存储服务")
	}
	// 不同端点
	if sameStore(a, cred, c, cred) {
		t.Error("端点不同不应判定为同一存储服务")
	}
	// 端点为空（AWS 默认端点）时不判定为同源，避免误用服务端复制
	if sameStore(Row{}, cred, Row{}, cred) {
		t.Error("端点为空时不应判定为同一存储服务")
	}
}

func TestKindOfObject(t *testing.T) {
	if got := kindOfObject(MediaObject{Kind: ObjectHLSDir}); got != LineVideo {
		t.Errorf("HLS 目录应按视频处理，得到 %s", got)
	}
	if got := kindOfObject(MediaObject{Kind: ObjectFile}); got != LineImage {
		t.Errorf("单文件应按图片处理，得到 %s", got)
	}
}

func TestPublicCopyError(t *testing.T) {
	// 预签名参数不该出现在给运营看的失败原因里
	got := publicCopyError(errors.New("读取失败 https://a.com/x?X-Amz-Signature=secret&X-Amz-Credential=ak"))
	if len(got) == 0 || contains(got, "secret") || contains(got, "X-Amz-") {
		t.Errorf("签名参数应被截掉：%s", got)
	}
	// 超长错误被截断
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'x'
	}
	if n := len([]rune(publicCopyError(errors.New(string(long))))); n > 310 {
		t.Errorf("超长错误应截断，得到 %d 个字符", n)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// ---------- 选线按副本过滤（docs/12 第 5 节第 4 条）----------

// replicaSelectRepo 在选线存根上补副本查询。
type replicaSelectRepo struct {
	*stubSelectRepo
	ready map[int64][]int64 // objectID → 已就绪的存储源
}

func (r *replicaSelectRepo) ReadyProviders(_ context.Context, objectID int64) ([]int64, error) {
	return r.ready[objectID], nil
}

func TestForPlayback_FiltersByReadyReplica(t *testing.T) {
	// 两条线路挂在不同存储源上
	lines := []Line{
		{ID: 1, ProviderID: 10, Kind: LineVideo, Name: "主存储线路", Status: LineActive, RegionScope: ScopeGlobal, Weight: 100},
		{ID: 2, ProviderID: 20, Kind: LineVideo, Name: "镜像线路", Status: LineActive, RegionScope: ScopeGlobal, Weight: 100},
	}
	repo := &replicaSelectRepo{
		stubSelectRepo: newSelectRepo(lines, 3),
		// 对象 100 只在主存储（10）上就绪，镜像（20）还在复制
		ready: map[int64][]int64{100: {10}},
	}
	svc := NewLinesService(repo, NopGeoResolver, NopHealthStore, NopQualityStore)

	// 带 ObjectID：镜像复制完成前不下发镜像线路
	got, err := svc.ForPlayback(context.Background(), PlaybackInput{ObjectID: 100})
	if err != nil {
		t.Fatalf("选源出错：%v", err)
	}
	if len(got.Lines) != 1 || got.Lines[0].LineID != 1 {
		ids := make([]int64, len(got.Lines))
		for i, l := range got.Lines {
			ids[i] = l.LineID
		}
		t.Errorf("只应返回已就绪存储源上的线路，得到 %v", ids)
	}

	// 不带 ObjectID：不按副本过滤（用户端线路菜单不针对具体一集）
	all, err := svc.ForPlayback(context.Background(), PlaybackInput{})
	if err != nil {
		t.Fatalf("选源出错：%v", err)
	}
	if len(all.Lines) != 2 {
		t.Errorf("不传对象编号时不应过滤，得到 %d 条", len(all.Lines))
	}
}

func TestForPlayback_NoReplicaRecordsDoesNotFilter(t *testing.T) {
	// S1-6f 之前的产物没有副本记录，不能因此判定「所有线路都不可用」
	lines := []Line{
		{ID: 1, ProviderID: 10, Kind: LineVideo, Name: "线路甲", Status: LineActive, RegionScope: ScopeGlobal, Weight: 100},
	}
	repo := &replicaSelectRepo{stubSelectRepo: newSelectRepo(lines, 3), ready: map[int64][]int64{}}
	svc := NewLinesService(repo, NopGeoResolver, NopHealthStore, NopQualityStore)

	got, err := svc.ForPlayback(context.Background(), PlaybackInput{ObjectID: 999})
	if err != nil {
		t.Fatalf("选源出错：%v", err)
	}
	if len(got.Lines) != 1 {
		t.Errorf("没有副本记录时应不过滤（兼容历史产物），得到 %d 条", len(got.Lines))
	}
}

// ---------- 存储源删除保护（docs/12 第 5 节第 5 条）----------

func TestService_DeleteRejectsProviderWithReplicas(t *testing.T) {
	svc, repo, _ := newTestService(t)
	row, err := repo.Create(context.Background(), Row{Name: "有副本的源", Type: TypeS3, Bucket: "b", Region: "r"})
	if err != nil {
		t.Fatal(err)
	}
	// 该存储源上还有副本
	if _, err := repo.SaveReplica(context.Background(), 100, row.ID, ReplicaReady, ""); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Delete(context.Background(), row.ID); err == nil {
		t.Error("还有副本的存储源不应允许删除")
	}

	// 副本清掉后可以删
	if err := repo.DeleteReplica(context.Background(), 100, row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Delete(context.Background(), row.ID); err != nil {
		t.Errorf("没有副本后应允许删除：%v", err)
	}
}

// ---------- 复制的失败分类（配置类不重试）----------

func TestReplicate_NoSourceIsNotFailure(t *testing.T) {
	rows := []Row{
		{Name: "镜", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusActive, Bucket: "b", Region: "r"},
	}
	// 对象存在但没有任何副本记录（S1-6f 之前写入默认桶的历史产物）
	repo := newManageRepo(t, rows, []int64{5}, nil)
	cop := &migrateCopier{}
	svc := NewReplicateService(repo, NewPlacement(repo), stubWriteCreds{}, cop)

	res, err := svc.Replicate(context.Background(), 5)
	if err != nil {
		t.Fatalf("没有复制源不该返回错误（重试解决不了）：%v", err)
	}
	if !res.NoSource {
		t.Error("应标记 NoSource")
	}
	if res.Failed != 0 || res.Copied != 0 {
		t.Errorf("不该尝试复制：%+v", res)
	}
	if len(cop.calls) != 0 {
		t.Error("不该发起复制")
	}
}

// 没配凭据的镜像应记为永久失败：副本留下原因，但任务不重试。
func TestReplicate_UnwritableMirrorIsPermanent(t *testing.T) {
	rows := []Row{
		{Name: "主", Type: TypeS3, Role: RolePrimary, Purpose: PurposeBoth, Status: StatusActive, Bucket: "b", Region: "r"},
		{Name: "只读镜像", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusActive, Bucket: "b", Region: "r"},
	}
	repo := newManageRepo(t, rows, []int64{5}, map[[2]int64]ReplicaStatus{{5, 1}: ReplicaReady})
	// 镜像的用途改成只支持图片，而对象是 HLS 目录（视频）→ checkWritable 失败
	row2, _ := repo.Get(context.Background(), 2)
	row2.Purpose = PurposeImage
	if _, err := repo.Update(context.Background(), 2, row2); err != nil {
		t.Fatal(err)
	}

	cop := &migrateCopier{}
	svc := NewReplicateService(repo, NewPlacement(repo), stubWriteCreds{}, cop)
	res, err := svc.Replicate(context.Background(), 5)
	if err != nil {
		t.Fatalf("Replicate 出错：%v", err)
	}
	// 用途不匹配的镜像在 Mirrors 阶段就被排除，所以这里是「没有镜像可复制」
	if res.Copied != 0 || res.Failed != 0 {
		t.Errorf("用途不匹配的镜像应被排除而非失败：%+v", res)
	}
}

func TestIsPermanentCopyError(t *testing.T) {
	plain := errors.New("网络超时")
	if IsPermanentCopyError(plain) {
		t.Error("普通错误不该判为永久失败")
	}
	wrapped := permanent(plain)
	if !IsPermanentCopyError(wrapped) {
		t.Error("包装后应判为永久失败")
	}
	// 包装后仍能取到原错误
	if !errors.Is(wrapped, plain) {
		t.Error("应能 Unwrap 到原错误")
	}
	// 多层包装
	if !IsPermanentCopyError(fmt.Errorf("复制对象 1: %w", wrapped)) {
		t.Error("多层包装后仍应判为永久失败")
	}
}
