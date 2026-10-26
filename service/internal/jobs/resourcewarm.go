package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"xsite/service/internal/domain/resourcewarm"
)

// 资源预热（S4-5，docs/03 第 9.5 节）：预先拉取清单与前 N 个切片，让边缘缓存住。
//
// 一轮预热是一个任务（不像体检那样按剧分片）：「同一时间仅一轮」是这个功能的硬约束，
// 进度、流量与停止信号都要归属到一轮上，拆成多个任务反而要再造一套聚合。
// 并发在任务内部按 concurrency 参数控制。

const (
	// resourceWarmTimeout 是一轮预热的上限。
	//
	// 全站预热是以小时计的：几百集 × 每集几档 × 每档几片。超时后 River 会重试，
	// 但 Run 会看到任务已经不是 running（心跳超时被判定中断）而直接返回，不会重复跑。
	resourceWarmTimeout = 2 * time.Hour
	// resourceWarmMaxAttempts 是 1：预热是长任务且幂等性靠的是「已热好的会很快命中缓存」，
	// 自动重试一轮几小时的任务不是运营想要的 —— 中断了让他自己决定要不要再开。
	resourceWarmMaxAttempts = 1
)

// ResourceWarmArgs 是一轮预热。
type ResourceWarmArgs struct {
	JobID int64 `json:"job_id"`
}

func (ResourceWarmArgs) Kind() string { return "resource_warm" }

func (ResourceWarmArgs) InsertOpts() river.InsertOpts {
	// 进 media 队列：和转封装、复制、体检同属媒体类出站流量
	return river.InsertOpts{Queue: QueueMedia, MaxAttempts: resourceWarmMaxAttempts}
}

// ResourceWarmer 执行一轮预热（domain/resourcewarm.Service）。
type ResourceWarmer interface {
	Run(ctx context.Context, jobID int64) error
}

// ResourceWarmWorker 跑一轮预热。
type ResourceWarmWorker struct {
	river.WorkerDefaults[ResourceWarmArgs]
	Svc ResourceWarmer
}

func (w *ResourceWarmWorker) Timeout(*river.Job[ResourceWarmArgs]) time.Duration {
	return resourceWarmTimeout
}

func (w *ResourceWarmWorker) Work(ctx context.Context, job *river.Job[ResourceWarmArgs]) error {
	id := job.Args.JobID
	if id <= 0 {
		return river.JobCancel(fmt.Errorf("预热任务编号不正确: %d", id))
	}
	start := time.Now()
	if err := w.Svc.Run(ctx, id); err != nil {
		return fmt.Errorf("预热任务 %d: %w", id, err)
	}
	slog.InfoContext(ctx, "资源预热结束", "job_id", id, "ms", time.Since(start).Milliseconds())
	return nil
}

// ConfigureResourceWarm 注册预热任务。没有周期任务：由运营在后台发起。
func ConfigureResourceWarm(cfg *river.Config, w *ResourceWarmWorker) {
	river.AddWorker(cfg.Workers, w)
}

// ResourceWarmEnqueuer 入队预热任务（resourcewarm.Enqueuer），供 API 进程使用。
type ResourceWarmEnqueuer struct {
	River *river.Client[pgx.Tx]
	// Queue 为空时用 Args 上声明的队列；测试用独立队列。
	Queue string
}

func (e *ResourceWarmEnqueuer) EnqueueResourceWarm(ctx context.Context, jobID int64) error {
	var opts *river.InsertOpts
	if e.Queue != "" {
		opts = &river.InsertOpts{Queue: e.Queue, MaxAttempts: resourceWarmMaxAttempts}
	}
	if _, err := e.River.Insert(ctx, ResourceWarmArgs{JobID: jobID}, opts); err != nil {
		return fmt.Errorf("入队资源预热: %w", err)
	}
	return nil
}

var _ resourcewarm.Enqueuer = (*ResourceWarmEnqueuer)(nil)
