-- +goose Up
-- 后台管理员（docs/02 第 6.1 节）。用户名统一存小写，不区分大小写唯一；二次验证字段在 S2-6 使用。
CREATE TABLE admin_user (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    username        text        NOT NULL UNIQUE CHECK (username = lower(username) AND length(username) BETWEEN 3 AND 32),
    password_hash   text        NOT NULL,
    role            text        NOT NULL CHECK (role IN ('superadmin', 'operator', 'moderator', 'readonly')),
    status          text        NOT NULL DEFAULT 'unused' CHECK (status IN ('active', 'inactive', 'unused')),
    totp_secret_enc text,
    mfa_required    boolean     NOT NULL DEFAULT false,
    last_login_at   timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- 后台登录记录，安全中心展示（docs/06 第 39 节）；只存 IP 网段与 UA 摘要，保留 180 天（docs/10 第 1 节）。
CREATE TABLE admin_login_log (
    id             bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    admin_id       bigint      REFERENCES admin_user (id) ON DELETE SET NULL,
    username_tried text        NOT NULL,
    result         text        NOT NULL CHECK (result IN ('success', 'bad_password', 'inactive', 'mfa_failed', 'locked')),
    ip_prefix      text        NOT NULL DEFAULT '',
    ua_digest      text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_login_log_created_at_idx ON admin_login_log (created_at DESC);
CREATE INDEX admin_login_log_admin_idx ON admin_login_log (admin_id, created_at DESC);

-- 审计日志（docs/02 第 6.2 节）。哈希链 prev_hash / row_hash 与校验接口在 S4-7 加入。
-- 管理员被审计引用后不能物理删除（外键无级联），只能停用。
CREATE TABLE audit_log (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    admin_id    bigint      NOT NULL REFERENCES admin_user (id),
    action      text        NOT NULL,
    target_type text        NOT NULL DEFAULT '',
    target_id   text        NOT NULL DEFAULT '',
    diff_before jsonb,
    diff_after  jsonb,
    ip_prefix   text        NOT NULL DEFAULT '',
    request_id  text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_log_search_idx ON audit_log (admin_id, action, target_type, created_at DESC);
CREATE INDEX audit_log_created_at_idx ON audit_log (created_at DESC);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE admin_login_log;
DROP TABLE admin_user;
