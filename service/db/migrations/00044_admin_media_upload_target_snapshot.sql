-- +goose Up
-- 上传会话在创建时固定物理存储目标，避免运营修改同一 provider 的连接参数后，
-- 已开始会话把对象写入或清理到新的 bucket/path_prefix。凭据不写入会话，仍从 provider 解密。
ALTER TABLE admin_media_upload
    ADD COLUMN provider_endpoint text NOT NULL DEFAULT '',
    ADD COLUMN provider_region text NOT NULL DEFAULT '',
    ADD COLUMN provider_bucket text NOT NULL DEFAULT '',
    ADD COLUMN provider_path_style boolean NOT NULL DEFAULT false,
    ADD COLUMN provider_path_prefix text NOT NULL DEFAULT '';

UPDATE admin_media_upload AS upload
SET provider_endpoint = provider.endpoint,
    provider_region = provider.region,
    provider_bucket = provider.bucket,
    provider_path_style = provider.path_style,
    provider_path_prefix = provider.path_prefix
FROM storage_provider AS provider
WHERE upload.provider_id = provider.id
  AND upload.provider_bucket = '';

-- +goose Down
ALTER TABLE admin_media_upload
    DROP COLUMN provider_path_prefix,
    DROP COLUMN provider_path_style,
    DROP COLUMN provider_bucket,
    DROP COLUMN provider_region,
    DROP COLUMN provider_endpoint;
