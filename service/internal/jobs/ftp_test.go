package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"

	"xsite/service/internal/domain/resource"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg"
)

// 集成测试：API 侧入队 → worker 执行 → 读到结果，走真实 PostgreSQL 上的 River。
// 每个测试用独立队列，避免被本机 compose 里正在运行的 worker 取走。

func riverPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("未设置 DATABASE_URL，跳过 River 集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pg.NewPool(ctx, url)
	if err != nil {
		t.Skipf("数据库不可用，跳过：%v", err)
	}
	t.Cleanup(pool.Close)
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		t.Fatalf("River 迁移失败：%v", err)
	}
	return pool
}

// testQueue 返回本测试独用的队列名，结束后清掉队列中的任务与队列记录。
func testQueue(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	q := fmt.Sprintf("t_ftp_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, `DELETE FROM river_job WHERE queue = $1`, q)
		_, _ = pool.Exec(ctx, `DELETE FROM river_queue WHERE name = $1`, q)
	})
	return q
}

// fakeFTPProber 记录收到的请求；block 为 true 时一直等到 ctx 结束（模拟卡住的资源机）。
type fakeFTPProber struct {
	mu       sync.Mutex
	got      []resource.ProbeRequest
	block    bool
	canceled chan struct{}
}

func (f *fakeFTPProber) Probe(ctx context.Context, req resource.ProbeRequest) resource.ProbeResult {
	f.mu.Lock()
	f.got = append(f.got, req)
	f.mu.Unlock()
	if f.block {
		<-ctx.Done()
		close(f.canceled)
		return resource.ProbeResult{}
	}
	at := time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)
	return resource.ProbeResult{
		OK: true, ElapsedMs: 42, Path: "/剧集",
		Steps:   []resource.Step{{Name: resource.StepResolve, OK: true, Ms: 1, Message: "解析到 203.0.113.9"}, {Name: resource.StepList, OK: true, Ms: 9, Message: "目录 /剧集 共 2 项"}},
		Entries: []resource.Entry{{Name: "霸道总裁", Type: resource.EntryDir, ModifiedAt: &at}, {Name: "a.mp4", Type: resource.EntryFile, Size: 1024}},
	}
}

// startWorker 启动只消费 queue 的 River 客户端（相当于 worker 进程）。
func startWorker(t *testing.T, pool *pgxpool.Pool, queue string, prober FTPProber) {
	t.Helper()
	workers := river.NewWorkers()
	river.AddWorker(workers, &FTPProbeWorker{Prober: prober})
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues:            map[string]river.QueueConfig{queue: {MaxWorkers: 2}},
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
}

// insertOnly 是 API 进程里的 River 客户端：只入队、不消费。
func insertOnly(t *testing.T, pool *pgxpool.Pool) *river.Client[pgx.Tx] {
	t.Helper()
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestFTPProbe_RoundTripThroughRiver(t *testing.T) {
	pool := riverPool(t)
	queue := testQueue(t, pool)
	prober := &fakeFTPProber{}
	startWorker(t, pool, queue, prober)

	c := &FTPProbeClient{River: insertOnly(t, pool), Queue: queue, Poll: 20 * time.Millisecond}
	srv := resource.FTPServer{ID: 9, Name: "不随任务传递", Host: "ftp.example.com", Port: 2121, TLS: resource.TLSExplicit, Passive: true, CredRef: "HK1"}
	res, err := c.Probe(context.Background(), resource.ProbeRequest{Op: resource.ProbeList, Server: srv, Path: "/剧集"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.ElapsedMs != 42 || res.Path != "/剧集" || len(res.Steps) != 2 || res.Steps[1].Message != "目录 /剧集 共 2 项" ||
		len(res.Entries) != 2 || res.Entries[0].ModifiedAt == nil || !res.Entries[0].ModifiedAt.Equal(time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)) ||
		res.Entries[1].Size != 1024 || res.Entries[1].ModifiedAt != nil {
		t.Fatalf("结果应原样回到 API：%+v", res)
	}
	prober.mu.Lock()
	defer prober.mu.Unlock()
	want := srv
	want.Name = "" // 名称与连接无关，不放进任务参数
	if len(prober.got) != 1 || prober.got[0].Server != want || prober.got[0].Op != resource.ProbeList || prober.got[0].Path != "/剧集" {
		t.Fatalf("worker 应收到发起时的资源机配置：%+v", prober.got)
	}
}

func TestFTPProbe_TimeoutCancelsQueuedJob(t *testing.T) {
	pool := riverPool(t)
	client := insertOnly(t, pool)
	// 没有 worker 消费这个队列
	c := &FTPProbeClient{River: client, Queue: testQueue(t, pool), Wait: 300 * time.Millisecond, Poll: 50 * time.Millisecond}
	start := time.Now()
	_, err := c.Probe(context.Background(), resource.ProbeRequest{Op: resource.ProbeTest, Server: resource.FTPServer{ID: 1, Host: "ftp.example.com", Port: 21}})
	if !errors.Is(err, errs.ErrUnavailable) || errs.From(err).Message != errFTPProbeTimeout.Message {
		t.Fatalf("没人处理时应 503 并提示检查 worker，得到 %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("应在等待上限附近返回，实际 %v", time.Since(start))
	}
	jobs, err := client.JobList(context.Background(), river.NewJobListParams().Queues(c.Queue).States(rivertype.JobStateCancelled))
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs.Jobs) != 1 || jobs.Jobs[0].Kind != "ftp_probe" || jobs.Jobs[0].MaxAttempts != 1 {
		t.Fatalf("超时的任务应被撤销（且只试一次）：%+v", jobs.Jobs)
	}
}

func TestFTPProbe_TimeoutInterruptsRunningJob(t *testing.T) {
	pool := riverPool(t)
	queue := testQueue(t, pool)
	prober := &fakeFTPProber{block: true, canceled: make(chan struct{})}
	startWorker(t, pool, queue, prober)

	c := &FTPProbeClient{River: insertOnly(t, pool), Queue: queue, Wait: 700 * time.Millisecond, Poll: 20 * time.Millisecond}
	if _, err := c.Probe(context.Background(), resource.ProbeRequest{Op: resource.ProbeTest, Server: resource.FTPServer{ID: 1, Host: "h", Port: 21}}); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("应超时，得到 %v", err)
	}
	select {
	case <-prober.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("撤销任务后 worker 中的操作应被中断")
	}
}

func TestFTPProbe_CallerGoneCancelsJob(t *testing.T) {
	pool := riverPool(t)
	client := insertOnly(t, pool)
	c := &FTPProbeClient{River: client, Queue: testQueue(t, pool), Poll: 20 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	if _, err := c.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: resource.FTPServer{ID: 1, Host: "h", Port: 21}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("调用方离开时应返回 context.Canceled，得到 %v", err)
	}
	jobs, err := client.JobList(context.Background(), river.NewJobListParams().Queues(c.Queue).States(rivertype.JobStateCancelled))
	if err != nil || len(jobs.Jobs) != 1 {
		t.Fatalf("调用方离开后任务应被撤销：%v %+v", err, jobs)
	}
}
