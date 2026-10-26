-- 游客设备与会话（S1-10，docs/02 第 5.1、5.1.1 节）。

-- name: CreateGuestDevice :exec
INSERT INTO guest_device (id, source_channel, platform, attest_level)
VALUES ($1, $2, $3, $4);

-- name: CreateSession :exec
INSERT INTO user_session (
    id, family_id, user_id, guest_device_id, refresh_hash, ua_digest, ip_prefix, platform, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: LockSessionByRefreshHash :one
-- 轮换事务内锁住命中的会话行，串行化同一刷新令牌的并发刷新（并发第二次会看到已吊销 → 判为复用）。
SELECT id, family_id, user_id, guest_device_id, platform, expires_at, revoked_at
FROM user_session
WHERE refresh_hash = $1
FOR UPDATE;

-- name: RevokeSession :execrows
-- 吊销单个会话行（轮换时吊销旧行）。只影响尚未吊销的行。
UPDATE user_session
SET revoked_at = now(), revoke_reason = $2
WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeFamily :execrows
-- 吊销整个会话族（复用检测、登出）。只影响尚未吊销的行。
UPDATE user_session
SET revoked_at = now(), revoke_reason = $2
WHERE family_id = $1 AND revoked_at IS NULL;

-- name: RevokeFamilyByRefreshHash :execrows
-- 按刷新令牌吊销其所在会话族（登出）；令牌即使已被轮换吊销，也能据它找到族。
UPDATE user_session
SET revoked_at = now(), revoke_reason = $2
WHERE family_id = (SELECT s.family_id FROM user_session s WHERE s.refresh_hash = $1)
  AND revoked_at IS NULL;

-- name: ListUserSessions :many
-- 「我的设备」：某注册用户所有未吊销且未过期的会话，按最近活跃倒序（S2-1，docs/03 第 2.2 节）。
SELECT id, ua_digest, ip_prefix, platform, created_at, last_seen_at
FROM user_session
WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > $2
ORDER BY last_seen_at DESC;

-- name: RevokeUserSessionByID :execrows
-- 设备下线：吊销该用户名下的指定会话。带 user_id 作用域，防止吊销他人会话。
UPDATE user_session
SET revoked_at = now(), revoke_reason = $3
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: RevokeAllForUser :execrows
-- 改密 / 封禁：吊销该用户的全部会话。
UPDATE user_session
SET revoked_at = now(), revoke_reason = $2
WHERE user_id = $1 AND revoked_at IS NULL;
