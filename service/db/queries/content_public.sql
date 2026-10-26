-- 内容域的前台只读查询（S1-4，docs/03 第 3.2 节、docs/specs/S1-4.md）。
-- 剧的可见性一律 deleted_at IS NULL AND status = 'published' AND (publish_at IS NULL OR publish_at <= now())，
-- 每条查询都写全，便于单独看懂。
-- 剧列表（探索 / 标签 / 频道 / 自动专题）按排序白名单在 store/pg/content_public.go 里拼装：sqlc 的 CASE 排序
-- 用不上 (status, 排序列, id) 复合索引，而前台列表是最热的查询。

-- ---------- 剧详情 ----------

-- name: GetPublicDrama :one
SELECT *
FROM drama d
WHERE d.id = $1
  AND d.deleted_at IS NULL
  AND d.status = 'published'
  AND (d.publish_at IS NULL OR d.publish_at <= now());

-- name: ListPublicDramaTags :many
-- 隐藏（status = hidden）与已合并的标签不显示；敏感标签（is_hidden）照常返回，由前台按年龄门决定是否展示。
SELECT t.*
FROM drama_tag dt
JOIN tag t ON t.id = dt.tag_id
WHERE dt.drama_id = $1 AND t.status = 'active' AND t.merged_into_id IS NULL
ORDER BY t.id;

-- name: ListPublicDramaTerms :many
-- 停用的取值与停用维度下的取值不显示；顺序与探索分面一致（维度顺序 → 维度内取值顺序）。
SELECT t.id, t.taxonomy_id, t.name, t.slug, t.code, t.is_channel,
       x.name AS taxonomy_name, x.slug AS taxonomy_slug
FROM drama_term dm
JOIN taxonomy_term t ON t.id = dm.term_id AND t.status = 'active'
JOIN taxonomy x ON x.id = t.taxonomy_id AND x.status = 'active'
WHERE dm.drama_id = $1
ORDER BY x.sort, x.id, t.sort, t.id;

-- name: ListPublicEpisodes :many
-- 分集全部返回：未转封装（media_status <> 'ready'）的 playable 为 false，前台显示「敬请期待」。
-- 分集封面为空时用转封装生成的首帧图。
SELECT id, number, title,
       coalesce(nullif(cover, ''), poster)::text AS cover,
       duration_sec, access, trial_sec,
       (media_status = 'ready')::boolean AS playable
FROM episode
WHERE drama_id = $1
ORDER BY number;

-- name: ListRelatedDramas :many
-- 相关推荐（docs/03 第 3.4 节）：同标签或同取值召回，按共同项数 → 热度 → 编号排序。
-- 召回池按编号倒序各取 pool_limit 条（新剧优先）：频道这类取值可能挂着整个片库，不限量会让详情页随片库规模变慢。
WITH tag_pool AS (
    SELECT dt.drama_id AS id
    FROM drama_tag dt
    WHERE dt.tag_id = ANY(@tag_ids::bigint[]) AND dt.drama_id <> @exclude_id::bigint
    ORDER BY dt.drama_id DESC
    LIMIT sqlc.arg(pool_limit)
), term_pool AS (
    SELECT dm.drama_id AS id
    FROM drama_term dm
    WHERE dm.term_id = ANY(@term_ids::bigint[]) AND dm.drama_id <> @exclude_id
    ORDER BY dm.drama_id DESC
    LIMIT sqlc.arg(pool_limit)
), scored AS (
    SELECT id, count(*) AS shared
    FROM (SELECT id FROM tag_pool UNION ALL SELECT id FROM term_pool) pool
    GROUP BY id
)
SELECT d.id, d.title, d.slug, d.cover_portrait, d.cover_landscape, d.badge, d.score, d.year,
       d.is_finished, d.latest_episode, (d.view_base + d.view_count)::bigint AS play_count,
       d.publish_at, d.created_at, d.hot_score, d.sort_weight
FROM scored s
JOIN drama d ON d.id = s.id
WHERE d.deleted_at IS NULL
  AND d.status = 'published'
  AND (d.publish_at IS NULL OR d.publish_at <= now())
ORDER BY s.shared DESC, d.hot_score DESC, d.id DESC
LIMIT sqlc.arg(page_limit);

-- ---------- 频道与标签 ----------

-- name: GetPublicChannel :one
-- 频道是设为频道的取值，slug 全站唯一；停用的取值或停用维度下的取值前台 404。
SELECT t.id, t.name, t.slug, t.code, t.taxonomy_id, x.name AS taxonomy_name, x.slug AS taxonomy_slug
FROM taxonomy_term t
JOIN taxonomy x ON x.id = t.taxonomy_id
WHERE t.slug = $1 AND t.is_channel AND t.status = 'active' AND x.status = 'active';

-- name: GetPublicTagBySlug :one
SELECT * FROM tag WHERE slug = $1 AND status = 'active';

-- name: GetPublicTagByID :one
-- 跟随 merged_into_id 取目标标签（合并是扁平的，最多跟随一次）。
SELECT * FROM tag WHERE id = $1 AND status = 'active';

-- ---------- 探索分面 ----------

-- name: ListExploreFacetTerms :many
-- 启用且「在探索页显示」的维度与它们启用的取值；没有启用取值的维度不出现。
SELECT x.id AS taxonomy_id, x.name AS taxonomy_name, x.slug AS taxonomy_slug, x.multi_select,
       t.id, t.name, t.slug, t.code, t.is_channel
FROM taxonomy x
JOIN taxonomy_term t ON t.taxonomy_id = x.id AND t.status = 'active'
WHERE x.status = 'active' AND x.show_in_explore
ORDER BY x.sort, x.id, t.sort, t.id;

-- name: ResolveTermIDs :many
-- 把「维度slug:取值slug」解析为取值编号（取值 slug 只在维度内唯一，所以必须成对匹配）。
-- 入参每项形如 drama-genre:romance，格式由领域层校验；停用的维度或取值解析不到（调用方据此返回空列表）。
SELECT t.id
FROM unnest(@pairs::text[]) AS p (pair)
JOIN taxonomy x ON x.slug = split_part(p.pair, ':', 1) AND x.status = 'active'
JOIN taxonomy_term t ON t.taxonomy_id = x.id AND t.slug = split_part(p.pair, ':', 2) AND t.status = 'active';

-- name: ResolveTermIDsByCodes :many
-- 自动专题规则用取值编号（taxonomy_term.code）引用取值；查不到的编号不返回，规则即无剧满足。
SELECT t.id
FROM taxonomy_term t
JOIN taxonomy x ON x.id = t.taxonomy_id AND x.status = 'active'
WHERE t.code = ANY(@codes::text[]) AND t.code <> '' AND t.status = 'active';

-- ---------- 专题 ----------

-- name: ListPublicTopics :many
-- 已发布且列在专题列表页的专题，按编号倒序的游标分页（cursor_id = 0 为第一页）。
SELECT *
FROM topic
WHERE status = 'published'
  AND list_in_index
  AND (sqlc.arg(cursor_id)::bigint = 0 OR id < sqlc.arg(cursor_id))
ORDER BY id DESC
LIMIT sqlc.arg(page_limit);

-- name: CountPublicTopics :one
SELECT count(*) FROM topic WHERE status = 'published' AND list_in_index;

-- name: GetPublicTopic :one
-- 未列在列表页的专题仍可按 slug 直接打开；未发布的 404。
SELECT * FROM topic WHERE slug = $1 AND status = 'published';

-- name: ListPublicTopicDramas :many
-- 手选专题：按后台保存的顺序，过滤掉不可见的剧。
SELECT d.id, d.title, d.slug, d.cover_portrait, d.cover_landscape, d.badge, d.score, d.year,
       d.is_finished, d.latest_episode, (d.view_base + d.view_count)::bigint AS play_count,
       d.publish_at, d.created_at, d.hot_score, d.sort_weight
FROM topic_drama td
JOIN drama d ON d.id = td.drama_id
WHERE td.topic_id = $1
  AND d.deleted_at IS NULL
  AND d.status = 'published'
  AND (d.publish_at IS NULL OR d.publish_at <= now())
ORDER BY td.sort, td.drama_id
LIMIT sqlc.arg(page_limit);

-- name: SuggestPublicDramas :many
-- 搜索联想（S3-4）：剧名或别名命中的可见剧。剧名以搜索词开头的排在前面，其余按热度、编号。
-- 两个模式都由领域层转小写并转义：pattern 是 %词%，prefix 是 词%（只用来排序）。
-- 按 lower(…) 写是为了用上迁移 00035 的 pg_bigm 索引（ILIKE 用不上索引）。
SELECT d.id, d.title
FROM drama d
WHERE d.deleted_at IS NULL AND d.status = 'published' AND (d.publish_at IS NULL OR d.publish_at <= now())
  AND (lower(d.title) LIKE sqlc.arg(pattern) OR lower(aliases_text(d.aliases)) LIKE sqlc.arg(pattern))
ORDER BY (lower(d.title) LIKE sqlc.arg(prefix)) DESC, d.hot_score DESC, d.id DESC
LIMIT sqlc.arg(page_limit);
