-- 内容域的后台读写（S1-3，docs/03 第 3.1 节）。
-- 文本搜索的 LIKE 模式由领域层转义并加 %（pg_bigm 的 GIN 索引支持任意位置匹配）；pattern 为 NULL 表示不按文本搜。

-- ---------- 剧集 ----------

-- name: ListDramas :many
-- 后台剧集列表（页码分页，不含回收站中的剧）。sort 由领域层按白名单传入，其他值按编号倒序。
-- status 为 scheduled 时只含已上架但定时未到点的剧，published 只含已经可见的。
SELECT d.*
FROM drama d
WHERE d.deleted_at IS NULL
  AND ((sqlc.narg(pattern)::text IS NULL AND sqlc.narg(id_eq)::bigint IS NULL)
       OR d.title LIKE sqlc.narg(pattern)
       OR aliases_text(d.aliases) LIKE sqlc.narg(pattern)
       OR d.id = sqlc.narg(id_eq))
  AND (sqlc.narg(status)::text IS NULL
       OR (sqlc.narg(status) = 'scheduled' AND d.status = 'published' AND d.publish_at > now())
       OR (sqlc.narg(status) = 'published' AND d.status = 'published' AND (d.publish_at IS NULL OR d.publish_at <= now()))
       OR (sqlc.narg(status) NOT IN ('scheduled', 'published') AND d.status = sqlc.narg(status)))
  AND (sqlc.narg(tag_id)::bigint IS NULL
       OR EXISTS (SELECT 1 FROM drama_tag dt WHERE dt.drama_id = d.id AND dt.tag_id = sqlc.narg(tag_id)))
  AND (sqlc.narg(term_id)::bigint IS NULL
       OR EXISTS (SELECT 1 FROM drama_term dm WHERE dm.drama_id = d.id AND dm.term_id = sqlc.narg(term_id)))
ORDER BY
  CASE WHEN sqlc.arg(sort)::text = 'latest' THEN d.publish_at END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort) = 'updated' THEN d.updated_at END DESC,
  CASE WHEN sqlc.arg(sort) = 'hot' THEN d.hot_score END DESC,
  CASE WHEN sqlc.arg(sort) = 'recommend' THEN d.sort_weight END DESC,
  d.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountDramas :one
-- 与 ListDramas 相同的筛选条件。
SELECT count(*)
FROM drama d
WHERE d.deleted_at IS NULL
  AND ((sqlc.narg(pattern)::text IS NULL AND sqlc.narg(id_eq)::bigint IS NULL)
       OR d.title LIKE sqlc.narg(pattern)
       OR aliases_text(d.aliases) LIKE sqlc.narg(pattern)
       OR d.id = sqlc.narg(id_eq))
  AND (sqlc.narg(status)::text IS NULL
       OR (sqlc.narg(status) = 'scheduled' AND d.status = 'published' AND d.publish_at > now())
       OR (sqlc.narg(status) = 'published' AND d.status = 'published' AND (d.publish_at IS NULL OR d.publish_at <= now()))
       OR (sqlc.narg(status) NOT IN ('scheduled', 'published') AND d.status = sqlc.narg(status)))
  AND (sqlc.narg(tag_id)::bigint IS NULL
       OR EXISTS (SELECT 1 FROM drama_tag dt WHERE dt.drama_id = d.id AND dt.tag_id = sqlc.narg(tag_id)))
  AND (sqlc.narg(term_id)::bigint IS NULL
       OR EXISTS (SELECT 1 FROM drama_term dm WHERE dm.drama_id = d.id AND dm.term_id = sqlc.narg(term_id)));

-- name: GetDrama :one
SELECT * FROM drama WHERE id = $1 AND deleted_at IS NULL;

-- name: LockLiveDrama :one
-- 修改分集与关联的事务内先锁住剧（回收站中的剧查不到），串行化同一部剧的并发修改与「更新至 N 集」刷新。
SELECT id FROM drama WHERE id = $1 AND deleted_at IS NULL FOR UPDATE;

-- name: CreateDrama :one
-- 新剧为草稿；热度分 = 初始播放量（真实播放量为 0）。
INSERT INTO drama (title, aliases, slug, summary, cover_portrait, cover_landscape, badge, score, year, publish_at,
                   is_finished, sort_weight, view_base, hot_score, seo_title, seo_desc)
VALUES (@title, @aliases, sqlc.narg(slug), @summary, @cover_portrait, @cover_landscape, @badge, sqlc.narg(score),
        sqlc.narg(year), sqlc.narg(publish_at), @is_finished, @sort_weight, @view_base, @view_base, @seo_title, @seo_desc)
RETURNING *;

-- name: UpdateDrama :one
-- 整体保存剧资料；热度分随初始播放量同步为 view_base + view_count。
UPDATE drama
SET title = @title, aliases = @aliases, slug = sqlc.narg(slug), summary = @summary,
    cover_portrait = @cover_portrait, cover_landscape = @cover_landscape, badge = @badge,
    score = sqlc.narg(score), year = sqlc.narg(year), status = @status, publish_at = sqlc.narg(publish_at),
    is_finished = @is_finished, sort_weight = @sort_weight, view_base = @view_base,
    hot_score = sqlc.arg(view_base) + view_count, seo_title = @seo_title, seo_desc = @seo_desc, updated_at = now()
WHERE id = @id AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteDrama :one
-- 放进回收站：分集与关联保留以便还原。
UPDATE drama SET deleted_at = now(), updated_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING *;

-- name: TouchDramas :exec
UPDATE drama SET updated_at = now() WHERE id = ANY(@ids::bigint[]);

-- name: LiveDramaIDs :many
SELECT id FROM drama WHERE id = ANY(@ids::bigint[]) AND deleted_at IS NULL;

-- ---------- 剧的标签、维度取值 ----------

-- name: ListDramaTags :many
SELECT t.*
FROM drama_tag dt
JOIN tag t ON t.id = dt.tag_id
WHERE dt.drama_id = $1
ORDER BY t.id;

-- name: DeleteDramaTags :exec
DELETE FROM drama_tag WHERE drama_id = $1;

-- name: InsertDramaTags :exec
INSERT INTO drama_tag (drama_id, tag_id)
SELECT @drama_id::bigint, unnest(@tag_ids::bigint[]);

-- name: ListDramaTerms :many
SELECT tt.id, tt.taxonomy_id, x.name AS taxonomy_name, x.multi_select, tt.name, tt.slug, tt.status
FROM drama_term dm
JOIN taxonomy_term tt ON tt.id = dm.term_id
JOIN taxonomy x ON x.id = tt.taxonomy_id
WHERE dm.drama_id = $1
ORDER BY x.sort, x.id, tt.sort, tt.id;

-- name: DeleteDramaTerms :exec
DELETE FROM drama_term WHERE drama_id = $1;

-- name: InsertDramaTerms :exec
INSERT INTO drama_term (drama_id, term_id)
SELECT @drama_id::bigint, unnest(@term_ids::bigint[]);

-- ---------- 批量 ----------

-- name: BulkSetDramaStatus :execrows
-- 批量上架 / 下架 / 转草稿：只改状态不同的剧；上架时没有上架时间的写入当前时间。
UPDATE drama
SET status = @status::text,
    publish_at = CASE WHEN @status = 'published' THEN coalesce(publish_at, now()) ELSE publish_at END,
    updated_at = now()
WHERE id = ANY(@ids::bigint[]) AND deleted_at IS NULL
  AND (status <> @status OR (@status = 'published' AND publish_at IS NULL));

-- name: BulkAddDramaTags :many
-- 追加标签，返回新加了标签的剧（同一部剧可能出现多次）。
INSERT INTO drama_tag (drama_id, tag_id)
SELECT d.id, t.id
FROM drama d
CROSS JOIN tag t
WHERE d.id = ANY(@drama_ids::bigint[]) AND d.deleted_at IS NULL AND t.id = ANY(@tag_ids::bigint[])
ON CONFLICT DO NOTHING
RETURNING drama_id;

-- name: AppendTopicDramas :many
-- 按传入顺序追加到专题末尾；已在专题中的剧不动。
INSERT INTO topic_drama (topic_id, drama_id, sort)
SELECT @topic_id::bigint, d.id,
       (SELECT coalesce(max(x.sort), -1) FROM topic_drama x WHERE x.topic_id = @topic_id)
         + row_number() OVER (ORDER BY array_position(@drama_ids::bigint[], d.id))
FROM drama d
WHERE d.id = ANY(@drama_ids) AND d.deleted_at IS NULL
ON CONFLICT DO NOTHING
RETURNING drama_id;

-- name: DeleteOtherDramaTerms :many
-- 批量设取值：在给定维度（单选维度）中移除 keep_term_ids 以外的取值，返回有变化的剧。
DELETE FROM drama_term dm
USING taxonomy_term tt
WHERE tt.id = dm.term_id
  AND dm.drama_id = ANY(@drama_ids::bigint[])
  AND tt.taxonomy_id = ANY(@taxonomy_ids::bigint[])
  AND dm.term_id <> ALL(@keep_term_ids::bigint[])
RETURNING dm.drama_id;

-- name: BulkAddDramaTerms :many
INSERT INTO drama_term (drama_id, term_id)
SELECT d.id, tt.id
FROM drama d
CROSS JOIN taxonomy_term tt
WHERE d.id = ANY(@drama_ids::bigint[]) AND d.deleted_at IS NULL AND tt.id = ANY(@term_ids::bigint[])
ON CONFLICT DO NOTHING
RETURNING drama_id;

-- ---------- 分集 ----------

-- name: ListEpisodes :many
SELECT * FROM episode WHERE drama_id = $1 ORDER BY number;

-- name: GetEpisode :one
-- 所属的剧在回收站中时视为不存在。
SELECT e.*
FROM episode e
JOIN drama d ON d.id = e.drama_id
WHERE e.id = $1 AND d.deleted_at IS NULL;

-- name: GetEpisodeDramaID :one
SELECT drama_id FROM episode WHERE id = $1;

-- name: CreateEpisode :one
INSERT INTO episode (drama_id, number, title, cover, duration_sec, access, trial_sec)
VALUES (@drama_id, @number, @title, @cover, @duration_sec, @access, @trial_sec)
RETURNING *;

-- name: UpdateEpisode :one
UPDATE episode
SET number = @number, title = @title, cover = @cover, duration_sec = @duration_sec, access = @access,
    trial_sec = @trial_sec, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteEpisode :one
DELETE FROM episode WHERE id = $1 RETURNING *;

-- name: RefreshLatestEpisode :exec
-- 「更新至 N 集」取可播放（转封装完成，media_status = ready）分集的最大集号，同时更新剧的修改时间。
UPDATE drama
SET latest_episode = coalesce((SELECT max(e.number) FROM episode e WHERE e.drama_id = @id::bigint AND e.media_status = 'ready'), 0),
    updated_at = now()
WHERE id = @id;

-- ---------- 标签 ----------

-- name: ListTags :many
SELECT *
FROM tag
WHERE (sqlc.narg(pattern)::text IS NULL OR name LIKE sqlc.narg(pattern) OR slug LIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(merged)::boolean IS NULL OR (merged_into_id IS NOT NULL) = sqlc.narg(merged))
ORDER BY id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountTags :one
SELECT count(*)
FROM tag
WHERE (sqlc.narg(pattern)::text IS NULL OR name LIKE sqlc.narg(pattern) OR slug LIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(merged)::boolean IS NULL OR (merged_into_id IS NOT NULL) = sqlc.narg(merged));

-- name: GetTag :one
SELECT * FROM tag WHERE id = $1;

-- name: GetTagsByIDs :many
SELECT * FROM tag WHERE id = ANY(@ids::bigint[]);

-- name: CreateTag :one
INSERT INTO tag (name, slug, is_hidden, status)
VALUES (@name, @slug, @is_hidden, @status)
RETURNING *;

-- name: UpdateTag :one
UPDATE tag SET name = @name, slug = @slug, is_hidden = @is_hidden, status = @status WHERE id = @id RETURNING *;

-- name: LockTagsForMerge :many
-- 合并的事务内按编号顺序锁住源标签与目标标签（固定顺序避免死锁）。
SELECT id, merged_into_id FROM tag WHERE id = ANY(@ids::bigint[]) ORDER BY id FOR UPDATE;

-- name: CopyTagDramas :exec
INSERT INTO drama_tag (drama_id, tag_id)
SELECT dt.drama_id, @target_id::bigint FROM drama_tag dt WHERE dt.tag_id = @source_id::bigint
ON CONFLICT DO NOTHING;

-- name: DeleteTagDramas :execrows
DELETE FROM drama_tag WHERE tag_id = $1;

-- name: MergeTagInto :exec
-- 源标签与原先并入源标签的标签都改指目标。
UPDATE tag SET merged_into_id = @target_id::bigint WHERE id = @source_id::bigint OR merged_into_id = @source_id;

-- ---------- 筛选维度与取值 ----------

-- name: ListTaxonomies :many
SELECT * FROM taxonomy ORDER BY sort, id;

-- name: GetTaxonomy :one
SELECT * FROM taxonomy WHERE id = $1;

-- name: ListAllTerms :many
SELECT * FROM taxonomy_term ORDER BY taxonomy_id, sort, id;

-- name: ListTerms :many
SELECT * FROM taxonomy_term WHERE taxonomy_id = $1 ORDER BY sort, id;

-- name: CreateTaxonomy :one
-- 新维度排在最后。
INSERT INTO taxonomy (name, slug, multi_select, show_in_explore, status, sort)
VALUES (@name, @slug, @multi_select, @show_in_explore, @status, (SELECT coalesce(max(x.sort), -1) + 1 FROM taxonomy x))
RETURNING *;

-- name: UpdateTaxonomy :one
UPDATE taxonomy
SET name = @name, slug = @slug, multi_select = @multi_select, show_in_explore = @show_in_explore, status = @status
WHERE id = @id
RETURNING *;

-- name: DeleteTaxonomy :one
-- 取值与剧的关联级联删除。
DELETE FROM taxonomy WHERE id = $1 RETURNING *;

-- name: CountMultiTermDramas :one
-- 在该维度选了多个取值的剧数。
SELECT count(*)
FROM (SELECT dm.drama_id
      FROM drama_term dm
      JOIN taxonomy_term tt ON tt.id = dm.term_id
      WHERE tt.taxonomy_id = $1
      GROUP BY dm.drama_id
      HAVING count(*) > 1) x;

-- name: TaxonomyIDs :many
SELECT id FROM taxonomy;

-- name: SetTaxonomySort :exec
UPDATE taxonomy SET sort = array_position(@ids::bigint[], id) - 1 WHERE id = ANY(@ids);

-- name: GetTerm :one
SELECT * FROM taxonomy_term WHERE id = @id AND taxonomy_id = @taxonomy_id;

-- name: GetTermsByIDs :many
SELECT tt.id, tt.taxonomy_id, x.name AS taxonomy_name, x.multi_select, tt.name, tt.slug, tt.status
FROM taxonomy_term tt
JOIN taxonomy x ON x.id = tt.taxonomy_id
WHERE tt.id = ANY(@ids::bigint[]);

-- name: ExistingTermCodes :many
SELECT code FROM taxonomy_term WHERE code = ANY(@codes::text[]);

-- name: CreateTerm :one
-- 新取值排在该维度最后。
INSERT INTO taxonomy_term (taxonomy_id, name, slug, status, is_channel, code, sort)
VALUES (@taxonomy_id, @name, @slug, @status, @is_channel, @code,
        (SELECT coalesce(max(x.sort), -1) + 1 FROM taxonomy_term x WHERE x.taxonomy_id = @taxonomy_id))
RETURNING *;

-- name: UpdateTerm :one
UPDATE taxonomy_term
SET name = @name, slug = @slug, status = @status, is_channel = @is_channel, code = @code
WHERE id = @id AND taxonomy_id = @taxonomy_id
RETURNING *;

-- name: DeleteTerm :one
DELETE FROM taxonomy_term WHERE id = @id AND taxonomy_id = @taxonomy_id RETURNING *;

-- name: TermIDs :many
SELECT id FROM taxonomy_term WHERE taxonomy_id = $1;

-- name: SetTermSort :exec
UPDATE taxonomy_term
SET sort = array_position(@ids::bigint[], id) - 1
WHERE taxonomy_id = @taxonomy_id AND id = ANY(@ids);

-- ---------- 专题 ----------

-- name: ListTopics :many
SELECT *
FROM topic
WHERE (sqlc.narg(pattern)::text IS NULL
       OR title LIKE sqlc.narg(pattern) OR summary LIKE sqlc.narg(pattern) OR slug LIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(source_type)::text IS NULL OR source_type = sqlc.narg(source_type))
ORDER BY id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountTopics :one
SELECT count(*)
FROM topic
WHERE (sqlc.narg(pattern)::text IS NULL
       OR title LIKE sqlc.narg(pattern) OR summary LIKE sqlc.narg(pattern) OR slug LIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(source_type)::text IS NULL OR source_type = sqlc.narg(source_type));

-- name: GetTopic :one
SELECT * FROM topic WHERE id = $1;

-- name: LockTopic :one
-- 替换手选剧的事务内先锁住专题，串行化同一专题的并发保存。
SELECT id FROM topic WHERE id = $1 FOR UPDATE;

-- name: CreateTopic :one
INSERT INTO topic (title, slug, summary, status, source_type, rule_json, faq_json, list_in_index)
VALUES (@title, @slug, @summary, @status, @source_type, @rule_json, @faq_json, @list_in_index)
RETURNING *;

-- name: UpdateTopic :one
UPDATE topic
SET title = @title, slug = @slug, summary = @summary, status = @status, source_type = @source_type,
    rule_json = @rule_json, faq_json = @faq_json, list_in_index = @list_in_index
WHERE id = @id
RETURNING *;

-- name: DeleteTopic :one
DELETE FROM topic WHERE id = $1 RETURNING *;

-- name: ListTopicDramas :many
SELECT d.id, d.title, d.status, d.publish_at, d.cover_portrait
FROM topic_drama td
JOIN drama d ON d.id = td.drama_id
WHERE td.topic_id = $1 AND d.deleted_at IS NULL
ORDER BY td.sort, td.drama_id;

-- name: DeleteLiveTopicDramas :exec
-- 整体替换手选剧：回收站中的剧保留在专题里，还原后回到原位置。
DELETE FROM topic_drama td
USING drama d
WHERE td.topic_id = @topic_id AND d.id = td.drama_id AND d.deleted_at IS NULL;

-- name: InsertTopicDramas :exec
INSERT INTO topic_drama (topic_id, drama_id, sort)
SELECT @topic_id::bigint, unnest(@drama_ids::bigint[]), unnest(@sorts::int[]);
