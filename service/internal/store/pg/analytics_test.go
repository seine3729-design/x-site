package pg

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/domain/analytics"
)

// 埋点采集（S4-1）走真实 PostgreSQL：CopyFrom 批量写入 event_raw 分区表，
// 覆盖分区路由（按 occurred_at 落到当天分区）、可空外键（user/drama/episode）与 jsonb props。
func TestAnalyticsRepo_InsertEvents(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	dev := fmt.Sprintf("it-ev-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM event_raw WHERE device_id = $1`, dev)
	})

	repo := NewAnalyticsRepo(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	drama := int64(7)
	user := int64(42)
	n, err := repo.InsertEvents(ctx, []analytics.Event{
		{OccurredAt: now, Type: "play_start", DeviceID: dev, UserID: &user, DramaID: &drama, Client: "web", Region: "CN", Props: []byte(`{}`)},
		{OccurredAt: now, Type: "watch", DeviceID: dev, Client: "app", Props: []byte(`{"seconds":30}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("应写入 2 条，得 %d", n)
	}

	type row struct {
		drama, user, ep *int64
		client, props   string
	}
	got := map[string]row{}
	rows, err := pool.Query(ctx,
		`SELECT event_type, drama_id, user_id, episode_id, client, props::text FROM event_raw WHERE device_id = $1`, dev)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var et string
		var r row
		if err := rows.Scan(&et, &r.drama, &r.user, &r.ep, &r.client, &r.props); err != nil {
			t.Fatal(err)
		}
		got[et] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("读回 %d 条，期望 2", len(got))
	}

	ps := got["play_start"]
	if ps.drama == nil || *ps.drama != 7 || ps.user == nil || *ps.user != 42 || ps.ep != nil || ps.client != "web" {
		t.Errorf("play_start 字段不符：%+v", ps)
	}
	w := got["watch"]
	if w.drama != nil || w.user != nil || w.client != "app" || !strings.Contains(w.props, "seconds") {
		t.Errorf("watch 字段不符：%+v", w)
	}
}
