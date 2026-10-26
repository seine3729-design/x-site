-- +goose Up
-- 删除演员模块（S1-3a）：产品不需要演员。去掉演员表与剧的演员表、后台「演员」页面及其权限点、站点「演员」功能开关。
-- 已录入的演员与演员表随表删除；回滚只恢复结构、菜单与权限点，不恢复数据和各角色对该页面的授权。

DROP TABLE drama_actor;
DROP TABLE actor;

-- 权限点随页面级联删除（admin_permission.menu_id），角色授权随权限点级联删除（admin_role_permission.permission_code）
DELETE FROM admin_menu WHERE code = 'actors' AND kind = 'page';

-- version + 1：前台与 APP 按版本号判断站点配置是否变化（ETag）
UPDATE site_config
SET features = features - 'actors', version = version + 1, updated_at = now()
WHERE features ? 'actors';

-- +goose Down
-- 与 00004 的定义一致
CREATE TABLE actor (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         text        NOT NULL CHECK (name <> ''),
    aliases      text[]      NOT NULL DEFAULT '{}',
    slug         text        NOT NULL UNIQUE,
    bio          text        NOT NULL DEFAULT '',
    avatar       text        NOT NULL DEFAULT '',
    status       text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    follow_count bigint      NOT NULL DEFAULT 0 CHECK (follow_count >= 0),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE drama_actor (
    drama_id bigint NOT NULL REFERENCES drama (id) ON DELETE CASCADE,
    actor_id bigint NOT NULL REFERENCES actor (id) ON DELETE CASCADE,
    role     text   NOT NULL CHECK (role IN ('lead', 'support')),
    sort     int    NOT NULL DEFAULT 0,
    PRIMARY KEY (drama_id, actor_id)
);
CREATE INDEX drama_actor_actor_idx ON drama_actor (actor_id, drama_id);

-- 页面挂回「内容」目录；该目录已被删除时作为一级菜单
INSERT INTO admin_menu (parent_id, kind, code, title, path, icon, sort, builtin)
VALUES ((SELECT id FROM admin_menu WHERE code = 'content' AND kind = 'group'), 'page', 'actors', '演员', '/actors', 'Users', 20, true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO admin_permission (code, menu_id, name, sensitive, sort)
SELECT v.code, m.id, v.name, false, v.sort
FROM (VALUES ('actors.view', '查看', 0), ('actors.edit', '修改', 1)) AS v (code, name, sort)
JOIN admin_menu m ON m.code = 'actors'
ON CONFLICT (code) DO NOTHING;

UPDATE site_config
SET features = features || '{"actors": true}', version = version + 1, updated_at = now()
WHERE NOT features ? 'actors';
