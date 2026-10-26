package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"xsite/service/internal/domain/storage"
)

// 媒体复制（docs/12 第 5 节第 3 条，S1-6f）：产物写入主存储后入队，worker 把对象复制到全部镜像存储源。
// 任务在产物记录的同一事务内入队（与转封装任务一致），因此不会出现「有产物但没有复制任务」。

// ReplicateMaxAttempts 是复制任务的尝试次数：镜像存储源可能临时不可用，多给几次。
const ReplicateMaxAttempts = 5

// replicateJobTimeout 是一次复制的上限：一集产物有上百个切片，跨云复制要留足时间。
const replicateJobTimeout = 30 * time.Minute

// ReplicateArgs 是一次媒体复制任务。
type ReplicateArgs struct {
	ObjectID int64 `json:"object_id"`
}

func (ReplicateArgs) Kind() string { return "media_replicate" }

func (ReplicateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMedia, MaxAttempts: ReplicateMaxAttempts}
}

// Replicator 执行复制（domain/storage.ReplicateService）。
type Replicator interface {
	Replicate(ctx context.Context, objectID int64) (storage.ReplicateResult, error)
}

// ReplicateWorker 把一个媒体对象复制到全部镜像存储源。
type ReplicateWorker struct {
	river.WorkerDefaults[ReplicateArgs]
	Svc Replicator
}

func (w *ReplicateWorker) Timeout(*river.Job[ReplicateArgs]) time.Duration {
	return replicateJobTimeout
}

func (w *ReplicateWorker) Work(ctx context.Context, job *river.Job[ReplicateArgs]) error {
	res, err := w.Svc.Replicate(ctx, job.Args.ObjectID)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "媒体复制完成", "object_id", res.ObjectID, "copied", res.Copied,
		"failed", res.Failed, "permanent", res.Permanent, "skipped", res.Skipped, "no_source", res.NoSource)
	// 只有临时失败（网络、存储抖动）才让 River 重试；已成功的镜像在下一轮会因 ready 被跳过。
	// 配置类失败与「没有复制源」重试解决不了，任务按完成处理，副本状态里留着原因等运营处理。
	if res.Failed > 0 {
		return &replicaFailedError{objectID: res.ObjectID, failed: res.Failed}
	}
	return nil
}

type replicaFailedError struct {
	objectID int64
	failed   int
}

func (e *replicaFailedError) Error() string {
	return fmt.Sprintf("对象 %d 有 %d 个镜像存储源复制失败，等待重试", e.objectID, e.failed)
}

// ConfigureReplicate 注册复制 worker（只在 worker 进程调用）。
func ConfigureReplicate(cfg *river.Config, w *ReplicateWorker) {
	river.AddWorker(cfg.Workers, w)
}

// ---------- 存储源迁移（S1-6f，docs/12 第 5 节第 5 条）----------

// MigrateArgs 是把一个对象从一个存储源迁移到另一个（复制到目标后删除源的副本记录）。
type MigrateArgs struct {
	ObjectID       int64 `json:"object_id"`
	FromProviderID int64 `json:"from_provider_id"`
	ToProviderID   int64 `json:"to_provider_id"`
}

func (MigrateArgs) Kind() string { return "media_migrate" }

func (MigrateArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMedia, MaxAttempts: ReplicateMaxAttempts}
}

// Migrator 执行迁移（domain/storage.ReplicateService）。
type Migrator interface {
	MigrateOne(ctx context.Context, objectID, fromProviderID, toProviderID int64) error
}

// MigrateWorker 迁移一个媒体对象。
type MigrateWorker struct {
	river.WorkerDefaults[MigrateArgs]
	Svc Migrator
}

func (w *MigrateWorker) Timeout(*river.Job[MigrateArgs]) time.Duration { return replicateJobTimeout }

func (w *MigrateWorker) Work(ctx context.Context, job *river.Job[MigrateArgs]) error {
	return w.Svc.MigrateOne(ctx, job.Args.ObjectID, job.Args.FromProviderID, job.Args.ToProviderID)
}

// ConfigureMigrate 注册迁移 worker（只在 worker 进程调用）。
func ConfigureMigrate(cfg *river.Config, w *MigrateWorker) {
	river.AddWorker(cfg.Workers, w)
}

// ---------- 入队器（API 进程用）----------

// MediaEnqueuer 把复制与迁移任务入队，实现 storage.ReplicateEnqueuer 与 storage.MigrateEnqueuer。
// 这两个任务不要求与数据库写操作同事务（手动触发是独立操作），所以用普通 Insert。
type MediaEnqueuer struct {
	River *river.Client[pgx.Tx]
}

func (e *MediaEnqueuer) EnqueueReplicate(ctx context.Context, objectID int64) error {
	_, err := e.River.Insert(ctx, ReplicateArgs{ObjectID: objectID}, nil)
	return err
}

func (e *MediaEnqueuer) EnqueueMigrate(ctx context.Context, objectID, fromProviderID, toProviderID int64) error {
	_, err := e.River.Insert(ctx, MigrateArgs{
		ObjectID: objectID, FromProviderID: fromProviderID, ToProviderID: toProviderID,
	}, nil)
	return err
}

// ---------- 接管历史产物（S1-6f，docs/12 第 5 节末）----------

// AdoptArgs 是把一个历史对象登记为某存储源副本的任务（任务里先校验对象真实存在）。
type AdoptArgs struct {
	ObjectID   int64 `json:"object_id"`
	ProviderID int64 `json:"provider_id"`
}

func (AdoptArgs) Kind() string { return "media_adopt" }

func (AdoptArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: QueueMedia, MaxAttempts: ReplicateMaxAttempts}
}

// Adopter 执行接管（domain/storage.AdoptService）。
type Adopter interface {
	AdoptOne(ctx context.Context, objectID, providerID int64) error
}

// AdoptWorker 校验并登记一个历史对象的副本。
type AdoptWorker struct {
	river.WorkerDefaults[AdoptArgs]
	Svc Adopter
}

// adoptJobTimeout：只做存在性校验与一次写库，不搬数据。
const adoptJobTimeout = 2 * time.Minute

func (w *AdoptWorker) Timeout(*river.Job[AdoptArgs]) time.Duration { return adoptJobTimeout }

func (w *AdoptWorker) Work(ctx context.Context, job *river.Job[AdoptArgs]) error {
	return w.Svc.AdoptOne(ctx, job.Args.ObjectID, job.Args.ProviderID)
}

// ConfigureAdopt 注册接管 worker（只在 worker 进程调用）。
func ConfigureAdopt(cfg *river.Config, w *AdoptWorker) {
	river.AddWorker(cfg.Workers, w)
}

func (e *MediaEnqueuer) EnqueueAdopt(ctx context.Context, objectID, providerID int64) error {
	_, err := e.River.Insert(ctx, AdoptArgs{ObjectID: objectID, ProviderID: providerID}, nil)
	return err
}
