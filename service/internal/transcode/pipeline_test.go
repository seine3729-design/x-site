package transcode

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"xsite/service/internal/store/blob"
)

// 集成测试：真实 FFmpeg（service 开发镜像自带）+ 内存对象存储；产物再交给 FFmpeg 按密钥解密并完整解码，
// 用另一套实现证明加密 HLS 是标准格式。本机没有 FFmpeg 时跳过。

// memStore 是内存中的对象存储，实现 ObjectStore 与 SourceStore；failAfter > 0 时第 failAfter 次写入失败。
type memStore struct {
	mu        sync.Mutex
	objs      map[string][]byte
	opts      map[string]blob.PutOptions
	puts      int
	failAfter int
}

func newMemStore() *memStore {
	return &memStore{objs: map[string][]byte{}, opts: map[string]blob.PutOptions{}}
}

func (m *memStore) PutBytes(_ context.Context, key string, data []byte, opt blob.PutOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	if m.failAfter > 0 && m.puts >= m.failAfter {
		return errors.New("模拟的存储故障")
	}
	m.objs[key], m.opts[key] = bytes.Clone(data), opt
	return nil
}

func (m *memStore) PutFile(ctx context.Context, key, p string, opt blob.PutOptions) error {
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	return m.PutBytes(ctx, key, data, opt)
}

func (m *memStore) DeletePrefix(_ context.Context, prefix string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k := range m.objs {
		if strings.HasPrefix(k, prefix) {
			delete(m.objs, k)
			n++
		}
	}
	return n, nil
}

func (m *memStore) Head(_ context.Context, key string) (blob.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objs[key]
	if !ok {
		return blob.ObjectInfo{}, blob.ErrNotFound
	}
	return blob.ObjectInfo{Key: key, Size: int64(len(data))}, nil
}

func (m *memStore) Get(ctx context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error) {
	info, err := m.Head(ctx, key)
	if err != nil {
		return nil, info, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return io.NopCloser(bytes.NewReader(m.objs[key])), info, nil
}

func (m *memStore) keys(prefix string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for k := range m.objs {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func (m *memStore) get(t *testing.T, key string) []byte {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objs[key]
	if !ok {
		t.Fatalf("对象 %s 不存在", key)
	}
	return data
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("没有 %s，跳过转封装集成测试（请用 ./dev.sh go test 在开发镜像中运行）", bin)
		}
	}
}

// makeSource 用 FFmpeg 生成测试源文件，返回内容。
func makeSource(t *testing.T, args ...string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "src.mp4")
	cmd := exec.Command("ffmpeg", append(append([]string{"-hide_banner", "-v", "error", "-y"}, args...), out)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("生成测试源文件失败：%v\n%s", err, b)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type fixture struct {
	src, media, images *memStore
	p                  *Pipeline
}

func newFixture(t *testing.T, source []byte) fixture {
	t.Helper()
	f := fixture{src: newMemStore(), media: newMemStore(), images: newMemStore()}
	f.src.objs["fixtures/t.mp4"] = source
	f.p = &Pipeline{FFmpeg: "ffmpeg", FFprobe: "ffprobe", Preset: "ultrafast", WorkDir: t.TempDir(), Source: f.src, Media: f.media, Images: f.images}
	return f
}

func (f fixture) run(t *testing.T, jobID string, episodeID int64, keyVer int32, key []byte) (Result, []int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ws, err := f.p.Fetch(ctx, jobID, "fixtures/t.mp4")
	if err != nil {
		return Result{}, nil, err
	}
	defer func() {
		_ = ws.Close()
		if _, err := os.Stat(ws.dir); !os.IsNotExist(err) {
			t.Errorf("临时目录应已删除：%v", err)
		}
	}()
	var mu sync.Mutex
	var progress []int
	res, err := f.p.Package(ctx, ws, Request{JobID: jobID, EpisodeID: episodeID, KeyVer: keyVer, Key: key}, func(p int) {
		mu.Lock()
		progress = append(progress, p)
		mu.Unlock()
	})
	return res, progress, err
}

var testKey = []byte("0123456789abcdef")

// checkOutput 校验 MPEG-TS 产物结构，返回各档清单名。
func checkOutput(t *testing.T, f fixture, res Result, keyVer int32) []string {
	t.Helper()
	master := string(f.media.get(t, res.Prefix+"/master.m3u8"))
	if sum := sha256.Sum256([]byte(master)); hex.EncodeToString(sum[:]) != res.MasterSHA256 {
		t.Fatal("MasterSHA256 应是主清单内容的哈希")
	}
	if !strings.Contains(master, "#EXT-X-VERSION:4") || strings.Contains(master, "#EXT-X-INDEPENDENT-SEGMENTS") {
		t.Fatalf("主清单应是 MPEG-TS profile：\n%s", master)
	}
	var variants []string
	sc := bufio.NewScanner(strings.NewReader(master))
	for sc.Scan() {
		if line := sc.Text(); line != "" && !strings.HasPrefix(line, "#") {
			variants = append(variants, line)
		}
	}
	if len(variants) != len(res.Renditions) {
		t.Fatalf("主清单 %d 档，结果 %d 档", len(variants), len(res.Renditions))
	}

	referenced := map[string]bool{res.Prefix + "/master.m3u8": true}
	for i, v := range variants {
		r := res.Renditions[i]
		if v != r.Name+".m3u8" || r.Bitrate <= 0 || r.PeakBitrate < r.Bitrate || !strings.HasPrefix(r.Codecs, "avc1.") || len(strings.Split(r.Codecs, ",")[0]) != 11 {
			t.Fatalf("第 %d 档不符合预期：%s %+v", i, v, r)
		}
		if !strings.HasSuffix(r.Codecs, ",mp4a.40.2") {
			t.Fatalf("新产物必须包含 AAC-LC：%+v", r)
		}
		if i > 0 && res.Renditions[i-1].Width*res.Renditions[i-1].Height >= r.Width*r.Height {
			t.Fatal("主清单应按分辨率从低到高排列（播放器从第一档起播）")
		}
		key := res.Prefix + "/" + v
		referenced[key] = true
		if opt := f.media.opts[key]; opt.ContentType != "application/vnd.apple.mpegurl" || opt.CacheControl != "no-cache" {
			t.Fatalf("清单响应头不对：%+v", opt)
		}
		text := string(f.media.get(t, key))
		if !strings.Contains(text, "#EXT-X-VERSION:4") || strings.Contains(text, "#EXT-X-MAP") {
			t.Fatalf("媒体清单应是无 init 段的 MPEG-TS：\n%s", text)
		}
		lines := strings.Split(strings.TrimSpace(text), "\n")
		ivs := map[string]bool{}
		for j, line := range lines {
			switch {
			case strings.HasPrefix(line, "#EXT-X-KEY:"):
				want := fmt.Sprintf(`#EXT-X-KEY:METHOD=AES-128,URI="xsite-key:%d",IV=0x`, keyVer)
				if !strings.HasPrefix(line, want) || len(line) != len(want)+32 {
					t.Fatalf("EXT-X-KEY 格式不对：%s", line)
				}
				iv := line[len(want):]
				if ivs[iv] {
					t.Fatal("每个切片的 IV 应不同")
				}
				ivs[iv] = true
				if j+2 >= len(lines) || !strings.HasPrefix(lines[j+1], "#EXTINF:") || !strings.HasPrefix(lines[j+2], "s/") {
					t.Fatalf("EXT-X-KEY 之后应是 EXTINF 与切片：%q", lines[j:])
				}
			case strings.HasPrefix(line, "s/"):
				seg := checkHashed(t, f.media, res.Prefix+"/"+line, ".ts", "video/mp2t")
				if len(seg)%16 != 0 || len(seg) < 16 {
					t.Fatal("切片应是 AES-128-CBC 密文")
				}
				referenced[res.Prefix+"/"+line] = true
			}
		}
		if len(ivs) == 0 {
			t.Fatal("清单中没有切片")
		}
	}

	// 产物目录下只有清单引用到的文件（重试不留半成品），总字节数与结果一致。
	var total int64
	for _, k := range f.media.keys(res.Prefix + "/") {
		if !referenced[k] {
			t.Errorf("产物目录中有未被引用的对象 %s", k)
		}
		total += int64(len(f.media.get(t, k)))
	}
	if total != res.SizeBytes {
		t.Errorf("SizeBytes = %d，实际 %d", res.SizeBytes, total)
	}

	poster := f.images.get(t, res.Poster)
	if !strings.HasPrefix(res.Poster, "img/posters/") || !bytes.HasPrefix(poster, []byte("RIFF")) || !bytes.Contains(poster[:16], []byte("WEBP")) {
		t.Fatalf("首帧图应为 WebP：%s", res.Poster)
	}
	if opt := f.images.opts[res.Poster]; opt.ContentType != "image/webp" || opt.CacheControl != immutable {
		t.Fatalf("首帧图响应头不对：%+v", opt)
	}
	return variants
}

// checkHashed 确认对象按内容哈希命名、响应头正确，返回内容。
func checkHashed(t *testing.T, s *memStore, key, ext, contentType string) []byte {
	t.Helper()
	data := s.get(t, key)
	if want := hashName(data) + ext; path.Base(key) != want {
		t.Fatalf("%s 应按内容哈希命名为 %s", key, want)
	}
	if opt := s.opts[key]; opt.ContentType != contentType || opt.CacheControl != immutable {
		t.Fatalf("%s 响应头不对：%+v", key, opt)
	}
	return data
}

// decodeHLS 把产物写到本地目录，把密钥占位符换成本地密钥文件，用 FFmpeg 解密并完整解码某一档，返回视频帧数。
func decodeHLS(t *testing.T, f fixture, res Result, keyVer int32, key []byte, variant string) int {
	t.Helper()
	dir := t.TempDir()
	for _, k := range f.media.keys(res.Prefix + "/") {
		data := f.media.get(t, k)
		if strings.HasSuffix(k, ".m3u8") {
			data = bytes.ReplaceAll(data, []byte(KeyURI(keyVer)), []byte("key.bin"))
		}
		p := filepath.Join(dir, strings.TrimPrefix(k, res.Prefix+"/"))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "key.bin"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("ffprobe", "-v", "error", "-allowed_extensions", "ALL", "-protocol_whitelist", "file,crypto,data",
		"-count_frames", "-select_streams", "v:0", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0",
		filepath.Join(dir, variant)).CombinedOutput()
	if err != nil {
		t.Fatalf("FFmpeg 解码 %s 失败：%v\n%s", variant, err, out)
	}
	// HLS 输入时 ffprobe 会在节目与流下各输出一次帧数
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		t.Fatalf("读不到帧数：%q", out)
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("读不到帧数：%q", out)
	}
	return n
}

func TestPipeline_EndToEnd(t *testing.T) {
	requireFFmpeg(t)
	source := makeSource(t, "-f", "lavfi", "-i", "testsrc2=size=720x1280:rate=30", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "5", "-ac", "2", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac")
	f := newFixture(t, source)

	res, progress, err := f.run(t, "job1", 7, 2, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if res.Prefix != "video/episodes/7/job1" || res.Duration < 4.9 || res.Duration > 5.1 {
		t.Fatalf("结果不符合预期：%+v", res)
	}
	got := make([]string, len(res.Renditions))
	for i, r := range res.Renditions {
		got[i] = fmt.Sprintf("%s:%dx%d", r.Name, r.Width, r.Height)
		if !strings.HasSuffix(r.Codecs, ",mp4a.40.2") {
			t.Fatalf("有音轨时 CODECS 应包含 AAC-LC：%s", r.Codecs)
		}
	}
	if !slices.Equal(got, []string{"v360:360x640", "v480:480x854", "v720:720x1280"}) {
		t.Fatalf("720p 源应出三档，得到 %v", got)
	}
	if len(progress) == 0 || slices.Max(progress) < 80 || slices.Max(progress) > 99 {
		t.Fatalf("进度应覆盖转码与上传阶段（≤ 99），得到 %v", progress)
	}
	variants := checkOutput(t, f, res, 2)
	for _, v := range variants {
		if n := decodeHLS(t, f, res, 2, testKey, v); n != 150 {
			t.Fatalf("%s 解密解码后应有 5 秒 × 30 帧，得到 %d", v, n)
		}
	}
}

func TestPipeline_SmallSilentSource(t *testing.T) {
	requireFFmpeg(t)
	// 比起播档还小、没有音轨、60fps（输出减半到 30fps）
	source := makeSource(t, "-f", "lavfi", "-i", "testsrc2=size=202x360:rate=60", "-t", "3", "-c:v", "libx264", "-preset", "ultrafast")
	f := newFixture(t, source)
	res, _, err := f.run(t, "job2", 8, 1, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Renditions) != 1 || res.Renditions[0].Name != "v202" || !strings.HasSuffix(res.Renditions[0].Codecs, ",mp4a.40.2") {
		t.Fatalf("应只出一档、不含音频：%+v", res.Renditions)
	}
	if master := string(f.media.get(t, res.Prefix+"/master.m3u8")); !strings.Contains(master, "FRAME-RATE=30.000") {
		t.Fatalf("60fps 源应输出 30fps：\n%s", master)
	}
	checkOutput(t, f, res, 1)
	if n := decodeHLS(t, f, res, 1, testKey, "v202.m3u8"); n != 90 {
		t.Fatalf("3 秒 × 30 帧，得到 %d", n)
	}
}

func TestPipeline_RetryLeavesNoPartialOutput(t *testing.T) {
	requireFFmpeg(t)
	source := makeSource(t, "-f", "lavfi", "-i", "testsrc2=size=360x640:rate=25", "-t", "4", "-c:v", "libx264", "-preset", "ultrafast")
	f := newFixture(t, source)

	f.media.failAfter = 3 // 第一次尝试上传到一半失败
	if _, _, err := f.run(t, "job3", 9, 1, testKey); err == nil || IsPermanent(err) {
		t.Fatalf("存储故障应返回可重试的错误，得到 %v", err)
	}
	if len(f.media.keys("video/episodes/9/job3/")) == 0 {
		t.Fatal("测试前提：第一次尝试应已上传部分产物")
	}

	f.media.failAfter, f.media.puts = 0, 0
	res, _, err := f.run(t, "job3", 9, 1, []byte("fedcba9876543210")) // 重试换了新密钥，旧的半成品必须清掉
	if err != nil {
		t.Fatal(err)
	}
	checkOutput(t, f, res, 1)
	if n := decodeHLS(t, f, res, 1, []byte("fedcba9876543210"), "v360.m3u8"); n != 100 {
		t.Fatalf("4 秒 × 25 帧，得到 %d", n)
	}

	if err := f.p.Discard(context.Background(), 9, "job3"); err != nil || len(f.media.keys("video/episodes/9/job3/")) != 0 {
		t.Fatalf("Discard 应删除整个产物目录：%v", err)
	}
}

func TestPipeline_PermanentErrors(t *testing.T) {
	requireFFmpeg(t)
	ctx := context.Background()

	f := newFixture(t, []byte("not a video"))
	if _, err := f.p.Fetch(ctx, "j", "fixtures/missing.mp4"); !IsPermanent(err) {
		t.Fatalf("源文件不存在应为不可恢复错误，得到 %v", err)
	}
	f.src.objs["fixtures/empty.mp4"] = nil
	if _, err := f.p.Fetch(ctx, "j", "fixtures/empty.mp4"); !IsPermanent(err) {
		t.Fatalf("空文件应为不可恢复错误，得到 %v", err)
	}

	// 不是视频；伪装成播放列表、试图读取 worker 本地文件的源文件被格式白名单拒绝
	for name, data := range map[string][]byte{
		"文本":   []byte("not a video"),
		"播放列表": []byte("#EXTM3U\n#EXT-X-TARGETDURATION:1\n#EXTINF:1,\nfile:///etc/hostname\n#EXT-X-ENDLIST\n"),
		"拼接列表": []byte("ffconcat version 1.0\nfile '/etc/hostname'\n"),
	} {
		f := newFixture(t, data)
		_, _, err := f.run(t, "j", 1, 1, testKey)
		if !IsPermanent(err) {
			t.Errorf("%s：应为不可恢复错误，得到 %v", name, err)
		}
		if err != nil && strings.Contains(f.p.PublicError(err), f.p.WorkDir) {
			t.Errorf("%s：后台看到的错误不应包含临时目录：%s", name, f.p.PublicError(err))
		}
		if len(f.media.keys("")) != 0 {
			t.Errorf("%s：失败时不应上传产物", name)
		}
	}
}
