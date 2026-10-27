package upload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	dom "xsite/service/internal/domain/upload"
	"xsite/service/internal/store/blob"
)

type imageStoreStub struct{ data map[string][]byte }

func (s imageStoreStub) PutStream(context.Context, string, io.Reader, int64, blob.PutOptions) error {
	return nil
}
func (s imageStoreStub) Head(_ context.Context, key string) (blob.ObjectInfo, error) {
	return blob.ObjectInfo{Key: key, Size: int64(len(s.data[key]))}, nil
}
func (s imageStoreStub) Get(_ context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error) {
	data := s.data[key]
	return io.NopCloser(bytes.NewReader(data)), blob.ObjectInfo{Key: key, Size: int64(len(data))}, nil
}
func (s imageStoreStub) Delete(_ context.Context, key string) error {
	delete(s.data, key)
	return nil
}

func TestVerifyImageChecksSelectedFormatMagic(t *testing.T) {
	store := imageStoreStub{data: map[string][]byte{
		"webp": []byte("RIFF1234WEBPpayload"),
		"avif": []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'a', 'v', 'i', 'f', 0, 0, 0, 0},
		"bad":  []byte("not-an-image"),
	}}
	for _, tc := range []struct {
		name  string
		asset dom.Asset
		want  bool
	}{
		{"webp", dom.Asset{SourceKey: "webp", ImageFormat: dom.FormatWebP}, true},
		{"avif", dom.Asset{SourceKey: "avif", ImageFormat: dom.FormatAVIF}, true},
		{"伪造 webp", dom.Asset{SourceKey: "bad", ImageFormat: dom.FormatWebP}, false},
		{"伪造 avif", dom.Asset{SourceKey: "bad", ImageFormat: dom.FormatAVIF}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyImage(context.Background(), store, tc.asset)
			if (err == nil) != tc.want {
				t.Fatalf("verifyImage() err = %v，want valid=%v", err, tc.want)
			}
		})
	}
}

// recordingStore 记录写入内容的对象存储桩，用来断言「哪一桶收到了什么」。
type recordingStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	// putErr 非 nil 时直接返回错误且完全不读 body，模拟「桶一上来就拒绝写入」。
	putErr error
	// failGet 为 true 时读取即失败，用来证明暂存已就位的路径不会再回主存储搬运文件。
	failGet bool
	gets    int
}

func newRecordingStore() *recordingStore {
	return &recordingStore{objects: map[string][]byte{}}
}

func (s *recordingStore) PutStream(_ context.Context, key string, body io.Reader, size int64, _ blob.PutOptions) error {
	if s.putErr != nil {
		return s.putErr
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	if int64(len(data)) != size {
		return fmt.Errorf("写入 %s 的长度 %d 与声明的 %d 不一致", key, len(data), size)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = data
	return nil
}

func (s *recordingStore) Head(_ context.Context, key string) (blob.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	if !ok {
		return blob.ObjectInfo{}, fmt.Errorf("blob: 查询 %s: %w", key, blob.ErrNotFound)
	}
	return blob.ObjectInfo{Key: key, Size: int64(len(data)), ContentType: "video/mp4"}, nil
}

func (s *recordingStore) Get(_ context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	if s.failGet {
		return nil, blob.ObjectInfo{}, errors.New("不应该从主存储读取")
	}
	data, ok := s.objects[key]
	if !ok {
		return nil, blob.ObjectInfo{}, fmt.Errorf("blob: 读取 %s: %w", key, blob.ErrNotFound)
	}
	return io.NopCloser(bytes.NewReader(data)), blob.ObjectInfo{Key: key, Size: int64(len(data)), ContentType: "video/mp4"}, nil
}

func (s *recordingStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *recordingStore) object(key string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.objects[key]
	return data, ok
}

func videoAsset(size int64) dom.Asset {
	return dom.Asset{ProviderID: 7, Purpose: dom.PurposeEpisodeVideo, Kind: dom.KindVideo,
		MIME: "video/mp4", SourceKey: "video/source/abc.mp4", SizeBytes: size}
}

// 视频源文件只写私有转码暂存桶：主存储里那一份没有读者，多付一次上行会把跨公网的上传拖垮。
func TestWriteContentSendsVideoOnlyToStage(t *testing.T) {
	payload := bytes.Repeat([]byte("xsite-video-payload"), 5000)
	primary, stage := newRecordingStore(), newRecordingStore()
	h := &Handlers{Source: stage}
	a := videoAsset(int64(len(payload)))

	if err := h.writeContent(context.Background(), primary, a, bytes.NewReader(payload)); err != nil {
		t.Fatalf("writeContent() err = %v", err)
	}

	staged, ok := stage.object(dom.StageKey(a.ProviderID, a.SourceKey))
	if !ok || !bytes.Equal(staged, payload) {
		t.Fatalf("暂存对象不正确：存在=%v 长度=%d", ok, len(staged))
	}
	if len(primary.objects) != 0 {
		t.Fatalf("视频源不应写入主存储，得到 %v", slices.Collect(maps.Keys(primary.objects)))
	}
}

// 没有配置暂存桶时视频上传必须明确失败，而不是退回去写主存储。
func TestWriteContentRejectsVideoWithoutStage(t *testing.T) {
	primary := newRecordingStore()
	h := &Handlers{}
	a := videoAsset(8)
	if err := h.writeContent(context.Background(), primary, a, bytes.NewReader([]byte("12345678"))); err == nil {
		t.Fatal("暂存桶未配置时应返回错误")
	}
	if len(primary.objects) != 0 {
		t.Fatalf("不应回退写入主存储，得到 %v", slices.Collect(maps.Keys(primary.objects)))
	}
}

// 图片要经 CDN 分发，仍然写配置的主存储。
func TestWriteContentKeepsImageOnPrimary(t *testing.T) {
	payload := []byte("RIFF1234WEBPpayload")
	primary, stage := newRecordingStore(), newRecordingStore()
	h := &Handlers{Source: stage}
	a := dom.Asset{ProviderID: 7, Purpose: dom.PurposeDramaCover, Kind: dom.KindImage,
		MIME: "image/webp", ImageFormat: dom.FormatWebP, SourceKey: "img/drama_cover/abc.webp", SizeBytes: int64(len(payload))}

	if err := h.writeContent(context.Background(), primary, a, bytes.NewReader(payload)); err != nil {
		t.Fatalf("writeContent() err = %v", err)
	}
	if _, ok := primary.object(a.SourceKey); !ok {
		t.Fatal("图片没有写入主存储")
	}
	if len(stage.objects) != 0 {
		t.Fatalf("图片不应写入转码暂存桶，得到 %v", slices.Collect(maps.Keys(stage.objects)))
	}
}

// 暂存桶写入失败时整次上传判失败。
func TestWriteContentFailsWhenStageRejects(t *testing.T) {
	payload := bytes.Repeat([]byte("video"), 20000)
	primary, stage := newRecordingStore(), newRecordingStore()
	stage.putErr = errors.New("暂存桶不可用")
	h := &Handlers{Source: stage}
	if err := h.writeContent(context.Background(), primary, videoAsset(int64(len(payload))), bytes.NewReader(payload)); err == nil {
		t.Fatal("暂存写入失败时 writeContent 应返回错误")
	}
}

// 暂存已在上传阶段写好：完成阶段只核对元数据，不再从主存储下载重传。
func TestStageVideoSkipsCopyWhenAlreadyStaged(t *testing.T) {
	payload := bytes.Repeat([]byte("v"), 4096)
	primary, stage := newRecordingStore(), newRecordingStore()
	primary.failGet = true
	a := videoAsset(int64(len(payload)))
	stage.objects[dom.StageKey(a.ProviderID, a.SourceKey)] = payload
	h := &Handlers{Source: stage}

	if err := h.stageVideo(context.Background(), primary, a); err != nil {
		t.Fatalf("stageVideo() err = %v", err)
	}
	if primary.gets != 0 {
		t.Fatalf("暂存已就位时不应读取主存储，实际读了 %d 次", primary.gets)
	}
}

// 历史会话（上传时还没有并行写暂存）仍然能在完成阶段补上一次复制。
func TestStageVideoCopiesWhenStageMissing(t *testing.T) {
	payload := bytes.Repeat([]byte("v"), 4096)
	primary, stage := newRecordingStore(), newRecordingStore()
	a := videoAsset(int64(len(payload)))
	primary.objects[a.SourceKey] = payload
	h := &Handlers{Source: stage}

	if err := h.stageVideo(context.Background(), primary, a); err != nil {
		t.Fatalf("stageVideo() err = %v", err)
	}
	staged, ok := stage.object(dom.StageKey(a.ProviderID, a.SourceKey))
	if !ok || !bytes.Equal(staged, payload) {
		t.Fatalf("回退复制没有写入暂存：存在=%v 长度=%d", ok, len(staged))
	}
}

// 预览代理按 key 直接读对象存储，所以这道校验是唯一的边界：
// 放过 img/ 之外的路径就等于把私有视频源、HLS 产物和内容密钥也变成后台可下载的东西。
func TestValidImageKeyOnlyAcceptsImagePrefix(t *testing.T) {
	for _, tc := range []struct {
		key  string
		want bool
	}{
		{"img/drama_cover/01a1-7abc.webp", true},
		{"img/posters/ab/cd/hash.webp", true},
		{"img/logo.webp", true},
		{"", false},
		{"video/source/01a1.mp4", false},
		{"/img/logo.webp", false},
		{"img/../video/source/01a1.mp4", false},
		{"img/./logo.webp", false},
		{"img//logo.webp", false},
		{"img/logo.webp?x=1", false},
		{"img/logo.webp#f", false},
		{`img\logo.webp`, false},
		{"img/logo\n.webp", false},
		{"https://cdn.example.com/img/logo.webp", false},
		{"img/" + strings.Repeat("a", imageKeyMaxLen), false},
	} {
		t.Run(tc.key, func(t *testing.T) {
			if got := validImageKey(tc.key); got != tc.want {
				t.Fatalf("validImageKey(%q) = %v，want %v", tc.key, got, tc.want)
			}
		})
	}
}
