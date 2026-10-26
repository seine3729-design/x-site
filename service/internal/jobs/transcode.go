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

	"xsite/service/internal/crypto/mediakey"
	"xsite/service/internal/domain/media"
	"xsite/service/internal/transcode"
)

// 媒体转封装（S1-5，docs/specs/S1-5.md）：API 在入队事务内插入任务，带 FFmpeg 的 worker 在 media 队列执行。

// TranscodeMaxAttempts 是最多尝试次数：首次 + 自动重试 3 次（docs/03 第 9.6 节），用尽后记 ERROR 告警。
const TranscodeMaxAttempts = 4

const (
	progressEvery  = 5 * time.Second // 进度写库的最小间隔
	heartbeatEvery = time.Minute     // 进度没变时也至少这么久写一次（media.StaleAfter 判断进程是否还在）
	cleanupTimeout = time.Minute
)

// TranscodeArgs 是转封装任务的参数：任务记录在 transcode_job，这里只放编号。
type TranscodeArgs struct {
	JobID string `json:"job_id"`
}

func (TranscodeArgs) Kind() string { return "media_transcode" }

func (TranscodeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMedia, MaxAttempts: TranscodeMaxAttempts}
}

// TranscodeRepo 是 worker 用到的仓储方法（media.Repo 的一部分）。
type TranscodeRepo interface {
	StartAttempt(ctx context.Context, id uuid.UUID) (media.Attempt, error)
	ReportProgress(ctx context.Context, id uuid.UUID, progress int) error
	NextKeyVer(ctx context.Context, episodeID int64) (int32, error)
	Skip(ctx context.Context, id uuid.UUID, sourceHash string) error
	Retry(ctx context.Context, id uuid.UUID, sourceHash, msg string) error
	Fail(ctx context.Context, id uuid.UUID, sourceHash, msg string) error
	Complete(ctx context.Context, id uuid.UUID, out media.Output) error
}

// Packager 生成并上传产物（*transcode.Pipeline）。
type Packager interface {
	Fetch(ctx context.Context, jobID, sourceKey string) (*transcode.Workspace, error)
	Package(ctx context.Context, ws *transcode.Workspace, req transcode.Request, progress func(int)) (transcode.Result, error)
	Discard(ctx context.Context, episodeID int64, jobID string) error
	PublicError(err error) string
}

// KeyWrapper 包装内容密钥（*mediakey.Keyring）。
type KeyWrapper interface {
	Wrap(episodeID int64, keyVer int32, key []byte) ([]byte, error)
}

// TranscodeWorker 执行转封装任务。
type TranscodeWorker struct {
	river.WorkerDefaults[TranscodeArgs]
	Repo       TranscodeRepo
	Packager   Packager
	Keys       KeyWrapper
	JobTimeout time.Duration // 单次尝试的超时（MEDIA_TRANSCODE_TIMEOUT）
	// ProgressEvery 为 0 时取 5 秒；测试可调小。
	ProgressEvery time.Duration
	// ProviderID 是产物写入的存储源（S1-6f）；为 0 表示写的是 config.Media 的默认桶，不登记媒体副本。
	ProviderID int64
}

func (w *TranscodeWorker) Timeout(*river.Job[TranscodeArgs]) time.Duration { return w.JobTimeout }

// ConfigureTranscode 注册转封装任务并按并发数配置 media 队列。w 为 nil（本进程不转封装）时去掉 media 队列：
// 没有注册对应任务的进程取到 media 任务只会让它失败。
func ConfigureTranscode(cfg *river.Config, w *TranscodeWorker, concurrency int) {
	if w == nil || concurrency <= 0 {
		delete(cfg.Queues, QueueMedia)
		return
	}
	river.AddWorker(cfg.Workers, w)
	cfg.Queues[QueueMedia] = river.QueueConfig{MaxWorkers: concurrency}
	// 进程被杀后卡在 running 的任务由 River 重新调度，判定时间不能早于单次尝试的超时
	if rescue := w.JobTimeout + 15*time.Minute; rescue > cfg.RescueStuckJobsAfter {
		cfg.RescueStuckJobsAfter = rescue
	}
}

func (w *TranscodeWorker) Work(ctx context.Context, job *river.Job[TranscodeArgs]) error {
	id, err := uuid.Parse(job.Args.JobID)
	if err != nil {
		return river.JobCancel(fmt.Errorf("转封装任务参数无效: %w", err))
	}
	a, err := w.Repo.StartAttempt(ctx, id)
	if errors.Is(err, media.ErrJobGone) {
		slog.InfoContext(ctx, "转封装任务已结束或分集已删除，不再执行", "job_id", id)
		return nil
	}
	if err != nil {
		return err
	}
	log := slog.With("job_id", id, "episode_id", a.EpisodeID, "attempt", job.Attempt)
	started := time.Now()
	hash, err := w.attempt(ctx, a)
	if err == nil {
		log.InfoContext(ctx, "转封装完成", "elapsed", time.Since(started).Round(time.Millisecond))
		return nil
	}

	// 失败：删除本次上传的产物（下一次尝试会重新上传），请求的 ctx 可能已超时，换一个
	bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if derr := w.Packager.Discard(bg, a.EpisodeID, id.String()); derr != nil {
		log.WarnContext(ctx, "清理失败尝试的产物", "err", derr)
	}
	if errors.Is(err, media.ErrJobGone) {
		log.InfoContext(ctx, "转封装途中任务被终止（分集已删除或任务已超时）")
		return nil
	}
	msg := w.Packager.PublicError(err)
	permanent := transcode.IsPermanent(err)
	if !permanent && job.Attempt < job.MaxAttempts {
		if rerr := w.Repo.Retry(bg, id, hash, msg); rerr != nil && !errors.Is(rerr, media.ErrJobGone) {
			log.ErrorContext(ctx, "记录转封装重试", "err", rerr)
		}
		log.WarnContext(ctx, "转封装失败，稍后重试", "err", err)
		return err
	}
	if ferr := w.Repo.Fail(bg, id, hash, msg); ferr != nil && !errors.Is(ferr, media.ErrJobGone) {
		log.ErrorContext(ctx, "记录转封装失败", "err", ferr)
	}
	// 不可恢复或重试用尽：alert=true 供告警规则匹配（告警通道随 X4-1 接入）
	log.ErrorContext(ctx, "转封装失败", "alert", true, "permanent", permanent, "err", err)
	if permanent {
		return river.JobCancel(err)
	}
	return err
}

// attempt 执行一次尝试，返回源文件哈希（下载成功后才有）。
func (w *TranscodeWorker) attempt(ctx context.Context, a media.Attempt) (string, error) {
	report, stop := w.startProgress(ctx, a.ID)
	defer stop()
	ws, err := w.Packager.Fetch(ctx, a.ID.String(), a.SourceKey)
	if err != nil {
		return "", err
	}
	defer func() { _ = ws.Close() }()
	if a.ShouldSkip(ws.SHA256) {
		return ws.SHA256, w.Repo.Skip(ctx, a.ID, ws.SHA256)
	}

	keyVer, err := w.Repo.NextKeyVer(ctx, a.EpisodeID)
	if err != nil {
		return ws.SHA256, err
	}
	key := mediakey.NewContentKey()
	defer clear(key)
	wrapped, err := w.Keys.Wrap(a.EpisodeID, keyVer, key)
	if err != nil {
		return ws.SHA256, fmt.Errorf("包装内容密钥: %w", err)
	}
	res, err := w.Packager.Package(ctx, ws, transcode.Request{JobID: a.ID.String(), EpisodeID: a.EpisodeID, KeyVer: keyVer, Key: key}, report)
	if err != nil {
		return ws.SHA256, err
	}
	out := media.Output{
		EpisodeID: a.EpisodeID, DramaID: a.DramaID, Prefix: res.Prefix, SizeBytes: res.SizeBytes, MasterSHA256: res.MasterSHA256,
		SourceHash: ws.SHA256, KeyVer: keyVer, WrappedKey: wrapped, Poster: res.Poster, DurationSec: int32(res.Duration + 0.5),
		Renditions: make([]media.Rendition, len(res.Renditions)),
		ProviderID: w.ProviderID,
	}
	for i, r := range res.Renditions {
		out.Renditions[i] = media.Rendition(r)
	}
	return ws.SHA256, w.Repo.Complete(ctx, a.ID, out)
}

// startProgress 在后台定时把最新进度写库（同时是心跳），不让 FFmpeg 的输出读取等数据库。
func (w *TranscodeWorker) startProgress(ctx context.Context, id uuid.UUID) (report func(int), stop func()) {
	every := w.ProgressEvery
	if every <= 0 {
		every = progressEvery
	}
	var pct atomic.Int32
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(every)
		defer tick.Stop()
		written, lastWrite := int32(0), time.Now() // 开始尝试时已写过 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			p := pct.Load()
			if p == written && time.Since(lastWrite) < heartbeatEvery {
				continue
			}
			wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
			if err := w.Repo.ReportProgress(wctx, id, int(p)); err != nil && ctx.Err() == nil {
				slog.WarnContext(ctx, "更新转封装进度", "job_id", id, "err", err)
			}
			wcancel()
			written, lastWrite = p, time.Now()
		}
	}()
	return func(p int) { pct.Store(int32(p)) }, func() { cancel(); <-done }
}
