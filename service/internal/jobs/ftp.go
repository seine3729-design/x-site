package jobs

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"xsite/service/internal/domain/resource"
	"xsite/service/internal/errs"
)

// 资源机测连 / 列目录（S1-6）：API 入队后等待结果，由 worker 发起连接（docs/03 第 9.7 节，不暴露 API 节点 IP）。
// 时长：worker 单次操作 15 秒（ftpclient），任务超时 20 秒，API 最多等 25 秒（HTTP 写超时 30 秒）。
const (
	ftpProbeJobTimeout = 20 * time.Second
	ftpProbeWait       = 25 * time.Second
	ftpProbePoll       = 200 * time.Millisecond
)

var (
	errFTPProbeTimeout = errs.ErrUnavailable.WithMessage("worker 没有在 25 秒内返回资源机的结果，请确认 worker 正在运行后重试")
	errFTPProbeFailed  = errs.ErrUnavailable.WithMessage("worker 执行资源机任务失败，请稍后重试")
)

// FTPProbeArgs 是一次测连或列目录。参数里是发起时的资源机配置（不含账号密码），worker 不再读库。
type FTPProbeArgs struct {
	Op       string `json:"op"`
	ServerID int64  `json:"server_id"`
	Host     string `json:"host"`
	Port     int32  `json:"port"`
	TLS      string `json:"tls"`
	Passive  bool   `json:"passive"`
	CredRef  string `json:"cred_ref"`
	Path     string `json:"path,omitempty"`
}

func (FTPProbeArgs) Kind() string { return "ftp_probe" }

func ftpProbeArgsOf(req resource.ProbeRequest) FTPProbeArgs {
	s := req.Server
	return FTPProbeArgs{
		Op: string(req.Op), ServerID: s.ID, Host: s.Host, Port: s.Port, TLS: string(s.TLS), Passive: s.Passive,
		CredRef: s.CredRef, Path: req.Path,
	}
}

func (a FTPProbeArgs) request() resource.ProbeRequest {
	return resource.ProbeRequest{
		Op: resource.ProbeOp(a.Op), Path: a.Path,
		Server: resource.FTPServer{
			ID: a.ServerID, Host: a.Host, Port: a.Port, TLS: resource.TLSMode(a.TLS), Passive: a.Passive, CredRef: a.CredRef,
		},
	}
}

// ftpProbeOutput 是任务结果（River 的 output，存在 river_job.metadata 中）。
type ftpProbeOutput struct {
	OK        bool            `json:"ok"`
	ElapsedMs int64           `json:"elapsed_ms"`
	Steps     []ftpProbeStep  `json:"steps"`
	Path      string          `json:"path"`
	Entries   []ftpProbeEntry `json:"entries"`
	Truncated bool            `json:"truncated"`
}

type ftpProbeStep struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Ms      int64  `json:"ms"`
	Message string `json:"message"`
}

type ftpProbeEntry struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Size       uint64     `json:"size"`
	ModifiedAt *time.Time `json:"modified_at,omitempty"`
	Target     string     `json:"target,omitempty"`
}

func ftpProbeOutputOf(r resource.ProbeResult) ftpProbeOutput {
	out := ftpProbeOutput{
		OK: r.OK, ElapsedMs: r.ElapsedMs, Path: r.Path, Truncated: r.Truncated,
		Steps: make([]ftpProbeStep, len(r.Steps)), Entries: make([]ftpProbeEntry, len(r.Entries)),
	}
	for i, s := range r.Steps {
		out.Steps[i] = ftpProbeStep{Name: string(s.Name), OK: s.OK, Ms: s.Ms, Message: s.Message}
	}
	for i, e := range r.Entries {
		out.Entries[i] = ftpProbeEntry{Name: e.Name, Type: string(e.Type), Size: e.Size, ModifiedAt: e.ModifiedAt, Target: e.Target}
	}
	return out
}

func (o ftpProbeOutput) result() resource.ProbeResult {
	r := resource.ProbeResult{
		OK: o.OK, ElapsedMs: o.ElapsedMs, Path: o.Path, Truncated: o.Truncated,
		Steps: make([]resource.Step, len(o.Steps)), Entries: make([]resource.Entry, len(o.Entries)),
	}
	for i, s := range o.Steps {
		r.Steps[i] = resource.Step{Name: resource.StepName(s.Name), OK: s.OK, Ms: s.Ms, Message: s.Message}
	}
	for i, e := range o.Entries {
		r.Entries[i] = resource.Entry{Name: e.Name, Type: resource.EntryType(e.Type), Size: e.Size, ModifiedAt: e.ModifiedAt, Target: e.Target}
	}
	return r
}

// FTPProber 在 worker 上执行探测（ftpclient.Prober）。
type FTPProber interface {
	Probe(ctx context.Context, req resource.ProbeRequest) resource.ProbeResult
}

// FTPProbeWorker 执行资源机测连 / 列目录，结果写入任务的 output。只试一次：结果是给人看的，过时重试没有意义。
type FTPProbeWorker struct {
	river.WorkerDefaults[FTPProbeArgs]
	Prober FTPProber
}

func (w *FTPProbeWorker) Timeout(*river.Job[FTPProbeArgs]) time.Duration { return ftpProbeJobTimeout }

func (w *FTPProbeWorker) Work(ctx context.Context, job *river.Job[FTPProbeArgs]) error {
	req := job.Args.request()
	if req.Op != resource.ProbeTest && req.Op != resource.ProbeList {
		return river.JobCancel(fmt.Errorf("未知的资源机操作 %q", job.Args.Op))
	}
	return river.RecordOutput(ctx, ftpProbeOutputOf(w.Prober.Probe(ctx, req)))
}

// FTPProbeClient 实现 resource.Prober：入队 ftp_probe 任务并轮询结果，供 API 进程使用。
type FTPProbeClient struct {
	River *river.Client[pgx.Tx]
	// Queue 为空时用 default 队列；测试用独立队列，避免被本机正在运行的 worker 取走。
	Queue string
	// Wait、Poll 为 0 时取默认值（最多等 25 秒，每 200 毫秒查一次）。
	Wait time.Duration
	Poll time.Duration
}

// Probe 入队并等待 worker 的结果；超时或调用方离开时撤销任务。
func (c *FTPProbeClient) Probe(ctx context.Context, req resource.ProbeRequest) (resource.ProbeResult, error) {
	inserted, err := c.River.Insert(ctx, ftpProbeArgsOf(req), &river.InsertOpts{
		Queue: cmp.Or(c.Queue, QueueDefault), MaxAttempts: 1,
	})
	if err != nil {
		return resource.ProbeResult{}, fmt.Errorf("资源机任务入队: %w", err)
	}
	id := inserted.Job.ID

	waitCtx, cancel := context.WithTimeout(ctx, cmp.Or(c.Wait, ftpProbeWait))
	defer cancel()
	tick := time.NewTicker(cmp.Or(c.Poll, ftpProbePoll))
	defer tick.Stop()
	for {
		select {
		case <-waitCtx.Done():
			c.cancel(id)
			if ctx.Err() != nil {
				return resource.ProbeResult{}, ctx.Err()
			}
			slog.WarnContext(ctx, "资源机任务等待超时", "job_id", id, "server_id", req.Server.ID, "op", req.Op)
			return resource.ProbeResult{}, errFTPProbeTimeout
		case <-tick.C:
		}
		job, err := c.River.JobGet(waitCtx, id)
		if err != nil {
			if waitCtx.Err() != nil {
				continue // 下一轮进入超时分支
			}
			return resource.ProbeResult{}, fmt.Errorf("读取资源机任务 %d: %w", id, err)
		}
		switch job.State {
		case rivertype.JobStateCompleted:
			var out ftpProbeOutput
			if err := json.Unmarshal(job.Output(), &out); err != nil {
				return resource.ProbeResult{}, fmt.Errorf("解析资源机任务 %d 的结果: %w", id, err)
			}
			return out.result(), nil
		case rivertype.JobStateDiscarded, rivertype.JobStateCancelled:
			var last string
			if n := len(job.Errors); n > 0 {
				last = job.Errors[n-1].Error
			}
			slog.ErrorContext(ctx, "资源机任务失败", "job_id", id, "state", job.State, "err", last)
			return resource.ProbeResult{}, errFTPProbeFailed
		}
	}
}

// cancel 撤销没等到结果的任务：还没开始的不再执行，执行中的会中断连接。请求可能已经结束，不用它的 ctx。
func (c *FTPProbeClient) cancel(id int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.River.JobCancel(ctx, id); err != nil {
		slog.Warn("撤销资源机任务失败", "job_id", id, "err", err)
	}
}
