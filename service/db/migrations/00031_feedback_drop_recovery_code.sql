-- 社区账号去掉恢复码（feedback/docs/adr/0011）。
--
-- 用户明确要求删除恢复码与「忘记密码」，并确认知悉代价：忘记密码即永久失去账号，
-- 运营也没有代为重置的手段。
--
-- 这里**刻意不 DROP COLUMN**，只解除约束：
--   - DROP COLUMN 在生产上不可逆，而列里存的是哈希，删了也换不回明文，收益为零；
--   - 保留列让这次改动可以整体回滚（down 把约束加回去即可）；
--   - 保持 NOT NULL + DEFAULT '' 则 sqlc 生成的字段类型不变（仍是 string），
--     不会连带把 models.go 里所有 fb_user 的扫描代码改一遍。
--
-- 应用层已经完全不读写这一列（见 db/queries/feedback.sql）。
-- 等这个决定稳定一段时间、确认不需要回滚之后，可以单独发一个迁移把列删掉。

-- +goose Up

-- CHECK 必须先去掉：应用层不再提供值，走 DEFAULT '' 会直接撞上 <> '' 这个约束。
ALTER TABLE fb_user DROP CONSTRAINT IF EXISTS fb_user_recovery_code_hash_check;

-- 列保持 NOT NULL，靠默认值满足：新注册的行不再带恢复码哈希。
ALTER TABLE fb_user ALTER COLUMN recovery_code_hash SET DEFAULT '';

-- 已有账号的恢复码一并作废：留着也没有任何代码会去校验它，
-- 留着反而是一份无人使用却仍可被读取的敏感数据。
UPDATE fb_user SET recovery_code_hash = '' WHERE recovery_code_hash <> '';

COMMENT ON COLUMN fb_user.recovery_code_hash IS
    '已废弃（adr/0011）：恢复码功能已删除，此列恒为空串，不再读写。';

-- +goose Down

-- 注意：原有的恢复码哈希在 Up 里已被清空，回滚只能恢复结构，不能恢复数据。
-- 回滚后所有账号的恢复码都是空串，而 CHECK 要求非空 —— 所以先填一个占位值，
-- 否则加约束会直接失败。占位值不是任何有效恢复码的哈希，校验必然不通过，
-- 也就是说回滚后仍然没有人能用恢复码重置密码，需要重新走一遍注册发码流程。
UPDATE fb_user SET recovery_code_hash = 'revoked' WHERE recovery_code_hash = '';

ALTER TABLE fb_user ALTER COLUMN recovery_code_hash DROP DEFAULT;

ALTER TABLE fb_user ADD CONSTRAINT fb_user_recovery_code_hash_check
    CHECK (recovery_code_hash <> '');

COMMENT ON COLUMN fb_user.recovery_code_hash IS NULL;
