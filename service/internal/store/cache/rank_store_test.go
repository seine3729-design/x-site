package cache

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xsite/service/internal/domain/ops"
)

func TestRankStore_Integration(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	s := NewRankStore(client, prefix)
	t.Cleanup(func() {
		keys, _ := client.Do(context.Background(), client.B().Keys().Pattern(prefix+"*").Build()).AsStrSlice()
		for _, k := range keys {
			_ = client.Do(context.Background(), client.B().Del().Key(k).Build()).Error()
		}
	})

	if _, _, ok, err := s.Top(ctx, ops.BoardDramaWeek, 20); ok || err != nil {
		t.Fatalf("还没算过时 ok=false：%v %v", ok, err)
	}
	at := time.Date(2026, 10, 24, 9, 0, 0, 123, time.UTC)
	entries := []ops.RankEntry{{DramaID: 7, Score: 15.5, Pinned: true}, {DramaID: 3, Score: 300}, {DramaID: 9, Score: 0}}
	if err := s.Replace(ctx, ops.BoardDramaWeek, entries, at); err != nil {
		t.Fatal(err)
	}
	got, gotAt, ok, err := s.Top(ctx, ops.BoardDramaWeek, 2)
	if err != nil || !ok || !gotAt.Equal(at) || len(got) != 2 || got[0] != (ops.RankEntry{DramaID: 7, Score: 15.5}) || got[1] != (ops.RankEntry{DramaID: 3, Score: 300}) {
		t.Fatalf("按名次返回前 n 名与热度（置顶的分数可以比后面的低）：%+v %v %v %v", got, gotAt, ok, err)
	}
	if all, _, _, _ := s.Top(ctx, ops.BoardDramaWeek, 100); len(all) != 3 || all[2].Score != 0 {
		t.Fatalf("n 超过名次数时返回全部：%+v", all)
	}
	for _, k := range []string{prefix + "drama_week", prefix + "drama_week:heat"} {
		if ttl, _ := client.Do(ctx, client.B().Pttl().Key(k).Build()).AsInt64(); ttl != -1 {
			t.Fatalf("线上榜单不应过期（临时键的过期时间要去掉）：%s %d", k, ttl)
		}
	}
	if tmp, _ := client.Do(ctx, client.B().Keys().Pattern(prefix+"*:tmp:*").Build()).AsStrSlice(); len(tmp) != 0 {
		t.Fatalf("交换后不留临时键：%v", tmp)
	}
	if _, _, ok, _ := s.Top(ctx, ops.BoardDramaMonth, 20); ok {
		t.Fatal("两张榜互不影响")
	}

	// 空榜：清空线上榜单，但记作「算过」
	if err := s.Replace(ctx, ops.BoardDramaWeek, nil, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, gotAt, ok, err := s.Top(ctx, ops.BoardDramaWeek, 20); err != nil || !ok || len(got) != 0 || !gotAt.Equal(at.Add(time.Minute)) {
		t.Fatalf("空榜：%+v %v %v %v", got, gotAt, ok, err)
	}
}

// 验收（S3-5）：重算期间读接口无空窗——并发读榜单时反复替换，读到的始终是完整的一份。
func TestRankStore_NoEmptyWindowDuringReplace(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	s := NewRankStore(client, prefix)
	t.Cleanup(func() {
		keys, _ := client.Do(context.Background(), client.B().Keys().Pattern(prefix+"*").Build()).AsStrSlice()
		for _, k := range keys {
			_ = client.Do(context.Background(), client.B().Del().Key(k).Build()).Error()
		}
	})
	board := func(base int64) []ops.RankEntry {
		out := make([]ops.RankEntry, 100)
		for i := range out {
			out[i] = ops.RankEntry{DramaID: base + int64(i), Score: float64(1000 - i)}
		}
		return out
	}
	if err := s.Replace(ctx, ops.BoardDramaWeek, board(1), time.Now()); err != nil {
		t.Fatal(err)
	}

	var reads, bad atomic.Int64
	var firstBad atomic.Value
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				got, _, ok, err := s.Top(ctx, ops.BoardDramaWeek, 20)
				reads.Add(1)
				// 每一份榜单的前 20 名都来自同一次重算：编号连续、热度从 1000 递减
				valid := err == nil && ok && len(got) == 20
				for i := 1; valid && i < len(got); i++ {
					valid = got[i].DramaID == got[0].DramaID+int64(i) && got[i].Score == float64(1000-i)
				}
				if !valid {
					bad.Add(1)
					firstBad.CompareAndSwap(nil, got)
				}
			}
		}()
	}
	for i := range 200 {
		if err := s.Replace(ctx, ops.BoardDramaWeek, board(int64(i%2*1000+1)), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if reads.Load() < 200 || bad.Load() != 0 {
		t.Fatalf("并发读 %d 次，其中 %d 次读到空榜或不完整的榜单：%v", reads.Load(), bad.Load(), firstBad.Load())
	}
	t.Logf("替换 200 次期间并发读 %d 次，都读到完整的榜单", reads.Load())
}
