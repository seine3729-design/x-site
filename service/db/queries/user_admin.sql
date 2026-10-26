-- 后台用户管理（S2-7 / A2-2，docs/06 第 26 节）。user 是 SQL 保留字，查询里一律用双引号 "user"。
-- 搜索一律写成 lower(列) LIKE 才能用上 00045 的 pg_bigm 索引（ILIKE 用不上，见 00035 的实测结论），
-- 搜索词在领域层转小写并包成 %词%。回收站里的账号（deleted_at 非空）不进后台列表。

-- name: ListAdminUsers :many
SELECT id, username, nickname, avatar, bio, status, locale, verified_at, created_at
FROM "user"
WHERE deleted_at IS NULL
  AND (@status::text = '' OR status = @status::text)
  AND (@pattern::text = '' OR lower(username) LIKE @pattern::text OR lower(nickname) LIKE @pattern::text)
ORDER BY created_at DESC, id DESC
LIMIT @lim::int OFFSET @off::int;

-- name: CountAdminUsers :one
SELECT count(*)
FROM "user"
WHERE deleted_at IS NULL
  AND (@status::text = '' OR status = @status::text)
  AND (@pattern::text = '' OR lower(username) LIKE @pattern::text OR lower(nickname) LIKE @pattern::text);

-- name: AdminUserByID :one
-- 详情头部：比列表多出资料审核状态与「暂停记录历史」开关，不含密码哈希（后台任何接口都不读它）。
SELECT id, username, nickname, avatar, bio, status, locale,
       nickname_status, bio_status, avatar_status, history_paused, verified_at, created_at
FROM "user"
WHERE id = $1 AND deleted_at IS NULL;

-- name: AdminUserStats :one
-- 详情里的社区足迹，供运营在改状态前判断。docs/06 第 26 节还要求会员状态 / 等级成长值 / 金币余额 /
-- 邀请关系 / 风控标记，这些表要等 S3-10（会员与支付）与 M6（福利体系）建好，这里先不含。
-- 他自己发出的举报数在这里（report_sent）；针对他的举报数见下面的 AdminUserReportAgainst。
SELECT
    (SELECT count(*) FROM comment WHERE user_id = @id::bigint AND deleted_at IS NULL)::bigint AS comment_count,
    (SELECT count(*) FROM danmaku WHERE user_id = @id::bigint)::bigint                        AS danmaku_count,
    (SELECT count(*) FROM report  WHERE reporter_id = @id::bigint)::bigint                    AS report_sent,
    (SELECT count(*) FROM follow        WHERE user_id = @id::bigint)::bigint                  AS follow_count,
    (SELECT count(*) FROM watch_history WHERE user_id = @id::bigint)::bigint                  AS history_count,
    (SELECT count(*) FROM user_session
      WHERE user_id = @id::bigint AND revoked_at IS NULL AND expires_at > now())::bigint      AS session_count;

-- name: AdminUserReportAgainst :one
-- 针对该用户所发评论 / 弹幕的举报数（与他自己发出的举报分开看）。
-- 写成两个平级子查询相加而不是 EXISTS / IN 嵌套：sqlc 对嵌套层里的 user_id 会报歧义
-- （comment 与 danmaku 都有这一列），平级子查询各自成作用域才能通过分析。
-- 参数与结果都显式转型：不加的话 sqlc 推不出 @author_id 的类型（comment 与 danmaku 都有 user_id 列），
-- 两个 count 相加的结果也会被推成 int32。
SELECT (
    (SELECT count(*) FROM report r JOIN comment c ON c.id = r.target_id
      WHERE r.target_type = 'comment' AND c.user_id = @author_id::bigint)
  + (SELECT count(*) FROM report r JOIN danmaku d ON d.id = r.target_id
      WHERE r.target_type = 'danmaku' AND d.user_id = @author_id::bigint)
)::bigint AS report_against;

-- name: SetAdminUserStatus :one
-- 改账号状态。会话吊销在领域层经 identity 完成（封禁要即时踢下线，docs/11 S2-7）。
UPDATE "user" SET status = @status
WHERE id = @id AND deleted_at IS NULL
RETURNING id, username, nickname, avatar, bio, status, locale, verified_at, created_at;

-- name: ListAdminGuests :many
-- 游客设备列表。guest_device 没有账号状态也没有回收站列，后台只读：游客要约束只能用游客互动总开关。
-- merged_user_id 是后来注册并把游客数据并过去的账号。
SELECT g.id, g.first_seen, g.source_channel, g.platform, g.attest_level, g.merged_user_id,
       COALESCE(u.username, '') AS merged_username
FROM guest_device g
LEFT JOIN "user" u ON u.id = g.merged_user_id
WHERE (@pattern::text = '' OR lower(g.id::text) LIKE @pattern::text OR lower(COALESCE(u.username, '')) LIKE @pattern::text)
ORDER BY g.first_seen DESC, g.id DESC
LIMIT @lim::int OFFSET @off::int;

-- name: CountAdminGuests :one
SELECT count(*)
FROM guest_device g
LEFT JOIN "user" u ON u.id = g.merged_user_id
WHERE (@pattern::text = '' OR lower(g.id::text) LIKE @pattern::text OR lower(COALESCE(u.username, '')) LIKE @pattern::text);
