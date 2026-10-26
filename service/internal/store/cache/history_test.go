package cache

import (
	"context"
	"fmt"
	"testing"
)

// 观看历史合并写缓冲（S2-3）集成测试：合并写（同剧只留最新）、drain 取出 / 重试 / 清理、
// Clear 删指定剧、ClearAll 只删本人。
func TestHistoryStore_Integration(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	s := NewHistoryStore(client, prefix)
	t.Cleanup(func() {
		keys, _ := client.Do(context.Background(), client.B().Keys().Pattern(prefix+"*").Build()).AsStrSlice()
		for _, k := range keys {
			_ = client.Do(context.Background(), client.B().Del().Key(k).Build()).Error()
		}
	})

	// 合并写：同一 (uid, drama) 多次上报只保留最新
	for _, r := range []struct {
		uid, drama int64
		payload    string
	}{
		{1, 10, `{"p":5}`},
		{1, 10, `{"p":30}`}, // 覆盖上一条
		{1, 11, `{"p":1}`},
		{2, 20, `{"p":9}`},
	} {
		if err := s.Report(ctx, r.uid, r.drama, r.payload); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.TakePending(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("合并后应取出 3 条，得到 %d：%+v", len(got), got)
	}
	byKey := map[string]string{}
	for _, p := range got {
		byKey[fmt.Sprintf("%d:%d", p.UserID, p.DramaID)] = p.Payload
	}
	if byKey["1:10"] != `{"p":30}` {
		t.Fatalf("1:10 应保留最新 payload，得到 %q", byKey["1:10"])
	}

	// 同 token 重试拿同一份；取走后新上报进下一轮
	if err := s.Report(ctx, 3, 30, `{"p":7}`); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.TakePending(ctx, "job-1"); len(again) != 3 {
		t.Fatalf("同 token 重试应拿回同一份 3 条，得到 %d", len(again))
	}
	if err := s.DonePending(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	if next, _ := s.TakePending(ctx, "job-2"); len(next) != 1 || next[0].DramaID != 30 {
		t.Fatalf("下一轮应拿到新上报的 1 条，得到 %+v", next)
	}
	_ = s.DonePending(ctx, "job-2")

	// Clear：删指定剧的待落库断点；ClearAll：删某用户全部、不碰别人
	for _, r := range []struct {
		uid, drama int64
	}{{5, 50}, {5, 51}, {5, 52}} {
		if err := s.Report(ctx, r.uid, r.drama, `{"p":1}`); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Clear(ctx, 5, []int64{50, 52}); err != nil {
		t.Fatal(err)
	}
	if err := s.Report(ctx, 6, 60, `{"p":1}`); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearAll(ctx, 5); err != nil {
		t.Fatal(err)
	}
	left, _ := s.TakePending(ctx, "job-3")
	if len(left) != 1 || left[0].UserID != 6 {
		t.Fatalf("Clear/ClearAll 后应只剩用户 6 的 1 条，得到 %+v", left)
	}
	_ = s.DonePending(ctx, "job-3")
}
