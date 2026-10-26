-- name: FindDramasForImport :many
-- 导入查重（docs/11 S4-6「重复不重建」）：按剧名或别名精确匹配已有的剧。
-- titles 是本批要导入的全部剧名（已归一化为小写）；同时比对已有剧的剧名与别名。
-- 回收站里的剧也算已存在 —— 否则导入会建一部同名的新剧，把运营刚删掉的东西又带回来。
SELECT id, title, aliases, (deleted_at IS NOT NULL)::bool AS deleted
FROM drama
WHERE lower(title) = ANY(@titles::text[])
   OR EXISTS (SELECT 1 FROM unnest(aliases) a WHERE lower(a) = ANY(@titles::text[]));

-- name: ImportCreateDrama :one
-- 新建一部剧。新剧为草稿（docs/06 第 8 节）；source 是采集批次标识。
-- 不走 content.CreateDrama 是因为那条路径不写 source 列，而导入必须记下批次来源。
INSERT INTO drama (title, aliases, summary, status, source, hot_score)
VALUES (@title, @aliases, @summary, 'draft', @source, 0)
RETURNING id;

-- name: ImportUpdateDrama :exec
-- 更新已有的剧：**只覆盖 CSV 带来的那几列**，不动封面、评分、状态、上架时间等。
-- 不用 content.UpdateDrama：那是整体保存语义，会把 CSV 没提供的字段清成默认值。
--
-- 别名按并集合并（不删已有的）：运营在后台手填的别名不该被一次导入抹掉。
-- 简介只在导入带了内容时覆盖，留空表示「这次不改」。
UPDATE drama
SET aliases = (
        SELECT coalesce(array_agg(DISTINCT a ORDER BY a), '{}')
        FROM unnest(aliases || @aliases::text[]) a
        WHERE a <> ''
    ),
    summary = CASE WHEN @summary::text = '' THEN summary ELSE @summary END,
    source = CASE WHEN @source::text = '' THEN source ELSE @source END,
    updated_at = now()
WHERE id = @id;

-- name: FindTagsByNames :many
-- 按名称精确找标签（大小写不敏感）。已被合并的标签返回 merged_into_id，导入时改指目标。
SELECT id, name, merged_into_id
FROM tag
WHERE lower(name) = ANY(@names::text[]);

-- name: ImportCreateTag :one
-- 导入时自动建标签。tag.name 没有唯一约束，所以调用方要先查过一遍；
-- 导入是运营手动触发的低频单请求操作，同一批次内串行处理，不会自己和自己并发。
INSERT INTO tag (name, slug, status)
VALUES (@name, @slug, 'active')
RETURNING id;

-- name: ImportAddDramaTags :exec
-- 给剧追加标签（不是整体替换）：导入不该抹掉剧上 CSV 里没写的标签。
INSERT INTO drama_tag (drama_id, tag_id)
SELECT @drama_id, t FROM unnest(@tag_ids::bigint[]) t
ON CONFLICT DO NOTHING;

-- name: CreateImportLog :one
-- 记一次正式导入（预览不记，docs/06 第 8 节）。
INSERT INTO import_log (
    source, file_name, rows_total, dramas_created, dramas_updated,
    episodes_created, episodes_updated, skipped, rejected, tags_created, admin_id
) VALUES (
    @source, @file_name, @rows_total, @dramas_created, @dramas_updated,
    @episodes_created, @episodes_updated, @skipped, @rejected, @tags_created, @admin_id
)
RETURNING id, source, file_name, rows_total, dramas_created, dramas_updated,
          episodes_created, episodes_updated, skipped, rejected, tags_created, admin_id, created_at;

-- name: ListImportLogs :many
-- 导入记录，倒序；可按日期区间筛（docs/06 第 8 节「可搜日期」）。
SELECT l.id, l.source, l.file_name, l.rows_total, l.dramas_created, l.dramas_updated,
       l.episodes_created, l.episodes_updated, l.skipped, l.rejected, l.tags_created,
       l.admin_id, coalesce(u.username, '') AS admin_name, l.created_at
FROM import_log l
LEFT JOIN admin_user u ON u.id = l.admin_id
WHERE (@from_ts::timestamptz IS NULL OR l.created_at >= @from_ts)
  AND (@to_ts::timestamptz IS NULL OR l.created_at < @to_ts)
ORDER BY l.id DESC
LIMIT @lim OFFSET @off;

-- name: CountImportLogs :one
SELECT count(*)
FROM import_log
WHERE (@from_ts::timestamptz IS NULL OR created_at >= @from_ts)
  AND (@to_ts::timestamptz IS NULL OR created_at < @to_ts);
