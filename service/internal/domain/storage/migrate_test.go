package storage

import (
	"context"
	"errors"
	"testing"
)

// 记录入队调用的存根。
type recordingEnqueuer struct {
	replicated []int64
	migrated   [][3]int64 // objectID, from, to
	failAfter  int        // 超过这个数量后入队报错；0 表示不报错
}

func (e *recordingEnqueuer) EnqueueReplicate(_ context.Context, objectID int64) error {
	if e.failAfter > 0 && len(e.replicated) >= e.failAfter {
		return errors.New("队列不可用")
	}
	e.replicated = append(e.replicated, objectID)
	return nil
}

func (e *recordingEnqueuer) EnqueueMigrate(_ context.Context, objectID, from, to int64) error {
	e.migrated = append(e.migrated, [3]int64{objectID, from, to})
	return nil
}

// manageRepo 组合副本存根与存储源存根，供批量运维使用。
func newManageRepo(t *testing.T, rows []Row, objects []int64, replicas map[[2]int64]ReplicaStatus) *fakeRepo {
	t.Helper()
	repo := newFakeRepo()
	for _, row := range rows {
		if _, err := repo.Create(context.Background(), row); err != nil {
			t.Fatal(err)
		}
	}
	repo.objects = map[int64]MediaObject{}
	for _, id := range objects {
		repo.objects[id] = MediaObject{ID: id, Kind: ObjectHLSDir, Key: "hls/1/j"}
	}
	for k, status := range replicas {
		if _, err := repo.SaveReplica(context.Background(), k[0], k[1], status, ""); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func TestManageService_ReplicateScopes(t *testing.T) {
	rows := []Row{
		{Name: "主", Type: TypeS3, Role: RolePrimary, Purpose: PurposeBoth, Status: StatusActive, Bucket: "b", Region: "r"},
		{Name: "镜", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusActive, Bucket: "b", Region: "r"},
	}
	// 对象 1、2 在主存储（id=1）上有副本，对象 3 没有任何副本
	repo := newManageRepo(t, rows, []int64{1, 2, 3}, map[[2]int64]ReplicaStatus{
		{1, 1}: ReplicaReady,
		{2, 1}: ReplicaReady,
	})
	enq := &recordingEnqueuer{}
	svc := NewManageService(repo, NewPlacement(repo), enq, enq)

	// 全部：3 个对象都入队
	res, err := svc.Replicate(context.Background(), ReplicateRequest{Scope: ScopeAll})
	if err != nil {
		t.Fatalf("按全部触发出错：%v", err)
	}
	if res.Objects != 3 || len(enq.replicated) != 3 {
		t.Errorf("按全部应入队 3 个对象，得到 %d（实际入队 %d）", res.Objects, len(enq.replicated))
	}
	if res.Capped {
		t.Error("3 个对象不应触发上限截断")
	}

	// 按存储源：只有该源上有副本的对象（1、2）
	enq.replicated = nil
	res, err = svc.Replicate(context.Background(), ReplicateRequest{Scope: ScopeProvider, ProviderID: 1})
	if err != nil {
		t.Fatalf("按存储源触发出错：%v", err)
	}
	if res.Objects != 2 {
		t.Errorf("按存储源应入队 2 个对象，得到 %d", res.Objects)
	}
}

func TestManageService_ReplicateRejectsBadRequest(t *testing.T) {
	repo := newManageRepo(t, nil, nil, nil)
	enq := &recordingEnqueuer{}
	svc := NewManageService(repo, NewPlacement(repo), enq, enq)

	cases := []struct {
		name string
		req  ReplicateRequest
	}{
		{"未知范围", ReplicateRequest{Scope: "weird"}},
		{"按剧但没给剧编号", ReplicateRequest{Scope: ScopeDrama}},
		{"按存储源但没给编号", ReplicateRequest{Scope: ScopeProvider}},
		{"存储源不存在", ReplicateRequest{Scope: ScopeProvider, ProviderID: 999}},
	}
	for _, c := range cases {
		if _, err := svc.Replicate(context.Background(), c.req); err == nil {
			t.Errorf("%s 应报错", c.name)
		}
	}
}

func TestManageService_ReplicatePropagatesEnqueueError(t *testing.T) {
	repo := newManageRepo(t, nil, []int64{1, 2, 3}, nil)
	enq := &recordingEnqueuer{failAfter: 2}
	svc := NewManageService(repo, NewPlacement(repo), enq, enq)

	// 入队失败要暴露出来，不能静默少入队
	if _, err := svc.Replicate(context.Background(), ReplicateRequest{Scope: ScopeAll}); err == nil {
		t.Error("入队失败应返回错误")
	}
}

func TestManageService_Migrate(t *testing.T) {
	rows := []Row{
		{Name: "待下线", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusDraining, Bucket: "b", Region: "r"},
		{Name: "新家", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusActive, Bucket: "b", Region: "r"},
	}
	// 对象 1、2 在待下线的源（id=1）上
	repo := newManageRepo(t, rows, []int64{1, 2, 3}, map[[2]int64]ReplicaStatus{
		{1, 1}: ReplicaReady,
		{2, 1}: ReplicaReady,
		{3, 2}: ReplicaReady, // 对象 3 已经在新家，不该被迁移
	})
	enq := &recordingEnqueuer{}
	svc := NewManageService(repo, NewPlacement(repo), enq, enq)

	res, err := svc.Migrate(context.Background(), MigrateRequest{FromProviderID: 1, ToProviderID: 2})
	if err != nil {
		t.Fatalf("迁移出错：%v", err)
	}
	if res.Objects != 2 {
		t.Errorf("应入队 2 个对象，得到 %d", res.Objects)
	}
	for _, m := range enq.migrated {
		if m[1] != 1 || m[2] != 2 {
			t.Errorf("迁移参数错误：%v", m)
		}
	}
}

func TestManageService_MigrateRejectsBadTarget(t *testing.T) {
	rows := []Row{
		{Name: "源", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusDraining, Bucket: "b", Region: "r"},
		{Name: "停写的目标", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusDraining, Bucket: "b", Region: "r"},
		{Name: "停用的目标", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusDisabled, Bucket: "b", Region: "r"},
		{Name: "只读的目标", Type: TypeS3, Role: RoleReadonly, Purpose: PurposeBoth, Status: StatusActive, Bucket: "b", Region: "r"},
	}
	repo := newManageRepo(t, rows, nil, nil)
	enq := &recordingEnqueuer{}
	svc := NewManageService(repo, NewPlacement(repo), enq, enq)

	cases := []struct {
		name string
		req  MigrateRequest
	}{
		{"缺迁出", MigrateRequest{ToProviderID: 2}},
		{"缺迁入", MigrateRequest{FromProviderID: 1}},
		{"迁出与迁入相同", MigrateRequest{FromProviderID: 1, ToProviderID: 1}},
		{"迁出不存在", MigrateRequest{FromProviderID: 999, ToProviderID: 2}},
		{"迁入不存在", MigrateRequest{FromProviderID: 1, ToProviderID: 999}},
		{"迁入已停写", MigrateRequest{FromProviderID: 1, ToProviderID: 2}},
		{"迁入已停用", MigrateRequest{FromProviderID: 1, ToProviderID: 3}},
		{"迁入是只读角色", MigrateRequest{FromProviderID: 1, ToProviderID: 4}},
	}
	for _, c := range cases {
		if _, err := svc.Migrate(context.Background(), c.req); err == nil {
			t.Errorf("%s 应报错", c.name)
		}
	}
}

// ---------- 迁移的执行 ----------

// migrateCopier 记录复制调用。
type migrateCopier struct {
	calls [][2]string // from bucket, to bucket
	err   error
}

func (c *migrateCopier) CopyObject(_ context.Context, req CopyRequest) (CopyResult, error) {
	if c.err != nil {
		return CopyResult{}, c.err
	}
	c.calls = append(c.calls, [2]string{req.From.Bucket, req.To.Bucket})
	return CopyResult{Objects: 1, Bytes: 10}, nil
}

type stubWriteCreds struct{}

func (stubWriteCreds) WriteCredential(Row) (Credential, error) {
	return Credential{AccessKeyID: "ak", SecretAccessKey: "sk"}, nil
}

func TestReplicateService_MigrateOne(t *testing.T) {
	rows := []Row{
		{Name: "源", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusDraining, Bucket: "from", Region: "r"},
		{Name: "目标", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusActive, Bucket: "to", Region: "r"},
	}
	repo := newManageRepo(t, rows, []int64{7}, map[[2]int64]ReplicaStatus{{7, 1}: ReplicaReady})
	cop := &migrateCopier{}
	svc := NewReplicateService(repo, NewPlacement(repo), stubWriteCreds{}, cop)

	if err := svc.MigrateOne(context.Background(), 7, 1, 2); err != nil {
		t.Fatalf("迁移出错：%v", err)
	}
	if len(cop.calls) != 1 || cop.calls[0] != [2]string{"from", "to"} {
		t.Errorf("应从 from 复制到 to，得到 %v", cop.calls)
	}
	// 迁入就绪、迁出记录被撤掉
	ready, _ := repo.ReadyProviders(context.Background(), 7)
	if len(ready) != 1 || ready[0] != 2 {
		t.Errorf("迁移后应只有目标存储源就绪，得到 %v", ready)
	}
}

func TestReplicateService_MigrateOneSkipsWithoutSource(t *testing.T) {
	rows := []Row{
		{Name: "源", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusDraining, Bucket: "from", Region: "r"},
		{Name: "目标", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusActive, Bucket: "to", Region: "r"},
	}
	// 源上没有 ready 副本（可能已迁完）：跳过而不是报错，让重复触发是安全的
	repo := newManageRepo(t, rows, []int64{7}, map[[2]int64]ReplicaStatus{{7, 1}: ReplicaFailed})
	cop := &migrateCopier{}
	svc := NewReplicateService(repo, NewPlacement(repo), stubWriteCreds{}, cop)

	if err := svc.MigrateOne(context.Background(), 7, 1, 2); err != nil {
		t.Errorf("源上没有可用副本时应跳过而不报错，得到 %v", err)
	}
	if len(cop.calls) != 0 {
		t.Error("不应发起复制")
	}
}

func TestReplicateService_MigrateOneKeepsSourceOnFailure(t *testing.T) {
	rows := []Row{
		{Name: "源", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusDraining, Bucket: "from", Region: "r"},
		{Name: "目标", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusActive, Bucket: "to", Region: "r"},
	}
	repo := newManageRepo(t, rows, []int64{7}, map[[2]int64]ReplicaStatus{{7, 1}: ReplicaReady})
	cop := &migrateCopier{err: errors.New("目标不可写")}
	svc := NewReplicateService(repo, NewPlacement(repo), stubWriteCreds{}, cop)

	if err := svc.MigrateOne(context.Background(), 7, 1, 2); err == nil {
		t.Error("复制失败时应返回错误")
	}
	// 关键：复制失败不能撤掉迁出副本，否则对象会变成「没有任何可用副本」
	ready, _ := repo.ReadyProviders(context.Background(), 7)
	if len(ready) != 1 || ready[0] != 1 {
		t.Errorf("复制失败后源副本必须保留，得到 %v", ready)
	}
}

func TestReplicateService_MigrateOneIdempotent(t *testing.T) {
	rows := []Row{
		{Name: "源", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusDraining, Bucket: "from", Region: "r"},
		{Name: "目标", Type: TypeS3, Role: RoleMirror, Purpose: PurposeBoth, Status: StatusActive, Bucket: "to", Region: "r"},
	}
	// 两边都已就绪：不再复制，只撤掉迁出记录
	repo := newManageRepo(t, rows, []int64{7}, map[[2]int64]ReplicaStatus{
		{7, 1}: ReplicaReady,
		{7, 2}: ReplicaReady,
	})
	cop := &migrateCopier{}
	svc := NewReplicateService(repo, NewPlacement(repo), stubWriteCreds{}, cop)

	if err := svc.MigrateOne(context.Background(), 7, 1, 2); err != nil {
		t.Fatalf("迁移出错：%v", err)
	}
	if len(cop.calls) != 0 {
		t.Error("目标已有就绪副本时不应重复复制")
	}
	ready, _ := repo.ReadyProviders(context.Background(), 7)
	if len(ready) != 1 || ready[0] != 2 {
		t.Errorf("应只保留目标存储源，得到 %v", ready)
	}
}
