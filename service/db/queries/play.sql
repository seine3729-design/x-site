-- 播放服务的查询（docs/03 第 4 节）：选源（S1-7）、清单与密钥（S1-8）。
-- name: GetPlayableEpisode :one
-- 选源与清单：剧前台可见（不在回收站、已上架、定时已到点，与 S1-4 的可见性一致）且分集已转封装完成才返回，否则视为不存在。
-- video_object_id 是当前产物，选线据此只挑已有就绪副本的存储源（S1-6f）；object_key 是产物目录，清单接口据此读清单原文、拼切片地址。
-- duration_sec 是整集时长，前台据此把进度条显示为整集长度；试看时清单只含前 trial_sec 秒，可播区间到此为止、其余为锁定段。
SELECT e.id, e.drama_id, e.access, e.trial_sec, e.duration_sec, e.video_object_id::bigint AS video_object_id, o.key AS object_key
FROM episode e
JOIN drama d ON d.id = e.drama_id
JOIN media_object o ON o.id = e.video_object_id
WHERE e.id = $1
  AND e.media_status = 'ready'
  AND o.kind = 'hls_dir'
  AND d.deleted_at IS NULL
  AND d.status = 'published'
  AND (d.publish_at IS NULL OR d.publish_at <= now());

-- name: GetMediaKey :one
-- 按版本取一集的内容密钥（信封加密，用 MEDIA_KEKS 解开）。清单里的密钥地址自带版本，重新转封装前的旧版本仍可取。
SELECT wrapped_key FROM media_key WHERE episode_id = $1 AND key_ver = $2;
