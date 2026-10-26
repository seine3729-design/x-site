-- 资源与任务域（docs/02 第 4 节）：资源机 FTP（S1-6）。

-- name: ListFTPServers :many
-- pattern 是已转义的 ILIKE 模式（匹配名称或地址）；为空表示不按文本搜。
SELECT *
FROM ftp_server
WHERE (sqlc.narg(pattern)::text IS NULL OR name ILIKE sqlc.narg(pattern) OR host ILIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountFTPServers :one
SELECT count(*)
FROM ftp_server
WHERE (sqlc.narg(pattern)::text IS NULL OR name ILIKE sqlc.narg(pattern) OR host ILIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status));

-- name: GetFTPServer :one
SELECT * FROM ftp_server WHERE id = $1;

-- name: CreateFTPServer :one
INSERT INTO ftp_server (name, host, port, tls, passive, cred_ref, status)
VALUES (@name, @host, @port, @tls, @passive, @cred_ref, @status)
RETURNING *;

-- name: UpdateFTPServer :one
UPDATE ftp_server
SET name = @name, host = @host, port = @port, tls = @tls, passive = @passive, cred_ref = @cred_ref, status = @status,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteFTPServer :one
DELETE FROM ftp_server WHERE id = $1 RETURNING *;
