package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"xsite/service/internal/domain/billing"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

var _ billing.OrderRepo = (*BillingRepo)(nil)

func (r *BillingRepo) CreateVIPOrder(ctx context.Context, in billing.CreateVIPOrderCommand) (billing.Order, error) {
	var out billing.Order
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockPayOrderUser(ctx, in.UserID); err != nil {
			return fmt.Errorf("锁定下单用户: %w", err)
		}
		existing, err := q.FindPayOrderByIdempotency(ctx, sqlcgen.FindPayOrderByIdempotencyParams{UserID: in.UserID, IdempotencyKey: in.IdempotencyKey})
		if err == nil {
			out, err = orderFromFind(existing)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("读取幂等订单: %w", err)
		}

		planRow, err := q.GetVIPPlanForOrder(ctx, in.PlanID)
		if err != nil {
			return fmt.Errorf("锁定会员套餐: %w", err)
		}
		channelRow, err := q.GetPaymentChannelForOrder(ctx, string(in.Channel))
		if err != nil {
			return fmt.Errorf("锁定支付渠道: %w", err)
		}
		plan := billing.VIPOrderPlan{
			ID: planRow.ID, Code: planRow.Code, DurationDays: int(planRow.DurationDays), PriceFen: planRow.PriceFen,
			PriceMicroUSDT: planRow.PriceMicroUsdt, Channels: planRow.Channels, Clients: planRow.Clients, Status: planRow.Status,
		}
		snapshot, err := billing.BuildVIPOrderSnapshot(plan, in.Channel, in.Client)
		if err != nil {
			return err
		}
		channel := billing.ChannelConfig{
			Channel: billing.Channel(channelRow.Channel), Enabled: channelRow.Enabled, Clients: channelRow.Clients,
			RegionsAllow: channelRow.RegionsAllow, RegionsDeny: channelRow.RegionsDeny,
		}
		if in.Mock {
			// development 模拟不依赖真实凭据或渠道开关，但仍按客户端/地区范围限制。
			channel.Enabled = true
		}
		if !billing.ChannelAvailable(channel, in.Client, in.Country) {
			return errs.ErrUnavailable.WithMessage("支付渠道当前不可用")
		}
		pending, err := q.CountActivePendingPayOrders(ctx, in.UserID)
		if err != nil {
			return fmt.Errorf("统计待支付订单: %w", err)
		}
		if pending >= billing.PendingOrderLimit {
			return errs.ErrRateLimited.WithMessage("未支付订单过多，请先完成或取消已有订单")
		}
		snapshotJSON, err := json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("编码订单快照: %w", err)
		}
		row, err := q.InsertPayOrder(ctx, sqlcgen.InsertPayOrderParams{
			OrderNo: in.OrderNo, UserID: in.UserID, ProductType: string(billing.ProductVIPPlan), ProductID: in.PlanID,
			ProductSnapshot: snapshotJSON, Channel: string(in.Channel), Client: in.Client, Currency: string(snapshot.Currency),
			Amount: snapshot.Amount, ExpireAt: requiredTimestamptz(in.ExpireAt), IdempotencyKey: in.IdempotencyKey,
		})
		if err != nil {
			return fmt.Errorf("创建支付订单: %w", err)
		}
		out, err = orderFromInsert(row)
		return err
	})
	if err != nil {
		return billing.Order{}, paymentErr(err, "创建会员订单")
	}
	return out, nil
}

func (r *BillingRepo) GetPayOrder(ctx context.Context, userID int64, orderNo string) (billing.Order, error) {
	row, err := r.q.GetPayOrderForUser(ctx, sqlcgen.GetPayOrderForUserParams{UserID: userID, OrderNo: orderNo})
	if err != nil {
		return billing.Order{}, paymentErr(err, "读取支付订单")
	}
	return orderFromGet(row)
}

func (r *BillingRepo) ListPayOrders(ctx context.Context, userID, beforeID int64, limit int) ([]billing.Order, error) {
	rows, err := r.q.ListPayOrdersForUser(ctx, sqlcgen.ListPayOrdersForUserParams{UserID: userID, BeforeID: beforeID, Lim: int32(limit)})
	if err != nil {
		return nil, paymentErr(err, "读取我的订单")
	}
	out := make([]billing.Order, len(rows))
	for i, row := range rows {
		if out[i], err = orderFromList(row); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *BillingRepo) CancelPayOrder(ctx context.Context, userID int64, orderNo string) (billing.Order, error) {
	n, err := r.q.CancelPendingPayOrder(ctx, sqlcgen.CancelPendingPayOrderParams{UserID: userID, OrderNo: orderNo})
	if err != nil {
		return billing.Order{}, paymentErr(err, "取消支付订单")
	}
	order, err := r.GetPayOrder(ctx, userID, orderNo)
	if err != nil {
		return billing.Order{}, err
	}
	if n == 0 {
		return billing.Order{}, errs.ErrConflict.WithMessage("订单已关闭、已支付或已过期")
	}
	return order, nil
}

// CompleteMockPayOrder 是 development 模拟到账的事务实现。没有第三方回调原文，所以不写 pay_notify_log；
// production 不会注册调用它的 HTTP 路径。
func (r *BillingRepo) CompleteMockPayOrder(ctx context.Context, userID int64, orderNo string, paidAt time.Time) (billing.Order, error) {
	var out billing.Order
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		row, err := q.LockPayOrderForPayment(ctx, orderNo)
		if err != nil {
			return fmt.Errorf("锁定模拟订单: %w", err)
		}
		order, err := orderFromLocked(row)
		if err != nil {
			return err
		}
		if order.UserID != userID {
			return errs.ErrNotFound
		}
		if order.Status == billing.OrderPaid {
			out = order
			return nil
		}
		if !billing.CanOrderTransition(order.Status, billing.OrderPaid) {
			return errs.ErrConflict.WithMessage("订单当前不能完成支付")
		}
		paidAt = paidAt.UTC()
		late := order.Status == billing.OrderClosed || !order.ExpireAt.After(paidAt)
		tradeNo := "mock_" + order.OrderNo
		n, err := q.MarkPayOrderPaid(ctx, sqlcgen.MarkPayOrderPaidParams{
			ID: order.ID, ChannelTradeNo: tradeNo, PaidAt: requiredTimestamptz(paidAt), Late: late,
		})
		if err != nil {
			return fmt.Errorf("标记模拟订单支付: %w", err)
		}
		if n != 1 {
			return errs.ErrConflict.WithMessage("订单状态已变化，请刷新后重试")
		}
		if err := q.EnsureUserVIP(ctx, sqlcgen.EnsureUserVIPParams{UserID: userID, ExpireAt: requiredTimestamptz(paidAt), UpdatedAt: requiredTimestamptz(paidAt)}); err != nil {
			return fmt.Errorf("创建会员状态: %w", err)
		}
		vip, err := q.LockUserVIP(ctx, userID)
		if err != nil {
			return fmt.Errorf("锁定会员状态: %w", err)
		}
		_, to, err := billing.ExtendVIPUntil(vip.ExpireAt.Time, paidAt, order.Snapshot.DurationDays)
		if err != nil {
			return err
		}
		if err := q.UpdateUserVIPAfterPurchase(ctx, sqlcgen.UpdateUserVIPAfterPurchaseParams{
			UserID: userID, ExpireAt: requiredTimestamptz(to), DurationDays: int64(order.Snapshot.DurationDays), PaidAt: requiredTimestamptz(paidAt),
		}); err != nil {
			return fmt.Errorf("更新会员到期时间: %w", err)
		}
		if err := q.InsertVIPPurchaseLedger(ctx, sqlcgen.InsertVIPPurchaseLedgerParams{
			UserID: userID, OrderID: order.ID, ChangeDays: int32(order.Snapshot.DurationDays), FromExpire: vip.ExpireAt, ToExpire: requiredTimestamptz(to),
		}); err != nil {
			return fmt.Errorf("写会员流水: %w", err)
		}
		order.Status = billing.OrderPaid
		order.ChannelTradeNo = tradeNo
		order.PaidAt = &paidAt
		order.Late = late
		order.UpdatedAt = paidAt
		out = order
		return nil
	})
	if err != nil {
		return billing.Order{}, paymentErr(err, "完成模拟支付")
	}
	return out, nil
}

func paymentErr(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if errs.From(err) != errs.ErrInternal {
		return err
	}
	return fmt.Errorf("%s: %w", what, err)
}

type paymentOrderWire struct {
	ID              int64
	OrderNo         string
	UserID          int64
	ProductType     string
	ProductID       int64
	ProductSnapshot []byte
	Channel         string
	Client          string
	Currency        string
	Amount          int64
	Status          string
	ChannelTradeNo  pgtype.Text
	PaidAt          pgtype.Timestamptz
	ExpireAt        pgtype.Timestamptz
	Late            bool
	CreatedAt       pgtype.Timestamptz
	UpdatedAt       pgtype.Timestamptz
}

func orderOf(row paymentOrderWire) (billing.Order, error) {
	var snapshot billing.VIPOrderSnapshot
	if err := json.Unmarshal(row.ProductSnapshot, &snapshot); err != nil {
		return billing.Order{}, fmt.Errorf("解析订单商品快照: %w", err)
	}
	if !row.ExpireAt.Valid || !row.CreatedAt.Valid || !row.UpdatedAt.Valid {
		return billing.Order{}, fmt.Errorf("订单时间字段无效")
	}
	var paidAt *time.Time
	if row.PaidAt.Valid {
		v := row.PaidAt.Time.UTC()
		paidAt = &v
	}
	return billing.Order{
		ID: row.ID, OrderNo: row.OrderNo, UserID: row.UserID, ProductType: billing.ProductType(row.ProductType),
		ProductID: row.ProductID, Snapshot: snapshot, Channel: billing.Channel(row.Channel), Client: row.Client,
		Currency: billing.Currency(row.Currency), Amount: row.Amount, Status: billing.OrderStatus(row.Status),
		ChannelTradeNo: row.ChannelTradeNo.String, PaidAt: paidAt, ExpireAt: row.ExpireAt.Time.UTC(), Late: row.Late,
		CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC(),
	}, nil
}

func orderFromFind(row sqlcgen.FindPayOrderByIdempotencyRow) (billing.Order, error) {
	return orderOf(paymentOrderWire{ID: row.ID, OrderNo: row.OrderNo, UserID: row.UserID, ProductType: row.ProductType, ProductID: row.ProductID, ProductSnapshot: row.ProductSnapshot, Channel: row.Channel, Client: row.Client, Currency: row.Currency, Amount: row.Amount, Status: row.Status, ChannelTradeNo: row.ChannelTradeNo, PaidAt: row.PaidAt, ExpireAt: row.ExpireAt, Late: row.Late, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
}

func orderFromInsert(row sqlcgen.InsertPayOrderRow) (billing.Order, error) {
	return orderOf(paymentOrderWire{ID: row.ID, OrderNo: row.OrderNo, UserID: row.UserID, ProductType: row.ProductType, ProductID: row.ProductID, ProductSnapshot: row.ProductSnapshot, Channel: row.Channel, Client: row.Client, Currency: row.Currency, Amount: row.Amount, Status: row.Status, ChannelTradeNo: row.ChannelTradeNo, PaidAt: row.PaidAt, ExpireAt: row.ExpireAt, Late: row.Late, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
}

func orderFromGet(row sqlcgen.GetPayOrderForUserRow) (billing.Order, error) {
	return orderOf(paymentOrderWire{ID: row.ID, OrderNo: row.OrderNo, UserID: row.UserID, ProductType: row.ProductType, ProductID: row.ProductID, ProductSnapshot: row.ProductSnapshot, Channel: row.Channel, Client: row.Client, Currency: row.Currency, Amount: row.Amount, Status: row.Status, ChannelTradeNo: row.ChannelTradeNo, PaidAt: row.PaidAt, ExpireAt: row.ExpireAt, Late: row.Late, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
}

func orderFromList(row sqlcgen.ListPayOrdersForUserRow) (billing.Order, error) {
	return orderOf(paymentOrderWire{ID: row.ID, OrderNo: row.OrderNo, UserID: row.UserID, ProductType: row.ProductType, ProductID: row.ProductID, ProductSnapshot: row.ProductSnapshot, Channel: row.Channel, Client: row.Client, Currency: row.Currency, Amount: row.Amount, Status: row.Status, ChannelTradeNo: row.ChannelTradeNo, PaidAt: row.PaidAt, ExpireAt: row.ExpireAt, Late: row.Late, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
}

func orderFromLocked(row sqlcgen.LockPayOrderForPaymentRow) (billing.Order, error) {
	return orderOf(paymentOrderWire{ID: row.ID, OrderNo: row.OrderNo, UserID: row.UserID, ProductType: row.ProductType, ProductID: row.ProductID, ProductSnapshot: row.ProductSnapshot, Channel: row.Channel, Client: row.Client, Currency: row.Currency, Amount: row.Amount, Status: row.Status, ChannelTradeNo: row.ChannelTradeNo, PaidAt: row.PaidAt, ExpireAt: row.ExpireAt, Late: row.Late, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
}

func requiredTimestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
