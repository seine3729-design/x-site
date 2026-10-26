-- 把种子数据的时间往前摊开。
--
-- 只在本地开发库上跑。种子数据是刚才一次性造出来的，所有时间都是「刚刚」，
-- 页面上满屏「刚刚」看不出相对时间、排序和时间线的真实效果。
-- 这个脚本只改 fb_* 表的时间字段，不动任何业务内容。
--
-- 用法（仓库根目录）：
--   docker compose -f compose.yaml --env-file .env exec -T postgres \
--     psql -U xsite_app -d xsite < feedback/scripts/spread-dev-time.sql

BEGIN;

-- 用户：越早注册的编号越小
WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY id DESC) AS rn FROM fb_user
)
UPDATE fb_user u
SET created_at = now() - (o.rn * interval '4 days') - (random() * interval '12 hours')
FROM ordered o
WHERE o.id = u.id;

-- 项目：按排序值铺在过去三个月里
WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY sort, id) AS rn FROM fb_project
)
UPDATE fb_project p
SET created_at = now() - (interval '90 days') + (o.rn * interval '12 days'),
    updated_at = now() - (o.rn * interval '2 days')
FROM ordered o
WHERE o.id = p.id;

-- 反馈：id 越大越新，一条一条往前推约一天
WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY id DESC) AS rn FROM fb_feedback
)
UPDATE fb_feedback f
SET created_at = now() - (o.rn * interval '26 hours') - (random() * interval '6 hours')
FROM ordered o
WHERE o.id = f.id;

-- 最近活动时间：在创建之后，且越新的反馈活动越近。
-- 还在处理中的（in_progress / verifying）活动时间压到最近两天内，
-- 这样列表按「最近更新」排序时它们会靠前，符合直觉。
UPDATE fb_feedback
SET last_activity_at = CASE
    WHEN status IN ('in_progress', 'verifying') THEN now() - (random() * interval '2 days')
    WHEN status IN ('submitted') THEN created_at + (random() * interval '2 hours')
    ELSE created_at + (random() * (now() - created_at) * 0.8)
END;

UPDATE fb_feedback
SET last_activity_at = greatest(last_activity_at, created_at);

-- 状态历史：第一条落在反馈创建时刻，其余按顺序均匀铺到最近活动时间
WITH e AS (
    SELECT ev.id,
           row_number() OVER (PARTITION BY ev.feedback_id ORDER BY ev.id) AS rn,
           count(*) OVER (PARTITION BY ev.feedback_id) AS cnt,
           f.created_at,
           f.last_activity_at
    FROM fb_feedback_event ev
    JOIN fb_feedback f ON f.id = ev.feedback_id
)
UPDATE fb_feedback_event ev
SET created_at = e.created_at
    + (e.last_activity_at - e.created_at) * ((e.rn - 1)::numeric / greatest(e.cnt - 1, 1))
FROM e
WHERE e.id = ev.id;

-- 动态：置顶的放最近，其余往前推
WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY pinned DESC, id DESC) AS rn FROM fb_post
)
UPDATE fb_post p
SET published_at = CASE WHEN p.status = 'published'
        THEN now() - (o.rn * interval '38 hours') - (random() * interval '5 hours')
        ELSE p.published_at END,
    created_at = now() - (o.rn * interval '40 hours'),
    updated_at = now() - (o.rn * interval '38 hours')
FROM ordered o
WHERE o.id = p.id;

-- 议题：征集中的更近
WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY (status = 'open') DESC, id DESC) AS rn FROM fb_topic
)
UPDATE fb_topic t
SET published_at = CASE WHEN t.status <> 'draft'
        THEN now() - (o.rn * interval '5 days') - (random() * interval '8 hours')
        ELSE t.published_at END,
    created_at = now() - (o.rn * interval '5 days') - interval '1 hour',
    updated_at = now() - (o.rn * interval '4 days')
FROM ordered o
WHERE o.id = t.id;

-- 议题回答：按顺序铺在议题发布之后
WITH a AS (
    SELECT ans.id,
           row_number() OVER (PARTITION BY ans.topic_id ORDER BY ans.id) AS rn,
           count(*) OVER (PARTITION BY ans.topic_id) AS cnt,
           coalesce(t.published_at, t.created_at) AS start_at
    FROM fb_topic_answer ans
    JOIN fb_topic t ON t.id = ans.topic_id
)
UPDATE fb_topic_answer ans
SET created_at = a.start_at + (now() - a.start_at) * (a.rn::numeric / (a.cnt + 1)),
    updated_at = a.start_at + (now() - a.start_at) * (a.rn::numeric / (a.cnt + 1))
FROM a
WHERE a.id = ans.id;

-- 给议题设一个还没到的截止时间，让「剩 N 天」有东西可显示
UPDATE fb_topic
SET ends_at = now() + interval '6 days' + (random() * interval '2 days')
WHERE status = 'open' AND ends_at IS NULL;

COMMIT;

SELECT 'feedback' AS t,
       to_char(min(created_at), 'MM-DD HH24:MI') AS 最早,
       to_char(max(created_at), 'MM-DD HH24:MI') AS 最新
FROM fb_feedback
UNION ALL
SELECT 'post', to_char(min(published_at), 'MM-DD HH24:MI'), to_char(max(published_at), 'MM-DD HH24:MI')
FROM fb_post
UNION ALL
SELECT 'topic', to_char(min(published_at), 'MM-DD HH24:MI'), to_char(max(published_at), 'MM-DD HH24:MI')
FROM fb_topic;
