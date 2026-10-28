-- +goose Up
-- 后台视频分片并行上传：会话记录服务端规定的分片大小与对象存储的分片上传 ID。
-- 单个请求上传整段视频会撞上 Cloudflare 的请求体上限（Free / Pro 100 MB）与 Traefik 的 60 秒读超时，
-- 也只能用到一条回源连接；改为浏览器按片并发提交，服务端逐片写入私有入库桶的同一次分片上传。
-- 两列默认值表示「整段单次上传」，老会话与图片会话不受影响。
ALTER TABLE admin_media_upload
    ADD COLUMN part_size bigint NOT NULL DEFAULT 0 CONSTRAINT admin_media_upload_part_size_check CHECK (part_size >= 0),
    ADD COLUMN multipart_upload_id text NOT NULL DEFAULT '';

COMMENT ON COLUMN admin_media_upload.part_size IS '分片大小（字节）；0 表示整段单次上传。除最后一片外每片都是这个大小';
COMMENT ON COLUMN admin_media_upload.multipart_upload_id IS '私有入库桶上的分片上传 ID；空串表示未使用分片上传。会话过期未完成时由清理任务放弃该分片上传';

-- +goose Down
ALTER TABLE admin_media_upload
    DROP COLUMN multipart_upload_id,
    DROP COLUMN part_size;
