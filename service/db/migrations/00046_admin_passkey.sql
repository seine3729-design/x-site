-- +goose Up
-- 后台二次验证 Passkey / WebAuthn（S2-6，docs/02 第 6.1 节、docs/06 第 33 节）。
-- 一个管理员可注册多把 Passkey。credential_id 是 WebAuthn 凭据 ID（原始字节），全局唯一、登录断言时按它查凭据。
-- public_key 为 COSE 公钥；sign_count 防重放（每次断言校验后写回，计数回退即疑似克隆）；transports 供前端优化选择器；
-- name 为用户给这把钥匙起的名字（便于在列表里区分 / 删除）。
CREATE TABLE admin_passkey (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    admin_id      bigint      NOT NULL REFERENCES admin_user (id) ON DELETE CASCADE,
    credential_id bytea       NOT NULL UNIQUE,
    public_key    bytea       NOT NULL,
    sign_count    bigint      NOT NULL DEFAULT 0,
    transports    text[]      NOT NULL DEFAULT '{}',
    name          text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz
);

-- 按管理员列出其全部 Passkey（登录断言、自助管理都要）。
CREATE INDEX admin_passkey_admin_idx ON admin_passkey (admin_id, id);

-- +goose Down
DROP TABLE admin_passkey;
