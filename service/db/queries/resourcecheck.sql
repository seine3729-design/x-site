-- name: ListResourceChecks :many
-- 后台列表（docs/06 第 9 节）：按剧编号筛选、只看打不开、搜剧名或剧编号。
-- 带上剧名与集号，页面不用再回查。
SELECT c.id, c.drama_id, d.title AS drama_title, c.episode_id, e.number AS episode_number,
       c.file_type, c.field, c.url, c.line_id, l.name AS line_name,
       c.result, c.status_code, c.elapsed_ms, c.message, c.checked_at
FROM resource_check c
JOIN drama d ON d.id = c.drama_id
LEFT JOIN episode e ON e.id = c.episode_id
LEFT JOIN storage_line l ON l.id = c.line_id
WHERE (@drama_id::bigint = 0 OR c.drama_id = @drama_id)
  AND (@bad_only::bool = false OR c.result <> 'ok')
  -- 搜索词是纯数字时也当剧编号匹配（数值比较，走 resource_check_drama_idx；文本比较用不上索引）
  AND (@pattern::text = '' OR d.title ILIKE @pattern OR (@word::bigint > 0 AND c.drama_id = @word::bigint))
-- 坏的排在最前：运营要的就是「哪些打不开」
ORDER BY (c.result = 'ok'), c.drama_id, c.episode_id NULLS FIRST, c.field
LIMIT @lim;

-- name: CountResourceChecks :one
SELECT count(*)
FROM resource_check c
JOIN drama d ON d.id = c.drama_id
WHERE (@drama_id::bigint = 0 OR c.drama_id = @drama_id)
  AND (@bad_only::bool = false OR c.result <> 'ok')
  AND (@pattern::text = '' OR d.title ILIKE @pattern OR (@word::bigint > 0 AND c.drama_id = @word::bigint));

-- name: ResourceCheckSummary :one
-- 概览：总条数、打不开的条数、最近一次体检时间。
SELECT count(*) AS total,
       count(*) FILTER (WHERE result <> 'ok') AS bad,
       max(checked_at)::timestamptz AS last_checked_at
FROM resource_check;

-- name: UpsertResourceCheck :exec
-- 一个资源只留最后一次的结果（唯一索引 resource_check_target_key）。
INSERT INTO resource_check (drama_id, episode_id, file_type, field, url, line_id, result, status_code, elapsed_ms, message, checked_at)
VALUES (@drama_id, sqlc.narg(episode_id), @file_type, @field, @url, sqlc.narg(line_id), @result, @status_code, @elapsed_ms, @message, now())
ON CONFLICT (drama_id, coalesce(episode_id, 0), file_type, field) DO UPDATE
SET url = excluded.url, line_id = excluded.line_id, result = excluded.result,
    status_code = excluded.status_code, elapsed_ms = excluded.elapsed_ms,
    message = excluded.message, checked_at = excluded.checked_at;

-- name: ClearResourceChecks :execrows
-- 清空结果：drama_id 为 0 时清全部。只清体检表，不动源文件。
DELETE FROM resource_check WHERE (@drama_id::bigint = 0 OR drama_id = @drama_id);

-- name: GetDramaForCheck :one
-- 取一部剧的待检封面。
SELECT id, title, status, cover_portrait, cover_landscape
FROM drama
WHERE id = $1 AND deleted_at IS NULL;

-- name: ListEpisodeAssetsForCheck :many
-- 取一部剧各分集的待检资源：本集封面、首帧图、当前视频产物。
-- object_key 为空表示这一集还没有产物（没转封装或失败），不算体检失败，跳过。
SELECT e.id, e.number, e.cover, e.poster, e.media_status,
       coalesce(o.key, '') AS object_key, coalesce(o.kind, '') AS object_kind
FROM episode e
LEFT JOIN media_object o ON o.id = e.video_object_id
WHERE e.drama_id = $1
ORDER BY e.number;

-- name: ListDramaIDsForCheck :many
-- 体检范围：include_unpublished 为假时只取已上架的剧。
SELECT id
FROM drama
WHERE deleted_at IS NULL
  AND (@include_unpublished::bool = true OR status = 'published')
ORDER BY id;
