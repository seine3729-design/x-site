-- 埋点采集（docs/03 第 8.1 节，S4-1）：批量 COPY 进 event_raw 分区表。

-- name: InsertEvents :copyfrom
INSERT INTO event_raw (occurred_at, event_type, device_id, user_id, drama_id, episode_id, client, region, isp, props)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);
