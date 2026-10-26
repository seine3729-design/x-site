package upload

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

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

// recordingStore 记录写入内容的对象存储桩。
// PutStream 必须真正把 body 读完：视频双写用 io.Pipe 把主存储与暂存串在一起，
// 任何一方不读都会把另一方阻塞住，这正是这些用例要盯住的行为。
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

// within 保证用例不会被双写死锁挂住，而是明确报出来。
func within(t *testing.T, fn func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("写入 5 秒内没有返回：两路写入互相卡住了")
		return nil
	}
}

func videoAsset(size int64) dom.Asset {
	return dom.Asset{ProviderID: 7, Purpose: dom.PurposeEpisodeVideo, Kind: dom.KindVideo,
		MIME: "video/mp4", SourceKey: "video/source/abc.mp4", SizeBytes: size}
}

// 视频在接收请求体的同时写入私有转码暂存桶：完成阶段不必再把整个源文件搬一遍。
func TestWriteContentStagesVideoWhileReceiving(t *testing.T) {
	payload := bytes.Repeat([]byte("xsite-video-payload"), 5000)
	primary, stage := newRecordingStore(), newRecordingStore()
	h := &Handlers{Source: stage}
	a := videoAsset(int64(len(payload)))

	if err := within(t, func() error {
		return h.writeContent(context.Background(), primary, a, bytes.NewReader(payload))
	}); err != nil {
		t.Fatalf("writeContent() err = %v", err)
	}

	got, ok := primary.object(a.SourceKey)
	if !ok || !bytes.Equal(got, payload) {
		t.Fatalf("主存储对象不正确：存在=%v 长度=%d", ok, len(got))
	}
	staged, ok := stage.object(dom.StageKey(a.ProviderID, a.SourceKey))
	if !ok || !bytes.Equal(staged, payload) {
		t.Fatalf("暂存对象不正确：存在=%v 长度=%d", ok, len(staged))
	}
}

// 图片没有转码环节，只写主存储。
func TestWriteContentKeepsImageSingleWrite(t *testing.T) {
	payload := []byte("RIFF1234WEBPpayload")
	primary, stage := newRecordingStore(), newRecordingStore()
	h := &Handlers{Source: stage}
	a := dom.Asset{ProviderID: 7, Purpose: dom.PurposeDramaCover, Kind: dom.KindImage,
		MIME: "image/webp", ImageFormat: dom.FormatWebP, SourceKey: "img/drama_cover/abc.webp", SizeBytes: int64(len(payload))}

	if err := within(t, func() error {
		return h.writeContent(context.Background(), primary, a, bytes.NewReader(payload))
	}); err != nil {
		t.Fatalf("writeContent() err = %v", err)
	}
	if _, ok := primary.object(a.SourceKey); !ok {
		t.Fatal("图片没有写入主存储")
	}
	if len(stage.objects) != 0 {
		t.Fatalf("图片不应写入转码暂存桶，得到 %v", stage.objects)
	}
}

// 任一路写入失败都要立刻把整次上传判失败，并且不能让另一路卡在等数据上。
func TestWriteContentFailsFastWhenEitherTargetRejects(t *testing.T) {
	payload := bytes.Repeat([]byte("video"), 20000)
	for _, tc := range []struct {
		name            string
		primary, staged error
	}{
		{"主存储拒绝", errors.New("主存储不可用"), nil},
		{"暂存桶拒绝", nil, errors.New("暂存桶不可用")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary, stage := newRecordingStore(), newRecordingStore()
			primary.putErr, stage.putErr = tc.primary, tc.staged
			h := &Handlers{Source: stage}
			err := within(t, func() error {
				return h.writeContent(context.Background(), primary, videoAsset(int64(len(payload))), bytes.NewReader(payload))
			})
			if err == nil {
				t.Fatal("一路写入失败时 writeContent 应返回错误")
			}
		})
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
