-- name: GetFeatureConfig :one
-- 功能开关与年龄门（docs/06 第 36 节，S3-3）。两段都在 site_config 单行里。
SELECT features, age_gate FROM site_config WHERE id = 1;

-- name: SetFeatureFlags :exec
-- 整体保存功能开关。调用方已经把已废弃的键剔掉、未设置的补上默认值。
UPDATE site_config SET features = @features, updated_at = now() WHERE id = 1;

-- name: SetAgeGate :exec
-- 整体保存年龄确认门。
UPDATE site_config SET age_gate = @age_gate, updated_at = now() WHERE id = 1;

-- ---------- 品牌、配色与布局（docs/06 第 16 节，S3-3，docs/specs/A3-3.md）----------
-- name: GetBranding :one
SELECT site, theme, layout FROM site_config WHERE id = 1;

-- name: SetBrand :exec
-- 整体保存品牌段（站名、口号、版权、客服链接、Logo、favicon）。
UPDATE site_config SET site = @site, updated_at = now() WHERE id = 1;

-- name: SetTheme :exec
-- 整体保存配色段。
UPDATE site_config SET theme = @theme, updated_at = now() WHERE id = 1;

-- name: SetLayout :exec
-- 整体保存布局段。
UPDATE site_config SET layout = @layout, updated_at = now() WHERE id = 1;

-- ---------- SEO 与统计代码（docs/06 第 17 节，S3-3，docs/specs/A3-3.md）----------
-- name: GetSEOConfig :one
SELECT seo, analytics FROM site_config WHERE id = 1;

-- name: SetSEO :exec
-- 整体保存 SEO 段（标题 / 描述模板、收录与分享、站长验证、robots 追加）。
UPDATE site_config SET seo = @seo, updated_at = now() WHERE id = 1;

-- name: SetAnalytics :exec
-- 整体保存统计代码段（要 seo.analytics_code 权限，调用方已校验）。
UPDATE site_config SET analytics = @analytics, updated_at = now() WHERE id = 1;
