package play

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/crypto/playtoken"
	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
)

type previewRepoStub struct {
	ep  Episode
	err error
}

func (r previewRepoStub) PlayableEpisode(context.Context, int64) (Episode, error) {
	return r.ep, r.err
}

type previewLinesStub struct {
	sel storage.PlaybackSelection
	err error
}

func (l previewLinesStub) ForPlayback(_ context.Context, in storage.PlaybackInput) (storage.PlaybackSelection, error) {
	return l.sel, l.err
}

type previewSignerStub struct{ last playtoken.Claims }

func (s *previewSignerStub) Sign(c playtoken.Claims) (string, error) {
	s.last = c
	return "p1.body.sig", nil
}

func previewFixture(lines []storage.PlaybackLine) (*PreviewService, *previewSignerStub) {
	signer := &previewSignerStub{}
	// 试看 30 秒的 VIP 分集：后台试播要无视这两项，给完整时长
	repo := previewRepoStub{ep: Episode{ID: 2817, DramaID: 9, Access: content.AccessVIP, TrialSec: 30, VideoObjectID: 802, ObjectKey: "video/episodes/2817/x"}}
	svc := NewPreviewService(repo, previewLinesStub{sel: storage.PlaybackSelection{Lines: lines}}, signer)
	svc.now = func() time.Time { return time.Unix(1_793_000_000, 0).UTC() }
	return svc, signer
}

func TestPreview_GrantFullLengthToken(t *testing.T) {
	svc, signer := previewFixture([]storage.PlaybackLine{{LineID: 472, Name: "R2-CDN"}, {LineID: 370, Name: "本地"}})
	got, err := svc.Grant(context.Background(), 2817)
	if err != nil {
		t.Fatalf("Grant: %v", err)
	}
	// 取打分最高的那条线路
	if got.LineID != 472 || got.LineName != "R2-CDN" {
		t.Errorf("应取第一条线路，得到 %+v", got)
	}
	if got.EpisodeID != 2817 || got.DramaID != 9 {
		t.Errorf("分集信息不对：%+v", got)
	}
	if !strings.HasPrefix(got.URL, "/api/admin/preview/") || !strings.HasSuffix(got.URL, "/index.m3u8") {
		t.Errorf("清单地址应是后台路由：%q", got.URL)
	}
	if strings.Contains(got.URL, "/api/v1/") {
		t.Errorf("不能指向前台加密接口：%q", got.URL)
	}
	// 后台试播看完整的一集：不裁剪试看、不绑设备
	if signer.last.TrialSec != 0 {
		t.Errorf("试看秒数应为 0（完整时长），得到 %d", signer.last.TrialSec)
	}
	if signer.last.Device != "" {
		t.Errorf("后台没有设备标识，不该绑定：%q", signer.last.Device)
	}
	if signer.last.EpisodeID != 2817 || signer.last.LineID != 472 {
		t.Errorf("令牌内容不对：%+v", signer.last)
	}
	if want := svc.now().Add(PreviewTokenTTL); !got.ExpiresAt.Equal(want) {
		t.Errorf("过期时间 = %v，期望 %v", got.ExpiresAt, want)
	}
}

func TestPreview_NoLines(t *testing.T) {
	svc, _ := previewFixture(nil)
	_, err := svc.Grant(context.Background(), 2817)
	if !errors.Is(err, ErrPreviewNoLines) {
		t.Fatalf("应提示去配置线路，得到 %v", err)
	}
	if !strings.Contains(err.Error(), "线路") {
		t.Errorf("提示要点出线路：%v", err)
	}
}

func TestPreview_EpisodeNotFound(t *testing.T) {
	signer := &previewSignerStub{}
	svc := NewPreviewService(previewRepoStub{err: errs.ErrNotFound}, previewLinesStub{}, signer)
	if _, err := svc.Grant(context.Background(), 1); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("应 404，得到 %v", err)
	}
}
