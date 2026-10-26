-- +goose Up
-- 前台「最新」排序（S1-4，docs/03 第 3.2 节）的索引。
-- 迁移 00004 的 drama_status_publish_idx 是 (status, publish_at DESC, id DESC)，但前台的排序键是
-- coalesce(publish_at, created_at)：已上架的剧一定有上架时间，极少数历史数据没有时按创建时间排，
-- 这样游标的行值比较里不会出现 NULL。表达式与索引键不一致时规划器只能全表扫描 + 排序，
-- 而「最新」是探索、标签、频道三个页面的默认排序，所以单独为它建部分索引。
-- 部分索引的条件只用不可变表达式；publish_at <= now() 仍作为过滤条件。
CREATE INDEX drama_public_latest_idx ON drama (coalesce(publish_at, created_at) DESC, id DESC)
    WHERE deleted_at IS NULL AND status = 'published';

-- +goose Down
DROP INDEX drama_public_latest_idx;
