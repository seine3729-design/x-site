package cache

import (
	"context"
	"testing"

	"xsite/service/internal/domain/ops"
)

func TestHotCounter_Integration(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	c := NewHotCounter(client, prefix)
	t.Cleanup(func() {
		keys, _ := client.Do(context.Background(), client.B().Keys().Pattern(prefix+"*").Build()).AsStrSlice()
		for _, k := range keys {
			_ = client.Do(context.Background(), client.B().Del().Key(k).Build()).Error()
		}
	})

	for _, tt := range []struct {
		word string
		miss bool
	}{
		{"霸道总裁", false}, {"霸道总裁", true}, {"ceo 甜宠", false}, {"s:m", true},
	} {
		if err := c.AddSearch(ctx, tt.word, tt.miss); err != nil {
			t.Fatal(err)
		}
	}
	got, err := c.TakePending(ctx, "job-1")
	if err != nil || len(got) != 3 || got["霸道总裁"] != (ops.HotCounts{Searches: 2, Misses: 1}) ||
		got["ceo 甜宠"] != (ops.HotCounts{Searches: 1}) || got["s:m"] != (ops.HotCounts{Searches: 1, Misses: 1}) {
		t.Fatalf("取出待落库的计数（词里有冒号也能正确拆开）：%v %v", got, err)
	}
	_ = c.AddSearch(ctx, "重生", false)
	if again, _ := c.TakePending(ctx, "job-1"); len(again) != 3 {
		t.Fatalf("同一个 token 重试时拿回上次那一份，新词留给下一轮：%v", again)
	}
	if err := c.DonePending(ctx, "job-1"); err != nil {
		t.Fatal(err)
	}
	if next, _ := c.TakePending(ctx, "job-2"); len(next) != 1 || next["重生"] != (ops.HotCounts{Searches: 1}) {
		t.Fatalf("下一轮拿到新词：%v", next)
	}
	_ = c.DonePending(ctx, "job-2")

	// 一轮内不同词的数量有上限：已有的词照常累加，新词丢弃（防止刷随机词撑爆内存）
	c.maxFields = 4 // 每个词最多两个字段（搜索 + 片库没有）
	for _, w := range []string{"a", "b", "c"} {
		_ = c.AddSearch(ctx, w, true)
	}
	_ = c.AddSearch(ctx, "a", false)
	capped, _ := c.TakePending(ctx, "job-3")
	if len(capped) != 2 || capped["a"] != (ops.HotCounts{Searches: 2, Misses: 1}) || capped["c"] != (ops.HotCounts{}) {
		t.Fatalf("超过上限的新词不计，已有的照常累加：%v", capped)
	}
}
