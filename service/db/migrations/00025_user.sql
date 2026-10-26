-- +goose Up
-- 注册用户（docs/02 第 5.1 节，S2-1）。用户名统一存小写、大小写不敏感唯一；展示用 nickname。
-- 不提供找回密码（adr/0001），因此不存恢复码、邮箱、手机号。资料审核字段见 docs/13 第 2.5 节。
CREATE TABLE "user" (
    id                  bigint        GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username            text          NOT NULL UNIQUE CHECK (username = lower(username) AND username ~ '^[a-z0-9_]{3,32}$'),
    nickname            text          NOT NULL DEFAULT '' CHECK (length(nickname) <= 40),
    password_hash       text          NOT NULL CHECK (password_hash <> ''),   -- Argon2id PHC 串
    avatar              text          NOT NULL DEFAULT '',
    bio                 text          NOT NULL DEFAULT '' CHECK (length(bio) <= 80),
    nickname_status     text          NOT NULL DEFAULT 'ok' CHECK (nickname_status IN ('ok', 'pending', 'rejected')),
    bio_status          text          NOT NULL DEFAULT 'ok' CHECK (bio_status      IN ('ok', 'pending', 'rejected')),
    avatar_status       text          NOT NULL DEFAULT 'ok' CHECK (avatar_status   IN ('ok', 'pending', 'rejected')),
    nickname_changed_at timestamptz[] NOT NULL DEFAULT '{}',                  -- 近 30 天改名记录（docs/13 第 2.5 节）
    locale              text,                                                 -- 界面语言偏好，可空
    history_paused      boolean       NOT NULL DEFAULT false,
    status              text          NOT NULL DEFAULT 'normal' CHECK (status IN ('normal', 'muted', 'banned')),
    verified_at         timestamptz,                                          -- 实名 / 手机号验证时间，按运营地开关启用
    created_at          timestamptz   NOT NULL DEFAULT now(),
    deleted_at          timestamptz
);

-- 回填 S1-10（迁移 00023）预留的外键：user 表此时才建。
-- 删用户连带删其会话；游客设备保留（只清合并标记，merged_user_id 仅用于统计）。
ALTER TABLE user_session
    ADD CONSTRAINT user_session_user_fk FOREIGN KEY (user_id) REFERENCES "user" (id) ON DELETE CASCADE;
ALTER TABLE guest_device
    ADD CONSTRAINT guest_device_merged_user_fk FOREIGN KEY (merged_user_id) REFERENCES "user" (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE guest_device DROP CONSTRAINT IF EXISTS guest_device_merged_user_fk;
ALTER TABLE user_session DROP CONSTRAINT IF EXISTS user_session_user_fk;
DROP TABLE IF EXISTS "user";
