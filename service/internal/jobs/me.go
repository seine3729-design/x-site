package jobs

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/riverqueue/river"
)

// 「我的」域的周期任务（S2-3，docs/03 第 6 节）：观看历史续播断点每分钟从 Valkey 批量 UPSERT 落库。
// 高频上报先写 Valkey 合并缓冲，worker 每分钟取出落库，降低写放大。worker 只在 worker 进程注册。

// HistoryFlusher 把 Valkey 里待落库的续播断点批量 UPSERT 到数据库（domain/me.Service），返回落库行数。
// token 相同时重试同一份：写库失败后 River 重试同一个任务，不丢不重（UPSERT 幂等）。
type HistoryFlusher interface {
	FlushHistory(ctx context.Context, token string) (int, error)
}

// HistoryFlushArgs 是一轮观看历史落库（无参数）。
type HistoryFlushArgs struct{}

func (HistoryFlushArgs) Kind() string { return "history_flush" }

// HistoryFlushWorker 执行一轮观看历史落库。
type HistoryFlushWorker struct {
	river.WorkerDefaults[HistoryFlushArgs]
	Flusher HistoryFlusher
}

func (w *HistoryFlushWorker) Timeout(*river.Job[HistoryFlushArgs]) time.Duration {
	return opsFlushTimeout
}

func (w *HistoryFlushWorker) Work(ctx context.Context, job *river.Job[HistoryFlushArgs]) error {
	n, err := w.Flusher.FlushHistory(ctx, strconv.FormatInt(job.ID, 10))
	if err != nil {
		return err
	}
	if n > 0 {
		slog.InfoContext(ctx, "观看历史已落库", "rows", n)
	}
	return nil
}

// ConfigureMe 注册「我的」域的 worker 与周期任务（入 metrics 队列）。flusher 为 nil 时不注册。
func ConfigureMe(cfg *river.Config, flusher HistoryFlusher) {
	if flusher == nil {
		return
	}
	river.AddWorker(cfg.Workers, &HistoryFlushWorker{Flusher: flusher})
	cfg.PeriodicJobs = append(cfg.PeriodicJobs, periodic(time.Minute, HistoryFlushArgs{}))
}
