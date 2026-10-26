package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// AuditQueryRepo 实现 platform.AuditQueryRepo 与 platform.AuditChainRepo（审计日志查询与哈希链校验，S4-7）。
type AuditQueryRepo struct {
	q *sqlcgen.Queries
}

func NewAuditQueryRepo(pool *pgxpool.Pool) *AuditQueryRepo {
	return &AuditQueryRepo{q: sqlcgen.New(pool)}
}

func (r *AuditQueryRepo) ListAudit(ctx context.Context, q platform.AuditQuery) ([]platform.AuditItem, int64, error) {
	rows, err := r.q.ListAuditLogs(ctx, sqlcgen.ListAuditLogsParams{
		Pattern: q.Pattern, Action: q.Action, TargetType: q.TargetType, AdminID: q.AdminID,
		FromAt: timestamptz(q.From), ToAt: timestamptz(q.To),
		Lim: int32(q.Limit), Off: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取审计日志: %w", err)
	}
	total, err := r.q.CountAuditLogs(ctx, sqlcgen.CountAuditLogsParams{
		Pattern: q.Pattern, Action: q.Action, TargetType: q.TargetType, AdminID: q.AdminID,
		FromAt: timestamptz(q.From), ToAt: timestamptz(q.To),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("统计审计日志: %w", err)
	}
	out := make([]platform.AuditItem, len(rows))
	for i, x := range rows {
		out[i] = platform.AuditItem{
			ID: x.ID, AdminID: x.AdminID, Username: x.Username, Action: x.Action,
			TargetType: x.TargetType, TargetID: x.TargetID, IPPrefix: x.IpPrefix,
			RequestID: x.RequestID, CreatedAt: x.CreatedAt.Time, Chained: x.Chained, HasDiff: x.HasDiff,
		}
	}
	return out, total, nil
}

func (r *AuditQueryRepo) GetAudit(ctx context.Context, id int64) (platform.AuditDetail, error) {
	row, err := r.q.GetAuditLog(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.AuditDetail{}, errs.ErrNotFound
	}
	if err != nil {
		return platform.AuditDetail{}, fmt.Errorf("读取审计详情: %w", err)
	}
	return platform.AuditDetail{
		AuditItem: platform.AuditItem{
			ID: row.ID, AdminID: row.AdminID, Username: row.Username, Action: row.Action,
			TargetType: row.TargetType, TargetID: row.TargetID, IPPrefix: row.IpPrefix,
			RequestID: row.RequestID, CreatedAt: row.CreatedAt.Time,
			Chained: row.Hash != "", HasDiff: row.DiffBefore != nil || row.DiffAfter != nil,
		},
		Before: row.DiffBefore, After: row.DiffAfter, PrevHash: row.PrevHash, Hash: row.Hash,
	}, nil
}

func (r *AuditQueryRepo) AuditFacets(ctx context.Context) (platform.AuditFacets, error) {
	rows, err := r.q.AuditFacets(ctx)
	if err != nil {
		return platform.AuditFacets{}, fmt.Errorf("读取审计筛选项: %w", err)
	}
	out := platform.AuditFacets{
		Actions: []platform.AuditFacet{}, TargetTypes: []platform.AuditFacet{}, Admins: []platform.AuditAdminFacet{},
	}
	for _, x := range rows {
		f := platform.AuditFacet{Value: x.Value, Count: x.N}
		if x.Kind == "action" {
			out.Actions = append(out.Actions, f)
		} else {
			out.TargetTypes = append(out.TargetTypes, f)
		}
	}
	admins, err := r.q.AuditAdmins(ctx)
	if err != nil {
		return platform.AuditFacets{}, fmt.Errorf("读取审计操作人: %w", err)
	}
	for _, a := range admins {
		out.Admins = append(out.Admins, platform.AuditAdminFacet{ID: a.ID, Username: a.Username, Count: a.N})
	}
	return out, nil
}

// ChainPage 按 id 升序返回带哈希的记录，供逐条重算校验。
func (r *AuditQueryRepo) ChainPage(ctx context.Context, afterID int64, limit int) ([]platform.AuditRecord, error) {
	rows, err := r.q.ListAuditChain(ctx, sqlcgen.ListAuditChainParams{AfterID: afterID, Lim: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("读取审计链: %w", err)
	}
	out := make([]platform.AuditRecord, len(rows))
	for i, x := range rows {
		out[i] = platform.AuditRecord{
			ID: x.ID, PrevHash: x.PrevHash, Hash: x.Hash,
			Entry: platform.AuditEntry{
				AdminID: x.AdminID, Action: x.Action, TargetType: x.TargetType, TargetID: x.TargetID,
				Before: x.DiffBefore, After: x.DiffAfter, IPPrefix: x.IpPrefix, RequestID: x.RequestID,
				CreatedAt: x.CreatedAt.Time,
			},
		}
	}
	return out, nil
}

func (r *AuditQueryRepo) ChainStats(ctx context.Context) (platform.AuditChainStats, error) {
	row, err := r.q.AuditChainStats(ctx)
	if err != nil {
		return platform.AuditChainStats{}, fmt.Errorf("读取审计链概览: %w", err)
	}
	unchained, err := r.q.CountUnchainedAudit(ctx)
	if err != nil {
		return platform.AuditChainStats{}, fmt.Errorf("统计未入链的审计记录: %w", err)
	}
	return platform.AuditChainStats{
		Chained: row.Chained, Unchained: unchained,
		FirstAt: row.FirstAt.Time, LastAt: row.LastAt.Time,
	}, nil
}

// timestamptz 把可空时间转成 pgtype（与 configbackup.go 的同名函数共用一份实现）。
var _ = pgtype.Timestamptz{}
