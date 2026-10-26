-- 媒体转封装（S1-5，docs/specs/S1-5.md）：转封装任务、媒体对象、内容密钥。

-- ---------- 入队（API） ----------

-- name: LockEpisodeForTranscode :one
-- 入队前锁住分集，串行化同一集的并发提交；所属的剧在回收站中时查不到。
SELECT e.id, e.drama_id, e.number, e.media_status
FROM episode e
JOIN drama d ON d.id = e.drama_id
WHERE e.id = $1 AND d.deleted_at IS NULL
FOR UPDATE OF e;

-- name: FailStaleTranscodeJobs :exec
-- 超过 stale_seconds 没有更新进度的 running 任务（worker 被杀、机器重启）标为失败，不再占住这一集。
UPDATE transcode_job
SET status = 'failed', error = @error, finished_at = now(), updated_at = now()
WHERE episode_id = @episode_id AND status = 'running'
  AND updated_at < now() - make_interval(secs => @stale_seconds::int);

-- name: CountActiveTranscodeJobs :one
SELECT count(*) FROM transcode_job WHERE episode_id = $1 AND status IN ('queued', 'running');

-- name: LatestTranscodeSourceKey :one
SELECT source_key FROM transcode_job WHERE episode_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1;

-- name: InsertTranscodeJob :exec
INSERT INTO transcode_job (id, episode_id, source_key, force) VALUES (@id, @episode_id, @source_key, @force);

-- name: MarkEpisodeTranscoding :exec
-- 还没有可播放视频的分集显示为「转封装中」；已有视频的分集保持 ready，旧产物在新产物完成前继续可播。
UPDATE episode SET media_status = 'transcoding', updated_at = now()
WHERE id = $1 AND media_status IN ('pending', 'failed');

-- ---------- 查询（后台） ----------

-- name: GetTranscodeJob :one
-- 所属的剧在回收站中时视为不存在。
SELECT j.*, e.drama_id, e.number AS episode_number
FROM transcode_job j
JOIN episode e ON e.id = j.episode_id
JOIN drama d ON d.id = e.drama_id
WHERE j.id = $1 AND d.deleted_at IS NULL;

-- name: ListTranscodeJobs :many
SELECT j.*, e.drama_id, e.number AS episode_number
FROM transcode_job j
JOIN episode e ON e.id = j.episode_id
JOIN drama d ON d.id = e.drama_id
WHERE d.deleted_at IS NULL
  AND (sqlc.narg(episode_id)::bigint IS NULL OR j.episode_id = sqlc.narg(episode_id))
  AND (sqlc.narg(drama_id)::bigint IS NULL OR e.drama_id = sqlc.narg(drama_id))
  AND (sqlc.narg(status)::text IS NULL OR j.status = sqlc.narg(status))
ORDER BY j.created_at DESC, j.id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountTranscodeJobs :one
SELECT count(*)
FROM transcode_job j
JOIN episode e ON e.id = j.episode_id
JOIN drama d ON d.id = e.drama_id
WHERE d.deleted_at IS NULL
  AND (sqlc.narg(episode_id)::bigint IS NULL OR j.episode_id = sqlc.narg(episode_id))
  AND (sqlc.narg(drama_id)::bigint IS NULL OR e.drama_id = sqlc.narg(drama_id))
  AND (sqlc.narg(status)::text IS NULL OR j.status = sqlc.narg(status));

-- ---------- 执行（worker） ----------

-- name: StartTranscodeAttempt :one
-- 开始一次尝试；任务已结束（成功、跳过、失败）或已被删除时没有结果行。
UPDATE transcode_job j
SET status = 'running', attempts = j.attempts + 1, progress = 0, error = '',
    started_at = coalesce(j.started_at, now()), updated_at = now()
FROM episode e
WHERE j.id = $1 AND e.id = j.episode_id AND j.status IN ('queued', 'running')
RETURNING j.*, e.drama_id, e.number AS episode_number, e.media_status AS episode_media_status,
          e.source_hash AS episode_source_hash;

-- name: UpdateTranscodeProgress :exec
UPDATE transcode_job SET progress = @progress, updated_at = now() WHERE id = @id AND status = 'running';

-- name: NextMediaKeyVer :one
SELECT (coalesce(max(key_ver), 0) + 1)::int AS key_ver FROM media_key WHERE episode_id = $1;

-- name: SkipTranscodeJob :execrows
UPDATE transcode_job
SET status = 'skipped', progress = 100, source_hash = @source_hash, finished_at = now(), updated_at = now()
WHERE id = @id AND status = 'running';

-- name: RequeueTranscodeJob :execrows
-- 本次尝试失败、等待 River 重试：记录错误，状态回到 queued。
UPDATE transcode_job
SET status = 'queued', error = @error, source_hash = CASE WHEN @source_hash::text = '' THEN source_hash ELSE @source_hash END,
    updated_at = now()
WHERE id = @id AND status = 'running';

-- name: FailTranscodeJob :execrows
-- 最终失败（调用方已锁住分集）。
UPDATE transcode_job
SET status = 'failed', error = @error, source_hash = CASE WHEN @source_hash::text = '' THEN source_hash ELSE @source_hash END,
    finished_at = now(), updated_at = now()
WHERE id = @id AND status IN ('queued', 'running');

-- name: MarkEpisodeTranscodeFailed :exec
-- 只改「转封装中」的分集：已有可播放视频的分集重新转封装失败时保持 ready。
UPDATE episode SET media_status = 'failed', updated_at = now() WHERE id = $1 AND media_status = 'transcoding';

-- name: LockDramaForMedia :one
-- 切换产物前锁住剧（与分集增删改同样的加锁顺序：先剧后分集），刷新「更新至 N 集」。回收站中的剧也要锁。
SELECT id FROM drama WHERE id = $1 FOR UPDATE;

-- name: LockEpisodeMedia :one
SELECT id FROM episode WHERE id = $1 FOR UPDATE;

-- name: LockTranscodeJob :one
-- 加锁顺序与入队一致：先分集后任务。
SELECT status, episode_id FROM transcode_job WHERE id = $1 FOR UPDATE;

-- name: GetTranscodeJobEpisodeID :one
SELECT episode_id FROM transcode_job WHERE id = $1;

-- name: InsertMediaObject :one
INSERT INTO media_object (kind, key, size_bytes, sha256, episode_id)
VALUES ('hls_dir', @key, @size_bytes, @sha256, @episode_id)
RETURNING id;

-- name: InsertMediaKey :exec
INSERT INTO media_key (episode_id, key_ver, wrapped_key) VALUES (@episode_id, @key_ver, @wrapped_key);

-- name: SetEpisodeMedia :exec
-- 切换到新产物；时长为 0（未知）时用探测结果回填，已填写的不覆盖。
UPDATE episode
SET video_object_id = @video_object_id, key_ver = @key_ver, renditions = @renditions, media_status = 'ready',
    source_hash = @source_hash, poster = @poster,
    duration_sec = CASE WHEN duration_sec = 0 THEN @duration_sec ELSE duration_sec END, updated_at = now()
WHERE id = @id;

-- name: CompleteTranscodeJob :exec
UPDATE transcode_job
SET status = 'succeeded', progress = 100, error = '', source_hash = @source_hash, output_object_id = @output_object_id,
    key_ver = @key_ver, renditions = @renditions, poster = @poster, duration_sec = @duration_sec,
    finished_at = now(), updated_at = now()
WHERE id = @id;

-- name: GetEpisodeMedia :one
-- 分集当前的产物目录与内容密钥（mediatool verify 使用；S1-8 的清单与密钥接口按同样的关系读取）。
SELECT e.id, e.media_status, e.key_ver, e.poster, o.key AS object_key, k.wrapped_key
FROM episode e
JOIN media_object o ON o.id = e.video_object_id
JOIN media_key k ON k.episode_id = e.id AND k.key_ver = e.key_ver
WHERE e.id = $1;

-- ---------- 媒体副本（S1-6f，docs/12 第 5 节） ----------

-- 记录一个副本；已存在时更新状态（复制重试会重复写入）
-- name: UpsertMediaReplica :one
INSERT INTO media_replica (object_id, provider_id, status, error, replicated_at)
VALUES (
    @object_id, @provider_id, @status, @error,
    CASE WHEN @status::text = 'ready' THEN now() ELSE NULL END
)
ON CONFLICT (object_id, provider_id) DO UPDATE
SET status        = EXCLUDED.status,
    error         = EXCLUDED.error,
    replicated_at = CASE WHEN EXCLUDED.status = 'ready' THEN now() ELSE media_replica.replicated_at END,
    updated_at    = now()
RETURNING *;

-- 一个对象的全部副本
-- name: ListMediaReplicas :many
SELECT * FROM media_replica WHERE object_id = $1 ORDER BY provider_id;

-- 一个对象上已就绪的存储源编号：选源据此过滤线路
-- name: ListReadyProviders :many
SELECT provider_id FROM media_replica WHERE object_id = $1 AND status = 'ready' ORDER BY provider_id;

-- 统计某存储源上的副本数（按状态）：删除与迁移前的检查
-- name: CountReplicasByProvider :one
SELECT count(*) FROM media_replica WHERE provider_id = $1;

-- 某存储源上的副本（分页，迁移任务逐批处理）
-- name: ListReplicasByProvider :many
SELECT r.object_id, r.provider_id, r.status, o.key, o.kind, o.size_bytes
FROM media_replica r
JOIN media_object o ON o.id = r.object_id
WHERE r.provider_id = @provider_id
  AND (@status::text = '' OR r.status = @status)
  AND r.object_id > @cursor_id
ORDER BY r.object_id
LIMIT @page_limit;

-- 删除一个副本记录（迁移完成或对象已从该存储源删除后）
-- name: DeleteMediaReplica :execrows
DELETE FROM media_replica WHERE object_id = $1 AND provider_id = $2;

-- 读对象自身（复制任务要拿 key 与 kind）
-- name: GetMediaObject :one
SELECT * FROM media_object WHERE id = $1;

-- 列出媒体对象编号（游标分页，供手动触发复制与迁移逐批入队）。
-- drama_id 为 0 时不按剧筛选；provider_id 为 0 时不按存储源筛选（按存储源时只列该源上有副本的对象）。
-- name: ListMediaObjectIDs :many
SELECT DISTINCT o.id
FROM media_object o
LEFT JOIN episode e ON e.id = o.episode_id
LEFT JOIN media_replica r ON r.object_id = o.id
WHERE (@drama_id::bigint = 0 OR e.drama_id = @drama_id)
  AND (@provider_id::bigint = 0 OR r.provider_id = @provider_id)
  AND o.id > @cursor_id
ORDER BY o.id
LIMIT @page_limit;

-- 列出还没有任何副本记录的媒体对象（S1-6f 之前写入默认桶的历史产物），游标分页。
-- name: ListOrphanObjectIDs :many
SELECT o.id
FROM media_object o
LEFT JOIN media_replica r ON r.object_id = o.id
WHERE r.object_id IS NULL
  AND o.id > @cursor_id
ORDER BY o.id
LIMIT @page_limit;

-- ---------- 孤立对象的标记与清理（S1-6f，docs/12 第 5 节第 6 条） ----------

-- 标记不再被任何分集引用的对象（首次发现时记时间，保留期从此开始算）。返回新标记的数量。
-- name: MarkOrphanObjects :execrows
UPDATE media_object o
SET orphaned_at = now()
WHERE o.orphaned_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM episode e WHERE e.video_object_id = o.id);

-- 对象重新被引用时撤销标记（正常不会发生，兜底防止误删）。返回撤销的数量。
-- name: UnmarkOrphanObjects :execrows
UPDATE media_object o
SET orphaned_at = NULL
WHERE o.orphaned_at IS NOT NULL
  AND EXISTS (SELECT 1 FROM episode e WHERE e.video_object_id = o.id);

-- 列出已过保留期、可以清理的孤立对象（游标分页）。
-- name: ListExpiredOrphanObjects :many
SELECT o.id, o.kind, o.key, o.size_bytes, o.orphaned_at
FROM media_object o
WHERE o.orphaned_at IS NOT NULL
  AND o.orphaned_at < @before
  AND NOT EXISTS (SELECT 1 FROM episode e WHERE e.video_object_id = o.id)
  AND o.id > @cursor_id
ORDER BY o.id
LIMIT @page_limit;

-- 统计已标记为孤立的对象（看板与手动触发时的预览）。
-- name: CountOrphanObjects :one
SELECT
    count(*) AS total,
    count(*) FILTER (WHERE orphaned_at < @before) AS expired,
    coalesce(sum(size_bytes), 0)::bigint AS bytes
FROM media_object
WHERE orphaned_at IS NOT NULL;

-- 删除一个媒体对象（副本记录随外键级联删除）。被分集引用时由外键阻止。
-- name: DeleteMediaObject :execrows
DELETE FROM media_object o
WHERE o.id = $1
  AND NOT EXISTS (SELECT 1 FROM episode e WHERE e.video_object_id = o.id);
