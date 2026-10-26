-- 弹幕（S2-5，docs/02 第 5.5 节，docs/03 第 5.2 节）。作者多态：user_id 或 guest_device_id。
-- 按 [from_ms, to_ms) 窗口拉取显示中的弹幕，命中 danmaku_episode_time_idx。

-- name: CreateDanmaku :one
INSERT INTO danmaku (episode_id, user_id, guest_device_id, time_ms, content, color, status)
VALUES (@episode_id, sqlc.narg(user_id), sqlc.narg(guest_device_id), @time_ms, @content, @color, @status)
RETURNING id, episode_id, time_ms, content, color, status, created_at;

-- name: ListDanmakuWindow :many
SELECT id, time_ms, content, color
FROM danmaku
WHERE episode_id = @episode_id AND status = 'visible'
  AND time_ms >= @from_ms::int AND time_ms < @to_ms::int
ORDER BY time_ms ASC, id ASC
LIMIT @lim::int;
