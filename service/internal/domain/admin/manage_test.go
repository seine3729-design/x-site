package admin

import (
	"context"
	"slices"
	"testing"

	"xsite/service/internal/errs"
)

// fakeManageRepo 是内存版仓储。超级管理员保护规则（至少保留一个）与真实仓储一致。
type fakeManageRepo struct {
	admins     map[int64]AdminSummary
	hashes     map[int64]string
	revoked    map[int64]int
	clearedMFA map[int64]int
	roles      map[int64]RoleInfo
	perms      []Permission
	menus      map[int64]Menu
	nextID     int64
}

func newFakeManageRepo() *fakeManageRepo {
	r := &fakeManageRepo{
		admins: map[int64]AdminSummary{}, hashes: map[int64]string{}, revoked: map[int64]int{}, clearedMFA: map[int64]int{},
		roles: map[int64]RoleInfo{}, menus: map[int64]Menu{}, nextID: 100,
	}
	r.roles[1] = RoleInfo{ID: 1, Code: "superadmin", Name: "超级管理员", IsSuper: true, Builtin: true, Active: true}
	r.roles[2] = RoleInfo{ID: 2, Code: "operator", Name: "运营", Builtin: true, Active: true,
		Permissions: []string{"dramas.edit", "dramas.view", "ads.view"}}
	r.roles[3] = RoleInfo{ID: 3, Code: "editor", Name: "编辑", Active: true, Permissions: []string{"dramas.view"}}
	r.roles[4] = RoleInfo{ID: 4, Code: "paused", Name: "已停用角色", Active: false}

	// 一级：数据看板（页面）、内容（目录）、权限（目录）；二级：目录下的页面
	content, access := int64(10), int64(20)
	r.menus[5] = Menu{ID: 5, Kind: MenuPage, Code: "dashboard", Title: "数据看板", Path: "/", Sort: 5, Visible: true, Builtin: true}
	r.menus[content] = Menu{ID: content, Kind: MenuGroup, Code: "content", Title: "内容", Sort: 10, Visible: true, Builtin: true}
	r.menus[access] = Menu{ID: access, Kind: MenuGroup, Code: "access", Title: "权限", Sort: 20, Visible: true, Builtin: true}
	r.menus[11] = Menu{ID: 11, ParentID: &content, Kind: MenuPage, Code: "dramas", Title: "剧集", Path: "/dramas", Sort: 10, Visible: true, Builtin: true}
	r.menus[12] = Menu{ID: 12, ParentID: &content, Kind: MenuPage, Code: "ads", Title: "广告位", Path: "/ads", Sort: 20, Visible: false, Builtin: true}
	r.menus[21] = Menu{ID: 21, ParentID: &access, Kind: MenuPage, Code: "admins", Title: "管理员", Path: "/admins", Sort: 10, Visible: true, Builtin: true}
	r.perms = []Permission{
		{Code: "dashboard.view", MenuID: 5, Name: "查看"},
		{Code: "dramas.view", MenuID: 11, Name: "查看"}, {Code: "dramas.edit", MenuID: 11, Name: "修改", Sort: 1},
		{Code: "ads.view", MenuID: 12, Name: "查看"}, {Code: "ads.edit", MenuID: 12, Name: "修改", Sort: 1},
		{Code: "ads.custom_html", MenuID: 12, Name: "自定义 HTML", Sort: 2},
		{Code: "admins.view", MenuID: 21, Name: "查看"}, {Code: "admins.edit", MenuID: 21, Name: "修改", Sort: 1, Sensitive: true},
	}
	r.admins[1] = AdminSummary{ID: 1, Username: "root", Status: StatusActive, Role: r.ref(1)}
	r.admins[2] = AdminSummary{ID: 2, Username: "ops", Status: StatusActive, Role: r.ref(2)}
	r.admins[3] = AdminSummary{ID: 3, Username: "newbie", Status: StatusUnused, Role: r.ref(3)}
	return r
}

func (r *fakeManageRepo) ref(roleID int64) RoleRef {
	x := r.roles[roleID]
	return RoleRef{ID: x.ID, Code: x.Code, Name: x.Name, IsSuper: x.IsSuper, Active: x.Active}
}

func (r *fakeManageRepo) ListAdmins(context.Context) ([]AdminSummary, error) {
	out := make([]AdminSummary, 0, len(r.admins))
	for _, a := range r.admins {
		out = append(out, a)
	}
	return out, nil
}

func (r *fakeManageRepo) GetAdmin(_ context.Context, id int64) (AdminSummary, error) {
	a, ok := r.admins[id]
	if !ok {
		return AdminSummary{}, errs.ErrNotFound
	}
	return a, nil
}

func (r *fakeManageRepo) CreateAdmin(_ context.Context, username, hash string, roleID int64) (int64, error) {
	for _, a := range r.admins {
		if a.Username == username {
			return 0, errs.ErrConflict.WithMessage("用户名已存在")
		}
	}
	r.nextID++
	r.admins[r.nextID] = AdminSummary{ID: r.nextID, Username: username, Status: StatusUnused, Role: r.ref(roleID)}
	r.hashes[r.nextID] = hash
	return r.nextID, nil
}

func (r *fakeManageRepo) UpdateAdminRoleStatus(_ context.Context, id, roleID int64, status Status) error {
	old := r.admins[id]
	a := old
	a.Role, a.Status = r.ref(roleID), status
	r.admins[id] = a
	usable := 0
	for _, x := range r.admins {
		if x.Role.IsSuper && x.Status != StatusInactive {
			usable++
		}
	}
	if usable == 0 {
		r.admins[id] = old // 事务回滚
		return ErrLastSuperAdmin
	}
	return nil
}

func (r *fakeManageRepo) ResetAdminPassword(_ context.Context, id int64, hash string) error {
	r.hashes[id] = hash
	r.revoked[id]++
	return nil
}

func (r *fakeManageRepo) ClearMFA(_ context.Context, id int64) error {
	r.clearedMFA[id]++
	return nil
}

func (r *fakeManageRepo) RevokeAdminSessions(_ context.Context, id int64) error {
	r.revoked[id]++
	return nil
}

func (r *fakeManageRepo) ListRoles(context.Context) ([]RoleInfo, error) {
	out := make([]RoleInfo, 0, len(r.roles))
	for _, x := range r.roles {
		out = append(out, x)
	}
	return out, nil
}

func (r *fakeManageRepo) GetRole(_ context.Context, id int64) (RoleInfo, error) {
	x, ok := r.roles[id]
	if !ok {
		return RoleInfo{}, errs.ErrNotFound
	}
	for _, a := range r.admins {
		if a.Role.ID == id {
			x.MemberCount++
		}
	}
	return x, nil
}

func (r *fakeManageRepo) GetRoleIDByCode(_ context.Context, code string) (int64, error) {
	for _, x := range r.roles {
		if x.Code == code {
			return x.ID, nil
		}
	}
	return 0, errs.ErrNotFound
}

func (r *fakeManageRepo) CreateRole(_ context.Context, in RoleInput) (int64, error) {
	r.nextID++
	r.roles[r.nextID] = RoleInfo{ID: r.nextID, Code: in.Code, Name: in.Name, Description: in.Description, Active: in.Active}
	return r.nextID, nil
}

func (r *fakeManageRepo) UpdateRole(_ context.Context, id int64, in RoleInput) error {
	x := r.roles[id]
	x.Name, x.Description, x.Active, x.Sort = in.Name, in.Description, in.Active, in.Sort
	r.roles[id] = x
	return nil
}

func (r *fakeManageRepo) DeleteRole(_ context.Context, id int64) (bool, error) {
	delete(r.roles, id)
	return true, nil
}

func (r *fakeManageRepo) ReplaceRolePermissions(_ context.Context, roleID int64, codes []string) error {
	x := r.roles[roleID]
	x.Permissions = codes
	r.roles[roleID] = x
	return nil
}

func (r *fakeManageRepo) ListPermissions(context.Context) ([]Permission, error) { return r.perms, nil }

func (r *fakeManageRepo) ListMenus(context.Context) ([]Menu, error) {
	out := make([]Menu, 0, len(r.menus))
	for _, m := range r.menus {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b Menu) int { return int(a.Sort-b.Sort)*1000 + int(a.ID-b.ID) })
	return out, nil
}

func (r *fakeManageRepo) GetMenu(_ context.Context, id int64) (Menu, error) {
	m, ok := r.menus[id]
	if !ok {
		return Menu{}, errs.ErrNotFound
	}
	return m, nil
}

func (r *fakeManageRepo) CreateMenu(_ context.Context, m Menu) (int64, error) {
	r.nextID++
	m.ID = r.nextID
	r.menus[m.ID] = m
	return m.ID, nil
}

func (r *fakeManageRepo) UpdateMenu(_ context.Context, m Menu) error {
	r.menus[m.ID] = m
	return nil
}

func (r *fakeManageRepo) DeleteMenu(_ context.Context, id int64) (bool, error) {
	for _, m := range r.menus {
		if m.ParentID != nil && *m.ParentID == id {
			return false, nil
		}
	}
	delete(r.menus, id)
	return true, nil
}

func newManager() (*Manager, *fakeManageRepo) {
	repo := newFakeManageRepo()
	return NewManager(repo, &fakeHasher{}), repo
}

// actorOf 用仓储中的管理员与其角色权限构造身份。
func actorOf(repo *fakeManageRepo, adminID int64) Identity {
	a := repo.admins[adminID]
	return NewIdentity(a.ID, a.Username, a.Role, repo.roles[a.Role.ID].Permissions)
}

var ctx = context.Background()

// ---------- 管理员 ----------

func TestCreateAdmin(t *testing.T) {
	m, repo := newManager()
	root, ops := actorOf(repo, 1), actorOf(repo, 2)

	tests := []struct {
		name  string
		actor Identity
		in    NewAccount
		want  *errs.Error
	}{
		{"运营不能创建超级管理员", ops, NewAccount{"boss", "s3cure-pass", 1}, errs.ErrForbidden},
		{"已停用的角色不能分配", root, NewAccount{"temp", "s3cure-pass", 4}, errs.ErrBadRequest},
		{"角色不存在", root, NewAccount{"temp", "s3cure-pass", 999}, errs.ErrNotFound},
		{"用户名不合法", root, NewAccount{"a b", "s3cure-pass", 3}, errs.ErrBadRequest},
		{"密码过短", root, NewAccount{"temp", "short", 3}, errs.ErrBadRequest},
		{"密码与用户名相同", root, NewAccount{"temp-user", "TEMP-USER", 3}, errs.ErrBadRequest},
		{"用户名重复（不区分大小写）", root, NewAccount{"OPS", "s3cure-pass", 3}, errs.ErrConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := m.CreateAdmin(ctx, tt.actor, tt.in)
			assertBusinessError(t, err, tt.want)
		})
	}

	id, err := m.CreateAdmin(ctx, ops, NewAccount{" Writer.01 ", "s3cure-pass", 3})
	if err != nil {
		t.Fatal(err)
	}
	if got := repo.admins[id]; got.Username != "writer.01" || got.Status != StatusUnused || repo.hashes[id] != "hash2:s3cure-pass" {
		t.Fatalf("应保存小写用户名、unused 状态与密码哈希：%+v %q", got, repo.hashes[id])
	}
	if _, err := m.CreateAdmin(ctx, root, NewAccount{"boss", "s3cure-pass", 1}); err != nil {
		t.Fatalf("超级管理员可以创建超级管理员：%v", err)
	}
}

func TestCreateAccountWithRoleCode(t *testing.T) {
	repo := newFakeManageRepo()
	if _, err := CreateAccountWithRoleCode(ctx, repo, &fakeHasher{}, "boot", "s3cure-pass", "nope"); !errs.ErrNotFound.Is(err) {
		t.Fatalf("角色编码不存在时应返回 NOT_FOUND，得到 %v", err)
	}
	id, err := CreateAccountWithRoleCode(ctx, repo, &fakeHasher{}, "boot", "s3cure-pass", "superadmin")
	if err != nil || !repo.admins[id].Role.IsSuper {
		t.Fatalf("命令行可直接创建超级管理员：%v", err)
	}
}

func TestUpdateAdmin(t *testing.T) {
	tests := []struct {
		name  string
		actor int64
		id    int64
		in    AdminUpdate
		want  *errs.Error
	}{
		{"不能改自己", 1, 1, AdminUpdate{RoleID: 2, Status: StatusActive}, errs.ErrForbidden},
		{"运营不能改超级管理员", 2, 1, AdminUpdate{RoleID: 3, Status: StatusActive}, errs.ErrForbidden},
		{"运营不能授予超级管理员角色", 2, 3, AdminUpdate{RoleID: 1, Status: StatusActive}, errs.ErrForbidden},
		{"状态只能是 active / inactive", 1, 3, AdminUpdate{RoleID: 3, Status: StatusUnused}, errs.ErrBadRequest},
		{"不能换到已停用的角色", 1, 3, AdminUpdate{RoleID: 4, Status: StatusActive}, errs.ErrBadRequest},
		{"管理员不存在", 1, 999, AdminUpdate{RoleID: 3, Status: StatusActive}, errs.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, repo := newManager()
			_, err := m.UpdateAdmin(ctx, actorOf(repo, tt.actor), tt.id, tt.in)
			assertBusinessError(t, err, tt.want)
		})
	}

	t.Run("从未登录的账号启用后仍为 unused，前值用于审计", func(t *testing.T) {
		m, repo := newManager()
		before, err := m.UpdateAdmin(ctx, actorOf(repo, 2), 3, AdminUpdate{RoleID: 2, Status: StatusActive})
		if err != nil || before.Role.ID != 3 || repo.admins[3].Status != StatusUnused || repo.admins[3].Role.ID != 2 {
			t.Fatalf("得到 %+v %+v %v", before, repo.admins[3], err)
		}
	})

	t.Run("不能停用或降级最后一个超级管理员", func(t *testing.T) {
		m, repo := newManager()
		repo.admins[5] = AdminSummary{ID: 5, Username: "root2", Status: StatusActive, Role: repo.ref(1)}
		actor := actorOf(repo, 5)
		_, err := m.UpdateAdmin(ctx, actor, 1, AdminUpdate{RoleID: 2, Status: StatusActive})
		if err != nil {
			t.Fatalf("还有另一个超管时可以降级：%v", err)
		}
		// 现在 root2 是唯一的超管。模拟两个超管同时互相停用时后执行的那一方：仓储在事务内发现没有可用超管，回滚
		other := NewIdentity(99, "ghost", repo.ref(1), nil)
		_, err = m.UpdateAdmin(ctx, other, 5, AdminUpdate{RoleID: 1, Status: StatusInactive})
		assertBusinessError(t, err, ErrLastSuperAdmin)
		if repo.admins[5].Status != StatusActive {
			t.Fatal("违反保护规则的修改应回滚")
		}
	})
}

func TestResetPasswordAndRevokeSessions(t *testing.T) {
	m, repo := newManager()
	root, ops := actorOf(repo, 1), actorOf(repo, 2)

	assertBusinessError(t, m.ResetAdminPassword(ctx, root, 1, "n3w-password"), errs.ErrForbidden)
	assertBusinessError(t, m.ResetAdminPassword(ctx, ops, 1, "n3w-password"), errs.ErrForbidden)
	assertBusinessError(t, m.ResetAdminPassword(ctx, root, 3, "short"), errs.ErrBadRequest)
	if err := m.ResetAdminPassword(ctx, ops, 3, "n3w-password"); err != nil {
		t.Fatal(err)
	}
	if repo.hashes[3] != "hash2:n3w-password" || repo.revoked[3] != 1 {
		t.Fatal("重置密码应保存新哈希并吊销其全部会话")
	}

	assertBusinessError(t, m.RevokeAdminSessions(ctx, ops, 2), errs.ErrForbidden)
	assertBusinessError(t, m.RevokeAdminSessions(ctx, ops, 1), errs.ErrForbidden)
	if err := m.RevokeAdminSessions(ctx, root, 2); err != nil || repo.revoked[2] != 1 {
		t.Fatalf("超管可以强制下线运营：%v", err)
	}
}

func TestResetAdminMFA(t *testing.T) {
	m, repo := newManager()
	root, ops := actorOf(repo, 1), actorOf(repo, 2)

	assertBusinessError(t, m.ResetAdminMFA(ctx, ops, 2), errs.ErrForbidden) // 不能重置自己
	assertBusinessError(t, m.ResetAdminMFA(ctx, ops, 1), errs.ErrForbidden) // 运营不能操作超管
	if err := m.ResetAdminMFA(ctx, root, 3); err != nil {
		t.Fatalf("超管可以重置他人的二次验证：%v", err)
	}
	if repo.clearedMFA[3] != 1 || repo.revoked[3] != 1 {
		t.Fatalf("重置二次验证应清除因子并吊销会话：cleared=%d revoked=%d", repo.clearedMFA[3], repo.revoked[3])
	}
}

// ---------- 角色 ----------

func TestRoles(t *testing.T) {
	m, repo := newManager()

	for _, code := range []string{"A", "1abc", "has-dash", "x"} {
		_, err := m.CreateRole(ctx, RoleInput{Code: code, Name: "名称", Active: true})
		assertBusinessError(t, err, errs.ErrBadRequest)
	}
	_, err := m.CreateRole(ctx, RoleInput{Code: "finance", Name: "  ", Active: true})
	assertBusinessError(t, err, errs.ErrBadRequest)
	id, err := m.CreateRole(ctx, RoleInput{Code: "finance", Name: " 财务 ", Active: true})
	if err != nil || repo.roles[id].Name != "财务" || len(repo.roles[id].Permissions) != 0 {
		t.Fatalf("新角色应去掉空白且没有任何权限：%+v %v", repo.roles[id], err)
	}

	_, err = m.UpdateRole(ctx, 1, RoleInput{Name: "超级管理员", Active: false})
	assertBusinessError(t, err, errs.ErrConflict)
	if _, err := m.UpdateRole(ctx, 3, RoleInput{Name: "编辑组", Active: false}); err != nil || repo.roles[3].Active {
		t.Fatalf("自定义角色可以停用：%v", err)
	}

	_, err = m.DeleteRole(ctx, 2) // 内置
	assertBusinessError(t, err, errs.ErrConflict)
	_, err = m.DeleteRole(ctx, 3) // 有成员
	assertBusinessError(t, err, errs.ErrConflict)
	if _, err := m.DeleteRole(ctx, id); err != nil {
		t.Fatalf("没有成员的自定义角色可以删除：%v", err)
	}
}

func TestSetRolePermissions(t *testing.T) {
	tests := []struct {
		name  string
		actor int64
		role  int64
		codes []string
		want  *errs.Error
		after []string
	}{
		{"超级角色不能改", 1, 1, []string{"dramas.view"}, errs.ErrConflict, nil},
		{"权限点不存在", 1, 3, []string{"dramas.view", "nope.edit"}, errs.ErrBadRequest, nil},
		{"授予修改时自动补上查看，结果去重排序", 1, 3, []string{"ads.edit", "dramas.view", "dramas.view"}, nil,
			[]string{"ads.edit", "ads.view", "dramas.view"}},
		{"非超管不能改自己所属的角色", 2, 2, []string{"dramas.view"}, errs.ErrForbidden, nil},
		{"非超管不能授予自己没有的权限", 2, 3, []string{"dramas.view", "admins.view"}, errs.ErrForbidden, nil},
		{"非超管只调整自己拥有的权限", 2, 3, []string{"dramas.edit", "ads.view"}, nil,
			[]string{"ads.view", "dramas.edit", "dramas.view"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, repo := newManager()
			before, after, err := m.SetRolePermissions(ctx, actorOf(repo, tt.actor), tt.role, tt.codes)
			if tt.want != nil {
				assertBusinessError(t, err, tt.want)
				return
			}
			if err != nil || !slices.Equal(after, tt.after) || !slices.Equal(repo.roles[tt.role].Permissions, tt.after) {
				t.Fatalf("得到 %v %v，期望 %v", after, err, tt.after)
			}
			if !slices.Equal(before, []string{"dramas.view"}) {
				t.Fatalf("应返回修改前的权限点用于审计，得到 %v", before)
			}
		})
	}

	t.Run("非超管不能收回自己没有的权限", func(t *testing.T) {
		m, repo := newManager()
		r := repo.roles[3]
		r.Permissions = []string{"admins.view", "dramas.view"}
		repo.roles[3] = r
		_, _, err := m.SetRolePermissions(ctx, actorOf(repo, 2), 3, []string{"dramas.view"})
		assertBusinessError(t, err, errs.ErrForbidden)
	})
}

func TestPermissionTree_FollowsMenus(t *testing.T) {
	m, repo := newManager()
	repo.menus[30] = Menu{ID: 30, Kind: MenuGroup, Code: "c_links", Title: "只有外链的目录", Sort: 30, Visible: true}
	repo.menus[31] = Menu{ID: 31, ParentID: ptr(int64(30)), Kind: MenuLink, Code: "c_help", Title: "帮助", Path: "https://help.example.com", Visible: true}

	tree, err := m.PermissionTree(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var top []string
	for _, n := range tree {
		top = append(top, string(n.Kind)+":"+n.Code)
	}
	if !slices.Equal(top, []string{"page:dashboard", "group:content", "group:access"}) {
		t.Fatalf("一级页面直接是树的节点，按排序值排列；没有页面的目录不出现：%v", top)
	}
	if d := tree[0]; len(d.Permissions) != 1 || len(d.Children) != 0 {
		t.Fatalf("一级页面直接带权限点：%+v", d)
	}
	pages := tree[1].Children
	if len(tree[1].Permissions) != 0 || len(pages) != 2 || pages[0].Code != "dramas" || pages[1].Code != "ads" || len(pages[1].Permissions) != 3 {
		t.Fatalf("目录下是二级页面，隐藏的页面也要列出权限点：%+v", tree[1])
	}
}

func ptr[T any](v T) *T { return &v }

// ---------- 菜单 ----------

func TestMyMenus(t *testing.T) {
	m, repo := newManager()
	content := int64(10)
	repo.menus[13] = Menu{ID: 13, ParentID: &content, Kind: MenuLink, Code: "c_help", Title: "帮助", Path: "https://help.example.com", Sort: 30, Visible: true}
	repo.menus[30] = Menu{ID: 30, Kind: MenuGroup, Code: "c_empty", Title: "空目录", Sort: 30, Visible: true}
	repo.menus[40] = Menu{ID: 40, Kind: MenuLink, Code: "c_status", Title: "状态页", Path: "https://status.example.com", Sort: 40, Visible: true}
	repo.menus[41] = Menu{ID: 41, Kind: MenuLink, Code: "c_hidden", Title: "隐藏外链", Path: "https://x.example.com", Sort: 41, Visible: false}

	codes := func(nodes []MenuNode) (top []string, children map[string][]string) {
		children = map[string][]string{}
		for _, n := range nodes {
			top = append(top, n.Code)
			for _, c := range n.Children {
				children[n.Code] = append(children[n.Code], c.Code)
			}
		}
		return top, children
	}

	nodes, err := m.MyMenus(ctx, actorOf(repo, 3)) // editor：只有 dramas.view
	if err != nil {
		t.Fatal(err)
	}
	top, children := codes(nodes)
	if !slices.Equal(top, []string{"content", "c_status"}) {
		t.Fatalf("没有查看权限的一级页面、没有可见二级菜单的目录与隐藏的菜单都不显示，一级外链对所有人显示：%v", top)
	}
	if !slices.Equal(children["content"], []string{"dramas", "c_help"}) {
		t.Fatalf("无查看权限的页面与隐藏的菜单不显示，外链对所有人显示：%v", children["content"])
	}

	all, _ := m.MyMenus(ctx, actorOf(repo, 1))
	top, children = codes(all)
	if !slices.Equal(top, []string{"dashboard", "content", "access", "c_status"}) || len(children["dashboard"]) != 0 ||
		!slices.Equal(children["access"], []string{"admins"}) {
		t.Fatalf("超管看到全部可见菜单，一级页面没有子菜单：%v %v", top, children)
	}
}

func TestMenus(t *testing.T) {
	content, page := int64(10), int64(11)
	tests := []struct {
		name string
		in   MenuInput
	}{
		{"不能新建页面", MenuInput{Kind: MenuPage, ParentID: &content, Title: "新页面", Path: "/x", Visible: true}},
		{"目录只能是一级菜单", MenuInput{Kind: MenuGroup, ParentID: &content, Title: "子目录", Visible: true}},
		{"外链的上级必须是目录", MenuInput{Kind: MenuLink, ParentID: &page, Title: "外链", Path: "https://a.example.com", Visible: true}},
		{"外链的上级不存在", MenuInput{Kind: MenuLink, ParentID: ptr(int64(999)), Title: "外链", Path: "https://a.example.com", Visible: true}},
		{"拒绝 javascript: 地址", MenuInput{Kind: MenuLink, ParentID: &content, Title: "外链", Path: "javascript:alert(1)", Visible: true}},
		{"拒绝相对地址", MenuInput{Kind: MenuLink, ParentID: &content, Title: "外链", Path: "/dramas", Visible: true}},
		{"名称不能为空", MenuInput{Kind: MenuGroup, Title: " ", Visible: true}},
		{"图标名称无效", MenuInput{Kind: MenuGroup, Title: "目录", Icon: "<svg>", Visible: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newManager()
			_, err := m.CreateMenu(ctx, tt.in)
			assertBusinessError(t, err, errs.ErrBadRequest)
		})
	}

	m, repo := newManager()
	gid, err := m.CreateMenu(ctx, MenuInput{Kind: MenuGroup, Title: "常用", Icon: "Star", Sort: 5, Visible: true})
	if err != nil || repo.menus[gid].Code[:2] != "c_" || repo.menus[gid].Builtin {
		t.Fatalf("新建目录应生成自定义编码：%+v %v", repo.menus[gid], err)
	}
	lid, err := m.CreateMenu(ctx, MenuInput{Kind: MenuLink, ParentID: &gid, Title: "监控", Path: " https://grafana.example.com/d/1 ", Visible: true})
	if err != nil || repo.menus[lid].Path != "https://grafana.example.com/d/1" {
		t.Fatalf("外链应去掉空白：%+v %v", repo.menus[lid], err)
	}
	topLink, err := m.CreateMenu(ctx, MenuInput{Kind: MenuLink, Title: "状态页", Path: "https://status.example.com", Visible: true})
	if err != nil || repo.menus[topLink].ParentID != nil {
		t.Fatalf("外链可以是一级菜单：%+v %v", repo.menus[topLink], err)
	}

	// 页面移到新目录、改名改图标，但路径保持不变
	before, err := m.UpdateMenu(ctx, 11, MenuInput{ParentID: &gid, Title: "短剧", Path: "/hack", Icon: "Film", Sort: 1, Visible: true})
	if err != nil || before.Title != "剧集" {
		t.Fatalf("应返回修改前的记录：%+v %v", before, err)
	}
	if got := repo.menus[11]; got.Path != "/dramas" || *got.ParentID != gid || got.Title != "短剧" || got.Kind != MenuPage {
		t.Fatalf("页面路径与类型不能改：%+v", got)
	}
	// 二级页面提为一级菜单，再放回目录
	if _, err := m.UpdateMenu(ctx, 11, MenuInput{Title: "短剧", Sort: 1, Visible: true}); err != nil || repo.menus[11].ParentID != nil {
		t.Fatalf("页面可以提为一级菜单：%+v %v", repo.menus[11], err)
	}
	if _, err := m.UpdateMenu(ctx, 11, MenuInput{ParentID: &content, Title: "短剧", Sort: 1, Visible: true}); err != nil {
		t.Fatal(err)
	}
	_, err = m.UpdateMenu(ctx, 10, MenuInput{ParentID: &gid, Title: "内容", Visible: true})
	assertBusinessError(t, err, errs.ErrBadRequest) // 目录不能挂到别的目录下

	_, err = m.DeleteMenu(ctx, 10) // 内置目录
	assertBusinessError(t, err, errs.ErrConflict)
	_, err = m.DeleteMenu(ctx, gid) // 还有外链
	assertBusinessError(t, err, errs.ErrConflict)
	if _, err := m.DeleteMenu(ctx, lid); err != nil {
		t.Fatalf("自定义外链可以删除：%v", err)
	}
}
