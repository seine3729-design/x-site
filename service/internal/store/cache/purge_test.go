package cache

import (
	"context"
	"strings"
	"testing"
)

func TestCheckPattern(t *testing.T) {
	// 过于笼统的模式会把会话、限流计数、待落库统计一起清掉，必须拦住
	bad := []string{"", "*", "xsite:*", "xsite:", " xsite:content:*", "xsite:content:* ", "content:*"}
	for _, p := range bad {
		if err := checkPattern(p); err == nil {
			t.Errorf("模式 %q 应当被拒绝", p)
		}
	}
	good := []string{
		"xsite:content:*", "xsite:content:drama:1024", "xsite:ops:rank:*",
		"xsite:site_config:public", "xsite:play:manifest:media/d1/e1*",
	}
	for _, p := range good {
		if err := checkPattern(p); err != nil {
			t.Errorf("模式 %q 应当通过：%v", p, err)
		}
	}
}

func TestPurger_Integration(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	p := NewPurger(client)

	// prefix 形如 xsite:test:<纳秒>:，再加一级用途凑够 checkPattern 要求的两个冒号
	cacheKeys := []string{prefix + "cache:a", prefix + "cache:b", prefix + "cache:c"}
	keepKeys := []string{prefix + "sess:x", prefix + "sess:y"}
	for _, k := range append(append([]string{}, cacheKeys...), keepKeys...) {
		if err := client.Do(ctx, client.B().Set().Key(k).Value("v").Build()).Error(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		keys, _ := client.Do(context.Background(), client.B().Keys().Pattern(prefix+"*").Build()).AsStrSlice()
		for _, k := range keys {
			_ = client.Do(context.Background(), client.B().Del().Key(k).Build()).Error()
		}
	})

	// 按前缀计数
	if n, err := p.Count(ctx, prefix+"cache:*"); err != nil || n != 3 {
		t.Fatalf("按前缀计数 = %d, %v，期望 3", n, err)
	}
	// 单键退化为存在性检查
	if n, err := p.Count(ctx, prefix+"cache:a"); err != nil || n != 1 {
		t.Fatalf("单键计数 = %d, %v，期望 1", n, err)
	}
	if n, err := p.Count(ctx, prefix+"cache:missing"); err != nil || n != 0 {
		t.Fatalf("不存在的键计数 = %d, %v，期望 0", n, err)
	}

	// 按前缀清除，只动匹配的键
	if n, err := p.Purge(ctx, prefix+"cache:*"); err != nil || n != 3 {
		t.Fatalf("按前缀清除 = %d, %v，期望 3", n, err)
	}
	if n, _ := p.Count(ctx, prefix+"cache:*"); n != 0 {
		t.Errorf("清除后还剩 %d 个键", n)
	}
	// 同一个父前缀下、不匹配的键必须留着（这就是「不能误伤会话」的那条线）
	if n, _ := p.Count(ctx, prefix+"sess:*"); n != 2 {
		t.Errorf("不该被清的键剩 %d 个，期望 2", n)
	}

	// 清不存在的键不报错，返回 0
	if n, err := p.Purge(ctx, prefix+"cache:*"); err != nil || n != 0 {
		t.Errorf("重复清除 = %d, %v，期望 0", n, err)
	}

	// 单键清除
	if n, err := p.Purge(ctx, prefix+"sess:x"); err != nil || n != 1 {
		t.Fatalf("单键清除 = %d, %v，期望 1", n, err)
	}
	if n, _ := p.Count(ctx, prefix+"sess:*"); n != 1 {
		t.Errorf("单键清除后剩 %d 个，期望 1", n)
	}

	// 统计：键总数与内存占用
	keys, used, text, err := p.Stats(ctx)
	if err != nil {
		t.Fatalf("读取统计失败: %v", err)
	}
	if keys <= 0 {
		t.Errorf("键总数 = %d，期望大于 0", keys)
	}
	if used <= 0 || text == "" {
		t.Errorf("内存占用 = %d / %q，期望都有值", used, text)
	}
}

// 键很多时要能走完多轮 SCAN。
func TestPurger_ScanManyKeys(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	p := NewPurger(client)
	t.Cleanup(func() {
		keys, _ := client.Do(context.Background(), client.B().Keys().Pattern(prefix+"*").Build()).AsStrSlice()
		for _, k := range keys {
			_ = client.Do(context.Background(), client.B().Del().Key(k).Build()).Error()
		}
	})

	// 超过单次 SCAN 的 COUNT 与单次 UNLINK 的批量上限，确保游标与分批都被走到
	const total = scanCount*2 + unlinkBatch + 37
	for i := range total {
		k := prefix + "bulk:" + strings.Repeat("k", 1) + itoaFast(i)
		if err := client.Do(ctx, client.B().Set().Key(k).Value("v").Build()).Error(); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := p.Count(ctx, prefix+"bulk:*"); err != nil || n != total {
		t.Fatalf("计数 = %d, %v，期望 %d", n, err, total)
	}
	if n, err := p.Purge(ctx, prefix+"bulk:*"); err != nil || n != total {
		t.Fatalf("清除 = %d, %v，期望 %d", n, err, total)
	}
	if n, _ := p.Count(ctx, prefix+"bulk:*"); n != 0 {
		t.Errorf("清除后还剩 %d 个键", n)
	}
}

func itoaFast(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
