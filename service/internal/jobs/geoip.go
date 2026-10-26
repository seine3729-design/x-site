package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// GeoIP 数据库热更新（docs/12 第 4.2 节，S1-6c）：周期检查数据文件是否变化，变了就原子替换内存中的读取器。
//
// 文件由运维更新（放到 GEOIP_XDB_PATH），不需要重启进程。
// 注意这个任务只在**本进程内**替换读取器，所以每个需要解析地区的进程（API、worker）都要各自注册。
// 真正按地区选线的是 API 进程；worker 这边注册是为了让健康探测与复制任务里的日志也能带上地区。

// GeoIPReloadArgs 是一次数据库变更检查。
type GeoIPReloadArgs struct{}

func (GeoIPReloadArgs) Kind() string { return "geoip_reload" }

// GeoReloader 重新加载数据库（geoip.Resolver）。
type GeoReloader interface {
	Reload() (bool, error)
}

// GeoIPReloadWorker 检查并热更新 GeoIP 数据库。
type GeoIPReloadWorker struct {
	river.WorkerDefaults[GeoIPReloadArgs]
	R GeoReloader
}

func (w *GeoIPReloadWorker) Timeout(*river.Job[GeoIPReloadArgs]) time.Duration { return time.Minute }

func (w *GeoIPReloadWorker) Work(ctx context.Context, job *river.Job[GeoIPReloadArgs]) error {
	changed, err := w.R.Reload()
	if err != nil {
		// 文件损坏或暂时读不到时继续用旧数据：宁可用过期数据，也不要失去解析能力
		slog.WarnContext(ctx, "GeoIP 数据库热更新失败，继续使用当前数据", "err", err)
		return nil
	}
	if changed {
		slog.InfoContext(ctx, "GeoIP 数据库已热更新")
	}
	return nil
}

// ConfigureGeoIPReload 注册热更新 worker 与周期任务。r 为 nil（没配数据文件）时什么都不做。
func ConfigureGeoIPReload(cfg *river.Config, r GeoReloader, interval time.Duration) {
	if r == nil {
		return
	}
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	river.AddWorker(cfg.Workers, &GeoIPReloadWorker{R: r})
	cfg.PeriodicJobs = append(cfg.PeriodicJobs, river.NewPeriodicJob(
		river.PeriodicInterval(interval),
		func() (river.JobArgs, *river.InsertOpts) {
			return GeoIPReloadArgs{}, &river.InsertOpts{Queue: QueueMetrics}
		},
		nil,
	))
}
