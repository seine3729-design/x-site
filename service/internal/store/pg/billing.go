package pg

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/billing"
	"xsite/service/internal/store/pg/sqlcgen"
)

// BillingRepo 实现 billing.Repo 与 billing.PlanAdminRepo（S3-10 / S3-15）。
type BillingRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewBillingRepo(pool *pgxpool.Pool) *BillingRepo {
	return &BillingRepo{pool: pool, q: sqlcgen.New(pool)}
}

var (
	_ billing.Repo          = (*BillingRepo)(nil)
	_ billing.PlanAdminRepo = (*BillingRepo)(nil)
)

func (r *BillingRepo) ListAvailableVIPPlans(ctx context.Context, client string) ([]billing.Plan, error) {
	rows, err := r.q.ListAvailableVIPPlans(ctx, client)
	if err != nil {
		return nil, dbErr(err, "读取会员套餐")
	}
	out := make([]billing.Plan, len(rows))
	for i, row := range rows {
		out[i] = billing.Plan{
			ID:             row.ID,
			Code:           row.Code,
			DurationDays:   int(row.DurationDays),
			PriceFen:       row.PriceFen,
			ListPriceFen:   row.ListPriceFen,
			PriceMicroUSDT: row.PriceMicroUsdt,
			BadgeKey:       row.BadgeKey.String,
			Recommended:    row.Recommended,
			Channels:       row.Channels,
		}
	}
	return out, nil
}

func (r *BillingRepo) VIPUntil(ctx context.Context, userID int64) (time.Time, error) {
	until, err := r.q.GetVIPUntil(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, dbErr(err, "读取会员状态")
	}
	if !until.Valid {
		return time.Time{}, nil
	}
	return until.Time.UTC(), nil
}

func (r *BillingRepo) ListAdminVIPPlans(ctx context.Context) ([]billing.AdminVIPPlan, error) {
	rows, err := r.q.ListAdminVIPPlans(ctx)
	if err != nil {
		return nil, dbErr(err, "读取后台会员套餐")
	}
	out := make([]billing.AdminVIPPlan, len(rows))
	for i, row := range rows {
		out[i] = adminVIPPlanOf(row)
	}
	return out, nil
}

func (r *BillingRepo) GetAdminVIPPlan(ctx context.Context, id int64) (billing.AdminVIPPlan, error) {
	row, err := r.q.GetAdminVIPPlan(ctx, id)
	if err != nil {
		return billing.AdminVIPPlan{}, dbErr(err, "读取后台会员套餐")
	}
	return adminVIPPlanOf(row), nil
}

func (r *BillingRepo) UpdateAdminVIPPlan(ctx context.Context, id int64, in billing.AdminVIPPlanInput) (billing.AdminVIPPlan, error) {
	row, err := r.q.UpdateAdminVIPPlan(ctx, sqlcgen.UpdateAdminVIPPlanParams{
		ID: id, DurationDays: int32(in.DurationDays), PriceFen: in.PriceFen, ListPriceFen: in.ListPriceFen,
		PriceMicroUsdt: in.PriceMicroUSDT, BadgeKey: nullText(in.BadgeKey), Recommended: in.Recommended,
		Channels: in.Channels, Clients: in.Clients, Sort: int32(in.Sort), Status: in.Status,
	})
	if err != nil {
		return billing.AdminVIPPlan{}, dbErr(err, "保存后台会员套餐")
	}
	return adminVIPPlanOf(row), nil
}

func adminVIPPlanOf(row sqlcgen.VipPlan) billing.AdminVIPPlan {
	return billing.AdminVIPPlan{
		ID: row.ID, Code: row.Code, DurationDays: int(row.DurationDays), PriceFen: row.PriceFen,
		ListPriceFen: row.ListPriceFen, PriceMicroUSDT: row.PriceMicroUsdt, BadgeKey: row.BadgeKey.String,
		Recommended: row.Recommended, Channels: row.Channels, Clients: row.Clients, Sort: int(row.Sort),
		Status: row.Status, UpdatedAt: row.UpdatedAt.Time.UTC(),
	}
}
