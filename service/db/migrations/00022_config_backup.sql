-- +goose Up
-- 配置备份（S4-4，docs/06 第 29 节）：导出的备份正文留一份在库里，导入前自动存档以便回滚。
--
-- payload 是备份正文（与导出文件里的 data 段一致），signature 是对「格式版本 + 正文」的 HMAC。
-- admin_id 不加外键：备份要比管理员账号活得久（账号停用甚至将来清理掉，备份仍要能追溯是谁做的）。
CREATE TABLE config_backup (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    format     int         NOT NULL CHECK (format > 0),
    reason     text        NOT NULL CHECK (reason IN ('manual', 'before_import')),
    admin_id   bigint      NOT NULL,
    note       text        NOT NULL DEFAULT '',
    payload    jsonb       NOT NULL CHECK (jsonb_typeof(payload) = 'object'),
    signature  text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- 列表按时间倒序翻页
CREATE INDEX config_backup_created_idx ON config_backup (created_at DESC, id DESC);

-- +goose Down
DROP TABLE config_backup;
