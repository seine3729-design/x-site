-- +goose Up
-- 站点配置：单行，按后台分段保存，version 每次变更 +1（docs/02 第 3.6 节）。
CREATE TABLE site_config (
    id          smallint    PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    version     bigint      NOT NULL DEFAULT 1,
    site        jsonb       NOT NULL DEFAULT '{}',
    theme       jsonb       NOT NULL DEFAULT '{}',
    layout      jsonb       NOT NULL DEFAULT '{}',
    seo         jsonb       NOT NULL DEFAULT '{}',
    -- analytics 含统计代码，只有后台接口读取，不对前台下发
    analytics   jsonb       NOT NULL DEFAULT '{}',
    age_gate    jsonb       NOT NULL DEFAULT '{}',
    features    jsonb       NOT NULL DEFAULT '{}',
    addresses   jsonb       NOT NULL DEFAULT '{}',
    updated_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO site_config (site, features, age_gate)
VALUES (
    '{"site_name": "短剧", "slogan": "随时随地，追你想看的"}',
    '{"comments": true, "danmaku": true, "register": true, "guest_interaction": true, "articles": true, "actors": true, "ranks": true, "share_poster": true, "pwa": true, "search_suggest": true}',
    '{"enabled": false}'
);

-- +goose Down
DROP TABLE site_config;
