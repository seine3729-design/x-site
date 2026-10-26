package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/admin"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
)

// testPool 连接本地 compose 的 PostgreSQL 并执行迁移；未配置或不可用时跳过。
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("未设置 DATABASE_URL，跳过数据库集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := NewPool(ctx, url)
	if err != nil {
		t.Skipf("数据库不可用，跳过：%v", err)
	}
	t.Cleanup(pool.Close)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	return pool
}

func roleID(t *testing.T, repo *AdminRepo, code string) int64 {
	t.Helper()
	id, err := repo.GetRoleIDByCode(context.Background(), code)
	if err != nil {
		t.Fatalf("内置角色 %s 应存在：%v", code, err)
	}
	return id
}

func TestAdminRepo_SeedMatchesDefaultMatrix(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAdminRepo(pool)

	roles, err := repo.ListRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byCode := map[string]admin.RoleInfo{}
	for _, r := range roles {
		byCode[r.Code] = r
	}
	if !byCode["superadmin"].IsSuper || !byCode["superadmin"].Builtin || len(byCode["superadmin"].Permissions) != 0 {
		t.Fatalf("超级角色不存权限行，由 is_super 表示全部：%+v", byCode["superadmin"])
	}
	// 迁移 00005 起只内置超级管理员，其余角色由管理员新建
	var builtin []string
	for _, r := range roles {
		if r.Builtin {
			builtin = append(builtin, r.Code)
		}
	}
	if !slices.Equal(builtin, []string{"superadmin"}) {
		t.Fatalf("内置角色只有超级管理员，得到 %v", builtin)
	}

	perms, err := repo.ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sensitive := map[string]bool{}
	for _, p := range perms {
		sensitive[p.Code] = p.Sensitive
	}
	if _, ok := sensitive["audit.edit"]; ok {
		t.Fatal("审计日志是只读页面，不应有 .edit 权限点")
	}
	if !sensitive["storage_lines.edit"] || !sensitive["admins.edit"] || sensitive["dramas.edit"] {
		t.Fatal("改线路、改管理员为高敏权限点，改剧集不是")
	}

	menus, err := repo.ListMenus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]admin.Menu{}
	byMenuCode := map[string]admin.Menu{}
	for _, m := range menus {
		byID[m.ID] = m
		byMenuCode[m.Code] = m
	}
	pages := 0
	for _, m := range menus {
		if m.Kind == admin.MenuPage && m.Builtin {
			pages++
			if m.Path == "" {
				t.Fatalf("内置页面需有路径：%+v", m)
			}
		}
		if m.ParentID != nil {
			if parent := byID[*m.ParentID]; parent.Kind != admin.MenuGroup || parent.ParentID != nil {
				t.Fatalf("二级菜单的上级必须是一级目录：%+v → %+v", m, parent)
			}
		}
	}
	// 00003 的 42 个页面，减去演员（00010）、资讯（00014），
	// 加上反馈社区的 5 个（00012）与议题区、开放接口（00016），再加表情管理（00039）
	if pages != 48 {
		t.Fatalf("默认应有 48 个页面（与 admin/src/config/navigation.ts 一致），得到 %d", pages)
	}
	// 迁移 00010 删除演员、00014 删除资讯：页面及其权限点（角色授权随之级联删除）
	for _, page := range []string{"actors", "articles"} {
		if m, ok := byMenuCode[page]; ok {
			t.Fatalf("页面 %s 应已删除：%+v", page, m)
		}
		for _, code := range []string{page + ".view", page + ".edit"} {
			if _, ok := sensitive[code]; ok {
				t.Fatalf("权限点 %s 应随页面删除", code)
			}
		}
	}
	for _, code := range []string{"dashboard", "analytics_dramas"} {
		if m := byMenuCode[code]; m.ParentID != nil {
			t.Fatalf("%s 默认是一级菜单：%+v", code, m)
		}
	}
	if _, ok := byMenuCode["overview"]; ok {
		t.Fatal("「概览」目录的两个页面提为一级菜单后应删除")
	}
}

func TestAdminRepo_Integration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAdminRepo(pool)
	suffix := time.Now().UnixNano()
	username := fmt.Sprintf("it-%d", suffix)
	otherCode := fmt.Sprintf("it2_%d", suffix%1_000_000_000) // 第二个自定义角色，测试换角色

	// 自定义角色：只读剧集
	rid, err := repo.CreateRole(ctx, admin.RoleInput{Code: fmt.Sprintf("it_%d", suffix%1_000_000_000), Name: "集成测试", Active: true, Sort: 99})
	if err != nil {
		t.Fatal(err)
	}
	id, err := repo.CreateAdmin(ctx, username, "hash-1", rid)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, "DELETE FROM audit_log WHERE admin_id = $1", id)
		_, _ = pool.Exec(bg, "DELETE FROM admin_login_log WHERE admin_id = $1 OR username_tried = $2", id, username)
		_, _ = pool.Exec(bg, "DELETE FROM admin_user WHERE id = $1", id)
		_, _ = pool.Exec(bg, "DELETE FROM admin_role WHERE id = $1 OR code = $2", rid, otherCode)
	})

	if _, err := repo.CreateAdmin(ctx, username, "hash-2", rid); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("重复用户名应返回 CONFLICT，得到 %v", err)
	}
	if err := repo.ReplaceRolePermissions(ctx, rid, []string{"dramas.view"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceRolePermissions(ctx, rid, []string{"dramas.edit", "dramas.view"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceRolePermissions(ctx, rid, []string{"nope.edit"}); err == nil {
		t.Fatal("不存在的权限点应被外键拒绝")
	}

	acct, err := repo.GetByUsername(ctx, username)
	if err != nil {
		t.Fatal(err)
	}
	if acct.ID != id || acct.Status != admin.StatusUnused || acct.Role.ID != rid || !acct.Role.Active || acct.Role.IsSuper ||
		!slices.Equal(acct.Permissions, []string{"dramas.edit", "dramas.view"}) || acct.SessionVersion != 0 {
		t.Fatalf("登录读取的账号不符合预期：%+v", acct)
	}

	if err := repo.MarkLogin(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := repo.ResetAdminPassword(ctx, id, "hash-new"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RevokeAdminSessions(ctx, id); err != nil {
		t.Fatal(err)
	}
	acct, err = repo.GetByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if acct.Status != admin.StatusActive || acct.PasswordHash != "hash-new" || acct.SessionVersion != 2 {
		t.Fatalf("首次登录后为 active；重置密码与强制下线各让会话版本加一：%+v", acct)
	}

	other, err := repo.CreateRole(ctx, admin.RoleInput{Code: otherCode, Name: "集成测试二", Active: true, Sort: 99})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateAdminRoleStatus(ctx, id, other, admin.StatusInactive); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetAdmin(ctx, id)
	if err != nil || got.Role.Code != otherCode || got.Status != admin.StatusInactive {
		t.Fatalf("角色与状态应已修改：%+v %v", got, err)
	}
	if deleted, err := repo.DeleteRole(ctx, other); err != nil || deleted {
		t.Fatalf("有成员的角色不能删除：%v %v", deleted, err)
	}
	if deleted, err := repo.DeleteRole(ctx, roleID(t, repo, "superadmin")); err != nil || deleted {
		t.Fatalf("内置角色不能删除：%v %v", deleted, err)
	}

	// 至少保留一个可用的超级管理员：把库中现有的超管之外的测试账号设为超管，再停用全部超管必失败
	super := roleID(t, repo, "superadmin")
	if err := repo.UpdateAdminRoleStatus(ctx, id, super, admin.StatusActive); err != nil {
		t.Fatal(err)
	}
	var others int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_user u JOIN admin_role r ON r.id = u.role_id
		WHERE r.is_super AND u.status <> 'inactive' AND u.id <> $1`, id).Scan(&others); err != nil {
		t.Fatal(err)
	}
	err = repo.UpdateAdminRoleStatus(ctx, id, super, admin.StatusInactive)
	if others == 0 && !errors.Is(err, admin.ErrLastSuperAdmin) {
		t.Fatalf("停用最后一个超管应被拒绝，得到 %v", err)
	}
	if others > 0 && err != nil {
		t.Fatalf("还有其他超管时可以停用：%v", err)
	}

	for _, l := range []admin.LoginLog{
		{AdminID: &id, UsernameTried: username, Result: admin.LoginSuccess, IPPrefix: "203.0.0.0/16", UADigest: "abc"},
		{UsernameTried: username, Result: admin.LoginInactive},
	} {
		if err := repo.InsertLoginLog(ctx, l); err != nil {
			t.Fatalf("写登录记录失败：%v", err)
		}
	}

	audit := NewAuditRepo(pool)
	after, _ := json.Marshal(map[string]any{"title": "新剧"})
	entry := platform.AuditEntry{
		AdminID: id, Action: "drama.create", TargetType: "drama", TargetID: "42",
		After: after, IPPrefix: "203.0.0.0/16", RequestID: "req-1", CreatedAt: time.Now(),
	}
	if err := audit.InsertAudit(ctx, entry); err != nil {
		t.Fatal(err)
	}
	var action, title string
	var before []byte
	err = pool.QueryRow(ctx, "SELECT action, diff_after->>'title', diff_before FROM audit_log WHERE admin_id = $1", id).
		Scan(&action, &title, &before)
	if err != nil || action != "drama.create" || title != "新剧" || before != nil {
		t.Fatalf("审计记录不符合预期：%q %q %v %v", action, title, before, err)
	}
}

// 新建角色按请求的状态写入，新建为停用的角色不能直接生效。
func TestAdminRepo_CreateRoleStatus(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAdminRepo(pool)
	for _, active := range []bool{true, false} {
		code := fmt.Sprintf("it_st_%d", time.Now().UnixNano()%1_000_000_000)
		rid, err := repo.CreateRole(ctx, admin.RoleInput{Code: code, Name: "状态测试", Active: active, Sort: 99})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM admin_role WHERE id = $1", rid) })
		got, err := repo.GetRole(ctx, rid)
		if err != nil || got.Active != active {
			t.Fatalf("新建角色的状态应为 active=%v：%+v %v", active, got, err)
		}
	}
}

func TestAdminRepo_Menus(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAdminRepo(pool)

	gid, err := repo.CreateMenu(ctx, admin.Menu{Kind: admin.MenuGroup, Code: fmt.Sprintf("c_it%d", time.Now().UnixNano()%1e9), Title: "测试分组", Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM admin_menu WHERE id = $1 OR parent_id = $1", gid)
	})
	lid, err := repo.CreateMenu(ctx, admin.Menu{ParentID: &gid, Kind: admin.MenuLink, Code: fmt.Sprintf("c_itl%d", time.Now().UnixNano()%1e9),
		Title: "外链", Path: "https://example.com", Visible: true})
	if err != nil {
		t.Fatal(err)
	}
	if deleted, err := repo.DeleteMenu(ctx, gid); err != nil || deleted {
		t.Fatalf("分组下还有菜单时不能删除：%v %v", deleted, err)
	}
	m, err := repo.GetMenu(ctx, lid)
	if err != nil {
		t.Fatal(err)
	}
	m.Title, m.Visible = "外链 2", false
	if err := repo.UpdateMenu(ctx, m); err != nil {
		t.Fatal(err)
	}
	if m, _ := repo.GetMenu(ctx, lid); m.Title != "外链 2" || m.Visible || *m.ParentID != gid {
		t.Fatalf("修改未生效：%+v", m)
	}
	if deleted, err := repo.DeleteMenu(ctx, lid); err != nil || !deleted {
		t.Fatalf("自定义外链可以删除：%v %v", deleted, err)
	}
	if deleted, err := repo.DeleteMenu(ctx, gid); err != nil || !deleted {
		t.Fatalf("空的自定义分组可以删除：%v %v", deleted, err)
	}
	var builtin int64
	if err := pool.QueryRow(ctx, "SELECT id FROM admin_menu WHERE code = 'dramas'").Scan(&builtin); err != nil {
		t.Fatal(err)
	}
	if deleted, _ := repo.DeleteMenu(ctx, builtin); deleted {
		t.Fatal("内置菜单不能删除")
	}
}
