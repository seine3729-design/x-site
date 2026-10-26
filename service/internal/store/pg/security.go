package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/security"
	"xsite/service/internal/store/pg/sqlcgen"
)

// SecurityRepo 实现 security.LoginLogRepo（安全中心的登录记录查询）。
type SecurityRepo struct {
	q *sqlcgen.Queries
}

func NewSecurityRepo(pool *pgxpool.Pool) *SecurityRepo {
	return &SecurityRepo{q: sqlcgen.New(pool)}
}

func (r *SecurityRepo) ListLoginLogs(ctx context.Context, f security.LoginLogFilter) ([]security.LoginLog, int64, error) {
	result, pattern := nullText(f.Result), nullText(f.Pattern)
	since, until := tsOf(f.Since), tsOf(f.Until)
	rows, err := r.q.ListAdminLoginLogs(ctx, sqlcgen.ListAdminLoginLogsParams{
		Result: result, Pattern: pattern, Since: since, Until: until,
		PageLimit: int32(f.PageSize), PageOffset: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取登录记录: %w", err)
	}
	total, err := r.q.CountAdminLoginLogs(ctx, sqlcgen.CountAdminLoginLogsParams{
		Result: result, Pattern: pattern, Since: since, Until: until,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("统计登录记录: %w", err)
	}
	out := make([]security.LoginLog, len(rows))
	for i, x := range rows {
		out[i] = security.LoginLog{
			ID: x.ID, AdminID: optInt8Ptr(x.AdminID), Username: x.UsernameTried,
			CurrentUsername: x.AdminUsername.String, Result: x.Result,
			IPPrefix: x.IpPrefix, UADigest: x.UaDigest, CreatedAt: x.CreatedAt.Time.UTC(),
		}
	}
	return out, total, nil
}
