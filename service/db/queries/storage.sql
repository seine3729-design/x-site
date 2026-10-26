-- 存储源（docs/12 第 2、7 节，S1-6a）。
-- 凭据密文按「有新值才更新」：修改时若未重新输入凭据，业务层回填原密文，这里总是整行写入。

-- name: ListStorageProviders :many
-- pattern 是已转义的 ILIKE 模式（匹配名称或桶名）；为空表示不按文本搜。
SELECT *
FROM storage_provider
WHERE (sqlc.narg(pattern)::text IS NULL OR name ILIKE sqlc.narg(pattern) OR bucket ILIKE sqlc.narg(pattern))
  AND (sqlc.narg(type)::text IS NULL OR type = sqlc.narg(type))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountStorageProviders :one
SELECT count(*)
FROM storage_provider
WHERE (sqlc.narg(pattern)::text IS NULL OR name ILIKE sqlc.narg(pattern) OR bucket ILIKE sqlc.narg(pattern))
  AND (sqlc.narg(type)::text IS NULL OR type = sqlc.narg(type))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status));

-- name: GetStorageProvider :one
SELECT * FROM storage_provider WHERE id = $1;

-- name: CreateStorageProvider :one
INSERT INTO storage_provider (
    name, type, subtype, purpose, role, endpoint, region, bucket, account_id, path_style, path_prefix,
    credential_enc, read_credential_enc, cred_tail, read_cred_tail, max_presign_ttl, status
) VALUES (
    @name, @type, @subtype, @purpose, @role, @endpoint, @region, @bucket, @account_id, @path_style, @path_prefix,
    @credential_enc, @read_credential_enc, @cred_tail, @read_cred_tail, @max_presign_ttl, @status
)
RETURNING *;

-- name: UpdateStorageProvider :one
UPDATE storage_provider
SET name = @name, type = @type, subtype = @subtype, purpose = @purpose, role = @role, endpoint = @endpoint,
    region = @region, bucket = @bucket, account_id = @account_id, path_style = @path_style, path_prefix = @path_prefix,
    credential_enc = @credential_enc, read_credential_enc = @read_credential_enc,
    cred_tail = @cred_tail, read_cred_tail = @read_cred_tail, max_presign_ttl = @max_presign_ttl, status = @status,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteStorageProvider :one
DELETE FROM storage_provider WHERE id = $1 RETURNING *;

-- ---------- 线路与地区规则（S1-6b）----------

-- name: ListStorageLines :many
SELECT *
FROM storage_line
WHERE (sqlc.narg(provider_id)::bigint IS NULL OR provider_id = sqlc.narg(provider_id))
  AND (sqlc.narg(kind)::text IS NULL OR kind = sqlc.narg(kind))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY provider_id, priority DESC, id;

-- name: GetStorageLine :one
SELECT * FROM storage_line WHERE id = $1;

-- name: CreateStorageLine :one
INSERT INTO storage_line (
    provider_id, kind, name, base_url, access_mode, sign_type, sign_key_ref, token_ttl,
    priority, weight, region_scope, video_process, probe_path, status, note
) VALUES (
    @provider_id, @kind, @name, @base_url, @access_mode, @sign_type, @sign_key_ref, @token_ttl,
    @priority, @weight, @region_scope, @video_process, @probe_path, @status, @note
)
RETURNING *;

-- name: UpdateStorageLine :one
UPDATE storage_line
SET kind = @kind, name = @name, base_url = @base_url, access_mode = @access_mode, sign_type = @sign_type,
    sign_key_ref = @sign_key_ref, token_ttl = @token_ttl, priority = @priority, weight = @weight,
    region_scope = @region_scope, video_process = @video_process, probe_path = @probe_path, status = @status,
    note = @note, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteStorageLine :one
DELETE FROM storage_line WHERE id = $1 RETURNING *;

-- name: ListLineRules :many
SELECT * FROM storage_line_rule WHERE line_id = $1 ORDER BY id;

-- name: ListLineRulesByProvider :many
SELECT r.*
FROM storage_line_rule r
JOIN storage_line l ON l.id = r.line_id
WHERE l.provider_id = $1
ORDER BY r.line_id, r.id;

-- name: DeleteLineRules :exec
DELETE FROM storage_line_rule WHERE line_id = $1;

-- name: CreateLineRule :one
INSERT INTO storage_line_rule (line_id, country, province, isp, asn, effect, boost)
VALUES (@line_id, @country, @province, @isp, @asn, @effect, @boost)
RETURNING *;

-- ---------- 存储全局设置（S1-6d 设置部分）----------

-- name: GetStorageSettings :one
SELECT * FROM storage_settings WHERE id = 1;

-- name: UpdateStorageSettings :one
UPDATE storage_settings
SET candidate_count = @candidate_count, default_ttl = @default_ttl, smart_routing = @smart_routing,
    primary_video_provider_id = @primary_video_provider_id, primary_image_provider_id = @primary_image_provider_id,
    replicate_on_upload = @replicate_on_upload, updated_at = now()
WHERE id = 1
RETURNING *;
