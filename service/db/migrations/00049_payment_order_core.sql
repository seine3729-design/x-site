-- +goose Up
-- 订单与支付框架（S3-11，docs/02 第 5.10 节、docs/03 第 12 节）。
-- 本迁移只建立渠道配置、订单、回调留痕与对账数据模型；具体渠道适配分别在 S3-12～S3-14 接入。

-- 渠道配置：凭据只以信封密文保存，后台只写不读。初始全部关闭，未配置真实价格/凭据时不可创建支付订单。
CREATE TABLE payment_channel (
    channel        text        PRIMARY KEY CHECK (channel IN ('alipay', 'wechat', 'usdt', 'iap_apple', 'iap_google')),
    enabled        boolean     NOT NULL DEFAULT false,
    clients        text[]      NOT NULL DEFAULT '{}' CHECK (clients <@ ARRAY['web_pc', 'web_mobile', 'app_official', 'app_store']::text[]),
    regions_allow  text[]      NOT NULL DEFAULT '{}',
    regions_deny   text[]      NOT NULL DEFAULT '{}',
    config         jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(config) = 'object'),
    credential_enc bytea,
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- 订单金额与商品快照在创建时固定，后续套餐改价不会影响已有订单。
CREATE TABLE pay_order (
    id               bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_no         text        NOT NULL UNIQUE CHECK (length(order_no) BETWEEN 20 AND 64),
    user_id          bigint      NOT NULL REFERENCES "user" (id) ON DELETE RESTRICT,
    product_type     text        NOT NULL CHECK (product_type IN ('vip_plan', 'coin_package')),
    product_id       bigint      NOT NULL CHECK (product_id > 0),
    product_snapshot jsonb       NOT NULL CHECK (jsonb_typeof(product_snapshot) = 'object'),
    coupon_id        bigint,
    discount_fen     bigint      NOT NULL DEFAULT 0 CHECK (discount_fen >= 0),
    channel          text        NOT NULL REFERENCES payment_channel (channel) ON DELETE RESTRICT,
    client           text        NOT NULL CHECK (client IN ('web_pc', 'web_mobile', 'app_official', 'app_store')),
    currency         text        NOT NULL CHECK (currency IN ('CNY', 'USDT')),
    amount           bigint      NOT NULL CHECK (amount >= 0),
    status           text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'paid', 'closed', 'refunded')),
    channel_trade_no text,
    paid_at          timestamptz,
    expire_at        timestamptz NOT NULL DEFAULT (now() + interval '30 minutes'),
    late             boolean     NOT NULL DEFAULT false,
    refund_amount    bigint      NOT NULL DEFAULT 0 CHECK (refund_amount >= 0),
    idempotency_key  text        NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, idempotency_key)
);
CREATE UNIQUE INDEX pay_order_channel_trade_no_uniq ON pay_order (channel, channel_trade_no) WHERE channel_trade_no IS NOT NULL;
CREATE INDEX pay_order_user_created_idx ON pay_order (user_id, created_at DESC, id DESC);
CREATE INDEX pay_order_pending_expire_idx ON pay_order (status, expire_at) WHERE status = 'pending';

-- 支付回调与链上事件留痕：原文只有 SHA-256 摘要和信封密文，禁止写入日志。
CREATE TABLE pay_notify_log (
    id               bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    channel          text        NOT NULL CHECK (channel IN ('alipay', 'wechat', 'usdt', 'iap_apple', 'iap_google')),
    order_no         text        NOT NULL DEFAULT '',
    channel_trade_no text        NOT NULL DEFAULT '',
    verified         boolean     NOT NULL DEFAULT false,
    amount           bigint,
    raw_digest       bytea       NOT NULL CHECK (octet_length(raw_digest) = 32),
    raw_enc          bytea       NOT NULL,
    received_at      timestamptz NOT NULL DEFAULT now(),
    handled_result   text        NOT NULL DEFAULT '' CHECK (length(handled_result) <= 200)
);
CREATE INDEX pay_notify_log_order_idx ON pay_notify_log (order_no, received_at DESC);
CREATE INDEX pay_notify_log_trade_idx ON pay_notify_log (channel, channel_trade_no) WHERE channel_trade_no <> '';

-- 每日对账结果；账单下载和人工处理在后续渠道/后台任务中接入。
CREATE TABLE pay_reconcile_daily (
    day          date        NOT NULL,
    channel      text        NOT NULL REFERENCES payment_channel (channel) ON DELETE RESTRICT,
    orders_paid  bigint      NOT NULL DEFAULT 0 CHECK (orders_paid >= 0),
    amount_paid  bigint      NOT NULL DEFAULT 0 CHECK (amount_paid >= 0),
    bill_count   bigint      NOT NULL DEFAULT 0 CHECK (bill_count >= 0),
    bill_amount  bigint      NOT NULL DEFAULT 0 CHECK (bill_amount >= 0),
    diff         jsonb       NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(diff) = 'object'),
    status       text        NOT NULL DEFAULT 'ok' CHECK (status IN ('ok', 'diff', 'manual_ok')),
    PRIMARY KEY (day, channel)
);

-- S3-10 已预留 order_id；订单表落地后补齐引用完整性，删除订单不会抹掉会员流水。
ALTER TABLE vip_ledger
    ADD CONSTRAINT vip_ledger_order_fk FOREIGN KEY (order_id) REFERENCES pay_order (id) ON DELETE SET NULL;

-- 渠道默认关闭：没有真实价格、凭据和 S3-12～14 适配器时，系统不能发起支付。
INSERT INTO payment_channel (channel, clients)
VALUES
    ('alipay', ARRAY['web_pc', 'web_mobile', 'app_official']::text[]),
    ('wechat', ARRAY['web_pc', 'web_mobile', 'app_official']::text[]),
    ('usdt', ARRAY['web_pc', 'web_mobile', 'app_official']::text[]),
    ('iap_apple', ARRAY['app_store']::text[]),
    ('iap_google', ARRAY['app_store']::text[])
ON CONFLICT (channel) DO NOTHING;

-- +goose Down
ALTER TABLE vip_ledger DROP CONSTRAINT IF EXISTS vip_ledger_order_fk;
DROP TABLE pay_reconcile_daily;
DROP TABLE pay_notify_log;
DROP TABLE pay_order;
DROP TABLE payment_channel;
