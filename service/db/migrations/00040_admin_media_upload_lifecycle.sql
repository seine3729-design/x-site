-- +goose Up
-- 上传会话生命周期：先原子领取写入权，避免同一会话并发 PUT 覆盖对象。
ALTER TABLE admin_media_upload DROP CONSTRAINT admin_media_upload_status_check;
ALTER TABLE admin_media_upload
    ADD CONSTRAINT admin_media_upload_status_check
    CHECK (status IN ('created', 'uploading', 'uploaded', 'queued', 'running', 'ready', 'failed'));

-- +goose Down
UPDATE admin_media_upload SET status = 'failed' WHERE status = 'uploading';
ALTER TABLE admin_media_upload DROP CONSTRAINT admin_media_upload_status_check;
ALTER TABLE admin_media_upload
    ADD CONSTRAINT admin_media_upload_status_check
    CHECK (status IN ('created', 'uploaded', 'queued', 'running', 'ready', 'failed'));
