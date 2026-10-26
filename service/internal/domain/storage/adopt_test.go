package storage

import (
	"context"
	"errors"
	"testing"
)

// stubVerifier 按对象键决定「是否存在」。
type stubVerifier struct {
	exists map[string]VerifyResult
	err    error
	calls  int
}

func (v *stubVerifier) VerifyObject(
	_ context.Context, _ Conn, _ Credential, _ ObjectKind, key string,
) (VerifyResult, error) {
	v.calls++
	if v.err != nil {
		return VerifyResult{}, v.err
	}
	return v.exists[key], nil
}

type adoptEnqueuer struct {
	calls [][2]int64 // objectID, providerID
}

func (e *adoptEnqueuer) EnqueueAdopt(_ context.Context, objectID, providerID int64) error {
	e.calls = append(e.calls, [2]int64{objectID, providerID})
	return nil
}

func newAdoptRepo(t *testing.T, objects map[int64]string, replicas map[[2]int64]ReplicaStatus) *fakeRepo {
	t.Helper()
	repo := newFakeRepo()
	if _, err := repo.Create(context.Background(), Row{
		Name: "原默认桶", Type: TypeCustom, Subtype: SubtypeS3Compatible, Role: RolePrimary,
		Purpose: PurposeBoth, Status: StatusActive, Bucket: "xsite-media", Region: "us-east-1",
		CredentialEnc: []byte("enc"), // 有凭据，WriteCredential 由存根提供
	}); err != nil {
		t.Fatal(err)
	}
	repo.objects = map[int64]MediaObject{}
	for id, key := range objects {
		repo.objects[id] = MediaObject{ID: id, Kind: ObjectHLSDir, Key: key}
	}
	for k, status := range replicas {
		if _, err := repo.SaveReplica(context.Background(), k[0], k[1], status, ""); err != nil {
			t.Fatal(err)
		}
	}
	return repo
}

func TestAdoptService_EnqueuesOnlyOrphans(t *testing.T) {
	// 对象 1、2 没有副本记录（历史产物），对象 3 已有副本
	repo := newAdoptRepo(t, map[int64]string{1: "hls/1/a", 2: "hls/2/b", 3: "hls/3/c"},
		map[[2]int64]ReplicaStatus{{3, 1}: ReplicaReady})
	enq := &adoptEnqueuer{}
	svc := NewAdoptService(repo, stubWriteCreds{}, &stubVerifier{}, enq)

	res, err := svc.Adopt(context.Background(), AdoptRequest{ProviderID: 1})
	if err != nil {
		t.Fatalf("接管出错：%v", err)
	}
	if res.Objects != 2 {
		t.Errorf("应只入队 2 个没有副本的对象，得到 %d", res.Objects)
	}
	for _, c := range enq.calls {
		if c[0] == 3 {
			t.Error("已有副本的对象不该入队")
		}
		if c[1] != 1 {
			t.Errorf("存储源编号应为 1，得到 %d", c[1])
		}
	}
}

func TestAdoptService_RejectsBadRequest(t *testing.T) {
	repo := newAdoptRepo(t, nil, nil)
	enq := &adoptEnqueuer{}
	svc := NewAdoptService(repo, stubWriteCreds{}, &stubVerifier{}, enq)

	if _, err := svc.Adopt(context.Background(), AdoptRequest{}); err == nil {
		t.Error("没给存储源应报错")
	}
	if _, err := svc.Adopt(context.Background(), AdoptRequest{ProviderID: 999}); err == nil {
		t.Error("存储源不存在应报错")
	}

	// 只读 HTTP 源不能作为副本所在的存储源
	httpRow := Row{
		Name: "HTTP 源", Type: TypeCustom, Subtype: SubtypeHTTPOrigin, Role: RoleReadonly,
		Purpose: PurposeBoth, Status: StatusActive, Endpoint: "https://cdn.example.com",
	}
	row, err := repo.Create(context.Background(), httpRow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Adopt(context.Background(), AdoptRequest{ProviderID: row.ID}); err == nil {
		t.Error("HTTP 源应被拒绝")
	}
}

// 核心：只有在存储源上真实存在的对象才登记副本，否则选源会把线路下发给没有文件的存储源。
func TestAdoptService_AdoptOneVerifiesExistence(t *testing.T) {
	repo := newAdoptRepo(t, map[int64]string{1: "hls/1/a", 2: "hls/2/missing"}, nil)
	ver := &stubVerifier{exists: map[string]VerifyResult{
		"hls/1/a": {Exists: true, Objects: 12, Bytes: 2048},
		// hls/2/missing 不在 map 里 → Exists=false
	}}
	svc := NewAdoptService(repo, stubWriteCreds{}, ver, nil)

	// 存在 → 登记 ready 副本
	if err := svc.AdoptOne(context.Background(), 1, 1); err != nil {
		t.Fatalf("接管存在的对象出错：%v", err)
	}
	ready, _ := repo.ReadyProviders(context.Background(), 1)
	if len(ready) != 1 || ready[0] != 1 {
		t.Errorf("应登记 ready 副本，得到 %v", ready)
	}

	// 不存在 → 跳过且不报错（历史数据里可能有文件已被清掉的记录），也不写副本
	if err := svc.AdoptOne(context.Background(), 2, 1); err != nil {
		t.Errorf("对象不存在时应跳过而不报错，得到 %v", err)
	}
	if reps, _ := repo.ListReplicas(context.Background(), 2); len(reps) != 0 {
		t.Errorf("不存在的对象不该写出副本记录，得到 %+v", reps)
	}
}

func TestAdoptService_AdoptOneSkipsWhenAlreadyHasReplica(t *testing.T) {
	repo := newAdoptRepo(t, map[int64]string{1: "hls/1/a"},
		map[[2]int64]ReplicaStatus{{1, 1}: ReplicaReady})
	ver := &stubVerifier{exists: map[string]VerifyResult{"hls/1/a": {Exists: true}}}
	svc := NewAdoptService(repo, stubWriteCreds{}, ver, nil)

	if err := svc.AdoptOne(context.Background(), 1, 1); err != nil {
		t.Fatalf("出错：%v", err)
	}
	// 已有副本就不再校验，省掉一次存储访问
	if ver.calls != 0 {
		t.Errorf("已有副本记录时不该再校验存在性，实际调用 %d 次", ver.calls)
	}
}

func TestAdoptService_AdoptOnePropagatesVerifyError(t *testing.T) {
	repo := newAdoptRepo(t, map[int64]string{1: "hls/1/a"}, nil)
	// 校验本身失败（存储服务不可用）要返回错误让 River 重试，不能当成「不存在」
	ver := &stubVerifier{err: errors.New("存储服务暂时不可用")}
	svc := NewAdoptService(repo, stubWriteCreds{}, ver, nil)

	if err := svc.AdoptOne(context.Background(), 1, 1); err == nil {
		t.Error("校验失败应返回错误（与「对象不存在」区分开）")
	}
	if reps, _ := repo.ListReplicas(context.Background(), 1); len(reps) != 0 {
		t.Error("校验失败时不该写副本")
	}
}
