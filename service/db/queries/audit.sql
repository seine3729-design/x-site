-- name: LockAuditChain :exec
-- 串行化审计插入（S4-7）：哈希链要求「读链尾 → 算哈希 → 插入」三步之间没有别的插入挤进来。
-- 单个进程内 AuditRecorder 已经是串行的，这把锁防的是多实例并发写。
-- 用事务级 advisory lock 而不是对最后一行加行锁：表空时没有行可锁，而且意图更清楚。
SELECT pg_advisory_xact_lock(4771001);

-- name: LastAuditHash :one
-- 链尾的哈希。表空或还没有任何带哈希的记录时返回空串。
SELECT coalesce((SELECT hash FROM audit_log WHERE hash <> '' ORDER BY id DESC LIMIT 1), '')::text;

-- name: InsertAuditLogChained :one
INSERT INTO audit_log (admin_id, action, target_type, target_id, diff_before, diff_after,
                       ip_prefix, request_id, created_at, prev_hash, hash)
VALUES (@admin_id, @action, @target_type, @target_id, @diff_before, @diff_after,
        @ip_prefix, @request_id, @created_at, @prev_hash, @hash)
RETURNING id;

-- name: ListAuditLogs :many
-- 后台审计列表（docs/06 第 34 节）：搜操作人、动作或对象；可按动作、对象类型、操作人与日期筛选。
-- 不返回 diff_before / diff_after：改动内容可能很大，列表只给摘要，详情单独取。
SELECT a.id, a.admin_id, u.username, a.action, a.target_type, a.target_id,
       a.ip_prefix, a.request_id, a.created_at, (a.hash <> '')::boolean AS chained,
       (a.diff_before IS NOT NULL OR a.diff_after IS NOT NULL)::boolean AS has_diff
FROM audit_log a
JOIN admin_user u ON u.id = a.admin_id
WHERE (@pattern::text = '' OR u.username ILIKE @pattern OR a.action ILIKE @pattern
       OR a.target_type ILIKE @pattern OR a.target_id ILIKE @pattern)
  AND (@action::text = '' OR a.action = @action)
  AND (@target_type::text = '' OR a.target_type = @target_type)
  AND (@admin_id::bigint = 0 OR a.admin_id = @admin_id)
  AND (@from_at::timestamptz IS NULL OR a.created_at >= @from_at)
  AND (@to_at::timestamptz IS NULL OR a.created_at < @to_at)
ORDER BY a.created_at DESC, a.id DESC
LIMIT @lim OFFSET @off;

-- name: CountAuditLogs :one
SELECT count(*)
FROM audit_log a
JOIN admin_user u ON u.id = a.admin_id
WHERE (@pattern::text = '' OR u.username ILIKE @pattern OR a.action ILIKE @pattern
       OR a.target_type ILIKE @pattern OR a.target_id ILIKE @pattern)
  AND (@action::text = '' OR a.action = @action)
  AND (@target_type::text = '' OR a.target_type = @target_type)
  AND (@admin_id::bigint = 0 OR a.admin_id = @admin_id)
  AND (@from_at::timestamptz IS NULL OR a.created_at >= @from_at)
  AND (@to_at::timestamptz IS NULL OR a.created_at < @to_at);

-- name: GetAuditLog :one
-- 单条详情，含改动前后。
SELECT a.id, a.admin_id, u.username, a.action, a.target_type, a.target_id,
       a.diff_before, a.diff_after, a.ip_prefix, a.request_id, a.created_at,
       a.prev_hash, a.hash
FROM audit_log a
JOIN admin_user u ON u.id = a.admin_id
WHERE a.id = $1;

-- name: AuditFacets :many
-- 筛选下拉的可选项：出现过的动作与对象类型（docs/06 说对象约 32 种，实际取决于已发生的操作）。
SELECT 'action'::text AS kind, action AS value, count(*) AS n FROM audit_log GROUP BY action
UNION ALL
SELECT 'target_type'::text, target_type, count(*) FROM audit_log WHERE target_type <> '' GROUP BY target_type
ORDER BY kind, n DESC;

-- name: AuditAdmins :many
-- 筛选下拉的操作人：在审计里出现过的管理员。
SELECT u.id, u.username, count(*) AS n
FROM audit_log a
JOIN admin_user u ON u.id = a.admin_id
GROUP BY u.id, u.username
ORDER BY n DESC;

-- name: ListAuditChain :many
-- 校验用：按 id 升序取带哈希的记录及其全部被哈希字段，逐条重算比对。
SELECT id, admin_id, action, target_type, target_id, diff_before, diff_after,
       ip_prefix, request_id, created_at, prev_hash, hash
FROM audit_log
WHERE hash <> '' AND id > @after_id
ORDER BY id
LIMIT @lim;

-- name: CountUnchainedAudit :one
-- 启用哈希链之前的记录数（不参与校验，单独报告）。
SELECT count(*) FROM audit_log WHERE hash = '';

-- name: AuditChainStats :one
-- 校验概览：带哈希的记录数与最早、最晚时间。
SELECT count(*) AS chained,
       coalesce(min(created_at), now())::timestamptz AS first_at,
       coalesce(max(created_at), now())::timestamptz AS last_at
FROM audit_log
WHERE hash <> '';
