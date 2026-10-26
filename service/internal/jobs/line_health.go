package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"xsite/service/internal/domain/storage"
)

// 线路健康探测（S1-6d，docs/12 第 4.3 节）：worker 周期性拉每条线路的 probe_path，把成功率与耗时换算成
// 健康分写入 Valkey，供选线算法与后台健康看板使用。只在 worker 进程注册（API 进程不消费）。

// 整批探测的任务上限：单条探测有自己的超时，这里兜底防止某条卡住拖垮整批。
const lineHealthJobTimeout = 3 * time.Minute

// LineHealthProbeArgs 是一轮线路健康探测（无参数，探测哪些线路在 worker 内决定）。
type LineHealthProbeArgs struct{}

func (LineHealthProbeArgs) Kind() string { return "line_health_probe" }

// LineHealthWorker 执行一轮线路健康探测。依赖在 worker 进程注入（列线路、HTTP 探测、Valkey 健康分存储）。
type LineHealthWorker struct {
	river.WorkerDefaults[LineHealthProbeArgs]
	Lines        storage.LineLister
	Prober       storage.LineProber
	Store        storage.HealthStore
	ProbeTimeout time.Duration
	TTL          time.Duration
	// Origins 是站点入口域名，用于探测线路是否返回允许它们的 CORS 头；接入站点域名配置前可空。
	Origins []string
}

func (w *LineHealthWorker) Timeout(*river.Job[LineHealthProbeArgs]) time.Duration {
	return lineHealthJobTimeout
}

func (w *LineHealthWorker) Work(ctx context.Context, job *river.Job[LineHealthProbeArgs]) error {
	lines, err := w.Lines.ListLines(ctx, storage.LineQuery{})
	if err != nil {
		return fmt.Errorf("列出线路: %w", err)
	}
	var probed, failed int
	for _, l := range lines {
		// 停用的不探测；直连存储（无 base_url）的健康由存储源连通性决定，这里无独立端点可探。
		if l.Status == storage.LineDisabled || l.BaseURL == "" {
			continue
		}
		res := w.probe(ctx, l)
		prev, hasPrev, err := w.Store.GetHealth(ctx, l.ID)
		if err != nil {
			slog.WarnContext(ctx, "读取线路历史健康分失败，按首次处理", "line_id", l.ID, "err", err)
			hasPrev = false
		}
		h := storage.NewHealthFromProbe(l.ID, res, prev, hasPrev, time.Now().UTC())
		if err := w.Store.SaveHealth(ctx, h, w.TTL); err != nil {
			slog.WarnContext(ctx, "写入线路健康分失败", "line_id", l.ID, "err", err)
			continue
		}
		probed++
		if !res.OK {
			failed++
		}
	}
	slog.InfoContext(ctx, "线路健康探测完成", "probed", probed, "failed", failed)
	return nil
}

// probe 对单条线路发起一次 HTTP 探测，带单条超时。
func (w *LineHealthWorker) probe(ctx context.Context, l storage.Line) storage.LineProbeResult {
	if w.ProbeTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, w.ProbeTimeout)
		defer cancel()
	}
	return w.Prober.ProbeLine(ctx, storage.LineProbeRequest{BaseURL: l.BaseURL, ProbePath: l.ProbePath, Origins: w.Origins})
}

// ConfigureLineHealth 注册健康探测 worker 并加一条每 interval 执行一次的周期任务（入 metrics 队列）。
// 只在启用探测时由 worker 进程调用（照 ConfigureTranscode 的模式）。
func ConfigureLineHealth(cfg *river.Config, w *LineHealthWorker, interval time.Duration) {
	river.AddWorker(cfg.Workers, w)
	cfg.PeriodicJobs = append(cfg.PeriodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) {
			return LineHealthProbeArgs{}, &river.InsertOpts{Queue: QueueMetrics}
		},
		&river.PeriodicJobOpts{RunOnStart: true},
	))
}
