-- +goose Up
-- 静态页（docs/02 第 3.3 节、docs/06 第 13 节，S3-2 / A3-2，docs/specs/A3-1.md）：条款、隐私、FAQ 等富文本，前台 /p/:slug。
-- body 存按白名单清洗后的 HTML（两种格式都清洗，与广告的自定义 HTML 同一策略）；草稿不对前台下发。
-- 设置类数据，删除不进回收站；删除后指向它的前台菜单项自动隐藏（docs/06 第 12 节）。
CREATE TABLE page (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title       text        NOT NULL CHECK (length(title) BETWEEN 1 AND 100),
    -- 前台地址 /p/:slug；格式由业务层校验（与剧集、专题同一套 slug 规则）
    slug        text        NOT NULL UNIQUE,
    body        text        NOT NULL,
    -- rich 富文本编辑器产出 / html 直接写 HTML；入库都是清洗后的结果
    body_format text        NOT NULL DEFAULT 'rich' CHECK (body_format IN ('rich', 'html')),
    status      text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    seo_title   text        NOT NULL DEFAULT '',
    seo_desc    text        NOT NULL DEFAULT '',
    -- 标记为页脚页：随站点配置下发，前台页脚「帮助与条款」显示
    is_footer   boolean     NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
-- 页脚页：已发布且标记为页脚的少数几页，按创建顺序下发
CREATE INDEX page_footer_idx ON page (id) WHERE status = 'published' AND is_footer;

-- +goose Down
DROP TABLE page;
