-- 注册用户（S2-1，docs/02 第 5.1 节）。user 是 SQL 保留字，查询里一律用双引号 "user"。

-- name: CreateUser :one
-- 建新用户；用户名唯一键冲突由仓储层映射为 409 CONFLICT。
INSERT INTO "user" (username, nickname, password_hash)
VALUES ($1, $2, $3)
RETURNING id, username, nickname, avatar, bio, status, locale, created_at;

-- name: UserByUsername :one
-- 登录用：按用户名（已规范化为小写）取用户，含密码哈希用于校验。回收站中的账号视为不存在。
SELECT id, username, nickname, avatar, bio, status, locale, password_hash, created_at
FROM "user"
WHERE username = $1 AND deleted_at IS NULL;

-- name: UserByID :one
-- 改密 / 鉴权用：按 ID 取用户，含密码哈希。回收站中的账号视为不存在。
SELECT id, username, nickname, avatar, bio, status, locale, password_hash, created_at
FROM "user"
WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateUserPassword :exec
-- 改密：只更新哈希；会话吊销在领域层经 identity 完成。
UPDATE "user"
SET password_hash = $2
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetUserProfile :one
-- 读可编辑资料（GET /me/profile）。
SELECT nickname, avatar, bio, nickname_status, bio_status, avatar_status, locale
FROM "user"
WHERE id = $1 AND deleted_at IS NULL;

-- name: UpdateUserProfile :one
-- 改资料（PATCH /me/profile）：narg 为 NULL 的字段不改。S2-3 不动审核状态。
UPDATE "user" SET
    nickname = COALESCE(sqlc.narg('nickname'), nickname),
    avatar   = COALESCE(sqlc.narg('avatar'), avatar),
    bio      = COALESCE(sqlc.narg('bio'), bio)
WHERE id = @id AND deleted_at IS NULL
RETURNING nickname, avatar, bio, nickname_status, bio_status, avatar_status, locale;

-- name: UserMuted :one
-- 是否禁止发评论 / 弹幕，供发表前校验（S2-5）。回收站中的账号视为不存在。
-- banned 也算：封禁是比禁言更重的处置，只挡登录却放行手上未过期的 Access Token 继续发言，
-- 等于封禁在 15 分钟内形同虚设（docs/11 S2-7 要求封禁即时生效）。
SELECT (status IN ('muted', 'banned'))::boolean FROM "user" WHERE id = $1 AND deleted_at IS NULL;
