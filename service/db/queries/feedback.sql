-- 反馈社区（feedback/docs/03，迁移 00012）。
-- 约定：pattern 是业务层已转义的 ILIKE 模式；可见性过滤写在 SQL 里，不在应用层事后筛。

-- ---------- 社区用户 ----------

-- name: CreateFbUser :one
-- 不写 recovery_code_hash：恢复码已删除（adr/0011），该列走 DEFAULT ''。
INSERT INTO fb_user (handle, handle_norm, password_hash, nickname)
VALUES (@handle, lower(@handle), @password_hash, @nickname)
RETURNING *;

-- name: GetFbUserByHandle :one
SELECT * FROM fb_user WHERE handle_norm = lower(@handle);

-- name: GetFbUser :one
SELECT * FROM fb_user WHERE id = $1;

-- name: UpdateFbUserSecret :one
-- 改密：让旧会话全部失效。没有「用恢复码重置」这条路了（adr/0011）。
UPDATE fb_user
SET password_hash = @password_hash,
    session_version = session_version + 1, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UpdateFbUserProfile :one
UPDATE fb_user
SET nickname = @nickname, bio = @bio, show_level = @show_level, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: UpdateFbUserStatus :one
-- 后台停用/恢复社区账号：停用时让会话立即失效。
UPDATE fb_user
SET status = @status,
    session_version = CASE WHEN @status::text = 'active' THEN session_version ELSE session_version + 1 END,
    updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ListFbUsers :many
SELECT * FROM fb_user
WHERE (sqlc.narg(pattern)::text IS NULL OR handle ILIKE sqlc.narg(pattern) OR nickname ILIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountFbUsers :one
SELECT count(*) FROM fb_user
WHERE (sqlc.narg(pattern)::text IS NULL OR handle ILIKE sqlc.narg(pattern) OR nickname ILIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status));

-- ---------- 邀请码 ----------

-- name: CreateFbInvite :one
INSERT INTO fb_invite (code_hash, code_hint, project_id, role, max_uses, note, expires_at, created_by)
VALUES (@code_hash, @code_hint, sqlc.narg(project_id), @role, @max_uses, @note, sqlc.narg(expires_at), @created_by)
RETURNING *;

-- name: LockFbInviteByHash :one
-- 核销前锁行，避免并发超额使用。
SELECT * FROM fb_invite WHERE code_hash = @code_hash FOR UPDATE;

-- name: ConsumeFbInvite :one
UPDATE fb_invite SET used_count = used_count + 1 WHERE id = @id RETURNING *;

-- name: CreateFbInviteRedemption :exec
INSERT INTO fb_invite_redemption (invite_id, user_id) VALUES (@invite_id, @user_id);

-- name: RevokeFbInvite :one
UPDATE fb_invite SET revoked_at = now() WHERE id = @id AND revoked_at IS NULL RETURNING *;

-- name: GetFbInvite :one
SELECT * FROM fb_invite WHERE id = $1;

-- name: ListFbInvites :many
SELECT i.*, p.name AS project_name
FROM fb_invite i
LEFT JOIN fb_project p ON p.id = i.project_id
WHERE (sqlc.narg(project_id)::bigint IS NULL OR i.project_id = sqlc.narg(project_id))
ORDER BY i.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountFbInvites :one
SELECT count(*) FROM fb_invite
WHERE (sqlc.narg(project_id)::bigint IS NULL OR project_id = sqlc.narg(project_id));

-- ---------- 项目 ----------

-- name: CreateFbProject :one
INSERT INTO fb_project (slug, key_prefix, name, summary, description, cover, try_url, status, visibility, join_mode, sort)
VALUES (@slug, @key_prefix, @name, @summary, @description, @cover, @try_url, @status, @visibility, @join_mode, @sort)
RETURNING *;

-- name: UpdateFbProject :one
UPDATE fb_project
SET slug = @slug, key_prefix = @key_prefix, name = @name, summary = @summary, description = @description,
    cover = @cover, try_url = @try_url, status = @status, visibility = @visibility, join_mode = @join_mode,
    sort = @sort, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteFbProject :one
DELETE FROM fb_project WHERE id = $1 RETURNING *;

-- name: GetFbProject :one
SELECT * FROM fb_project WHERE id = $1;

-- name: GetFbProjectBySlug :one
SELECT * FROM fb_project WHERE slug = $1;

-- name: ListFbProjects :many
SELECT * FROM fb_project
WHERE (sqlc.narg(pattern)::text IS NULL OR name ILIKE sqlc.narg(pattern) OR slug ILIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY sort DESC, id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountFbProjects :one
SELECT count(*) FROM fb_project
WHERE (sqlc.narg(pattern)::text IS NULL OR name ILIKE sqlc.narg(pattern) OR slug ILIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status));

-- name: ListVisibleFbProjects :many
-- 前台项目目录：公开项目对所有人可见；成员项目只对已加入的用户可见。草稿与归档不列出。
SELECT p.*, (m.user_id IS NOT NULL)::boolean AS joined,
       (SELECT count(*) FROM fb_project_member x WHERE x.project_id = p.id AND x.status = 'active') AS member_count,
       (SELECT count(*) FROM fb_feedback f WHERE f.project_id = p.id) AS feedback_count
FROM fb_project p
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
WHERE p.status IN ('recruiting', 'active', 'paused')
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL)
ORDER BY p.sort DESC, p.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountVisibleFbProjects :one
-- 可见项目总数。筛选条件必须和 ListVisibleFbProjects 完全一致，
-- 否则分页器会显示出一个翻不到的页码。
SELECT count(*)
FROM fb_project p
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
WHERE p.status IN ('recruiting', 'active', 'paused')
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL);

-- name: GetVisibleFbProject :one
-- 关联成员行时**不**按 status 过滤，这样「申请中」（pending）也能被看到，
-- 前台才能把按钮从「申请加入」换成「申请中，等待确认」。
-- 但可见性与 joined 仍然只认 active：待审的人不该看到仅成员项目的内容。
--
-- joined 必须套 COALESCE。这是 LEFT JOIN：匿名访客与未加入的用户没有成员行，
-- m.status 是 NULL，而 SQL 三值逻辑下 `NULL = 'active'` 的结果是 **NULL 而不是 false**。
-- 直接写 `(m.status = 'active')::boolean` 会让这一列返回 NULL，Go 侧按 bool 扫描时
-- 报 `cannot scan NULL into *bool`，整个项目详情接口 500。
-- 隔壁 ListVisibleFbProjects 用的是 `IS NOT NULL`，那种写法天然不会是 NULL。
SELECT p.*, COALESCE(m.status = 'active', false)::boolean AS joined, COALESCE(m.role, '') AS member_role,
       COALESCE(m.status, '') AS member_status,
       (SELECT count(*) FROM fb_project_member x WHERE x.project_id = p.id AND x.status = 'active') AS member_count,
       (SELECT count(*) FROM fb_feedback f WHERE f.project_id = p.id) AS feedback_count
FROM fb_project p
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint
WHERE p.slug = @slug
  AND p.status <> 'draft'
  AND (p.visibility = 'public' OR m.status = 'active');

-- name: NextFbFeedbackNumber :one
-- 事务内分配项目内编号。
UPDATE fb_project SET next_number = next_number + 1, updated_at = now()
WHERE id = @id
RETURNING (next_number - 1)::bigint AS number, key_prefix;

-- ---------- 成员 ----------

-- name: UpsertFbMember :one
INSERT INTO fb_project_member (project_id, user_id, role, status)
VALUES (@project_id, @user_id, @role, @status)
ON CONFLICT (project_id, user_id) DO UPDATE
SET role = EXCLUDED.role, status = EXCLUDED.status
RETURNING *;

-- name: GetFbMember :one
SELECT * FROM fb_project_member WHERE project_id = @project_id AND user_id = @user_id;

-- name: ListFbMembers :many
SELECT m.*, u.handle, u.nickname, u.status AS user_status
FROM fb_project_member m
JOIN fb_user u ON u.id = m.user_id
WHERE m.project_id = @project_id AND m.status <> 'removed'
ORDER BY m.joined_at DESC, m.user_id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountFbMembers :one
SELECT count(*) FROM fb_project_member WHERE project_id = @project_id AND status <> 'removed';

-- ---------- 项目动态 ----------

-- name: CreateFbPost :one
INSERT INTO fb_post (project_id, kind, title, body, status, visibility, pinned, published_at)
VALUES (@project_id, @kind, @title, @body, @status, @visibility, @pinned, sqlc.narg(published_at))
RETURNING *;

-- name: UpdateFbPost :one
UPDATE fb_post
SET kind = @kind, title = @title, body = @body, status = @status, visibility = @visibility,
    pinned = @pinned, published_at = sqlc.narg(published_at), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteFbPost :one
DELETE FROM fb_post WHERE id = $1 RETURNING *;

-- name: GetFbPost :one
SELECT * FROM fb_post WHERE id = $1;

-- name: ListFbPosts :many
SELECT po.*, p.name AS project_name, p.slug AS project_slug
FROM fb_post po
JOIN fb_project p ON p.id = po.project_id
WHERE (sqlc.narg(project_id)::bigint IS NULL OR po.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(status)::text IS NULL OR po.status = sqlc.narg(status))
ORDER BY po.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountFbPosts :one
SELECT count(*) FROM fb_post
WHERE (sqlc.narg(project_id)::bigint IS NULL OR project_id = sqlc.narg(project_id))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status));

-- name: ListVisibleFbPosts :many
-- 前台动态流：置顶优先，然后按发布时间。成员可见内容要求已加入项目。
SELECT po.*, p.name AS project_name, p.slug AS project_slug, p.key_prefix
FROM fb_post po
JOIN fb_project p ON p.id = po.project_id
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
WHERE po.status = 'published'
  AND p.status <> 'draft'
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (po.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (sqlc.narg(project_id)::bigint IS NULL OR po.project_id = sqlc.narg(project_id))
ORDER BY po.pinned DESC, po.published_at DESC, po.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountVisibleFbPosts :one
-- 可见动态总数，筛选条件与 ListVisibleFbPosts 保持一致。
SELECT count(*)
FROM fb_post po
JOIN fb_project p ON p.id = po.project_id
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
WHERE po.status = 'published'
  AND p.status <> 'draft'
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (po.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (sqlc.narg(project_id)::bigint IS NULL OR po.project_id = sqlc.narg(project_id));

-- name: GetVisibleFbPost :one
SELECT po.*, p.name AS project_name, p.slug AS project_slug, p.key_prefix
FROM fb_post po
JOIN fb_project p ON p.id = po.project_id
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
WHERE po.id = @id
  AND po.status = 'published'
  AND p.status <> 'draft'
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (po.visibility = 'public' OR m.user_id IS NOT NULL);

-- ---------- 反馈 ----------

-- name: CreateFbFeedback :one
-- 社区用户提交（source 用默认值 community）。page_url 便于定位问题页面。
INSERT INTO fb_feedback (project_id, number, author_id, kind, title, body, env, visibility, page_url)
VALUES (@project_id, @number, @author_id, @kind, @title, @body, @env, @visibility, @page_url)
RETURNING *;

-- name: GetFbFeedback :one
SELECT f.*, p.slug AS project_slug, p.key_prefix, p.name AS project_name,
       COALESCE(u.handle, '') AS author_handle, COALESCE(u.nickname, '') AS author_nickname
FROM fb_feedback f
JOIN fb_project p ON p.id = f.project_id
LEFT JOIN fb_user u ON u.id = f.author_id
WHERE f.id = $1;

-- name: GetFbFeedbackByKey :one
-- 前台按 <slug>/<number> 取详情。可见性过滤写在这里：查不到与无权查看都返回 0 行，
-- 调用方统一按「不存在」处理，不暴露私密反馈是否存在。
SELECT f.*, p.slug AS project_slug, p.key_prefix, p.name AS project_name,
       COALESCE(u.handle, '') AS author_handle, COALESCE(u.nickname, '') AS author_nickname,
       (v.user_id IS NOT NULL)::boolean AS voted
FROM fb_feedback f
JOIN fb_project p ON p.id = f.project_id
LEFT JOIN fb_user u ON u.id = f.author_id
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
LEFT JOIN fb_vote v ON v.feedback_id = f.id AND v.user_id = sqlc.narg(viewer_id)::bigint
WHERE p.slug = @slug AND f.number = @number
  AND p.status <> 'draft'
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (f.visibility = 'public'
       OR (f.visibility = 'members' AND m.user_id IS NOT NULL)
       OR (f.visibility = 'private' AND f.author_id = sqlc.narg(viewer_id)::bigint));

-- name: ListVisibleFbFeedback :many
SELECT f.*, p.slug AS project_slug, p.key_prefix, p.name AS project_name,
       COALESCE(u.handle, '') AS author_handle, COALESCE(u.nickname, '') AS author_nickname,
       (v.user_id IS NOT NULL)::boolean AS voted
FROM fb_feedback f
JOIN fb_project p ON p.id = f.project_id
LEFT JOIN fb_user u ON u.id = f.author_id
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
LEFT JOIN fb_vote v ON v.feedback_id = f.id AND v.user_id = sqlc.narg(viewer_id)::bigint
WHERE p.status <> 'draft'
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (f.visibility = 'public'
       OR (f.visibility = 'members' AND m.user_id IS NOT NULL)
       OR (f.visibility = 'private' AND f.author_id = sqlc.narg(viewer_id)::bigint))
  AND (sqlc.narg(project_id)::bigint IS NULL OR f.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(status)::text IS NULL OR f.status = sqlc.narg(status))
  AND (sqlc.narg(kind)::text IS NULL OR f.kind = sqlc.narg(kind))
  AND (sqlc.narg(author_id)::bigint IS NULL OR f.author_id = sqlc.narg(author_id))
  AND (sqlc.narg(pattern)::text IS NULL OR f.title ILIKE sqlc.narg(pattern))
ORDER BY
    CASE WHEN sqlc.arg(sort_by)::text = 'votes' THEN f.vote_count ELSE 0 END DESC,
    CASE WHEN sqlc.arg(sort_by)::text = 'newest' THEN f.created_at ELSE f.last_activity_at END DESC,
    f.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountVisibleFbFeedback :one
SELECT count(*)
FROM fb_feedback f
JOIN fb_project p ON p.id = f.project_id
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
WHERE p.status <> 'draft'
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (f.visibility = 'public'
       OR (f.visibility = 'members' AND m.user_id IS NOT NULL)
       OR (f.visibility = 'private' AND f.author_id = sqlc.narg(viewer_id)::bigint))
  AND (sqlc.narg(project_id)::bigint IS NULL OR f.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(status)::text IS NULL OR f.status = sqlc.narg(status))
  AND (sqlc.narg(kind)::text IS NULL OR f.kind = sqlc.narg(kind))
  AND (sqlc.narg(author_id)::bigint IS NULL OR f.author_id = sqlc.narg(author_id))
  AND (sqlc.narg(pattern)::text IS NULL OR f.title ILIKE sqlc.narg(pattern));

-- name: ListAdminFbFeedback :many
SELECT f.*, p.slug AS project_slug, p.key_prefix, p.name AS project_name,
       COALESCE(u.handle, '') AS author_handle, COALESCE(u.nickname, '') AS author_nickname
FROM fb_feedback f
JOIN fb_project p ON p.id = f.project_id
LEFT JOIN fb_user u ON u.id = f.author_id
WHERE (sqlc.narg(project_id)::bigint IS NULL OR f.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(status)::text IS NULL OR f.status = sqlc.narg(status))
  AND (sqlc.narg(kind)::text IS NULL OR f.kind = sqlc.narg(kind))
  AND (sqlc.narg(priority)::text IS NULL OR f.priority = sqlc.narg(priority))
  AND (sqlc.narg(visibility)::text IS NULL OR f.visibility = sqlc.narg(visibility))
  AND (sqlc.narg(source)::text IS NULL OR f.source = sqlc.narg(source))
  AND (sqlc.narg(pattern)::text IS NULL OR f.title ILIKE sqlc.narg(pattern))
ORDER BY
    CASE WHEN sqlc.arg(sort_by)::text = 'oldest' THEN 0 ELSE 1 END,
    CASE WHEN sqlc.arg(sort_by)::text = 'oldest' THEN f.created_at END ASC,
    f.last_activity_at DESC,
    f.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountAdminFbFeedback :one
SELECT count(*) FROM fb_feedback f
WHERE (sqlc.narg(project_id)::bigint IS NULL OR f.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(status)::text IS NULL OR f.status = sqlc.narg(status))
  AND (sqlc.narg(kind)::text IS NULL OR f.kind = sqlc.narg(kind))
  AND (sqlc.narg(priority)::text IS NULL OR f.priority = sqlc.narg(priority))
  AND (sqlc.narg(visibility)::text IS NULL OR f.visibility = sqlc.narg(visibility))
  AND (sqlc.narg(source)::text IS NULL OR f.source = sqlc.narg(source))
  AND (sqlc.narg(pattern)::text IS NULL OR f.title ILIKE sqlc.narg(pattern));

-- name: TransitionFbFeedback :one
-- 带乐观锁的状态流转；version 不匹配时返回 0 行，由业务层转成冲突。
UPDATE fb_feedback
SET status = @status, priority = @priority, visibility = @visibility,
    duplicate_of = sqlc.narg(duplicate_of),
    version = version + 1, last_activity_at = now(), updated_at = now()
WHERE id = @id AND version = @expected_version
RETURNING *;

-- name: CountFbFeedbackByStatus :many
SELECT status, count(*) AS total FROM fb_feedback
WHERE (sqlc.narg(project_id)::bigint IS NULL OR project_id = sqlc.narg(project_id))
GROUP BY status;

-- ---------- 状态历史 ----------

-- name: CreateFbFeedbackEvent :one
INSERT INTO fb_feedback_event (feedback_id, from_status, to_status, note, internal_note, actor_admin)
VALUES (@feedback_id, @from_status, @to_status, @note, @internal_note, sqlc.narg(actor_admin))
RETURNING *;

-- name: ListFbFeedbackEvents :many
SELECT e.*, COALESCE(a.username, '') AS actor_name
FROM fb_feedback_event e
LEFT JOIN admin_user a ON a.id = e.actor_admin
WHERE e.feedback_id = $1
ORDER BY e.id;

-- ---------- 投票 ----------

-- name: CreateFbVote :exec
INSERT INTO fb_vote (feedback_id, user_id) VALUES (@feedback_id, @user_id)
ON CONFLICT (feedback_id, user_id) DO NOTHING;

-- name: DeleteFbVote :exec
DELETE FROM fb_vote WHERE feedback_id = @feedback_id AND user_id = @user_id;

-- name: SyncFbFeedbackVotes :one
-- 票数以明细为准重算，重复点击不会把计数算错。
UPDATE fb_feedback
SET vote_count = (SELECT count(*) FROM fb_vote WHERE feedback_id = fb_feedback.id), updated_at = now()
WHERE id = @id
RETURNING *;

-- ---------- 贡献 ----------

-- name: CreateFbContribution :one
INSERT INTO fb_contribution (user_id, project_id, feedback_id, kind, points, reason, source_key)
VALUES (@user_id, sqlc.narg(project_id), sqlc.narg(feedback_id), @kind, @points, @reason, @source_key)
ON CONFLICT (source_key) DO NOTHING
RETURNING *;

-- name: BumpFbUserPoints :one
UPDATE fb_user
SET points = GREATEST(0, points + @delta::int), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: ListFbLeaderboard :many
SELECT u.id, u.handle, u.nickname, u.points, u.show_level
FROM fb_user u
WHERE u.status = 'active' AND u.points > 0 AND u.show_level
ORDER BY u.points DESC, u.id
LIMIT sqlc.arg(page_limit);
