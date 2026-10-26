-- 注册用户的收藏、点赞、观看历史、通知（S2-3，docs/02 第 5.2 / 5.3 / 5.9 节）。

-- name: AddFollow :exec
-- 收藏一部剧，幂等（已收藏不报错）。
INSERT INTO follow (user_id, drama_id) VALUES ($1, $2)
ON CONFLICT (user_id, drama_id) DO NOTHING;

-- name: RemoveFollow :execrows
DELETE FROM follow WHERE user_id = $1 AND drama_id = $2;

-- name: ListFollows :many
-- 收藏（追剧）列表：最近收藏在前，JOIN 剧取卡片字段。
-- 剧的可见性与前台列表一致：deleted_at IS NULL AND status = 'published' AND (publish_at IS NULL OR publish_at <= now())。
-- watched_episode 是这个用户在该剧看到的集号（没有观看记录则为 0），前台据此显示「还有 N 集未看」。
SELECT f.drama_id, f.created_at,
       d.title, d.slug, d.cover_portrait, d.cover_landscape, d.badge, d.score, d.year, d.publish_at,
       d.is_finished, d.latest_episode, (d.view_base + d.view_count)::bigint AS play_count,
       coalesce(h.episode_number, 0)::int AS watched_episode
FROM follow f
JOIN drama d ON d.id = f.drama_id
  AND d.deleted_at IS NULL
  AND d.status = 'published'
  AND (d.publish_at IS NULL OR d.publish_at <= now())
LEFT JOIN watch_history h ON h.user_id = f.user_id AND h.drama_id = f.drama_id
WHERE f.user_id = @user_id
ORDER BY f.created_at DESC, f.drama_id DESC
LIMIT @lim::int;

-- name: AddLike :exec
-- 点赞剧 / 分集，幂等。
INSERT INTO "like" (user_id, target_type, target_id) VALUES ($1, $2, $3)
ON CONFLICT (user_id, target_type, target_id) DO NOTHING;

-- name: RemoveLike :execrows
DELETE FROM "like" WHERE user_id = $1 AND target_type = $2 AND target_id = $3;

-- ---------- 观看历史（续播断点）----------

-- name: HistoryPaused :one
-- 用户是否开了「暂停记录」（领域层据此静默忽略上报，docs/13 第 2 节）。
SELECT history_paused FROM "user" WHERE id = $1;

-- name: UpsertHistory :exec
-- worker 每分钟批量落库续播断点。合并写：仅当上报时间更新时覆盖，避免老上报盖掉新进度。
-- 时间以 unix 秒传入（@tss），规避 timestamptz[] 数组参数映射。
-- JOIN drama 过滤掉不存在的剧：客户端可能上报伪造的 drama_id，不 JOIN 会因外键失败整批卡住 worker。
INSERT INTO watch_history (user_id, drama_id, episode_id, episode_number, position_sec, duration_sec, updated_at)
SELECT d.user_id, d.drama_id, d.episode_id, d.episode_number, d.position_sec, d.duration_sec, to_timestamp(d.ts)
FROM (
    SELECT
        unnest(@user_ids::bigint[])     AS user_id,
        unnest(@drama_ids::bigint[])    AS drama_id,
        unnest(@episode_ids::bigint[])  AS episode_id,
        unnest(@episode_numbers::int[]) AS episode_number,
        unnest(@position_secs::int[])   AS position_sec,
        unnest(@duration_secs::int[])   AS duration_sec,
        unnest(@tss::bigint[])          AS ts
) AS d
JOIN drama dr ON dr.id = d.drama_id
ON CONFLICT (user_id, drama_id) DO UPDATE SET
    episode_id     = excluded.episode_id,
    episode_number = excluded.episode_number,
    position_sec   = excluded.position_sec,
    duration_sec   = excluded.duration_sec,
    updated_at     = excluded.updated_at
WHERE excluded.updated_at > watch_history.updated_at;

-- name: ListHistory :many
-- 续播列表：最近在前，JOIN 剧取卡片字段；回收站中的剧不展示。
SELECT
    h.drama_id, h.episode_id, h.episode_number, h.position_sec, h.duration_sec, h.updated_at,
    d.title, d.cover_portrait, d.latest_episode
FROM watch_history h
JOIN drama d ON d.id = h.drama_id AND d.deleted_at IS NULL
WHERE h.user_id = $1
ORDER BY h.updated_at DESC
LIMIT $2;

-- name: DeleteHistory :exec
DELETE FROM watch_history WHERE user_id = $1 AND drama_id = ANY($2::bigint[]);

-- name: DeleteAllHistory :exec
DELETE FROM watch_history WHERE user_id = $1;

-- ---------- 站内信（通知）----------

-- name: ListNotifications :many
-- 消息列表：倒序游标分页，@before = 0 从最新开始，否则取 id 更小（更旧）的。
SELECT id, type, payload, is_read, created_at
FROM notification
WHERE user_id = @user_id AND (@before::bigint = 0 OR id < @before::bigint)
ORDER BY id DESC
LIMIT @lim::int;

-- name: MarkAllNotificationsRead :execrows
UPDATE notification SET is_read = true WHERE user_id = $1 AND is_read = false;

-- name: CountUnreadNotifications :one
SELECT count(*) FROM notification WHERE user_id = $1 AND is_read = false;

-- name: InsertNotification :exec
-- 写一条站内信（S2-5：评论被回复 / 被点赞；来源在 worker 里异步写入，仅注册用户）。
INSERT INTO notification (user_id, type, payload) VALUES (@user_id, @type, @payload);
