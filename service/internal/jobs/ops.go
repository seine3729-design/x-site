package jobs

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"xsite/service/internal/errs"
)

// 运营域的周期任务（docs/specs/A3-4.md）：广告曝光、点击每分钟从 Valkey 落库，热搜词的搜索次数每小时落库，
// 两张榜单每 5 分钟重新排名。worker 只在 worker 进程注册；API 进程用 RankRecomputeClient 入队「马上重新排名」。

const (
	opsFlushTimeout      = 30 * time.Second
	rankRecomputeTimeout = time.Minute
	rankRecomputeEvery   = 5 * time.Minute
	rankRecomputeWait    = 5 * time.Second // 「马上重新排名」最多等 worker 这么久
	rankRecomputePoll    = 200 * time.Millisecond
)

var errRankRecomputeFailed = errs.ErrUnavailable.WithMessage("重新排名失败，请稍后重试（榜单每 5 分钟也会自动重新排名）")

// CountFlusher 把 Valkey 里累计的计数写入数据库（domain/ops.AdService、HotsearchService），返回写入的条数。
// token 相同时重试同一份数据：写库失败后 River 重试同一个任务，不会丢也不会重复。
type CountFlusher interface {
	FlushCounts(ctx context.Context, token string) (int, error)
}

// RankRecomputer 重新计算全部榜单并原子替换线上榜单（domain/ops.RankService）。
type RankRecomputer interface {
	Recompute(ctx context.Context) error
}

// AdCountFlushArgs 是一轮广告计数落库（无参数）。
type AdCountFlushArgs struct{}

func (AdCountFlushArgs) Kind() string { return "ad_count_flush" }

// AdCountFlushWorker 执行一轮广告计数落库。
type AdCountFlushWorker struct {
	river.WorkerDefaults[AdCountFlushArgs]
	Flusher CountFlusher
}

func (w *AdCountFlushWorker) Timeout(*river.Job[AdCountFlushArgs]) time.Duration {
	return opsFlushTimeout
}

func (w *AdCountFlushWorker) Work(ctx context.Context, job *river.Job[AdCountFlushArgs]) error {
	return flush(ctx, w.Flusher, job.ID, "广告计数已落库", "ads")
}

// HotsearchFlushArgs 是一轮热搜计数落库（无参数）。
type HotsearchFlushArgs struct{}

func (HotsearchFlushArgs) Kind() string { return "hotsearch_flush" }

// HotsearchFlushWorker 执行一轮热搜计数落库。
type HotsearchFlushWorker struct {
	river.WorkerDefaults[HotsearchFlushArgs]
	Flusher CountFlusher
}

func (w *HotsearchFlushWorker) Timeout(*river.Job[HotsearchFlushArgs]) time.Duration {
	return opsFlushTimeout
}

func (w *HotsearchFlushWorker) Work(ctx context.Context, job *river.Job[HotsearchFlushArgs]) error {
	return flush(ctx, w.Flusher, job.ID, "热搜计数已落库", "words")
}

// flush 用任务编号作落库标识：写库失败时取出的计数留在该标识下，River 重试同一个任务时再写一次。
func flush(ctx context.Context, f CountFlusher, jobID int64, msg, unit string) error {
	n, err := f.FlushCounts(ctx, strconv.FormatInt(jobID, 10))
	if err != nil {
		return err
	}
	if n > 0 {
		slog.InfoContext(ctx, msg, unit, n)
	}
	return nil
}

// RankRecomputeArgs 是一轮榜单重新排名（无参数，两张榜一起算）。周期任务与后台「马上重新排名」共用。
type RankRecomputeArgs struct{}

func (RankRecomputeArgs) Kind() string { return "rank_recompute" }

// RankRecomputeWorker 执行一轮重新排名。重算是幂等的：同时有多个任务也只是多算几次。
type RankRecomputeWorker struct {
	river.WorkerDefaults[RankRecomputeArgs]
	Ranks RankRecomputer
}

func (w *RankRecomputeWorker) Timeout(*river.Job[RankRecomputeArgs]) time.Duration {
	return rankRecomputeTimeout
}

func (w *RankRecomputeWorker) Work(ctx context.Context, _ *river.Job[RankRecomputeArgs]) error {
	return w.Ranks.Recompute(ctx)
}

// RankRecomputeClient 是 API 进程里的「马上重新排名」（实现 domain/ops.RecomputeQueue）：入队后等 worker 算完。
type RankRecomputeClient struct {
	River *river.Client[pgx.Tx]
	// Queue 为空时用 metrics 队列（与周期任务相同）；测试用独立队列，避免被本机正在运行的 worker 取走。
	Queue string
	// Wait、Poll 为 0 时取默认值（最多等 5 秒，每 200 毫秒查一次）。
	Wait time.Duration
	Poll time.Duration
}

// RecomputeNow 入队并等待：等待时间内算完返回 true；还在排队或执行中返回 false（任务不撤销，worker 稍后会算完）；
// 任务被丢弃或撤销时返回错误。
func (c *RankRecomputeClient) RecomputeNow(ctx context.Context) (bool, error) {
	inserted, err := c.River.Insert(ctx, RankRecomputeArgs{}, &river.InsertOpts{Queue: cmp.Or(c.Queue, QueueMetrics)})
	if err != nil {
		return false, fmt.Errorf("重新排名任务入队: %w", err)
	}
	id := inserted.Job.ID

	waitCtx, cancel := context.WithTimeout(ctx, cmp.Or(c.Wait, rankRecomputeWait))
	defer cancel()
	tick := time.NewTicker(cmp.Or(c.Poll, rankRecomputePoll))
	defer tick.Stop()
	for {
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, nil
		case <-tick.C:
		}
		job, err := c.River.JobGet(waitCtx, id)
		if err != nil {
			if waitCtx.Err() != nil {
				continue // 下一轮进入超时分支
			}
			return false, fmt.Errorf("读取重新排名任务 %d: %w", id, err)
		}
		switch job.State {
		case rivertype.JobStateCompleted:
			return true, nil
		case rivertype.JobStateDiscarded, rivertype.JobStateCancelled:
			var last string
			if n := len(job.Errors); n > 0 {
				last = job.Errors[n-1].Error
			}
			slog.ErrorContext(ctx, "重新排名任务失败", "job_id", id, "state", job.State, "err", last)
			return false, errRankRecomputeFailed
		}
	}
}

// OpsWorkers 是运营域周期任务的依赖，由 worker 进程注入；为 nil 的项不注册。
type OpsWorkers struct {
	Ads       CountFlusher   // 每分钟
	Hotsearch CountFlusher   // 每小时
	Ranks     RankRecomputer // 每 5 分钟
}

// ConfigureOps 注册运营域的 worker 与周期任务（入 metrics 队列）。
func ConfigureOps(cfg *river.Config, w OpsWorkers) {
	if w.Ads != nil {
		river.AddWorker(cfg.Workers, &AdCountFlushWorker{Flusher: w.Ads})
		cfg.PeriodicJobs = append(cfg.PeriodicJobs, periodic(time.Minute, AdCountFlushArgs{}))
	}
	if w.Hotsearch != nil {
		river.AddWorker(cfg.Workers, &HotsearchFlushWorker{Flusher: w.Hotsearch})
		cfg.PeriodicJobs = append(cfg.PeriodicJobs, periodic(time.Hour, HotsearchFlushArgs{}))
	}
	if w.Ranks != nil {
		river.AddWorker(cfg.Workers, &RankRecomputeWorker{Ranks: w.Ranks})
		cfg.PeriodicJobs = append(cfg.PeriodicJobs, periodic(rankRecomputeEvery, RankRecomputeArgs{}))
	}
}

func periodic(interval time.Duration, args river.JobArgs) *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) { return args, &river.InsertOpts{Queue: QueueMetrics} },
		&river.PeriodicJobOpts{RunOnStart: true},
	)
}
