-- +goose Up
-- 公告（docs/02 第 3.5 节、docs/06 第 19 节，S3-2 / A3-2）：顶部滚动条与弹窗；站内信（inbox）暂不启用，业务层拒绝新建。
-- 设置类数据，删除不进回收站。多语言按条投放：locales 为空表示全部语言（docs/specs/A3-4.md）。
CREATE TABLE announcement (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind       text        NOT NULL CHECK (kind IN ('topbar', 'popup', 'inbox')),
    content    text        NOT NULL CHECK (length(content) BETWEEN 1 AND 500),
    click_url  text        NOT NULL DEFAULT '',
    -- always 每次显示 / once 每人一次 / daily 每人每天一次（记录存在客户端本机）
    frequency  text        NOT NULL DEFAULT 'always' CHECK (frequency IN ('always', 'once', 'daily')),
    pages      text[]      NOT NULL DEFAULT '{}',  -- 出现页面；空 = 全部页面
    locales    text[]      NOT NULL DEFAULT '{}',  -- 投放语言；空 = 全部语言
    image      text        NOT NULL DEFAULT '',    -- 弹窗配图（资源路径或图片地址）
    start_at   timestamptz,
    end_at     timestamptz,
    status     text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (start_at IS NULL OR end_at IS NULL OR end_at > start_at)
);
-- 前台只读「显示中且未结束」的少量公告
CREATE INDEX announcement_live_idx ON announcement (status, end_at);

-- +goose Down
DROP TABLE announcement;
