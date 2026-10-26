package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"xsite/service/internal/domain/platform"
)

// 回收站清理（docs/06 第 35 节，S4-7）：每天删掉超过保留期的内容。
//
// 删除是物理删除且不可恢复，所以不设 RunOnStart：worker 每次重启都立刻删一轮，
// 会让「刚发现删错了、正准备还原」的窗口变得不可预期。

// trashCleanupTimeout 是一轮清理的上限。删剧会级联删分集与各种关联，给足时间。
const trashCleanupTimeout = 30 * time.Minute

// TrashCleanupArgs 是一轮回收站清理（无参数）。
type TrashCleanupArgs struct{}

func (TrashCleanupArgs) Kind() string { return "trash_cleanup" }

// TrashCleaner 执行清理（domain/platform.TrashService）。
type TrashCleaner interface {
	PurgeExpired(ctx context.Context, now time.Time) ([]platform.TrashItem, error)
	Retention() time.Duration
}

// TrashCleanupWorker 删除已过保留期的回收站内容。
type TrashCleanupWorker struct {
	river.WorkerDefaults[TrashCleanupArgs]
	Svc TrashCleaner
}

func (w *TrashCleanupWorker) Timeout(*river.Job[TrashCleanupArgs]) time.Duration {
	return trashCleanupTimeout
}

func (w *TrashCleanupWorker) Work(ctx context.Context, job *river.Job[TrashCleanupArgs]) error {
	purged, err := w.Svc.PurgeExpired(ctx, time.Now())
	if err != nil {
		return err
	}
	if len(purged) == 0 {
		return nil
	}
	// 把被删的编号与标题记进日志：物理删除之后库里查不到了，出了问题只能靠这里回溯
	titles := make([]string, 0, len(purged))
	for _, p := range purged {
		titles = append(titles, p.Title)
	}
	slog.InfoContext(ctx, "回收站清理完成", "purged", len(purged),
		"retention", w.Svc.Retention().String(), "titles", titles)
	return nil
}

// ConfigureTrashCleanup 注册清理 worker 并加一条周期任务。interval 为 0 时用默认的 24 小时。
func ConfigureTrashCleanup(cfg *river.Config, w *TrashCleanupWorker, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	river.AddWorker(cfg.Workers, w)
	cfg.PeriodicJobs = append(cfg.PeriodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) {
			return TrashCleanupArgs{}, &river.InsertOpts{Queue: river.QueueDefault}
		},
		nil,
	))
}
