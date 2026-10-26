-- 订单与支付框架（S3-11）。调用写查询的 pg 仓储必须用同一事务锁定套餐、渠道、订单和会员行。

-- name: LockPayOrderUser :exec
-- 每个注册用户的下单事务串行化：先查同幂等键，再统计 pending，避免并发请求突破三笔上限。
SELECT pg_advisory_xact_lock(@user_id::bigint);

-- name: GetVIPPlanForOrder :one
SELECT id, code, duration_days, price_fen, price_micro_usdt, channels, clients, status
FROM vip_plan
WHERE id = @plan_id::bigint
FOR UPDATE;

-- name: GetPaymentChannelForOrder :one
SELECT channel, enabled, clients, regions_allow, regions_deny
FROM payment_channel
WHERE channel = @channel::text
FOR UPDATE;

-- name: FindPayOrderByIdempotency :one
SELECT id, order_no, user_id, product_type, product_id, product_snapshot, channel, client, currency, amount,
       status, channel_trade_no, paid_at, expire_at, late, created_at, updated_at
FROM pay_order
WHERE user_id = @user_id::bigint
  AND idempotency_key = @idempotency_key::text
FOR UPDATE;

-- name: CountActivePendingPayOrders :one
SELECT count(*)
FROM pay_order
WHERE user_id = @user_id::bigint
  AND status = 'pending'
  AND expire_at > now();

-- name: InsertPayOrder :one
INSERT INTO pay_order (
    order_no, user_id, product_type, product_id, product_snapshot, coupon_id, discount_fen,
    channel, client, currency, amount, status, expire_at, idempotency_key
) VALUES (
    @order_no::text, @user_id::bigint, @product_type::text, @product_id::bigint, @product_snapshot::jsonb,
    sqlc.narg(coupon_id)::bigint, @discount_fen::bigint, @channel::text, @client::text, @currency::text,
    @amount::bigint, 'pending', @expire_at::timestamptz, @idempotency_key::text
)
RETURNING id, order_no, user_id, product_type, product_id, product_snapshot, channel, client, currency, amount,
          status, channel_trade_no, paid_at, expire_at, late, created_at, updated_at;

-- name: GetPayOrderForUser :one
SELECT id, order_no, user_id, product_type, product_id, product_snapshot, channel, client, currency, amount,
       status, channel_trade_no, paid_at, expire_at, late, created_at, updated_at
FROM pay_order
WHERE order_no = @order_no::text
  AND user_id = @user_id::bigint;

-- name: ListPayOrdersForUser :many
SELECT id, order_no, user_id, product_type, product_id, product_snapshot, channel, client, currency, amount,
       status, channel_trade_no, paid_at, expire_at, late, created_at, updated_at
FROM pay_order
WHERE user_id = @user_id::bigint
  AND (@before_id::bigint = 0 OR id < @before_id::bigint)
ORDER BY id DESC
LIMIT @lim::int;

-- name: CancelPendingPayOrder :execrows
UPDATE pay_order
SET status = 'closed', updated_at = now()
WHERE order_no = @order_no::text
  AND user_id = @user_id::bigint
  AND status = 'pending'
  AND expire_at > now();

-- name: LockPayOrderForPayment :one
SELECT id, order_no, user_id, product_type, product_id, product_snapshot, channel, client, currency, amount,
       status, channel_trade_no, paid_at, expire_at, late, created_at, updated_at
FROM pay_order
WHERE order_no = @order_no::text
FOR UPDATE;

-- name: MarkPayOrderPaid :execrows
UPDATE pay_order
SET status = 'paid', channel_trade_no = @channel_trade_no::text, paid_at = @paid_at::timestamptz,
    late = @late::boolean, updated_at = @paid_at::timestamptz
WHERE id = @id::bigint
  AND status IN ('pending', 'closed');

-- name: EnsureUserVIP :exec
INSERT INTO user_vip (user_id, expire_at, total_days, updated_at)
VALUES (@user_id::bigint, @expire_at::timestamptz, 0, @updated_at::timestamptz)
ON CONFLICT (user_id) DO NOTHING;

-- name: LockUserVIP :one
SELECT user_id, expire_at, total_days, first_paid_at, updated_at
FROM user_vip
WHERE user_id = @user_id::bigint
FOR UPDATE;

-- name: UpdateUserVIPAfterPurchase :exec
UPDATE user_vip
SET expire_at = @expire_at::timestamptz,
    total_days = total_days + @duration_days::bigint,
    first_paid_at = COALESCE(first_paid_at, @paid_at::timestamptz),
    updated_at = @paid_at::timestamptz
WHERE user_id = @user_id::bigint;

-- name: InsertVIPPurchaseLedger :exec
INSERT INTO vip_ledger (user_id, order_id, change_days, from_expire, to_expire, reason, note)
VALUES (@user_id::bigint, @order_id::bigint, @change_days::int, @from_expire::timestamptz,
        @to_expire::timestamptz, 'purchase', '');

-- name: InsertPayNotifyLog :one
INSERT INTO pay_notify_log (
    channel, order_no, channel_trade_no, verified, amount, raw_digest, raw_enc, handled_result
) VALUES (
    @channel::text, @order_no::text, @channel_trade_no::text, @verified::boolean,
    sqlc.narg(amount)::bigint, @raw_digest::bytea, @raw_enc::bytea, @handled_result::text
)
RETURNING id;

-- name: CloseExpiredPayOrders :many
UPDATE pay_order
SET status = 'closed', updated_at = now()
WHERE status = 'pending'
  AND expire_at <= now()
RETURNING id, order_no, user_id, channel;
