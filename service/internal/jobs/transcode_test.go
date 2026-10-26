package jobs

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"uuid"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"xsite/service/internal/domain/media"
	"xsite/service/internal/transcode"
)

// 转封装任务的调度逻辑：成功、跳过、可重试与不可恢复的失败、重试用尽、任务中途被终止。产物生成用假实现，
// 真实 FFmpeg 流水线的测试在 internal/transcode。

type fakeTranscodeRepo struct {
	mu        sync.Mutex
	attempt   media.Attempt
	startErr  error
	complete  error
	calls     []string
	out       media.Output
	msg, hash string
	progress  []int
}

func (f *fakeTranscodeRepo) call(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *fakeTranscodeRepo) StartAttempt(context.Context, uuid.UUID) (media.Attempt, error) {
	f.call("start")
	return f.attempt, f.startErr
}

func (f *fakeTranscodeRepo) ReportProgress(_ context.Context, _ uuid.UUID, p int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.progress = append(f.progress, p)
	return nil
}

func (f *fakeTranscodeRepo) NextKeyVer(context.Context, int64) (int32, error) {
	f.call("keyver")
	return 3, nil
}

func (f *fakeTranscodeRepo) Skip(_ context.Context, _ uuid.UUID, hash string) error {
	f.call("skip")
	f.hash = hash
	return nil
}

func (f *fakeTranscodeRepo) Retry(_ context.Context, _ uuid.UUID, hash, msg string) error {
	f.call("retry")
	f.hash, f.msg = hash, msg
	return nil
}

func (f *fakeTranscodeRepo) Fail(_ context.Context, _ uuid.UUID, hash, msg string) error {
	f.call("fail")
	f.hash, f.msg = hash, msg
	return nil
}

func (f *fakeTranscodeRepo) Complete(_ context.Context, _ uuid.UUID, out media.Output) error {
	f.call("complete")
	f.out = out
	return f.complete
}

type fakePackager struct {
	fetchErr, packErr error
	hash              string
	result            transcode.Result
	discarded         int
	beforeReturn      func(report func(int))
	req               transcode.Request
}

func (p *fakePackager) Fetch(context.Context, string, string) (*transcode.Workspace, error) {
	if p.fetchErr != nil {
		return nil, p.fetchErr
	}
	return &transcode.Workspace{SHA256: p.hash}, nil
}

func (p *fakePackager) Package(_ context.Context, _ *transcode.Workspace, req transcode.Request, report func(int)) (transcode.Result, error) {
	p.req = req
	if p.beforeReturn != nil {
		p.beforeReturn(report)
	}
	return p.result, p.packErr
}

func (p *fakePackager) Discard(context.Context, int64, string) error { p.discarded++; return nil }

func (p *fakePackager) PublicError(err error) string { return "后台显示：" + err.Error() }

type fakeKeys struct{}

func (fakeKeys) Wrap(episodeID int64, keyVer int32, key []byte) ([]byte, error) {
	if len(key) != 16 {
		return nil, errors.New("密钥长度不对")
	}
	return []byte("wrapped"), nil
}

func newTranscodeWorker(repo *fakeTranscodeRepo, p *fakePackager) *TranscodeWorker {
	return &TranscodeWorker{Repo: repo, Packager: p, Keys: fakeKeys{}, JobTimeout: time.Minute, ProgressEvery: 5 * time.Millisecond}
}

func riverJob(id uuid.UUID, attempt int) *river.Job[TranscodeArgs] {
	return &river.Job[TranscodeArgs]{
		JobRow: &rivertype.JobRow{ID: 1, Attempt: attempt, MaxAttempts: TranscodeMaxAttempts},
		Args:   TranscodeArgs{JobID: id.String()},
	}
}

func attemptFor(id uuid.UUID) media.Attempt {
	return media.Attempt{Job: media.Job{ID: id, EpisodeID: 7, DramaID: 3, SourceKey: "fixtures/a.mp4", Status: media.JobRunning}}
}

func TestTranscodeWorker_Success(t *testing.T) {
	id := uuid.NewV7()
	repo := &fakeTranscodeRepo{attempt: attemptFor(id)}
	pack := &fakePackager{hash: "h1", result: transcode.Result{
		Prefix: "hls/7/x", SizeBytes: 99, MasterSHA256: "m", Poster: "posters/7/p.webp", Duration: 61.6,
		Renditions: []transcode.RenditionInfo{{Name: "v360", Width: 360, Height: 640, Bitrate: 1, PeakBitrate: 2, Codecs: "avc1.64001e"}},
	}}
	pack.beforeReturn = func(report func(int)) {
		report(42)
		time.Sleep(30 * time.Millisecond) // 让后台的进度写库跑几轮
	}
	if err := newTranscodeWorker(repo, pack).Work(context.Background(), riverJob(id, 1)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(repo.calls, []string{"start", "keyver", "complete"}) || pack.discarded != 0 {
		t.Fatalf("调用顺序：%v，清理 %d 次", repo.calls, pack.discarded)
	}
	out := repo.out
	if out.EpisodeID != 7 || out.DramaID != 3 || out.KeyVer != 3 || string(out.WrappedKey) != "wrapped" || out.SourceHash != "h1" ||
		out.DurationSec != 62 || out.Prefix != "hls/7/x" || len(out.Renditions) != 1 || out.Renditions[0].Codecs != "avc1.64001e" {
		t.Fatalf("提交的产物不符合预期：%+v", out)
	}
	if pack.req.KeyVer != 3 || len(pack.req.Key) != 16 || pack.req.JobID != id.String() {
		t.Fatalf("打包参数不符合预期：%+v", pack.req)
	}
	if !slices.Contains(repo.progress, 42) {
		t.Fatalf("进度应写库，得到 %v", repo.progress)
	}
	// 打包时拿到的就是任务里的那块内存：任务结束后应已清零
	if !bytes.Equal(pack.req.Key, make([]byte, 16)) {
		t.Fatal("任务结束后应清除内存中的内容密钥")
	}
}

func TestTranscodeWorker_SkipWhenSourceUnchanged(t *testing.T) {
	id := uuid.NewV7()
	a := attemptFor(id)
	a.EpisodeMediaStatus, a.EpisodeSourceHash = media.EpisodeReady, "same"
	repo := &fakeTranscodeRepo{attempt: a}
	pack := &fakePackager{hash: "same"}
	if err := newTranscodeWorker(repo, pack).Work(context.Background(), riverJob(id, 1)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(repo.calls, []string{"start", "skip"}) || repo.hash != "same" || pack.req.JobID != "" {
		t.Fatalf("源文件没变应跳过、不转码：%v", repo.calls)
	}
}

func TestTranscodeWorker_Failures(t *testing.T) {
	transient := errors.New("上传切片: 连接被重置")
	tests := []struct {
		name       string
		attempt    int
		pack       fakePackager
		wantCalls  []string
		wantCancel bool
		wantErr    bool
	}{
		{"可重试的错误：记录后等待重试", 1, fakePackager{hash: "h", packErr: transient}, []string{"start", "keyver", "retry"}, false, true},
		{"重试用尽：最终失败", TranscodeMaxAttempts, fakePackager{hash: "h", packErr: transient}, []string{"start", "keyver", "fail"}, false, true},
		{"不可恢复：不再重试", 1, fakePackager{fetchErr: transcode.Permanent(errors.New("源文件 a.mp4 不存在"))}, []string{"start", "fail"}, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := uuid.NewV7()
			repo := &fakeTranscodeRepo{attempt: attemptFor(id)}
			pack := tt.pack
			err := newTranscodeWorker(repo, &pack).Work(context.Background(), riverJob(id, tt.attempt))
			if (err != nil) != tt.wantErr || errors.Is(err, &rivertype.JobCancelError{}) != tt.wantCancel {
				t.Fatalf("返回 %v，期望取消=%v", err, tt.wantCancel)
			}
			if !slices.Equal(repo.calls, tt.wantCalls) {
				t.Fatalf("调用顺序 %v，期望 %v", repo.calls, tt.wantCalls)
			}
			if pack.discarded != 1 || repo.msg != "后台显示："+err.Error() && !tt.wantCancel {
				t.Fatalf("失败后应清理产物并记录错误：清理 %d 次，错误 %q", pack.discarded, repo.msg)
			}
			if tt.pack.hash != "" && repo.hash != "h" {
				t.Fatalf("下载成功后失败应记录源文件哈希，得到 %q", repo.hash)
			}
		})
	}
}

func TestTranscodeWorker_JobGone(t *testing.T) {
	// 任务已结束（River 重复投递）或分集已删除：不执行
	id := uuid.NewV7()
	repo := &fakeTranscodeRepo{startErr: media.ErrJobGone}
	pack := &fakePackager{}
	if err := newTranscodeWorker(repo, pack).Work(context.Background(), riverJob(id, 1)); err != nil || pack.discarded != 0 {
		t.Fatalf("任务已结束应直接返回：%v", err)
	}

	// 转封装途中分集被删除（Complete 返回 ErrJobGone）：清理产物，不重试也不记失败
	repo = &fakeTranscodeRepo{attempt: attemptFor(id), complete: media.ErrJobGone}
	pack = &fakePackager{hash: "h"}
	if err := newTranscodeWorker(repo, pack).Work(context.Background(), riverJob(id, 1)); err != nil {
		t.Fatalf("任务被终止应返回 nil，得到 %v", err)
	}
	if !slices.Equal(repo.calls, []string{"start", "keyver", "complete"}) || pack.discarded != 1 {
		t.Fatalf("应清理产物且不记录重试或失败：%v 清理 %d 次", repo.calls, pack.discarded)
	}

	// 参数无效：取消
	err := newTranscodeWorker(&fakeTranscodeRepo{}, &fakePackager{}).Work(context.Background(),
		&river.Job[TranscodeArgs]{JobRow: &rivertype.JobRow{Attempt: 1, MaxAttempts: 4}, Args: TranscodeArgs{JobID: "x"}})
	if !errors.Is(err, &rivertype.JobCancelError{}) {
		t.Fatalf("参数无效应取消，得到 %v", err)
	}
}

func TestConfigureTranscode(t *testing.T) {
	cfg := Config()
	ConfigureTranscode(cfg, nil, 2)
	if _, ok := cfg.Queues[QueueMedia]; ok {
		t.Fatal("不转封装的进程不应消费 media 队列")
	}

	cfg = Config()
	ConfigureTranscode(cfg, &TranscodeWorker{JobTimeout: 2 * time.Hour}, 3)
	if cfg.Queues[QueueMedia].MaxWorkers != 3 || cfg.RescueStuckJobsAfter != 2*time.Hour+15*time.Minute {
		t.Fatalf("media 队列并发与卡住判定时间不对：%+v %v", cfg.Queues[QueueMedia], cfg.RescueStuckJobsAfter)
	}
	if (TranscodeArgs{}).InsertOpts().Queue != QueueMedia || (TranscodeArgs{}).InsertOpts().MaxAttempts != 4 {
		t.Fatal("转封装任务入 media 队列，最多 4 次")
	}
}
