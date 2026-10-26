package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// 资源体检（S4-5，docs/03 第 9.4 节）：按剧分片，一部剧一个任务。
//
// 为什么分片到剧而不是整站一个任务：一部剧几十集、一集好几个资源，整站一个任务既无法并行、
// 失败重试也要从头再来。按剧分片之后，队列的并发度天然限制了同时压多少部剧，
// 单部剧内部再限并发（domain 的 probeConcurrency），两者相乘就是对源站的实际压力。

const (
	// resourceCheckTimeout 是一部剧的体检上限。单个资源最多探 10 秒，并发 4，
	// 按最长的剧（上百集、每集 3 个资源）估算也够。
	resourceCheckTimeout = 10 * time.Minute
	// resourceCheckMaxAttempts 是重试次数。源站 5xx 或网络抖动重试一次通常就好了；
	// 真正打不开的资源不该靠重试解决，结果里已经如实记下来了。
	resourceCheckMaxAttempts = 2
)

// ResourceCheckArgs 是一部剧的体检。
type ResourceCheckArgs struct {
	DramaID int64 `json:"drama_id"`
}

func (ResourceCheckArgs) Kind() string { return "resource_check" }

func (ResourceCheckArgs) InsertOpts() river.InsertOpts {
	// 进 media 队列：体检是对象存储的批量读，和转封装、复制同属媒体类出站流量，
	// 放一起便于统一控制对存储的压力，也不会把 default 队列堵住。
	return river.InsertOpts{Queue: QueueMedia, MaxAttempts: resourceCheckMaxAttempts}
}

// ResourceChecker 体检一部剧（domain/resourcecheck.Service）。
type ResourceChecker interface {
	CheckDrama(ctx context.Context, dramaID int64) (int, error)
}

// ResourceCheckWorker 执行一部剧的体检。
type ResourceCheckWorker struct {
	river.WorkerDefaults[ResourceCheckArgs]
	Svc ResourceChecker
}

func (w *ResourceCheckWorker) Timeout(*river.Job[ResourceCheckArgs]) time.Duration {
	return resourceCheckTimeout
}

func (w *ResourceCheckWorker) Work(ctx context.Context, job *river.Job[ResourceCheckArgs]) error {
	id := job.Args.DramaID
	if id <= 0 {
		return river.JobCancel(fmt.Errorf("资源体检的剧编号不正确: %d", id))
	}
	start := time.Now()
	n, err := w.Svc.CheckDrama(ctx, id)
	if err != nil {
		return fmt.Errorf("体检剧集 %d: %w", id, err)
	}
	slog.InfoContext(ctx, "资源体检完成", "drama_id", id, "checked", n, "ms", time.Since(start).Milliseconds())
	return nil
}

// ConfigureResourceCheck 注册体检任务。没有周期任务：体检由运营在后台按需发起。
func ConfigureResourceCheck(cfg *river.Config, w *ResourceCheckWorker) {
	river.AddWorker(cfg.Workers, w)
}

// ResourceCheckEnqueuer 入队体检任务（resourcecheck.Enqueuer），供 API 进程使用。
type ResourceCheckEnqueuer struct {
	River *river.Client[pgx.Tx]
	// Queue 为空时用 Args 上声明的队列；测试用独立队列。
	Queue string
}

func (e *ResourceCheckEnqueuer) EnqueueResourceCheck(ctx context.Context, dramaID int64) error {
	var opts *river.InsertOpts
	if e.Queue != "" {
		opts = &river.InsertOpts{Queue: e.Queue, MaxAttempts: resourceCheckMaxAttempts}
	}
	if _, err := e.River.Insert(ctx, ResourceCheckArgs{DramaID: dramaID}, opts); err != nil {
		return fmt.Errorf("入队资源体检: %w", err)
	}
	return nil
}
