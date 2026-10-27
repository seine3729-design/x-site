package platform_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"xsite/service/internal/domain/platform"
	"xsite/service/internal/store/cache"
)

// sharedRepo 是两实例共用的「数据库」：A 改了版本，B 能读到。
type sharedRepo struct {
	mu  sync.Mutex
	cfg platform.SiteConfig
}

func (r *sharedRepo) GetPublicSiteConfig(context.Context) (platform.SiteConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cfg, nil
}

func (r *sharedRepo) BumpVersion(context.Context) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cfg.Version++
	return r.cfg.Version, nil
}

// 端到端验收（S1-9「改配置后 5 秒内各实例生效」）：实例 A 改配置，实例 B 经 Pub/Sub 在 1 秒内拿到新版本，
// 远快于一级缓存 3 秒的兜底 TTL —— 证明是 Pub/Sub 广播在起作用，而不是等 TTL 自然过期。连本地 Valkey，未配置则跳过。
func TestSiteConfig_PropagatesAcrossInstances(t *testing.T) {
	url := os.Getenv("VALKEY_URL")
	if url == "" {
		t.Skip("未设置 VALKEY_URL，跳过跨实例失效集成测试")
	}
	client, err := cache.NewClient(url)
	if err != nil {
		t.Skipf("Valkey 不可用，跳过：%v", err)
	}
	t.Cleanup(client.Close)

	ctx := context.Background()
	prefix := fmt.Sprintf("xsite:test:%d:", time.Now().UnixNano())
	channel := prefix + "invalidate"
	repo := &sharedRepo{cfg: platform.SiteConfig{
		Version: 1, Site: json.RawMessage(`{"site_name":"x"}`), Theme: json.RawMessage(`{}`),
		Layout: json.RawMessage(`{}`), SEO: json.RawMessage(`{}`), AgeGate: json.RawMessage(`{}`),
		Features: json.RawMessage(`{}`), Addresses: json.RawMessage(`{}`), UpdatedAt: time.Now().UTC(),
	}}
	t.Cleanup(func() {
		// 清掉本测试写进 Valkey 的二级缓存键
		_ = cache.New(client, prefix).Delete(context.Background(), "site_config:public")
	})

	// 两个实例共用同一个 Valkey（同前缀 → 同二级缓存键；同频道 → 同失效广播），各自独立的一级缓存。
	svcA := platform.NewSiteConfigService(repo, cache.New(client, prefix), cache.NewInvalidator(client, channel), nil)
	svcB := platform.NewSiteConfigService(repo, cache.New(client, prefix), cache.NewInvalidator(client, channel), nil)

	// B 订阅失效广播：先清本地缓存，再把 topic 送进通道供测试观察（顺序保证收到时失效已完成）。
	seen := make(chan string, 16)
	stop := cache.NewInvalidator(client, channel).Subscribe(func(topic string) {
		if topic == platform.SiteConfigTopic {
			svcB.OnInvalidate(context.Background())
		}
		seen <- topic
	})
	defer stop()

	// 等订阅真正生效：反复发探测消息直到收到。
	waitReady(t, cache.NewInvalidator(client, channel), seen)

	// B 先读到版本 1，填满它的一级与二级缓存。
	if cfg, err := svcB.Get(ctx); err != nil || cfg.Version != 1 {
		t.Fatalf("实例 B 初始应读到版本 1：version=%d err=%v", cfg.Version, err)
	}

	// A 改配置（版本 +1 并广播）。
	start := time.Now()
	if v, err := svcA.BumpVersion(ctx); err != nil || v != 2 {
		t.Fatalf("实例 A 改配置应把版本 +1 到 2：v=%d err=%v", v, err)
	}

	// B 应在收到广播后拿到版本 2；限定 1 秒内（< 一级 TTL 3 秒）以证明走的是广播。
	if !waitTopic(seen, platform.SiteConfigTopic, time.Second) {
		t.Fatal("实例 B 1 秒内未收到站点配置失效广播")
	}
	cfg, err := svcB.Get(ctx)
	if err != nil || cfg.Version != 2 {
		t.Fatalf("实例 B 收到广播后应读到版本 2：version=%d err=%v", cfg.Version, err)
	}
	t.Logf("跨实例生效耗时 %v（一级缓存兜底 TTL 为 %v）", time.Since(start), 3*time.Second)
}

// waitReady 反复发探测 topic 直到订阅侧收到，然后排空通道。
func waitReady(t *testing.T, inv *cache.Invalidator, seen <-chan string) {
	t.Helper()
	ctx := context.Background()
	deadline := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			if err := inv.Publish(ctx, "__ready__"); err != nil {
				t.Fatalf("发布探测消息失败：%v", err)
			}
		case topic := <-seen:
			if topic != "__ready__" {
				continue
			}
			for {
				select {
				case <-seen:
				case <-time.After(200 * time.Millisecond):
					return
				}
			}
		case <-deadline:
			t.Fatal("订阅未能在 5 秒内建立")
		}
	}
}

func waitTopic(seen <-chan string, want string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case topic := <-seen:
			if topic == want {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
