package product

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/domain/play"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
)

// 读取产物清单（S1-8）：按就绪副本逐个存储源尝试、HTTP 源跳过、没有副本时读默认存储、大小上限与读穿缓存。

type fakeProviders map[int64]storage.Row

func (f fakeProviders) Get(_ context.Context, id int64) (storage.Row, error) {
	r, ok := f[id]
	if !ok {
		return storage.Row{}, errs.ErrNotFound
	}
	return r, nil
}

type fakeCreds struct{}

func (fakeCreds) ReadCredential(r storage.Row) (storage.Credential, error) {
	if r.Name == "no-cred" {
		return storage.Credential{}, errors.New("尚未配置凭据")
	}
	return storage.Credential{AccessKeyID: "ak-" + r.Name, SecretAccessKey: "sk"}, nil
}

// fakeBucket 是内存里的桶，记录读取过的键。
type fakeBucket struct {
	objects map[string]string
	size    int64 // 非 0 时作为 Get 返回的 Content-Length
	gets    []string
}

func (b *fakeBucket) Get(_ context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error) {
	b.gets = append(b.gets, key)
	data, ok := b.objects[key]
	if !ok {
		return nil, blob.ObjectInfo{}, blob.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(data)), blob.ObjectInfo{Key: key, Size: b.size}, nil
}

// memCache 模拟读穿缓存（同样经过 JSON 编码）。
type memCache struct {
	data  map[string][]byte
	loads int
}

func (c *memCache) GetOrLoadJSON(ctx context.Context, key string, _ time.Duration, dst any, load func(context.Context) (any, error)) error {
	if raw, ok := c.data[key]; ok {
		return json.Unmarshal(raw, dst)
	}
	c.loads++
	v, err := load(ctx)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(v)
	c.data[key] = raw
	return json.Unmarshal(raw, dst)
}

type fixture struct {
	reader   *Reader
	buckets  map[string]*fakeBucket // 按存储源名称
	fallback *fakeBucket
	opened   []storage.Credential
}

const objKey = "hls/42/j1"

func newFixture(t *testing.T, withFallback bool, c Cache) *fixture {
	t.Helper()
	f := &fixture{buckets: map[string]*fakeBucket{
		"p3": {objects: map[string]string{}},
		"p4": {objects: map[string]string{"media/" + objKey + "/master.m3u8": "#EXTM3U\n# p4\n"}},
	}}
	providers := fakeProviders{
		3:  {ID: 3, Name: "p3", Type: storage.TypeS3, Bucket: "p3"},
		4:  {ID: 4, Name: "p4", Type: storage.TypeR2, Bucket: "p4", PathPrefix: "media"},
		5:  {ID: 5, Name: "origin", Type: storage.TypeCustom, Subtype: storage.SubtypeHTTPOrigin},
		6:  {ID: 6, Name: "no-cred", Type: storage.TypeS3, Bucket: "p3"},
		10: {ID: 10, Name: "p10", Type: storage.TypeS3, Bucket: "p10"},
	}
	var fallback ObjectGetter
	if withFallback {
		f.fallback = &fakeBucket{objects: map[string]string{objKey + "/master.m3u8": "#EXTM3U\n# default\n"}}
		fallback = f.fallback
	}
	f.reader = NewReader(providers, fakeCreds{}, netguard.Policy{}, fallback, c)
	f.reader.open = func(conn storage.Conn, cred storage.Credential) (ObjectGetter, error) {
		f.opened = append(f.opened, cred)
		b, ok := f.buckets[conn.Bucket]
		if !ok {
			return nil, errors.New("连接失败")
		}
		return b, nil
	}
	return f
}

func TestReadFile_TriesProvidersInOrder(t *testing.T) {
	f := newFixture(t, true, nil)
	// 3 上没有文件（复制中被删等），换 4；4 有 path_prefix
	got, err := f.reader.ReadFile(context.Background(), play.Product{ObjectID: 1, Key: objKey, Providers: []int64{3, 4}}, "master.m3u8")
	if err != nil || string(got) != "#EXTM3U\n# p4\n" {
		t.Fatalf("应从第二个存储源读到：%q %v", got, err)
	}
	if g := f.buckets["p3"].gets; len(g) != 1 || g[0] != objKey+"/master.m3u8" {
		t.Fatalf("先试第一个存储源：%v", g)
	}
	if g := f.buckets["p4"].gets; len(g) != 1 || g[0] != "media/"+objKey+"/master.m3u8" {
		t.Fatalf("对象键要拼上存储源的 path_prefix：%v", g)
	}
	if len(f.fallback.gets) != 0 {
		t.Fatal("有副本时不读默认存储")
	}
	if f.opened[0].AccessKeyID != "ak-p3" || f.opened[1].AccessKeyID != "ak-p4" {
		t.Fatalf("用各存储源自己的只读凭据：%+v", f.opened)
	}

	// HTTP 源、没有凭据、连不上、不存在的存储源都跳过，全部失败时报错并带上每个存储源的原因
	_, err = f.reader.ReadFile(context.Background(), play.Product{Key: objKey, Providers: []int64{5, 6, 10, 99}}, "master.m3u8")
	if err == nil {
		t.Fatal("全部失败时应报错")
	}
	for _, want := range []string{"存储源 5", "HTTP 源", "存储源 6", "存储源 10", "存储源 99"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("错误信息应包含 %q：%v", want, err)
		}
	}
}

func TestReadFile_DefaultStorage(t *testing.T) {
	f := newFixture(t, true, nil)
	got, err := f.reader.ReadFile(context.Background(), play.Product{Key: objKey}, "master.m3u8")
	if err != nil || string(got) != "#EXTM3U\n# default\n" {
		t.Fatalf("没有副本记录时读默认存储：%q %v", got, err)
	}
	if _, err := f.reader.ReadFile(context.Background(), play.Product{Key: objKey}, "v720.m3u8"); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("默认存储没有该文件：%v", err)
	}

	f = newFixture(t, false, nil)
	if _, err := f.reader.ReadFile(context.Background(), play.Product{Key: objKey}, "master.m3u8"); err == nil {
		t.Fatal("没有配置默认存储时，没有副本的产物读不到")
	}
}

func TestReadFile_RejectsBadInput(t *testing.T) {
	f := newFixture(t, true, nil)
	for _, name := range []string{"", "../x.m3u8", "s/a.m4s", "a/b.m3u8", "master.m3u8?x", "key.bin"} {
		if _, err := f.reader.ReadFile(context.Background(), play.Product{Key: objKey}, name); err == nil {
			t.Errorf("%q：应拒绝", name)
		}
	}
	if _, err := f.reader.ReadFile(context.Background(), play.Product{}, "master.m3u8"); err == nil {
		t.Error("没有产物目录时应拒绝")
	}
	if len(f.fallback.gets) != 0 {
		t.Fatal("参数不对时不应读取存储")
	}
}

func TestReadFile_SizeLimit(t *testing.T) {
	f := newFixture(t, true, nil)
	big := strings.Repeat("#", MaxFileSize+1)
	f.fallback.objects[objKey+"/v720.m3u8"] = big
	// 没有 Content-Length 时按实际读到的字节数判断
	if _, err := f.reader.ReadFile(context.Background(), play.Product{Key: objKey}, "v720.m3u8"); err == nil {
		t.Fatal("超过上限应拒绝")
	}
	// 有 Content-Length 时不读内容直接拒绝
	f.fallback.size = MaxFileSize + 1
	f.fallback.objects[objKey+"/v360.m3u8"] = "#EXTM3U\n"
	if _, err := f.reader.ReadFile(context.Background(), play.Product{Key: objKey}, "v360.m3u8"); err == nil {
		t.Fatal("声明的大小超过上限应拒绝")
	}
	// 正好等于上限可以
	f.fallback.size = 0
	f.fallback.objects[objKey+"/v480.m3u8"] = big[:MaxFileSize]
	if got, err := f.reader.ReadFile(context.Background(), play.Product{Key: objKey}, "v480.m3u8"); err != nil || len(got) != MaxFileSize {
		t.Fatalf("等于上限应能读取：%d %v", len(got), err)
	}
}

func TestReadFile_Cache(t *testing.T) {
	c := &memCache{data: map[string][]byte{}}
	f := newFixture(t, true, c)
	p := play.Product{Key: objKey}
	for range 3 {
		got, err := f.reader.ReadFile(context.Background(), p, "master.m3u8")
		if err != nil || string(got) != "#EXTM3U\n# default\n" {
			t.Fatalf("读取失败：%q %v", got, err)
		}
	}
	if c.loads != 1 || len(f.fallback.gets) != 1 {
		t.Fatalf("同一文件只回源一次：回源 %d 次，读存储 %d 次", c.loads, len(f.fallback.gets))
	}
	if _, ok := c.data[objKey+"/master.m3u8"]; !ok {
		t.Fatalf("缓存键应是产物内的完整路径：%v", c.data)
	}
	// 读不到时不缓存（下次重新回源）
	for range 2 {
		if _, err := f.reader.ReadFile(context.Background(), p, "v720.m3u8"); err == nil {
			t.Fatal("应报错")
		}
	}
	if c.loads != 3 {
		t.Fatalf("失败结果不应缓存：回源 %d 次", c.loads)
	}
}

func TestReadFile_ContextCanceled(t *testing.T) {
	f := newFixture(t, true, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.reader.open = func(storage.Conn, storage.Credential) (ObjectGetter, error) { return nil, ctx.Err() }
	_, err := f.reader.ReadFile(ctx, play.Product{Key: objKey, Providers: []int64{3, 4}}, "master.m3u8")
	if !errors.Is(err, context.Canceled) || len(f.buckets["p4"].gets) != 0 {
		t.Fatalf("请求取消后不再尝试其他存储源：%v", err)
	}
}

// 真实读取本地 compose 的 SeaweedFS（默认存储）；未配置 S3_* 时跳过。
func TestReadFile_SeaweedFS(t *testing.T) {
	endpoint, ak, sk := os.Getenv("S3_ENDPOINT"), os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_SECRET_KEY")
	bucket := os.Getenv("S3_BUCKET_MEDIA")
	if endpoint == "" || ak == "" || sk == "" || bucket == "" {
		t.Skip("无 S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY/S3_BUCKET_MEDIA，跳过集成测试")
	}
	client, err := blob.New(blob.Config{Endpoint: endpoint, Region: os.Getenv("S3_REGION"), AccessKey: ak, SecretKey: sk, PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	b := client.Bucket(bucket)
	ctx := context.Background()
	prefix := fmt.Sprintf("it-product/%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = b.DeletePrefix(context.Background(), prefix+"/") })
	want := bytes.Repeat([]byte("#EXTINF:2.000000,\n"), 1000)
	if err := b.PutBytes(ctx, prefix+"/v720.m3u8", want, blob.PutOptions{ContentType: "application/vnd.apple.mpegurl"}); err != nil {
		t.Fatal(err)
	}

	r := NewReader(fakeProviders{}, fakeCreds{}, netguard.Policy{AllowPrivate: true}, b, nil)
	got, err := r.ReadFile(ctx, play.Product{Key: prefix}, "v720.m3u8")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("应读回写入的内容：%d 字节 %v", len(got), err)
	}
	if _, err := r.ReadFile(ctx, play.Product{Key: prefix}, "master.m3u8"); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("不存在的文件应返回 ErrNotFound：%v", err)
	}
}
