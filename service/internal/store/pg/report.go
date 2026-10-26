package pg

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/moderation"
	"xsite/service/internal/store/pg/sqlcgen"
)

// ReportRepo 实现 moderation.ReportRepo（S2-5）。
type ReportRepo struct {
	q *sqlcgen.Queries
}

func NewReportRepo(pool *pgxpool.Pool) *ReportRepo {
	return &ReportRepo{q: sqlcgen.New(pool)}
}

var _ moderation.ReportRepo = (*ReportRepo)(nil)

func (r *ReportRepo) Create(ctx context.Context, in moderation.NewReport) error {
	p := sqlcgen.CreateReportParams{
		TargetType: in.TargetType, TargetID: in.TargetID,
		Reason: in.Reason, Detail: in.Detail,
	}
	if in.Reporter.IsGuest() {
		p.ReporterGuestID = pgUUID(in.Reporter.GuestID)
	} else {
		p.ReporterID = nullID(in.Reporter.UserID)
	}
	return dbErr(r.q.CreateReport(ctx, p), "提交举报")
}

func (r *ReportRepo) List(ctx context.Context, q moderation.ReportQuery) ([]moderation.ReportView, int64, error) {
	rows, err := r.q.ListReports(ctx, sqlcgen.ListReportsParams{
		Status: q.Status, TargetType: q.TargetType, Reason: q.Reason, Pattern: q.Pattern,
		Lim: int32(q.Limit), Off: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, dbErr(err, "读取举报")
	}
	total, err := r.q.CountReports(ctx, sqlcgen.CountReportsParams{
		Status: q.Status, TargetType: q.TargetType, Reason: q.Reason, Pattern: q.Pattern,
	})
	if err != nil {
		return nil, 0, dbErr(err, "统计举报")
	}
	out := make([]moderation.ReportView, len(rows))
	for i, x := range rows {
		out[i] = moderation.ReportView{
			ID: x.ID, TargetType: x.TargetType, TargetID: x.TargetID, Reason: x.Reason,
			Detail: x.Detail, Status: x.Status, HandleNote: x.HandleNote, CreatedAt: x.CreatedAt.Time.UTC(),
			Reporter: authorView(x.ReporterID, x.Nickname, x.Avatar),
		}
	}
	return out, total, nil
}

func (r *ReportRepo) Handle(ctx context.Context, id int64, status, note string) (moderation.ReportView, error) {
	row, err := r.q.HandleReport(ctx, sqlcgen.HandleReportParams{ID: id, Status: status, HandleNote: note})
	if err != nil {
		return moderation.ReportView{}, dbErr(err, "处理举报")
	}
	return moderation.ReportView{
		ID: row.ID, TargetType: row.TargetType, TargetID: row.TargetID, Reason: row.Reason,
		Detail: row.Detail, Status: row.Status, HandleNote: row.HandleNote, CreatedAt: row.CreatedAt.Time.UTC(),
		Reporter: authorView(row.ReporterID, "", ""),
	}, nil
}
