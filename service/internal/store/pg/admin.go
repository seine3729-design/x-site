package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/admin"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// AdminRepo 实现 admin.AccountRepo 与 admin.ManageRepo。
type AdminRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewAdminRepo(pool *pgxpool.Pool) *AdminRepo {
	return &AdminRepo{pool: pool, q: sqlcgen.New(pool)}
}

// ---------- 登录与会话（admin.AccountRepo） ----------

func (r *AdminRepo) GetByUsername(ctx context.Context, username string) (admin.Account, error) {
	row, err := r.q.GetAdminAuthByUsername(ctx, username)
	if err != nil {
		return admin.Account{}, notFound(err, "读取管理员")
	}
	return admin.Account{
		ID: row.ID, Username: row.Username, PasswordHash: row.PasswordHash, Status: admin.Status(row.Status),
		SessionVersion: row.SessionVersion, MFAEnabled: row.HasTotp || row.HasPasskey,
		HasTOTP: row.HasTotp, HasPasskey: row.HasPasskey, Permissions: row.Permissions,
		Role: admin.RoleRef{ID: row.RoleID, Code: row.RoleCode, Name: row.RoleName, IsSuper: row.IsSuper, Active: row.RoleStatus == "active"},
	}, nil
}

func (r *AdminRepo) GetByID(ctx context.Context, id int64) (admin.Account, error) {
	row, err := r.q.GetAdminAuthByID(ctx, id)
	if err != nil {
		return admin.Account{}, notFound(err, "读取管理员")
	}
	return admin.Account{
		ID: row.ID, Username: row.Username, PasswordHash: row.PasswordHash, Status: admin.Status(row.Status),
		SessionVersion: row.SessionVersion, MFAEnabled: row.MfaEnabled, Permissions: row.Permissions,
		Role: admin.RoleRef{ID: row.RoleID, Code: row.RoleCode, Name: row.RoleName, IsSuper: row.IsSuper, Active: row.RoleStatus == "active"},
	}, nil
}

func (r *AdminRepo) MarkLogin(ctx context.Context, id int64) error {
	if err := r.q.MarkAdminLogin(ctx, id); err != nil {
		return fmt.Errorf("记录管理员登录: %w", err)
	}
	return nil
}

func (r *AdminRepo) UpdatePasswordHash(ctx context.Context, id int64, hash string) error {
	if err := r.q.UpdateAdminPasswordHash(ctx, sqlcgen.UpdateAdminPasswordHashParams{ID: id, PasswordHash: hash}); err != nil {
		return fmt.Errorf("更新管理员密码哈希: %w", err)
	}
	return nil
}

func (r *AdminRepo) InsertLoginLog(ctx context.Context, l admin.LoginLog) error {
	var adminID pgtype.Int8
	if l.AdminID != nil {
		adminID = pgtype.Int8{Int64: *l.AdminID, Valid: true}
	}
	err := r.q.InsertAdminLoginLog(ctx, sqlcgen.InsertAdminLoginLogParams{
		AdminID: adminID, UsernameTried: l.UsernameTried, Result: string(l.Result),
		IpPrefix: l.IPPrefix, UaDigest: l.UADigest,
	})
	if err != nil {
		return fmt.Errorf("写入后台登录记录: %w", err)
	}
	return nil
}

// ---------- 管理员（admin.ManageRepo） ----------

func (r *AdminRepo) ListAdmins(ctx context.Context) ([]admin.AdminSummary, error) {
	rows, err := r.q.ListAdmins(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取管理员列表: %w", err)
	}
	out := make([]admin.AdminSummary, len(rows))
	for i, row := range rows {
		out[i] = admin.AdminSummary{
			ID: row.ID, Username: row.Username, Status: admin.Status(row.Status), MFAEnabled: row.MfaEnabled,
			Role:        admin.RoleRef{ID: row.RoleID, Code: row.RoleCode, Name: row.RoleName, IsSuper: row.IsSuper},
			LastLoginAt: optTime(row.LastLoginAt), CreatedAt: row.CreatedAt.Time,
		}
	}
	return out, nil
}

func (r *AdminRepo) GetAdmin(ctx context.Context, id int64) (admin.AdminSummary, error) {
	row, err := r.q.GetAdmin(ctx, id)
	if err != nil {
		return admin.AdminSummary{}, notFound(err, "读取管理员")
	}
	return admin.AdminSummary{
		ID: row.ID, Username: row.Username, Status: admin.Status(row.Status), MFAEnabled: row.MfaEnabled,
		Role:        admin.RoleRef{ID: row.RoleID, Code: row.RoleCode, Name: row.RoleName, IsSuper: row.IsSuper},
		LastLoginAt: optTime(row.LastLoginAt), CreatedAt: row.CreatedAt.Time,
	}, nil
}

func (r *AdminRepo) CreateAdmin(ctx context.Context, username, passwordHash string, roleID int64) (int64, error) {
	id, err := r.q.CreateAdmin(ctx, sqlcgen.CreateAdminParams{Username: username, PasswordHash: passwordHash, RoleID: roleID})
	if isUniqueViolation(err) {
		return 0, errs.ErrConflict.WithMessage("用户名已存在")
	}
	if err != nil {
		return 0, fmt.Errorf("创建管理员: %w", err)
	}
	return id, nil
}

// UpdateAdminRoleStatus 在事务内修改；用咨询锁串行化，修改后若没有可用的超级管理员则回滚。
func (r *AdminRepo) UpdateAdminRoleStatus(ctx context.Context, id, roleID int64, status admin.Status) error {
	return InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockSuperAdminGuard(ctx); err != nil {
			return fmt.Errorf("加锁: %w", err)
		}
		if err := q.UpdateAdminRoleStatus(ctx, sqlcgen.UpdateAdminRoleStatusParams{ID: id, RoleID: roleID, Status: string(status)}); err != nil {
			return fmt.Errorf("修改管理员: %w", err)
		}
		n, err := q.CountUsableSuperAdmins(ctx)
		if err != nil {
			return fmt.Errorf("统计超级管理员: %w", err)
		}
		if n == 0 {
			return admin.ErrLastSuperAdmin
		}
		return nil
	})
}

func (r *AdminRepo) ResetAdminPassword(ctx context.Context, id int64, passwordHash string) error {
	if err := r.q.ResetAdminPassword(ctx, sqlcgen.ResetAdminPasswordParams{ID: id, PasswordHash: passwordHash}); err != nil {
		return fmt.Errorf("重置管理员密码: %w", err)
	}
	return nil
}

func (r *AdminRepo) RevokeAdminSessions(ctx context.Context, id int64) error {
	if err := r.q.RevokeAdminSessions(ctx, id); err != nil {
		return fmt.Errorf("吊销管理员会话: %w", err)
	}
	return nil
}

// ---------- 角色与权限点 ----------

func roleInfo(id int64, code, name, desc string, isSuper, builtin bool, status string, sort int32, members int64, perms []string) admin.RoleInfo {
	return admin.RoleInfo{
		ID: id, Code: code, Name: name, Description: desc, IsSuper: isSuper, Builtin: builtin,
		Active: status == "active", Sort: sort, MemberCount: members, Permissions: perms,
	}
}

func (r *AdminRepo) ListRoles(ctx context.Context) ([]admin.RoleInfo, error) {
	rows, err := r.q.ListRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取角色列表: %w", err)
	}
	out := make([]admin.RoleInfo, len(rows))
	for i, x := range rows {
		out[i] = roleInfo(x.ID, x.Code, x.Name, x.Description, x.IsSuper, x.Builtin, x.Status, x.Sort, x.MemberCount, x.Permissions)
	}
	return out, nil
}

func (r *AdminRepo) GetRole(ctx context.Context, id int64) (admin.RoleInfo, error) {
	x, err := r.q.GetRole(ctx, id)
	if err != nil {
		return admin.RoleInfo{}, notFound(err, "读取角色")
	}
	return roleInfo(x.ID, x.Code, x.Name, x.Description, x.IsSuper, x.Builtin, x.Status, x.Sort, x.MemberCount, x.Permissions), nil
}

func (r *AdminRepo) GetRoleIDByCode(ctx context.Context, code string) (int64, error) {
	id, err := r.q.GetRoleIDByCode(ctx, code)
	if err != nil {
		return 0, notFound(err, "读取角色")
	}
	return id, nil
}

func (r *AdminRepo) CreateRole(ctx context.Context, in admin.RoleInput) (int64, error) {
	id, err := r.q.CreateRole(ctx, sqlcgen.CreateRoleParams{
		Code: in.Code, Name: in.Name, Description: in.Description, Status: roleStatus(in.Active), Sort: in.Sort,
	})
	if isUniqueViolation(err) {
		return 0, errs.ErrConflict.WithMessage("角色编码已存在")
	}
	if err != nil {
		return 0, fmt.Errorf("创建角色: %w", err)
	}
	return id, nil
}

func (r *AdminRepo) UpdateRole(ctx context.Context, id int64, in admin.RoleInput) error {
	err := r.q.UpdateRole(ctx, sqlcgen.UpdateRoleParams{
		ID: id, Name: in.Name, Description: in.Description, Status: roleStatus(in.Active), Sort: in.Sort,
	})
	if err != nil {
		return fmt.Errorf("修改角色: %w", err)
	}
	return nil
}

// roleStatus 把领域层的启用标记转为 admin_role.status 的取值。
func roleStatus(active bool) string {
	if active {
		return "active"
	}
	return "inactive"
}

func (r *AdminRepo) DeleteRole(ctx context.Context, id int64) (bool, error) {
	n, err := r.q.DeleteRole(ctx, id)
	if err != nil {
		return false, fmt.Errorf("删除角色: %w", err)
	}
	return n > 0, nil
}

func (r *AdminRepo) ReplaceRolePermissions(ctx context.Context, roleID int64, codes []string) error {
	return InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.DeleteRolePermissions(ctx, roleID); err != nil {
			return fmt.Errorf("清除角色权限: %w", err)
		}
		if err := q.InsertRolePermissions(ctx, sqlcgen.InsertRolePermissionsParams{RoleID: roleID, Codes: codes}); err != nil {
			return fmt.Errorf("写入角色权限: %w", err)
		}
		if err := q.TouchRole(ctx, roleID); err != nil {
			return fmt.Errorf("更新角色时间: %w", err)
		}
		return nil
	})
}

func (r *AdminRepo) ListPermissions(ctx context.Context) ([]admin.Permission, error) {
	rows, err := r.q.ListPermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取权限点: %w", err)
	}
	out := make([]admin.Permission, len(rows))
	for i, p := range rows {
		out[i] = admin.Permission{Code: p.Code, MenuID: p.MenuID, Name: p.Name, Sensitive: p.Sensitive, Sort: p.Sort}
	}
	return out, nil
}

// ---------- 后台菜单 ----------

func menuOf(id int64, parent pgtype.Int8, kind, code, title, path, icon string, sort int32, visible, builtin bool) admin.Menu {
	m := admin.Menu{
		ID: id, Kind: admin.MenuKind(kind), Code: code, Title: title, Path: path, Icon: icon,
		Sort: sort, Visible: visible, Builtin: builtin,
	}
	if parent.Valid {
		p := parent.Int64
		m.ParentID = &p
	}
	return m
}

func optInt8(p *int64) pgtype.Int8 {
	if p == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *p, Valid: true}
}

func (r *AdminRepo) ListMenus(ctx context.Context) ([]admin.Menu, error) {
	rows, err := r.q.ListMenus(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取后台菜单: %w", err)
	}
	out := make([]admin.Menu, len(rows))
	for i, x := range rows {
		out[i] = menuOf(x.ID, x.ParentID, x.Kind, x.Code, x.Title, x.Path, x.Icon, x.Sort, x.Visible, x.Builtin)
	}
	return out, nil
}

func (r *AdminRepo) GetMenu(ctx context.Context, id int64) (admin.Menu, error) {
	x, err := r.q.GetMenu(ctx, id)
	if err != nil {
		return admin.Menu{}, notFound(err, "读取后台菜单")
	}
	return menuOf(x.ID, x.ParentID, x.Kind, x.Code, x.Title, x.Path, x.Icon, x.Sort, x.Visible, x.Builtin), nil
}

func (r *AdminRepo) CreateMenu(ctx context.Context, m admin.Menu) (int64, error) {
	id, err := r.q.CreateMenu(ctx, sqlcgen.CreateMenuParams{
		ParentID: optInt8(m.ParentID), Kind: string(m.Kind), Code: m.Code, Title: m.Title,
		Path: m.Path, Icon: m.Icon, Sort: m.Sort, Visible: m.Visible,
	})
	if err != nil {
		return 0, fmt.Errorf("创建后台菜单: %w", err)
	}
	return id, nil
}

func (r *AdminRepo) UpdateMenu(ctx context.Context, m admin.Menu) error {
	err := r.q.UpdateMenu(ctx, sqlcgen.UpdateMenuParams{
		ID: m.ID, ParentID: optInt8(m.ParentID), Title: m.Title, Path: m.Path, Icon: m.Icon, Sort: m.Sort, Visible: m.Visible,
	})
	if err != nil {
		return fmt.Errorf("修改后台菜单: %w", err)
	}
	return nil
}

func (r *AdminRepo) DeleteMenu(ctx context.Context, id int64) (bool, error) {
	n, err := r.q.DeleteMenu(ctx, id)
	if err != nil {
		return false, fmt.Errorf("删除后台菜单: %w", err)
	}
	return n > 0, nil
}

// ---------- 审计 ----------

// AuditRepo 实现 platform.AuditRepo。
type AuditRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewAuditRepo(pool *pgxpool.Pool) *AuditRepo {
	return &AuditRepo{pool: pool, q: sqlcgen.New(pool)}
}

// InsertAudit 写入一条审计记录，并接上哈希链（S4-7）。
//
// 「读链尾 → 算哈希 → 插入」必须是原子的，否则两条记录会指向同一个前驱、链就分叉了。
// 单个进程内 AuditRecorder 已经串行，这里的事务级 advisory lock 防的是多实例并发写。
// 审计是异步写入的（请求已经返回），所以这把锁不会拖慢任何请求。
func (r *AuditRepo) InsertAudit(ctx context.Context, e platform.AuditEntry) error {
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockAuditChain(ctx); err != nil {
			return fmt.Errorf("锁定审计链: %w", err)
		}
		prev, err := q.LastAuditHash(ctx)
		if err != nil {
			return fmt.Errorf("读取审计链尾: %w", err)
		}
		_, err = q.InsertAuditLogChained(ctx, sqlcgen.InsertAuditLogChainedParams{
			AdminID: e.AdminID, Action: e.Action, TargetType: e.TargetType, TargetID: e.TargetID,
			DiffBefore: e.Before, DiffAfter: e.After, IpPrefix: e.IPPrefix, RequestID: e.RequestID,
			CreatedAt: pgtype.Timestamptz{Time: e.CreatedAt, Valid: true},
			PrevHash:  prev, Hash: platform.AuditHash(prev, e),
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("写入审计日志: %w", err)
	}
	return nil
}

func optTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func notFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
