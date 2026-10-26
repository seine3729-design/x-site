-- 域名与证书（docs/02 第 6.4 节，A4-4 / S4-8）：入口域名池、签名清单发布历史、健康日表。

-- name: ListDomains :many
-- 不分页：入口域名池规模很小（数十条），一次全部返回，前端按角色分组展示。
-- pattern 是已转义的 ILIKE 模式（匹配域名或备注）；status / role 为空表示不按该项筛。
SELECT *
FROM domain_entry
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(role)::text IS NULL OR role = sqlc.narg(role))
  AND (sqlc.narg(pattern)::text IS NULL OR host ILIKE sqlc.narg(pattern) OR note ILIKE sqlc.narg(pattern))
ORDER BY role, weight DESC, id;

-- name: GetDomain :one
SELECT * FROM domain_entry WHERE id = $1;

-- name: CreateDomain :one
INSERT INTO domain_entry (host, role, weight, region, cdn, dns_provider, cert_expires_at, status, note)
VALUES (@host, @role, @weight, @region, @cdn, @dns_provider, @cert_expires_at, @status, @note)
RETURNING *;

-- name: UpdateDomain :one
UPDATE domain_entry
SET host = @host, role = @role, weight = @weight, region = @region, cdn = @cdn,
    dns_provider = @dns_provider, cert_expires_at = @cert_expires_at, status = @status, note = @note,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteDomain :one
DELETE FROM domain_entry WHERE id = $1 RETURNING *;

-- name: UpdateDomainCertExpiry :one
-- 上传证书后回填到期日（只改这一列，不动其它字段）。
UPDATE domain_entry
SET cert_expires_at = @cert_expires_at, updated_at = now()
WHERE id = @id
RETURNING *;

-- 签名清单发布历史 ----------------------------------------------------------

-- name: ListDomainReleases :many
SELECT r.version, r.payload, r.signature, r.note, r.published_by, r.published_at,
       a.username AS published_by_name
FROM domain_list_release r
LEFT JOIN admin_user a ON a.id = r.published_by
ORDER BY r.version DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountDomainReleases :one
SELECT count(*) FROM domain_list_release;

-- name: GetDomainRelease :one
SELECT r.version, r.payload, r.signature, r.note, r.published_by, r.published_at,
       a.username AS published_by_name
FROM domain_list_release r
LEFT JOIN admin_user a ON a.id = r.published_by
WHERE r.version = $1;

-- name: LatestDomainRelease :one
SELECT r.version, r.payload, r.signature, r.note, r.published_by, r.published_at,
       a.username AS published_by_name
FROM domain_list_release r
LEFT JOIN admin_user a ON a.id = r.published_by
ORDER BY r.version DESC
LIMIT 1;

-- name: NextReleaseVersion :one
-- 下一个清单版本号：当前最大版本 + 1（空表时为 1）。显式 ::bigint 固定类型。
SELECT (COALESCE(max(version), 0) + 1)::bigint AS next FROM domain_list_release;

-- name: InsertDomainRelease :one
-- version 由应用层传入（= 发布时的 NextReleaseVersion，并已校验与 payload.v 一致）。
INSERT INTO domain_list_release (version, payload, signature, note, published_by)
VALUES (@version, @payload, @signature, @note, @published_by)
RETURNING version, payload, signature, note, published_by, published_at;

-- 域名健康日表 --------------------------------------------------------------

-- name: ListDomainHealth :many
-- 返回时间窗口内的明细行，按天倒序；成功率、切换数与失败原因的聚合在领域层完成。
SELECT day, host, region, probe_ok, probe_fail, client_failover_count, fail_reasons
FROM domain_health_daily
WHERE day >= @since
ORDER BY day DESC, host;
