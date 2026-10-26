package pg

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/me"
	"xsite/service/internal/store/pg/sqlcgen"
)

// MeRepo 实现 me.Repo（S2-3：收藏、点赞、观看历史、通知）。
type MeRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewMeRepo(pool *pgxpool.Pool) *MeRepo {
	return &MeRepo{pool: pool, q: sqlcgen.New(pool)}
}

var _ me.Repo = (*MeRepo)(nil)

func (r *MeRepo) AddFollow(ctx context.Context, userID, dramaID int64) error {
	return dbErr(r.q.AddFollow(ctx, sqlcgen.AddFollowParams{UserID: userID, DramaID: dramaID}), "收藏")
}

func (r *MeRepo) RemoveFollow(ctx context.Context, userID, dramaID int64) error {
	_, err := r.q.RemoveFollow(ctx, sqlcgen.RemoveFollowParams{UserID: userID, DramaID: dramaID})
	return dbErr(err, "取消收藏")
}

func (r *MeRepo) AddLike(ctx context.Context, userID int64, targetType string, targetID int64) error {
	return dbErr(r.q.AddLike(ctx, sqlcgen.AddLikeParams{UserID: userID, TargetType: targetType, TargetID: targetID}), "点赞")
}

func (r *MeRepo) RemoveLike(ctx context.Context, userID int64, targetType string, targetID int64) error {
	_, err := r.q.RemoveLike(ctx, sqlcgen.RemoveLikeParams{UserID: userID, TargetType: targetType, TargetID: targetID})
	return dbErr(err, "取消点赞")
}

// ---------- 观看历史 ----------

func (r *MeRepo) HistoryPaused(ctx context.Context, userID int64) (bool, error) {
	paused, err := r.q.HistoryPaused(ctx, userID)
	if err != nil {
		return false, dbErr(err, "读取历史记录开关")
	}
	return paused, nil
}

func (r *MeRepo) UpsertHistory(ctx context.Context, rows []me.HistoryRow) error {
	if len(rows) == 0 {
		return nil
	}
	p := sqlcgen.UpsertHistoryParams{
		UserIds:        make([]int64, len(rows)),
		DramaIds:       make([]int64, len(rows)),
		EpisodeIds:     make([]int64, len(rows)),
		EpisodeNumbers: make([]int32, len(rows)),
		PositionSecs:   make([]int32, len(rows)),
		DurationSecs:   make([]int32, len(rows)),
		Tss:            make([]int64, len(rows)),
	}
	for i, row := range rows {
		p.UserIds[i] = row.UserID
		p.DramaIds[i] = row.DramaID
		p.EpisodeIds[i] = row.EpisodeID
		p.EpisodeNumbers[i] = int32(row.EpisodeNumber)
		p.PositionSecs[i] = int32(row.PositionSec)
		p.DurationSecs[i] = int32(row.DurationSec)
		p.Tss[i] = row.UpdatedAt.Unix()
	}
	return dbErr(r.q.UpsertHistory(ctx, p), "落库观看历史")
}

func (r *MeRepo) ListFollows(ctx context.Context, userID int64, limit int) ([]me.FollowView, error) {
	rows, err := r.q.ListFollows(ctx, sqlcgen.ListFollowsParams{UserID: userID, Lim: int32(limit)})
	if err != nil {
		return nil, dbErr(err, "读取追剧列表")
	}
	out := make([]me.FollowView, len(rows))
	for i, row := range rows {
		var year *int32
		if row.Year.Valid {
			y := row.Year.Int32
			year = &y
		}
		out[i] = me.FollowView{
			ID:             row.DramaID,
			Title:          row.Title,
			Slug:           row.Slug.String,
			CoverPortrait:  row.CoverPortrait,
			CoverLandscape: row.CoverLandscape,
			Badge:          row.Badge,
			Score:          scoreOf(row.Score),
			Year:           year,
			IsFinished:     row.IsFinished,
			LatestEpisode:  row.LatestEpisode,
			PlayCount:      row.PlayCount,
			PublishAt:      timePtr(row.PublishAt),
			WatchedEpisode: row.WatchedEpisode,
			FollowedAt:     row.CreatedAt.Time.UTC(),
		}
	}
	return out, nil
}

func (r *MeRepo) ListHistory(ctx context.Context, userID int64, limit int) ([]me.HistoryView, error) {
	rows, err := r.q.ListHistory(ctx, sqlcgen.ListHistoryParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, dbErr(err, "读取观看历史")
	}
	out := make([]me.HistoryView, len(rows))
	for i, row := range rows {
		out[i] = me.HistoryView{
			DramaID:       row.DramaID,
			Title:         row.Title,
			Cover:         row.CoverPortrait,
			EpisodeID:     row.EpisodeID,
			EpisodeNumber: int(row.EpisodeNumber),
			PositionSec:   int(row.PositionSec),
			DurationSec:   int(row.DurationSec),
			LatestEpisode: int(row.LatestEpisode),
			UpdatedAt:     row.UpdatedAt.Time.UTC(),
		}
	}
	return out, nil
}

func (r *MeRepo) DeleteHistory(ctx context.Context, userID int64, dramaIDs []int64) error {
	if len(dramaIDs) == 0 {
		return nil
	}
	return dbErr(r.q.DeleteHistory(ctx, sqlcgen.DeleteHistoryParams{UserID: userID, Column2: dramaIDs}), "删除观看历史")
}

func (r *MeRepo) DeleteAllHistory(ctx context.Context, userID int64) error {
	return dbErr(r.q.DeleteAllHistory(ctx, userID), "清空观看历史")
}

// ---------- 站内信 ----------

func (r *MeRepo) ListNotifications(ctx context.Context, userID, before int64, limit int) ([]me.Notification, error) {
	rows, err := r.q.ListNotifications(ctx, sqlcgen.ListNotificationsParams{UserID: userID, Before: before, Lim: int32(limit)})
	if err != nil {
		return nil, dbErr(err, "读取消息")
	}
	out := make([]me.Notification, len(rows))
	for i, row := range rows {
		out[i] = me.Notification{
			ID: row.ID, Type: row.Type, Payload: row.Payload,
			IsRead: row.IsRead, CreatedAt: row.CreatedAt.Time.UTC().Unix(),
		}
	}
	return out, nil
}

func (r *MeRepo) MarkAllNotificationsRead(ctx context.Context, userID int64) (int, error) {
	n, err := r.q.MarkAllNotificationsRead(ctx, userID)
	if err != nil {
		return 0, dbErr(err, "标记消息已读")
	}
	return int(n), nil
}

func (r *MeRepo) CountUnreadNotifications(ctx context.Context, userID int64) (int, error) {
	n, err := r.q.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return 0, dbErr(err, "统计未读消息")
	}
	return int(n), nil
}

// InsertNotification 写一条站内信（S2-5：评论被回复 / 被点赞，由 worker 异步调用）。
func (r *MeRepo) InsertNotification(ctx context.Context, userID int64, typ string, payload []byte) error {
	return dbErr(r.q.InsertNotification(ctx, sqlcgen.InsertNotificationParams{UserID: userID, Type: typ, Payload: payload}), "写入站内信")
}
