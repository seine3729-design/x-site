-- +goose Up
-- 存储源（docs/12 第 2、7 节，S1-6a）：视频切片、封面等媒体资源的存储后端（S3 / Cloudflare R2 / 自定义）。
-- 一个存储源 = 一个桶 + 一组凭据。线路 storage_line 挂在存储源下（S1-6b），副本 media_replica 随复制任务建立（S1-6f）。
-- 凭据只写不读：access_key_id 与 secret_access_key 一起信封加密存入 *_credential_enc
-- （docs/10 第 3.2 节；S2-8 接入 KMS 前用 STORAGE_KEKS 包装），后台列表只显示 secret 末 4 位提示（cred_tail）。
-- 读写凭据（上传、复制、删除）与只读凭据（预签名）分开存，预签名用只读凭据（docs/12 第 2 节）。
CREATE TABLE storage_provider (
    id                  bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name                text        NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    -- s3：AWS S3 或 S3 兼容云存储；r2：Cloudflare R2；custom：自建 S3 兼容或只读 HTTP 源
    type                text        NOT NULL CHECK (type IN ('s3', 'r2', 'custom')),
    -- 仅 custom 用：s3_compatible 走 S3 协议，http_origin 为只读 HTTP 源（base_url 存入 endpoint）；其它类型留空（业务层校验）
    subtype             text        NOT NULL DEFAULT '' CHECK (subtype IN ('', 's3_compatible', 'http_origin')),
    -- 用途：video 切片与清单 / image 封面等图片 / both
    purpose             text        NOT NULL CHECK (purpose IN ('video', 'image', 'both')),
    -- 角色：primary 新内容写入 / mirror 异步复制 / readonly 只读历史资源
    role                text        NOT NULL CHECK (role IN ('primary', 'mirror', 'readonly')),
    -- 访问端点：s3 可选（S3 兼容服务填写）；r2 由 account_id 自动生成；custom/s3_compatible 必填；http_origin 存 base_url
    endpoint            text        NOT NULL DEFAULT '',
    region              text        NOT NULL DEFAULT '',  -- s3 必填；r2 固定 auto（业务层补齐）
    bucket              text        NOT NULL DEFAULT '',  -- s3 / r2 / s3_compatible 必填；http_origin 可空
    account_id          text        NOT NULL DEFAULT '',  -- r2 必填
    path_style          boolean     NOT NULL DEFAULT false,
    path_prefix         text        NOT NULL DEFAULT '',  -- 桶内前缀（如 media/），便于多环境共用一个桶
    -- 凭据密文（信封加密后的 {access_key_id, secret_access_key}）；NULL 表示未配置
    credential_enc      bytea,
    read_credential_enc bytea,
    -- 凭据 secret 的末 4 位，仅用于后台显示「已配置 ****abcd」；不足 4 位时按实际长度（非敏感）
    cred_tail           text        NOT NULL DEFAULT '',
    read_cred_tail      text        NOT NULL DEFAULT '',
    -- 预签名最长有效期（秒），默认 600，上限 7 天（S3/R2 协议上限）
    max_presign_ttl     int         NOT NULL DEFAULT 600 CHECK (max_presign_ttl BETWEEN 60 AND 604800),
    status              text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'draining', 'disabled')),
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE storage_provider;
