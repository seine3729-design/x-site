package upload

import (
	"context"
	"errors"
	"testing"
	"time"

	"uuid"
)

type fakeRepo struct{ asset Asset }

func (f *fakeRepo) Create(_ context.Context, a Asset) (Asset, error)  { f.asset = a; return a, nil }
func (f *fakeRepo) Get(_ context.Context, _ uuid.UUID) (Asset, error) { return f.asset, nil }
func (f *fakeRepo) MarkUploaded(_ context.Context, _ uuid.UUID, _ int64) error {
	f.asset.Status = StatusUploaded
	return nil
}
func (f *fakeRepo) CompleteImage(_ context.Context, _ uuid.UUID, _ int64) error {
	f.asset.Status = StatusReady
	return nil
}
func (f *fakeRepo) LinkEpisodeTranscode(_ context.Context, _ uuid.UUID, _ int64, id uuid.UUID) error {
	f.asset.Status, f.asset.TranscodeJobID = StatusRunning, &id
	return nil
}
func (f *fakeRepo) QueueAdVideo(_ context.Context, _ uuid.UUID) error {
	f.asset.Status = StatusQueued
	return nil
}
func (f *fakeRepo) StartAdVideo(_ context.Context, _ uuid.UUID) (Asset, error)        { return f.asset, nil }
func (f *fakeRepo) UpdateAdVideoProgress(_ context.Context, _ uuid.UUID, _ int) error { return nil }
func (f *fakeRepo) RequeueAdVideo(_ context.Context, _ uuid.UUID, _ string) error     { return nil }
func (f *fakeRepo) CompleteAdVideo(_ context.Context, _ uuid.UUID, _ string) error    { return nil }
func (f *fakeRepo) FailAdVideo(_ context.Context, _ uuid.UUID, _ string) error        { return nil }

func newServiceForTest(repo Repository) *Service {
	s := NewService(repo)
	s.now = func() time.Time { return time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC) }
	return s
}

func TestInitPlacesImagesAndVideosUnderDedicatedPrefixes(t *testing.T) {
	repo := &fakeRepo{}
	s := newServiceForTest(repo)
	image, err := s.Init(context.Background(), 7, InitInput{ProviderID: 1, ProviderBucket: "images", Purpose: PurposeDramaCover, MIME: "image/webp", ImageFormat: FormatWebP, SizeBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if image.Kind != KindImage || image.SourceKey != image.OutputKey || image.SourceKey[:4] != "img/" || image.ImageFormat != FormatWebP {
		t.Fatalf("图片 key 或格式错误：%+v", image)
	}
	video, err := s.Init(context.Background(), 7, InitInput{ProviderID: 1, ProviderBucket: "videos", Purpose: PurposeEpisodeVideo, MIME: "video/mp4", SizeBytes: 1024, EpisodeID: 11})
	if err != nil {
		t.Fatal(err)
	}
	if video.Kind != KindVideo || video.SourceKey[:6] != "video/" || video.EpisodeID != 11 {
		t.Fatalf("视频 key 或分集错误：%+v", video)
	}
}

func TestInitRejectsWrongFormatAndPurpose(t *testing.T) {
	s := newServiceForTest(&fakeRepo{})
	cases := []InitInput{
		{Purpose: PurposeDramaCover, MIME: "image/jpeg", ImageFormat: FormatWebP, SizeBytes: 1},
		{Purpose: PurposeDramaCover, MIME: "image/avif", ImageFormat: FormatWebP, SizeBytes: 1},
		{Purpose: PurposeEpisodeVideo, MIME: "video/mp4", SizeBytes: 1},
		{Purpose: PurposeAdVideo, MIME: "image/webp", SizeBytes: 1},
	}
	for _, in := range cases {
		if _, err := s.Init(context.Background(), 1, in); err == nil {
			t.Fatalf("%+v 应被拒绝", in)
		}
	}
	if _, err := s.Init(context.Background(), 0, InitInput{ProviderID: 1, Purpose: PurposeAdImage, MIME: "image/webp", ImageFormat: FormatWebP, SizeBytes: 1}); err == nil {
		t.Fatal("无管理员身份应拒绝")
	}
}

func TestCompleteImageRejectsExpiredSession(t *testing.T) {
	repo := &fakeRepo{asset: Asset{
		ID: uuid.NewV7(), AdminID: 9, Purpose: PurposeAdImage, Kind: KindImage, Status: StatusUploaded,
		ExpiresAt: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC),
	}}
	s := newServiceForTest(repo)
	if _, err := s.CompleteImage(context.Background(), repo.asset.ID, 9); err == nil {
		t.Fatal("过期会话不能完成图片上传")
	}
	if repo.asset.Status != StatusUploaded {
		t.Fatalf("过期会话不应改变状态：%s", repo.asset.Status)
	}
}

func (f *fakeRepo) ClaimWrite(_ context.Context, _ uuid.UUID, _ int64) error {
	if f.asset.Status != StatusCreated {
		return errors.New("会话已经被领取")
	}
	f.asset.Status = StatusUploading
	return nil
}
func (f *fakeRepo) Fail(_ context.Context, _ uuid.UUID, _ int64, _ string) error {
	f.asset.Status = StatusFailed
	return nil
}
func (f *fakeRepo) ListExpired(_ context.Context, _ int) ([]Asset, error) { return nil, nil }
func (f *fakeRepo) DeleteExpired(_ context.Context, _ uuid.UUID) error    { return nil }

func TestClaimWriteTransitionsToUploading(t *testing.T) {
	repo := &fakeRepo{asset: Asset{
		ID: uuid.NewV7(), AdminID: 7, Purpose: PurposeAdImage, Kind: KindImage, Status: StatusCreated,
		ExpiresAt: time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC),
	}}
	s := newServiceForTest(repo)
	a, err := s.ClaimWrite(context.Background(), repo.asset.ID, 7)
	if err != nil || a.Status != StatusUploading || repo.asset.Status != StatusUploading {
		t.Fatalf("领取上传会话失败：asset=%+v err=%v", a, err)
	}
	if _, err := s.ClaimWrite(context.Background(), repo.asset.ID, 7); err == nil {
		t.Fatal("同一会话不能被第二个 PUT 再次领取")
	}
}
