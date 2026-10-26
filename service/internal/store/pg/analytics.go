package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/analytics"
	"xsite/service/internal/store/pg/sqlcgen"
)

// AnalyticsRepo 实现 analytics.Repo：批量 COPY 写入 event_raw 分区表。
type AnalyticsRepo struct {
	q *sqlcgen.Queries
}

func NewAnalyticsRepo(pool *pgxpool.Pool) *AnalyticsRepo {
	return &AnalyticsRepo{q: sqlcgen.New(pool)}
}

func (r *AnalyticsRepo) InsertEvents(ctx context.Context, events []analytics.Event) (int, error) {
	params := make([]sqlcgen.InsertEventsParams, len(events))
	for i, e := range events {
		params[i] = sqlcgen.InsertEventsParams{
			OccurredAt: pgtype.Timestamptz{Time: e.OccurredAt, Valid: true},
			EventType:  e.Type,
			DeviceID:   e.DeviceID,
			UserID:     eventNullID(e.UserID),
			DramaID:    eventNullID(e.DramaID),
			EpisodeID:  eventNullID(e.EpisodeID),
			Client:     e.Client,
			Region:     e.Region,
			Isp:        e.ISP,
			Props:      e.Props,
		}
	}
	n, err := r.q.InsertEvents(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("写入埋点事件: %w", err)
	}
	return int(n), nil
}

func eventNullID(p *int64) pgtype.Int8 {
	if p == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *p, Valid: true}
}
