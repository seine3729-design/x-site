-- +goose Up
-- 表情管理页（S2-5，docs/06「表情/表情包管理」）：加到「社区」组，供 /api/admin/emojis 维护、前台 /api/v1/emojis 下发。
-- 权限点与内置角色授权沿用 00003 的规矩：每页「查看」+ 有写则「修改」；社区组由 operator / moderator 可改、readonly 只读、
-- 超管（is_super）自动拥有含以后新增的全部权限、无需显式授权。表 emoji / emoji_pack 已在 00034 建好。

INSERT INTO admin_menu (parent_id, kind, code, title, path, icon, sort, builtin)
SELECT g.id, 'page', 'emojis', '表情管理', '/emojis', 'Smile', 50, true
FROM admin_menu g WHERE g.code = 'community';

INSERT INTO admin_permission (code, menu_id, name, sensitive, sort)
SELECT 'emojis.view', id, '查看', false, 0 FROM admin_menu WHERE code = 'emojis';
INSERT INTO admin_permission (code, menu_id, name, sensitive, sort)
SELECT 'emojis.edit', id, '修改', false, 1 FROM admin_menu WHERE code = 'emojis';

-- 不做内置角色授权：00005 已把 operator/moderator/readonly 等非超管内置角色删除或转为自定义角色，
-- 新部署初始只有 superadmin（is_super 自动拥有含以后新增在内的全部权限）。自定义角色由运营在「权限矩阵」按需授予
-- emojis.view / emojis.edit（与 resource_warm 等 00005 之后新增页面的处理一致）。

-- +goose Down
-- 删菜单页即级联删其权限点（admin_permission.menu_id ON DELETE CASCADE），进而级联删角色授权
-- （admin_role_permission.permission_code ON DELETE CASCADE）。
DELETE FROM admin_menu WHERE code = 'emojis';
