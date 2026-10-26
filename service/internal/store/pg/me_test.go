package pg

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"xsite/service/internal/domain/me"
	"xsite/service/internal/errs"
)

// 收藏 / 点赞仓储（S2-3）PostgreSQL 集成测试：幂等（ON CONFLICT）、外键、多态点赞、execrows。
func TestMeRepo_FollowLike(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewMeRepo(pool)

	nonce := time.Now().UnixNano()
	var uid, dramaID int64
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (username, password_hash) VALUES ($1,$2) RETURNING id`,
		fmt.Sprintf("tm%d", nonce), "x").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO drama (title) VALUES ($1) RETURNING id`, "me-test").Scan(&dramaID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, uid)
		_, _ = pool.Exec(context.Background(), `DELETE FROM drama WHERE id=$1`, dramaID)
	})

	count := func(q string) int {
		var n int
		if err := pool.QueryRow(ctx, q, uid).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 收藏幂等
	if err := repo.AddFollow(ctx, uid, dramaID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddFollow(ctx, uid, dramaID); err != nil {
		t.Fatalf("重复收藏应幂等：%v", err)
	}
	if n := count(`SELECT count(*) FROM follow WHERE user_id=$1`); n != 1 {
		t.Fatalf("收藏应只有 1 行，得到 %d", n)
	}
	// 取消收藏幂等（未收藏删 0 行也不报错）
	if err := repo.RemoveFollow(ctx, uid, dramaID); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveFollow(ctx, uid, dramaID); err != nil {
		t.Fatalf("重复取消应幂等：%v", err)
	}
	// 收藏不存在的剧 → 外键失败映射 400
	if err := repo.AddFollow(ctx, uid, 999999999); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("收藏不存在的剧应 400，得到 %v", err)
	}

	// 点赞剧 / 分集（episode 的 target_id 多态、无外键）
	if err := repo.AddLike(ctx, uid, "drama", dramaID); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddLike(ctx, uid, "drama", dramaID); err != nil {
		t.Fatalf("重复点赞应幂等：%v", err)
	}
	if err := repo.AddLike(ctx, uid, "episode", 42); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM "like" WHERE user_id=$1`); n != 2 {
		t.Fatalf("点赞应有 2 行，得到 %d", n)
	}
	if err := repo.RemoveLike(ctx, uid, "drama", dramaID); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM "like" WHERE user_id=$1`); n != 1 {
		t.Fatalf("取消一个点赞后应剩 1 行，得到 %d", n)
	}
}

// 观看历史仓储（S2-3）集成测试：合并写（老上报不覆盖、新上报覆盖）、JOIN 跳过伪造剧、
// 列表卡片字段与软删剔除、删除 / 清空、暂停开关。
func TestMeRepo_History(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewMeRepo(pool)

	nonce := time.Now().UnixNano()
	var uid, dramaA, dramaB int64
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (username, password_hash) VALUES ($1,$2) RETURNING id`,
		fmt.Sprintf("th%d", nonce), "x").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO drama (title, cover_portrait, latest_episode) VALUES ($1,$2,$3) RETURNING id`,
		"hist-A", "coverA.jpg", 12).Scan(&dramaA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO drama (title) VALUES ($1) RETURNING id`, "hist-B").Scan(&dramaB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, uid)
		_, _ = pool.Exec(context.Background(), `DELETE FROM drama WHERE id IN ($1,$2)`, dramaA, dramaB)
	})

	// 暂停开关默认关
	if paused, err := repo.HistoryPaused(ctx, uid); err != nil || paused {
		t.Fatalf("默认不应暂停：paused=%v err=%v", paused, err)
	}

	base := time.Unix(1_700_000_000, 0).UTC()
	// 首次落库（含一条伪造剧，JOIN drama 应跳过而非整批外键失败）
	if err := repo.UpsertHistory(ctx, []me.HistoryRow{
		{UserID: uid, DramaID: dramaA, EpisodeID: 101, EpisodeNumber: 3, PositionSec: 30, DurationSec: 90, UpdatedAt: base},
		{UserID: uid, DramaID: dramaB, EpisodeID: 201, EpisodeNumber: 1, PositionSec: 5, DurationSec: 80, UpdatedAt: base},
		{UserID: uid, DramaID: 999999999, EpisodeID: 1, EpisodeNumber: 1, PositionSec: 1, DurationSec: 1, UpdatedAt: base},
	}); err != nil {
		t.Fatal(err)
	}

	pos := func(drama int64) int {
		var p int
		if err := pool.QueryRow(ctx, `SELECT position_sec FROM watch_history WHERE user_id=$1 AND drama_id=$2`, uid, drama).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := pos(dramaA); p != 30 {
		t.Fatalf("A 首次应为 30，得到 %d", p)
	}
	var bogus int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM watch_history WHERE user_id=$1 AND drama_id=999999999`, uid).Scan(&bogus); err != nil {
		t.Fatal(err)
	}
	if bogus != 0 {
		t.Fatal("伪造 drama_id 不应落库")
	}

	// 老上报不覆盖（updated_at 更早）
	if err := repo.UpsertHistory(ctx, []me.HistoryRow{
		{UserID: uid, DramaID: dramaA, EpisodeID: 100, EpisodeNumber: 2, PositionSec: 10, DurationSec: 90, UpdatedAt: base.Add(-time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	if p := pos(dramaA); p != 30 {
		t.Fatalf("老上报不应覆盖，应仍为 30，得到 %d", p)
	}
	// 新上报覆盖
	if err := repo.UpsertHistory(ctx, []me.HistoryRow{
		{UserID: uid, DramaID: dramaA, EpisodeID: 102, EpisodeNumber: 4, PositionSec: 50, DurationSec: 90, UpdatedAt: base.Add(time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	if p := pos(dramaA); p != 50 {
		t.Fatalf("新上报应覆盖为 50，得到 %d", p)
	}

	// 列表：最近在前（A 的 updated_at 更新），含卡片字段
	views, err := repo.ListHistory(ctx, uid, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Fatalf("应有 2 条历史，得到 %d", len(views))
	}
	if views[0].DramaID != dramaA {
		t.Fatalf("A 应排最前，得到 %d", views[0].DramaID)
	}
	if views[0].Title != "hist-A" || views[0].Cover != "coverA.jpg" || views[0].LatestEpisode != 12 {
		t.Fatalf("A 卡片字段不对：%+v", views[0])
	}

	// 软删 B 的剧 → 列表不再展示（但历史行仍在）
	if _, err := pool.Exec(ctx, `UPDATE drama SET deleted_at=now() WHERE id=$1`, dramaB); err != nil {
		t.Fatal(err)
	}
	views, err = repo.ListHistory(ctx, uid, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].DramaID != dramaA {
		t.Fatalf("软删剧应从列表剔除，得到 %+v", views)
	}

	// 删除指定剧
	if err := repo.DeleteHistory(ctx, uid, []int64{dramaA}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM watch_history WHERE user_id=$1`, uid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("删 A 后应剩 1 行（B 的历史仍在），得到 %d", n)
	}
	// 清空
	if err := repo.DeleteAllHistory(ctx, uid); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM watch_history WHERE user_id=$1`, uid).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("清空后应 0 行，得到 %d", n)
	}

	// 暂停开关
	if _, err := pool.Exec(ctx, `UPDATE "user" SET history_paused=true WHERE id=$1`, uid); err != nil {
		t.Fatal(err)
	}
	if paused, err := repo.HistoryPaused(ctx, uid); err != nil || !paused {
		t.Fatalf("应为暂停：paused=%v err=%v", paused, err)
	}
}

// 站内信仓储（S2-3）集成测试：列表倒序游标、未读计数、全部已读。
func TestMeRepo_Notifications(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewMeRepo(pool)

	nonce := time.Now().UnixNano()
	var uid int64
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (username, password_hash) VALUES ($1,$2) RETURNING id`,
		fmt.Sprintf("tn%d", nonce), "x").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, uid) })

	// 插 3 条通知（走 InsertNotification 写入路径，S2-5 的 reply/like 来源即用它；回查 id 供游标断言）
	var ids []int64
	for _, ty := range []string{"reply", "like", "update"} {
		if err := repo.InsertNotification(ctx, uid, ty, []byte(`{"k":1}`)); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := pool.QueryRow(ctx, `SELECT id FROM notification WHERE user_id=$1 ORDER BY id DESC LIMIT 1`, uid).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	// 未读计数 = 3
	if n, err := repo.CountUnreadNotifications(ctx, uid); err != nil || n != 3 {
		t.Fatalf("未读应为 3，得到 %d %v", n, err)
	}

	// 列表倒序，before=0 从最新开始，limit 生效
	list, err := repo.ListNotifications(ctx, uid, 0, 2)
	if err != nil || len(list) != 2 || list[0].ID != ids[2] || list[1].ID != ids[1] {
		t.Fatalf("倒序前两条应为最新两条：%+v %v", list, err)
	}
	if list[0].Type != "update" || len(list[0].Payload) == 0 || list[0].IsRead {
		t.Fatalf("字段映射不对（type/payload/is_read）：%+v", list[0])
	}
	// 游标 before=第二条 → 只剩最旧一条
	list, err = repo.ListNotifications(ctx, uid, ids[1], 10)
	if err != nil || len(list) != 1 || list[0].ID != ids[0] {
		t.Fatalf("游标 before 应只返回更旧的：%+v %v", list, err)
	}

	// 全部已读 → 标记 3 条，未读归零
	if n, err := repo.MarkAllNotificationsRead(ctx, uid); err != nil || n != 3 {
		t.Fatalf("应标记 3 条已读，得到 %d %v", n, err)
	}
	if n, err := repo.CountUnreadNotifications(ctx, uid); err != nil || n != 0 {
		t.Fatalf("全部已读后未读应为 0，得到 %d %v", n, err)
	}
	// 再次全部已读 → 0 条（幂等）
	if n, _ := repo.MarkAllNotificationsRead(ctx, uid); n != 0 {
		t.Fatalf("已无未读，应标记 0 条，得到 %d", n)
	}
}
