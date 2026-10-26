package pg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"xsite/service/internal/domain/moderation"
)

func TestCommentPolicyRepo(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewCommentPolicyRepo(pool)
	// 共享开发库：结束时还原
	orig, _ := repo.GetCommentPolicy(ctx)
	t.Cleanup(func() { _, _ = repo.SetCommentPolicy(context.Background(), orig) })

	if _, err := repo.SetCommentPolicy(ctx, moderation.CommentPolicy{PreModeration: true}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetCommentPolicy(ctx)
	if err != nil || !got.PreModeration {
		t.Fatalf("应读到 pre_moderation=true：%+v %v", got, err)
	}
	if _, err := repo.SetCommentPolicy(ctx, moderation.CommentPolicy{PreModeration: false}); err != nil {
		t.Fatal(err)
	}
	if g, _ := repo.GetCommentPolicy(ctx); g.PreModeration {
		t.Fatal("应改回 false")
	}
}

func TestModerationRepo(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewModerationRepo(pool)

	nonce := time.Now().UnixNano()
	var uid, dramaID, epID int64
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (username, password_hash, nickname) VALUES ($1,$2,$3) RETURNING id`,
		fmt.Sprintf("tm%d", nonce), "x", "审核用户").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO drama (title) VALUES ('m') RETURNING id`).Scan(&dramaID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO episode (drama_id, number) VALUES ($1, 1) RETURNING id`, dramaID).Scan(&epID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM drama WHERE id=$1`, dramaID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, uid)
	})

	// 两条 pending 评论 + 一条 visible 评论；给第一条 pending 加一条举报
	var c1, c2, c3 int64
	for _, x := range []struct {
		content, status string
		dst             *int64
	}{{"待审一", "pending", &c1}, {"待审二", "pending", &c2}, {"已显示", "visible", &c3}} {
		if err := pool.QueryRow(ctx, `INSERT INTO comment (episode_id, user_id, content, status) VALUES ($1,$2,$3,$4) RETURNING id`,
			epID, uid, x.content, x.status).Scan(x.dst); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO report (target_type, target_id, reporter_id, reason) VALUES ('comment',$1,$2,'ad')`, c1, uid); err != nil {
		t.Fatal(err)
	}

	// pending 列表：2 条
	pending, err := repo.ListComments(ctx, moderation.ModerationQuery{Status: "pending", Limit: 20, Offset: 0})
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending 评论应 2 条：%+v %v", pending, err)
	}
	if pending[0].Author.Nickname != "审核用户" || pending[0].Kind != moderation.KindComment {
		t.Fatalf("作者 / kind 展示：%+v", pending[0])
	}
	// 被举报筛选：只剩 c1
	reported, err := repo.ListComments(ctx, moderation.ModerationQuery{Reported: true, Limit: 20, Offset: 0})
	if err != nil || len(reported) != 1 || reported[0].ID != c1 || reported[0].ReportCount != 1 {
		t.Fatalf("被举报筛选应只剩 c1：%+v %v", reported, err)
	}
	// Q 搜内容
	byContent, _ := repo.ListComments(ctx, moderation.ModerationQuery{Pattern: "%待审二%", Limit: 20, Offset: 0})
	if len(byContent) != 1 || byContent[0].ID != c2 {
		t.Fatalf("按内容搜应命中 c2：%+v", byContent)
	}
	if n, _ := repo.CountComments(ctx, moderation.ModerationQuery{Status: "pending"}); n != 2 {
		t.Fatalf("pending 计数应 2，得到 %d", n)
	}

	// 通过 c1、c2
	if n, err := repo.SetCommentsStatus(ctx, []int64{c1, c2}, "visible"); err != nil || n != 2 {
		t.Fatalf("通过 2 条：n=%d err=%v", n, err)
	}
	if n, _ := repo.CountComments(ctx, moderation.ModerationQuery{Status: "pending"}); n != 0 {
		t.Fatalf("通过后 pending 应 0，得到 %d", n)
	}
	// 软删 c3
	if n, err := repo.SoftDeleteComments(ctx, []int64{c3}); err != nil || n != 1 {
		t.Fatalf("软删 1 条：n=%d err=%v", n, err)
	}
	var left int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE episode_id=$1 AND deleted_at IS NULL`, epID).Scan(&left)
	if left != 2 {
		t.Fatalf("软删后未删的应 2 条，得到 %d", left)
	}

	// 弹幕：插两条，硬删一条
	var d1, d2 int64
	_ = pool.QueryRow(ctx, `INSERT INTO danmaku (episode_id, user_id, time_ms, content, status) VALUES ($1,$2,100,'弹幕一','visible') RETURNING id`, epID, uid).Scan(&d1)
	_ = pool.QueryRow(ctx, `INSERT INTO danmaku (episode_id, user_id, time_ms, content, status) VALUES ($1,$2,200,'弹幕二','pending') RETURNING id`, epID, uid).Scan(&d2)
	dlist, _ := repo.ListDanmaku(ctx, moderation.ModerationQuery{Limit: 20, Offset: 0})
	if len(dlist) < 2 {
		t.Fatalf("弹幕列表应含 2 条：%+v", dlist)
	}
	if n, err := repo.DeleteDanmaku(ctx, []int64{d1}); err != nil || n != 1 {
		t.Fatalf("硬删弹幕 1 条：n=%d err=%v", n, err)
	}
	var dleft int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM danmaku WHERE id=$1`, d1).Scan(&dleft)
	if dleft != 0 {
		t.Fatal("弹幕应被硬删")
	}
}
