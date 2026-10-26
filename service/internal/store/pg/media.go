package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/media"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// Enqueuer 在入队事务内插入 River 任务（River InsertTx），由 app 装配时提供：任务行与 River 任务同时提交或同时回滚。
type Enqueuer func(ctx context.Context, tx pgx.Tx, jobID uuid.UUID) error

// ObjectEnqueuer 在事务内按媒体对象编号插入 River 任务（S1-6f 的复制任务）。
type ObjectEnqueuer func(ctx context.Context, tx pgx.Tx, objectID int64) error

// MediaRepo 实现 media.Repo。
type MediaRepo struct {
	pool             *pgxpool.Pool
	q                *sqlcgen.Queries
	enqueue          Enqueuer
	enqueueReplicate ObjectEnqueuer
}

func NewMediaRepo(pool *pgxpool.Pool, enqueue Enqueuer) *MediaRepo {
	return &MediaRepo{pool: pool, q: sqlcgen.New(pool), enqueue: enqueue}
}

// WithReplicateEnqueuer 设置产物记录成功后入队媒体复制任务的回调（worker 进程装配时调用，S1-6f）。
// 返回自身便于链式装配。
func (r *MediaRepo) WithReplicateEnqueuer(enqueue ObjectEnqueuer) *MediaRepo {
	r.enqueueReplicate = enqueue
	return r
}

const staleJobMessage = "worker 超过 10 分钟没有更新进度（进程可能已退出），任务已终止，请重新提交"

func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }

// mediaErr 映射查询错误：查不到为 ErrNotFound，同一集已有进行中的任务为 ErrActive，分集已被删除（外键）为 ErrNotFound。
func mediaErr(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505" && pgErr.ConstraintName == "transcode_job_active_key":
			return media.ErrActive
		case pgErr.Code == "23503":
			return errs.ErrNotFound
		}
	}
	var e *errs.Error
	if errors.As(err, &e) || errors.Is(err, media.ErrJobGone) {
		return err
	}
	return fmt.Errorf("%s: %w", what, err)
}

// jobOf 转换任务行（GetTranscodeJob 与 ListTranscodeJobs 的结果列相同）。
func jobOf(r sqlcgen.GetTranscodeJobRow) (media.Job, error) {
	j := media.Job{
		ID: uuid.UUID(r.ID.Bytes), EpisodeID: r.EpisodeID, DramaID: r.DramaID, EpisodeNumber: r.EpisodeNumber,
		SourceKey: r.SourceKey, Force: r.Force, Status: media.JobStatus(r.Status), Progress: r.Progress, Attempts: r.Attempts,
		Error: r.Error, SourceHash: r.SourceHash, KeyVer: r.KeyVer, Poster: r.Poster, DurationSec: r.DurationSec,
		CreatedAt: r.CreatedAt.Time.UTC(), StartedAt: utcPtr(r.StartedAt), FinishedAt: utcPtr(r.FinishedAt), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
	if err := json.Unmarshal(r.Renditions, &j.Renditions); err != nil {
		return media.Job{}, fmt.Errorf("解析任务 %s 的码率阶梯: %w", j.ID, err)
	}
	if j.Renditions == nil {
		j.Renditions = []media.Rendition{}
	}
	return j, nil
}

// ---------- 后台 ----------

func (r *MediaRepo) CreateJob(ctx context.Context, id uuid.UUID, episodeID int64, sourceKey string, force bool) (media.Job, error) {
	if r.enqueue == nil {
		return media.Job{}, errors.New("MediaRepo 没有配置入队函数（worker 进程不提交任务）")
	}
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockEpisodeForTranscode(ctx, episodeID); err != nil {
			return mediaErr(err, "锁定分集")
		}
		if err := q.FailStaleTranscodeJobs(ctx, sqlcgen.FailStaleTranscodeJobsParams{
			Error: staleJobMessage, EpisodeID: episodeID, StaleSeconds: int32(media.StaleAfter.Seconds()),
		}); err != nil {
			return mediaErr(err, "清理超时任务")
		}
		n, err := q.CountActiveTranscodeJobs(ctx, episodeID)
		if err != nil {
			return mediaErr(err, "查询进行中的任务")
		}
		if n > 0 {
			return media.ErrActive
		}
		if sourceKey == "" {
			k, err := q.LatestTranscodeSourceKey(ctx, episodeID)
			if errors.Is(err, pgx.ErrNoRows) {
				return media.ErrNoSource
			}
			if err != nil {
				return mediaErr(err, "读取上一个任务的源文件")
			}
			sourceKey = k
		}
		if err := q.InsertTranscodeJob(ctx, sqlcgen.InsertTranscodeJobParams{
			ID: pgUUID(id), EpisodeID: episodeID, SourceKey: sourceKey, Force: force,
		}); err != nil {
			return mediaErr(err, "写入转封装任务")
		}
		if err := r.enqueue(ctx, tx, id); err != nil {
			return fmt.Errorf("转封装任务入队: %w", err)
		}
		return mediaErr(q.MarkEpisodeTranscoding(ctx, episodeID), "更新分集状态")
	})
	if err != nil {
		return media.Job{}, err
	}
	return r.GetJob(ctx, id)
}

func (r *MediaRepo) GetJob(ctx context.Context, id uuid.UUID) (media.Job, error) {
	row, err := r.q.GetTranscodeJob(ctx, pgUUID(id))
	if err != nil {
		return media.Job{}, mediaErr(err, "读取转封装任务")
	}
	return jobOf(row)
}

func (r *MediaRepo) ListJobs(ctx context.Context, q media.JobQuery) ([]media.Job, int64, error) {
	epID, dramaID, status := nullID(q.EpisodeID), nullID(q.DramaID), nullText(string(q.Status))
	rows, err := r.q.ListTranscodeJobs(ctx, sqlcgen.ListTranscodeJobsParams{
		EpisodeID: epID, DramaID: dramaID, Status: status, PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取转封装任务: %w", err)
	}
	total, err := r.q.CountTranscodeJobs(ctx, sqlcgen.CountTranscodeJobsParams{EpisodeID: epID, DramaID: dramaID, Status: status})
	if err != nil {
		return nil, 0, fmt.Errorf("统计转封装任务: %w", err)
	}
	out := make([]media.Job, len(rows))
	for i, row := range rows {
		if out[i], err = jobOf(sqlcgen.GetTranscodeJobRow(row)); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

// ---------- worker ----------

func (r *MediaRepo) StartAttempt(ctx context.Context, id uuid.UUID) (media.Attempt, error) {
	row, err := r.q.StartTranscodeAttempt(ctx, pgUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return media.Attempt{}, media.ErrJobGone
	}
	if err != nil {
		return media.Attempt{}, fmt.Errorf("开始转封装任务: %w", err)
	}
	j, err := jobOf(sqlcgen.GetTranscodeJobRow{
		ID: row.ID, EpisodeID: row.EpisodeID, SourceKey: row.SourceKey, Force: row.Force, Status: row.Status, Progress: row.Progress,
		Attempts: row.Attempts, Error: row.Error, SourceHash: row.SourceHash, OutputObjectID: row.OutputObjectID, KeyVer: row.KeyVer,
		Renditions: row.Renditions, Poster: row.Poster, DurationSec: row.DurationSec, CreatedAt: row.CreatedAt,
		StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, UpdatedAt: row.UpdatedAt, DramaID: row.DramaID, EpisodeNumber: row.EpisodeNumber,
	})
	if err != nil {
		return media.Attempt{}, err
	}
	return media.Attempt{Job: j, EpisodeMediaStatus: row.EpisodeMediaStatus, EpisodeSourceHash: row.EpisodeSourceHash}, nil
}

func (r *MediaRepo) ReportProgress(ctx context.Context, id uuid.UUID, progress int) error {
	if err := r.q.UpdateTranscodeProgress(ctx, sqlcgen.UpdateTranscodeProgressParams{
		ID: pgUUID(id), Progress: int32(min(max(progress, 0), 99)),
	}); err != nil {
		return fmt.Errorf("更新转封装进度: %w", err)
	}
	return nil
}

func (r *MediaRepo) NextKeyVer(ctx context.Context, episodeID int64) (int32, error) {
	v, err := r.q.NextMediaKeyVer(ctx, episodeID)
	if err != nil {
		return 0, fmt.Errorf("读取内容密钥版本: %w", err)
	}
	return v, nil
}

func (r *MediaRepo) Skip(ctx context.Context, id uuid.UUID, sourceHash string) error {
	n, err := r.q.SkipTranscodeJob(ctx, sqlcgen.SkipTranscodeJobParams{ID: pgUUID(id), SourceHash: sourceHash})
	return rowsOrGone(n, err, "记录跳过")
}

func (r *MediaRepo) Retry(ctx context.Context, id uuid.UUID, sourceHash, msg string) error {
	n, err := r.q.RequeueTranscodeJob(ctx, sqlcgen.RequeueTranscodeJobParams{ID: pgUUID(id), SourceHash: sourceHash, Error: msg})
	return rowsOrGone(n, err, "记录重试")
}

func (r *MediaRepo) Fail(ctx context.Context, id uuid.UUID, sourceHash, msg string) error {
	episodeID, err := r.q.GetTranscodeJobEpisodeID(ctx, pgUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return media.ErrJobGone
	}
	if err != nil {
		return fmt.Errorf("读取转封装任务: %w", err)
	}
	return InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		// 先锁分集再改任务，与入队的加锁顺序一致
		if _, err := q.LockEpisodeMedia(ctx, episodeID); err != nil {
			return gone(err, "锁定分集")
		}
		n, err := q.FailTranscodeJob(ctx, sqlcgen.FailTranscodeJobParams{ID: pgUUID(id), SourceHash: sourceHash, Error: msg})
		if err := rowsOrGone(n, err, "记录失败"); err != nil {
			return err
		}
		if err := q.MarkEpisodeTranscodeFailed(ctx, episodeID); err != nil {
			return fmt.Errorf("更新分集状态: %w", err)
		}
		return nil
	})
}

func (r *MediaRepo) Complete(ctx context.Context, id uuid.UUID, out media.Output) error {
	renditions, err := json.Marshal(out.Renditions)
	if err != nil {
		return err
	}
	return InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		// 加锁顺序：剧 → 分集 → 任务（与分集增删改、入队一致，避免死锁）
		if _, err := q.LockDramaForMedia(ctx, out.DramaID); err != nil {
			return gone(err, "锁定剧集")
		}
		if _, err := q.LockEpisodeMedia(ctx, out.EpisodeID); err != nil {
			return gone(err, "锁定分集")
		}
		job, err := q.LockTranscodeJob(ctx, pgUUID(id))
		if err != nil {
			return gone(err, "锁定转封装任务")
		}
		if job.Status != string(media.JobRunning) || job.EpisodeID != out.EpisodeID {
			return media.ErrJobGone
		}
		objectID, err := q.InsertMediaObject(ctx, sqlcgen.InsertMediaObjectParams{
			Key: out.Prefix, SizeBytes: out.SizeBytes, Sha256: out.MasterSHA256, EpisodeID: nullID(out.EpisodeID),
		})
		if err != nil {
			return fmt.Errorf("写入媒体对象: %w", err)
		}
		if err := q.InsertMediaKey(ctx, sqlcgen.InsertMediaKeyParams{
			EpisodeID: out.EpisodeID, KeyVer: out.KeyVer, WrappedKey: out.WrappedKey,
		}); err != nil {
			return fmt.Errorf("写入内容密钥: %w", err)
		}
		if err := q.SetEpisodeMedia(ctx, sqlcgen.SetEpisodeMediaParams{
			ID: out.EpisodeID, VideoObjectID: nullID(objectID), KeyVer: out.KeyVer, Renditions: renditions,
			SourceHash: out.SourceHash, Poster: out.Poster, DurationSec: out.DurationSec,
		}); err != nil {
			return fmt.Errorf("更新分集产物: %w", err)
		}
		if err := q.CompleteTranscodeJob(ctx, sqlcgen.CompleteTranscodeJobParams{
			ID: pgUUID(id), SourceHash: out.SourceHash, OutputObjectID: nullID(objectID), KeyVer: out.KeyVer,
			Renditions: renditions, Poster: out.Poster, DurationSec: out.DurationSec,
		}); err != nil {
			return fmt.Errorf("记录转封装成功: %w", err)
		}
		if err := q.RefreshLatestEpisode(ctx, out.DramaID); err != nil {
			return fmt.Errorf("刷新最新集号: %w", err)
		}
		// 产物写入了存储源（S1-6f）：同一事务内登记主存储副本并入队复制任务，
		// 不会出现「有产物但没有副本记录」或「登记了副本却没有复制任务」
		if out.ProviderID > 0 {
			if _, err := q.UpsertMediaReplica(ctx, sqlcgen.UpsertMediaReplicaParams{
				ObjectID: objectID, ProviderID: out.ProviderID, Status: string(storage.ReplicaReady),
			}); err != nil {
				return fmt.Errorf("登记主存储副本: %w", err)
			}
			if r.enqueueReplicate != nil {
				if err := r.enqueueReplicate(ctx, tx, objectID); err != nil {
					return fmt.Errorf("媒体复制任务入队: %w", err)
				}
			}
		}
		return nil
	})
}

// EpisodeMedia 是分集当前的产物与内容密钥（密文）。
type EpisodeMedia struct {
	EpisodeID   int64
	MediaStatus string
	KeyVer      int32
	Poster      string
	ObjectKey   string // 媒体桶中的产物目录
	WrappedKey  []byte
}

// CurrentMedia 读取分集当前的产物；还没有产物时返回 errs.ErrNotFound。
func (r *MediaRepo) CurrentMedia(ctx context.Context, episodeID int64) (EpisodeMedia, error) {
	row, err := r.q.GetEpisodeMedia(ctx, episodeID)
	if err != nil {
		return EpisodeMedia{}, mediaErr(err, "读取分集产物")
	}
	return EpisodeMedia{
		EpisodeID: row.ID, MediaStatus: row.MediaStatus, KeyVer: row.KeyVer, Poster: row.Poster,
		ObjectKey: row.ObjectKey, WrappedKey: row.WrappedKey,
	}, nil
}

// gone 把「查不到」映射为 ErrJobGone（分集或任务已被删除）。
func gone(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return media.ErrJobGone
	}
	return fmt.Errorf("%s: %w", what, err)
}

func rowsOrGone(n int64, err error, what string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if n == 0 {
		return media.ErrJobGone
	}
	return nil
}
