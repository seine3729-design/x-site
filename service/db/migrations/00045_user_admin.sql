-- +goose Up
-- 用户管理（S2-7 / A2-2，docs/06 第 26 节）只加索引：user 表的 status（CHECK normal/muted/banned）
-- 与 deleted_at 在 00025 就已就位，账号状态与回收站不需要新列。

-- 列表按注册时间倒序取最近若干位，可按账号状态筛，所以建 (status, created_at DESC) 复合索引；
-- 不筛状态时走 (created_at DESC)。两条都带 deleted_at IS NULL 部分条件：回收站里的账号不进列表，
-- 部分索引既小，又与查询的 WHERE 完全对齐。
CREATE INDEX user_status_created_idx ON "user" (status, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX user_created_idx        ON "user" (created_at DESC)         WHERE deleted_at IS NULL;

-- 搜用户名 / 昵称要大小写不敏感的子串匹配。ILIKE 用不上 GIN 索引（00035 里有实测结论），
-- 所以沿用同一套机制：对小写形式建 pg_bigm 索引，查询写成 lower(x) LIKE '%词%'。
-- user 表的写入只有注册与改资料，频率远低于后台检索，GIN 的写放大可以接受。
CREATE INDEX user_username_lower_bigm_idx ON "user" USING gin (lower(username) gin_bigm_ops);
CREATE INDEX user_nickname_lower_bigm_idx ON "user" USING gin (lower(nickname) gin_bigm_ops);

-- 游客列表按首次出现倒序（guest_device 只有主键索引）。
CREATE INDEX guest_device_first_seen_idx ON guest_device (first_seen DESC);

-- +goose Down
DROP INDEX guest_device_first_seen_idx;
DROP INDEX user_nickname_lower_bigm_idx;
DROP INDEX user_username_lower_bigm_idx;
DROP INDEX user_created_idx;
DROP INDEX user_status_created_idx;
