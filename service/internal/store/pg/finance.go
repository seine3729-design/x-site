package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/finance"
)

// FinanceRepo 读写月度手填收支，并从 pay_order 按月汇总会员收入（docs/06 第 23 节，A4-1）。
// 全部手写参数化 SQL：upsert 的 ON CONFLICT 与多维聚合不经 sqlc。
type FinanceRepo struct {
	pool *pgxpool.Pool
}

func NewFinanceRepo(pool *pgxpool.Pool) *FinanceRepo {
	return &FinanceRepo{pool: pool}
}

func (r *FinanceRepo) GetMonth(ctx context.Context, monthStart time.Time) (finance.Entry, error) {
	var e finance.Entry
	var updatedAt pgtype.Timestamptz
	err := r.pool.QueryRow(ctx,
		`SELECT ad_revenue, referral_revenue, traffic_cost, server_cost, payment_fee,
		        ad_note, referral_note, traffic_note, server_note, payment_note, updated_at
		 FROM finance_monthly WHERE month = $1`, pgDay(monthStart)).
		Scan(&e.AdRevenue, &e.ReferralRevenue, &e.TrafficCost, &e.ServerCost, &e.PaymentFee,
			&e.AdNote, &e.ReferralNote, &e.TrafficNote, &e.ServerNote, &e.PaymentNote, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return finance.Entry{}, nil
	}
	if err != nil {
		return finance.Entry{}, fmt.Errorf("读取月度收支: %w", err)
	}
	e.Saved = true
	if updatedAt.Valid {
		t := updatedAt.Time
		e.UpdatedAt = &t
	}
	return e, nil
}

func (r *FinanceRepo) SaveMonth(ctx context.Context, monthStart time.Time, in finance.Input) (finance.Entry, error) {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO finance_monthly (month, ad_revenue, referral_revenue, traffic_cost, server_cost, payment_fee,
		                              ad_note, referral_note, traffic_note, server_note, payment_note, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11, now())
		 ON CONFLICT (month) DO UPDATE SET
		   ad_revenue=EXCLUDED.ad_revenue, referral_revenue=EXCLUDED.referral_revenue,
		   traffic_cost=EXCLUDED.traffic_cost, server_cost=EXCLUDED.server_cost, payment_fee=EXCLUDED.payment_fee,
		   ad_note=EXCLUDED.ad_note, referral_note=EXCLUDED.referral_note, traffic_note=EXCLUDED.traffic_note,
		   server_note=EXCLUDED.server_note, payment_note=EXCLUDED.payment_note, updated_at=now()`,
		pgDay(monthStart), in.AdRevenue, in.ReferralRevenue, in.TrafficCost, in.ServerCost, in.PaymentFee,
		in.AdNote, in.ReferralNote, in.TrafficNote, in.ServerNote, in.PaymentNote)
	if err != nil {
		return finance.Entry{}, fmt.Errorf("保存月度收支: %w", err)
	}
	return r.GetMonth(ctx, monthStart)
}

// MembershipRevenue 汇总 [from, to)（UTC）内已支付的 vip_plan 订单。
// 口径：按订单 paid_at 归属月，gross=sum(amount)、refund=sum(refund_amount)（退款归到原支付月，第一版简化）。
func (r *FinanceRepo) MembershipRevenue(ctx context.Context, from, to time.Time) (finance.Membership, error) {
	ch, err := r.channelRevenue(ctx, from, to)
	if err != nil {
		return finance.Membership{}, err
	}
	pl, err := r.planRevenue(ctx, from, to)
	if err != nil {
		return finance.Membership{}, err
	}
	return finance.Membership{ByChannel: ch, ByPlan: pl}, nil
}

func (r *FinanceRepo) channelRevenue(ctx context.Context, from, to time.Time) ([]finance.ChannelRevenue, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT channel, currency, count(*)::bigint,
		        COALESCE(sum(amount), 0)::bigint, COALESCE(sum(refund_amount), 0)::bigint
		 FROM pay_order
		 WHERE product_type = 'vip_plan' AND paid_at IS NOT NULL AND paid_at >= $1 AND paid_at < $2
		 GROUP BY channel, currency
		 ORDER BY channel, currency`, from, to)
	if err != nil {
		return nil, fmt.Errorf("汇总会员渠道收入: %w", err)
	}
	defer rows.Close()
	out := []finance.ChannelRevenue{}
	for rows.Next() {
		var c finance.ChannelRevenue
		if err := rows.Scan(&c.Channel, &c.Currency, &c.Orders, &c.Gross, &c.Refund); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *FinanceRepo) planRevenue(ctx context.Context, from, to time.Time) ([]finance.PlanRevenue, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT p.code, o.currency, count(*)::bigint,
		        COALESCE(sum(o.amount), 0)::bigint, COALESCE(sum(o.refund_amount), 0)::bigint
		 FROM pay_order o JOIN vip_plan p ON p.id = o.product_id
		 WHERE o.product_type = 'vip_plan' AND o.paid_at IS NOT NULL AND o.paid_at >= $1 AND o.paid_at < $2
		 GROUP BY p.code, o.currency
		 ORDER BY p.code, o.currency`, from, to)
	if err != nil {
		return nil, fmt.Errorf("汇总会员套餐收入: %w", err)
	}
	defer rows.Close()
	out := []finance.PlanRevenue{}
	for rows.Next() {
		var p finance.PlanRevenue
		if err := rows.Scan(&p.PlanCode, &p.Currency, &p.Orders, &p.Gross, &p.Refund); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
