-- name: ListTrashedDramas :many
-- 回收站里的剧（docs/06 第 35 节）：按删除时间倒序。分集数一并给出，让运维知道还原会带回多少内容。
SELECT d.id, d.title, d.slug, d.cover_portrait, d.status, d.deleted_at, d.updated_at,
       (SELECT count(*) FROM episode e WHERE e.drama_id = d.id)::int AS episodes
FROM drama d
WHERE d.deleted_at IS NOT NULL
  AND (@pattern::text = '' OR d.title ILIKE @pattern OR d.slug ILIKE @pattern)
ORDER BY d.deleted_at DESC, d.id DESC
LIMIT @lim OFFSET @off;

-- name: CountTrashedDramas :one
SELECT count(*)
FROM drama d
WHERE d.deleted_at IS NOT NULL
  AND (@pattern::text = '' OR d.title ILIKE @pattern OR d.slug ILIKE @pattern);

-- name: RestoreDrama :one
-- 还原：清掉删除标记。分集与关联行一直保留着，所以还原后内容完整。
-- 只还原仍在回收站里的（已被彻底删除的查不到，返回 no rows）。
UPDATE drama SET deleted_at = NULL, updated_at = now()
WHERE id = $1 AND deleted_at IS NOT NULL
RETURNING id, title, slug, status;

-- name: PurgeDrama :one
-- 彻底删除：物理删除。分集、标签与维度关联、专题关联、排行干预都会级联删除；
-- 分集引用的转码产物随之失去引用，由孤立产物清理任务（S1-6f）回收存储上的文件。
DELETE FROM drama
WHERE id = $1 AND deleted_at IS NOT NULL
RETURNING id, title, slug;

-- name: GetTrashedDrama :one
SELECT d.id, d.title, d.slug, d.deleted_at,
       (SELECT count(*) FROM episode e WHERE e.drama_id = d.id)::int AS episodes
FROM drama d
WHERE d.id = $1 AND d.deleted_at IS NOT NULL;

-- name: PurgeExpiredDramas :many
-- 清理任务用：删除已过保留期的剧，返回被删的编号与标题（写日志用）。
DELETE FROM drama
WHERE deleted_at IS NOT NULL AND deleted_at < @before
RETURNING id, title;

-- name: TrashCounts :one
-- 各分类的条数与最早的删除时间（最早的决定「最近一条到期」）。
-- 目前只有剧集一类：其余模块要么是设置类不进回收站（docs/06 第 35 节），要么还没实现软删。
SELECT count(*)::int AS dramas,
       min(deleted_at)::timestamptz AS oldest_deleted_at
FROM drama
WHERE deleted_at IS NOT NULL;
