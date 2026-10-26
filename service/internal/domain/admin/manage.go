package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

// AdminSummary 是管理员列表中的一行。
type AdminSummary struct {
	ID          int64
	Username    string
	Status      Status
	MFAEnabled  bool
	Role        RoleRef
	LastLoginAt *time.Time
	CreatedAt   time.Time
}

// RoleInfo 是角色及其权限点。
type RoleInfo struct {
	ID          int64
	Code        string
	Name        string
	Description string
	IsSuper     bool
	Builtin     bool
	Active      bool
	Sort        int32
	MemberCount int64
	Permissions []string
}

// Permission 是一个权限点（admin_permission），挂在某个页面菜单下。
type Permission struct {
	Code      string
	MenuID    int64
	Name      string
	Sensitive bool
	Sort      int32
}

// MenuKind 是后台菜单类型。
type MenuKind string

const (
	MenuGroup MenuKind = "group" // 目录：只能是一级菜单，用来收纳二级菜单
	MenuPage  MenuKind = "page"  // 代码提供的页面（path 为前端路由）
	MenuLink  MenuKind = "link"  // 外部链接（新窗口打开）
)

// Menu 是后台菜单（admin_menu）。最多两级：一级菜单是目录、页面或外链，二级菜单是目录下的页面或外链。
type Menu struct {
	ID       int64
	ParentID *int64
	Kind     MenuKind
	Code     string
	Title    string
	Path     string
	Icon     string
	Sort     int32
	Visible  bool
	Builtin  bool
}

// ManageRepo 读写管理员、角色、权限点与菜单。查不到时返回 errs.ErrNotFound，唯一键冲突返回 errs.ErrConflict。
type ManageRepo interface {
	ListAdmins(ctx context.Context) ([]AdminSummary, error)
	GetAdmin(ctx context.Context, id int64) (AdminSummary, error)
	CreateAdmin(ctx context.Context, username, passwordHash string, roleID int64) (int64, error)
	// UpdateAdminRoleStatus 在事务内修改，修改后没有可用的超级管理员时回滚并返回 ErrLastSuperAdmin。
	UpdateAdminRoleStatus(ctx context.Context, id, roleID int64, status Status) error
	// ResetAdminPassword 更新密码哈希并吊销该管理员的全部会话。
	ResetAdminPassword(ctx context.Context, id int64, passwordHash string) error
	RevokeAdminSessions(ctx context.Context, id int64) error
	// ClearMFA 清除某管理员的全部二次验证因子（TOTP + Passkey），用于重置二次验证。
	ClearMFA(ctx context.Context, id int64) error

	ListRoles(ctx context.Context) ([]RoleInfo, error)
	GetRole(ctx context.Context, id int64) (RoleInfo, error)
	GetRoleIDByCode(ctx context.Context, code string) (int64, error)
	CreateRole(ctx context.Context, in RoleInput) (int64, error)
	UpdateRole(ctx context.Context, id int64, in RoleInput) error
	// DeleteRole 只删除没有成员的自定义角色，返回是否删除。
	DeleteRole(ctx context.Context, id int64) (bool, error)
	// ReplaceRolePermissions 在事务内整体替换角色的权限点。
	ReplaceRolePermissions(ctx context.Context, roleID int64, codes []string) error
	ListPermissions(ctx context.Context) ([]Permission, error)

	ListMenus(ctx context.Context) ([]Menu, error)
	GetMenu(ctx context.Context, id int64) (Menu, error)
	CreateMenu(ctx context.Context, m Menu) (int64, error)
	UpdateMenu(ctx context.Context, m Menu) error
	// DeleteMenu 只删除自定义菜单，目录下还有菜单时不删除；返回是否删除。
	DeleteMenu(ctx context.Context, id int64) (bool, error)
}

var (
	// ErrLastSuperAdmin 由仓储在修改管理员角色或状态后发现没有可用的超级管理员时返回。
	ErrLastSuperAdmin = errs.ErrConflict.WithMessage("至少要保留一个可用的超级管理员")

	errSelf           = errs.ErrForbidden.WithMessage("不能修改自己的账号，请让其他管理员操作")
	errSuperOnly      = errs.ErrForbidden.WithMessage("只有超级管理员可以操作超级管理员账号或授予超级管理员角色")
	errRoleInactive   = errs.ErrBadRequest.WithMessage("该角色已停用，请先启用或换一个角色")
	errSuperRoleFixed = errs.ErrConflict.WithMessage("超级管理员角色拥有全部权限，不能修改权限或停用")
	errOwnRole        = errs.ErrForbidden.WithMessage("不能修改自己所属角色的权限，请让超级管理员操作")
	errGrantBeyond    = errs.ErrForbidden.WithMessage("只能授予或收回自己拥有的权限")
	errRoleInUse      = errs.ErrConflict.WithMessage("内置角色不能删除；有成员的角色需先把成员换到其他角色")
	errMenuInUse      = errs.ErrConflict.WithMessage("内置菜单不能删除，可以隐藏；目录下还有菜单时不能删除")
)

// Manager 处理管理员、角色、角色权限与后台菜单的管理（docs/06 第 33、33a–33c 节）。
type Manager struct {
	repo   ManageRepo
	hasher PasswordHasher
}

// NewManager 创建管理服务。
func NewManager(repo ManageRepo, hasher PasswordHasher) *Manager {
	return &Manager{repo: repo, hasher: hasher}
}

// ---------- 管理员 ----------

// ListAdmins 返回全部管理员。
func (m *Manager) ListAdmins(ctx context.Context) ([]AdminSummary, error) {
	return m.repo.ListAdmins(ctx)
}

// NewAccount 是新建管理员的输入。
type NewAccount struct {
	Username string
	Password string
	RoleID   int64
}

// CreateAdmin 由 actor 新建管理员（状态为 unused）。授予超级管理员角色只能由超级管理员操作。
func (m *Manager) CreateAdmin(ctx context.Context, actor Identity, in NewAccount) (int64, error) {
	role, err := m.repo.GetRole(ctx, in.RoleID)
	if err != nil {
		return 0, roleNotFound(err)
	}
	if role.IsSuper && !actor.Role.IsSuper {
		return 0, errSuperOnly
	}
	if !role.Active {
		return 0, errRoleInactive
	}
	return createAccount(ctx, m.repo, m.hasher, in)
}

// CreateAccountWithRoleCode 是命令行工具创建管理员的入口（首个超级管理员只能这样创建，不经过 actor 校验）。
func CreateAccountWithRoleCode(ctx context.Context, repo ManageRepo, hasher PasswordHasher, username, password, roleCode string) (int64, error) {
	roleID, err := repo.GetRoleIDByCode(ctx, roleCode)
	if err != nil {
		return 0, roleNotFound(err)
	}
	return createAccount(ctx, repo, hasher, NewAccount{Username: username, Password: password, RoleID: roleID})
}

func createAccount(ctx context.Context, repo ManageRepo, hasher PasswordHasher, in NewAccount) (int64, error) {
	username := NormalizeUsername(in.Username)
	if !ValidUsername(username) {
		return 0, errs.ErrBadRequest.WithMessage("用户名需 3–32 位，只能包含字母、数字、下划线、点和连字符")
	}
	if err := validatePassword(username, in.Password); err != nil {
		return 0, err
	}
	hash, err := hasher.Hash(ctx, in.Password)
	if err != nil {
		return 0, fmt.Errorf("哈希密码: %w", err)
	}
	return repo.CreateAdmin(ctx, username, hash, in.RoleID)
}

func validatePassword(username, pw string) error {
	if len(pw) < 8 || len(pw) > 128 {
		return errs.ErrBadRequest.WithMessage("密码长度需 8–128 位")
	}
	if NormalizeUsername(pw) == username {
		return errs.ErrBadRequest.WithMessage("密码不能与用户名相同")
	}
	return nil
}

// AdminUpdate 是对管理员角色与状态的修改。
type AdminUpdate struct {
	RoleID int64
	Status Status
}

// UpdateAdmin 修改管理员的角色与状态，返回修改前的记录（用于审计）。
// 不能改自己；涉及超级管理员（目标本身是超管，或要换成超管角色）只能由超级管理员操作；停用后立即失效。
func (m *Manager) UpdateAdmin(ctx context.Context, actor Identity, id int64, in AdminUpdate) (AdminSummary, error) {
	if in.Status != StatusActive && in.Status != StatusInactive {
		return AdminSummary{}, errs.ErrBadRequest.WithMessage("状态只能是 active 或 inactive")
	}
	target, err := m.targetAdmin(ctx, actor, id)
	if err != nil {
		return AdminSummary{}, err
	}
	role, err := m.repo.GetRole(ctx, in.RoleID)
	if err != nil {
		return AdminSummary{}, roleNotFound(err)
	}
	if role.IsSuper && !actor.Role.IsSuper {
		return AdminSummary{}, errSuperOnly
	}
	if !role.Active && in.Status == StatusActive {
		return AdminSummary{}, errRoleInactive
	}
	status := in.Status
	// 从未登录过的账号保持 unused，直到首次登录
	if status == StatusActive && target.Status == StatusUnused {
		status = StatusUnused
	}
	if err := m.repo.UpdateAdminRoleStatus(ctx, id, in.RoleID, status); err != nil {
		return AdminSummary{}, err
	}
	return target, nil
}

// ResetAdminPassword 由 actor 重置其他管理员的密码，该管理员的全部会话随即失效。
// 这是后台管理员之间的操作；用户端账号不提供任何重设密码的途径（docs/adr/0001）。
func (m *Manager) ResetAdminPassword(ctx context.Context, actor Identity, id int64, password string) error {
	target, err := m.targetAdmin(ctx, actor, id)
	if err != nil {
		return err
	}
	if err := validatePassword(target.Username, password); err != nil {
		return err
	}
	hash, err := m.hasher.Hash(ctx, password)
	if err != nil {
		return fmt.Errorf("哈希密码: %w", err)
	}
	return m.repo.ResetAdminPassword(ctx, id, hash)
}

// RevokeAdminSessions 让该管理员的全部会话立即失效（强制下线）。
func (m *Manager) RevokeAdminSessions(ctx context.Context, actor Identity, id int64) error {
	if _, err := m.targetAdmin(ctx, actor, id); err != nil {
		return err
	}
	return m.repo.RevokeAdminSessions(ctx, id)
}

// ResetAdminMFA 清除某管理员的全部二次验证因子（TOTP + Passkey），用于其丢失认证器时由他人重置（S2-6）。
// 同时吊销该管理员的会话，避免其在旧会话里绕过「超管强制开启」等后续校验。
func (m *Manager) ResetAdminMFA(ctx context.Context, actor Identity, id int64) error {
	if _, err := m.targetAdmin(ctx, actor, id); err != nil {
		return err
	}
	if err := m.repo.ClearMFA(ctx, id); err != nil {
		return err
	}
	return m.repo.RevokeAdminSessions(ctx, id)
}

// targetAdmin 查出被操作的管理员，并校验 actor 是否能操作它。
func (m *Manager) targetAdmin(ctx context.Context, actor Identity, id int64) (AdminSummary, error) {
	if id == actor.AdminID {
		return AdminSummary{}, errSelf
	}
	target, err := m.repo.GetAdmin(ctx, id)
	if errors.Is(err, errs.ErrNotFound) {
		return AdminSummary{}, errs.ErrNotFound.WithMessage("管理员不存在")
	}
	if err != nil {
		return AdminSummary{}, err
	}
	if target.Role.IsSuper && !actor.Role.IsSuper {
		return AdminSummary{}, errSuperOnly
	}
	return target, nil
}

// ---------- 角色 ----------

// RoleInput 是新建或修改角色的输入。Code 只在新建时使用，之后不能改。
type RoleInput struct {
	Code        string
	Name        string
	Description string
	Active      bool
	Sort        int32
}

var roleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

// ListRoles 返回全部角色及其权限点、成员数。
func (m *Manager) ListRoles(ctx context.Context) ([]RoleInfo, error) { return m.repo.ListRoles(ctx) }

// SensitivePermissions 返回标为高敏（admin_permission.sensitive）的权限点编码，供启动时装配重新验证门槛（S2-6）。
func (m *Manager) SensitivePermissions(ctx context.Context) ([]string, error) {
	perms, err := m.repo.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		if p.Sensitive {
			out = append(out, p.Code)
		}
	}
	return out, nil
}

// CreateRole 新建自定义角色（新角色没有任何权限，在权限矩阵中授予）。
func (m *Manager) CreateRole(ctx context.Context, in RoleInput) (int64, error) {
	in.Code = strings.TrimSpace(in.Code)
	if !roleCodePattern.MatchString(in.Code) {
		return 0, errs.ErrBadRequest.WithMessage("角色编码需 2–32 位，以小写字母开头，只能包含小写字母、数字和下划线")
	}
	if err := validateRoleText(&in); err != nil {
		return 0, err
	}
	return m.repo.CreateRole(ctx, in)
}

// UpdateRole 修改角色名称、说明、状态与排序，返回修改前的记录。超级管理员角色不能停用。
func (m *Manager) UpdateRole(ctx context.Context, id int64, in RoleInput) (RoleInfo, error) {
	before, err := m.repo.GetRole(ctx, id)
	if err != nil {
		return RoleInfo{}, roleNotFound(err)
	}
	if err := validateRoleText(&in); err != nil {
		return RoleInfo{}, err
	}
	if before.IsSuper && !in.Active {
		return RoleInfo{}, errSuperRoleFixed
	}
	return before, m.repo.UpdateRole(ctx, id, in)
}

// DeleteRole 删除没有成员的自定义角色，返回删除前的记录。
func (m *Manager) DeleteRole(ctx context.Context, id int64) (RoleInfo, error) {
	before, err := m.repo.GetRole(ctx, id)
	if err != nil {
		return RoleInfo{}, roleNotFound(err)
	}
	if before.Builtin || before.MemberCount > 0 {
		return RoleInfo{}, errRoleInUse
	}
	deleted, err := m.repo.DeleteRole(ctx, id)
	if err != nil {
		return RoleInfo{}, err
	}
	if !deleted { // 检查之后有人把管理员换到了这个角色
		return RoleInfo{}, errRoleInUse
	}
	return before, nil
}

func validateRoleText(in *RoleInput) error {
	in.Name, in.Description = strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if n := utf8.RuneCountInString(in.Name); n < 1 || n > 32 {
		return errs.ErrBadRequest.WithMessage("角色名称需 1–32 个字")
	}
	if utf8.RuneCountInString(in.Description) > 200 {
		return errs.ErrBadRequest.WithMessage("角色说明不能超过 200 个字")
	}
	return nil
}

// SetRolePermissions 整体替换角色的权限点，返回修改前后的权限点（用于审计）。
//
//   - 超级管理员角色拥有全部权限，不能修改。
//   - 非超级管理员不能改自己所属角色，且只能授予或收回自己拥有的权限点（防止越权给自己或别人提权）。
//   - 授予某页面的任一权限点时自动补上该页面的「查看」，否则页面在菜单中不可见。
func (m *Manager) SetRolePermissions(ctx context.Context, actor Identity, roleID int64, codes []string) (before, after []string, err error) {
	role, err := m.repo.GetRole(ctx, roleID)
	if err != nil {
		return nil, nil, roleNotFound(err)
	}
	if role.IsSuper {
		return nil, nil, errSuperRoleFixed
	}
	catalog, err := m.repo.ListPermissions(ctx)
	if err != nil {
		return nil, nil, err
	}
	after, err = normalizeGrant(catalog, codes)
	if err != nil {
		return nil, nil, err
	}
	if !actor.Role.IsSuper {
		if actor.Role.ID == roleID {
			return nil, nil, errOwnRole
		}
		for _, c := range symmetricDiff(role.Permissions, after) {
			if !actor.Can(c) {
				return nil, nil, errGrantBeyond
			}
		}
	}
	if err := m.repo.ReplaceRolePermissions(ctx, roleID, after); err != nil {
		return nil, nil, err
	}
	return role.Permissions, after, nil
}

// normalizeGrant 校验权限点都存在、去重排序，并为每个被授予权限的页面补上「查看」。
func normalizeGrant(catalog []Permission, codes []string) ([]string, error) {
	known := make(map[string]Permission, len(catalog))
	viewOf := make(map[int64]string)
	for _, p := range catalog {
		known[p.Code] = p
		if strings.HasSuffix(p.Code, ".view") {
			viewOf[p.MenuID] = p.Code
		}
	}
	set := make(map[string]bool, len(codes))
	for _, c := range codes {
		p, ok := known[c]
		if !ok {
			return nil, errs.ErrBadRequest.WithMessage("权限点不存在：" + c)
		}
		set[c] = true
		if v, ok := viewOf[p.MenuID]; ok {
			set[v] = true
		}
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	slices.Sort(out)
	return out, nil
}

func symmetricDiff(a, b []string) []string {
	in := func(list []string, s string) bool { return slices.Contains(list, s) }
	var out []string
	for _, x := range a {
		if !in(b, x) {
			out = append(out, x)
		}
	}
	for _, x := range b {
		if !in(a, x) {
			out = append(out, x)
		}
	}
	return out
}

// ---------- 权限点目录 ----------

// PermissionNode 是权限点目录（权限树）中的一个节点：一级目录的 Children 是它下面的页面；
// 页面（一级或二级）的 Permissions 是该页面的权限点，没有 Children。
type PermissionNode struct {
	Code        string
	Kind        MenuKind // MenuGroup 或 MenuPage
	Title       string
	Permissions []Permission
	Children    []PermissionNode
}

// PermissionTree 按当前菜单结构（一级菜单 → 二级菜单）组织全部权限点，含已隐藏的菜单；
// 外链没有权限点，不出现；没有页面的目录也不出现。
func (m *Manager) PermissionTree(ctx context.Context) ([]PermissionNode, error) {
	menus, err := m.repo.ListMenus(ctx)
	if err != nil {
		return nil, err
	}
	perms, err := m.repo.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	byMenu := make(map[int64][]Permission)
	for _, p := range perms {
		byMenu[p.MenuID] = append(byMenu[p.MenuID], p)
	}
	pageNode := func(p Menu) (PermissionNode, bool) {
		if p.Kind != MenuPage || len(byMenu[p.ID]) == 0 {
			return PermissionNode{}, false
		}
		return PermissionNode{Code: p.Code, Kind: MenuPage, Title: p.Title, Permissions: byMenu[p.ID]}, true
	}
	var out []PermissionNode
	for _, top := range menus {
		if top.ParentID != nil {
			continue
		}
		if top.Kind != MenuGroup {
			if n, ok := pageNode(top); ok {
				out = append(out, n)
			}
			continue
		}
		dir := PermissionNode{Code: top.Code, Kind: MenuGroup, Title: top.Title}
		for _, child := range childrenOf(menus, top.ID) {
			if n, ok := pageNode(child); ok {
				dir.Children = append(dir.Children, n)
			}
		}
		if len(dir.Children) > 0 {
			out = append(out, dir)
		}
	}
	return out, nil
}

// childrenOf 返回某个目录下的二级菜单（menus 已按排序值排列）。
func childrenOf(menus []Menu, parentID int64) []Menu {
	var out []Menu
	for _, it := range menus {
		if it.ParentID != nil && *it.ParentID == parentID {
			out = append(out, it)
		}
	}
	return out
}

// PermissionCodes 返回权限点目录中的全部编码（启动时与路由声明的权限点比对）。
func (m *Manager) PermissionCodes(ctx context.Context) ([]string, error) {
	perms, err := m.repo.ListPermissions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = p.Code
	}
	return out, nil
}

// ---------- 后台菜单 ----------

// MenuInput 是新建或修改菜单的输入。新建只能是目录或外链；页面由代码提供，只能改展示属性与上级目录。
// ParentID 为空表示一级菜单；目录只能是一级菜单。
type MenuInput struct {
	Kind     MenuKind
	ParentID *int64
	Title    string
	Path     string
	Icon     string
	Sort     int32
	Visible  bool
}

var iconPattern = regexp.MustCompile(`^[A-Za-z0-9]{0,40}$`)

// ListMenus 返回全部菜单（含隐藏的），按排序值。
func (m *Manager) ListMenus(ctx context.Context) ([]Menu, error) { return m.repo.ListMenus(ctx) }

// MenuNode 是侧栏中的一个一级菜单：目录的 Children 是它下面可见的二级菜单；页面与外链没有 Children。
type MenuNode struct {
	Menu
	Children []Menu
}

// MyMenus 返回 actor 可见的两级侧栏：隐藏的菜单不显示；页面需有「查看」权限；外链对所有人显示；
// 没有可见二级菜单的目录不显示。
func (m *Manager) MyMenus(ctx context.Context, actor Identity) ([]MenuNode, error) {
	menus, err := m.repo.ListMenus(ctx)
	if err != nil {
		return nil, err
	}
	shown := func(it Menu) bool {
		return it.Visible && (it.Kind != MenuPage || actor.Can(it.Code+".view"))
	}
	var out []MenuNode
	for _, top := range menus {
		if top.ParentID != nil || !shown(top) {
			continue
		}
		if top.Kind != MenuGroup {
			out = append(out, MenuNode{Menu: top})
			continue
		}
		node := MenuNode{Menu: top}
		for _, child := range childrenOf(menus, top.ID) {
			if shown(child) {
				node.Children = append(node.Children, child)
			}
		}
		if len(node.Children) > 0 {
			out = append(out, node)
		}
	}
	return out, nil
}

// CreateMenu 新建目录或外链。
func (m *Manager) CreateMenu(ctx context.Context, in MenuInput) (int64, error) {
	if in.Kind != MenuGroup && in.Kind != MenuLink {
		return 0, errs.ErrBadRequest.WithMessage("只能新建目录或外链；页面由系统提供")
	}
	code, err := customMenuCode()
	if err != nil {
		return 0, err
	}
	menu := Menu{Kind: in.Kind, Code: code}
	if err := m.applyMenuInput(ctx, &menu, in); err != nil {
		return 0, err
	}
	return m.repo.CreateMenu(ctx, menu)
}

// UpdateMenu 修改菜单，返回修改前的记录。页面的路径不能改。
func (m *Manager) UpdateMenu(ctx context.Context, id int64, in MenuInput) (Menu, error) {
	before, err := m.repo.GetMenu(ctx, id)
	if err != nil {
		return Menu{}, menuNotFound(err)
	}
	menu := before
	in.Kind = before.Kind
	if before.Kind == MenuPage {
		in.Path = before.Path
	}
	if err := m.applyMenuInput(ctx, &menu, in); err != nil {
		return Menu{}, err
	}
	return before, m.repo.UpdateMenu(ctx, menu)
}

// DeleteMenu 删除自定义菜单，返回删除前的记录。
func (m *Manager) DeleteMenu(ctx context.Context, id int64) (Menu, error) {
	before, err := m.repo.GetMenu(ctx, id)
	if err != nil {
		return Menu{}, menuNotFound(err)
	}
	if before.Builtin {
		return Menu{}, errMenuInUse
	}
	deleted, err := m.repo.DeleteMenu(ctx, id)
	if err != nil {
		return Menu{}, err
	}
	if !deleted {
		return Menu{}, errMenuInUse
	}
	return before, nil
}

// applyMenuInput 校验并写入可修改的字段。菜单最多两级：目录只能是一级菜单；
// 页面与外链可以是一级菜单（不选上级），也可以挂在某个目录下。
func (m *Manager) applyMenuInput(ctx context.Context, menu *Menu, in MenuInput) error {
	title := strings.TrimSpace(in.Title)
	if n := utf8.RuneCountInString(title); n < 1 || n > 32 {
		return errs.ErrBadRequest.WithMessage("菜单名称需 1–32 个字")
	}
	if !iconPattern.MatchString(in.Icon) {
		return errs.ErrBadRequest.WithMessage("图标名称无效")
	}
	if in.Kind == MenuGroup && in.ParentID != nil {
		return errs.ErrBadRequest.WithMessage("目录只能是一级菜单")
	}
	if in.Kind != MenuGroup && in.ParentID != nil {
		parent, err := m.repo.GetMenu(ctx, *in.ParentID)
		if err != nil || parent.Kind != MenuGroup {
			return errs.ErrBadRequest.WithMessage("上级菜单必须是目录")
		}
	}
	if in.Kind == MenuLink {
		if err := validateLink(in.Path); err != nil {
			return err
		}
	}
	menu.ParentID, menu.Title, menu.Path, menu.Icon, menu.Sort, menu.Visible =
		in.ParentID, title, strings.TrimSpace(in.Path), in.Icon, in.Sort, in.Visible
	if in.Kind == MenuGroup {
		menu.Path = ""
	}
	return nil
}

// validateLink 只接受 http(s) 绝对地址，防止 javascript: 等协议进入侧栏链接。
func validateLink(raw string) error {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 500 || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return errs.ErrBadRequest.WithMessage("外链需为 http:// 或 https:// 开头的完整地址")
	}
	return nil
}

func customMenuCode() (string, error) {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成菜单编码: %w", err)
	}
	return "c_" + hex.EncodeToString(b), nil
}

func roleNotFound(err error) error {
	if errors.Is(err, errs.ErrNotFound) {
		return errs.ErrNotFound.WithMessage("角色不存在")
	}
	return err
}

func menuNotFound(err error) error {
	if errors.Is(err, errs.ErrNotFound) {
		return errs.ErrNotFound.WithMessage("菜单不存在")
	}
	return err
}
