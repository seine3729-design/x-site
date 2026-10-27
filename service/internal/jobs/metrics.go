package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// 看板汇总与事件分区维护（docs/03 第 8.2 节，S4-2）。两者都入 metrics 队列。

// ---------- 汇总 ----------

const metricsRollupTimeout = 10 * time.Minute

// MetricsRollupArgs 是一轮看板汇总（无参数）。
type MetricsRollupArgs struct{}

func (MetricsRollupArgs) Kind() string { return "metrics_rollup" }

// Roller 执行汇总（domain/analytics.Rollup）。
type Roller interface {
	Daily(ctx context.Context, days int) error
	Hourly(ctx context.Context, hours int) error
}

// MetricsRollupWorker 把 event_raw 汇总到 metric_* 表。
type MetricsRollupWorker struct {
	river.WorkerDefaults[MetricsRollupArgs]
	Svc Roller
}

func (w *MetricsRollupWorker) Timeout(*river.Job[MetricsRollupArgs]) time.Duration {
	return metricsRollupTimeout
}

func (w *MetricsRollupWorker) Work(ctx context.Context, _ *river.Job[MetricsRollupArgs]) error {
	// 重算今天 + 昨天（跨过北京时间凌晨后昨天即定稿），以及最近 3 个 UTC 小时的实时预聚合。
	if err := w.Svc.Daily(ctx, 2); err != nil {
		return err
	}
	return w.Svc.Hourly(ctx, 3)
}

// ConfigureMetricsRollup 注册汇总 worker 并加周期任务（入 metrics 队列）。interval 为 0 时用默认 1 小时。
func ConfigureMetricsRollup(cfg *river.Config, w *MetricsRollupWorker, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	river.AddWorker(cfg.Workers, w)
	cfg.PeriodicJobs = append(cfg.PeriodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) {
			return MetricsRollupArgs{}, &river.InsertOpts{Queue: QueueMetrics}
		},
		&river.PeriodicJobOpts{RunOnStart: true},
	))
}

// ---------- 分区维护 ----------

const eventPartitionTimeout = 5 * time.Minute

// EventPartitionArgs 是一轮事件分区维护（无参数）。
type EventPartitionArgs struct{}

func (EventPartitionArgs) Kind() string { return "event_partition" }

// PartitionMaintainer 维护 event_raw 分区（domain/analytics.Rollup）。
type PartitionMaintainer interface {
	MaintainPartitions(ctx context.Context, ahead int, retention time.Duration) (int, error)
}

// EventPartitionWorker 滚动创建未来分区并删除过期分区。
type EventPartitionWorker struct {
	river.WorkerDefaults[EventPartitionArgs]
	Svc       PartitionMaintainer
	Retention time.Duration // event_raw 保留期；为 0 时用默认 90 天
}

func (w *EventPartitionWorker) Timeout(*river.Job[EventPartitionArgs]) time.Duration {
	return eventPartitionTimeout
}

func (w *EventPartitionWorker) Work(ctx context.Context, _ *river.Job[EventPartitionArgs]) error {
	retention := w.Retention
	if retention <= 0 {
		retention = 90 * 24 * time.Hour
	}
	dropped, err := w.Svc.MaintainPartitions(ctx, 7, retention)
	if err != nil {
		return err
	}
	if dropped > 0 {
		slog.InfoContext(ctx, "清理过期事件分区", "dropped", dropped, "retention", retention.String())
	}
	return nil
}

// ConfigureEventPartition 注册分区维护 worker 并加周期任务（入 metrics 队列）。interval 为 0 时用默认 24 小时。
func ConfigureEventPartition(cfg *river.Config, w *EventPartitionWorker, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	river.AddWorker(cfg.Workers, w)
	cfg.PeriodicJobs = append(cfg.PeriodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) {
			return EventPartitionArgs{}, &river.InsertOpts{Queue: QueueMetrics}
		},
		&river.PeriodicJobOpts{RunOnStart: true},
	))
}
