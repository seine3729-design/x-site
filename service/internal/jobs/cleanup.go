package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"xsite/service/internal/domain/storage"
)

// 清理孤立产物（docs/12 第 5 节第 6 条，S1-6f）：每天刷新孤立标记并删除已过保留期的产物。
// 标记与清理放在同一个任务里：先标记再清理，保证新标记的对象至少过完整个保留期才会被删。

// cleanupJobTimeout 是一轮清理的上限：要删各存储源上的文件，给足时间。
const cleanupJobTimeout = time.Hour

// MediaCleanupArgs 是一轮孤立产物清理（无参数）。
type MediaCleanupArgs struct{}

func (MediaCleanupArgs) Kind() string { return "media_cleanup" }

// Cleaner 执行标记与清理（domain/storage.CleanupService）。
type Cleaner interface {
	Mark(ctx context.Context) (marked, unmarked int64, err error)
	Cleanup(ctx context.Context, dryRun bool, now time.Time) (storage.CleanupResult, error)
	Retention() time.Duration
}

// MediaCleanupWorker 刷新孤立标记并清理过期产物。
type MediaCleanupWorker struct {
	river.WorkerDefaults[MediaCleanupArgs]
	Svc Cleaner
}

func (w *MediaCleanupWorker) Timeout(*river.Job[MediaCleanupArgs]) time.Duration {
	return cleanupJobTimeout
}

func (w *MediaCleanupWorker) Work(ctx context.Context, job *river.Job[MediaCleanupArgs]) error {
	marked, unmarked, err := w.Svc.Mark(ctx)
	if err != nil {
		return err
	}
	res, err := w.Svc.Cleanup(ctx, false, time.Now())
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "孤立产物清理完成", "marked", marked, "unmarked", unmarked,
		"scanned", res.Scanned, "deleted", res.Deleted, "files", res.Files, "bytes", res.Bytes, "kept", res.Kept,
		"retention", w.Svc.Retention().String())
	return nil
}

// ConfigureMediaCleanup 注册清理 worker 并加一条周期任务。interval 为 0 时用默认的 24 小时。
// 不设 RunOnStart：清理会删文件，不该在每次 worker 重启时立刻跑一轮。
func ConfigureMediaCleanup(cfg *river.Config, w *MediaCleanupWorker, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	river.AddWorker(cfg.Workers, w)
	cfg.PeriodicJobs = append(cfg.PeriodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) {
			return MediaCleanupArgs{}, &river.InsertOpts{Queue: QueueMedia}
		},
		nil,
	))
}
