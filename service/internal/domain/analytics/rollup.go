package analytics

import (
	"context"
	"fmt"
	"time"
)

// RollupRepo 汇总与分区维护（store 实现）。
type RollupRepo interface {
	// RollupDay 定稿某一天：day 为北京日期字符串 YYYY-MM-DD，from/to 为该日的 UTC 时间范围。
	RollupDay(ctx context.Context, day string, from, to time.Time) error
	RollupHour(ctx context.Context, hour time.Time) error
	EnsurePartitions(ctx context.Context, from, to time.Time) error
	DropPartitionsBefore(ctx context.Context, cutoff time.Time) (int, error)
}

// Rollup 是看板汇总与 event_raw 分区维护（worker 进程用）。日表按北京时间日界定稿（docs/03 第 8.2 节）。
type Rollup struct {
	repo RollupRepo
	loc  *time.Location
	now  func() time.Time
}

// NewRollup 构造 Rollup。北京时区解析失败（容器缺 tzdata）时退回固定 +8。
func NewRollup(repo RollupRepo) *Rollup {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return &Rollup{repo: repo, loc: loc, now: time.Now}
}

// Daily 重算最近 days 天（含今天，北京时间日界）。今天是 partial，昨天及更早一经跨日即定稿。
func (r *Rollup) Daily(ctx context.Context, days int) error {
	if days < 1 {
		days = 1
	}
	today := r.now().In(r.loc)
	for i := 0; i < days; i++ {
		d := today.AddDate(0, 0, -i)
		start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, r.loc)
		if err := r.repo.RollupDay(ctx, start.Format("2006-01-02"), start.UTC(), start.AddDate(0, 0, 1).UTC()); err != nil {
			return fmt.Errorf("日汇总 %s: %w", start.Format("2006-01-02"), err)
		}
	}
	return nil
}

// Hourly 重算最近 hours 个 UTC 小时（含当前小时），供实时卡片。
func (r *Rollup) Hourly(ctx context.Context, hours int) error {
	if hours < 1 {
		hours = 1
	}
	cur := r.now().UTC().Truncate(time.Hour)
	for i := 0; i < hours; i++ {
		if err := r.repo.RollupHour(ctx, cur.Add(-time.Duration(i)*time.Hour)); err != nil {
			return fmt.Errorf("小时汇总: %w", err)
		}
	}
	return nil
}

// MaintainPartitions 建未来 ahead 天的 event_raw 分区，并删除 retention 之前的旧分区，返回删除数量。
func (r *Rollup) MaintainPartitions(ctx context.Context, ahead int, retention time.Duration) (int, error) {
	now := r.now().UTC()
	if err := r.repo.EnsurePartitions(ctx, now, now.AddDate(0, 0, ahead)); err != nil {
		return 0, err
	}
	return r.repo.DropPartitionsBefore(ctx, now.Add(-retention))
}
