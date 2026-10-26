-- +goose Up
-- 审计哈希链（S4-7，docs/06 第 34 节）：每条记录带上一条的哈希，改动任何一行都会让链断开。
--
-- hash = SHA256(prev_hash ‖ admin_id ‖ action ‖ target_type ‖ target_id ‖ diff_before ‖ diff_after
--               ‖ ip_prefix ‖ request_id ‖ created_at)，字段间用单元分隔符 0x1F 隔开（避免拼接歧义）。
-- 刻意不把 id 放进哈希：id 是插入时才生成的，放进去会变成循环依赖；链的顺序由 prev_hash 表达，
-- id 只决定物理顺序，校验时按 id 升序走链。
--
-- 启用之前已有的记录 hash 与 prev_hash 为空串：它们的内容无法再追认（当时没有链），
-- 校验接口会跳过并单独报告数量，而不是假装它们在链上。
ALTER TABLE audit_log
    ADD COLUMN prev_hash text NOT NULL DEFAULT '',
    ADD COLUMN hash      text NOT NULL DEFAULT '';

-- 校验时按 id 升序扫描有哈希的记录
CREATE INDEX audit_log_hash_idx ON audit_log (id) WHERE hash <> '';

-- +goose Down
DROP INDEX audit_log_hash_idx;
ALTER TABLE audit_log DROP COLUMN hash, DROP COLUMN prev_hash;
