package collector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/domain/upload"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/uploadstore"
)

type fakeFetcher struct {
	responses map[string]fakeResponse
	requests  []string
}

type fakeResponse struct {
	body []byte
	mime string
}

func (f *fakeFetcher) Fetch(_ context.Context, rawURL string, _ int64) (Fetched, error) {
	f.requests = append(f.requests, rawURL)
	response, ok := f.responses[rawURL]
	if !ok {
		return Fetched{}, badRequest("未准备测试响应")
	}
	return Fetched{Body: io.NopCloser(bytes.NewReader(response.body)), Size: int64(len(response.body)), MIME: response.mime}, nil
}

type fakeUploadStore struct {
	objects map[string][]byte
	deleted []string
	putErr  error
}

func (s *fakeUploadStore) PutStream(_ context.Context, key string, body io.Reader, size int64, _ blob.PutOptions) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return badRequest("测试对象大小错误")
	}
	s.objects[key] = data // 模拟底层写入后才返回错误的最坏情况。
	return s.putErr
}
func (s *fakeUploadStore) Head(_ context.Context, key string) (blob.ObjectInfo, error) {
	return blob.ObjectInfo{Key: key, Size: int64(len(s.objects[key]))}, nil
}
func (s *fakeUploadStore) Get(_ context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error) {
	data := s.objects[key]
	return io.NopCloser(bytes.NewReader(data)), blob.ObjectInfo{Key: key, Size: int64(len(data))}, nil
}
func (s *fakeUploadStore) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	s.deleted = append(s.deleted, key)
	return nil
}

type fakeResolver struct {
	images *fakeUploadStore
	videos *fakeUploadStore
}

func (r fakeResolver) Primary(_ context.Context, kind upload.Kind) (uploadstore.Target, error) {
	return r.target(kind), nil
}
func (r fakeResolver) Cleanup(_ context.Context, a upload.Asset) (uploadstore.Target, error) {
	return r.target(a.Kind), nil
}
func (r fakeResolver) target(kind upload.Kind) uploadstore.Target {
	store := blob.UploadStore(r.videos)
	bucket := "videos"
	if kind == upload.KindImage {
		store, bucket = r.images, "images"
	}
	return uploadstore.Target{ProviderID: 9, Endpoint: "https://storage.example.test", Region: "auto", Bucket: bucket, Store: store}
}

func collectorFixture() (Manifest, *fakeFetcher) {
	manifest := validManifest()
	fetcher := &fakeFetcher{responses: map[string]fakeResponse{
		"https://example.test/robots.txt":      {body: []byte("User-agent: xsite-test-collector\nAllow: /\n"), mime: "text/plain"},
		"https://example.test/covers/one.webp": {body: []byte("RIFF1234WEBPcover"), mime: "image/webp"},
		"https://example.test/videos/one.mp4":  {body: []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0}, mime: "video/mp4"},
	}}
	return manifest, fetcher
}

func TestCollectStagesCoverAndVideoToSeparatePrimaryStores(t *testing.T) {
	manifest, fetcher := collectorFixture()
	images := &fakeUploadStore{objects: map[string][]byte{}}
	videos := &fakeUploadStore{objects: map[string][]byte{}}
	service := NewService(fakeResolver{images: images, videos: videos}, fetcher, Options{Now: time.Now})
	result, err := service.Collect(context.Background(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Objects) != 2 || len(result.Dramas) != 1 {
		t.Fatalf("暂存结果错误：%+v", result)
	}
	if len(images.objects) != 1 || len(videos.objects) != 1 {
		t.Fatalf("图片/视频没有分流到主存储：images=%v videos=%v", images.objects, videos.objects)
	}
	if !strings.HasPrefix(result.Dramas[0].CoverKey, "img/collector/"+result.RunID+"/") || !strings.HasPrefix(result.Dramas[0].Episodes[0].SourceKey, "video/source/collector/"+result.RunID+"/") {
		t.Fatalf("对象 key 前缀错误：%+v", result.Dramas[0])
	}
	if len(fetcher.requests) != 3 {
		t.Fatalf("应依次取 robots、封面和视频，得到 %v", fetcher.requests)
	}
}

func TestCollectCleansStagedObjectsWhenRobotsRejectsVideo(t *testing.T) {
	manifest, fetcher := collectorFixture()
	fetcher.responses["https://example.test/robots.txt"] = fakeResponse{body: []byte("User-agent: xsite-test-collector\nAllow: /covers\nDisallow: /videos\n"), mime: "text/plain"}
	images := &fakeUploadStore{objects: map[string][]byte{}}
	videos := &fakeUploadStore{objects: map[string][]byte{}}
	service := NewService(fakeResolver{images: images, videos: videos}, fetcher, Options{Now: time.Now})
	if _, err := service.Collect(context.Background(), manifest); err == nil {
		t.Fatal("robots 拒绝视频时应失败")
	}
	if len(images.objects) != 0 || len(videos.objects) != 0 || len(images.deleted) != 1 {
		t.Fatalf("失败后必须清理已写对象：images=%v videos=%v deleted=%v", images.objects, videos.objects, images.deleted)
	}
}

func TestCollectDeletesObjectWhenPutFailsAfterWriting(t *testing.T) {
	manifest, fetcher := collectorFixture()
	images := &fakeUploadStore{objects: map[string][]byte{}, putErr: errors.New("模拟存储断开")}
	videos := &fakeUploadStore{objects: map[string][]byte{}}
	service := NewService(fakeResolver{images: images, videos: videos}, fetcher, Options{Now: time.Now})
	if _, err := service.Collect(context.Background(), manifest); err == nil {
		t.Fatal("写入失败必须返回错误")
	}
	if len(images.objects) != 0 || len(images.deleted) != 1 || len(videos.objects) != 0 {
		t.Fatalf("写入失败后对象必须补偿删除：images=%v deleted=%v videos=%v", images.objects, images.deleted, videos.objects)
	}
}

func TestCleanupRejectsObjectOutsideCollectorPrefix(t *testing.T) {
	store := &fakeUploadStore{objects: map[string][]byte{"img/other/key.webp": []byte("x")}}
	service := NewService(fakeResolver{images: store, videos: store}, &fakeFetcher{}, Options{Now: time.Now})
	err := service.Cleanup(context.Background(), Result{RunID: "run-1", Objects: []StoredObject{{Kind: upload.KindImage, Key: "img/other/key.webp", ProviderID: 1}}})
	if err == nil {
		t.Fatal("cleanup 不能删除采集前缀外的对象")
	}
	if len(store.deleted) != 0 {
		t.Fatal("不应删除非采集对象")
	}
}
