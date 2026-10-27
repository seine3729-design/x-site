-- +goose Up
-- 看板汇总表（docs/02 第 6.3 节、docs/03 第 8.2/11 节，S4-2）。
-- 由 River 周期任务从 event_raw 聚合而来：日表按北京时间日界定稿，小时表按 UTC 小时供实时卡片。
-- 第一版只落「服务端可观测」的计数类指标；前台埋点 SDK 接入后的 QoS / 留存曲线等放 extra 或后续加列。
-- 看板与剧集分析接口（S4-3）只读这些预聚合表，不扫 event_raw。

-- 全站日汇总（北京时间日）。
CREATE TABLE metric_daily (
    day            date        PRIMARY KEY,
    uv             bigint      NOT NULL DEFAULT 0,  -- 去重设备数
    play_attempts  bigint      NOT NULL DEFAULT 0,
    play_starts    bigint      NOT NULL DEFAULT 0,
    play_errors    bigint      NOT NULL DEFAULT 0,
    watch_seconds  bigint      NOT NULL DEFAULT 0,  -- 有效观看秒数之和
    registrations  bigint      NOT NULL DEFAULT 0,
    reg_wall_views bigint      NOT NULL DEFAULT 0,
    ad_impressions bigint      NOT NULL DEFAULT 0,
    ad_clicks      bigint      NOT NULL DEFAULT 0,
    extra          jsonb       NOT NULL DEFAULT '{}'::jsonb,  -- 预留：前台 SDK 接入后的扩展指标
    finalized_at   timestamptz NOT NULL DEFAULT now()
);

-- 按剧的日汇总（剧集分析列表）。
CREATE TABLE metric_drama_daily (
    day           date   NOT NULL,
    drama_id      bigint NOT NULL,
    viewers       bigint NOT NULL DEFAULT 0,
    play_starts   bigint NOT NULL DEFAULT 0,
    watch_seconds bigint NOT NULL DEFAULT 0,
    extra         jsonb  NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (day, drama_id)
);
CREATE INDEX metric_drama_daily_drama_idx ON metric_drama_daily (drama_id, day DESC);

-- 按分集的日汇总（单剧的集间留存曲线）。
CREATE TABLE metric_episode_daily (
    day           date   NOT NULL,
    episode_id    bigint NOT NULL,
    drama_id      bigint NOT NULL DEFAULT 0,
    viewers       bigint NOT NULL DEFAULT 0,
    play_starts   bigint NOT NULL DEFAULT 0,
    watch_seconds bigint NOT NULL DEFAULT 0,
    extra         jsonb  NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (day, episode_id)
);
CREATE INDEX metric_episode_daily_drama_idx ON metric_episode_daily (drama_id, day DESC);

-- 小时预聚合（UTC 小时），供看板实时卡片，不扫原始表。
CREATE TABLE metric_hourly (
    hour          timestamptz PRIMARY KEY,
    uv            bigint      NOT NULL DEFAULT 0,
    play_starts   bigint      NOT NULL DEFAULT 0,
    watch_seconds bigint      NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE metric_hourly;
DROP TABLE metric_episode_daily;
DROP TABLE metric_drama_daily;
DROP TABLE metric_daily;
