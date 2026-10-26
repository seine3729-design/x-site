package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"uuid"

	"github.com/riverqueue/river"

	"xsite/service/internal/domain/upload"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/transcode"
)

// AssetTranscodeMaxAttempts 与分集转封装一致：首次加三次自动重试。
const AssetTranscodeMaxAttempts = 4

// AdminVideoUploadArgs 是广告视频转码任务；任务详情保存在 admin_media_upload，避免将文件路径放进 River 参数。
type AdminVideoUploadArgs struct {
	UploadID string `json:"upload_id"`
}

func (AdminVideoUploadArgs) Kind() string { return "admin_video_transcode" }

func (AdminVideoUploadArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMedia, MaxAttempts: AssetTranscodeMaxAttempts}
}

// AdminVideoUploadRepo 是 worker 需要的上传会话状态操作。
type AdminVideoUploadRepo interface {
	StartAdVideo(ctx context.Context, id uuid.UUID) (upload.Asset, error)
	UpdateAdVideoProgress(ctx context.Context, id uuid.UUID, progress int) error
	RequeueAdVideo(ctx context.Context, id uuid.UUID, message string) error
	CompleteAdVideo(ctx context.Context, id uuid.UUID, outputKey string) error
	FailAdVideo(ctx context.Context, id uuid.UUID, message string) error
}

// AdminVideoPackager 复用现有的 MPEG-TS HLS 转封装器，但传入 Public=true 生成不含内容密钥的广告 HLS。
type AdminVideoPackager interface {
	Fetch(ctx context.Context, jobID, sourceKey string) (*transcode.Workspace, error)
	Package(ctx context.Context, ws *transcode.Workspace, req transcode.Request, progress func(int)) (transcode.Result, error)
	DiscardPrefix(ctx context.Context, prefix string) error
	PublicError(err error) string
}

// AdminVideoUploadWorker 处理广告视频：私有源视频 video/source/ → 公开 video/assets/<id>/master.m3u8。
type AdminVideoUploadWorker struct {
	river.WorkerDefaults[AdminVideoUploadArgs]
	Repo       AdminVideoUploadRepo
	Packager   AdminVideoPackager
	Source     blob.UploadStore
	JobTimeout time.Duration
}

func (w *AdminVideoUploadWorker) Timeout(*river.Job[AdminVideoUploadArgs]) time.Duration {
	return w.JobTimeout
}

func (w *AdminVideoUploadWorker) Work(ctx context.Context, job *river.Job[AdminVideoUploadArgs]) error {
	id, err := uuid.Parse(job.Args.UploadID)
	if err != nil {
		return river.JobCancel(fmt.Errorf("后台视频上传任务参数无效: %w", err))
	}
	a, err := w.Repo.StartAdVideo(ctx, id)
	if errors.Is(err, upload.ErrJobGone) {
		return nil
	}
	if err != nil {
		return err
	}

	prefix := transcode.AssetOutputPrefix(id.String())
	progress, stop := w.startProgress(ctx, id)
	defer stop()
	started := time.Now()
	ws, err := w.Packager.Fetch(ctx, id.String(), upload.StageKey(a.ProviderID, a.SourceKey))
	if err == nil {
		defer func() { _ = ws.Close() }()
		res, packageErr := w.Packager.Package(ctx, ws, transcode.Request{JobID: id.String(), Prefix: prefix, Public: true}, progress)
		if packageErr == nil {
			err = w.Repo.CompleteAdVideo(ctx, id, res.Prefix+"/master.m3u8")
		} else {
			err = packageErr
		}
	}
	if err == nil {
		w.deleteSource(context.WithoutCancel(ctx), a, id)
		slog.InfoContext(ctx, "后台视频转码完成", "upload_id", id, "elapsed", time.Since(started).Round(time.Millisecond))
		return nil
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if cleanupErr := w.Packager.DiscardPrefix(cleanupCtx, prefix); cleanupErr != nil {
		slog.WarnContext(ctx, "清理后台视频失败产物", "upload_id", id, "err", cleanupErr)
	}
	if errors.Is(err, upload.ErrJobGone) {
		return nil
	}
	message := w.Packager.PublicError(err)
	if !transcode.IsPermanent(err) && job.Attempt < job.MaxAttempts {
		if rerr := w.Repo.RequeueAdVideo(cleanupCtx, id, message); rerr != nil && !errors.Is(rerr, upload.ErrJobGone) {
			slog.ErrorContext(ctx, "记录后台视频转码重试失败", "upload_id", id, "err", rerr)
		}
		return err
	}
	if ferr := w.Repo.FailAdVideo(cleanupCtx, id, message); ferr != nil && !errors.Is(ferr, upload.ErrJobGone) {
		slog.ErrorContext(ctx, "记录后台视频转码失败", "upload_id", id, "err", ferr)
	}
	w.deleteSource(cleanupCtx, a, id)
	if transcode.IsPermanent(err) {
		return river.JobCancel(err)
	}
	return err
}

func (w *AdminVideoUploadWorker) startProgress(ctx context.Context, id uuid.UUID) (func(int), func()) {
	var current atomic.Int32
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(progressEvery)
		defer ticker.Stop()
		written := int32(-1)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			value := current.Load()
			if value == written {
				continue
			}
			writeCtx, stopWrite := context.WithTimeout(ctx, 5*time.Second)
			err := w.Repo.UpdateAdVideoProgress(writeCtx, id, int(value))
			stopWrite()
			if err != nil && ctx.Err() == nil && !errors.Is(err, upload.ErrJobGone) {
				slog.WarnContext(ctx, "更新后台视频转码进度失败", "upload_id", id, "err", err)
			}
			written = value
		}
	}()
	return func(value int) { current.Store(int32(value)) }, func() { cancel(); <-done }
}

// ConfigureAdminVideoUpload 注册广告视频转码 worker。队列并发由 ConfigureTranscode 统一控制。
func ConfigureAdminVideoUpload(cfg *river.Config, w *AdminVideoUploadWorker) {
	if w != nil {
		river.AddWorker(cfg.Workers, w)
	}
}

func (w *AdminVideoUploadWorker) deleteSource(ctx context.Context, a upload.Asset, id uuid.UUID) {
	if w.Source == nil || a.SourceKey == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, cleanupTimeout)
	defer cancel()
	if err := w.Source.Delete(cleanupCtx, upload.StageKey(a.ProviderID, a.SourceKey)); err != nil && !errors.Is(err, blob.ErrNotFound) {
		slog.WarnContext(ctx, "清理广告视频私有源文件失败，等待过期会话清理重试", "upload_id", id, "err", err)
	}
}
