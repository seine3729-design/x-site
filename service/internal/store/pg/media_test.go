package pg

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"uuid"

	"github.com/jackc/pgx/v5"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/media"
	"xsite/service/internal/errs"
)

// 转封装任务（S1-5）走真实 PostgreSQL：入队事务、状态流转、切换产物与「更新至 N 集」。River 入队用假实现记录调用，
// 不往本机正在运行的 worker 投递任务。

type fakeEnqueuer struct {
	mu   sync.Mutex
	ids  []uuid.UUID
	fail error
}

func (f *fakeEnqueuer) enqueue(_ context.Context, _ pgx.Tx, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.ids = append(f.ids, id)
	return nil
}

type mediaFixture struct {
	contentFixture
	media *MediaRepo
	svc   *media.Service
	enq   *fakeEnqueuer
}

func newMediaFixture(t *testing.T) mediaFixture {
	t.Helper()
	cf := newContentFixture(t)
	enq := &fakeEnqueuer{}
	repo := NewMediaRepo(cf.repo.pool, enq.enqueue)
	f := mediaFixture{contentFixture: cf, media: repo, svc: media.NewService(repo), enq: enq}
	t.Cleanup(func() {
		// 剧删除后分集、任务、密钥级联删除，媒体对象只是解除关联；按产物目录前缀清理
		_, _ = cf.repo.pool.Exec(context.Background(),
			`DELETE FROM media_object WHERE episode_id IS NULL AND key LIKE 'it-media/%'`)
	})
	return f
}

func (f mediaFixture) episode(t *testing.T, dramaID int64, number int32, duration int32) content.Episode {
	t.Helper()
	return must(f.contentFixture.svc.CreateEpisode(context.Background(), dramaID, content.EpisodeInput{Number: number, DurationSec: duration}))(t)
}

type episodeMedia struct {
	Status      string
	VideoObject *int64
	KeyVer      int32
	SourceHash  string
	Poster      string
	DurationSec int32
	Renditions  string
}

func (f mediaFixture) episodeMedia(t *testing.T, id int64) episodeMedia {
	t.Helper()
	var m episodeMedia
	err := f.repo.pool.QueryRow(context.Background(),
		`SELECT media_status, video_object_id, key_ver, source_hash, poster, duration_sec, renditions::text FROM episode WHERE id = $1`, id).
		Scan(&m.Status, &m.VideoObject, &m.KeyVer, &m.SourceHash, &m.Poster, &m.DurationSec, &m.Renditions)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func output(ep content.Episode, jobID uuid.UUID, keyVer int32, hash string) media.Output {
	return media.Output{
		EpisodeID: ep.ID, DramaID: ep.DramaID, Prefix: fmt.Sprintf("it-media/%d/%s", ep.ID, jobID), SizeBytes: 12345,
		MasterSHA256: "m" + hash, SourceHash: hash, KeyVer: keyVer, WrappedKey: []byte("dev1:wrapped"),
		Renditions: []media.Rendition{{Name: "v360", Width: 360, Height: 640, Bitrate: 400000, PeakBitrate: 520000, Codecs: "avc1.64001e,mp4a.40.2"}},
		Poster:     fmt.Sprintf("posters/%d/%s.webp", ep.ID, hash), DurationSec: 61,
	}
}

func TestMediaRepo_EnqueueRules(t *testing.T) {
	f := newMediaFixture(t)
	ctx := context.Background()
	d := f.drama(t, "转封装入队", content.DramaInput{})
	ep := f.episode(t, d.ID, 1, 0)

	if _, err := f.svc.Enqueue(ctx, ep.ID, media.EnqueueInput{}); !errors.Is(err, media.ErrNoSource) {
		t.Fatalf("没有上一个任务时必须填写源文件，得到 %v", err)
	}
	// River 入队失败时整个事务回滚：没有任务行，分集状态不变
	f.enq.fail = errors.New("river 不可用")
	if _, err := f.svc.Enqueue(ctx, ep.ID, media.EnqueueInput{SourceKey: "fixtures/a.mp4"}); err == nil {
		t.Fatal("入队失败应返回错误")
	}
	if page := must(f.svc.ListJobs(ctx, media.JobFilter{EpisodeID: ep.ID}))(t); page.Total != 0 || f.episodeMedia(t, ep.ID).Status != "pending" {
		t.Fatalf("入队失败应整体回滚：%+v", page)
	}
	f.enq.fail = nil

	j := must(f.svc.Enqueue(ctx, ep.ID, media.EnqueueInput{SourceKey: "fixtures/a.mp4", Force: true}))(t)
	if j.Status != media.JobQueued || j.EpisodeID != ep.ID || j.DramaID != d.ID || j.EpisodeNumber != 1 || !j.Force ||
		j.SourceKey != "fixtures/a.mp4" || j.Attempts != 0 || len(j.Renditions) != 0 || j.StartedAt != nil {
		t.Fatalf("新任务字段不符合预期：%+v", j)
	}
	if !slices.Equal(f.enq.ids, []uuid.UUID{j.ID}) {
		t.Fatalf("应在同一事务内入队一次，得到 %v", f.enq.ids)
	}
	if got := f.episodeMedia(t, ep.ID).Status; got != "transcoding" {
		t.Fatalf("没有视频的分集入队后为 transcoding，得到 %s", got)
	}
	if _, err := f.svc.Enqueue(ctx, ep.ID, media.EnqueueInput{SourceKey: "fixtures/b.mp4"}); !errors.Is(err, media.ErrActive) {
		t.Fatalf("已有排队中的任务应 409，得到 %v", err)
	}

	// 并发提交：分集行锁串行化，只有一个成功
	other := f.episode(t, d.ID, 2, 0)
	var wg sync.WaitGroup
	results := make([]error, 8)
	for i := range results {
		wg.Go(func() { _, results[i] = f.svc.Enqueue(ctx, other.ID, media.EnqueueInput{SourceKey: "fixtures/c.mp4"}) })
	}
	wg.Wait()
	ok := 0
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, media.ErrActive):
			t.Fatalf("并发提交应只得到成功或 409，得到 %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("并发提交应只有 1 个成功，得到 %d", ok)
	}

	// 分集不存在、剧在回收站：404
	if _, err := f.svc.Enqueue(ctx, 1<<62, media.EnqueueInput{SourceKey: "fixtures/a.mp4"}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("分集不存在应 404，得到 %v", err)
	}
	gone := f.drama(t, "回收站", content.DramaInput{})
	goneEp := f.episode(t, gone.ID, 1, 0)
	gj := must(f.svc.Enqueue(ctx, goneEp.ID, media.EnqueueInput{SourceKey: "fixtures/a.mp4"}))(t)
	must(f.contentFixture.svc.DeleteDrama(ctx, gone.ID))(t)
	if _, err := f.svc.Enqueue(ctx, goneEp.ID, media.EnqueueInput{SourceKey: "fixtures/a.mp4"}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("剧在回收站时应 404，得到 %v", err)
	}
	if _, err := f.svc.GetJob(ctx, gj.ID.String()); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("回收站中的剧的任务视为不存在，得到 %v", err)
	}

	// 列表：按剧、状态筛选，按创建时间倒序
	page := must(f.svc.ListJobs(ctx, media.JobFilter{DramaID: d.ID}))(t)
	if page.Total != 2 || page.List[0].EpisodeID != other.ID || page.List[1].ID != j.ID {
		t.Fatalf("按剧筛选：%+v", page)
	}
	if page := must(f.svc.ListJobs(ctx, media.JobFilter{DramaID: d.ID, Status: media.JobFailed}))(t); page.Total != 0 {
		t.Fatalf("按状态筛选：%+v", page)
	}
}

func TestMediaRepo_SuccessSwitchesOutput(t *testing.T) {
	f := newMediaFixture(t)
	ctx := context.Background()
	d := f.drama(t, "转封装成功", content.DramaInput{})
	ep1 := f.episode(t, d.ID, 1, 0)  // 时长未知：成功后回填
	ep2 := f.episode(t, d.ID, 2, 90) // 已填时长：不覆盖
	f.episode(t, d.ID, 3, 0)         // 没有转封装

	run := func(ep content.Episode, hash string) (media.Job, int32) {
		t.Helper()
		j := must(f.svc.Enqueue(ctx, ep.ID, media.EnqueueInput{SourceKey: "fixtures/" + hash + ".mp4"}))(t)
		a := must(f.media.StartAttempt(ctx, j.ID))(t)
		if a.Status != media.JobRunning || a.Attempts != 1 || a.StartedAt == nil {
			t.Fatalf("开始尝试：%+v", a)
		}
		if err := f.media.ReportProgress(ctx, j.ID, 42); err != nil {
			t.Fatal(err)
		}
		v := must(f.media.NextKeyVer(ctx, ep.ID))(t)
		if err := f.media.Complete(ctx, j.ID, output(ep, j.ID, v, hash)); err != nil {
			t.Fatal(err)
		}
		return must(f.media.GetJob(ctx, j.ID))(t), v
	}

	j, v := run(ep2, "h2")
	if v != 1 {
		t.Fatalf("第一把内容密钥版本为 1，得到 %d", v)
	}
	m := f.episodeMedia(t, ep2.ID)
	if m.Status != "ready" || m.VideoObject == nil || m.KeyVer != 1 || m.SourceHash != "h2" || m.DurationSec != 90 ||
		m.Poster != fmt.Sprintf("posters/%d/h2.webp", ep2.ID) || m.Renditions == "[]" {
		t.Fatalf("分集应切换到新产物且不覆盖已填的时长：%+v", m)
	}
	if j.Status != media.JobSucceeded || j.Progress != 100 || j.KeyVer != 1 || j.DurationSec != 61 || j.FinishedAt == nil ||
		len(j.Renditions) != 1 || j.Renditions[0].Codecs != "avc1.64001e,mp4a.40.2" {
		t.Fatalf("任务应记为成功并带产物信息：%+v", j)
	}
	var objKey, objSHA string
	var objEpisode *int64
	if err := f.repo.pool.QueryRow(ctx, `SELECT key, sha256, episode_id FROM media_object WHERE id = $1`, *m.VideoObject).
		Scan(&objKey, &objSHA, &objEpisode); err != nil || objKey != fmt.Sprintf("it-media/%d/%s", ep2.ID, j.ID) || objSHA != "mh2" || *objEpisode != ep2.ID {
		t.Fatalf("媒体对象不符合预期：%s %s %v %v", objKey, objSHA, objEpisode, err)
	}
	var wrapped []byte
	if err := f.repo.pool.QueryRow(ctx, `SELECT wrapped_key FROM media_key WHERE episode_id = $1 AND key_ver = 1`, ep2.ID).Scan(&wrapped); err != nil ||
		string(wrapped) != "dev1:wrapped" {
		t.Fatalf("内容密钥应入库：%q %v", wrapped, err)
	}
	if got := must(f.repo.GetDrama(ctx, d.ID))(t).LatestEpisode; got != 2 {
		t.Fatalf("更新至 N 集只算可播放的分集，应为 2，得到 %d", got)
	}
	if _, err := f.media.StartAttempt(ctx, j.ID); !errors.Is(err, media.ErrJobGone) {
		t.Fatalf("已成功的任务不再执行（River 重复投递），得到 %v", err)
	}

	run(ep1, "h1")
	if m := f.episodeMedia(t, ep1.ID); m.DurationSec != 61 {
		t.Fatalf("时长未知时用探测结果回填，得到 %d", m.DurationSec)
	}

	// 重新转封装：旧产物在新产物完成前继续可播；成功后换到新目录、密钥版本加一
	old := f.episodeMedia(t, ep2.ID)
	j2 := must(f.svc.Enqueue(ctx, ep2.ID, media.EnqueueInput{}))(t) // 不填源文件：沿用上一个任务的
	if j2.SourceKey != "fixtures/h2.mp4" || f.episodeMedia(t, ep2.ID).Status != "ready" {
		t.Fatalf("重试沿用上次的源文件，分集保持 ready：%+v", j2)
	}
	must(f.media.StartAttempt(ctx, j2.ID))(t)
	v2 := must(f.media.NextKeyVer(ctx, ep2.ID))(t)
	if v2 != 2 {
		t.Fatalf("内容密钥版本应递增为 2，得到 %d", v2)
	}
	if err := f.media.Complete(ctx, j2.ID, output(ep2, j2.ID, v2, "h2b")); err != nil {
		t.Fatal(err)
	}
	if m := f.episodeMedia(t, ep2.ID); m.KeyVer != 2 || *m.VideoObject == *old.VideoObject || m.SourceHash != "h2b" {
		t.Fatalf("应切换到新产物：%+v", m)
	}
}

func TestMediaRepo_FailSkipRetry(t *testing.T) {
	f := newMediaFixture(t)
	ctx := context.Background()
	d := f.drama(t, "转封装失败", content.DramaInput{})
	fresh := f.episode(t, d.ID, 1, 0)
	ready := f.episode(t, d.ID, 2, 0)

	// 先让第 2 集有可播放的产物
	j := must(f.svc.Enqueue(ctx, ready.ID, media.EnqueueInput{SourceKey: "fixtures/r.mp4"}))(t)
	must(f.media.StartAttempt(ctx, j.ID))(t)
	if err := f.media.Complete(ctx, j.ID, output(ready, j.ID, 1, "hr")); err != nil {
		t.Fatal(err)
	}

	// 重试：本次尝试的错误记下，状态回到 queued；下一次尝试清掉错误、次数加一
	jf := must(f.svc.Enqueue(ctx, fresh.ID, media.EnqueueInput{SourceKey: "fixtures/f.mp4"}))(t)
	must(f.media.StartAttempt(ctx, jf.ID))(t)
	if err := f.media.Retry(ctx, jf.ID, "hf", "上传切片失败"); err != nil {
		t.Fatal(err)
	}
	if got := must(f.media.GetJob(ctx, jf.ID))(t); got.Status != media.JobQueued || got.Error != "上传切片失败" || got.SourceHash != "hf" {
		t.Fatalf("等待重试：%+v", got)
	}
	a := must(f.media.StartAttempt(ctx, jf.ID))(t)
	if a.Attempts != 2 || a.Error != "" || a.EpisodeMediaStatus != "transcoding" {
		t.Fatalf("第二次尝试：%+v", a)
	}
	// 最终失败：没有视频的分集置为 failed
	if err := f.media.Fail(ctx, jf.ID, "", "转码: ffmpeg 退出码 1"); err != nil {
		t.Fatal(err)
	}
	if got := must(f.media.GetJob(ctx, jf.ID))(t); got.Status != media.JobFailed || got.SourceHash != "hf" || got.FinishedAt == nil {
		t.Fatalf("最终失败（源文件哈希保留）：%+v", got)
	}
	if got := f.episodeMedia(t, fresh.ID).Status; got != "failed" {
		t.Fatalf("没有视频的分集应为 failed，得到 %s", got)
	}
	if err := f.media.Fail(ctx, jf.ID, "", "again"); !errors.Is(err, media.ErrJobGone) {
		t.Fatalf("已结束的任务不能再改，得到 %v", err)
	}
	// 失败后可以重新提交（分集回到 transcoding）
	must(f.svc.Enqueue(ctx, fresh.ID, media.EnqueueInput{}))(t)
	if got := f.episodeMedia(t, fresh.ID).Status; got != "transcoding" {
		t.Fatalf("重新提交后应为 transcoding，得到 %s", got)
	}

	// 已有视频的分集：重新转封装失败不影响旧产物；源文件没变时跳过
	jr := must(f.svc.Enqueue(ctx, ready.ID, media.EnqueueInput{}))(t)
	a = must(f.media.StartAttempt(ctx, jr.ID))(t)
	if !a.ShouldSkip("hr") || a.ShouldSkip("other") {
		t.Fatalf("源文件与当前产物相同应跳过：%+v", a)
	}
	if err := f.media.Skip(ctx, jr.ID, "hr"); err != nil {
		t.Fatal(err)
	}
	if got := must(f.media.GetJob(ctx, jr.ID))(t); got.Status != media.JobSkipped || got.Progress != 100 {
		t.Fatalf("跳过：%+v", got)
	}
	jr2 := must(f.svc.Enqueue(ctx, ready.ID, media.EnqueueInput{Force: true}))(t)
	must(f.media.StartAttempt(ctx, jr2.ID))(t)
	if err := f.media.Fail(ctx, jr2.ID, "", "源文件不是可识别的视频"); err != nil {
		t.Fatal(err)
	}
	if m := f.episodeMedia(t, ready.ID); m.Status != "ready" || m.KeyVer != 1 || m.SourceHash != "hr" {
		t.Fatalf("重新转封装失败时旧产物继续可播：%+v", m)
	}
}

func TestMediaRepo_StaleAndDeleted(t *testing.T) {
	f := newMediaFixture(t)
	ctx := context.Background()
	d := f.drama(t, "转封装超时", content.DramaInput{})
	ep := f.episode(t, d.ID, 1, 0)

	j := must(f.svc.Enqueue(ctx, ep.ID, media.EnqueueInput{SourceKey: "fixtures/s.mp4"}))(t)
	must(f.media.StartAttempt(ctx, j.ID))(t)
	// worker 被杀：进度停在 11 分钟前；没超时之前仍然占住这一集
	if _, err := f.svc.Enqueue(ctx, ep.ID, media.EnqueueInput{}); !errors.Is(err, media.ErrActive) {
		t.Fatalf("运行中的任务应占住这一集，得到 %v", err)
	}
	if _, err := f.repo.pool.Exec(ctx, `UPDATE transcode_job SET updated_at = now() - interval '11 minutes' WHERE id = $1`, pgUUID(j.ID)); err != nil {
		t.Fatal(err)
	}
	j2 := must(f.svc.Enqueue(ctx, ep.ID, media.EnqueueInput{}))(t)
	if got := must(f.media.GetJob(ctx, j.ID))(t); got.Status != media.JobFailed || got.Error != staleJobMessage {
		t.Fatalf("超时的任务应标为失败：%+v", got)
	}
	// 被标为失败的旧任务即使 worker 回来也不能再提交产物
	if err := f.media.Complete(ctx, j.ID, output(ep, j.ID, 1, "x")); !errors.Is(err, media.ErrJobGone) {
		t.Fatalf("已失败的任务不能切换产物，得到 %v", err)
	}

	// 转封装途中分集被删除：任务随之删除，worker 提交时得到 ErrJobGone，不写媒体对象
	must(f.media.StartAttempt(ctx, j2.ID))(t)
	must(f.contentFixture.svc.DeleteEpisode(ctx, ep.ID))(t)
	if err := f.media.Complete(ctx, j2.ID, output(ep, j2.ID, 1, "y")); !errors.Is(err, media.ErrJobGone) {
		t.Fatalf("分集已删除应为 ErrJobGone，得到 %v", err)
	}
	for name, err := range map[string]error{
		"重试": f.media.Retry(ctx, j2.ID, "", "x"),
		"失败": f.media.Fail(ctx, j2.ID, "", "x"),
		"跳过": f.media.Skip(ctx, j2.ID, "x"),
		"开始": second(f.media.StartAttempt(ctx, j2.ID)),
	} {
		if !errors.Is(err, media.ErrJobGone) {
			t.Errorf("任务已删除时%s应为 ErrJobGone，得到 %v", name, err)
		}
	}
	var n int
	if err := f.repo.pool.QueryRow(ctx, `SELECT count(*) FROM media_object WHERE key LIKE $1`, fmt.Sprintf("it-media/%d/%%", ep.ID)).Scan(&n); err != nil || n != 0 {
		t.Fatalf("不应写入媒体对象：%d %v", n, err)
	}
}
