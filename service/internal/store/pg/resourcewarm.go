package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/play"
	"xsite/service/internal/domain/resourcewarm"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// ResourceWarmRepo 是资源预热的仓储（resourcewarm.Repo）。
type ResourceWarmRepo struct {
	q *sqlcgen.Queries
}

var _ resourcewarm.Repo = (*ResourceWarmRepo)(nil)

func NewResourceWarmRepo(pool *pgxpool.Pool) *ResourceWarmRepo {
	return &ResourceWarmRepo{q: sqlcgen.New(pool)}
}

// uniqueViolation 是 Postgres 的唯一约束冲突。
const uniqueViolation = "23505"

func (r *ResourceWarmRepo) Start(ctx context.Context, in resourcewarm.Input, episodesTotal int, adminID int64) (resourcewarm.Job, error) {
	row, err := r.q.StartWarmJob(ctx, sqlcgen.StartWarmJobParams{
		DramaID:            int8OrNull(in.DramaID),
		Mode:               string(in.Mode),
		SegmentsPerQuality: int32(in.SegmentsPerQuality),
		Concurrency:        int32(in.Concurrency),
		IncludeUnpublished: in.IncludeUnpublished,
		EpisodesTotal:      int32(episodesTotal),
		AdminID:            adminID,
	})
	if err != nil {
		// 部分唯一索引 resource_warm_single_running 挡住了第二轮（docs/02 第 4.4 节「同一时间仅一轮」）
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return resourcewarm.Job{}, errs.ErrConflict.WithMessage("已经有一轮预热在跑，先等它跑完或点停止")
		}
		return resourcewarm.Job{}, fmt.Errorf("开始预热: %w", err)
	}
	return resourcewarm.Job{
		ID: row.ID, DramaID: row.DramaID.Int64, Mode: resourcewarm.Mode(row.Mode),
		SegmentsPerQuality: int(row.SegmentsPerQuality), Concurrency: int(row.Concurrency),
		IncludeUnpublished: row.IncludeUnpublished, Status: resourcewarm.Status(row.Status),
		EpisodesTotal: int(row.EpisodesTotal), EpisodesDone: int(row.EpisodesDone),
		Warmed: row.Warmed, Failed: row.Failed, Fetched: row.Fetched, TrafficBytes: row.TrafficBytes,
		Current: row.Current, StopRequested: row.StopRequested, Error: row.Error, AdminID: row.AdminID,
		StartedAt: row.StartedAt.Time, HeartbeatAt: row.HeartbeatAt.Time, FinishedAt: timePtr(row.FinishedAt),
	}, nil
}

func (r *ResourceWarmRepo) Get(ctx context.Context, id int64) (resourcewarm.Job, error) {
	row, err := r.q.GetWarmJob(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return resourcewarm.Job{}, errs.ErrNotFound.WithMessage("预热任务不存在")
		}
		return resourcewarm.Job{}, fmt.Errorf("读预热任务 %d: %w", id, err)
	}
	return warmJobOf(warmRow(row)), nil
}

func (r *ResourceWarmRepo) Running(ctx context.Context) (resourcewarm.Job, error) {
	row, err := r.q.GetRunningWarmJob(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return resourcewarm.Job{}, errs.ErrNotFound
		}
		return resourcewarm.Job{}, fmt.Errorf("读在跑的预热任务: %w", err)
	}
	return warmJobOf(warmRow(row)), nil
}

func (r *ResourceWarmRepo) Latest(ctx context.Context) (resourcewarm.Job, error) {
	row, err := r.q.LatestWarmJob(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return resourcewarm.Job{}, errs.ErrNotFound
		}
		return resourcewarm.Job{}, fmt.Errorf("读最近一轮预热: %w", err)
	}
	return warmJobOf(warmRow(row)), nil
}

func (r *ResourceWarmRepo) History(ctx context.Context, limit int) ([]resourcewarm.Job, error) {
	rows, err := r.q.ListWarmJobs(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("读预热历史: %w", err)
	}
	out := make([]resourcewarm.Job, 0, len(rows))
	for _, row := range rows {
		out = append(out, warmJobOf(warmRow(row)))
	}
	return out, nil
}

func (r *ResourceWarmRepo) UpdateProgress(ctx context.Context, id int64, c resourcewarm.Counters) error {
	if err := r.q.UpdateWarmProgress(ctx, sqlcgen.UpdateWarmProgressParams{
		ID: id, EpisodesDone: int32(c.EpisodesDone), Warmed: c.Warmed, Failed: c.Failed,
		Fetched: c.Fetched, TrafficBytes: c.TrafficBytes, Current: c.Current,
	}); err != nil {
		return fmt.Errorf("写预热进度: %w", err)
	}
	return nil
}

func (r *ResourceWarmRepo) Finish(ctx context.Context, id int64, status resourcewarm.Status, c resourcewarm.Counters, errMsg string) error {
	if err := r.q.FinishWarmJob(ctx, sqlcgen.FinishWarmJobParams{
		ID: id, Status: string(status), EpisodesDone: int32(c.EpisodesDone), Warmed: c.Warmed,
		Failed: c.Failed, Fetched: c.Fetched, TrafficBytes: c.TrafficBytes, Error: errMsg,
	}); err != nil {
		return fmt.Errorf("收尾预热任务 %d: %w", id, err)
	}
	return nil
}

func (r *ResourceWarmRepo) RequestStop(ctx context.Context, id int64) (bool, error) {
	n, err := r.q.RequestWarmStop(ctx, id)
	if err != nil {
		return false, fmt.Errorf("请求停止预热 %d: %w", id, err)
	}
	return n > 0, nil
}

func (r *ResourceWarmRepo) StopRequested(ctx context.Context, id int64) (bool, error) {
	stop, err := r.q.IsWarmStopRequested(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 任务记录没了（被清理）：当作要停，别让 worker 空转
			return true, nil
		}
		return false, fmt.Errorf("读预热停止信号 %d: %w", id, err)
	}
	return stop, nil
}

func (r *ResourceWarmRepo) ReapStale(ctx context.Context, before time.Time) (int64, error) {
	n, err := r.q.ReapStaleWarmJobs(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("清理失去响应的预热任务: %w", err)
	}
	return n, nil
}

func (r *ResourceWarmRepo) Episodes(ctx context.Context, dramaID int64, includeUnpublished bool) ([]resourcewarm.Episode, error) {
	rows, err := r.q.ListWarmEpisodes(ctx, sqlcgen.ListWarmEpisodesParams{
		DramaID: dramaID, IncludeUnpublished: includeUnpublished,
	})
	if err != nil {
		return nil, fmt.Errorf("读待预热的分集: %w", err)
	}
	out := make([]resourcewarm.Episode, 0, len(rows))
	for _, row := range rows {
		out = append(out, resourcewarm.Episode{
			ID: row.ID, DramaID: row.DramaID, Number: row.Number, DramaTitle: row.DramaTitle,
		})
	}
	return out, nil
}

// warmRow 是四个查询共用的行形状（sqlc 为每个查询生成了独立类型，字段完全一致）。
type warmRow struct {
	ID                 int64
	DramaID            pgtype.Int8
	DramaTitle         pgtype.Text
	Mode               string
	SegmentsPerQuality int32
	Concurrency        int32
	IncludeUnpublished bool
	Status             string
	EpisodesTotal      int32
	EpisodesDone       int32
	Warmed             int64
	Failed             int64
	Fetched            int64
	TrafficBytes       int64
	Current            string
	StopRequested      bool
	Error              string
	AdminID            int64
	StartedAt          pgtype.Timestamptz
	HeartbeatAt        pgtype.Timestamptz
	FinishedAt         pgtype.Timestamptz
}

func warmJobOf(row warmRow) resourcewarm.Job {
	return resourcewarm.Job{
		ID: row.ID, DramaID: row.DramaID.Int64, DramaTitle: row.DramaTitle.String,
		Mode: resourcewarm.Mode(row.Mode), SegmentsPerQuality: int(row.SegmentsPerQuality),
		Concurrency: int(row.Concurrency), IncludeUnpublished: row.IncludeUnpublished,
		Status: resourcewarm.Status(row.Status), EpisodesTotal: int(row.EpisodesTotal),
		EpisodesDone: int(row.EpisodesDone), Warmed: row.Warmed, Failed: row.Failed,
		Fetched: row.Fetched, TrafficBytes: row.TrafficBytes, Current: row.Current,
		StopRequested: row.StopRequested, Error: row.Error, AdminID: row.AdminID,
		StartedAt: row.StartedAt.Time, HeartbeatAt: row.HeartbeatAt.Time, FinishedAt: timePtr(row.FinishedAt),
	}
}

// timePtr 见 configbackup.go（包内共用，避免重复声明）。

// WarmPlayRepo 是预热专用的分集读取（play.HLSRepo）。
//
// 和 PlayRepo 的唯一区别：PlayableEpisode **不要求剧已上架**。
// 预热未上架的剧是运营的正常需求（明天要上的剧提前热好），而选源那条路径必须严格按前台可见性来，
// 所以不能直接放宽 PlayRepo —— 那会让没上架的剧能被观众播到。
type WarmPlayRepo struct {
	q *sqlcgen.Queries
}

var _ play.HLSRepo = (*WarmPlayRepo)(nil)

func NewWarmPlayRepo(pool *pgxpool.Pool) *WarmPlayRepo {
	return &WarmPlayRepo{q: sqlcgen.New(pool)}
}

func (r *WarmPlayRepo) PlayableEpisode(ctx context.Context, id int64) (play.Episode, error) {
	row, err := r.q.GetWarmEpisode(ctx, id)
	if err != nil {
		return play.Episode{}, dbErr(err, "读取待预热分集")
	}
	return play.Episode{
		ID: row.ID, DramaID: row.DramaID, Access: content.Access(row.Access), TrialSec: row.TrialSec,
		VideoObjectID: row.VideoObjectID, ObjectKey: row.ObjectKey,
	}, nil
}

func (r *WarmPlayRepo) MediaKey(ctx context.Context, episodeID int64, keyVer int32) ([]byte, error) {
	wrapped, err := r.q.GetMediaKey(ctx, sqlcgen.GetMediaKeyParams{EpisodeID: episodeID, KeyVer: keyVer})
	if err != nil {
		return nil, dbErr(err, "读取内容密钥")
	}
	return wrapped, nil
}
