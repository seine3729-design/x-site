-- 评论（S2-5，docs/02 第 5.4 节，docs/03 第 5.1 节）。作者多态：user_id 或 guest_device_id。
-- 列表 LEFT JOIN user 取昵称 / 头像；游客这两列为空、前台显示「游客」。游标分页：latest 用 id、hot 用 (like_count,id)。

-- name: CreateComment :one
INSERT INTO comment (episode_id, user_id, guest_device_id, parent_id, content, status)
VALUES (@episode_id, sqlc.narg(user_id), sqlc.narg(guest_device_id), sqlc.narg(parent_id), @content, @status)
RETURNING id, episode_id, parent_id, content, status, like_count, created_at;

-- name: ListCommentsLatest :many
SELECT c.id, c.parent_id, c.content, c.image_count, c.like_count, c.created_at,
       c.user_id, COALESCE(u.nickname, '') AS nickname, COALESCE(u.avatar, '') AS avatar
FROM comment c
LEFT JOIN "user" u ON u.id = c.user_id
WHERE c.episode_id = @episode_id AND c.deleted_at IS NULL AND c.status = 'visible'
  AND (@cursor_id::bigint = 0 OR c.id < @cursor_id::bigint)
ORDER BY c.id DESC
LIMIT @lim::int;

-- name: ListCommentsHot :many
SELECT c.id, c.parent_id, c.content, c.image_count, c.like_count, c.created_at,
       c.user_id, COALESCE(u.nickname, '') AS nickname, COALESCE(u.avatar, '') AS avatar
FROM comment c
LEFT JOIN "user" u ON u.id = c.user_id
WHERE c.episode_id = @episode_id AND c.deleted_at IS NULL AND c.status = 'visible'
  AND (@cursor_like::bigint < 0 OR (c.like_count, c.id) < (@cursor_like::bigint, @cursor_id::bigint))
ORDER BY c.like_count DESC, c.id DESC
LIMIT @lim::int;

-- name: CommentAuthor :one
-- 删除鉴权 + 通知收件人：取作者、所属分集与是否已删。
SELECT user_id, guest_device_id, episode_id, (deleted_at IS NOT NULL)::boolean AS deleted FROM comment WHERE id = $1;

-- name: SoftDeleteComment :exec
UPDATE comment SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL;

-- name: InsertCommentLike :execrows
-- 幂等插入点赞；返回受影响行数（1=新增，0=已点过）。
INSERT INTO comment_like (comment_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING;

-- name: IncrCommentLikeCount :exec
UPDATE comment SET like_count = like_count + 1 WHERE id = $1;
