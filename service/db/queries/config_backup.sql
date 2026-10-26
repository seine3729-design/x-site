-- name: ListAllAdsForExport :many
-- 导出用：全部广告（含已停用），不含 imp_count / click_count（运行期统计不进备份）。
SELECT name, slot, creative_type, image, video_url, text, html, alt, click_url,
       device, locales, weight, end_at, freq_cap, status
FROM ad
ORDER BY id;

-- name: UpsertTaxonomyBySlug :one
-- 导入用：按 slug 匹配，已存在则更新、不存在则新建。返回编号与是否新建。
INSERT INTO taxonomy (name, slug, multi_select, show_in_explore, status, sort)
VALUES (@name, @slug, @multi_select, @show_in_explore, @status, @sort)
ON CONFLICT (slug) DO UPDATE
SET name = excluded.name, multi_select = excluded.multi_select,
    show_in_explore = excluded.show_in_explore, status = excluded.status, sort = excluded.sort
RETURNING id, (xmax = 0) AS inserted;

-- name: UpsertTermBySlug :one
-- 导入用：按「维度 + slug」匹配。频道标识与编码的全站唯一由部分唯一索引保证，撞了会报错并回滚整个导入。
INSERT INTO taxonomy_term (taxonomy_id, name, slug, status, is_channel, code, sort)
VALUES (@taxonomy_id, @name, @slug, @status, @is_channel, @code, @sort)
ON CONFLICT (taxonomy_id, slug) DO UPDATE
SET name = excluded.name, status = excluded.status, is_channel = excluded.is_channel,
    code = excluded.code, sort = excluded.sort
RETURNING id, (xmax = 0) AS inserted;

-- name: ListTaxonomySlugsNotIn :many
-- 导入后用：库里有、备份里没有的维度。这些**不删**（删维度会级联删取值，再级联删剧集的维度归属），
-- 只报告给运维，由其在维度页确认后手动处理。
SELECT slug, name FROM taxonomy
WHERE NOT (slug = ANY(@slugs::text[]))
ORDER BY sort, id;

-- name: ListTermSlugsNotIn :many
-- 导入后用：某个维度下、库里有而备份里没有的取值。同样不删，只报告。
SELECT t.slug, t.name FROM taxonomy_term t
WHERE t.taxonomy_id = @taxonomy_id
  AND NOT (t.slug = ANY(@slugs::text[]))
ORDER BY t.sort, t.id;

-- name: DeleteAllAds :execrows
-- 导入用：广告整表替换。ad 没有外键被引用，清空是安全的（代价是曝光点击计数归零）。
DELETE FROM ad;

-- name: InsertAdForImport :exec
INSERT INTO ad (name, slot, creative_type, image, video_url, text, html, alt, click_url,
                device, locales, weight, end_at, freq_cap, status)
VALUES (@name, @slot, @creative_type, @image, @video_url, @text, @html, @alt, @click_url,
        @device, @locales, @weight, @end_at, @freq_cap, @status);

-- name: InsertConfigBackup :one
INSERT INTO config_backup (format, reason, admin_id, note, payload, signature)
VALUES (@format, @reason, @admin_id, @note, @payload, @signature)
RETURNING id;

-- name: ListConfigBackups :many
-- 列表不返回正文（payload 可能很大），只给各类数量所需的计数。
SELECT id, format, reason, admin_id, note, signature, created_at,
       jsonb_array_length(coalesce(payload -> 'menus', '[]'::jsonb))        AS menus,
       jsonb_array_length(coalesce(payload -> 'static_pages', '[]'::jsonb)) AS static_pages,
       jsonb_array_length(coalesce(payload -> 'blocks', '[]'::jsonb))       AS blocks,
       jsonb_array_length(coalesce(payload -> 'taxonomies', '[]'::jsonb))   AS taxonomies,
       jsonb_array_length(coalesce(payload -> 'ads', '[]'::jsonb))          AS ads,
       (SELECT coalesce(sum(jsonb_array_length(coalesce(t -> 'terms', '[]'::jsonb))), 0)::int
        FROM jsonb_array_elements(coalesce(payload -> 'taxonomies', '[]'::jsonb)) AS t) AS terms
FROM config_backup
ORDER BY created_at DESC, id DESC
LIMIT @lim;

-- name: GetConfigBackup :one
SELECT id, format, reason, admin_id, note, payload, signature, created_at
FROM config_backup
WHERE id = $1;
