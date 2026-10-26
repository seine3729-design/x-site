-- 会员核心（S3-10，docs/03 第 12.1 节）。订单、支付渠道与地区规则在 S3-11 / S3-15 接入。

-- name: ListAvailableVIPPlans :many
-- 策略 A：当前仅按套餐允许客户端过滤；支付渠道的地区开关在 payment_channel 落地后再做交集。
SELECT id, code, duration_days, price_fen, list_price_fen, price_micro_usdt,
       badge_key, recommended, channels
FROM vip_plan
WHERE status = 'on'
  AND @client::text = ANY(clients)
ORDER BY sort ASC, id ASC;

-- name: GetVIPUntil :one
-- 没有会员记录时仓储层将 pgx.ErrNoRows 映射为零时间，不把“非会员”当作错误。
SELECT expire_at
FROM user_vip
WHERE user_id = @user_id::bigint;


-- ---------- 后台会员套餐（S3-15 / A3-5） ----------

-- name: ListAdminVIPPlans :many
SELECT id, code, duration_days, price_fen, list_price_fen, price_micro_usdt,
       badge_key, recommended, channels, clients, sort, status, updated_at
FROM vip_plan
ORDER BY sort ASC, id ASC;

-- name: GetAdminVIPPlan :one
SELECT id, code, duration_days, price_fen, list_price_fen, price_micro_usdt,
       badge_key, recommended, channels, clients, sort, status, updated_at
FROM vip_plan
WHERE id = @id::bigint;

-- name: UpdateAdminVIPPlan :one
UPDATE vip_plan
SET duration_days = @duration_days::int,
    price_fen = @price_fen::bigint,
    list_price_fen = @list_price_fen::bigint,
    price_micro_usdt = @price_micro_usdt::bigint,
    badge_key = sqlc.narg(badge_key)::text,
    recommended = @recommended::boolean,
    channels = @channels::text[],
    clients = @clients::text[],
    sort = @sort::int,
    status = @status::text,
    updated_at = now()
WHERE id = @id::bigint
RETURNING id, code, duration_days, price_fen, list_price_fen, price_micro_usdt,
          badge_key, recommended, channels, clients, sort, status, updated_at;
