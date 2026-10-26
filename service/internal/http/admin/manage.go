package admin

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	adm "xsite/service/internal/domain/admin"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// Manager 是管理员、角色、权限矩阵与后台菜单的管理服务（domain/admin.Manager）。
type Manager interface {
	ListAdmins(ctx context.Context) ([]adm.AdminSummary, error)
	CreateAdmin(ctx context.Context, actor adm.Identity, in adm.NewAccount) (int64, error)
	UpdateAdmin(ctx context.Context, actor adm.Identity, id int64, in adm.AdminUpdate) (adm.AdminSummary, error)
	ResetAdminPassword(ctx context.Context, actor adm.Identity, id int64, password string) error
	RevokeAdminSessions(ctx context.Context, actor adm.Identity, id int64) error
	ResetAdminMFA(ctx context.Context, actor adm.Identity, id int64) error

	ListRoles(ctx context.Context) ([]adm.RoleInfo, error)
	CreateRole(ctx context.Context, in adm.RoleInput) (int64, error)
	UpdateRole(ctx context.Context, id int64, in adm.RoleInput) (adm.RoleInfo, error)
	DeleteRole(ctx context.Context, id int64) (adm.RoleInfo, error)
	SetRolePermissions(ctx context.Context, actor adm.Identity, roleID int64, codes []string) (before, after []string, err error)
	PermissionTree(ctx context.Context) ([]adm.PermissionNode, error)

	ListMenus(ctx context.Context) ([]adm.Menu, error)
	MyMenus(ctx context.Context, actor adm.Identity) ([]adm.MenuNode, error)
	CreateMenu(ctx context.Context, in adm.MenuInput) (int64, error)
	UpdateMenu(ctx context.Context, id int64, in adm.MenuInput) (adm.Menu, error)
	DeleteMenu(ctx context.Context, id int64) (adm.Menu, error)
}

// ---------- DTO ----------

type roleRefDTO struct {
	ID      int64  `json:"id"`
	Code    string `json:"code"`
	Name    string `json:"name"`
	IsSuper bool   `json:"is_super"`
}

type adminDTO struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	Status      string     `json:"status"`
	MFAEnabled  bool       `json:"mfa_enabled"`
	Role        roleRefDTO `json:"role"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func adminOf(a adm.AdminSummary) adminDTO {
	return adminDTO{
		ID: a.ID, Username: a.Username, Status: string(a.Status), MFAEnabled: a.MFAEnabled,
		Role:        roleRefDTO{ID: a.Role.ID, Code: a.Role.Code, Name: a.Role.Name, IsSuper: a.Role.IsSuper},
		LastLoginAt: a.LastLoginAt, CreatedAt: a.CreatedAt,
	}
}

type roleDTO struct {
	ID          int64    `json:"id"`
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsSuper     bool     `json:"is_super"`
	Builtin     bool     `json:"builtin"`
	Status      string   `json:"status"`
	Sort        int32    `json:"sort"`
	MemberCount int64    `json:"member_count"`
	Permissions []string `json:"permissions"`
}

func roleOf(r adm.RoleInfo) roleDTO {
	status := "inactive"
	if r.Active {
		status = "active"
	}
	return roleDTO{
		ID: r.ID, Code: r.Code, Name: r.Name, Description: r.Description, IsSuper: r.IsSuper, Builtin: r.Builtin,
		Status: status, Sort: r.Sort, MemberCount: r.MemberCount, Permissions: r.Permissions,
	}
}

type permissionDTO struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Sensitive bool   `json:"sensitive"`
}

// permissionNodeDTO 是权限树的节点：目录（kind=group）只有 children，页面（kind=page）只有 permissions。
type permissionNodeDTO struct {
	Code        string              `json:"code"`
	Kind        string              `json:"kind"`
	Title       string              `json:"title"`
	Permissions []permissionDTO     `json:"permissions"`
	Children    []permissionNodeDTO `json:"children"`
}

func permissionNodeOf(n adm.PermissionNode) permissionNodeDTO {
	perms := make([]permissionDTO, len(n.Permissions))
	for i, p := range n.Permissions {
		perms[i] = permissionDTO{Code: p.Code, Name: p.Name, Sensitive: p.Sensitive}
	}
	children := make([]permissionNodeDTO, len(n.Children))
	for i, c := range n.Children {
		children[i] = permissionNodeOf(c)
	}
	return permissionNodeDTO{Code: n.Code, Kind: string(n.Kind), Title: n.Title, Permissions: perms, Children: children}
}

type menuDTO struct {
	ID       int64  `json:"id"`
	ParentID *int64 `json:"parent_id"`
	Kind     string `json:"kind"`
	Code     string `json:"code"`
	Title    string `json:"title"`
	Path     string `json:"path"`
	Icon     string `json:"icon"`
	Sort     int32  `json:"sort"`
	Visible  bool   `json:"visible"`
	Builtin  bool   `json:"builtin"`
}

func menuOf(m adm.Menu) menuDTO {
	return menuDTO{
		ID: m.ID, ParentID: m.ParentID, Kind: string(m.Kind), Code: m.Code, Title: m.Title, Path: m.Path,
		Icon: m.Icon, Sort: m.Sort, Visible: m.Visible, Builtin: m.Builtin,
	}
}

// navNodeDTO 是侧栏的一项：一级菜单可以是目录（children 为二级菜单）、页面或外链；二级菜单的 children 为空数组。
type navNodeDTO struct {
	Code     string       `json:"code"`
	Kind     string       `json:"kind"`
	Title    string       `json:"title"`
	Path     string       `json:"path"`
	Icon     string       `json:"icon"`
	Children []navNodeDTO `json:"children"`
}

func navNodeOf(m adm.Menu, children []adm.Menu) navNodeDTO {
	out := navNodeDTO{Code: m.Code, Kind: string(m.Kind), Title: m.Title, Path: m.Path, Icon: m.Icon, Children: make([]navNodeDTO, len(children))}
	for i, c := range children {
		out.Children[i] = navNodeOf(c, nil)
	}
	return out
}

type listDTO[T any] struct {
	List  []T `json:"list"`
	Total int `json:"total"`
}

func listOf[S, T any](items []S, f func(S) T) listDTO[T] {
	out := make([]T, len(items))
	for i, it := range items {
		out[i] = f(it)
	}
	return listDTO[T]{List: out, Total: len(out)}
}

type idDTO struct {
	ID int64 `json:"id"`
}

// ---------- 当前管理员的菜单 ----------

// MyMenus 是 GET /api/admin/me/menus：按权限过滤后的两级侧栏。
func (h *Handlers) MyMenus(c *gin.Context) {
	actor, _ := CurrentAdmin(c)
	nodes, err := h.Manage.MyMenus(c.Request.Context(), actor)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := make([]navNodeDTO, len(nodes))
	for i, n := range nodes {
		out[i] = navNodeOf(n.Menu, n.Children)
	}
	respond.OK(c, out)
}

// ---------- 管理员 ----------

type createAdminRequest struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=128"`
	RoleID   int64  `json:"role_id" binding:"required,min=1"`
}

type updateAdminRequest struct {
	RoleID int64  `json:"role_id" binding:"required,min=1"`
	Status string `json:"status" binding:"required,oneof=active inactive"`
}

type passwordRequest struct {
	Password string `json:"password" binding:"required,max=128"`
}

// ListAdmins 是 GET /api/admin/admins。
func (h *Handlers) ListAdmins(c *gin.Context) {
	list, err := h.Manage.ListAdmins(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, listOf(list, adminOf))
}

// CreateAdmin 是 POST /api/admin/admins。
func (h *Handlers) CreateAdmin(c *gin.Context) {
	var req createAdminRequest
	if !Bind(c, &req) {
		return
	}
	actor, _ := CurrentAdmin(c)
	id, err := h.Manage.CreateAdmin(c.Request.Context(), actor, adm.NewAccount{Username: req.Username, Password: req.Password, RoleID: req.RoleID})
	if err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin.create", TargetType: "admin", TargetID: itoa(id),
		After: gin.H{"username": adm.NormalizeUsername(req.Username), "role_id": req.RoleID}})
	respond.OK(c, idDTO{ID: id})
}

// UpdateAdmin 是 PATCH /api/admin/admins/:id：修改角色与状态。
func (h *Handlers) UpdateAdmin(c *gin.Context) {
	id, ok := PathID(c)
	var req updateAdminRequest
	if !ok || !Bind(c, &req) {
		return
	}
	actor, _ := CurrentAdmin(c)
	before, err := h.Manage.UpdateAdmin(c.Request.Context(), actor, id, adm.AdminUpdate{RoleID: req.RoleID, Status: adm.Status(req.Status)})
	if err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin.update", TargetType: "admin", TargetID: itoa(id),
		Before: gin.H{"role_id": before.Role.ID, "status": before.Status},
		After:  gin.H{"role_id": req.RoleID, "status": req.Status}})
	respond.OK(c, nil)
}

// ResetAdminPassword 是 POST /api/admin/admins/:id/password：重置其他管理员的密码并让其全部会话失效。
func (h *Handlers) ResetAdminPassword(c *gin.Context) {
	id, ok := PathID(c)
	var req passwordRequest
	if !ok || !Bind(c, &req) {
		return
	}
	actor, _ := CurrentAdmin(c)
	if err := h.Manage.ResetAdminPassword(c.Request.Context(), actor, id, req.Password); err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin.reset_password", TargetType: "admin", TargetID: itoa(id)})
	respond.OK(c, nil)
}

// RevokeAdminSessions 是 DELETE /api/admin/admins/:id/sessions：强制下线。
func (h *Handlers) RevokeAdminSessions(c *gin.Context) {
	id, ok := PathID(c)
	if !ok {
		return
	}
	actor, _ := CurrentAdmin(c)
	if err := h.Manage.RevokeAdminSessions(c.Request.Context(), actor, id); err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin.revoke_sessions", TargetType: "admin", TargetID: itoa(id)})
	respond.OK(c, nil)
}

// ResetAdminMFA 是 POST /api/admin/admins/:id/reset-mfa：清除其二次验证（TOTP + Passkey）并强制下线，
// 用于该管理员丢失认证器时由他人重置（S2-6，docs/06 第 33 节）。
func (h *Handlers) ResetAdminMFA(c *gin.Context) {
	id, ok := PathID(c)
	if !ok {
		return
	}
	actor, _ := CurrentAdmin(c)
	if err := h.Manage.ResetAdminMFA(c.Request.Context(), actor, id); err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin.reset_mfa", TargetType: "admin", TargetID: itoa(id)})
	respond.OK(c, nil)
}

// ---------- 角色与权限矩阵 ----------

type roleRequest struct {
	Code        string `json:"code" binding:"max=32"`
	Name        string `json:"name" binding:"required,max=64"`
	Description string `json:"description" binding:"max=400"`
	Status      string `json:"status" binding:"required,oneof=active inactive"`
	Sort        int32  `json:"sort"`
}

type rolePermissionsRequest struct {
	Codes []string `json:"codes" binding:"required,max=1000,dive,max=64"`
}

// ListRoles 是 GET /api/admin/roles：角色、成员数与已授予的权限点。
func (h *Handlers) ListRoles(c *gin.Context) {
	list, err := h.Manage.ListRoles(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, listOf(list, roleOf))
}

// CreateRole 是 POST /api/admin/roles。新角色没有任何权限，在权限矩阵中授予。
func (h *Handlers) CreateRole(c *gin.Context) {
	var req roleRequest
	if !Bind(c, &req) {
		return
	}
	in := adm.RoleInput{Code: req.Code, Name: req.Name, Description: req.Description, Active: req.Status == "active", Sort: req.Sort}
	id, err := h.Manage.CreateRole(c.Request.Context(), in)
	if err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "role.create", TargetType: "role", TargetID: itoa(id), After: req})
	respond.OK(c, idDTO{ID: id})
}

// UpdateRole 是 PATCH /api/admin/roles/:id：名称、说明、状态与排序（编码不能改）。
func (h *Handlers) UpdateRole(c *gin.Context) {
	id, ok := PathID(c)
	var req roleRequest
	if !ok || !Bind(c, &req) {
		return
	}
	in := adm.RoleInput{Name: req.Name, Description: req.Description, Active: req.Status == "active", Sort: req.Sort}
	before, err := h.Manage.UpdateRole(c.Request.Context(), id, in)
	if err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "role.update", TargetType: "role", TargetID: itoa(id),
		Before: gin.H{"name": before.Name, "description": before.Description, "active": before.Active, "sort": before.Sort},
		After:  gin.H{"name": in.Name, "description": in.Description, "active": in.Active, "sort": in.Sort}})
	respond.OK(c, nil)
}

// DeleteRole 是 DELETE /api/admin/roles/:id：只删除没有成员的自定义角色。
func (h *Handlers) DeleteRole(c *gin.Context) {
	id, ok := PathID(c)
	if !ok {
		return
	}
	before, err := h.Manage.DeleteRole(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "role.delete", TargetType: "role", TargetID: itoa(id), Before: roleOf(before)})
	respond.OK(c, nil)
}

// SetRolePermissions 是 PUT /api/admin/roles/:id/permissions：整体替换角色的权限点。
func (h *Handlers) SetRolePermissions(c *gin.Context) {
	id, ok := PathID(c)
	var req rolePermissionsRequest
	if !ok || !Bind(c, &req) {
		return
	}
	actor, _ := CurrentAdmin(c)
	before, after, err := h.Manage.SetRolePermissions(c.Request.Context(), actor, id, req.Codes)
	if err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "role.set_permissions", TargetType: "role", TargetID: itoa(id),
		Before: gin.H{"permissions": before}, After: gin.H{"permissions": after}})
	respond.OK(c, gin.H{"permissions": after})
}

// PermissionTree 是 GET /api/admin/permissions：按菜单结构（一级 → 二级）组织的权限树。
func (h *Handlers) PermissionTree(c *gin.Context) {
	tree, err := h.Manage.PermissionTree(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := make([]permissionNodeDTO, len(tree))
	for i, n := range tree {
		out[i] = permissionNodeOf(n)
	}
	respond.OK(c, out)
}

// ---------- 后台菜单 ----------

type menuRequest struct {
	Kind     string `json:"kind" binding:"omitempty,oneof=group link"`
	ParentID *int64 `json:"parent_id" binding:"omitempty,min=1"`
	Title    string `json:"title" binding:"required,max=64"`
	Path     string `json:"path" binding:"max=500"`
	Icon     string `json:"icon" binding:"max=40"`
	Sort     int32  `json:"sort"`
	Visible  *bool  `json:"visible" binding:"required"`
}

func (r menuRequest) input() adm.MenuInput {
	return adm.MenuInput{Kind: adm.MenuKind(r.Kind), ParentID: r.ParentID, Title: r.Title, Path: r.Path, Icon: r.Icon, Sort: r.Sort, Visible: *r.Visible}
}

// ListMenus 是 GET /api/admin/admin-menus：全部后台菜单（含隐藏的）。
func (h *Handlers) ListMenus(c *gin.Context) {
	list, err := h.Manage.ListMenus(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, listOf(list, menuOf))
}

// CreateMenu 是 POST /api/admin/admin-menus：新建目录或外链（外链可以是一级菜单，也可以挂在目录下）。
func (h *Handlers) CreateMenu(c *gin.Context) {
	var req menuRequest
	if !Bind(c, &req) {
		return
	}
	if req.Kind == "" {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请选择菜单类型"))
		return
	}
	id, err := h.Manage.CreateMenu(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin_menu.create", TargetType: "admin_menu", TargetID: itoa(id), After: req})
	respond.OK(c, idDTO{ID: id})
}

// UpdateMenu 是 PATCH /api/admin/admin-menus/:id：名称、图标、排序、显示与上级目录（页面路径不能改）。
func (h *Handlers) UpdateMenu(c *gin.Context) {
	id, ok := PathID(c)
	var req menuRequest
	if !ok || !Bind(c, &req) {
		return
	}
	before, err := h.Manage.UpdateMenu(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin_menu.update", TargetType: "admin_menu", TargetID: itoa(id), Before: menuOf(before), After: req})
	respond.OK(c, nil)
}

// DeleteMenu 是 DELETE /api/admin/admin-menus/:id：只删除自定义的目录（需为空）或外链。
func (h *Handlers) DeleteMenu(c *gin.Context) {
	id, ok := PathID(c)
	if !ok {
		return
	}
	before, err := h.Manage.DeleteMenu(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin_menu.delete", TargetType: "admin_menu", TargetID: itoa(id), Before: menuOf(before)})
	respond.OK(c, nil)
}

// ---------- 工具（业务模块的 handler 共用） ----------

// Bind 解析 JSON 请求体（拒绝未知字段）；失败时已写出 400。
func Bind(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		respond.Error(c, errs.ErrBadRequest)
		return false
	}
	return true
}

// PathID 解析路径中的 :id；失败时已写出 404。
func PathID(c *gin.Context) (int64, bool) { return ParamID(c, "id") }

// ParamID 解析路径参数中的正整数 ID（如 :term_id）；失败时已写出 404。
func ParamID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(c, errs.ErrNotFound)
		return 0, false
	}
	return id, true
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }
