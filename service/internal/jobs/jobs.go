// Package jobs 定义 River 任务与队列（docs/03 第 0.3 节）。
package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// 队列划分：默认、媒体处理、数据汇总、推送。
const (
	QueueDefault = river.QueueDefault
	QueueMedia   = "media"
	QueueMetrics = "metrics"
	QueuePush    = "push"
)

// HeartbeatArgs 是 worker 存活心跳，用于确认任务链路正常。
type HeartbeatArgs struct{}

func (HeartbeatArgs) Kind() string { return "heartbeat" }

// HeartbeatWorker 记录一条日志。
type HeartbeatWorker struct {
	river.WorkerDefaults[HeartbeatArgs]
}

func (w *HeartbeatWorker) Work(ctx context.Context, job *river.Job[HeartbeatArgs]) error {
	slog.InfoContext(ctx, "worker 心跳", "job_id", job.ID)
	return nil
}

// Config 返回 River 客户端配置：注册全部 worker、队列与周期任务。
func Config() *river.Config {
	workers := river.NewWorkers()
	river.AddWorker(workers, &HeartbeatWorker{})

	return &river.Config{
		Queues: map[string]river.QueueConfig{
			QueueDefault: {MaxWorkers: 20},
			QueueMedia:   {MaxWorkers: 4},
			QueueMetrics: {MaxWorkers: 4},
			QueuePush:    {MaxWorkers: 10},
		},
		Workers: workers,
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(5*time.Minute),
				func() (river.JobArgs, *river.InsertOpts) { return HeartbeatArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		},
	}
}
