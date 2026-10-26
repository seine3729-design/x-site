package analytics

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeRepo struct{ got []Event }

func (f *fakeRepo) InsertEvents(_ context.Context, events []Event) (int, error) {
	f.got = append(f.got, events...)
	return len(events), nil
}

func ptr[T any](v T) *T { return &v }

func TestCollect(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	fixed := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }

	n, err := svc.Collect(context.Background(), CollectInput{
		Client:   "web",
		DeviceID: "dev-1",
		Events: []EventItem{
			{Type: "play_start", DramaID: ptr(int64(7))},
			{Type: "bogus_type"},                                    // 未知类型：静默跳过
			{Type: " watch ", Props: map[string]any{"seconds": 30}}, // 前后空白应被去掉后匹配
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("应入库 2 条（未知类型被跳过），得 %d", n)
	}
	if len(repo.got) != 2 {
		t.Fatalf("repo 收到 %d 条", len(repo.got))
	}
	if !repo.got[0].OccurredAt.Equal(fixed) {
		t.Errorf("occurred_at 应为服务端时间，得 %v", repo.got[0].OccurredAt)
	}
	if repo.got[0].Client != "web" || repo.got[0].DeviceID != "dev-1" {
		t.Errorf("上下文透传错误: %+v", repo.got[0])
	}
	if repo.got[0].DramaID == nil || *repo.got[0].DramaID != 7 {
		t.Error("drama_id 透传错误")
	}
	if !strings.Contains(string(repo.got[1].Props), "seconds") {
		t.Errorf("props 应序列化，得 %s", repo.got[1].Props)
	}
}

func TestCollect_BatchLimit(t *testing.T) {
	svc := NewService(&fakeRepo{})
	items := make([]EventItem, MaxBatch+1)
	for i := range items {
		items[i] = EventItem{Type: "visitor"}
	}
	if _, err := svc.Collect(context.Background(), CollectInput{Events: items}); err == nil {
		t.Error("超过批量上限应返回 400")
	}
}

func TestCollect_ClientAndEmpty(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	n, _ := svc.Collect(context.Background(), CollectInput{Client: "hacker", Events: []EventItem{{Type: "visitor"}}})
	if n != 1 || repo.got[0].Client != "" {
		t.Error("非法 client 应归一为空")
	}

	empty := NewService(&fakeRepo{})
	if n, err := empty.Collect(context.Background(), CollectInput{}); err != nil || n != 0 {
		t.Error("空事件应返回 0 且不报错")
	}

	allBad := &fakeRepo{}
	if n, _ := NewService(allBad).Collect(context.Background(), CollectInput{Events: []EventItem{{Type: "xxx"}}}); n != 0 || len(allBad.got) != 0 {
		t.Error("全部未知类型应为 0 条且不写库")
	}
}

func TestCollect_PropsTooLarge(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	big := strings.Repeat("x", MaxPropsBytes+10)
	if n, _ := svc.Collect(context.Background(), CollectInput{Events: []EventItem{
		{Type: "visitor", Props: map[string]any{"k": big}},
	}}); n != 0 {
		t.Error("props 过大的事件应被跳过")
	}
}
