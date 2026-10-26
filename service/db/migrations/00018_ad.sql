-- +goose Up
-- 广告（docs/02 第 3.4 节、docs/06 第 18 节，S3-6 / A3-4，docs/specs/A3-4.md）。设置类数据，删除不进回收站。
-- html 只存 bluemonday 清洗后的结果；曝光、点击先在 Valkey 累加，worker 每分钟落库到 imp_count / click_count。
-- 多语言按条投放：locales 为空表示全部语言。
CREATE TABLE ad (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name          text        NOT NULL CHECK (length(name) BETWEEN 1 AND 50),  -- 广告名，后台识别用
    slot          text        NOT NULL CHECK (slot IN (
                      'global_popup', 'home_top', 'home_mid', 'home_bottom', 'footer_banner', 'list_feed',
                      'detail_banner', 'preroll', 'pause', 'immersive_feed', 'text_link', 'app_download')),
    creative_type text        NOT NULL CHECK (creative_type IN ('image', 'video', 'text', 'html')),
    image         text        NOT NULL DEFAULT '',  -- 图片素材；视频素材的封面
    video_url     text        NOT NULL DEFAULT '',
    text          text        NOT NULL DEFAULT '' CHECK (length(text) <= 200),
    html          text        NOT NULL DEFAULT '' CHECK (length(html) <= 20000),
    alt           text        NOT NULL DEFAULT '' CHECK (length(alt) <= 100),
    click_url     text        NOT NULL DEFAULT '',
    device        text        NOT NULL DEFAULT 'all' CHECK (device IN ('all', 'mobile', 'desktop')),
    locales       text[]      NOT NULL DEFAULT '{}',
    weight        int         NOT NULL DEFAULT 10 CHECK (weight BETWEEN 1 AND 100),  -- 同一位置内的出现机会
    end_at        timestamptz,                                                       -- 为空表示一直投放
    freq_cap      int         NOT NULL DEFAULT 0 CHECK (freq_cap BETWEEN 0 AND 1000), -- 每设备每天曝光上限，0 = 不限
    status        text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    imp_count     bigint      NOT NULL DEFAULT 0 CHECK (imp_count >= 0),
    click_count   bigint      NOT NULL DEFAULT 0 CHECK (click_count >= 0),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    -- 素材按类型必填
    CHECK (CASE creative_type
               WHEN 'image' THEN image <> ''
               WHEN 'video' THEN video_url <> ''
               WHEN 'text' THEN text <> ''
               ELSE html <> ''
           END)
);
-- 前台只读「投放中且未结束」的广告
CREATE INDEX ad_live_idx ON ad (status, end_at);

-- +goose Down
DROP TABLE ad;
