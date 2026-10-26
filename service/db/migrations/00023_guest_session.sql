-- +goose Up
-- 游客设备与会话（docs/02 第 5.1、5.1.1 节，S1-10）。

-- guest_device：设备级游客身份，服务端生成 UUIDv7，不存硬件标识；登录后并入账号（merged_user_id，user 表随 S2-1 建、届时补外键）。
CREATE TABLE guest_device (
    id             uuid        PRIMARY KEY,
    first_seen     timestamptz NOT NULL DEFAULT now(),
    source_channel text        NOT NULL DEFAULT '',
    platform       text        NOT NULL DEFAULT 'web' CHECK (platform IN ('web', 'ios', 'android')),
    attest_level   text        NOT NULL DEFAULT 'none' CHECK (attest_level IN ('none', 'basic', 'strong')),
    merged_user_id bigint,
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- user_session：刷新令牌的轮换族。user_id 与 guest_device_id 二选一（S1-10 只有游客）。
-- 刷新时新建行并吊销旧行；旧 refresh 令牌复用（命中已吊销的行）即判定泄露，吊销整族。服务端只存 refresh 的 SHA-256。
CREATE TABLE user_session (
    id              uuid        PRIMARY KEY,
    family_id       uuid        NOT NULL,
    user_id         bigint,
    guest_device_id uuid        REFERENCES guest_device (id) ON DELETE CASCADE,
    refresh_hash    bytea       NOT NULL UNIQUE,
    ua_digest       text        NOT NULL DEFAULT '',
    ip_prefix       text        NOT NULL DEFAULT '',
    platform        text        NOT NULL DEFAULT 'web' CHECK (platform IN ('web', 'ios', 'android')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_seen_at    timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    revoked_at      timestamptz,
    revoke_reason   text        NOT NULL DEFAULT '',
    -- 恰好绑定一个主体：游客用 guest_device_id，注册用户用 user_id（S2-1）
    CONSTRAINT user_session_subject_ck CHECK ((user_id IS NULL) <> (guest_device_id IS NULL))
);
-- 「我的设备」列表与改密吊销全部会话（S2-1）
CREATE INDEX user_session_user_idx ON user_session (user_id, revoked_at);
-- 游客会话查询与整族吊销
CREATE INDEX user_session_guest_idx ON user_session (guest_device_id, revoked_at);
CREATE INDEX user_session_family_idx ON user_session (family_id);
-- 过期会话清理（按 expires_at 批量删，运维 / 周期任务）
CREATE INDEX user_session_expires_idx ON user_session (expires_at);

-- +goose Down
DROP TABLE user_session;
DROP TABLE guest_device;
