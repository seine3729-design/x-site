-- +goose Up
-- 运营后台媒体上传会话：浏览器只通过受后台会话、CSRF 和所有者校验的同源接口上传；
-- 图片目标为 img/，视频源文件为私有桶的 video/source/，广告视频转码完成后 output_key 指向 m3u8。
CREATE TABLE admin_media_upload (
    id               uuid        PRIMARY KEY,
    admin_id         bigint      NOT NULL REFERENCES admin_user (id),
    purpose          text        NOT NULL CHECK (purpose IN (
        'drama_cover', 'episode_cover', 'announcement_image', 'ad_image', 'ad_poster',
        'feedback_project_cover', 'static_page_image', 'episode_video', 'ad_video'
    )),
    kind             text        NOT NULL CHECK (kind IN ('image', 'video')),
    mime             text        NOT NULL,
    image_format     text        NOT NULL DEFAULT '' CHECK (image_format IN ('', 'webp', 'avif')),
    size_bytes       bigint      NOT NULL CHECK (size_bytes > 0),
    source_key       text        NOT NULL UNIQUE,
    output_key       text        NOT NULL DEFAULT '',
    episode_id       bigint      REFERENCES episode (id) ON DELETE SET NULL,
    transcode_job_id uuid        REFERENCES transcode_job (id) ON DELETE SET NULL,
    status           text        NOT NULL DEFAULT 'created' CHECK (status IN ('created', 'uploaded', 'queued', 'running', 'ready', 'failed')),
    progress         int         NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
    attempts         int         NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    error            text        NOT NULL DEFAULT '',
    expires_at       timestamptz NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_media_upload_admin_created_idx ON admin_media_upload (admin_id, created_at DESC);
CREATE INDEX admin_media_upload_status_idx ON admin_media_upload (status, created_at);

-- +goose Down
DROP TABLE admin_media_upload;
