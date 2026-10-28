package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"uuid"

	"github.com/riverqueue/river"

	"xsite/service/internal/domain/upload"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/uploadstore"
)

const (
	adminUploadCleanupInterval = time.Hour
	adminUploadCleanupLimit    = 200
)

// AdminMediaUploadCleanupArgs 是一轮过期后台上传会话清理。
type AdminMediaUploadCleanupArgs struct{}

func (AdminMediaUploadCleanupArgs) Kind() string { return "admin_media_upload_cleanup" }

// AdminMediaUploadCleanerRepo 是会话过期清理需要的持久化操作。
type AdminMediaUploadCleanerRepo interface {
	ListExpired(ctx context.Context, limit int) ([]upload.Asset, error)
	DeleteExpired(ctx context.Context, id uuid.UUID) error
}

// UploadStageStore 是清理任务对私有入库桶需要的操作：删暂存对象、放弃没有完成的分片上传。
type UploadStageStore interface {
	Delete(ctx context.Context, key string) error
	AbortMultipart(ctx context.Context, key, uploadID string) error
}

// AdminMediaUploadCleanupWorker 删除过期、未完成会话留下的 provider 对象与私有视频暂存，并移除控制面记录。
// 已就绪图片和分集视频只删会话记录：前者仍被业务字段引用，后者的源可供既有转封装重试；
// 已就绪广告视频的私有源不再需要，会一并删除。停传过期的分片上传会被放弃（R2 / S3 对残片计费）。
type AdminMediaUploadCleanupWorker struct {
	river.WorkerDefaults[AdminMediaUploadCleanupArgs]
	Repo         AdminMediaUploadCleanerRepo
	Stores       uploadstore.Resolver
	LegacyImages blob.UploadStore // provider_id 为 0 的历史会话兼容
	LegacySource blob.UploadStore
	Stage        UploadStageStore
}

func (w *AdminMediaUploadCleanupWorker) Timeout(*river.Job[AdminMediaUploadCleanupArgs]) time.Duration {
	return cleanupJobTimeout
}

func (w *AdminMediaUploadCleanupWorker) Work(ctx context.Context, _ *river.Job[AdminMediaUploadCleanupArgs]) error {
	assets, err := w.Repo.ListExpired(ctx, adminUploadCleanupLimit)
	if err != nil {
		return err
	}
	var objects, stages, aborted, sessions int
	var cleanupErrs []error
	for _, a := range assets {
		if multipartUnfinished(a) && w.Stage != nil {
			// 已合并或已放弃的分片上传返回 ErrNotFound，按「已清理」处理
			if err := w.Stage.AbortMultipart(ctx, upload.StageKey(a.ProviderID, a.SourceKey), a.MultipartUploadID); err != nil && !errors.Is(err, blob.ErrNotFound) {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("放弃上传会话 %s 的分片上传: %w", a.ID, err))
				continue
			}
			aborted++
		}
		if w.shouldDeleteObject(a) {
			store, err := w.providerStore(ctx, a)
			if err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("打开上传会话 %s 的存储源: %w", a.ID, err))
				continue
			}
			if err := store.Delete(ctx, a.SourceKey); err != nil && !errors.Is(err, blob.ErrNotFound) {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("清理上传会话 %s 的对象: %w", a.ID, err))
				continue
			}
			objects++
		}
		if a.Kind == upload.KindVideo && a.Purpose != upload.PurposeEpisodeVideo && w.Stage != nil {
			if err := w.Stage.Delete(ctx, upload.StageKey(a.ProviderID, a.SourceKey)); err != nil && !errors.Is(err, blob.ErrNotFound) {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("清理上传会话 %s 的视频暂存: %w", a.ID, err))
				continue
			}
			stages++
		}
		if err := w.Repo.DeleteExpired(ctx, a.ID); err != nil && !errors.Is(err, upload.ErrJobGone) {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("删除过期上传会话 %s: %w", a.ID, err))
			continue
		}
		sessions++
	}
	slog.InfoContext(ctx, "过期后台上传会话清理完成", "sessions", sessions, "objects", objects, "stages", stages,
		"multipart_aborted", aborted, "failed", len(cleanupErrs))
	return errors.Join(cleanupErrs...)
}

// multipartUnfinished 表示会话开启过分片上传、却没有走到「已上传」：停传过期或中途失败。
// 已上传及之后的状态里，分片早已合并成对象，不能再放弃。
func multipartUnfinished(a upload.Asset) bool {
	if a.MultipartUploadID == "" {
		return false
	}
	switch a.Status {
	case upload.StatusCreated, upload.StatusUploading, upload.StatusFailed:
		return true
	default:
		return false
	}
}

func (w *AdminMediaUploadCleanupWorker) providerStore(ctx context.Context, a upload.Asset) (blob.UploadStore, error) {
	if a.ProviderID > 0 {
		if w.Stores == nil {
			return nil, fmt.Errorf("上传存储源解析器未配置")
		}
		target, err := w.Stores.Cleanup(ctx, a)
		if err != nil {
			return nil, err
		}
		return target.Store, nil
	}
	if a.Kind == upload.KindImage {
		return w.LegacyImages, nil
	}
	return w.LegacySource, nil
}

func (w *AdminMediaUploadCleanupWorker) shouldDeleteObject(a upload.Asset) bool {
	// 分集源由 transcode_job 复用；即使上传会话过期也不能在转码或人工重试前删除。
	if a.Purpose == upload.PurposeEpisodeVideo {
		return false
	}
	if a.Status != upload.StatusReady {
		return true
	}
	return a.Purpose == upload.PurposeAdVideo
}

// ConfigureAdminMediaUploadCleanup 注册到 default 队列；即使媒体转码 worker 未启用，也能回收失败的图片上传。
func ConfigureAdminMediaUploadCleanup(cfg *river.Config, w *AdminMediaUploadCleanupWorker) {
	if w == nil {
		return
	}
	river.AddWorker(cfg.Workers, w)
	cfg.PeriodicJobs = append(cfg.PeriodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(adminUploadCleanupInterval),
		func() (river.JobArgs, *river.InsertOpts) {
			return AdminMediaUploadCleanupArgs{}, &river.InsertOpts{Queue: QueueDefault}
		},
		&river.PeriodicJobOpts{RunOnStart: true},
	))
}
