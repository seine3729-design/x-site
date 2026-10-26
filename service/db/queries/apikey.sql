-- name: ListAPIKeys :many
-- 后台列表（docs/06 第 32 节）：搜用途、使用方或密钥开头几位；按状态筛选。
SELECT id, name, owner, key_hint, scopes, status, used_count, last_used_at, created_by, created_at, updated_at
FROM api_key
WHERE (@pattern::text = '' OR name ILIKE @pattern OR owner ILIKE @pattern OR key_hint ILIKE @pattern)
  AND (@status::text = '' OR status = @status)
ORDER BY id DESC
LIMIT @lim OFFSET @off;

-- name: CountAPIKeys :one
SELECT count(*)
FROM api_key
WHERE (@pattern::text = '' OR name ILIKE @pattern OR owner ILIKE @pattern OR key_hint ILIKE @pattern)
  AND (@status::text = '' OR status = @status);

-- name: CreateAPIKey :one
INSERT INTO api_key (name, owner, key_hash, key_hint, scopes, created_by)
VALUES (@name, @owner, @key_hash, @key_hint, @scopes, @created_by)
RETURNING id, name, owner, key_hint, scopes, status, used_count, last_used_at, created_by, created_at, updated_at;

-- name: SetAPIKeyStatus :one
-- 停用（即时失效）与重新启用。
UPDATE api_key SET status = @status, updated_at = now()
WHERE id = @id
RETURNING id, name, owner, key_hint, scopes, status, used_count, last_used_at, created_by, created_at, updated_at;

-- name: GetAPIKey :one
SELECT id, name, owner, key_hint, scopes, status, used_count, last_used_at, created_by, created_at, updated_at
FROM api_key
WHERE id = $1;

-- name: UpdateAPIKey :one
-- 改用途与使用方（密钥本身与权限范围不可改：改范围等于换一把密钥的能力，应当重新生成）。
UPDATE api_key SET name = @name, owner = @owner, updated_at = now()
WHERE id = @id
RETURNING id, name, owner, key_hint, scopes, status, used_count, last_used_at, created_by, created_at, updated_at;

-- name: AuthenticateAPIKey :one
-- 调用方鉴权（S4-6 接入时使用）：按哈希查，只认启用中的密钥。
SELECT id, name, scopes
FROM api_key
WHERE key_hash = @key_hash AND status = 'active';

-- name: TouchAPIKeyUsage :exec
-- 记录一次调用。与鉴权分开：鉴权在请求路径上，计数可以异步或批量落库。
UPDATE api_key SET used_count = used_count + 1, last_used_at = now() WHERE id = $1;
