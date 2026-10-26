-- +goose Up
-- 后台菜单改为一级 / 二级（docs/adr/0006）：一级菜单可以是目录、页面或外链，二级菜单是目录下的页面或外链。
-- 内置角色只保留超级管理员，其余角色由管理员按需新建并授权。

-- 目录只能是一级菜单；页面与外链既可以是一级菜单，也可以挂在目录下（「上级必须是目录」由业务层校验）
ALTER TABLE admin_menu DROP CONSTRAINT admin_menu_check;
ALTER TABLE admin_menu ADD CONSTRAINT admin_menu_group_top_level CHECK (kind <> 'group' OR parent_id IS NULL);

-- 数据看板、剧集分析提为一级菜单，排在最前；原「概览」目录没有子菜单后删除
UPDATE admin_menu
SET parent_id = NULL, sort = CASE code WHEN 'dashboard' THEN 10 ELSE 20 END, updated_at = now()
WHERE code IN ('dashboard', 'analytics_dramas') AND builtin;

DELETE FROM admin_menu g
WHERE g.code = 'overview' AND g.builtin AND NOT EXISTS (SELECT 1 FROM admin_menu c WHERE c.parent_id = g.id);

-- 其余内置目录整体后移，相对顺序不变
UPDATE admin_menu SET sort = sort + 10, updated_at = now()
WHERE kind = 'group' AND builtin
  AND code IN ('content', 'framework', 'whitelabel', 'operations', 'community', 'system', 'access');

-- 目录现在是侧栏中的一级菜单，补上默认图标（没设置过图标时才补）
UPDATE admin_menu m SET icon = v.icon, updated_at = now()
FROM (VALUES ('content', 'Film'), ('framework', 'Layers'), ('whitelabel', 'Package'), ('operations', 'Rocket'),
             ('community', 'MessageCircle'), ('system', 'Settings'), ('access', 'Shield')) AS v (code, icon)
WHERE m.code = v.code AND m.kind = 'group' AND m.builtin AND m.icon = '';

-- 角色授权页改为树形展示，不再是矩阵（名称与图标未被改过时才改）
UPDATE admin_menu SET title = '角色权限', updated_at = now()
WHERE code = 'role_permissions' AND title = '权限矩阵';
UPDATE admin_menu SET icon = 'Lock', updated_at = now()
WHERE code = 'role_permissions' AND icon = 'Grid3x3';

-- 只保留超级管理员一个内置角色：没有成员的其余内置角色删除（授权随之级联删除）；
-- 已有成员的转为自定义角色，权限不变，之后可以在后台修改或删除
DELETE FROM admin_role r
WHERE r.builtin AND NOT r.is_super AND NOT EXISTS (SELECT 1 FROM admin_user u WHERE u.role_id = r.id);

UPDATE admin_role SET builtin = false, updated_at = now() WHERE builtin AND NOT is_super;

-- +goose Down
-- 菜单：恢复「概览」目录并把两个页面挂回去；Up 之后新增的一级页面或外链也挂到「概览」下，满足旧约束
INSERT INTO admin_menu (kind, code, title, sort, builtin) VALUES ('group', 'overview', '概览', 10, true)
ON CONFLICT (code) DO NOTHING;

UPDATE admin_menu SET sort = sort - 10, updated_at = now()
WHERE kind = 'group' AND builtin
  AND code IN ('content', 'framework', 'whitelabel', 'operations', 'community', 'system', 'access');

UPDATE admin_menu
SET parent_id = (SELECT id FROM admin_menu WHERE code = 'overview'),
    sort = CASE code WHEN 'dashboard' THEN 10 WHEN 'analytics_dramas' THEN 20 ELSE sort END,
    updated_at = now()
WHERE kind <> 'group' AND parent_id IS NULL;

UPDATE admin_menu SET title = '权限矩阵', updated_at = now()
WHERE code = 'role_permissions' AND title = '角色权限';
UPDATE admin_menu SET icon = 'Grid3x3', updated_at = now()
WHERE code = 'role_permissions' AND icon = 'Lock';

-- 目录回到旧版的「侧栏分组标题」，不显示图标
UPDATE admin_menu m SET icon = '', updated_at = now()
FROM (VALUES ('content', 'Film'), ('framework', 'Layers'), ('whitelabel', 'Package'), ('operations', 'Rocket'),
             ('community', 'MessageCircle'), ('system', 'Settings'), ('access', 'Shield')) AS v (code, icon)
WHERE m.code = v.code AND m.kind = 'group' AND m.builtin AND m.icon = v.icon;

ALTER TABLE admin_menu DROP CONSTRAINT admin_menu_group_top_level;
ALTER TABLE admin_menu ADD CONSTRAINT admin_menu_check CHECK ((kind = 'group') = (parent_id IS NULL));

-- 角色：仍在的同编码角色改回内置（权限不动）；已删除的重新创建，并恢复 00003 的默认授权
UPDATE admin_role SET builtin = true, updated_at = now()
WHERE code IN ('operator', 'moderator', 'readonly') AND NOT builtin;

WITH restored AS (
    INSERT INTO admin_role (code, name, description, builtin, sort) VALUES
        ('operator',  '运营',   '内容、框架、白标、运营、社区可改；系统与权限只读', true, 10),
        ('moderator', '审核员', '社区可改，其余只读', true, 20),
        ('readonly',  '只读',   '全部只读', true, 30)
    ON CONFLICT (code) DO NOTHING
    RETURNING id, code
)
INSERT INTO admin_role_permission (role_id, permission_code)
SELECT r.id, p.code
FROM restored r JOIN admin_permission p ON p.code LIKE '%.view'
UNION ALL
SELECT r.id, p.code
FROM restored r
JOIN admin_permission p ON p.code LIKE '%.edit'
JOIN admin_menu page ON page.id = p.menu_id
JOIN admin_menu grp ON grp.id = page.parent_id
WHERE (r.code = 'operator' AND grp.code IN ('overview', 'content', 'framework', 'whitelabel', 'operations', 'community'))
   OR (r.code = 'moderator' AND grp.code = 'community');
