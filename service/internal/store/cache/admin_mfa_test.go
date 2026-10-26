package cache

import (
	"context"
	"testing"
	"time"
)

func TestAdminMFAStore_LoginTicket(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	s := NewAdminMFAStore(client, prefix)

	if _, found, err := s.LookupLoginTicket(ctx, "nope"); err != nil || found {
		t.Fatalf("不存在的票据应 (0,false,nil)：found=%v err=%v", found, err)
	}
	if err := s.CreateLoginTicket(ctx, "tkt", 7, time.Minute); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DeleteLoginTicket(context.Background(), "tkt") })

	if id, found, err := s.LookupLoginTicket(ctx, "tkt"); err != nil || !found || id != 7 {
		t.Fatalf("应换回管理员 ID：id=%d found=%v err=%v", id, found, err)
	}
	if err := s.DeleteLoginTicket(ctx, "tkt"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := s.LookupLoginTicket(ctx, "tkt"); found {
		t.Fatal("删除后票据应消失（一次性）")
	}
}

func TestAdminMFAStore_ReauthMarker(t *testing.T) {
	client, prefix := testClient(t)
	ctx := context.Background()
	s := NewAdminMFAStore(client, prefix)

	key := "sess-abc"
	t.Cleanup(func() {
		_ = client.Do(context.Background(), client.B().Del().Key(s.reauthKey(key)).Build()).Error()
	})

	if ok, err := s.IsReauthed(ctx, key, "203.0.0.0/16"); err != nil || ok {
		t.Fatalf("未标记时应为 false：ok=%v err=%v", ok, err)
	}
	if err := s.MarkReauthed(ctx, key, "203.0.0.0/16", time.Minute); err != nil {
		t.Fatal(err)
	}
	// 同网段 → 命中
	if ok, err := s.IsReauthed(ctx, key, "203.0.0.0/16"); err != nil || !ok {
		t.Fatalf("同网段应命中：ok=%v err=%v", ok, err)
	}
	// 网段变化 → 视为需重新验证（docs/10 第 6.3 节）
	if ok, err := s.IsReauthed(ctx, key, "198.51.0.0/16"); err != nil || ok {
		t.Fatalf("网段变化应为 false：ok=%v err=%v", ok, err)
	}
}
