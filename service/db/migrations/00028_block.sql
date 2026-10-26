-- +goose Up
-- 页面区块（docs/02 第 3.1 节、docs/06 第 15 节，S3-1 / A3-1，docs/specs/A3-1.md）：首页与频道页由区块拼装。
-- 前台 GET /api/v1/home?page= 一次返回该页面全部启用区块及其数据；广告位与公告条区块只给参数（素材按设备频控，另行请求）。
-- 设置类数据，删除不进回收站。
CREATE TABLE block (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- home 或 channel:{频道取值 slug}
    page         text        NOT NULL CHECK (page = 'home' OR page LIKE 'channel:_%'),
    type         text        NOT NULL CHECK (type IN ('carousel', 'feature', 'poster_grid', 'h_scroll', 'rank',
                                                      'topic_group', 'tag_cloud', 'ad', 'announce', 'custom_html')),
    title        text        NOT NULL DEFAULT '',
    subtitle     text        NOT NULL DEFAULT '',
    -- 最多显示几项（剧类 1–60，默认 24）；limit 是保留字，列名用 item_limit
    item_limit   integer     NOT NULL DEFAULT 24 CHECK (item_limit BETWEEN 1 AND 60),
    status       text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    more_link    text        NOT NULL DEFAULT '',  -- 「查看全部」链接
    cols_desktop integer     NOT NULL DEFAULT 6 CHECK (cols_desktop BETWEEN 2 AND 8),
    device       text        NOT NULL DEFAULT 'all' CHECK (device IN ('all', 'mobile', 'desktop')),
    -- 剧类区块的来源：manual 指定编号 / topic 用某专题 / auto 按规则选
    source_type  text        NOT NULL DEFAULT 'auto' CHECK (source_type IN ('manual', 'topic', 'auto')),
    -- 来源与类型参数：drama_ids / topic_id / {term_codes,tag_id,sort,window} / rank_board / ad_slot / announce_kind / topic_ids / tag_ids
    source_json  jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(source_json) = 'object'),
    custom_html  text        NOT NULL DEFAULT '',  -- 自定义 HTML（清洗后；要 blocks.custom_html 权限）
    sort         integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
-- 后台按页面分组显示、前台取该页面启用的区块，都按 (page, sort) 取
CREATE INDEX block_page_sort_idx ON block (page, sort, id);

-- +goose Down
DROP TABLE block;
