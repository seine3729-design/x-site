package resourcewarm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"xsite/service/internal/errs"
)

const (
	// DefaultSegmentsPerQuality 是每个清晰度默认拉几片。
	//
	// 2 片 ≈ 8 秒（切片 4 秒），覆盖起播和 ABR 进入稳定状态前的窗口，同时控制预热流量。
	DefaultSegmentsPerQuality = 2
	maxSegmentsPerQuality     = 20
	// DefaultConcurrency 是默认同时热几集。
	DefaultConcurrency = 4
	maxConcurrency     = 16
	// progressEvery 是每热几集往库里同步一次进度（兼心跳）。
	//
	// 不是每集都写：一轮几百集的话就是几百次 UPDATE。但也不能太稀，心跳太久不动会被判成中断。
	progressEvery = 1
	// StaleAfter 是心跳多久不动就判定这一轮中断了。
	//
	// 比单集预热的最长耗时留足余量：一集最多 6 个地址（3 档 × 2 片），每个 15 秒超时，
	// 最坏 90 秒，再加上并发排队，5 分钟是个不会误判的下限。
	StaleAfter = 5 * time.Minute
	// historyLimit 是历史记录最多返回几条。
	historyLimit = 20
)

// warmNote 是对预热效果的说明。
//
// 写死在后端：哪一层缓存被热到是后端行为决定的，前端照抄一份必然漂移。
const warmNote = "切片走边缘长缓存（按内容哈希、immutable），这是预热的正主；" +
	"清单是加密接口、不进边缘缓存，预热它热的是服务端的清单原文缓存，省掉回源读清单那一跳。"

// Repo 读写预热任务。
type Repo interface {
	// Start 开一轮。已有在跑的一轮时返回 errs.ErrConflict。
	Start(ctx context.Context, in Input, episodesTotal int, adminID int64) (Job, error)
	Get(ctx context.Context, id int64) (Job, error)
	// Running 返回在跑的那一轮；没有时返回 errs.ErrNotFound。
	Running(ctx context.Context) (Job, error)
	// Latest 返回最近一轮（不论状态）；从来没跑过时返回 errs.ErrNotFound。
	Latest(ctx context.Context) (Job, error)
	History(ctx context.Context, limit int) ([]Job, error)
	UpdateProgress(ctx context.Context, id int64, c Counters) error
	Finish(ctx context.Context, id int64, status Status, c Counters, errMsg string) error
	// RequestStop 设置停止信号，返回是否命中一行在跑的任务。
	RequestStop(ctx context.Context, id int64) (bool, error)
	StopRequested(ctx context.Context, id int64) (bool, error)
	// ReapStale 把心跳超时的任务判定为中断，返回处理了几条。
	ReapStale(ctx context.Context, before time.Time) (int64, error)
	// Episodes 列出待预热的分集。
	Episodes(ctx context.Context, dramaID int64, includeUnpublished bool) ([]Episode, error)
}

// Playlists 取一集的清单（play.HLSService，由 worker 侧注入）。
//
// 预热用它而不是自己拼地址：返回的清单里是和观众拿到的同一套签名地址。
type Playlists interface {
	// Master 返回主清单原文。
	Master(ctx context.Context, episodeID int64) ([]byte, error)
	// Variant 返回改写后的某一档清单（切片是线路上的签名地址）。
	Variant(ctx context.Context, episodeID int64, file string) ([]byte, error)
}

// Fetcher 拉一个地址（实现在 store/blob/s3probe）。
type Fetcher interface {
	// Fetch 完整拉取并丢弃内容，返回字节数。预热要的就是「让边缘回源一次」这个副作用。
	Fetch(ctx context.Context, url string) FetchResult
}

// CheckRecorder 把拿不到的资源记进资源体检的结果（domain/resourcecheck）。
//
// 复用体检表而不是另立一张：对运营来说「这个地址拿不到」就是一件事，
// 不该因为是预热发现的还是体检发现的就跑两个页面去看。
type CheckRecorder interface {
	RecordWarmFailure(ctx context.Context, dramaID, episodeID int64, url string, lineID int64, res FetchResult) error
}

// Enqueuer 把一轮预热入队（实现在 internal/jobs）。
type Enqueuer interface {
	EnqueueResourceWarm(ctx context.Context, jobID int64) error
}

// Service 是资源预热。
type Service struct {
	repo   Repo
	lists  Playlists
	fetch  Fetcher
	checks CheckRecorder
	queue  Enqueuer
	now    func() time.Time
	// stale 是心跳超时判定，测试可调。
	stale time.Duration
}

func NewService(repo Repo, lists Playlists, fetch Fetcher, checks CheckRecorder, queue Enqueuer) *Service {
	return &Service{repo: repo, lists: lists, fetch: fetch, checks: checks, queue: queue, now: time.Now, stale: StaleAfter}
}

// NewControlService 组装只做控制面的服务，供 API 进程使用。
//
// API 侧只负责开始、停止与看进度，真正的拉取在 worker，所以不需要清单、拉取器与体检记录器。
// 分开一个构造函数而不是传 nil：传 nil 的话一旦有人在 API 侧误调 Run 就是空指针，
// 这样 Run 会给出一句说得清的错误。
func NewControlService(repo Repo, queue Enqueuer) *Service {
	return &Service{repo: repo, queue: queue, now: time.Now, stale: StaleAfter}
}

// errNoRunner 是在没有执行依赖的进程里调用了 Run。
var errNoRunner = errors.New("这个进程没有装配预热的执行依赖（清单、拉取器），预热只能在 worker 里跑")

// errBusy 是「已经有一轮在跑」。
var errBusy = errs.ErrConflict.WithMessage("已经有一轮预热在跑，先等它跑完或点停止")

// Start 开一轮预热。
//
// 同一时间仅一轮（docs/02 第 4.4 节）：预热会对源站与边缘产生持续流量，两轮并行既压垮源站
// 也让进度与流量统计没法归属。靠库里的部分唯一索引保证，不靠应用层检查 —— 多实例下那是会漏的。
func (s *Service) Start(ctx context.Context, in Input, adminID int64) (Job, error) {
	clean, err := cleanInput(in)
	if err != nil {
		return Job{}, err
	}
	// 先清掉心跳超时的僵尸任务，否则 worker 崩过一次之后谁也开不了新的一轮
	if _, err := s.repo.ReapStale(ctx, s.now().Add(-s.stale)); err != nil {
		return Job{}, err
	}
	eps, err := s.repo.Episodes(ctx, clean.DramaID, clean.IncludeUnpublished)
	if err != nil {
		return Job{}, err
	}
	if len(eps) == 0 {
		return Job{}, badRequest("没有可预热的分集：要有转封装完成的视频产物才能预热，%s", scopeHint(clean))
	}
	job, err := s.repo.Start(ctx, clean, len(eps), adminID)
	if err != nil {
		return Job{}, err
	}
	if err := s.queue.EnqueueResourceWarm(ctx, job.ID); err != nil {
		// 入队失败要把任务收掉，否则这一行会一直挡着下一轮
		_ = s.repo.Finish(ctx, job.ID, StatusInterrupted, Counters{}, "入队失败，没有开始预热")
		return Job{}, fmt.Errorf("入队资源预热: %w", err)
	}
	return s.fill(job), nil
}

// Stop 请求停止。只设信号，worker 处理完当前这一集就收尾 —— 已经热好的算数（docs/06 第 10 节）。
func (s *Service) Stop(ctx context.Context, id int64) (Job, error) {
	if id <= 0 {
		return Job{}, badRequest("任务编号不正确")
	}
	ok, err := s.repo.RequestStop(ctx, id)
	if err != nil {
		return Job{}, err
	}
	if !ok {
		return Job{}, errs.ErrConflict.WithMessage("这一轮已经结束了，不用再停")
	}
	job, err := s.repo.Get(ctx, id)
	if err != nil {
		return Job{}, err
	}
	return s.fill(job), nil
}

// Current 返回当前进度：在跑的那一轮，没有的话给最近一轮（让运营看到上次的结果）。
func (s *Service) Current(ctx context.Context) (Progress, error) {
	// 顺手清僵尸：运营盯着进度看的时候，一个没人管的 running 不该一直显示「预热中」
	if _, err := s.repo.ReapStale(ctx, s.now().Add(-s.stale)); err != nil {
		return Progress{}, err
	}
	out := Progress{Statuses: Statuses(), Note: warmNote}
	job, err := s.repo.Latest(ctx)
	switch {
	case errors.Is(err, errs.ErrNotFound):
		return out, nil
	case err != nil:
		return Progress{}, err
	}
	filled := s.fill(job)
	out.Job = &filled
	out.Running = filled.Running()
	return out, nil
}

// History 返回最近几轮。
func (s *Service) History(ctx context.Context) ([]Job, error) {
	jobs, err := s.repo.History(ctx, historyLimit)
	if err != nil {
		return nil, err
	}
	for i := range jobs {
		jobs[i] = s.fill(jobs[i])
	}
	return jobs, nil
}

// variantRe 是主清单里各档清单的文件名（transcode 生成的 v{短边}.m3u8）。
//
// 自己扫一遍而不是导出 play 包的解析器：预热只需要档位名，不需要那边的签名替换、
// 试看裁剪与密钥占位符处理，为一行正则去改一个已完成功能的包不值得。
var variantRe = regexp.MustCompile(`^v[0-9]{1,5}\.m3u8$`)

// variantsOf 从主清单里挑出各档清单名，顺序与清单一致（码率从低到高）。
func variantsOf(master []byte) []string {
	var out []string
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(master), "\n") {
		v := strings.TrimSpace(line)
		if v == "" || strings.HasPrefix(v, "#") || !variantRe.MatchString(v) || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// segmentRe 匹配改写后清单里 init 段的完整 http(s) 地址。新 MPEG-TS 清单没有 init 段，
// 历史 fMP4 清单仍通过它预热 init 段。
var segmentRe = regexp.MustCompile(`URI="(https?://[^"]+)"`)

// segmentsOf 从改写后的清单里取前 n 个切片及（仅历史 fMP4 需要的）init 段。
func segmentsOf(variant []byte, n int) []string {
	if n <= 0 {
		return nil
	}
	var out []string
	segments := 0
	for line := range strings.SplitSeq(string(variant), "\n") {
		v := strings.TrimSpace(line)
		if v == "" {
			continue
		}
		if strings.HasPrefix(v, "#") {
			// EXT-X-KEY 的 URI 是相对密钥接口，正则只取 http(s) 地址，因此不会被预热。
			if strings.HasPrefix(v, "#EXT-X-MAP:") {
				if m := segmentRe.FindStringSubmatch(v); m != nil {
					out = append(out, m[1])
				}
			}
			continue
		}
		if !strings.HasPrefix(v, "http://") && !strings.HasPrefix(v, "https://") {
			continue
		}
		out = append(out, v)
		segments++
		if segments >= n {
			break
		}
	}
	return out
}

// Run 执行一轮预热。由 worker 调用，跑完才返回。
func (s *Service) Run(ctx context.Context, jobID int64) error {
	if s.lists == nil || s.fetch == nil {
		return errNoRunner
	}
	job, err := s.repo.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status != StatusRunning {
		// 已经被停掉或判定中断了，不要再跑（重试时会遇到）
		slog.InfoContext(ctx, "预热任务已不在运行状态，跳过", "job_id", jobID, "status", job.Status)
		return nil
	}
	eps, err := s.repo.Episodes(ctx, job.DramaID, job.IncludeUnpublished)
	if err != nil {
		return err
	}

	run := &runner{svc: s, job: job, total: len(eps)}
	runErr := run.do(ctx, eps)

	status, errMsg := StatusDone, ""
	switch {
	case ctx.Err() != nil:
		// worker 要关了。这一条要在 run.stopped 之前判断：进程被掐断时整轮并没有跑完，
		// 哪怕运营也点过停止，记成「已完成」都是在骗人 —— 运营会以为全站都热好了。
		status, errMsg = StatusInterrupted, "预热进程退出，任务中断"
	case runErr != nil:
		status, errMsg = StatusInterrupted, truncate(runErr.Error(), 500)
	case run.stopped:
		status = StatusStopped
	}
	// 收尾用独立的 ctx：任务 ctx 可能已经取消，但状态必须落库，否则这一行会一直挡着下一轮
	finCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.repo.Finish(finCtx, jobID, status, run.snapshot(), errMsg); err != nil {
		return fmt.Errorf("收尾预热任务 %d: %w", jobID, err)
	}
	if runErr != nil && ctx.Err() == nil {
		return runErr
	}
	return nil
}

// runner 跑一轮预热并累计进度。
type runner struct {
	svc   *Service
	job   Job
	total int

	mu sync.Mutex
	c  Counters

	stopped bool
}

func (r *runner) snapshot() Counters {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.c
}

func (r *runner) add(warmed, failed, fetched, bytes int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.c.Warmed += warmed
	r.c.Failed += failed
	r.c.Fetched += fetched
	r.c.TrafficBytes += bytes
}

func (r *runner) doneEpisode(label string) Counters {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.c.EpisodesDone++
	r.c.Current = label
	return r.c
}

// do 按并发逐集预热。
func (r *runner) do(ctx context.Context, eps []Episode) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(max(r.job.Concurrency, 1))
	for _, ep := range eps {
		if r.shouldStop(gctx) {
			break
		}
		g.Go(func() error {
			if r.shouldStop(gctx) {
				return nil
			}
			r.episode(gctx, ep)
			c := r.doneEpisode(fmt.Sprintf("%s 第 %d 集", ep.DramaTitle, ep.Number))
			if c.EpisodesDone%progressEvery == 0 {
				if err := r.svc.repo.UpdateProgress(gctx, r.job.ID, c); err != nil {
					return fmt.Errorf("写预热进度: %w", err)
				}
			}
			return nil
		})
	}
	return g.Wait()
}

// shouldStop 检查运营是否点了停止。
//
// 每集查一次库：一轮几百集就是几百次单行查询，比为一个布尔值再引一条 Valkey 通道简单，
// 而且 worker 重启后信号还在。
func (r *runner) shouldStop(ctx context.Context) bool {
	if r.stopped || ctx.Err() != nil {
		return true
	}
	stop, err := r.svc.repo.StopRequested(ctx, r.job.ID)
	if err != nil {
		slog.WarnContext(ctx, "读预热停止信号失败", "job_id", r.job.ID, "err", err)
		return false
	}
	if stop {
		r.stopped = true
	}
	return stop
}

// episode 预热一集：主清单 → 各档清单 →（with_segments 时）每档前 N 个地址。
func (r *runner) episode(ctx context.Context, ep Episode) {
	master, err := r.svc.lists.Master(ctx, ep.ID)
	if err != nil {
		// 取不到主清单：这一集没法往下热了。常见于产物被撤下、线路全停用
		r.add(0, 1, 0, 0)
		r.record(ctx, ep, "", FetchResult{Message: "取主清单失败：" + errs.From(err).Message})
		return
	}
	// 主清单本身热的是服务端清单原文缓存（它不走边缘）
	r.add(1, 0, 0, 0)

	for _, name := range variantsOf(master) {
		if r.shouldStop(ctx) {
			return
		}
		variant, err := r.svc.lists.Variant(ctx, ep.ID, name)
		if err != nil {
			r.add(0, 1, 0, 0)
			r.record(ctx, ep, "", FetchResult{Message: fmt.Sprintf("取 %s 清单失败：%s", name, errs.From(err).Message)})
			continue
		}
		r.add(1, 0, 0, 0)
		if r.job.Mode != ModeWithSegments {
			continue
		}
		for _, u := range segmentsOf(variant, r.job.SegmentsPerQuality) {
			if r.shouldStop(ctx) {
				return
			}
			res := r.svc.fetch.Fetch(ctx, u)
			if res.OK() {
				r.add(1, 0, 1, res.Bytes)
				continue
			}
			r.add(0, 1, 1, res.Bytes)
			r.record(ctx, ep, u, res)
		}
	}
}

// record 把拿不到的资源记进资源体检的结果，运营在那边逐条排查。
func (r *runner) record(ctx context.Context, ep Episode, url string, res FetchResult) {
	if r.svc.checks == nil {
		return
	}
	if err := r.svc.checks.RecordWarmFailure(ctx, ep.DramaID, ep.ID, url, 0, res); err != nil {
		slog.WarnContext(ctx, "记录预热失败项到体检结果失败", "episode_id", ep.ID, "err", err)
	}
}

// fill 补上派生字段（中文名、已跑时长、百分比）。
func (s *Service) fill(j Job) Job {
	j.ModeTitle = ModeTitle(j.Mode)
	j.StatusTitle = StatusTitle(j.Status)
	end := s.now()
	if j.FinishedAt != nil {
		end = *j.FinishedAt
	}
	if ms := end.Sub(j.StartedAt).Milliseconds(); ms > 0 {
		j.ElapsedMs = ms
	}
	if j.EpisodesTotal > 0 {
		p := j.EpisodesDone * 100 / j.EpisodesTotal
		j.Percent = min(max(p, 0), 100)
	}
	return j
}

func cleanInput(in Input) (Input, error) {
	out := Input{DramaID: in.DramaID, IncludeUnpublished: in.IncludeUnpublished}
	if in.DramaID < 0 {
		return Input{}, badRequest("剧编号不正确")
	}
	switch in.Mode {
	case "":
		out.Mode = ModeWithSegments
	case ModePlaylistOnly, ModeWithSegments:
		out.Mode = in.Mode
	default:
		return Input{}, badRequest("预热模式只能是 playlist_only（只拉清单）或 with_segments（连切片一起拉）")
	}
	switch n := in.SegmentsPerQuality; {
	case n == 0:
		out.SegmentsPerQuality = DefaultSegmentsPerQuality
	case n < 1 || n > maxSegmentsPerQuality:
		return Input{}, badRequest("每个清晰度的片数只能是 1–%d", maxSegmentsPerQuality)
	default:
		out.SegmentsPerQuality = n
	}
	switch n := in.Concurrency; {
	case n == 0:
		out.Concurrency = DefaultConcurrency
	case n < 1 || n > maxConcurrency:
		return Input{}, badRequest("并发集数只能是 1–%d", maxConcurrency)
	default:
		out.Concurrency = n
	}
	return out, nil
}

func scopeHint(in Input) string {
	if in.DramaID > 0 {
		return "这部剧的分集都还没有产物"
	}
	if in.IncludeUnpublished {
		return "全站都还没有转封装完成的分集"
	}
	return "可以勾选「含未上架」再试"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}
