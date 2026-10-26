package jobs

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"uuid"

	"github.com/riverqueue/river"

	"xsite/service/internal/domain/upload"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/uploadstore"
)

type uploadCleanupRepoStub struct {
	assets  []upload.Asset
	deleted []uuid.UUID
}

func (s *uploadCleanupRepoStub) ListExpired(context.Context, int) ([]upload.Asset, error) {
	return s.assets, nil
}
func (s *uploadCleanupRepoStub) DeleteExpired(_ context.Context, id uuid.UUID) error {
	s.deleted = append(s.deleted, id)
	return nil
}

type uploadDeleteStoreStub struct{ keys []string }

func (s *uploadDeleteStoreStub) PutStream(context.Context, string, io.Reader, int64, blob.PutOptions) error {
	return nil
}
func (s *uploadDeleteStoreStub) Head(context.Context, string) (blob.ObjectInfo, error) {
	return blob.ObjectInfo{}, nil
}
func (s *uploadDeleteStoreStub) Get(context.Context, string) (io.ReadCloser, blob.ObjectInfo, error) {
	return io.NopCloser(bytes.NewReader(nil)), blob.ObjectInfo{}, nil
}
func (s *uploadDeleteStoreStub) Delete(_ context.Context, key string) error {
	s.keys = append(s.keys, key)
	return nil
}

type uploadResolverStub struct {
	image blob.UploadStore
	video blob.UploadStore
}

func (s uploadResolverStub) Primary(context.Context, upload.Kind) (uploadstore.Target, error) {
	return uploadstore.Target{}, nil
}
func (s uploadResolverStub) Existing(_ context.Context, a upload.Asset) (uploadstore.Target, error) {
	return s.target(a.Kind)
}
func (s uploadResolverStub) Cleanup(_ context.Context, a upload.Asset) (uploadstore.Target, error) {
	return s.target(a.Kind)
}
func (s uploadResolverStub) target(kind upload.Kind) (uploadstore.Target, error) {
	if kind == upload.KindImage {
		return uploadstore.Target{ProviderID: 1, Store: s.image}, nil
	}
	return uploadstore.Target{ProviderID: 1, Store: s.video}, nil
}

func TestAdminMediaUploadCleanupDeletesOnlyUnreferencedObjects(t *testing.T) {
	now := time.Now().UTC()
	createdImage := upload.Asset{ID: uuid.NewV7(), ProviderID: 1, Purpose: upload.PurposeAdImage, Kind: upload.KindImage, Status: upload.StatusUploaded, SourceKey: "img/ad/a.webp", ExpiresAt: now}
	readyImage := upload.Asset{ID: uuid.NewV7(), ProviderID: 1, Purpose: upload.PurposeAdImage, Kind: upload.KindImage, Status: upload.StatusReady, SourceKey: "img/ad/ready.webp", ExpiresAt: now}
	readyAdVideo := upload.Asset{ID: uuid.NewV7(), ProviderID: 1, Purpose: upload.PurposeAdVideo, Kind: upload.KindVideo, Status: upload.StatusReady, SourceKey: "video/source/ad.mp4", ExpiresAt: now}
	readyEpisode := upload.Asset{ID: uuid.NewV7(), ProviderID: 1, Purpose: upload.PurposeEpisodeVideo, Kind: upload.KindVideo, Status: upload.StatusRunning, SourceKey: "video/source/episode.mp4", ExpiresAt: now}
	repo := &uploadCleanupRepoStub{assets: []upload.Asset{createdImage, readyImage, readyAdVideo, readyEpisode}}
	images, video, stage := &uploadDeleteStoreStub{}, &uploadDeleteStoreStub{}, &uploadDeleteStoreStub{}
	w := &AdminMediaUploadCleanupWorker{
		Repo: repo, Stores: uploadResolverStub{image: images, video: video}, Stage: stage,
	}
	if err := w.Work(context.Background(), &river.Job[AdminMediaUploadCleanupArgs]{}); err != nil {
		t.Fatal(err)
	}
	if len(images.keys) != 1 || images.keys[0] != createdImage.SourceKey {
		t.Fatalf("图片删除范围错误：%v", images.keys)
	}
	if len(video.keys) != 1 || video.keys[0] != readyAdVideo.SourceKey {
		t.Fatalf("provider 视频删除范围错误：%v", video.keys)
	}
	if len(stage.keys) != 1 || stage.keys[0] != upload.StageKey(readyAdVideo.ProviderID, readyAdVideo.SourceKey) {
		t.Fatalf("转码暂存删除范围错误：%v", stage.keys)
	}
	if len(repo.deleted) != 4 {
		t.Fatalf("应删除全部过期会话记录，得到 %d", len(repo.deleted))
	}
}
