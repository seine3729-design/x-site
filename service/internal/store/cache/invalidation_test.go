package cache

import (
	"context"
	"testing"
	"time"
)

// 集成测试：连本地 compose 的 Valkey（./dev.sh go test 注入 VALKEY_URL），验证 Pub/Sub 失效广播。
func TestInvalidator_PublishSubscribe(t *testing.T) {
	c, prefix := testClient(t)
	inv := NewInvalidator(c, prefix+"invalidate")

	got := make(chan string, 8)
	stop := inv.Subscribe(func(topic string) { got <- topic })
	defer stop()

	// 订阅在后台 goroutine 里异步建立，首条 PUBLISH 可能早于 SUBSCRIBE 生效；重复发布直到收到为止。
	ctx := context.Background()
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if err := inv.Publish(ctx, "site_config"); err != nil {
				t.Fatalf("发布失效消息失败：%v", err)
			}
		case topic := <-got:
			if topic != "site_config" {
				t.Fatalf("收到的 topic = %q，期望 site_config", topic)
			}
			return // 收到即通过
		case <-deadline:
			t.Fatal("5 秒内未收到失效消息，Pub/Sub 不通")
		}
	}
}

// 订阅建立后，一次发布应被收到一次（不丢不重），且能分辨 topic。
func TestInvalidator_DeliversEachTopicOnce(t *testing.T) {
	c, prefix := testClient(t)
	inv := NewInvalidator(c, prefix+"invalidate")

	got := make(chan string, 8)
	stop := inv.Subscribe(func(topic string) { got <- topic })

	// 先确保订阅已生效：重复发一个握手 topic 直到收到，再清空通道。
	ctx := context.Background()
	waitSubscribed(t, inv, got)

	if err := inv.Publish(ctx, "line_table"); err != nil {
		t.Fatalf("发布失败：%v", err)
	}
	select {
	case topic := <-got:
		if topic != "line_table" {
			t.Fatalf("topic = %q，期望 line_table", topic)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("2 秒内未收到 line_table")
	}

	// stop 后不再收到消息。
	stop()
	if err := inv.Publish(ctx, "after_stop"); err != nil {
		t.Fatalf("发布失败：%v", err)
	}
	select {
	case topic := <-got:
		t.Fatalf("stop 之后不应再收到消息，却收到 %q", topic)
	case <-time.After(500 * time.Millisecond):
	}
}

// waitSubscribed 反复发 "__ready__" 直到订阅侧收到，然后把通道里残留的握手消息排空。
func waitSubscribed(t *testing.T, inv *Invalidator, got <-chan string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if err := inv.Publish(ctx, "__ready__"); err != nil {
				t.Fatalf("发布失败：%v", err)
			}
		case topic := <-got:
			if topic != "__ready__" {
				continue
			}
			// 再排空可能已在途的多条握手消息
			for {
				select {
				case <-got:
				case <-time.After(200 * time.Millisecond):
					return
				}
			}
		case <-deadline:
			t.Fatal("订阅未能在 5 秒内建立")
		}
	}
}
