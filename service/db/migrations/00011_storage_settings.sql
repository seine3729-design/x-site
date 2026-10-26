-- +goose Up
-- 存储全局设置（docs/12 第 7、8.4 节，S1-6d 的设置部分）：单行配置，候选线路数量、默认有效期、
-- 智能选线开关、视频 / 图片主存储、上传后自动复制。选线算法与健康分在 S1-6d 其余部分。
CREATE TABLE storage_settings (
    id                        int         PRIMARY KEY DEFAULT 1 CHECK (id = 1),  -- 单行
    candidate_count           int         NOT NULL DEFAULT 3 CHECK (candidate_count BETWEEN 1 AND 5),
    default_ttl               int         NOT NULL DEFAULT 600 CHECK (default_ttl BETWEEN 60 AND 604800),
    smart_routing             boolean     NOT NULL DEFAULT true,
    primary_video_provider_id bigint      REFERENCES storage_provider (id) ON DELETE SET NULL,
    primary_image_provider_id bigint      REFERENCES storage_provider (id) ON DELETE SET NULL,
    replicate_on_upload       boolean     NOT NULL DEFAULT true,
    updated_at                timestamptz NOT NULL DEFAULT now()
);
-- 单行始终存在：读取时无需处理「未初始化」
INSERT INTO storage_settings (id) VALUES (1);

-- +goose Down
DROP TABLE storage_settings;
