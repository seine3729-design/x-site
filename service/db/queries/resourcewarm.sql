-- name: StartWarmJob :one
-- 开一轮预热。已有在跑的一轮时唯一索引 resource_warm_single_running 会报 23505，由领域层转成 409。
INSERT INTO resource_warm_job (
    drama_id, mode, segments_per_quality, concurrency, include_unpublished, status, episodes_total, admin_id
) VALUES (
    sqlc.narg(drama_id), @mode, @segments_per_quality, @concurrency, @include_unpublished, 'running', @episodes_total, @admin_id
)
RETURNING id, drama_id, mode, segments_per_quality, concurrency, include_unpublished, status,
          episodes_total, episodes_done, warmed, failed, fetched, traffic_bytes, current,
          stop_requested, error, admin_id, started_at, heartbeat_at, finished_at;

-- name: GetWarmJob :one
SELECT w.id, w.drama_id, d.title AS drama_title, w.mode, w.segments_per_quality, w.concurrency,
       w.include_unpublished, w.status, w.episodes_total, w.episodes_done, w.warmed, w.failed,
       w.fetched, w.traffic_bytes, w.current, w.stop_requested, w.error, w.admin_id,
       w.started_at, w.heartbeat_at, w.finished_at
FROM resource_warm_job w
LEFT JOIN drama d ON d.id = w.drama_id
WHERE w.id = $1;

-- name: GetRunningWarmJob :one
-- 当前在跑的那一轮（最多一条，由唯一索引保证）。
SELECT w.id, w.drama_id, d.title AS drama_title, w.mode, w.segments_per_quality, w.concurrency,
       w.include_unpublished, w.status, w.episodes_total, w.episodes_done, w.warmed, w.failed,
       w.fetched, w.traffic_bytes, w.current, w.stop_requested, w.error, w.admin_id,
       w.started_at, w.heartbeat_at, w.finished_at
FROM resource_warm_job w
LEFT JOIN drama d ON d.id = w.drama_id
WHERE w.status = 'running';

-- name: LatestWarmJob :one
-- 最近一轮（不论状态），页面上没有在跑的任务时显示上一轮的结果。
SELECT w.id, w.drama_id, d.title AS drama_title, w.mode, w.segments_per_quality, w.concurrency,
       w.include_unpublished, w.status, w.episodes_total, w.episodes_done, w.warmed, w.failed,
       w.fetched, w.traffic_bytes, w.current, w.stop_requested, w.error, w.admin_id,
       w.started_at, w.heartbeat_at, w.finished_at
FROM resource_warm_job w
LEFT JOIN drama d ON d.id = w.drama_id
ORDER BY w.id DESC
LIMIT 1;

-- name: ListWarmJobs :many
-- 历史记录，倒序。
SELECT w.id, w.drama_id, d.title AS drama_title, w.mode, w.segments_per_quality, w.concurrency,
       w.include_unpublished, w.status, w.episodes_total, w.episodes_done, w.warmed, w.failed,
       w.fetched, w.traffic_bytes, w.current, w.stop_requested, w.error, w.admin_id,
       w.started_at, w.heartbeat_at, w.finished_at
FROM resource_warm_job w
LEFT JOIN drama d ON d.id = w.drama_id
ORDER BY w.id DESC
LIMIT @lim;

-- name: UpdateWarmProgress :exec
-- worker 周期性写进度，顺便当心跳。只在 running 时写，避免已停止的任务被覆盖回去。
UPDATE resource_warm_job
SET episodes_done = @episodes_done, warmed = @warmed, failed = @failed,
    fetched = @fetched, traffic_bytes = @traffic_bytes, current = @current,
    heartbeat_at = now()
WHERE id = @id AND status = 'running';

-- name: FinishWarmJob :exec
-- 收尾：done（跑完）、stopped（运营点了停止）、interrupted（出错或 worker 没了）。
UPDATE resource_warm_job
SET status = @status, episodes_done = @episodes_done, warmed = @warmed, failed = @failed,
    fetched = @fetched, traffic_bytes = @traffic_bytes, current = '', error = @error,
    heartbeat_at = now(), finished_at = now()
WHERE id = @id AND status = 'running';

-- name: RequestWarmStop :execrows
-- 运营点停止：只设信号，worker 处理完当前这一集就收尾（已经热好的算数）。
UPDATE resource_warm_job SET stop_requested = true, heartbeat_at = heartbeat_at
WHERE id = @id AND status = 'running';

-- name: IsWarmStopRequested :one
SELECT stop_requested FROM resource_warm_job WHERE id = $1;

-- name: ReapStaleWarmJobs :execrows
-- 心跳超时的任务判定为中断：worker 崩了之后没人改它的状态，一直挂在 running 会挡住下一轮。
UPDATE resource_warm_job
SET status = 'interrupted', current = '', finished_at = now(),
    error = CASE WHEN error = '' THEN '预热进程失去响应，任务中断' ELSE error END
WHERE status = 'running' AND heartbeat_at < @before;

-- name: ListWarmEpisodes :many
-- 待预热的分集：有当前产物、转封装完成的才有东西可热。
-- include_unpublished 为假时只取前台可见的剧（已上架且定时已到点），与选源的可见性一致。
SELECT e.id, e.drama_id, e.number, d.title AS drama_title
FROM episode e
JOIN drama d ON d.id = e.drama_id
JOIN media_object o ON o.id = e.video_object_id
WHERE e.media_status = 'ready'
  AND o.kind = 'hls_dir'
  AND d.deleted_at IS NULL
  AND (@drama_id::bigint = 0 OR e.drama_id = @drama_id)
  AND (@include_unpublished::bool = true
       OR (d.status = 'published' AND (d.publish_at IS NULL OR d.publish_at <= now())))
ORDER BY e.drama_id, e.number;

-- name: GetWarmEpisode :one
-- 预热专用的分集读取：和 GetPlayableEpisode 一样，但**不要求剧已上架**。
-- 预热未上架的剧是运营的正常需求（明天要上的剧提前热好），而选源那条路径必须严格按前台可见性来。
SELECT e.id, e.drama_id, e.access, e.trial_sec, e.video_object_id::bigint AS video_object_id, o.key AS object_key
FROM episode e
JOIN drama d ON d.id = e.drama_id
JOIN media_object o ON o.id = e.video_object_id
WHERE e.id = $1
  AND e.media_status = 'ready'
  AND o.kind = 'hls_dir'
  AND d.deleted_at IS NULL;
