package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubRemover 记录删除调用，可按 key 制造失败。
type stubRemover struct {
	removed  []string
	failKeys map[string]bool
}

func (r *stubRemover) RemoveObject(
	_ context.Context, _ Conn, _ Credential, _ ObjectKind, key string,
) (int, error) {
	if r.failKeys[key] {
		return 0, errors.New("存储服务拒绝删除")
	}
	r.removed = append(r.removed, key)
	return 3, nil // 假装删了 3 个文件
}

// newCleanupRepo 造一个带对象、副本与引用关系的存根。
func newCleanupRepo(t *testing.T, now time.Time) *fakeRepo {
	t.Helper()
	repo := newFakeRepo()
	if _, err := repo.Create(context.Background(), Row{
		Name: "主存储", Type: TypeS3, Role: RolePrimary, Purpose: PurposeBoth, Status: StatusActive,
		Bucket: "b", Region: "r", CredentialEnc: []byte("enc"),
	}); err != nil {
		t.Fatal(err)
	}
	repo.objects = map[int64]MediaObject{}
	repo.referenced = map[int64]bool{}
	repo.clock = now
	return repo
}

func addObject(repo *fakeRepo, id int64, key string, size int64, orphanedAt time.Time, referenced bool) {
	repo.objects[id] = MediaObject{ID: id, Kind: ObjectHLSDir, Key: key, SizeBytes: size, OrphanedAt: orphanedAt}
	repo.referenced[id] = referenced
}

func TestCleanupService_MarkAndUnmark(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repo := newCleanupRepo(t, now)
	addObject(repo, 1, "hls/1/a", 100, time.Time{}, true)  // 在用
	addObject(repo, 2, "hls/2/b", 200, time.Time{}, false) // 刚变孤立
	// 已标记但又被引用回来（兜底场景）
	addObject(repo, 3, "hls/3/c", 300, now.Add(-time.Hour), true)

	svc := NewCleanupService(repo, stubWriteCreds{}, &stubRemover{}, DefaultOrphanRetention)
	marked, unmarked, err := svc.Mark(context.Background())
	if err != nil {
		t.Fatalf("标记出错：%v", err)
	}
	if marked != 1 {
		t.Errorf("应新标记 1 个（对象 2），得到 %d", marked)
	}
	if unmarked != 1 {
		t.Errorf("应撤销 1 个（对象 3 又被引用），得到 %d", unmarked)
	}
	if repo.objects[1].OrphanedAt != (time.Time{}) {
		t.Error("在用的对象不该被标记")
	}
	if repo.objects[2].OrphanedAt.IsZero() {
		t.Error("孤立的对象应被标记")
	}
	if !repo.objects[3].OrphanedAt.IsZero() {
		t.Error("重新被引用的对象应撤销标记")
	}
}

func TestCleanupService_OnlyDeletesExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repo := newCleanupRepo(t, now)
	// 保留期 30 天：31 天前标记的过期，1 天前标记的没过期
	addObject(repo, 1, "hls/1/expired", 100, now.Add(-31*24*time.Hour), false)
	addObject(repo, 2, "hls/2/fresh", 200, now.Add(-24*time.Hour), false)
	if _, err := repo.SaveReplica(context.Background(), 1, 1, ReplicaReady, ""); err != nil {
		t.Fatal(err)
	}

	rem := &stubRemover{}
	svc := NewCleanupService(repo, stubWriteCreds{}, rem, DefaultOrphanRetention)
	res, err := svc.Cleanup(context.Background(), false, now)
	if err != nil {
		t.Fatalf("清理出错：%v", err)
	}
	if res.Deleted != 1 || res.Scanned != 1 {
		t.Errorf("应只删过期的 1 个，得到 %+v", res)
	}
	if res.Files != 3 || res.Bytes != 100 {
		t.Errorf("文件数与字节数统计错误：%+v", res)
	}
	if len(rem.removed) != 1 || rem.removed[0] != "hls/1/expired" {
		t.Errorf("只应删过期对象的文件，得到 %v", rem.removed)
	}
	// 过期对象的记录与副本都清掉了，未过期的还在
	if _, ok := repo.objects[1]; ok {
		t.Error("过期对象的记录应被删除")
	}
	if reps, _ := repo.ListReplicas(context.Background(), 1); len(reps) != 0 {
		t.Error("副本记录应随对象级联删除")
	}
	if _, ok := repo.objects[2]; !ok {
		t.Error("未过期的对象不该被删")
	}
}

func TestCleanupService_DryRunDeletesNothing(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repo := newCleanupRepo(t, now)
	addObject(repo, 1, "hls/1/a", 500, now.Add(-40*24*time.Hour), false)
	if _, err := repo.SaveReplica(context.Background(), 1, 1, ReplicaReady, ""); err != nil {
		t.Fatal(err)
	}

	rem := &stubRemover{}
	svc := NewCleanupService(repo, stubWriteCreds{}, rem, DefaultOrphanRetention)
	res, err := svc.Cleanup(context.Background(), true, now)
	if err != nil {
		t.Fatalf("预览出错：%v", err)
	}
	if !res.DryRun || res.Deleted != 1 || res.Bytes != 500 {
		t.Errorf("预览应报告会删 1 个对象 500 字节：%+v", res)
	}
	// 关键：预览不能动任何东西
	if len(rem.removed) != 0 {
		t.Error("预览不该删文件")
	}
	if _, ok := repo.objects[1]; !ok {
		t.Error("预览不该删对象记录")
	}
}

// 文件删不掉时必须保留对象记录：宁可留垃圾文件，也不能让数据库和存储不一致。
func TestCleanupService_KeepsRecordWhenFileDeleteFails(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repo := newCleanupRepo(t, now)
	addObject(repo, 1, "hls/1/stubborn", 100, now.Add(-40*24*time.Hour), false)
	if _, err := repo.SaveReplica(context.Background(), 1, 1, ReplicaReady, ""); err != nil {
		t.Fatal(err)
	}

	rem := &stubRemover{failKeys: map[string]bool{"hls/1/stubborn": true}}
	svc := NewCleanupService(repo, stubWriteCreds{}, rem, DefaultOrphanRetention)
	res, err := svc.Cleanup(context.Background(), false, now)
	if err != nil {
		t.Fatalf("清理不该整体失败：%v", err)
	}
	if res.Deleted != 0 || res.Kept != 1 {
		t.Errorf("删文件失败的对象应计入 kept：%+v", res)
	}
	if _, ok := repo.objects[1]; !ok {
		t.Error("文件没删掉时必须保留对象记录，下轮再试")
	}
}

// 标记后又被引用的对象不能删（删了用户就播不了）。
func TestCleanupService_SkipsRereferencedObject(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repo := newCleanupRepo(t, now)
	addObject(repo, 1, "hls/1/a", 100, now.Add(-40*24*time.Hour), false)
	if _, err := repo.SaveReplica(context.Background(), 1, 1, ReplicaReady, ""); err != nil {
		t.Fatal(err)
	}
	rem := &stubRemover{}
	svc := NewCleanupService(repo, stubWriteCreds{}, rem, DefaultOrphanRetention)

	// 列出之后、删除之前又被引用：DeleteObject 的 NOT EXISTS 兜住
	// （这里直接在调用前置位，模拟 ListExpiredOrphans 与 DeleteObject 之间的竞态）
	objs, err := repo.ListExpiredOrphans(context.Background(), now.Add(-DefaultOrphanRetention), 0, 100)
	if err != nil || len(objs) != 1 {
		t.Fatalf("应列出 1 个过期对象，得到 %d %v", len(objs), err)
	}
	repo.referenced[1] = true

	if _, err := svc.purge(context.Background(), objs[0]); err != nil {
		t.Fatalf("purge 出错：%v", err)
	}
	if _, ok := repo.objects[1]; !ok {
		t.Error("重新被引用的对象不该被删除")
	}
}

func TestCleanupService_Stats(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repo := newCleanupRepo(t, now)
	addObject(repo, 1, "hls/1/a", 100, now.Add(-40*24*time.Hour), false) // 过期
	addObject(repo, 2, "hls/2/b", 200, now.Add(-24*time.Hour), false)    // 已标记未过期
	addObject(repo, 3, "hls/3/c", 400, time.Time{}, true)                // 在用，不计

	svc := NewCleanupService(repo, stubWriteCreds{}, &stubRemover{}, DefaultOrphanRetention)
	stats, err := svc.Stats(context.Background(), now)
	if err != nil {
		t.Fatalf("统计出错：%v", err)
	}
	if stats.Total != 2 || stats.Expired != 1 || stats.Bytes != 300 {
		t.Errorf("统计错误：%+v（期望 total=2 expired=1 bytes=300）", stats)
	}
}

func TestCleanupService_RetentionDefaults(t *testing.T) {
	repo := newCleanupRepo(t, time.Now())
	// 0 或负数都回落到默认 30 天，避免配置写错导致立即删除
	for _, d := range []time.Duration{0, -time.Hour} {
		if got := NewCleanupService(repo, stubWriteCreds{}, &stubRemover{}, d).Retention(); got != DefaultOrphanRetention {
			t.Errorf("保留期 %v 应回落到默认值，得到 %v", d, got)
		}
	}
	if got := NewCleanupService(repo, stubWriteCreds{}, &stubRemover{}, 48*time.Hour).Retention(); got != 48*time.Hour {
		t.Errorf("显式配置的保留期应生效，得到 %v", got)
	}
}
