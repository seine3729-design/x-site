package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// TrashRepo 实现 platform.TrashRepo（回收站，docs/06 第 35 节）。
type TrashRepo struct {
	q *sqlcgen.Queries
}

func NewTrashRepo(pool *pgxpool.Pool) *TrashRepo {
	return &TrashRepo{q: sqlcgen.New(pool)}
}

func (r *TrashRepo) ListTrashedDramas(ctx context.Context, q platform.TrashQuery) ([]platform.TrashItem, int64, error) {
	rows, err := r.q.ListTrashedDramas(ctx, sqlcgen.ListTrashedDramasParams{
		Pattern: q.Pattern, Lim: int32(q.Limit), Off: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取回收站: %w", err)
	}
	total, err := r.q.CountTrashedDramas(ctx, q.Pattern)
	if err != nil {
		return nil, 0, fmt.Errorf("统计回收站: %w", err)
	}
	out := make([]platform.TrashItem, len(rows))
	for i, x := range rows {
		out[i] = platform.TrashItem{
			ID: x.ID, Title: x.Title, Slug: x.Slug.String, Cover: x.CoverPortrait, Status: x.Status,
			Extra: int(x.Episodes), DeletedAt: x.DeletedAt.Time, UpdatedAt: x.UpdatedAt.Time,
		}
	}
	return out, total, nil
}

func (r *TrashRepo) RestoreDrama(ctx context.Context, id int64) (platform.TrashItem, error) {
	row, err := r.q.RestoreDrama(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		// 不在回收站里：可能已经还原过，也可能已被彻底删除
		return platform.TrashItem{}, errs.ErrNotFound
	}
	if err != nil {
		return platform.TrashItem{}, fmt.Errorf("还原剧集: %w", err)
	}
	return platform.TrashItem{ID: row.ID, Title: row.Title, Slug: row.Slug.String, Status: row.Status}, nil
}

func (r *TrashRepo) PurgeDrama(ctx context.Context, id int64) (platform.TrashItem, error) {
	row, err := r.q.PurgeDrama(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.TrashItem{}, errs.ErrNotFound
	}
	if err != nil {
		return platform.TrashItem{}, fmt.Errorf("彻底删除剧集: %w", err)
	}
	return platform.TrashItem{ID: row.ID, Title: row.Title, Slug: row.Slug.String}, nil
}

func (r *TrashRepo) TrashedDrama(ctx context.Context, id int64) (platform.TrashItem, error) {
	row, err := r.q.GetTrashedDrama(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.TrashItem{}, errs.ErrNotFound
	}
	if err != nil {
		return platform.TrashItem{}, fmt.Errorf("读取回收站中的剧集: %w", err)
	}
	return platform.TrashItem{
		ID: row.ID, Title: row.Title, Slug: row.Slug.String,
		Extra: int(row.Episodes), DeletedAt: row.DeletedAt.Time,
	}, nil
}

func (r *TrashRepo) TrashCounts(ctx context.Context) (int, *time.Time, error) {
	row, err := r.q.TrashCounts(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("统计回收站: %w", err)
	}
	return int(row.Dramas), timePtr(row.OldestDeletedAt), nil
}

func (r *TrashRepo) PurgeExpiredDramas(ctx context.Context, before time.Time) ([]platform.TrashItem, error) {
	rows, err := r.q.PurgeExpiredDramas(ctx, timestamptz(&before))
	if err != nil {
		return nil, fmt.Errorf("清理过期的回收站内容: %w", err)
	}
	out := make([]platform.TrashItem, len(rows))
	for i, x := range rows {
		out[i] = platform.TrashItem{ID: x.ID, Title: x.Title}
	}
	return out, nil
}
