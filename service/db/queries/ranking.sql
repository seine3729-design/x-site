-- 榜单（S3-5 / A3-4，docs/specs/A3-4.md）。前台可见性与 content_public.sql 一致：不在回收站、已上架、定时已到点。

-- name: ListRankSignals :many
-- 计分数据：埋点（S4-1 / S4-2）与追剧、点赞（S2-3）就绪前，播放 = 剧的热度（初始播放量 + 真实播放量），
-- 追剧、点赞、看完按 0，周榜与月榜相同（docs/specs/A3-4.md 决定 3）。只算前台可见的剧。
SELECT d.id, (d.view_base + d.view_count)::bigint AS plays
FROM drama d
WHERE d.deleted_at IS NULL AND d.status = 'published' AND (d.publish_at IS NULL OR d.publish_at <= now());

-- name: ListRankOverrideRules :many
SELECT drama_id, pinned, blocked, boost FROM rank_override WHERE board = @board;

-- name: ListRankOverrides :many
-- 后台显示：带剧名、剧的状态与是否在回收站。置顶的在前，其次不让上榜的，再按加分倒序。
SELECT o.drama_id, o.pinned, o.blocked, o.boost, o.updated_at, d.title, d.status, (d.deleted_at IS NOT NULL)::boolean AS in_trash
FROM rank_override o
JOIN drama d ON d.id = o.drama_id
WHERE o.board = @board
ORDER BY o.pinned DESC, o.blocked DESC, o.boost DESC, o.drama_id;

-- name: LockRankOverrides :exec
-- 同一张榜的整体保存串行进行（事务级咨询锁），避免两人同时保存时主键冲突。
SELECT pg_advisory_xact_lock(hashtext('rank_override:' || @board::text));

-- name: DeleteRankOverrides :exec
DELETE FROM rank_override WHERE board = @board;

-- name: InsertRankOverrides :exec
INSERT INTO rank_override (board, drama_id, pinned, blocked, boost)
SELECT @board::text, unnest(@drama_ids::bigint[]), unnest(@pinned::boolean[]), unnest(@blocked::boolean[]), unnest(@boosts::bigint[]);

-- name: ListLiveDramaIDsAmong :many
-- 存在且不在回收站的剧（人工调整的目标校验）。
SELECT id FROM drama WHERE id = ANY(@ids::bigint[]) AND deleted_at IS NULL;

-- name: ListDramaTitles :many
SELECT id, title FROM drama WHERE id = ANY(@ids::bigint[]);

-- name: ListRankCards :many
-- 前台榜单的剧卡片：按 ids 的顺序返回，只含前台可见的剧（上次重算后下架的剧在这里被滤掉）。列与 content_public 的卡片一致。
SELECT d.id, d.title, d.slug, d.cover_portrait, d.cover_landscape, d.badge, d.score, d.year,
       d.is_finished, d.latest_episode, (d.view_base + d.view_count)::bigint AS play_count,
       d.publish_at, d.created_at, d.hot_score, d.sort_weight
FROM drama d
WHERE d.id = ANY(@ids::bigint[])
  AND d.deleted_at IS NULL AND d.status = 'published' AND (d.publish_at IS NULL OR d.publish_at <= now())
ORDER BY array_position(@ids::bigint[], d.id);

-- name: GetRankingWeights :one
SELECT ranking FROM site_config WHERE id = 1;

-- name: SetRankingWeights :one
-- 只给后台读的段，不改前台配置的 version 与 updated_at。
UPDATE site_config SET ranking = @ranking WHERE id = 1
RETURNING ranking;
