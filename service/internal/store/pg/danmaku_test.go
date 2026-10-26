package pg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"uuid"

	"xsite/service/internal/domain/moderation"
)

// 弹幕仓储（S2-5）集成测试：注册 / 游客作者、按窗口拉取（仅 visible、时间升序、窗口边界）。
func TestDanmakuRepo(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewDanmakuRepo(pool)

	nonce := time.Now().UnixNano()
	var uid, dramaID, epID int64
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (username, password_hash, nickname) VALUES ($1,$2,$3) RETURNING id`,
		fmt.Sprintf("td%d", nonce), "x", "弹幕君").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO drama (title) VALUES ('d') RETURNING id`).Scan(&dramaID); err != nil {
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

	// 注册用户弹幕（窗口内可见）+ 游客弹幕（窗口内，时间更早）+ pending（不应出现）+ 窗口外
	if _, err := repo.Create(ctx, moderation.NewDanmaku{EpisodeID: epID, Author: moderation.Author{UserID: uid}, TimeMS: 2000, Content: "起飞", Color: "FF0000", Status: "visible"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, moderation.NewDanmaku{EpisodeID: epID, Author: moderation.Author{GuestID: gid}, TimeMS: 1000, Content: "路过", Color: "FFFFFF", Status: "visible"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, moderation.NewDanmaku{EpisodeID: epID, Author: moderation.Author{UserID: uid}, TimeMS: 3000, Content: "待审", Color: "FFFFFF", Status: "pending"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Create(ctx, moderation.NewDanmaku{EpisodeID: epID, Author: moderation.Author{UserID: uid}, TimeMS: 90000, Content: "窗口外", Color: "FFFFFF", Status: "visible"}); err != nil {
		t.Fatal(err)
	}

	// [0, 60000) 窗口：只含两条 visible，按时间升序（游客 1000ms 在前）
	win, err := repo.ListWindow(ctx, epID, 0, 60000, 300)
	if err != nil || len(win) != 2 {
		t.Fatalf("窗口应 2 条可见：%+v %v", win, err)
	}
	if win[0].TimeMS != 1000 || win[0].Content != "路过" {
		t.Fatalf("应按时间升序，游客在前：%+v", win[0])
	}
	if win[1].TimeMS != 2000 || win[1].Color != "FF0000" {
		t.Fatalf("第二条应是起飞/红色：%+v", win[1])
	}

	// 右边界开区间：to_ms=2000 不含 2000ms 那条
	win2, _ := repo.ListWindow(ctx, epID, 0, 2000, 300)
	if len(win2) != 1 || win2[0].TimeMS != 1000 {
		t.Fatalf("右开区间应只含 1000ms：%+v", win2)
	}
}
