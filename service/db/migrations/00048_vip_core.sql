-- +goose Up
-- 会员核心（S3-10，docs/02 第 5.10 节、docs/03 第 12.1 节）。
-- 支付订单、渠道凭据与后台配置留给 S3-11 / S3-15；本迁移只建立套餐、会员状态与只追加流水。

CREATE TABLE vip_plan (
    id               bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code             text        NOT NULL UNIQUE CHECK (code IN ('day', 'week', 'month', 'quarter', 'year')),
    duration_days    int         NOT NULL CHECK (duration_days > 0),
    price_fen        bigint      NOT NULL DEFAULT 0 CHECK (price_fen >= 0),
    list_price_fen   bigint      NOT NULL DEFAULT 0 CHECK (list_price_fen >= 0),
    price_micro_usdt bigint      NOT NULL DEFAULT 0 CHECK (price_micro_usdt >= 0),
    badge_key        text,
    recommended      boolean     NOT NULL DEFAULT false,
    channels         text[]      NOT NULL DEFAULT '{}' CHECK (channels <@ ARRAY['alipay', 'wechat', 'usdt', 'iap_apple', 'iap_google']::text[]),
    clients          text[]      NOT NULL DEFAULT '{}' CHECK (clients <@ ARRAY['web_pc', 'web_mobile', 'app_official', 'app_store']::text[]),
    sort             int         NOT NULL DEFAULT 0 CHECK (sort BETWEEN 0 AND 9999),
    status           text        NOT NULL DEFAULT 'off' CHECK (status IN ('on', 'off')),
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX vip_plan_available_idx ON vip_plan (status, sort, id);

-- 会员状态一位用户一行；到期前可按 max(expire_at, now()) 顺延，所有变动同时写 vip_ledger。
CREATE TABLE user_vip (
    user_id       bigint      PRIMARY KEY REFERENCES "user" (id) ON DELETE CASCADE,
    expire_at     timestamptz NOT NULL,
    total_days    bigint      NOT NULL DEFAULT 0 CHECK (total_days >= 0),
    first_paid_at timestamptz,
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX user_vip_expire_idx ON user_vip (expire_at);

-- 会员流水只追加。order_id 在 S3-11 创建 pay_order 后补外键，避免提前耦合尚未存在的订单表。
CREATE TABLE vip_ledger (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     bigint      NOT NULL REFERENCES "user" (id) ON DELETE CASCADE,
    order_id    bigint,
    change_days int         NOT NULL CHECK (change_days <> 0),
    from_expire timestamptz NOT NULL,
    to_expire   timestamptz NOT NULL,
    reason      text        NOT NULL CHECK (reason IN ('purchase', 'refund', 'admin_grant', 'admin_revoke', 'migrate_in', 'migrate_out', 'compensation', 'reward', 'mall')),
    admin_id    bigint      REFERENCES admin_user (id) ON DELETE SET NULL,
    note        text        NOT NULL DEFAULT '' CHECK (length(note) <= 500),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX vip_ledger_user_created_idx ON vip_ledger (user_id, created_at DESC, id DESC);
CREATE INDEX vip_ledger_order_idx ON vip_ledger (order_id) WHERE order_id IS NOT NULL;

-- 尚未确认真实价格时安全默认下架：结构、时长、客户端与未来渠道范围先固定，不向用户显示或开放购买。
INSERT INTO vip_plan (code, duration_days, channels, clients, sort, status)
VALUES
    ('day',     1, ARRAY['alipay', 'wechat', 'usdt']::text[], ARRAY['web_pc', 'web_mobile', 'app_official', 'app_store']::text[], 10, 'off'),
    ('week',    7, ARRAY['alipay', 'wechat', 'usdt']::text[], ARRAY['web_pc', 'web_mobile', 'app_official', 'app_store']::text[], 20, 'off'),
    ('month',  30, ARRAY['alipay', 'wechat', 'usdt']::text[], ARRAY['web_pc', 'web_mobile', 'app_official', 'app_store']::text[], 30, 'off'),
    ('quarter',90, ARRAY['alipay', 'wechat', 'usdt']::text[], ARRAY['web_pc', 'web_mobile', 'app_official', 'app_store']::text[], 40, 'off'),
    ('year',  365, ARRAY['alipay', 'wechat', 'usdt']::text[], ARRAY['web_pc', 'web_mobile', 'app_official', 'app_store']::text[], 50, 'off')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DROP TABLE vip_ledger;
DROP TABLE user_vip;
DROP TABLE vip_plan;
