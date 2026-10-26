package pg

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/moderation"
	"xsite/service/internal/store/pg/sqlcgen"
)

// DanmakuRepo 实现 moderation.DanmakuRepo（S2-5）。
type DanmakuRepo struct {
	q *sqlcgen.Queries
}

func NewDanmakuRepo(pool *pgxpool.Pool) *DanmakuRepo {
	return &DanmakuRepo{q: sqlcgen.New(pool)}
}

var _ moderation.DanmakuRepo = (*DanmakuRepo)(nil)

func (r *DanmakuRepo) Create(ctx context.Context, in moderation.NewDanmaku) (moderation.Danmaku, error) {
	p := sqlcgen.CreateDanmakuParams{
		EpisodeID: in.EpisodeID, TimeMs: int32(in.TimeMS),
		Content: in.Content, Color: in.Color, Status: in.Status,
	}
	if in.Author.IsGuest() {
		p.GuestDeviceID = pgUUID(in.Author.GuestID)
	} else {
		p.UserID = nullID(in.Author.UserID)
	}
	row, err := r.q.CreateDanmaku(ctx, p)
	if err != nil {
		return moderation.Danmaku{}, dbErr(err, "发送弹幕")
	}
	return moderation.Danmaku{
		ID: row.ID, EpisodeID: row.EpisodeID, TimeMS: int(row.TimeMs),
		Content: row.Content, Color: row.Color, Status: row.Status,
		CreatedAt: row.CreatedAt.Time.UTC(),
	}, nil
}

func (r *DanmakuRepo) ListWindow(ctx context.Context, episodeID int64, fromMS, toMS, limit int) ([]moderation.DanmakuView, error) {
	rows, err := r.q.ListDanmakuWindow(ctx, sqlcgen.ListDanmakuWindowParams{
		EpisodeID: episodeID, FromMs: int32(fromMS), ToMs: int32(toMS), Lim: int32(limit),
	})
	if err != nil {
		return nil, dbErr(err, "读取弹幕")
	}
	out := make([]moderation.DanmakuView, len(rows))
	for i, x := range rows {
		out[i] = moderation.DanmakuView{
			ID: x.ID, TimeMS: int(x.TimeMs), Content: x.Content, Color: x.Color,
		}
	}
	return out, nil
}
