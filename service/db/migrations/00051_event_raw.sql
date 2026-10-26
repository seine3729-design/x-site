-- +goose Up
-- 原始埋点事件（docs/02 第 6.3 / 6.3.1 节、docs/03 第 8 节，S4-1）。
-- 通用事件表：前台 / APP 经 POST /api/v1/events 批量上报，服务端校验后 COPY 入库。
-- 按天 RANGE 分区（occurred_at 为 UTC 日界）；只保留必要索引（occurred_at BRIN + device_id），
-- 汇总查询走 metric_* 预聚合表（S4-2），不直接扫原始表。90 天后导出 Parquet 并 DETACH/DROP（S4-2）。
CREATE TABLE event_raw (
    occurred_at timestamptz NOT NULL,
    -- 事件类型（应用层白名单校验，不加 CHECK 以便扩展）：visitor / play_attempt / play_start /
    -- play_error / watch / ad_impression / ad_click / register_wall / register / domain_switch 等
    event_type  text        NOT NULL,
    device_id   text        NOT NULL DEFAULT '',
    user_id     bigint,            -- 注册用户；游客为空
    drama_id    bigint,
    episode_id  bigint,
    client      text        NOT NULL DEFAULT '',  -- web / app / ...
    region      text        NOT NULL DEFAULT '',  -- 国家地区（GeoIP，可空）
    isp         text        NOT NULL DEFAULT '',
    props       jsonb       NOT NULL DEFAULT '{}'::jsonb
) PARTITION BY RANGE (occurred_at);

-- 分区表父表上建索引，PG 自动传播到现有与未来分区（PG 11+）。
CREATE INDEX event_raw_occurred_brin ON event_raw USING brin (occurred_at);
CREATE INDEX event_raw_device_idx ON event_raw (device_id);

-- 初始建「昨天起未来 30 天」的日分区（覆盖时钟偏差与未来几天）；
-- 之后的分区滚动创建与 90 天过期回收由 S4-2 的 River 周期任务维护。
-- +goose StatementBegin
DO $$
DECLARE
    d date;
BEGIN
    FOR i IN -1..30 LOOP
        d := current_date + i;
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF event_raw FOR VALUES FROM (%L) TO (%L)',
            'event_raw_' || to_char(d, 'YYYYMMDD'),
            (d::timestamp AT TIME ZONE 'UTC'),
            ((d + 1)::timestamp AT TIME ZONE 'UTC')
        );
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE event_raw;
