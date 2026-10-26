-- +goose Up
-- 前台搜索（S3-4，docs/specs/S3-4.md）要大小写不敏感，而 pg_bigm 的 GIN 索引只支持 LIKE：
-- 实测 ILIKE 连索引路径都没有（SET enable_seqscan = off 后仍是顺序扫描），所以把小写形式单独建索引，
-- 查询必须写成 lower(title) LIKE … / lower(aliases_text(aliases)) LIKE … 才能用上（与 00004 的原始索引同一套机制）。
-- 搜索词在领域层经 internal/textnorm.Search 转小写，正好与这两条索引对齐。
-- drama 行数在万级且只有后台写入，普通 CREATE INDEX 够快，不用 CONCURRENTLY（本仓库的迁移都在事务里跑）。
CREATE INDEX drama_title_lower_bigm_idx   ON drama USING gin (lower(title) gin_bigm_ops);
CREATE INDEX drama_aliases_lower_bigm_idx ON drama USING gin (lower(aliases_text(aliases)) gin_bigm_ops);

-- +goose Down
DROP INDEX drama_aliases_lower_bigm_idx;
DROP INDEX drama_title_lower_bigm_idx;
