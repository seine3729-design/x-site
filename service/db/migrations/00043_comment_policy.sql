-- +goose Up
-- 评论先审后显开关（S2-5，docs/06 第 24 节「设置：先审后显（comment_policy）」）。
-- 后端专用段：只影响评论发表时的落库状态（pre_moderation=true 时所有评论先进待审），
-- 不随 GET /api/v1/site-config 下发、不参与站点版本（与 ranking 同属后台读写段）。
ALTER TABLE site_config ADD COLUMN comment_policy jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE site_config DROP COLUMN comment_policy;
