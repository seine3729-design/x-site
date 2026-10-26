-- 媒体对象的孤立标记（S1-6f，docs/12 第 5 节第 6 条）。
-- 分集删除后 media_object.episode_id 置空、重新转封装后旧产物不再被 episode.video_object_id 引用，
-- 这些对象要在保留期（回收站 30 天）后连同各存储源上的物理文件一起清理。
-- orphaned_at 记「首次被发现不再被引用」的时间，保留期从它开始算 —— 用 created_at 算会让老产物一发现就被删。
-- +goose Up
ALTER TABLE media_object ADD COLUMN orphaned_at timestamptz;

-- 清理任务按 orphaned_at 找到期的对象；部分索引只覆盖已标记的行（绝大多数对象在用，不进索引）
CREATE INDEX media_object_orphaned_idx ON media_object (orphaned_at) WHERE orphaned_at IS NOT NULL;

-- +goose Down
DROP INDEX media_object_orphaned_idx;
ALTER TABLE media_object DROP COLUMN orphaned_at;
