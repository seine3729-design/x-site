-- +goose Up
-- 会员套餐后台配置（S3-15 / A3-5，docs/06 第 23a 节）。
-- 支付渠道凭据另行接入：必须先具备独立 PAYMENT_KEKS，不能复用存储凭据密钥。

INSERT INTO admin_menu (parent_id, kind, code, title, path, icon, sort, builtin)
SELECT g.id, 'page', 'vip_plans', '会员套餐', '/vip/plans', 'CreditCard', 70, true
FROM admin_menu g
WHERE g.code = 'operations' AND g.kind = 'group'
ON CONFLICT (code) DO NOTHING;

INSERT INTO admin_permission (code, menu_id, name, sensitive, sort)
SELECT m.code || v.suffix, m.id, v.name, v.sensitive, v.sort
FROM admin_menu m
CROSS JOIN (VALUES
    ('.view', '查看', false, 0),
    ('.edit', '修改', false, 1)
) AS v (suffix, name, sensitive, sort)
WHERE m.code = 'vip_plans'
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM admin_menu WHERE code = 'vip_plans';
