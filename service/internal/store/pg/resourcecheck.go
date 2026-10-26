package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/resourcecheck"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// ResourceCheckRepo 是资源体检的仓储（resourcecheck.Repo）。
type ResourceCheckRepo struct {
	q *sqlcgen.Queries
}

var _ resourcecheck.Repo = (*ResourceCheckRepo)(nil)

func NewResourceCheckRepo(pool *pgxpool.Pool) *ResourceCheckRepo {
	return &ResourceCheckRepo{q: sqlcgen.New(pool)}
}

func (r *ResourceCheckRepo) ListChecks(ctx context.Context, f resourcecheck.Filter) ([]resourcecheck.Row, error) {
	rows, err := r.q.ListResourceChecks(ctx, sqlcgen.ListResourceChecksParams{
		DramaID: f.DramaID, BadOnly: f.BadOnly,
		Pattern: f.Pattern, Word: dramaIDWord(f.Q), Lim: int32(f.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("查体检结果: %w", err)
	}
	out := make([]resourcecheck.Row, 0, len(rows))
	for _, row := range rows {
		out = append(out, resourcecheck.Row{
			ID: row.ID, DramaID: row.DramaID, DramaTitle: row.DramaTitle,
			EpisodeID: row.EpisodeID.Int64, EpisodeNumber: row.EpisodeNumber.Int32,
			FileType: resourcecheck.FileType(row.FileType), Field: resourcecheck.Field(row.Field),
			URL: row.Url, LineID: row.LineID.Int64, LineName: row.LineName.String,
			Result: resourcecheck.Result(row.Result), StatusCode: row.StatusCode,
			ElapsedMs: row.ElapsedMs, Message: row.Message, CheckedAt: row.CheckedAt.Time,
		})
	}
	return out, nil
}

func (r *ResourceCheckRepo) CountChecks(ctx context.Context, f resourcecheck.Filter) (int64, error) {
	n, err := r.q.CountResourceChecks(ctx, sqlcgen.CountResourceChecksParams{
		DramaID: f.DramaID, BadOnly: f.BadOnly,
		Pattern: f.Pattern, Word: dramaIDWord(f.Q),
	})
	if err != nil {
		return 0, fmt.Errorf("统计体检结果: %w", err)
	}
	return n, nil
}

func (r *ResourceCheckRepo) Summary(ctx context.Context) (resourcecheck.Summary, error) {
	row, err := r.q.ResourceCheckSummary(ctx)
	if err != nil {
		return resourcecheck.Summary{}, fmt.Errorf("体检概览: %w", err)
	}
	out := resourcecheck.Summary{Total: row.Total, Bad: row.Bad}
	if row.LastCheckedAt.Valid {
		t := row.LastCheckedAt.Time
		out.LastCheckedAt = &t
	}
	return out, nil
}

func (r *ResourceCheckRepo) UpsertCheck(ctx context.Context, o resourcecheck.Outcome) error {
	if err := r.q.UpsertResourceCheck(ctx, sqlcgen.UpsertResourceCheckParams{
		DramaID:   o.DramaID,
		EpisodeID: int8OrNull(o.EpisodeID),
		FileType:  string(o.FileType), Field: string(o.Field), Url: o.URL,
		LineID: int8OrNull(o.LineID), Result: string(o.Result),
		StatusCode: o.StatusCode, ElapsedMs: o.ElapsedMs, Message: o.Message,
	}); err != nil {
		return fmt.Errorf("写体检结果: %w", err)
	}
	return nil
}

func (r *ResourceCheckRepo) ClearChecks(ctx context.Context, dramaID int64) (int64, error) {
	n, err := r.q.ClearResourceChecks(ctx, dramaID)
	if err != nil {
		return 0, fmt.Errorf("清空体检结果: %w", err)
	}
	return n, nil
}

func (r *ResourceCheckRepo) DramaAssets(ctx context.Context, dramaID int64) (resourcecheck.DramaAssets, error) {
	d, err := r.q.GetDramaForCheck(ctx, dramaID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return resourcecheck.DramaAssets{}, errs.ErrNotFound.WithMessage("剧集不存在")
		}
		return resourcecheck.DramaAssets{}, fmt.Errorf("读剧集 %d: %w", dramaID, err)
	}
	eps, err := r.q.ListEpisodeAssetsForCheck(ctx, dramaID)
	if err != nil {
		return resourcecheck.DramaAssets{}, fmt.Errorf("读剧集 %d 的分集: %w", dramaID, err)
	}
	out := resourcecheck.DramaAssets{
		DramaID: d.ID, Title: d.Title, Status: d.Status,
		CoverPortrait: d.CoverPortrait, CoverLandscape: d.CoverLandscape,
		Episodes: make([]resourcecheck.EpisodeAssets, 0, len(eps)),
	}
	for _, e := range eps {
		out.Episodes = append(out.Episodes, resourcecheck.EpisodeAssets{
			ID: e.ID, Number: e.Number, Cover: e.Cover, Poster: e.Poster,
			MediaStatus: e.MediaStatus, ObjectKey: e.ObjectKey, ObjectKind: e.ObjectKind,
		})
	}
	return out, nil
}

func (r *ResourceCheckRepo) DramaIDsForCheck(ctx context.Context, includeUnpublished bool) ([]int64, error) {
	ids, err := r.q.ListDramaIDsForCheck(ctx, includeUnpublished)
	if err != nil {
		return nil, fmt.Errorf("读待体检的剧集: %w", err)
	}
	return ids, nil
}

// int8OrNull 把 0 当作 NULL。分集编号与线路编号都是正数，
// 0 表示「剧级资源，没有分集」或「填的是完整网址，没经线路」。
func int8OrNull(v int64) pgtype.Int8 {
	if v == 0 {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: v, Valid: true}
}

// dramaIDWord 把搜索词当作剧编号来用（搜「12」既匹配剧名含 12 的，也匹配 12 号剧）。
// 不是纯数字时返回 0，SQL 里 drama_id 不可能是 0，自然不命中。
func dramaIDWord(q string) int64 {
	var n int64
	for _, c := range q {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
		if n > 1<<40 {
			return 0
		}
	}
	return n
}
