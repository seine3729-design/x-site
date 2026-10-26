-- name: GetPublicSiteConfig :one
-- 前台可见的配置段。analytics（统计代码）也在内：它最终要原样注入页面 HTML，本身就是公开内容，
-- 不下发的话这段配置只能写不能用（docs/specs/A3-3.md 决定 3）。
SELECT version, site, theme, layout, seo, analytics, age_gate, features, addresses, updated_at
FROM site_config
WHERE id = 1;
-- name: BumpSiteConfigVersion :one
-- 令站点版本 +1（S1-9）。前台配置的 ETag 由版本号派生，所以这一步会作废客户端与 CDN 上的旧配置。
UPDATE site_config
SET version = version + 1, updated_at = now()
WHERE id = 1
RETURNING version;

-- name: DramaObjectPrefixes :many
-- 一部剧各分集当前产物的目录（清缓存时据此清掉清单原文的读穿缓存，S4-4）。
SELECT DISTINCT o.key
FROM episode e
JOIN media_object o ON o.id = e.video_object_id
WHERE e.drama_id = $1
ORDER BY o.key;
