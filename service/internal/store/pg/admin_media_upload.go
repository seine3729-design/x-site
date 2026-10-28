package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/upload"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// UploadEnqueuer 把广告视频的转码任务与会话状态修改放入同一事务。
type UploadEnqueuer func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error

// AdminMediaUploadRepo 实现后台媒体上传会话仓储。
type AdminMediaUploadRepo struct {
	pool    *pgxpool.Pool
	q       *sqlcgen.Queries
	enqueue UploadEnqueuer
}

func NewAdminMediaUploadRepo(pool *pgxpool.Pool, enqueue UploadEnqueuer) *AdminMediaUploadRepo {
	return &AdminMediaUploadRepo{pool: pool, q: sqlcgen.New(pool), enqueue: enqueue}
}

func adminUploadOf(r sqlcgen.AdminMediaUpload) upload.Asset {
	a := upload.Asset{
		ID: uuid.UUID(r.ID.Bytes), AdminID: r.AdminID,
		ProviderEndpoint: r.ProviderEndpoint, ProviderRegion: r.ProviderRegion, ProviderBucket: r.ProviderBucket,
		ProviderPathStyle: r.ProviderPathStyle, ProviderPathPrefix: r.ProviderPathPrefix,
		Purpose: upload.Purpose(r.Purpose), Kind: upload.Kind(r.Kind), MIME: r.Mime,
		ImageFormat: upload.ImageFormat(r.ImageFormat), SizeBytes: r.SizeBytes, SourceKey: r.SourceKey, OutputKey: r.OutputKey,
		Status: upload.Status(r.Status), Progress: r.Progress, Attempts: r.Attempts, Error: r.Error,
		ExpiresAt: r.ExpiresAt.Time.UTC(), CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
		PartSize: r.PartSize, MultipartUploadID: r.MultipartUploadID,
	}
	if r.ProviderID.Valid {
		a.ProviderID = r.ProviderID.Int64
	}
	if r.EpisodeID.Valid {
		a.EpisodeID = r.EpisodeID.Int64
	}
	if r.TranscodeJobID.Valid {
		id := uuid.UUID(r.TranscodeJobID.Bytes)
		a.TranscodeJobID = &id
	}
	return a
}

func uploadErr(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

func uploadRows(n int64, err error, what string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if n == 0 {
		return errs.ErrBadRequest.WithMessage("上传会话状态已变化，请重新选择文件")
	}
	return nil
}

func workerRows(n int64, err error, what string) error {
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if n == 0 {
		return upload.ErrJobGone
	}
	return nil
}

func (r *AdminMediaUploadRepo) Create(ctx context.Context, a upload.Asset) (upload.Asset, error) {
	row, err := r.q.CreateAdminMediaUpload(ctx, sqlcgen.CreateAdminMediaUploadParams{
		ID: pgUUID(a.ID), AdminID: a.AdminID, ProviderID: nullID(a.ProviderID),
		ProviderEndpoint: a.ProviderEndpoint, ProviderRegion: a.ProviderRegion, ProviderBucket: a.ProviderBucket,
		ProviderPathStyle: a.ProviderPathStyle, ProviderPathPrefix: a.ProviderPathPrefix,
		Purpose: string(a.Purpose), Kind: string(a.Kind), Mime: a.MIME, ImageFormat: string(a.ImageFormat),
		SizeBytes: a.SizeBytes, SourceKey: a.SourceKey, OutputKey: a.OutputKey,
		EpisodeID: nullID(a.EpisodeID), ExpiresAt: pgtype.Timestamptz{Time: a.ExpiresAt, Valid: true},
		PartSize: a.PartSize,
	})
	if err != nil {
		return upload.Asset{}, uploadErr(err, "创建上传会话")
	}
	return adminUploadOf(row), nil
}

func (r *AdminMediaUploadRepo) Get(ctx context.Context, id uuid.UUID) (upload.Asset, error) {
	row, err := r.q.GetAdminMediaUpload(ctx, pgUUID(id))
	if err != nil {
		return upload.Asset{}, uploadErr(err, "读取上传会话")
	}
	return adminUploadOf(row), nil
}

func (r *AdminMediaUploadRepo) ClaimWrite(ctx context.Context, id uuid.UUID, adminID int64) error {
	n, err := r.q.ClaimAdminMediaUploadWrite(ctx, sqlcgen.ClaimAdminMediaUploadWriteParams{ID: pgUUID(id), AdminID: adminID})
	return uploadRows(n, err, "领取上传会话")
}

func (r *AdminMediaUploadRepo) SetMultipart(ctx context.Context, id uuid.UUID, adminID int64, uploadID string) error {
	n, err := r.q.SetAdminMediaUploadMultipart(ctx, sqlcgen.SetAdminMediaUploadMultipartParams{
		ID: pgUUID(id), AdminID: adminID, MultipartUploadID: uploadID,
	})
	return uploadRows(n, err, "登记分片上传")
}

func (r *AdminMediaUploadRepo) TouchPart(ctx context.Context, id uuid.UUID, adminID int64, expiresAt time.Time) error {
	n, err := r.q.TouchAdminMediaUploadPart(ctx, sqlcgen.TouchAdminMediaUploadPartParams{
		ID: pgUUID(id), AdminID: adminID, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	return uploadRows(n, err, "领取分片写入")
}

func (r *AdminMediaUploadRepo) MarkUploaded(ctx context.Context, id uuid.UUID, adminID int64) error {
	n, err := r.q.MarkAdminMediaUploadUploaded(ctx, sqlcgen.MarkAdminMediaUploadUploadedParams{ID: pgUUID(id), AdminID: adminID})
	return uploadRows(n, err, "记录上传完成")
}

func (r *AdminMediaUploadRepo) Fail(ctx context.Context, id uuid.UUID, adminID int64, message string) error {
	n, err := r.q.FailAdminMediaUpload(ctx, sqlcgen.FailAdminMediaUploadParams{ID: pgUUID(id), AdminID: adminID, Error: truncateUploadError(message)})
	return uploadRows(n, err, "记录上传失败")
}

func (r *AdminMediaUploadRepo) CompleteImage(ctx context.Context, id uuid.UUID, adminID int64) error {
	n, err := r.q.CompleteAdminImageUpload(ctx, sqlcgen.CompleteAdminImageUploadParams{ID: pgUUID(id), AdminID: adminID})
	return uploadRows(n, err, "完成图片上传")
}

func (r *AdminMediaUploadRepo) LinkEpisodeTranscode(ctx context.Context, id uuid.UUID, adminID int64, jobID uuid.UUID) error {
	n, err := r.q.LinkAdminEpisodeUpload(ctx, sqlcgen.LinkAdminEpisodeUploadParams{ID: pgUUID(id), AdminID: adminID, TranscodeJobID: pgUUID(jobID)})
	return uploadRows(n, err, "关联分集转码任务")
}

func (r *AdminMediaUploadRepo) QueueAdVideo(ctx context.Context, id uuid.UUID) error {
	if r.enqueue == nil {
		return errors.New("后台视频转码没有配置入队函数")
	}
	return InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		n, err := q.QueueAdminVideoUpload(ctx, pgUUID(id))
		if err := workerRows(n, err, "排队广告视频转码"); err != nil {
			return err
		}
		if err := r.enqueue(ctx, tx, id); err != nil {
			return fmt.Errorf("广告视频转码任务入队: %w", err)
		}
		return nil
	})
}

func (r *AdminMediaUploadRepo) StartAdVideo(ctx context.Context, id uuid.UUID) (upload.Asset, error) {
	row, err := r.q.StartAdminVideoUpload(ctx, pgUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return upload.Asset{}, upload.ErrJobGone
	}
	if err != nil {
		return upload.Asset{}, fmt.Errorf("开始广告视频转码: %w", err)
	}
	return adminUploadOf(row), nil
}

func (r *AdminMediaUploadRepo) UpdateAdVideoProgress(ctx context.Context, id uuid.UUID, progress int) error {
	n, err := r.q.UpdateAdminVideoUploadProgress(ctx, sqlcgen.UpdateAdminVideoUploadProgressParams{ID: pgUUID(id), Progress: int32(min(max(progress, 0), 99))})
	return workerRows(n, err, "更新广告视频转码进度")
}

func (r *AdminMediaUploadRepo) RequeueAdVideo(ctx context.Context, id uuid.UUID, message string) error {
	n, err := r.q.RequeueAdminVideoUpload(ctx, sqlcgen.RequeueAdminVideoUploadParams{ID: pgUUID(id), Error: message})
	return workerRows(n, err, "记录广告视频转码重试")
}

func (r *AdminMediaUploadRepo) CompleteAdVideo(ctx context.Context, id uuid.UUID, outputKey string) error {
	n, err := r.q.CompleteAdminVideoUpload(ctx, sqlcgen.CompleteAdminVideoUploadParams{ID: pgUUID(id), OutputKey: outputKey})
	return workerRows(n, err, "完成广告视频转码")
}

func (r *AdminMediaUploadRepo) FailAdVideo(ctx context.Context, id uuid.UUID, message string) error {
	n, err := r.q.FailAdminVideoUpload(ctx, sqlcgen.FailAdminVideoUploadParams{ID: pgUUID(id), Error: message})
	return workerRows(n, err, "记录广告视频转码失败")
}

func (r *AdminMediaUploadRepo) ListExpired(ctx context.Context, limit int) ([]upload.Asset, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.q.ListExpiredAdminMediaUploads(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("读取过期上传会话: %w", err)
	}
	out := make([]upload.Asset, len(rows))
	for i, row := range rows {
		out[i] = adminUploadOf(row)
	}
	return out, nil
}

func (r *AdminMediaUploadRepo) DeleteExpired(ctx context.Context, id uuid.UUID) error {
	n, err := r.q.DeleteExpiredAdminMediaUpload(ctx, pgUUID(id))
	return workerRows(n, err, "删除过期上传会话")
}

func truncateUploadError(message string) string {
	chars := []rune(message)
	if len(chars) <= 500 {
		return message
	}
	return string(chars[:500]) + "…"
}
