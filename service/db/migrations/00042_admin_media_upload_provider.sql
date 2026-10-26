-- +goose Up
-- 上传会话固定到创建时选中的后台主存储源；后续主存储切换不影响已开始上传的对象定位。
ALTER TABLE admin_media_upload
    ADD COLUMN provider_id bigint REFERENCES storage_provider (id);
CREATE INDEX admin_media_upload_provider_idx ON admin_media_upload (provider_id, status);

-- +goose Down
DROP INDEX admin_media_upload_provider_idx;
ALTER TABLE admin_media_upload DROP COLUMN provider_id;
