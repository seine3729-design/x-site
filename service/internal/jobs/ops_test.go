package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
)

type fakeAdFlusher struct {
	tokens []string
	n      int
	err    error
}

func (f *fakeAdFlusher) FlushCounts(_ context.Context, token string) (int, error) {
	f.tokens = append(f.tokens, token)
	return f.n, f.err
}

func TestAdCountFlushWorker(t *testing.T) {
	f := &fakeAdFlusher{n: 3}
	w := &AdCountFlushWorker{Flusher: f}
	job := &river.Job[AdCountFlushArgs]{JobRow: &rivertype.JobRow{ID: 42}}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	// 重试时 River 用同一个任务编号，落库失败的那一份能被同一个任务再写一次
	if len(f.tokens) != 1 || f.tokens[0] != "42" {
		t.Fatalf("用任务编号作落库标识：%v", f.tokens)
	}
	f.err = errors.New("db down")
	if err := w.Work(context.Background(), job); err == nil {
		t.Fatal("落库失败应返回错误，由 River 重试")
	}
}

func TestConfigureOps(t *testing.T) {
	cfg := Config()
	before := len(cfg.PeriodicJobs)
	ConfigureOps(cfg, OpsWorkers{Ads: &fakeAdFlusher{}})
	if len(cfg.PeriodicJobs) != before+1 {
		t.Fatalf("应加一条每分钟的周期任务：%d → %d", before, len(cfg.PeriodicJobs))
	}
	if AdCountFlushArgs.Kind(AdCountFlushArgs{}) != "ad_count_flush" {
		t.Fatal("任务类型名")
	}
}

func TestHotsearchFlushWorker(t *testing.T) {
	f := &fakeAdFlusher{n: 2}
	w := &HotsearchFlushWorker{Flusher: f}
	job := &river.Job[HotsearchFlushArgs]{JobRow: &rivertype.JobRow{ID: 7}}
	if err := w.Work(context.Background(), job); err != nil || len(f.tokens) != 1 || f.tokens[0] != "7" {
		t.Fatalf("用任务编号作落库标识：%v %v", f.tokens, err)
	}
	f.err = errors.New("db down")
	if err := w.Work(context.Background(), job); err == nil {
		t.Fatal("落库失败应返回错误，由 River 重试")
	}
	if HotsearchFlushArgs.Kind(HotsearchFlushArgs{}) != "hotsearch_flush" {
		t.Fatal("任务类型名")
	}
}

func TestConfigureOpsWithHotsearch(t *testing.T) {
	cfg := Config()
	before := len(cfg.PeriodicJobs)
	ConfigureOps(cfg, OpsWorkers{Ads: &fakeAdFlusher{}, Hotsearch: &fakeAdFlusher{}})
	if len(cfg.PeriodicJobs) != before+2 {
		t.Fatalf("广告每分钟、热搜每小时各一条周期任务：%d → %d", before, len(cfg.PeriodicJobs))
	}
}

type fakeRanks struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (f *fakeRanks) Recompute(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.err
}

func TestRankRecomputeWorker(t *testing.T) {
	f := &fakeRanks{}
	w := &RankRecomputeWorker{Ranks: f}
	job := &river.Job[RankRecomputeArgs]{JobRow: &rivertype.JobRow{ID: 1}}
	if err := w.Work(context.Background(), job); err != nil || f.calls != 1 {
		t.Fatalf("重算：%d %v", f.calls, err)
	}
	f.err = errors.New("valkey down")
	if err := w.Work(context.Background(), job); err == nil {
		t.Fatal("重算失败应返回错误，由 River 重试")
	}
	if RankRecomputeArgs.Kind(RankRecomputeArgs{}) != "rank_recompute" {
		t.Fatal("任务类型名")
	}
	cfg := Config()
	before := len(cfg.PeriodicJobs)
	ConfigureOps(cfg, OpsWorkers{Ads: &fakeAdFlusher{}, Hotsearch: &fakeAdFlusher{}, Ranks: f})
	if len(cfg.PeriodicJobs) != before+3 {
		t.Fatalf("广告、热搜、榜单各一条周期任务：%d → %d", before, len(cfg.PeriodicJobs))
	}
}

// 「马上重新排名」：API 入队 → worker 执行 → API 在等待时间内看到完成；没有 worker 消费时按「已提交」返回。
func TestRankRecomputeClient_RoundTripThroughRiver(t *testing.T) {
	pool := riverPool(t)
	queue := testQueue(t, pool)
	ranks := &fakeRanks{}
	workers := river.NewWorkers()
	river.AddWorker(workers, &RankRecomputeWorker{Ranks: ranks})
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:            map[string]river.QueueConfig{queue: {MaxWorkers: 1}},
		Workers:           workers,
		FetchCooldown:     20 * time.Millisecond,
		FetchPollInterval: 50 * time.Millisecond,
		TestOnly:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Stop(ctx)
	})

	c := &RankRecomputeClient{River: insertOnly(t, pool), Queue: queue, Poll: 20 * time.Millisecond}
	done, err := c.RecomputeNow(context.Background())
	if err != nil || !done || ranks.calls != 1 {
		t.Fatalf("应在等待时间内算完：%v %v（重算 %d 次）", done, err, ranks.calls)
	}

	// 没有 worker 消费的队列：等到上限后按「已提交、稍后完成」返回，不报错、不撤销
	idle := testQueue(t, pool) + "_idle"
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE queue = $1`, idle) })
	c2 := &RankRecomputeClient{River: insertOnly(t, pool), Queue: idle, Wait: 300 * time.Millisecond, Poll: 20 * time.Millisecond}
	done, err = c2.RecomputeNow(context.Background())
	if err != nil || done {
		t.Fatalf("没算完时 done=false 且不报错：%v %v", done, err)
	}
	var state string
	if err := pool.QueryRow(context.Background(), `SELECT state FROM river_job WHERE queue = $1`, idle).Scan(&state); err != nil || state != "available" {
		t.Fatalf("任务留在队列里等 worker：%q %v", state, err)
	}
}
