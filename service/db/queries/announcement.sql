-- 公告（S3-2 / A3-2，docs/specs/A3-4.md）。LIKE 模式由领域层转义并加 %；pattern 为 NULL 表示不按文本搜。

-- name: ListAnnouncements :many
SELECT *
FROM announcement
WHERE (sqlc.narg(pattern)::text IS NULL OR content LIKE sqlc.narg(pattern) OR click_url LIKE sqlc.narg(pattern))
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountAnnouncements :one
SELECT count(*)
FROM announcement
WHERE (sqlc.narg(pattern)::text IS NULL OR content LIKE sqlc.narg(pattern) OR click_url LIKE sqlc.narg(pattern))
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status));

-- name: GetAnnouncement :one
SELECT * FROM announcement WHERE id = $1;

-- name: CreateAnnouncement :one
INSERT INTO announcement (kind, content, click_url, frequency, pages, locales, image, start_at, end_at, status)
VALUES (@kind, @content, @click_url, @frequency, @pages, @locales, @image, @start_at, @end_at, @status)
RETURNING *;

-- name: UpdateAnnouncement :one
UPDATE announcement
SET kind = @kind, content = @content, click_url = @click_url, frequency = @frequency, pages = @pages,
    locales = @locales, image = @image, start_at = @start_at, end_at = @end_at, status = @status, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteAnnouncement :one
DELETE FROM announcement WHERE id = $1 RETURNING *;

-- name: ListAnnouncementCandidates :many
-- 前台候选：显示中、未结束的顶部滚动条与弹窗（是否已开始、投放语言由领域层按请求判断）。新的在前。
SELECT *
FROM announcement
WHERE status = 'active' AND kind IN ('topbar', 'popup') AND (end_at IS NULL OR end_at > now())
ORDER BY id DESC
LIMIT 200;
