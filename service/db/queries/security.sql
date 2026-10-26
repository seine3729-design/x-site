-- 安全中心（docs/06 第 39 节，A4-4 / S4-8）：管理员登录记录（读 admin_login_log）。
-- 密钥轮换记录来自运行时 keyring、风控概览来自 Valkey 计数，都不经数据库，故不在此。

-- name: ListAdminLoginLogs :many
-- pattern 是已转义的 ILIKE 模式（匹配尝试的用户名）；result / since / until 为空表示不按该项筛。
-- admin_username 来自 LEFT JOIN：用户名不存在或账号已删时为空。
SELECT l.id, l.admin_id, l.username_tried, l.result, l.ip_prefix, l.ua_digest, l.created_at,
       a.username AS admin_username
FROM admin_login_log l
LEFT JOIN admin_user a ON a.id = l.admin_id
WHERE (sqlc.narg(result)::text IS NULL OR l.result = sqlc.narg(result))
  AND (sqlc.narg(pattern)::text IS NULL OR l.username_tried ILIKE sqlc.narg(pattern))
  AND (sqlc.narg(since)::timestamptz IS NULL OR l.created_at >= sqlc.narg(since))
  AND (sqlc.narg(until)::timestamptz IS NULL OR l.created_at < sqlc.narg(until))
ORDER BY l.created_at DESC, l.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountAdminLoginLogs :one
SELECT count(*)
FROM admin_login_log l
WHERE (sqlc.narg(result)::text IS NULL OR l.result = sqlc.narg(result))
  AND (sqlc.narg(pattern)::text IS NULL OR l.username_tried ILIKE sqlc.narg(pattern))
  AND (sqlc.narg(since)::timestamptz IS NULL OR l.created_at >= sqlc.narg(since))
  AND (sqlc.narg(until)::timestamptz IS NULL OR l.created_at < sqlc.narg(until));
