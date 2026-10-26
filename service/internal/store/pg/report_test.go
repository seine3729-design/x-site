package pg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"uuid"

	"xsite/service/internal/domain/moderation"
)

// 举报仓储（S2-5）集成测试：注册 / 游客举报人多态落库。
func TestReportRepo(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewReportRepo(pool)

	nonce := time.Now().UnixNano()
	var uid int64
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (username, password_hash, nickname) VALUES ($1,$2,$3) RETURNING id`,
		fmt.Sprintf("tr%d", nonce), "x", "举报人").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	gid := uuid.NewV7()
	if _, err := pool.Exec(ctx, `INSERT INTO guest_device (id) VALUES ($1)`, gid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM report WHERE reporter_id=$1 OR reporter_guest_id=$2`, uid, gid)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, uid)
		_, _ = pool.Exec(context.Background(), `DELETE FROM guest_device WHERE id=$1`, gid)
	})

	// 注册用户举报评论
	if err := repo.Create(ctx, moderation.NewReport{
		Reporter: moderation.Author{UserID: uid}, TargetType: "comment", TargetID: 101, Reason: "ad", Detail: "广告",
	}); err != nil {
		t.Fatal(err)
	}
	// 游客举报弹幕
	if err := repo.Create(ctx, moderation.NewReport{
		Reporter: moderation.Author{GuestID: gid}, TargetType: "danmaku", TargetID: 202, Reason: "attack",
	}); err != nil {
		t.Fatal(err)
	}

	var byUser, byGuest int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM report WHERE reporter_id=$1 AND target_type='comment' AND target_id=101`, uid).Scan(&byUser); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM report WHERE reporter_guest_id=$1 AND target_type='danmaku' AND target_id=202`, gid).Scan(&byGuest); err != nil {
		t.Fatal(err)
	}
	if byUser != 1 || byGuest != 1 {
		t.Fatalf("举报落库计数：byUser=%d byGuest=%d", byUser, byGuest)
	}

	// 列表：按对象类型筛选只剩注册用户那条；举报人展示注册用户昵称
	list, total, err := repo.List(ctx, moderation.ReportQuery{TargetType: "comment", Limit: 20, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	var mine *moderation.ReportView
	for i := range list {
		if list[i].TargetID == 101 && list[i].TargetType == "comment" {
			mine = &list[i]
		}
	}
	if mine == nil || total < 1 {
		t.Fatalf("应能按对象类型查到评论举报：total=%d list=%+v", total, list)
	}
	if mine.Reporter.IsGuest || mine.Reporter.Nickname != "举报人" || mine.Reason != "ad" {
		t.Fatalf("举报人应展示注册用户昵称：%+v", mine.Reporter)
	}

	// Q 搜说明命中（detail=广告）
	byDetail, _, err := repo.List(ctx, moderation.ReportQuery{Pattern: "%广告%", Limit: 20, Offset: 0})
	if err != nil || len(byDetail) == 0 {
		t.Fatalf("按说明搜索应命中：%+v %v", byDetail, err)
	}

	// 处理：置为已处理 + 说明
	handled, err := repo.Handle(ctx, mine.ID, moderation.ReportResolved, "已隐藏该评论")
	if err != nil || handled.Status != moderation.ReportResolved || handled.HandleNote != "已隐藏该评论" {
		t.Fatalf("处理举报：%+v %v", handled, err)
	}
	// 处理后按 pending 筛选不再包含它
	pending, _, _ := repo.List(ctx, moderation.ReportQuery{Status: moderation.ReportPending, TargetType: "comment", Limit: 20, Offset: 0})
	for _, v := range pending {
		if v.ID == mine.ID {
			t.Fatalf("已处理的举报不应再出现在 pending：%+v", v)
		}
	}
	// 处理不存在的举报 → NotFound
	if _, err := repo.Handle(ctx, 1<<40, moderation.ReportRejected, ""); err == nil {
		t.Fatal("处理不存在的举报应报错")
	}
}
