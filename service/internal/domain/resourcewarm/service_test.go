package resourcewarm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

// ---------- 替身 ----------

type fakeRepo struct {
	mu sync.Mutex

	jobs     map[int64]*Job
	nextID   int64
	eps      []Episode
	busy     bool // Start 时报 409
	startErr error
	epsErr   error
	getErr   error
	reaped   int64
	stopped  map[int64]bool
	// progress 记录每次写进度的快照
	progress []Counters
	finished []finishCall
}

type finishCall struct {
	id     int64
	status Status
	c      Counters
	errMsg string
}

func newRepo(eps ...Episode) *fakeRepo {
	return &fakeRepo{jobs: map[int64]*Job{}, stopped: map[int64]bool{}, eps: eps}
}

func (f *fakeRepo) Start(_ context.Context, in Input, total int, adminID int64) (Job, error) {
	if f.startErr != nil {
		return Job{}, f.startErr
	}
	if f.busy {
		return Job{}, errs.ErrConflict.WithMessage("已经有一轮预热在跑，先等它跑完或点停止")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	j := Job{
		ID: f.nextID, DramaID: in.DramaID, Mode: in.Mode, SegmentsPerQuality: in.SegmentsPerQuality,
		Concurrency: in.Concurrency, IncludeUnpublished: in.IncludeUnpublished, Status: StatusRunning,
		EpisodesTotal: total, AdminID: adminID, StartedAt: time.Now().Add(-time.Minute),
		HeartbeatAt: time.Now(),
	}
	f.jobs[j.ID] = &j
	return j, nil
}

func (f *fakeRepo) Get(_ context.Context, id int64) (Job, error) {
	if f.getErr != nil {
		return Job{}, f.getErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok {
		return Job{}, errs.ErrNotFound.WithMessage("预热任务不存在")
	}
	return *j, nil
}

func (f *fakeRepo) Running(context.Context) (Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, j := range f.jobs {
		if j.Status == StatusRunning {
			return *j, nil
		}
	}
	return Job{}, errs.ErrNotFound
}

func (f *fakeRepo) Latest(context.Context) (Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var best *Job
	for _, j := range f.jobs {
		if best == nil || j.ID > best.ID {
			best = j
		}
	}
	if best == nil {
		return Job{}, errs.ErrNotFound
	}
	return *best, nil
}

func (f *fakeRepo) History(_ context.Context, limit int) ([]Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Job
	for _, j := range f.jobs {
		out = append(out, *j)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeRepo) UpdateProgress(_ context.Context, id int64, c Counters) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.progress = append(f.progress, c)
	if j, ok := f.jobs[id]; ok {
		j.EpisodesDone, j.Warmed, j.Failed = c.EpisodesDone, c.Warmed, c.Failed
		j.Fetched, j.TrafficBytes, j.Current = c.Fetched, c.TrafficBytes, c.Current
	}
	return nil
}

func (f *fakeRepo) Finish(_ context.Context, id int64, status Status, c Counters, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = append(f.finished, finishCall{id, status, c, errMsg})
	if j, ok := f.jobs[id]; ok {
		j.Status = status
		j.Error = errMsg
		now := time.Now()
		j.FinishedAt = &now
	}
	return nil
}

func (f *fakeRepo) RequestStop(_ context.Context, id int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok || j.Status != StatusRunning {
		return false, nil
	}
	f.stopped[id] = true
	j.StopRequested = true
	return true, nil
}

func (f *fakeRepo) StopRequested(_ context.Context, id int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped[id], nil
}

func (f *fakeRepo) ReapStale(_ context.Context, _ time.Time) (int64, error) {
	f.reaped++
	return 0, nil
}

func (f *fakeRepo) Episodes(context.Context, int64, bool) ([]Episode, error) {
	if f.epsErr != nil {
		return nil, f.epsErr
	}
	return f.eps, nil
}

type fakeLists struct {
	mu        sync.Mutex
	master    map[int64][]byte
	variants  map[string][]byte
	masterErr error
	varErr    error
	calls     []string
}

func (f *fakeLists) Master(_ context.Context, epID int64) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("master:%d", epID))
	f.mu.Unlock()
	if f.masterErr != nil {
		return nil, f.masterErr
	}
	if b, ok := f.master[epID]; ok {
		return b, nil
	}
	return nil, errs.ErrNotFound
}

func (f *fakeLists) Variant(_ context.Context, epID int64, file string) ([]byte, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf("variant:%d:%s", epID, file))
	f.mu.Unlock()
	if f.varErr != nil {
		return nil, f.varErr
	}
	if b, ok := f.variants[fmt.Sprintf("%d:%s", epID, file)]; ok {
		return b, nil
	}
	return nil, errs.ErrNotFound
}

type fakeFetcher struct {
	mu     sync.Mutex
	got    []string
	byFrag map[string]FetchResult
	deflt  FetchResult
}

func (f *fakeFetcher) Fetch(_ context.Context, url string) FetchResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, url)
	for frag, r := range f.byFrag {
		if strings.Contains(url, frag) {
			return r
		}
	}
	return f.deflt
}

func (f *fakeFetcher) urls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

type fakeChecks struct {
	mu   sync.Mutex
	rows []string
}

func (f *fakeChecks) RecordWarmFailure(_ context.Context, dramaID, episodeID int64, url string, _ int64, res FetchResult) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, fmt.Sprintf("d%d/e%d/%s/%s", dramaID, episodeID, url, res.Message))
	return nil
}

type fakeQueue struct {
	ids []int64
	err error
}

func (f *fakeQueue) EnqueueResourceWarm(_ context.Context, id int64) error {
	if f.err != nil {
		return f.err
	}
	f.ids = append(f.ids, id)
	return nil
}

// ---------- 夹具 ----------

const masterTwo = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-STREAM-INF:BANDWIDTH=520000,CODECS="avc1.64001e",RESOLUTION=360x640
v360.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1900000,CODECS="avc1.64001f",RESOLUTION=720x1280
v720.m3u8
`

// variantSigned 是改写后的清单：init 与切片都是带签名的绝对地址，密钥是相对地址。
const variantSigned = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-TARGETDURATION:3
#EXT-X-MAP:URI="https://cdn.example.com/hls/1/j/s/init.mp4?exp=1&s=SIG"
#EXT-X-KEY:METHOD=AES-128,URI="/api/v1/hls/key/1/3?t=TOKEN",IV=0x01
#EXTINF:2.002000,
https://cdn.example.com/hls/1/j/s/a.m4s?exp=1&s=SIG
#EXT-X-KEY:METHOD=AES-128,URI="/api/v1/hls/key/1/3?t=TOKEN",IV=0x02
#EXTINF:2.002000,
https://cdn.example.com/hls/1/j/s/b.m4s?exp=1&s=SIG
#EXT-X-KEY:METHOD=AES-128,URI="/api/v1/hls/key/1/3?t=TOKEN",IV=0x03
#EXTINF:2.002000,
https://cdn.example.com/hls/1/j/s/c.m4s?exp=1&s=SIG
#EXT-X-ENDLIST
`

func newSvc(repo *fakeRepo, lists *fakeLists, fetch *fakeFetcher, checks *fakeChecks, queue *fakeQueue) *Service {
	s := NewService(repo, lists, fetch, checks, queue)
	s.now = func() time.Time { return time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC) }
	return s
}

// ---------- 入参校验 ----------

func TestCleanInputDefaults(t *testing.T) {
	got, err := cleanInput(Input{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != ModeWithSegments {
		t.Errorf("默认该连切片一起拉，得到 %s", got.Mode)
	}
	if DefaultSegmentsPerQuality != 2 || got.SegmentsPerQuality != 2 {
		t.Errorf("默认片数应为 2（约 8 秒），得到常量 %d、入参 %d", DefaultSegmentsPerQuality, got.SegmentsPerQuality)
	}
	if got.Concurrency != DefaultConcurrency {
		t.Errorf("默认并发该是 %d，得到 %d", DefaultConcurrency, got.Concurrency)
	}
}

func TestCleanInputValidation(t *testing.T) {
	cases := []struct {
		name string
		in   Input
	}{
		{"负剧编号", Input{DramaID: -1}},
		{"未知模式", Input{Mode: "deep"}},
		{"片数为负", Input{SegmentsPerQuality: -1}},
		{"片数超上限", Input{SegmentsPerQuality: maxSegmentsPerQuality + 1}},
		{"并发为负", Input{Concurrency: -1}},
		{"并发超上限", Input{Concurrency: maxConcurrency + 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := cleanInput(c.in); !errors.Is(err, errs.ErrBadRequest) {
				t.Fatalf("想要 400，得到 %v", err)
			}
		})
	}
}

func TestCleanInputAcceptsBoundaries(t *testing.T) {
	got, err := cleanInput(Input{Mode: ModePlaylistOnly, SegmentsPerQuality: maxSegmentsPerQuality, Concurrency: maxConcurrency})
	if err != nil {
		t.Fatal(err)
	}
	if got.SegmentsPerQuality != maxSegmentsPerQuality || got.Concurrency != maxConcurrency {
		t.Fatalf("边界值该被接受，得到 %+v", got)
	}
}

// ---------- 清单解析 ----------

func TestVariantsOf(t *testing.T) {
	got := variantsOf([]byte(masterTwo))
	if len(got) != 2 || got[0] != "v360.m3u8" || got[1] != "v720.m3u8" {
		t.Fatalf("该按清单顺序取出两档，得到 %+v", got)
	}
}

func TestVariantsOfIgnoresJunk(t *testing.T) {
	master := `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=1
v360.m3u8
v360.m3u8
master.m3u8
../etc/passwd
s/a.m4s
notavariant.txt
v99999.m3u8
`
	got := variantsOf([]byte(master))
	if len(got) != 2 || got[0] != "v360.m3u8" || got[1] != "v99999.m3u8" {
		t.Fatalf("只认 v{数字}.m3u8 且去重，得到 %+v", got)
	}
}

func TestVariantsOfEmpty(t *testing.T) {
	if got := variantsOf([]byte("#EXTM3U\n")); len(got) != 0 {
		t.Fatalf("没有档位时该为空，得到 %+v", got)
	}
}

func TestSegmentsOf(t *testing.T) {
	// 要前 2 片：init 段 + 2 片 = 3 个地址（init 不占片数）
	got := segmentsOf([]byte(variantSigned), 2)
	if len(got) != 3 {
		t.Fatalf("想要 3 个地址（init + 2 片），得到 %d：%+v", len(got), got)
	}
	if !strings.Contains(got[0], "init.mp4") {
		t.Errorf("第一个该是 init 段，得到 %s", got[0])
	}
	if !strings.Contains(got[1], "a.m4s") || !strings.Contains(got[2], "b.m4s") {
		t.Errorf("该按顺序取前两片，得到 %+v", got[1:])
	}
	for _, u := range got {
		if strings.Contains(u, "/api/v1/hls/key/") {
			t.Errorf("密钥地址不该被当成切片拉：%s", u)
		}
	}
}

func TestSegmentsOfTakesAllWhenFewer(t *testing.T) {
	// 要 10 片但只有 3 片：全取
	got := segmentsOf([]byte(variantSigned), 10)
	if len(got) != 4 {
		t.Fatalf("想要 4 个地址（init + 3 片），得到 %d", len(got))
	}
}

func TestSegmentsOfSkipsRelativeURIs(t *testing.T) {
	// 没改写过的清单（切片是相对路径）：一个都不该拉，否则会拿相对路径去发请求
	raw := `#EXTM3U
#EXT-X-MAP:URI="s/init.mp4"
#EXT-X-KEY:METHOD=AES-128,URI="xsite-key:3",IV=0x01
#EXTINF:2.0,
s/a.m4s
`
	if got := segmentsOf([]byte(raw), 3); len(got) != 0 {
		t.Fatalf("相对地址不该被拉，得到 %+v", got)
	}
}

// ---------- 开始 ----------

func TestStart(t *testing.T) {
	repo := newRepo(Episode{ID: 1, DramaID: 7, Number: 1, DramaTitle: "剧"})
	queue := &fakeQueue{}
	svc := newSvc(repo, &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, queue)

	job, err := svc.Start(context.Background(), Input{DramaID: 7}, 42)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusRunning || job.EpisodesTotal != 1 {
		t.Fatalf("开出来的任务不对：%+v", job)
	}
	if job.StatusTitle != "预热中" || job.ModeTitle == "" {
		t.Errorf("该补上中文名：%+v", job)
	}
	if job.AdminID != 42 {
		t.Errorf("该记下发起人，得到 %d", job.AdminID)
	}
	if len(queue.ids) != 1 || queue.ids[0] != job.ID {
		t.Fatalf("该入队这一轮，得到 %+v", queue.ids)
	}
	if repo.reaped == 0 {
		t.Error("开始前该先清一遍心跳超时的僵尸任务")
	}
}

func TestStartRejectsSecondRound(t *testing.T) {
	repo := newRepo(Episode{ID: 1})
	repo.busy = true
	svc := newSvc(repo, &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	_, err := svc.Start(context.Background(), Input{}, 1)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("已有一轮在跑时该报 409，得到 %v", err)
	}
}

func TestStartRejectsEmptyScope(t *testing.T) {
	svc := newSvc(newRepo(), &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	_, err := svc.Start(context.Background(), Input{}, 1)
	if !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("没有可预热的分集时该报 400，得到 %v", err)
	}
	if !strings.Contains(errs.From(err).Message, "含未上架") {
		t.Errorf("该提示可以勾选含未上架，得到 %q", errs.From(err).Message)
	}
}

func TestStartScopeHints(t *testing.T) {
	svc := newSvc(newRepo(), &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	// 指定某部剧时提示该剧没产物，而不是让他去勾「含未上架」
	_, err := svc.Start(context.Background(), Input{DramaID: 9}, 1)
	if msg := errs.From(err).Message; !strings.Contains(msg, "这部剧") {
		t.Errorf("指定剧时的提示该说这部剧，得到 %q", msg)
	}
	// 已经勾了含未上架还是空的：不该再提示去勾它
	_, err = svc.Start(context.Background(), Input{IncludeUnpublished: true}, 1)
	if msg := errs.From(err).Message; strings.Contains(msg, "可以勾选") {
		t.Errorf("已经勾了就不该再提示勾它，得到 %q", msg)
	}
}

func TestStartRollsBackOnEnqueueFailure(t *testing.T) {
	repo := newRepo(Episode{ID: 1})
	svc := newSvc(repo, &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{err: errors.New("队列挂了")})
	if _, err := svc.Start(context.Background(), Input{}, 1); err == nil {
		t.Fatal("入队失败该报错")
	}
	// 否则这一行会一直挂在 running，挡住以后所有的预热
	if len(repo.finished) != 1 || repo.finished[0].status != StatusInterrupted {
		t.Fatalf("入队失败该把任务收成中断，得到 %+v", repo.finished)
	}
}

// ---------- 停止 ----------

func TestStop(t *testing.T) {
	repo := newRepo(Episode{ID: 1})
	svc := newSvc(repo, &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{}, 1)

	got, err := svc.Stop(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.StopRequested {
		t.Error("该设上停止信号")
	}
	// 状态仍是 running：worker 处理完当前这一集才收尾
	if got.Status != StatusRunning {
		t.Errorf("点停止时还不该直接改状态，得到 %s", got.Status)
	}
}

func TestStopAlreadyFinished(t *testing.T) {
	repo := newRepo(Episode{ID: 1})
	svc := newSvc(repo, &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{}, 1)
	_ = repo.Finish(context.Background(), job.ID, StatusDone, Counters{}, "")

	if _, err := svc.Stop(context.Background(), job.ID); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("已结束的该报 409，得到 %v", err)
	}
}

func TestStopBadID(t *testing.T) {
	svc := newSvc(newRepo(), &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	if _, err := svc.Stop(context.Background(), 0); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("编号为 0 该报 400，得到 %v", err)
	}
}

// ---------- 进度 ----------

func TestCurrentEmpty(t *testing.T) {
	svc := newSvc(newRepo(), &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	out, err := svc.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Job != nil || out.Running {
		t.Fatalf("从来没跑过时该是空的，得到 %+v", out)
	}
	if len(out.Statuses) == 0 || out.Note == "" {
		t.Error("该带上状态说明与预热效果说明")
	}
}

func TestCurrentFallsBackToLatest(t *testing.T) {
	repo := newRepo(Episode{ID: 1})
	svc := newSvc(repo, &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{}, 1)
	_ = repo.Finish(context.Background(), job.ID, StatusDone, Counters{EpisodesDone: 1, Warmed: 3}, "")

	out, err := svc.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 没有在跑的时候给最近一轮，让运营看到上次的结果
	if out.Job == nil || out.Running {
		t.Fatalf("该给出最近一轮且 running 为假，得到 %+v", out)
	}
	if out.Job.Status != StatusDone || out.Job.StatusTitle != "已完成" {
		t.Errorf("状态不对：%+v", out.Job)
	}
}

func TestCurrentReapsStale(t *testing.T) {
	repo := newRepo()
	svc := newSvc(repo, &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	if _, err := svc.Current(context.Background()); err != nil {
		t.Fatal(err)
	}
	// 运营盯着看的时候，没人管的 running 不该一直显示「预热中」
	if repo.reaped == 0 {
		t.Error("看进度时该顺手清一遍心跳超时的任务")
	}
}

func TestFillDerivedFields(t *testing.T) {
	svc := newSvc(newRepo(), &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	start := svc.now().Add(-90 * time.Second)
	got := svc.fill(Job{
		Mode: ModeWithSegments, Status: StatusRunning,
		EpisodesTotal: 8, EpisodesDone: 2, StartedAt: start,
	})
	if got.Percent != 25 {
		t.Errorf("2/8 该是 25%%，得到 %d", got.Percent)
	}
	if got.ElapsedMs != 90000 {
		t.Errorf("已跑 90 秒，得到 %d 毫秒", got.ElapsedMs)
	}
	if got.ModeTitle != "连切片一起拉" || got.StatusTitle != "预热中" {
		t.Errorf("中文名不对：%+v", got)
	}
}

func TestFillUsesFinishedAt(t *testing.T) {
	svc := newSvc(newRepo(), &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	start := svc.now().Add(-10 * time.Minute)
	end := start.Add(2 * time.Minute)
	got := svc.fill(Job{StartedAt: start, FinishedAt: &end})
	// 跑完的任务用结束时刻算时长，不能跟着当前时间一直涨
	if got.ElapsedMs != 120000 {
		t.Fatalf("该是 2 分钟，得到 %d 毫秒", got.ElapsedMs)
	}
}

func TestFillPercentCaps(t *testing.T) {
	svc := newSvc(newRepo(), &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	if got := svc.fill(Job{EpisodesTotal: 2, EpisodesDone: 5}); got.Percent != 100 {
		t.Fatalf("百分比该封顶 100，得到 %d", got.Percent)
	}
	if got := svc.fill(Job{EpisodesTotal: 0, EpisodesDone: 3}); got.Percent != 0 {
		t.Fatalf("没有总数时该是 0，得到 %d", got.Percent)
	}
}

// ---------- 执行 ----------

func warmFixture() (*fakeRepo, *fakeLists, *fakeFetcher, *fakeChecks) {
	repo := newRepo(Episode{ID: 1, DramaID: 7, Number: 1, DramaTitle: "测试剧"})
	lists := &fakeLists{
		master: map[int64][]byte{1: []byte(masterTwo)},
		variants: map[string][]byte{
			"1:v360.m3u8": []byte(variantSigned),
			"1:v720.m3u8": []byte(variantSigned),
		},
	}
	fetch := &fakeFetcher{deflt: FetchResult{HTTPStatus: 200, Bytes: 1024, Message: "已拉取 1024 字节"}}
	return repo, lists, fetch, &fakeChecks{}
}

func TestRunWithSegments(t *testing.T) {
	repo, lists, fetch, checks := warmFixture()
	svc := newSvc(repo, lists, fetch, checks, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{Mode: ModeWithSegments, SegmentsPerQuality: 2}, 1)

	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if len(repo.finished) != 1 || repo.finished[0].status != StatusDone {
		t.Fatalf("该正常跑完，得到 %+v", repo.finished)
	}
	c := repo.finished[0].c
	// 两档 × (init + 2 片) = 6 次拉取
	if c.Fetched != 6 {
		t.Errorf("该拉 6 次，得到 %d", c.Fetched)
	}
	if c.TrafficBytes != 6*1024 {
		t.Errorf("流量该是 6KB，得到 %d", c.TrafficBytes)
	}
	// 1 主清单 + 2 档清单 + 6 个切片 = 9 个资源热好
	if c.Warmed != 9 {
		t.Errorf("该热好 9 个资源，得到 %d", c.Warmed)
	}
	if c.Failed != 0 {
		t.Errorf("不该有失败，得到 %d", c.Failed)
	}
	if c.EpisodesDone != 1 {
		t.Errorf("该跑完 1 集，得到 %d", c.EpisodesDone)
	}
	// 拉的必须是清单里那些带签名的绝对地址
	for _, u := range fetch.urls() {
		if !strings.HasPrefix(u, "https://cdn.example.com/") {
			t.Errorf("拉的地址该来自改写后的清单，得到 %s", u)
		}
	}
}

func TestRunPlaylistOnly(t *testing.T) {
	repo, lists, fetch, checks := warmFixture()
	svc := newSvc(repo, lists, fetch, checks, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{Mode: ModePlaylistOnly}, 1)

	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if n := len(fetch.urls()); n != 0 {
		t.Fatalf("只拉清单的模式不该去拉切片，却拉了 %d 个", n)
	}
	c := repo.finished[0].c
	// 1 主清单 + 2 档清单
	if c.Warmed != 3 || c.Fetched != 0 {
		t.Fatalf("只该热 3 个清单、0 次切片拉取，得到 warmed=%d fetched=%d", c.Warmed, c.Fetched)
	}
	// 清单还是取到了（热的是服务端清单原文缓存）
	if len(lists.calls) != 3 {
		t.Errorf("该取 1 主清单 + 2 档清单，实际 %+v", lists.calls)
	}
}

func TestRunRecordsFailures(t *testing.T) {
	repo, lists, fetch, checks := warmFixture()
	fetch.byFrag = map[string]FetchResult{
		"b.m4s": {HTTPStatus: 404, Message: "源站返回 HTTP 404"},
	}
	svc := newSvc(repo, lists, fetch, checks, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{Mode: ModeWithSegments, SegmentsPerQuality: 2}, 1)

	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	c := repo.finished[0].c
	// 两档各有一个 b.m4s 拿不到
	if c.Failed != 2 {
		t.Errorf("该有 2 个失败，得到 %d", c.Failed)
	}
	if len(checks.rows) != 2 {
		t.Fatalf("失败的该记进体检结果，得到 %+v", checks.rows)
	}
	for _, r := range checks.rows {
		if !strings.Contains(r, "d7/e1/") || !strings.Contains(r, "404") {
			t.Errorf("记录内容不对：%s", r)
		}
	}
	// 失败不影响整轮的完成状态
	if repo.finished[0].status != StatusDone {
		t.Errorf("个别资源拿不到仍算跑完，得到 %s", repo.finished[0].status)
	}
}

func TestRunMasterFailureSkipsEpisode(t *testing.T) {
	repo, lists, fetch, checks := warmFixture()
	lists.masterErr = errs.ErrUnavailable.WithMessage("线路暂不可用，请切换线路")
	svc := newSvc(repo, lists, fetch, checks, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{}, 1)

	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	c := repo.finished[0].c
	if c.Failed != 1 || c.Warmed != 0 {
		t.Fatalf("取不到主清单该记一次失败且不往下热，得到 warmed=%d failed=%d", c.Warmed, c.Failed)
	}
	if len(fetch.urls()) != 0 {
		t.Error("取不到主清单就不该再拉切片")
	}
	if len(checks.rows) != 1 || !strings.Contains(checks.rows[0], "取主清单失败") {
		t.Errorf("该记下原因，得到 %+v", checks.rows)
	}
}

func TestRunVariantFailureContinues(t *testing.T) {
	repo, lists, fetch, checks := warmFixture()
	// 只有 v720 的清单取不到，v360 应该照常热
	delete(lists.variants, "1:v720.m3u8")
	svc := newSvc(repo, lists, fetch, checks, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{Mode: ModeWithSegments, SegmentsPerQuality: 1}, 1)

	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	c := repo.finished[0].c
	// 1 主清单 + 1 档清单 + (init + 1 片) = 4 热好；v720 清单算 1 次失败
	if c.Warmed != 4 || c.Failed != 1 {
		t.Fatalf("一档失败不该拖垮另一档，得到 warmed=%d failed=%d", c.Warmed, c.Failed)
	}
}

func TestRunStopsOnSignal(t *testing.T) {
	eps := make([]Episode, 20)
	for i := range eps {
		eps[i] = Episode{ID: int64(i + 1), DramaID: 7, Number: int32(i + 1), DramaTitle: "剧"}
	}
	repo := newRepo(eps...)
	lists := &fakeLists{master: map[int64][]byte{}, variants: map[string][]byte{}}
	for _, e := range eps {
		lists.master[e.ID] = []byte(masterTwo)
		lists.variants[fmt.Sprintf("%d:v360.m3u8", e.ID)] = []byte(variantSigned)
		lists.variants[fmt.Sprintf("%d:v720.m3u8", e.ID)] = []byte(variantSigned)
	}
	svc := newSvc(repo, lists, &fakeFetcher{deflt: FetchResult{HTTPStatus: 200, Bytes: 1}}, &fakeChecks{}, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{Concurrency: 1, SegmentsPerQuality: 1}, 1)

	// 立刻请求停止：第一集之后就该收手
	if _, err := svc.Stop(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if len(repo.finished) != 1 || repo.finished[0].status != StatusStopped {
		t.Fatalf("该记成已停止，得到 %+v", repo.finished)
	}
	// 已经热好的算数，但远没跑完 20 集
	if done := repo.finished[0].c.EpisodesDone; done >= len(eps) {
		t.Errorf("停止后不该把 20 集都跑完，得到 %d", done)
	}
}

func TestRunSkipsNonRunningJob(t *testing.T) {
	repo, lists, fetch, checks := warmFixture()
	svc := newSvc(repo, lists, fetch, checks, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{}, 1)
	_ = repo.Finish(context.Background(), job.ID, StatusStopped, Counters{}, "")

	// River 重试时会遇到：任务已经被停掉或判定中断了，不该再跑一遍
	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if len(lists.calls) != 0 {
		t.Fatalf("不该再取清单，实际 %+v", lists.calls)
	}
}

func TestRunMissingJob(t *testing.T) {
	_, lists, fetch, checks := warmFixture()
	svc := newSvc(newRepo(), lists, fetch, checks, &fakeQueue{})
	if err := svc.Run(context.Background(), 999); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("任务不存在该返回 NotFound，得到 %v", err)
	}
}

func TestRunWithoutRunnerDeps(t *testing.T) {
	repo := newRepo(Episode{ID: 1})
	// API 进程里装的是控制面服务：误调 Run 该给一句说得清的错误，而不是空指针
	svc := NewControlService(repo, &fakeQueue{})
	if err := svc.Run(context.Background(), 1); !errors.Is(err, errNoRunner) {
		t.Fatalf("想要 errNoRunner，得到 %v", err)
	}
}

func TestRunWritesProgress(t *testing.T) {
	repo, lists, fetch, checks := warmFixture()
	svc := newSvc(repo, lists, fetch, checks, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{SegmentsPerQuality: 1}, 1)

	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	if len(repo.progress) == 0 {
		t.Fatal("跑的过程中该写进度（兼心跳）")
	}
	last := repo.progress[len(repo.progress)-1]
	if last.Current == "" {
		t.Error("该写上当前在热什么")
	}
	if !strings.Contains(last.Current, "测试剧") {
		t.Errorf("当前进展该带剧名，得到 %q", last.Current)
	}
}

func TestRunFinishesEvenWhenCtxCancelled(t *testing.T) {
	repo, lists, fetch, checks := warmFixture()
	svc := newSvc(repo, lists, fetch, checks, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{}, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = svc.Run(ctx, job.ID)
	// ctx 取消了状态也必须落库，否则这一行会一直挡着下一轮
	if len(repo.finished) != 1 {
		t.Fatalf("该收尾，得到 %+v", repo.finished)
	}
	if repo.finished[0].status != StatusInterrupted {
		t.Errorf("worker 退出该记成中断，得到 %s", repo.finished[0].status)
	}
}

// ---------- 其他 ----------

func TestFetchResultOK(t *testing.T) {
	cases := []struct {
		name string
		in   FetchResult
		want bool
	}{
		{"200", FetchResult{HTTPStatus: 200}, true},
		{"206", FetchResult{HTTPStatus: 206}, true},
		{"301", FetchResult{HTTPStatus: 301}, true},
		{"404", FetchResult{HTTPStatus: 404}, false},
		{"500", FetchResult{HTTPStatus: 500}, false},
		{"连不上", FetchResult{Unreachable: true}, false},
		{"连不上但有状态码", FetchResult{Unreachable: true, HTTPStatus: 200}, false},
		{"没发出请求", FetchResult{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.OK(); got != c.want {
				t.Fatalf("OK() = %v，想要 %v", got, c.want)
			}
		})
	}
}

func TestStatusesAllDescribed(t *testing.T) {
	all := []Status{StatusRunning, StatusDone, StatusStopped, StatusInterrupted}
	infos := Statuses()
	if len(infos) != len(all) {
		t.Fatalf("状态说明 %d 条，枚举 %d 个", len(infos), len(all))
	}
	for _, s := range all {
		found := false
		for _, i := range infos {
			if i.Status == s {
				found = true
				if i.Title == "" || i.Desc == "" {
					t.Errorf("%s 缺中文名或说明", s)
				}
			}
		}
		if !found {
			t.Errorf("状态 %s 没出现在说明里", s)
		}
		if StatusTitle(s) == string(s) {
			t.Errorf("状态 %s 没有中文名", s)
		}
	}
}

func TestModeTitles(t *testing.T) {
	for _, m := range []Mode{ModePlaylistOnly, ModeWithSegments} {
		if ModeTitle(m) == string(m) {
			t.Errorf("模式 %s 没有中文名", m)
		}
	}
	if got := ModeTitle("x"); got != "x" {
		t.Fatalf("未知模式该原样返回，得到 %q", got)
	}
}

func TestJobRunning(t *testing.T) {
	if !(Job{Status: StatusRunning}).Running() {
		t.Error("running 该是在跑")
	}
	for _, s := range []Status{StatusDone, StatusStopped, StatusInterrupted} {
		if (Job{Status: s}).Running() {
			t.Errorf("%s 不该算在跑", s)
		}
	}
}

func TestHistory(t *testing.T) {
	repo := newRepo(Episode{ID: 1})
	svc := newSvc(repo, &fakeLists{}, &fakeFetcher{}, &fakeChecks{}, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{}, 1)
	_ = repo.Finish(context.Background(), job.ID, StatusDone, Counters{}, "")

	got, err := svc.History(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].StatusTitle == "" {
		t.Fatalf("历史记录该带中文名，得到 %+v", got)
	}
}

func TestWarmNoteExplainsBothLayers(t *testing.T) {
	// 这段说明是为了避免运营误以为清单也进了 CDN。它写死在后端，前端照抄一份必然漂移
	if !strings.Contains(warmNote, "切片") || !strings.Contains(warmNote, "清单") {
		t.Fatalf("预热效果说明该把两层都讲清楚，得到 %q", warmNote)
	}
}

const variantSignedTS = `#EXTM3U
#EXT-X-VERSION:4
#EXT-X-TARGETDURATION:4
#EXT-X-KEY:METHOD=AES-128,URI="/api/v1/hls/key/1/3?t=TOKEN",IV=0x01
#EXTINF:4.000000,
https://cdn.example.com/hls/1/j/s/a.ts?exp=1&s=SIG
#EXT-X-KEY:METHOD=AES-128,URI="/api/v1/hls/key/1/3?t=TOKEN",IV=0x02
#EXTINF:4.000000,
https://cdn.example.com/hls/1/j/s/b.ts?exp=1&s=SIG
#EXT-X-KEY:METHOD=AES-128,URI="/api/v1/hls/key/1/3?t=TOKEN",IV=0x03
#EXTINF:4.000000,
https://cdn.example.com/hls/1/j/s/c.ts?exp=1&s=SIG
#EXT-X-ENDLIST
`

func TestSegmentsOfMPEGTSTakesExactlyRequestedSegments(t *testing.T) {
	got := segmentsOf([]byte(variantSignedTS), 2)
	if len(got) != 2 || !strings.Contains(got[0], "a.ts") || !strings.Contains(got[1], "b.ts") {
		t.Fatalf("MPEG-TS 应只取前两个切片，得到 %+v", got)
	}
}

func TestRunWithMPEGTSSegments(t *testing.T) {
	repo, lists, fetch, checks := warmFixture()
	lists.variants["1:v360.m3u8"] = []byte(variantSignedTS)
	lists.variants["1:v720.m3u8"] = []byte(variantSignedTS)
	svc := newSvc(repo, lists, fetch, checks, &fakeQueue{})
	job, _ := svc.Start(context.Background(), Input{Mode: ModeWithSegments, SegmentsPerQuality: 2}, 1)

	if err := svc.Run(context.Background(), job.ID); err != nil {
		t.Fatal(err)
	}
	c := repo.finished[0].c
	// 两档 × 两片；MPEG-TS 没有 init 段，不能再多拉一片。
	if c.Fetched != 4 || c.Warmed != 7 {
		t.Fatalf("MPEG-TS 预热计数不对：fetched=%d warmed=%d", c.Fetched, c.Warmed)
	}
}
