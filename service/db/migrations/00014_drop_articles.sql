-- +goose Up
-- 不做资讯模块（S3-7）：去掉后台「资讯」页面及其权限点、站点「资讯」功能开关。资讯表从未建立，这里不涉及数据。

-- 权限点随页面级联删除（admin_permission.menu_id），角色授权随权限点级联删除（admin_role_permission.permission_code）
DELETE FROM admin_menu WHERE code = 'articles' AND kind = 'page';

-- version + 1：前台与 APP 按版本号判断站点配置是否变化（ETag）
UPDATE site_config
SET features = features - 'articles', version = version + 1, updated_at = now()
WHERE features ? 'articles';

-- +goose Down
-- 页面挂回「运营」目录；该目录已被删除时作为一级菜单。各角色原先对该页面的授权不恢复（超级管理员仍拥有全部权限）。
INSERT INTO admin_menu (parent_id, kind, code, title, path, icon, sort, builtin)
VALUES ((SELECT id FROM admin_menu WHERE code = 'operations' AND kind = 'group'), 'page', 'articles', '资讯', '/articles', 'Newspaper', 30, true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO admin_permission (code, menu_id, name, sensitive, sort)
SELECT v.code, m.id, v.name, false, v.sort
FROM (VALUES ('articles.view', '查看', 0), ('articles.edit', '修改', 1)) AS v (code, name, sort)
JOIN admin_menu m ON m.code = 'articles'
ON CONFLICT (code) DO NOTHING;

UPDATE site_config
SET features = features || '{"articles": true}', version = version + 1, updated_at = now()
WHERE NOT features ? 'articles';
