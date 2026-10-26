package pg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"uuid"

	"xsite/service/internal/domain/moderation"
)

// 评论仓储（S2-5）集成测试：注册 / 游客作者、游标列表（latest/hot）、作者展示、软删剔除、点赞幂等。
func TestCommentRepo(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewCommentRepo(pool)

	nonce := time.Now().UnixNano()
	var uid, dramaID, epID int64
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (username, password_hash, nickname) VALUES ($1,$2,$3) RETURNING id`,
		fmt.Sprintf("tc%d", nonce), "x", "阿明").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO drama (title) VALUES ('c') RETURNING id`).Scan(&dramaID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO episode (drama_id, number) VALUES ($1, 1) RETURNING id`, dramaID).Scan(&epID); err != nil {
		t.Fatal(err)
	}
	gid := uuid.NewV7()
	if _, err := pool.Exec(ctx, `INSERT INTO guest_device (id) VALUES ($1)`, gid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, uid)
		_, _ = pool.Exec(context.Background(), `DELETE FROM drama WHERE id=$1`, dramaID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM guest_device WHERE id=$1`, gid)
	})

	// 注册用户评论 + 游客评论
	uc, err := repo.Create(ctx, moderation.NewComment{EpisodeID: epID, Author: moderation.Author{UserID: uid}, Content: "好剧", Status: "visible"})
	if err != nil || uc.ID == 0 {
		t.Fatalf("注册用户评论：%+v %v", uc, err)
	}
	gc, err := repo.Create(ctx, moderation.NewComment{EpisodeID: epID, Author: moderation.Author{GuestID: gid}, Content: "路过", Status: "visible"})
	if err != nil {
		t.Fatal(err)
	}
	// 点赞让 uc 热度更高
	if added, err := repo.Like(ctx, uc.ID, uid); err != nil || !added {
		t.Fatalf("点赞应新增：%v %v", added, err)
	}
	if added, _ := repo.Like(ctx, uc.ID, uid); added {
		t.Fatal("重复点赞不应再加")
	}

	// 最新列表：游客评论在前（id 更大）
	latest, _, err := repo.List(ctx, epID, false, "", 10)
	if err != nil || len(latest) != 2 {
		t.Fatalf("最新列表应 2 条：%+v %v", latest, err)
	}
	if latest[0].ID != gc.ID || !latest[0].Author.IsGuest || latest[0].Author.Nickname != "游客" {
		t.Fatalf("游客评论展示：%+v", latest[0])
	}
	if latest[1].Author.IsGuest || latest[1].Author.Nickname != "阿明" {
		t.Fatalf("注册用户展示昵称：%+v", latest[1])
	}

	// 最热列表：uc（1 赞）在前
	hot, _, err := repo.List(ctx, epID, true, "", 10)
	if err != nil || len(hot) != 2 || hot[0].ID != uc.ID || hot[0].LikeCount != 1 {
		t.Fatalf("最热列表 uc 在前：%+v %v", hot, err)
	}

	// 游标：latest 取第 1 条后，下一页应是第 2 条
	page1, next, err := repo.List(ctx, epID, false, "", 1)
	if err != nil || len(page1) != 1 || next == "" {
		t.Fatalf("第一页 1 条 + 游标：%+v next=%q %v", page1, next, err)
	}
	page2, _, _ := repo.List(ctx, epID, false, next, 1)
	if len(page2) != 1 || page2[0].ID == page1[0].ID {
		t.Fatalf("第二页应是不同的一条：%+v", page2)
	}

	// 软删后从列表剔除
	if err := repo.SoftDelete(ctx, gc.ID); err != nil {
		t.Fatal(err)
	}
	after, _, _ := repo.List(ctx, epID, false, "", 10)
	if len(after) != 1 || after[0].ID != uc.ID {
		t.Fatalf("软删后应只剩 uc：%+v", after)
	}

	// 删除鉴权信息 + 通知收件人解析（AuthorOf 返回作者、所属分集、是否已删）
	owner, epOf, deleted, err := repo.AuthorOf(ctx, gc.ID)
	if err != nil || !deleted || owner.GuestID != gid || epOf != epID {
		t.Fatalf("AuthorOf：owner=%+v episode=%d deleted=%v err=%v", owner, epOf, deleted, err)
	}
}
