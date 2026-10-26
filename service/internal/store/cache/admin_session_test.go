package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/admin"
	"xsite/service/internal/errs"
)

// 集成测试：连接本地 compose 的 Valkey（./dev.sh go test 会注入 VALKEY_URL）；未配置时跳过。
func testClient(t *testing.T) (valkey.Client, string) {
	t.Helper()
	url := os.Getenv("VALKEY_URL")
	if url == "" {
		t.Skip("未设置 VALKEY_URL，跳过 Valkey 集成测试")
	}
	c, err := NewClient(url)
	if err != nil {
		t.Skipf("Valkey 不可用，跳过：%v", err)
	}
	t.Cleanup(c.Close)
	return c, fmt.Sprintf("xsite:test:%d:", time.Now().UnixNano())
}

func TestAdminSessionStore_Integration(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	store := NewAdminSessionStore(client, prefix)

	created := time.Unix(1_790_000_000, 0)
	// 会话版本必须原样保存：读出为 0 时，被强制下线过的管理员每次登录后都会立刻失效
	sess := admin.Session{AdminID: 7, SessionVersion: 3, CSRFToken: "csrf", UADigest: "ua", CreatedAt: created}
	if err := store.Create(ctx, "k1", sess, time.Minute); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Delete(context.Background(), "k1") })

	if err := store.Create(ctx, "k1", admin.Session{AdminID: 8}, time.Minute); err == nil {
		t.Fatal("同一个键不能覆盖已有会话")
	}

	got, err := store.GetAndTouch(ctx, "k1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got != sess {
		t.Fatalf("会话内容不一致：%+v", got)
	}
	ttl, err := client.Do(ctx, client.B().Pttl().Key(prefix+"k1").Build()).AsInt64()
	if err != nil || ttl < int64(59*time.Minute/time.Millisecond) {
		t.Fatalf("读取后有效期应被重置为 1 小时，剩余 %dms %v", ttl, err)
	}

	if err := store.Delete(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAndTouch(ctx, "k1", time.Hour); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删除后应返回 NOT_FOUND，得到 %v", err)
	}
}

func TestCounter_Integration(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	c := NewCounter(client, prefix)
	t.Cleanup(func() { _ = c.Reset(context.Background(), "u:alice") })

	if n, ttl, err := c.Get(ctx, "u:alice"); err != nil || n != 0 || ttl != 0 {
		t.Fatalf("不存在的计数应为 0：%d %v %v", n, ttl, err)
	}
	for want := int64(1); want <= 3; want++ {
		n, err := c.Incr(ctx, "u:alice", 15*time.Minute)
		if err != nil || n != want {
			t.Fatalf("第 %d 次加一得到 %d %v", want, n, err)
		}
	}
	n, ttl, err := c.Get(ctx, "u:alice")
	if err != nil || n != 3 || ttl <= 14*time.Minute || ttl > 15*time.Minute {
		t.Fatalf("计数应为 3、有效期约 15 分钟：%d %v %v", n, ttl, err)
	}
	if err := c.Reset(ctx, "u:alice"); err != nil {
		t.Fatal(err)
	}
	if n, _, _ := c.Get(ctx, "u:alice"); n != 0 {
		t.Fatalf("清除后应为 0，得到 %d", n)
	}
}
