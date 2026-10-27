-- 埋点采集（docs/03 第 8.1 节，S4-1）：批量 COPY 进 event_raw 分区表。
-- 汇总（metric_*，复杂聚合 + ON CONFLICT）与分区维护（动态 DDL）sqlc 不支持，
-- 手写在 store/pg/analytics.go（参数化 pool.Exec），不经 sqlc。

-- name: InsertEvents :copyfrom
INSERT INTO event_raw (occurred_at, event_type, device_id, user_id, drama_id, episode_id, client, region, isp, props)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- 看板读查询（S4-3）：只读预聚合表，纯 SELECT，sqlc 支持。
-- 剧集分析列表要「按不同指标排序 + 分页」，ORDER BY 列无法参数化，手写在 store/pg/analytics.go。

-- name: ListMetricDaily :many
-- 某日期区间（北京时间日）的全站日汇总，升序。供看板窗口汇总、环比与每日序列。
SELECT day, uv, play_attempts, play_starts, play_errors, watch_seconds,
       registrations, reg_wall_views, ad_impressions, ad_clicks
FROM metric_daily
WHERE day >= @from_day::date AND day <= @to_day::date
ORDER BY day;
