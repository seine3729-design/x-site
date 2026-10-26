-- 广告（S3-6 / A3-4，docs/specs/A3-4.md）。LIKE 模式由领域层转义并加 %；pattern 为 NULL 表示不按文本搜。

-- name: ListAds :many
SELECT *
FROM ad
WHERE (sqlc.narg(pattern)::text IS NULL OR name LIKE sqlc.narg(pattern) OR click_url LIKE sqlc.narg(pattern))
  AND (sqlc.narg(slot)::text IS NULL OR slot = sqlc.narg(slot))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountAds :one
SELECT count(*)
FROM ad
WHERE (sqlc.narg(pattern)::text IS NULL OR name LIKE sqlc.narg(pattern) OR click_url LIKE sqlc.narg(pattern))
  AND (sqlc.narg(slot)::text IS NULL OR slot = sqlc.narg(slot))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status));

-- name: GetAd :one
SELECT * FROM ad WHERE id = $1;

-- name: CreateAd :one
INSERT INTO ad (name, slot, creative_type, image, video_url, text, html, alt, click_url, device, locales, weight, end_at, freq_cap, status)
VALUES (@name, @slot, @creative_type, @image, @video_url, @text, @html, @alt, @click_url, @device, @locales, @weight, @end_at, @freq_cap, @status)
RETURNING *;

-- name: UpdateAd :one
-- 整体保存；曝光、点击累计不受影响。
UPDATE ad
SET name = @name, slot = @slot, creative_type = @creative_type, image = @image, video_url = @video_url, text = @text,
    html = @html, alt = @alt, click_url = @click_url, device = @device, locales = @locales, weight = @weight,
    end_at = @end_at, freq_cap = @freq_cap, status = @status, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteAd :one
DELETE FROM ad WHERE id = $1 RETURNING *;

-- name: ListAdCandidates :many
-- 前台候选：投放中且未结束的广告（位置、端、投放语言、频控由领域层按请求判断）。新的在前。
SELECT *
FROM ad
WHERE status = 'active' AND (end_at IS NULL OR end_at > now())
ORDER BY id DESC
LIMIT 1000;

-- name: AddAdCounts :execrows
-- 把 Valkey 里累计的曝光、点击加到累计数上（worker 每分钟一次）。已删除的广告自然跳过；不改 updated_at（那是后台编辑时间）。
UPDATE ad
SET imp_count = ad.imp_count + d.imp, click_count = ad.click_count + d.clk
FROM (SELECT unnest(@ids::bigint[]) AS id, unnest(@imps::bigint[]) AS imp, unnest(@clicks::bigint[]) AS clk) AS d
WHERE ad.id = d.id;
