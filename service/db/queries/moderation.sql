-- 后台评论 / 弹幕审核与先审后显开关（S2-5，docs/06 第 24 节）。

-- ---------- 先审后显开关（site_config.comment_policy，后端专用段，不进前台配置、不 bump 版本）----------

-- name: GetCommentPolicy :one
SELECT comment_policy FROM site_config WHERE id = 1;

-- name: SetCommentPolicy :one
UPDATE site_config SET comment_policy = @comment_policy WHERE id = 1
RETURNING comment_policy;

-- ---------- 评论审核列表 ----------
-- 筛选：状态（空串不筛）、只看被举报（reported=true 时要求存在举报）、Q 搜内容或用户名（ILIKE，模式已转义）。
-- 只看含屏蔽词在服务层扫描（matcher 在进程内），这里不处理。已软删的评论不在审核范围。

-- name: ListModerationComments :many
SELECT c.id, c.episode_id, c.parent_id, c.content, c.status, c.like_count, c.created_at,
       c.user_id, COALESCE(u.nickname, '') AS nickname, COALESCE(u.avatar, '') AS avatar,
       (SELECT count(*) FROM report r WHERE r.target_type = 'comment' AND r.target_id = c.id) AS report_count
FROM comment c
LEFT JOIN "user" u ON u.id = c.user_id
WHERE c.deleted_at IS NULL
  AND (@status::text = '' OR c.status = @status::text)
  AND (@reported::bool = false OR EXISTS (SELECT 1 FROM report r WHERE r.target_type = 'comment' AND r.target_id = c.id))
  AND (@pattern::text = '' OR c.content ILIKE @pattern::text OR COALESCE(u.username, '') ILIKE @pattern::text)
ORDER BY c.id DESC
LIMIT @lim::int OFFSET @off::int;

-- name: CountModerationComments :one
SELECT count(*)
FROM comment c
LEFT JOIN "user" u ON u.id = c.user_id
WHERE c.deleted_at IS NULL
  AND (@status::text = '' OR c.status = @status::text)
  AND (@reported::bool = false OR EXISTS (SELECT 1 FROM report r WHERE r.target_type = 'comment' AND r.target_id = c.id))
  AND (@pattern::text = '' OR c.content ILIKE @pattern::text OR COALESCE(u.username, '') ILIKE @pattern::text);

-- ---------- 弹幕审核列表 ----------

-- name: ListModerationDanmaku :many
SELECT d.id, d.episode_id, d.time_ms, d.content, d.color, d.status, d.created_at,
       d.user_id, COALESCE(u.nickname, '') AS nickname, COALESCE(u.avatar, '') AS avatar,
       (SELECT count(*) FROM report r WHERE r.target_type = 'danmaku' AND r.target_id = d.id) AS report_count
FROM danmaku d
LEFT JOIN "user" u ON u.id = d.user_id
WHERE (@status::text = '' OR d.status = @status::text)
  AND (@reported::bool = false OR EXISTS (SELECT 1 FROM report r WHERE r.target_type = 'danmaku' AND r.target_id = d.id))
  AND (@pattern::text = '' OR d.content ILIKE @pattern::text OR COALESCE(u.username, '') ILIKE @pattern::text)
ORDER BY d.id DESC
LIMIT @lim::int OFFSET @off::int;

-- name: CountModerationDanmaku :one
SELECT count(*)
FROM danmaku d
LEFT JOIN "user" u ON u.id = d.user_id
WHERE (@status::text = '' OR d.status = @status::text)
  AND (@reported::bool = false OR EXISTS (SELECT 1 FROM report r WHERE r.target_type = 'danmaku' AND r.target_id = d.id))
  AND (@pattern::text = '' OR d.content ILIKE @pattern::text OR COALESCE(u.username, '') ILIKE @pattern::text);

-- ---------- 审核操作（批量，按 id 数组）----------
-- 通过 = visible，隐藏 = hidden。评论删除走软删（可随回收站还原）；弹幕删除是硬删（docs：永久删除、不可恢复）。

-- name: SetCommentsStatus :execrows
UPDATE comment SET status = @status WHERE id = ANY(@ids::bigint[]) AND deleted_at IS NULL;

-- name: SoftDeleteComments :execrows
UPDATE comment SET deleted_at = now() WHERE id = ANY(@ids::bigint[]) AND deleted_at IS NULL;

-- name: SetDanmakuStatus :execrows
UPDATE danmaku SET status = @status WHERE id = ANY(@ids::bigint[]);

-- name: DeleteDanmaku :execrows
DELETE FROM danmaku WHERE id = ANY(@ids::bigint[]);
