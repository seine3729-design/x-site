-- 反馈社区第二批（迁移 00016）：议题区、回答与「有道理」、开放提交接口的 API Key。

-- ---------- 议题（后台） ----------

-- name: CreateFbTopic :one
INSERT INTO fb_topic (project_id, title, body, answer_hint, status, visibility, pinned, ends_at, published_at, created_by)
VALUES (@project_id, @title, @body, @answer_hint, @status, @visibility, @pinned, sqlc.narg(ends_at), sqlc.narg(published_at), sqlc.narg(created_by))
RETURNING *;

-- name: UpdateFbTopic :one
UPDATE fb_topic
SET title = @title, body = @body, answer_hint = @answer_hint, status = @status, visibility = @visibility,
    pinned = @pinned, ends_at = sqlc.narg(ends_at), published_at = sqlc.narg(published_at), updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeleteFbTopic :one
DELETE FROM fb_topic WHERE id = $1 RETURNING *;

-- name: GetFbTopic :one
SELECT t.*, p.name AS project_name, p.slug AS project_slug
FROM fb_topic t
JOIN fb_project p ON p.id = t.project_id
WHERE t.id = $1;

-- name: ListFbTopics :many
SELECT t.*, p.name AS project_name, p.slug AS project_slug
FROM fb_topic t
JOIN fb_project p ON p.id = t.project_id
WHERE (sqlc.narg(project_id)::bigint IS NULL OR t.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(status)::text IS NULL OR t.status = sqlc.narg(status))
  AND (sqlc.narg(pattern)::text IS NULL OR t.title ILIKE sqlc.narg(pattern))
ORDER BY t.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountFbTopics :one
SELECT count(*) FROM fb_topic t
WHERE (sqlc.narg(project_id)::bigint IS NULL OR t.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(status)::text IS NULL OR t.status = sqlc.narg(status))
  AND (sqlc.narg(pattern)::text IS NULL OR t.title ILIKE sqlc.narg(pattern));

-- ---------- 议题（前台） ----------

-- name: ListVisibleFbTopics :many
-- 征集中优先、置顶优先，再按发布时间。草稿不下发；成员可见议题要求已加入项目。
SELECT t.*, p.name AS project_name, p.slug AS project_slug, p.cover AS project_cover
FROM fb_topic t
JOIN fb_project p ON p.id = t.project_id
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
WHERE t.status IN ('open', 'closed')
  AND p.status <> 'draft'
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (t.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (sqlc.narg(project_id)::bigint IS NULL OR t.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(status)::text IS NULL OR t.status = sqlc.narg(status))
ORDER BY (t.status = 'open') DESC, t.pinned DESC, t.published_at DESC, t.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountVisibleFbTopics :one
SELECT count(*)
FROM fb_topic t
JOIN fb_project p ON p.id = t.project_id
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
WHERE t.status IN ('open', 'closed')
  AND p.status <> 'draft'
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (t.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (sqlc.narg(project_id)::bigint IS NULL OR t.project_id = sqlc.narg(project_id))
  AND (sqlc.narg(status)::text IS NULL OR t.status = sqlc.narg(status));

-- name: GetVisibleFbTopic :one
SELECT t.*, p.name AS project_name, p.slug AS project_slug, p.cover AS project_cover,
       (m.user_id IS NOT NULL)::boolean AS is_member
FROM fb_topic t
JOIN fb_project p ON p.id = t.project_id
LEFT JOIN fb_project_member m ON m.project_id = p.id AND m.user_id = sqlc.narg(viewer_id)::bigint AND m.status = 'active'
WHERE t.id = @id
  AND t.status IN ('open', 'closed')
  AND p.status <> 'draft'
  AND (p.visibility = 'public' OR m.user_id IS NOT NULL)
  AND (t.visibility = 'public' OR m.user_id IS NOT NULL);

-- ---------- 回答 ----------

-- name: CreateFbTopicAnswer :one
INSERT INTO fb_topic_answer (topic_id, parent_id, user_id, body)
VALUES (@topic_id, sqlc.narg(parent_id), @user_id, @body)
RETURNING *;

-- name: GetFbTopicAnswer :one
SELECT * FROM fb_topic_answer WHERE id = $1;

-- name: GetFbUserBadge :one
-- 回答的展示署名。写入类查询用 RETURNING * 拿不到关联的用户信息，
-- 发布回答、点「有道理」、设精选之后用这个补齐，保证返回对象与列表里的一条长得一样。
SELECT tester_no, COALESCE(handle, '') AS handle, COALESCE(nickname, '') AS nickname
FROM fb_user WHERE id = $1;

-- name: ListVisibleFbAnswers :many
-- 精选优先、再按「有道理」数，最后按时间。隐藏的回答只对后台可见，这里不下发。
SELECT a.*, u.tester_no, COALESCE(u.handle, '') AS handle, COALESCE(u.nickname, '') AS nickname,
       (h.user_id IS NOT NULL)::boolean AS helpful_by_me
FROM fb_topic_answer a
LEFT JOIN fb_user u ON u.id = a.user_id
LEFT JOIN fb_answer_helpful h ON h.answer_id = a.id AND h.user_id = sqlc.narg(viewer_id)::bigint
WHERE a.topic_id = @topic_id AND a.status = 'visible'
ORDER BY a.featured DESC, a.helpful_count DESC, a.id
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountVisibleFbAnswers :one
SELECT count(*) FROM fb_topic_answer WHERE topic_id = @topic_id AND status = 'visible';

-- name: ListAdminFbAnswers :many
SELECT a.*, u.tester_no, COALESCE(u.handle, '') AS handle, COALESCE(u.nickname, '') AS nickname
FROM fb_topic_answer a
LEFT JOIN fb_user u ON u.id = a.user_id
WHERE a.topic_id = @topic_id
ORDER BY a.featured DESC, a.helpful_count DESC, a.id
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountAdminFbAnswers :one
SELECT count(*) FROM fb_topic_answer WHERE topic_id = @topic_id;

-- name: SetFbAnswerFeatured :one
UPDATE fb_topic_answer SET featured = @featured, updated_at = now() WHERE id = @id RETURNING *;

-- name: SetFbAnswerStatus :one
UPDATE fb_topic_answer SET status = @status, updated_at = now() WHERE id = @id RETURNING *;

-- name: SyncFbTopicAnswerCount :one
-- 回答数以明细为准重算（只算可见的顶层回答与回复）。
UPDATE fb_topic
SET answer_count = (SELECT count(*) FROM fb_topic_answer x WHERE x.topic_id = fb_topic.id AND x.status = 'visible'),
    updated_at = now()
WHERE fb_topic.id = @id
RETURNING *;

-- ---------- 「有道理」 ----------

-- name: CreateFbAnswerHelpful :exec
INSERT INTO fb_answer_helpful (answer_id, user_id) VALUES (@answer_id, @user_id)
ON CONFLICT (answer_id, user_id) DO NOTHING;

-- name: DeleteFbAnswerHelpful :exec
DELETE FROM fb_answer_helpful WHERE answer_id = @answer_id AND user_id = @user_id;

-- name: SyncFbAnswerHelpful :one
UPDATE fb_topic_answer
SET helpful_count = (SELECT count(*) FROM fb_answer_helpful x WHERE x.answer_id = fb_topic_answer.id), updated_at = now()
WHERE fb_topic_answer.id = @id
RETURNING *;

-- ---------- 开放提交接口的 API Key ----------

-- name: CreateFbApiKey :one
INSERT INTO fb_api_key (project_id, name, key_hash, key_hint, default_visibility, allowed_origins, daily_limit, status, created_by)
VALUES (@project_id, @name, @key_hash, @key_hint, @default_visibility, @allowed_origins, @daily_limit, @status, @created_by)
RETURNING *;

-- name: UpdateFbApiKey :one
UPDATE fb_api_key
SET name = @name, default_visibility = @default_visibility, allowed_origins = @allowed_origins,
    daily_limit = @daily_limit, status = @status, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: RevokeFbApiKey :one
UPDATE fb_api_key SET revoked_at = now(), status = 'inactive', updated_at = now()
WHERE id = @id AND revoked_at IS NULL
RETURNING *;

-- name: GetFbApiKey :one
SELECT k.*, p.name AS project_name, p.slug AS project_slug
FROM fb_api_key k JOIN fb_project p ON p.id = k.project_id
WHERE k.id = $1;

-- name: GetFbApiKeyByHash :one
-- 开放接口鉴权用：只取可用的 Key。
SELECT k.*, p.name AS project_name, p.slug AS project_slug, p.status AS project_status
FROM fb_api_key k JOIN fb_project p ON p.id = k.project_id
WHERE k.key_hash = @key_hash AND k.revoked_at IS NULL AND k.status = 'active';

-- name: ListFbApiKeys :many
SELECT k.*, p.name AS project_name, p.slug AS project_slug
FROM fb_api_key k JOIN fb_project p ON p.id = k.project_id
WHERE (sqlc.narg(project_id)::bigint IS NULL OR k.project_id = sqlc.narg(project_id))
ORDER BY k.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountFbApiKeys :one
SELECT count(*) FROM fb_api_key
WHERE (sqlc.narg(project_id)::bigint IS NULL OR project_id = sqlc.narg(project_id));

-- name: TouchFbApiKey :exec
UPDATE fb_api_key SET used_count = used_count + 1, last_used_at = now() WHERE id = @id;

-- ---------- 外部提交的反馈 ----------

-- name: CreateFbFeedbackFromAPI :one
INSERT INTO fb_feedback (project_id, number, kind, title, body, env, visibility, source, api_key_id, contact, page_url)
VALUES (@project_id, @number, @kind, @title, @body, @env, @visibility, 'open_api', @api_key_id, @contact, @page_url)
RETURNING *;
