package pg

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/play"
	"xsite/service/internal/store/pg/sqlcgen"
)

// PlayRepo 实现 play.Repo（S1-7 选源）与 play.HLSRepo（S1-8 清单与密钥）。
type PlayRepo struct {
	q *sqlcgen.Queries
}

var _ play.HLSRepo = (*PlayRepo)(nil)

func NewPlayRepo(pool *pgxpool.Pool) *PlayRepo {
	return &PlayRepo{q: sqlcgen.New(pool)}
}

// PlayableEpisode 返回可播放的分集；剧不可见或分集未转封装时返回 errs.ErrNotFound。
func (r *PlayRepo) PlayableEpisode(ctx context.Context, id int64) (play.Episode, error) {
	row, err := r.q.GetPlayableEpisode(ctx, id)
	if err != nil {
		return play.Episode{}, dbErr(err, "读取可播放分集")
	}
	return play.Episode{
		ID: row.ID, DramaID: row.DramaID, Access: content.Access(row.Access), TrialSec: row.TrialSec,
		DurationSec: row.DurationSec, VideoObjectID: row.VideoObjectID, ObjectKey: row.ObjectKey,
	}, nil
}

// MediaKey 返回一集某个版本的内容密钥密文；没有时返回 errs.ErrNotFound。
func (r *PlayRepo) MediaKey(ctx context.Context, episodeID int64, keyVer int32) ([]byte, error) {
	wrapped, err := r.q.GetMediaKey(ctx, sqlcgen.GetMediaKeyParams{EpisodeID: episodeID, KeyVer: keyVer})
	if err != nil {
		return nil, dbErr(err, "读取内容密钥")
	}
	return wrapped, nil
}
