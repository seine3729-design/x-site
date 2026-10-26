-- 举报（S2-5，docs/02 第 5.6 节，docs/03 第 5.3 节）。举报人多态：reporter_id 或 reporter_guest_id。
-- 24 小时同人同对象去重由 Valkey 做，这里不建唯一约束、只管落库；举报队列的处理在后台（§25）。

-- name: CreateReport :exec
INSERT INTO report (target_type, target_id, reporter_id, reporter_guest_id, reason, detail)
VALUES (@target_type, @target_id, sqlc.narg(reporter_id), sqlc.narg(reporter_guest_id), @reason, @detail);

-- 后台举报队列（§25）。筛选：状态 / 对象类型 / 原因（空串表示不筛）；Q 匹配说明或举报人用户名（ILIKE，模式已转义）。
-- LEFT JOIN user 取举报人用户名 / 昵称 / 头像；游客这几列为空、前台显示「游客」。

-- name: ListReports :many
SELECT r.id, r.target_type, r.target_id, r.reason, r.detail, r.status, r.handle_note, r.created_at,
       r.reporter_id, COALESCE(u.nickname, '') AS nickname, COALESCE(u.avatar, '') AS avatar
FROM report r
LEFT JOIN "user" u ON u.id = r.reporter_id
WHERE (@status::text = '' OR r.status = @status::text)
  AND (@target_type::text = '' OR r.target_type = @target_type::text)
  AND (@reason::text = '' OR r.reason = @reason::text)
  AND (@pattern::text = '' OR r.detail ILIKE @pattern::text OR COALESCE(u.username, '') ILIKE @pattern::text)
ORDER BY r.id DESC
LIMIT @lim::int OFFSET @off::int;

-- name: CountReports :one
SELECT count(*)
FROM report r
LEFT JOIN "user" u ON u.id = r.reporter_id
WHERE (@status::text = '' OR r.status = @status::text)
  AND (@target_type::text = '' OR r.target_type = @target_type::text)
  AND (@reason::text = '' OR r.reason = @reason::text)
  AND (@pattern::text = '' OR r.detail ILIKE @pattern::text OR COALESCE(u.username, '') ILIKE @pattern::text);

-- name: HandleReport :one
UPDATE report SET status = @status, handle_note = @handle_note WHERE id = @id
RETURNING id, target_type, target_id, reason, detail, status, handle_note, created_at, reporter_id;
