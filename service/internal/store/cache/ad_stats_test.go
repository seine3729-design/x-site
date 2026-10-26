package cache

import (
	"context"
	"testing"
	"time"

	"xsite/service/internal/domain/ops"
)

func TestAdStats_Integration(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	s := NewAdStats(client, prefix)
	t.Cleanup(func() {
		keys, _ := client.Do(context.Background(), client.B().Keys().Pattern(prefix+"*").Build()).AsStrSlice()
		for _, k := range keys {
			_ = client.Do(context.Background(), client.B().Del().Key(k).Build()).Error()
		}
	})

	// 去重：同设备、同广告、同事件 1 秒内只有第一次算数；换事件或换设备分别算
	for _, tt := range []struct {
		device string
		ad     int64
		kind   ops.AdEventKind
		want   bool
	}{
		{"d1", 1, ops.AdImpression, true},
		{"d1", 1, ops.AdImpression, false},
		{"d1", 1, ops.AdClick, true},
		{"d2", 1, ops.AdImpression, true},
		{"d1", 2, ops.AdImpression, true},
	} {
		got, err := s.FirstEvent(ctx, tt.device, tt.ad, tt.kind)
		if err != nil || got != tt.want {
			t.Fatalf("%+v：得到 %v %v", tt, got, err)
		}
	}
	ttl, err := client.Do(ctx, client.B().Pttl().Key(prefix+"dedup:impression:1:d1").Build()).AsInt64()
	if err != nil || ttl <= 0 || ttl > 1000 {
		t.Fatalf("去重键 1 秒后过期：%d %v", ttl, err)
	}
	time.Sleep(1100 * time.Millisecond)
	if got, _ := s.FirstEvent(ctx, "d1", 1, ops.AdImpression); !got {
		t.Fatal("过了 1 秒重新计数")
	}

	// 计数与频控
	for _, ev := range []struct {
		ad          int64
		kind        ops.AdEventKind
		device, day string
	}{
		{1, ops.AdImpression, "d1", "20261025"},
		{1, ops.AdImpression, "d1", "20261025"},
		{1, ops.AdClick, "d1", ""},
		{2, ops.AdImpression, "d1", ""},
		{1, ops.AdImpression, "d2", "20261025"},
		{1, ops.AdImpression, "d1", "20261026"},
	} {
		if err := s.AddEvent(ctx, ev.ad, ev.kind, ev.device, ev.day); err != nil {
			t.Fatal(err)
		}
	}
	daily, err := s.DailyImpressions(ctx, "d1", "20261025", []int64{1, 2, 3})
	if err != nil || len(daily) != 1 || daily[1] != 2 {
		t.Fatalf("d1 当天对 1 号曝光 2 次，没传日期的不计：%v %v", daily, err)
	}
	if d, _ := s.DailyImpressions(ctx, "d2", "20261025", []int64{1}); d[1] != 1 {
		t.Fatalf("按设备分开计：%v", d)
	}
	if d, _ := s.DailyImpressions(ctx, "d1", "20261026", []int64{1}); d[1] != 1 {
		t.Fatalf("按日期分开计：%v", d)
	}
	ttl, _ = client.Do(ctx, client.B().Pttl().Key(prefix+"freq:20261025:d1").Build()).AsInt64()
	if ttl < int64(24*time.Hour/time.Millisecond) {
		t.Fatalf("频控计数至少保留一天：%dms", ttl)
	}

	// 落库：取出后清零，新事件进下一份；同一个 token 重复取拿到同一份（写库失败后重试）
	got, err := s.TakePending(ctx, "job-1")
	want := map[int64]ops.AdCounts{1: {Impressions: 4, Clicks: 1}, 2: {Impressions: 1}}
	if err != nil || len(got) != 2 || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("取出待落库的计数：%v %v", got, err)
	}
	if err := s.AddEvent(ctx, 3, ops.AdClick, "d1", ""); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.TakePending(ctx, "job-1"); len(again) != 2 || again[1] != want[1] {
		t.Fatalf("同一个 token 重试时拿回上次那一份，新事件留给下一轮：%v", again)
	}
	if err := s.DonePending(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	if next, _ := s.TakePending(ctx, "job-2"); len(next) != 1 || next[3] != (ops.AdCounts{Clicks: 1}) {
		t.Fatalf("下一轮拿到新事件：%v", next)
	}
	_ = s.DonePending(ctx, "job-2")
	if empty, err := s.TakePending(ctx, "job-3"); err != nil || len(empty) != 0 {
		t.Fatalf("没有待落库的计数时返回空：%v %v", empty, err)
	}
}
