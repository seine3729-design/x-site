package pg

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"xsite/service/internal/domain/user"
	"xsite/service/internal/errs"
)

// 后台用户管理仓储（S2-7）集成测试：列表筛选与搜索（含 LIKE 转义）、详情计数、改状态、游客列表。
// 同时验证 UserMuted 把 banned 一并算进「不能发表」—— 这是封禁即时生效的另一半（docs/11 S2-7）。
func TestUserAdminRepo(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewUserAdminRepo(pool)

	// 用独特前缀隔离本测试的数据。前缀里不放 _：它在 LIKE 里是通配符，会连带匹配别的测试的数据
	const pre = "tuaone"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE username LIKE $1`, pre+"%")
	})

	mk := func(name, nickname, status string) int64 {
		var id int64
		err := pool.QueryRow(ctx,
			`INSERT INTO "user" (username, nickname, password_hash, status) VALUES ($1, $2, 'x', $3) RETURNING id`,
			pre+name, nickname, status).Scan(&id)
		if err != nil {
			t.Fatalf("建用户 %s：%v", name, err)
		}
		return id
	}
	alice := mk("alice", "爱丽丝Ａ", "normal")
	mk("bob", "Bob_100%", "muted")
	carol := mk("carol", "卡罗尔", "banned")
	// 回收站里的账号不该出现在后台列表里
	deleted := mk("dave", "戴夫", "normal")
	if _, err := pool.Exec(ctx, `UPDATE "user" SET deleted_at = now() WHERE id = $1`, deleted); err != nil {
		t.Fatalf("软删：%v", err)
	}

	count := func(q user.AdminQuery) ([]user.AdminUser, int64) {
		q.Limit = 50
		list, total, err := repo.ListUsers(ctx, q)
		if err != nil {
			t.Fatalf("列表 %+v：%v", q, err)
		}
		return list, total
	}

	// 前缀搜索能拿到三条，软删的那条不在内
	list, total := count(user.AdminQuery{Pattern: "%" + pre + "%"})
	if total != 3 || len(list) != 3 {
		t.Fatalf("应有 3 条（软删的不算），得到 total=%d len=%d", total, len(list))
	}
	// 按注册时间倒序：最后建的（carol）在前
	if list[0].ID != carol {
		t.Fatalf("应按注册时间倒序，首条 id=%d 期望 %d", list[0].ID, carol)
	}

	// 状态筛选
	if _, total := count(user.AdminQuery{Pattern: "%" + pre + "%", Status: "banned"}); total != 1 {
		t.Fatalf("banned 应 1 条，得到 %d", total)
	}
	if _, total := count(user.AdminQuery{Pattern: "%" + pre + "%", Status: "muted"}); total != 1 {
		t.Fatalf("muted 应 1 条，得到 %d", total)
	}

	// 昵称搜索走 lower(nickname)：全角与大小写都要能命中（领域层已把词转小写、全角转半角）
	if _, total := count(user.AdminQuery{Pattern: "%bob%"}); total != 1 {
		t.Fatalf("按昵称 bob 搜应 1 条，得到 %d", total)
	}
	// LIKE 元字符必须按字面匹配：bob 的昵称里真有一个 %
	if _, total := count(user.AdminQuery{Pattern: `%100\%%`}); total != 1 {
		t.Fatalf("转义后的 %% 应按字面匹配，得到 %d", total)
	}
	// 不转义的 _ 会变成「任意一个字符」，转义后只能匹配真的下划线
	if _, total := count(user.AdminQuery{Pattern: `%bob\_100%`}); total != 1 {
		t.Fatalf("转义后的 _ 应按字面匹配，得到 %d", total)
	}

	// 分页：第 2 页每页 2 条只剩 1 条，total 仍是全部
	page2, total2, err := repo.ListUsers(ctx, user.AdminQuery{Pattern: "%" + pre + "%", Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("第 2 页：%v", err)
	}
	if len(page2) != 1 || total2 != 3 {
		t.Fatalf("第 2 页应 1 条、总数 3，得到 len=%d total=%d", len(page2), total2)
	}

	// ---------- 详情 ----------
	d, err := repo.UserDetail(ctx, alice)
	if err != nil {
		t.Fatalf("详情：%v", err)
	}
	if d.Username != pre+"alice" || d.NicknameStatus != "ok" || d.HistoryPaused {
		t.Fatalf("详情字段不对：%+v", d)
	}
	// 新账号的足迹全是 0，各子查询都要能跑通（真实 SQL 才查得出类型 / 列名错误）
	if d.Stats != (user.AdminUserStats{}) {
		t.Fatalf("新账号足迹应全为 0，得到 %+v", d.Stats)
	}
	if _, err := repo.UserDetail(ctx, deleted); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("回收站里的账号应 404，得到 %v", err)
	}
	if _, err := repo.UserDetail(ctx, -1); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不存在的用户应 404，得到 %v", err)
	}

	// ---------- 改状态 ----------
	u, err := repo.SetUserStatus(ctx, alice, user.StatusBanned)
	if err != nil {
		t.Fatalf("改状态：%v", err)
	}
	if u.Status != user.StatusBanned {
		t.Fatalf("返回的状态应为 banned，得到 %q", u.Status)
	}
	if _, err := repo.SetUserStatus(ctx, deleted, user.StatusBanned); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("改回收站里的账号应 404，得到 %v", err)
	}

	// ---------- 封禁的另一半：不能再发评论 / 弹幕 ----------
	// 原来的 UserMuted 只看 status='muted'，于是被封禁的账号靠手上没过期的 Access Token 还能继续发言。
	base := NewUserRepo(pool)
	for _, c := range []struct {
		id   int64
		want bool
		name string
	}{
		{alice, true, "banned"},
		{carol, true, "banned"},
	} {
		got, err := base.Muted(ctx, c.id)
		if err != nil {
			t.Fatalf("查 %s 的发表权限：%v", c.name, err)
		}
		if got != c.want {
			t.Fatalf("%s 账号应禁止发表（%v），得到 %v", c.name, c.want, got)
		}
	}
	// 恢复正常后可以发
	if _, err := repo.SetUserStatus(ctx, alice, user.StatusNormal); err != nil {
		t.Fatalf("恢复正常：%v", err)
	}
	if muted, err := base.Muted(ctx, alice); err != nil || muted {
		t.Fatalf("正常账号应可发表，得到 muted=%v err=%v", muted, err)
	}
}

// 游客设备列表：按首次出现倒序、可搜设备编号与已并入的账号用户名。
func TestUserAdminRepoGuests(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewUserAdminRepo(pool)

	const pre = "tuatwo"
	var gid, gid2 string
	var uid int64
	// 先注册清理再建数据：中途失败也不留垃圾给别的测试
	t.Cleanup(func() {
		c := context.Background()
		_, _ = pool.Exec(c, `DELETE FROM guest_device WHERE source_channel LIKE $1`, pre+"%")
		_, _ = pool.Exec(c, `DELETE FROM "user" WHERE username LIKE $1`, pre+"%")
	})
	if err := pool.QueryRow(ctx,
		`INSERT INTO "user" (username, nickname, password_hash) VALUES ($1, '并入账号', 'x') RETURNING id`,
		pre+"owner").Scan(&uid); err != nil {
		t.Fatalf("建用户：%v", err)
	}
	for i, merged := range []any{nil, uid} {
		var id string
		// guest_device.id 由应用生成（uuid v7），没有列默认值，测试里用内置函数顶上
		err := pool.QueryRow(ctx,
			`INSERT INTO guest_device (id, platform, source_channel, merged_user_id)
			 VALUES (gen_random_uuid(), $1, $2, $3) RETURNING id::text`,
			"web", fmt.Sprintf("%schan%d", pre, i), merged).Scan(&id)
		if err != nil {
			t.Fatalf("建游客设备：%v", err)
		}
		if i == 0 {
			gid = id
		} else {
			gid2 = id
		}
	}
	_ = gid2

	// 按设备编号精确搜到一条
	list, total, err := repo.ListGuests(ctx, user.AdminQuery{Pattern: "%" + gid + "%", Limit: 50})
	if err != nil {
		t.Fatalf("游客列表：%v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != gid {
		t.Fatalf("按设备编号应搜到 1 条，得到 total=%d list=%+v", total, list)
	}
	if list[0].MergedUserID != nil {
		t.Fatalf("未并入的设备 merged_user_id 应为空，得到 %v", *list[0].MergedUserID)
	}

	// 按已并入的账号用户名搜
	list, total, err = repo.ListGuests(ctx, user.AdminQuery{Pattern: "%" + pre + "owner%", Limit: 50})
	if err != nil {
		t.Fatalf("按用户名搜游客：%v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("按并入账号应搜到 1 条，得到 total=%d", total)
	}
	if list[0].MergedUserID == nil || *list[0].MergedUserID != uid || list[0].MergedUsername != pre+"owner" {
		t.Fatalf("并入信息不对：%+v", list[0])
	}
	if list[0].ID == "" || list[0].FirstSeen.IsZero() {
		t.Fatalf("设备编号与首次出现应有值：%+v", list[0])
	}
}
