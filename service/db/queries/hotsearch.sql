-- 热搜词（S3-4 / A3-4，docs/specs/A3-4.md）。LIKE 模式由领域层转义并加 %；narg 为 NULL 表示不按该项筛选。

-- name: ListHotWords :many
SELECT *
FROM hotsearch
WHERE (sqlc.narg(pattern)::text IS NULL OR word LIKE sqlc.narg(pattern))
  AND (sqlc.narg(visible)::boolean IS NULL OR visible = sqlc.narg(visible))
  AND (sqlc.narg(pinned)::boolean IS NULL OR pinned = sqlc.narg(pinned))
  AND (NOT sqlc.arg(missing)::boolean OR miss_count > 0)
ORDER BY pinned DESC, pin_sort, search_count DESC, id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountHotWords :one
SELECT count(*)
FROM hotsearch
WHERE (sqlc.narg(pattern)::text IS NULL OR word LIKE sqlc.narg(pattern))
  AND (sqlc.narg(visible)::boolean IS NULL OR visible = sqlc.narg(visible))
  AND (sqlc.narg(pinned)::boolean IS NULL OR pinned = sqlc.narg(pinned))
  AND (NOT sqlc.arg(missing)::boolean OR miss_count > 0);

-- name: GetHotWord :one
SELECT * FROM hotsearch WHERE id = $1;

-- name: CreateHotWord :one
-- 后台手动加词：默认显示。
INSERT INTO hotsearch (word, visible, source) VALUES (@word, true, 'manual')
RETURNING *;

-- name: LockHotPins :exec
-- 置顶数量与顺序的修改串行进行（事务级咨询锁，事务结束自动释放）。
SELECT pg_advisory_xact_lock(hashtext('hotsearch_pin'));

-- name: CountOtherPinnedHotWords :one
SELECT count(*) FROM hotsearch WHERE pinned AND id <> @id;

-- name: SetHotWordFlags :one
-- 新置顶的排到最后；取消置顶时清掉顺序。
UPDATE hotsearch
SET visible  = @visible,
    pinned   = @pinned::boolean,
    pin_sort = CASE
                   WHEN NOT @pinned::boolean THEN 0
                   WHEN hotsearch.pinned THEN hotsearch.pin_sort
                   ELSE (SELECT coalesce(max(h.pin_sort), 0) + 1 FROM hotsearch h WHERE h.pinned)
               END,
    updated_at = now()
WHERE hotsearch.id = @id
RETURNING *;

-- name: ListPinnedHotWordIDs :many
SELECT id FROM hotsearch WHERE pinned ORDER BY pin_sort, id;

-- name: SetHotWordPinSort :exec
UPDATE hotsearch
SET pin_sort = d.sort, updated_at = now()
FROM (SELECT unnest(@ids::bigint[]) AS id, unnest(@sorts::int[]) AS sort) AS d
WHERE hotsearch.id = d.id;

-- name: DeleteHotWord :one
DELETE FROM hotsearch WHERE id = $1 RETURNING *;

-- name: ListHotBoard :many
-- 热搜榜：显示中的词，置顶的按拖拽顺序在前，其余按搜索次数倒序。
SELECT *
FROM hotsearch
WHERE visible
ORDER BY pinned DESC, pin_sort, search_count DESC, id
LIMIT sqlc.arg(n);

-- name: AddHotCounts :exec
-- 把 Valkey 里累计的搜索次数加到累计上（worker 每小时一次）。新词自动收录、默认不显示；不改 updated_at（那是后台编辑时间）。
INSERT INTO hotsearch (word, search_count, miss_count, source, last_searched_at)
SELECT d.word, d.searches, d.misses, 'auto', now()
FROM (SELECT unnest(@words::text[]) AS word, unnest(@searches::bigint[]) AS searches, unnest(@misses::bigint[]) AS misses) AS d
ON CONFLICT (word) DO UPDATE
SET search_count     = hotsearch.search_count + EXCLUDED.search_count,
    miss_count       = hotsearch.miss_count + EXCLUDED.miss_count,
    last_searched_at = now();
