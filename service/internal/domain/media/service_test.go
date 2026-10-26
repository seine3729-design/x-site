package media

import (
	"context"
	"errors"
	"strings"
	"testing"

	"uuid"

	"xsite/service/internal/errs"
)

// fakeRepo 只实现服务用到的方法；嵌入的 Repo 为 nil，调用其他方法会 panic。
type fakeRepo struct {
	Repo
	created []string
	query   JobQuery
	job     Job
	err     error
}

func (f *fakeRepo) CreateJob(_ context.Context, id uuid.UUID, episodeID int64, sourceKey string, force bool) (Job, error) {
	f.created = append(f.created, sourceKey)
	return Job{ID: id, EpisodeID: episodeID, SourceKey: sourceKey, Force: force, Status: JobQueued}, f.err
}

func (f *fakeRepo) GetJob(_ context.Context, id uuid.UUID) (Job, error) {
	return Job{ID: id}, f.err
}

func (f *fakeRepo) ListJobs(_ context.Context, q JobQuery) ([]Job, int64, error) {
	f.query = q
	return []Job{f.job}, 41, f.err
}

func TestValidateSourceKey(t *testing.T) {
	for _, ok := range []string{"fixtures/f01-portrait-1080p30.mp4", "ingest/2026/10/第1集 正片.MP4", "a"} {
		if err := ValidateSourceKey(ok); err != nil {
			t.Errorf("%q 应合法：%v", ok, err)
		}
	}
	for _, bad := range []string{"", "/etc/passwd", "a/../b", "./a", "a//b", "a/", `a\b`, "a\x00b", "a\nb", "a\x7fb",
		strings.Repeat("a", 1025), string([]byte{0xff, 0xfe})} {
		if err := ValidateSourceKey(bad); !errors.Is(err, errs.ErrBadRequest) {
			t.Errorf("%q 应返回 400，得到 %v", bad, err)
		}
	}
}

func TestService_Enqueue(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	ctx := context.Background()

	j, err := svc.Enqueue(ctx, 5, EnqueueInput{SourceKey: "fixtures/a.mp4", Force: true})
	if err != nil || j.EpisodeID != 5 || !j.Force || j.Status != JobQueued {
		t.Fatalf("得到 %+v %v", j, err)
	}
	if j.ID.String() == "" || j.ID == (uuid.UUID{}) {
		t.Fatal("任务编号应为 UUIDv7")
	}
	// 不填源文件：交给仓储沿用上一个任务的源
	if _, err := svc.Enqueue(ctx, 5, EnqueueInput{}); err != nil || repo.created[1] != "" {
		t.Fatalf("空源文件应交给仓储沿用上次的源：%v %q", err, repo.created)
	}
	if _, err := svc.Enqueue(ctx, 5, EnqueueInput{SourceKey: "../x.mp4"}); !errors.Is(err, errs.ErrBadRequest) || len(repo.created) != 2 {
		t.Fatalf("非法路径应在入库前拒绝：%v", err)
	}
	repo.err = ErrActive
	if _, err := svc.Enqueue(ctx, 5, EnqueueInput{SourceKey: "fixtures/a.mp4"}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("已有进行中的任务应为 409，得到 %v", err)
	}
}

func TestService_GetAndList(t *testing.T) {
	repo := &fakeRepo{}
	svc := NewService(repo)
	ctx := context.Background()

	if _, err := svc.GetJob(ctx, "not-a-uuid"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("编号格式不对应为 404，得到 %v", err)
	}
	id := uuid.NewV7()
	if j, err := svc.GetJob(ctx, id.String()); err != nil || j.ID != id {
		t.Fatalf("得到 %+v %v", j, err)
	}

	page, err := svc.ListJobs(ctx, JobFilter{DramaID: 3, Status: JobFailed, Page: 3, PageSize: 10})
	if err != nil || page.Total != 41 || page.Page != 3 || page.PageSize != 10 {
		t.Fatalf("得到 %+v %v", page, err)
	}
	if repo.query != (JobQuery{DramaID: 3, Status: JobFailed, Limit: 10, Offset: 20}) {
		t.Fatalf("查询参数不对：%+v", repo.query)
	}
	if page, _ := svc.ListJobs(ctx, JobFilter{}); page.Page != 1 || page.PageSize != 20 {
		t.Fatalf("默认第 1 页、每页 20 条，得到 %+v", page)
	}
	for _, f := range []JobFilter{{Status: "done"}, {Page: -1}, {PageSize: 101}} {
		if _, err := svc.ListJobs(ctx, f); !errors.Is(err, errs.ErrBadRequest) {
			t.Errorf("%+v 应返回 400，得到 %v", f, err)
		}
	}
}

func TestAttempt_ShouldSkip(t *testing.T) {
	ready := Attempt{EpisodeMediaStatus: EpisodeReady, EpisodeSourceHash: "abc"}
	if !ready.ShouldSkip("abc") {
		t.Fatal("源文件与当前产物相同应跳过")
	}
	forced := ready
	forced.Force = true
	for name, a := range map[string]Attempt{
		"源文件变了":    ready,
		"强制重新转码":   forced,
		"分集还没有视频":  {EpisodeMediaStatus: EpisodeTranscoding, EpisodeSourceHash: "abc"},
		"之前没有记录哈希": {EpisodeMediaStatus: EpisodeReady},
	} {
		hash := "abc"
		if name == "源文件变了" {
			hash = "def"
		}
		if name == "之前没有记录哈希" {
			hash = ""
		}
		if a.ShouldSkip(hash) {
			t.Errorf("%s：不应跳过", name)
		}
	}
	if !JobQueued.Active() || !JobRunning.Active() || JobFailed.Active() || JobSkipped.Active() {
		t.Fatal("只有 queued、running 占住这一集")
	}
}
