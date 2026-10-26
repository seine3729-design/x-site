-- ---------- 登录与会话 ----------

-- name: GetAdminAuthByUsername :one
-- 登录用：账号、所属角色与权限点。has_totp / has_passkey 供登录第二步决定可用的二次验证方式。
SELECT u.id, u.username, u.password_hash, u.status, u.session_version,
       (u.totp_secret_enc IS NOT NULL)::boolean AS has_totp,
       EXISTS (SELECT 1 FROM admin_passkey p WHERE p.admin_id = u.id)::boolean AS has_passkey,
       r.id AS role_id, r.code AS role_code, r.name AS role_name, r.is_super, r.status AS role_status,
       coalesce(array_agg(rp.permission_code ORDER BY rp.permission_code) FILTER (WHERE rp.permission_code IS NOT NULL), '{}')::text[] AS permissions
FROM admin_user u
JOIN admin_role r ON r.id = u.role_id
LEFT JOIN admin_role_permission rp ON rp.role_id = r.id
WHERE u.username = $1
GROUP BY u.id, r.id;

-- name: GetAdminAuthByID :one
-- 每个后台请求读一次：账号、角色与角色的全部权限点（超级角色不存权限行，由 is_super 表示全部）。
SELECT u.id, u.username, u.password_hash, u.status, u.session_version,
       (u.totp_secret_enc IS NOT NULL OR EXISTS (SELECT 1 FROM admin_passkey p WHERE p.admin_id = u.id))::boolean AS mfa_enabled,
       r.id AS role_id, r.code AS role_code, r.name AS role_name, r.is_super, r.status AS role_status,
       coalesce(array_agg(rp.permission_code ORDER BY rp.permission_code) FILTER (WHERE rp.permission_code IS NOT NULL), '{}')::text[] AS permissions
FROM admin_user u
JOIN admin_role r ON r.id = u.role_id
LEFT JOIN admin_role_permission rp ON rp.role_id = r.id
WHERE u.id = $1
GROUP BY u.id, r.id;

-- name: MarkAdminLogin :exec
-- 记录登录时间；从未登录过（unused）的账号首次登录后变为 active。
UPDATE admin_user
SET last_login_at = now(),
    status = CASE WHEN status = 'unused' THEN 'active' ELSE status END,
    updated_at = now()
WHERE id = $1;

-- name: UpdateAdminPasswordHash :exec
-- 参数调整后的透明重哈希，不吊销会话。
UPDATE admin_user SET password_hash = $2, updated_at = now() WHERE id = $1;

-- name: InsertAdminLoginLog :exec
INSERT INTO admin_login_log (admin_id, username_tried, result, ip_prefix, ua_digest)
VALUES ($1, $2, $3, $4, $5);

-- name: InsertAuditLog :exec
INSERT INTO audit_log (admin_id, action, target_type, target_id, diff_before, diff_after, ip_prefix, request_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- ---------- 管理员 ----------

-- name: ListAdmins :many
SELECT u.id, u.username, u.status, u.last_login_at, u.created_at,
       (u.totp_secret_enc IS NOT NULL OR EXISTS (SELECT 1 FROM admin_passkey p WHERE p.admin_id = u.id))::boolean AS mfa_enabled,
       r.id AS role_id, r.code AS role_code, r.name AS role_name, r.is_super
FROM admin_user u
JOIN admin_role r ON r.id = u.role_id
ORDER BY u.id;

-- name: GetAdmin :one
SELECT u.id, u.username, u.status, u.last_login_at, u.created_at,
       (u.totp_secret_enc IS NOT NULL OR EXISTS (SELECT 1 FROM admin_passkey p WHERE p.admin_id = u.id))::boolean AS mfa_enabled,
       r.id AS role_id, r.code AS role_code, r.name AS role_name, r.is_super
FROM admin_user u
JOIN admin_role r ON r.id = u.role_id
WHERE u.id = $1;

-- name: CreateAdmin :one
INSERT INTO admin_user (username, password_hash, role_id)
VALUES ($1, $2, $3)
RETURNING id;

-- name: UpdateAdminRoleStatus :exec
UPDATE admin_user SET role_id = $2, status = $3, updated_at = now() WHERE id = $1;

-- name: ResetAdminPassword :exec
-- 由其他管理员重置密码：同时让该管理员的全部会话失效。
UPDATE admin_user
SET password_hash = $2, session_version = session_version + 1, updated_at = now()
WHERE id = $1;

-- name: RevokeAdminSessions :exec
UPDATE admin_user SET session_version = session_version + 1, updated_at = now() WHERE id = $1;

-- name: LockSuperAdminGuard :exec
-- 修改管理员角色或状态的事务内先加锁，串行化「至少保留一个可用的超级管理员」检查。
SELECT pg_advisory_xact_lock(hashtext('xsite.admin.super_guard'));

-- name: CountUsableSuperAdmins :one
SELECT count(*)
FROM admin_user u
JOIN admin_role r ON r.id = u.role_id
WHERE r.is_super AND u.status <> 'inactive';

-- ---------- 角色 ----------

-- name: ListRoles :many
SELECT r.id, r.code, r.name, r.description, r.is_super, r.builtin, r.status, r.sort,
       (SELECT count(*) FROM admin_user u WHERE u.role_id = r.id) AS member_count,
       coalesce((SELECT array_agg(rp.permission_code ORDER BY rp.permission_code)
                 FROM admin_role_permission rp WHERE rp.role_id = r.id), '{}')::text[] AS permissions
FROM admin_role r
ORDER BY r.sort, r.id;

-- name: GetRole :one
SELECT r.id, r.code, r.name, r.description, r.is_super, r.builtin, r.status, r.sort,
       (SELECT count(*) FROM admin_user u WHERE u.role_id = r.id) AS member_count,
       coalesce((SELECT array_agg(rp.permission_code ORDER BY rp.permission_code)
                 FROM admin_role_permission rp WHERE rp.role_id = r.id), '{}')::text[] AS permissions
FROM admin_role r
WHERE r.id = $1;

-- name: GetRoleIDByCode :one
SELECT id FROM admin_role WHERE code = $1;

-- name: CreateRole :one
INSERT INTO admin_role (code, name, description, status, sort)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: UpdateRole :exec
UPDATE admin_role SET name = $2, description = $3, status = $4, sort = $5, updated_at = now() WHERE id = $1;

-- name: DeleteRole :execrows
-- 只删除没有成员的自定义角色。
DELETE FROM admin_role r
WHERE r.id = $1 AND NOT r.builtin AND NOT EXISTS (SELECT 1 FROM admin_user u WHERE u.role_id = r.id);

-- name: DeleteRolePermissions :exec
DELETE FROM admin_role_permission WHERE role_id = $1;

-- name: InsertRolePermissions :exec
INSERT INTO admin_role_permission (role_id, permission_code)
SELECT $1, unnest(sqlc.arg(codes)::text[]);

-- name: TouchRole :exec
UPDATE admin_role SET updated_at = now() WHERE id = $1;

-- ---------- 权限点与菜单 ----------

-- name: ListPermissions :many
SELECT code, menu_id, name, sensitive, sort FROM admin_permission ORDER BY menu_id, sort, code;

-- name: ListMenus :many
SELECT id, parent_id, kind, code, title, path, icon, sort, visible, builtin
FROM admin_menu
ORDER BY sort, id;

-- name: GetMenu :one
SELECT id, parent_id, kind, code, title, path, icon, sort, visible, builtin
FROM admin_menu
WHERE id = $1;

-- name: CreateMenu :one
INSERT INTO admin_menu (parent_id, kind, code, title, path, icon, sort, visible)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id;

-- name: UpdateMenu :exec
UPDATE admin_menu
SET parent_id = $2, title = $3, path = $4, icon = $5, sort = $6, visible = $7, updated_at = now()
WHERE id = $1;

-- name: DeleteMenu :execrows
-- 只删除自定义菜单；分组下还有菜单时不删除。
DELETE FROM admin_menu m
WHERE m.id = $1 AND NOT m.builtin AND NOT EXISTS (SELECT 1 FROM admin_menu c WHERE c.parent_id = m.id);

-- ---------- 二次验证 TOTP（S2-6，docs/06 第 33 节）----------

-- name: SetAdminTOTP :exec
-- 绑定 / 换绑 TOTP：保存信封加密后的种子（totp_secret_enc）。
UPDATE admin_user SET totp_secret_enc = @totp_secret_enc, updated_at = now() WHERE id = @id;

-- name: GetAdminTOTPSecret :one
-- 登录 / reauth 校验 TOTP 时读加密种子；未绑定时为 NULL。
SELECT totp_secret_enc FROM admin_user WHERE id = $1;

-- name: ClearAdminTOTP :exec
-- 解绑 TOTP（本人在已开启其它因子时，或超管重置他人二次验证）。
UPDATE admin_user SET totp_secret_enc = NULL, updated_at = now() WHERE id = @id;

-- ---------- 二次验证 Passkey / WebAuthn（S2-6）----------

-- name: InsertAdminPasskey :one
-- 注册一把 Passkey（自助绑定第二步校验通过后写入）。
INSERT INTO admin_passkey (admin_id, credential_id, public_key, sign_count, transports, name)
VALUES (@admin_id, @credential_id, @public_key, @sign_count, @transports, @name)
RETURNING id, created_at;

-- name: ListAdminPasskeys :many
-- 列出某管理员的全部 Passkey：登录断言组装 WebAuthn 凭据、自助管理列表都用。
SELECT id, credential_id, public_key, sign_count, transports, name, created_at, last_used_at
FROM admin_passkey WHERE admin_id = $1 ORDER BY id;

-- name: UpdateAdminPasskeyOnLogin :exec
-- 断言校验通过后写回计数（防重放）与最近使用时间。
UPDATE admin_passkey SET sign_count = @sign_count, last_used_at = now() WHERE credential_id = @credential_id;

-- name: DeleteAdminPasskey :execrows
-- 自助删除本人的一把 Passkey（限定 admin_id，防越权删他人）。
DELETE FROM admin_passkey WHERE id = @id AND admin_id = @admin_id;

-- name: CountAdminPasskeys :one
SELECT count(*) FROM admin_passkey WHERE admin_id = $1;

-- name: DeleteAdminPasskeysByAdmin :exec
-- 重置某管理员的二次验证时清空其全部 Passkey（超管操作，S2-6 切片 6）。
DELETE FROM admin_passkey WHERE admin_id = @admin_id;
