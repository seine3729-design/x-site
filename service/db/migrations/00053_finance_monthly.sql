-- +goose Up
-- 收入与成本（A4-1 / S4-3，docs/06 第 23 节、docs/03 第 11 节收益板块）：运营按月手填收支，供数据看板⑦核算毛利。
-- 会员收入不手填，由后台从 pay_order 已支付订单按月自动汇总（按渠道、按套餐、分币种）。
-- 金额一律存人民币分（bigint fen），与订单口径一致；每项可写备注，每月一条。
CREATE TABLE finance_monthly (
    month            date        PRIMARY KEY,  -- 当月 1 号（北京月）
    ad_revenue       bigint      NOT NULL DEFAULT 0 CHECK (ad_revenue >= 0),       -- 本月广告收入（分）
    referral_revenue bigint      NOT NULL DEFAULT 0 CHECK (referral_revenue >= 0), -- 家族站导流 / 互推收入（分）
    traffic_cost     bigint      NOT NULL DEFAULT 0 CHECK (traffic_cost >= 0),     -- 视频流量费（分）
    server_cost      bigint      NOT NULL DEFAULT 0 CHECK (server_cost >= 0),      -- 服务器 / 数据库费（分）
    payment_fee      bigint      NOT NULL DEFAULT 0 CHECK (payment_fee >= 0),      -- 支付手续费（分）
    ad_note          text        NOT NULL DEFAULT '' CHECK (length(ad_note) <= 500),
    referral_note    text        NOT NULL DEFAULT '' CHECK (length(referral_note) <= 500),
    traffic_note     text        NOT NULL DEFAULT '' CHECK (length(traffic_note) <= 500),
    server_note      text        NOT NULL DEFAULT '' CHECK (length(server_note) <= 500),
    payment_note     text        NOT NULL DEFAULT '' CHECK (length(payment_note) <= 500),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE finance_monthly;
