-- 运营后台媒体上传会话：图片在浏览器转码后写入 img/；视频先写私有 video/source/，再异步转 HLS。

-- name: CreateAdminMediaUpload :one
INSERT INTO admin_media_upload (
    id, admin_id, provider_id, provider_endpoint, provider_region, provider_bucket, provider_path_style, provider_path_prefix,
    purpose, kind, mime, image_format, size_bytes, source_key, output_key, episode_id, expires_at
) VALUES (
    @id, @admin_id, @provider_id, @provider_endpoint, @provider_region, @provider_bucket, @provider_path_style, @provider_path_prefix,
    @purpose, @kind, @mime, @image_format, @size_bytes, @source_key, @output_key, @episode_id, @expires_at
)
RETURNING *;

-- name: GetAdminMediaUpload :one
SELECT * FROM admin_media_upload WHERE id = $1;

-- name: ClaimAdminMediaUploadWrite :execrows
-- 原子领取一次写入权：只有首个请求能 created → uploading，避免后到请求覆盖同一个对象 key。
UPDATE admin_media_upload
SET status = 'uploading', error = '', updated_at = now()
WHERE id = @id AND admin_id = @admin_id AND status = 'created' AND expires_at > now();

-- name: MarkAdminMediaUploadUploaded :execrows
UPDATE admin_media_upload
SET status = 'uploaded', progress = 100, error = '', updated_at = now()
WHERE id = @id AND admin_id = @admin_id AND status = 'uploading' AND expires_at > now();

-- name: FailAdminMediaUpload :execrows
-- 上传内容写入后发生元数据/格式/队列错误时标记失败；实际对象由 handler 立即补偿删除，周期任务兜底。
UPDATE admin_media_upload
SET status = 'failed', error = @error, updated_at = now()
WHERE id = @id AND admin_id = @admin_id AND status IN ('created', 'uploading', 'uploaded');

-- name: CompleteAdminImageUpload :execrows
UPDATE admin_media_upload
SET status = 'ready', progress = 100, error = '', output_key = source_key, updated_at = now()
WHERE id = @id AND admin_id = @admin_id AND kind = 'image' AND status = 'uploaded';

-- name: LinkAdminEpisodeUpload :execrows
UPDATE admin_media_upload
SET status = 'running', progress = 0, transcode_job_id = @transcode_job_id, error = '', updated_at = now()
WHERE id = @id AND admin_id = @admin_id AND purpose = 'episode_video' AND status = 'uploaded';

-- name: QueueAdminVideoUpload :execrows
UPDATE admin_media_upload
SET status = 'queued', progress = 0, error = '', updated_at = now()
WHERE id = $1 AND purpose = 'ad_video' AND status = 'uploaded';

-- name: StartAdminVideoUpload :one
UPDATE admin_media_upload
SET status = 'running', progress = 0, attempts = attempts + 1, error = '', updated_at = now()
WHERE id = $1 AND purpose = 'ad_video' AND status IN ('queued', 'running')
RETURNING *;

-- name: UpdateAdminVideoUploadProgress :execrows
UPDATE admin_media_upload
SET progress = @progress, updated_at = now()
WHERE id = $1 AND purpose = 'ad_video' AND status = 'running';

-- name: RequeueAdminVideoUpload :execrows
UPDATE admin_media_upload
SET status = 'queued', error = @error, updated_at = now()
WHERE id = $1 AND purpose = 'ad_video' AND status = 'running';

-- name: CompleteAdminVideoUpload :execrows
UPDATE admin_media_upload
SET status = 'ready', progress = 100, output_key = @output_key, error = '', updated_at = now()
WHERE id = $1 AND purpose = 'ad_video' AND status = 'running';

-- name: FailAdminVideoUpload :execrows
UPDATE admin_media_upload
SET status = 'failed', error = @error, updated_at = now()
WHERE id = $1 AND purpose = 'ad_video' AND status IN ('queued', 'running');

-- name: ListExpiredAdminMediaUploads :many
-- 已完成图片与分集视频只清理会话记录；广告视频完成后也可安全删除私有源文件。
-- 分集视频会话在既有 transcode_job 完成后不再更新，仍保留其原始源文件供转码重试；
-- 只有广告视频 queued/running 不能按短 TTL 清理，避免长视频转码途中丢源文件。
SELECT * FROM admin_media_upload
WHERE expires_at <= now()
  AND (purpose = 'episode_video' OR status NOT IN ('queued', 'running'))
ORDER BY expires_at
LIMIT @page_limit;

-- name: DeleteExpiredAdminMediaUpload :execrows
DELETE FROM admin_media_upload
WHERE id = $1
  AND (purpose = 'episode_video' OR status NOT IN ('queued', 'running'));
