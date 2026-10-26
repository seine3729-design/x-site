package blob

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testBucket 连接本地 compose 的 SeaweedFS（./dev.sh go test 会注入 S3_*）；未配置时跳过。
func testBucket(t *testing.T) *Bucket {
	t.Helper()
	endpoint, bucket := os.Getenv("S3_ENDPOINT"), os.Getenv("S3_BUCKET_MEDIA")
	if endpoint == "" || bucket == "" {
		t.Skip("未设置 S3_ENDPOINT / S3_BUCKET_MEDIA，跳过对象存储集成测试")
	}
	c, err := New(Config{
		Endpoint: endpoint, Region: os.Getenv("S3_REGION"), PathStyle: os.Getenv("S3_FORCE_PATH_STYLE") == "true",
		AccessKey: os.Getenv("S3_ACCESS_KEY"), SecretKey: os.Getenv("S3_SECRET_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c.Bucket(bucket)
}

func TestNew_RequiresRegionAndCredentials(t *testing.T) {
	if _, err := New(Config{AccessKey: "a", SecretKey: "b"}); err == nil {
		t.Fatal("缺少 region 应报错")
	}
	if _, err := New(Config{Region: "us-east-1"}); err == nil {
		t.Fatal("缺少凭据应报错")
	}
}

func TestBucket_DeletePrefixRejectsNonDirectoryPrefix(t *testing.T) {
	b := (&Client{}).Bucket("x")
	for _, p := range []string{"", "hls/1/ab"} {
		if _, err := b.DeletePrefix(context.Background(), p); err == nil {
			t.Fatalf("前缀 %q 不以 / 结尾，应拒绝（防止误删整个桶或同名前缀）", p)
		}
	}
}

func TestBucket_Integration(t *testing.T) {
	b := testBucket(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	prefix := fmt.Sprintf("it-blob/%d/", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = b.DeletePrefix(context.Background(), prefix) })

	key := prefix + "a.txt"
	if err := b.PutBytes(ctx, key, []byte("你好"), PutOptions{ContentType: "text/plain", CacheControl: "no-cache"}); err != nil {
		t.Fatal(err)
	}
	info, err := b.Head(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len("你好")) || info.ContentType != "text/plain" || info.CacheControl != "no-cache" {
		t.Fatalf("元数据不符合预期：%+v", info)
	}
	body, _, err := b.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil || string(data) != "你好" {
		t.Fatalf("读回内容不一致：%q %v", data, err)
	}

	file := filepath.Join(t.TempDir(), "b.bin")
	if err := os.WriteFile(file, []byte(strings.Repeat("x", 3000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.PutFile(ctx, prefix+"sub/b.bin", file, PutOptions{}); err != nil {
		t.Fatal(err)
	}
	// 同名前缀的另一个目录不能被 DeletePrefix 误删
	sibling := strings.TrimSuffix(prefix, "/") + "x/keep.txt"
	if err := b.PutBytes(ctx, sibling, []byte("keep"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Delete(context.Background(), sibling) })

	list, err := b.List(ctx, prefix)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Key != key || list[1].Key != prefix+"sub/b.bin" || list[1].Size != 3000 {
		t.Fatalf("列表不符合预期：%+v", list)
	}

	n, err := b.DeletePrefix(ctx, prefix)
	if err != nil || n != 2 {
		t.Fatalf("应删除 2 个对象，得到 %d %v", n, err)
	}
	if _, err := b.Head(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后 HEAD 应为 ErrNotFound，得到 %v", err)
	}
	if _, _, err := b.Get(ctx, prefix+"sub/b.bin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("删除后 GET 应为 ErrNotFound，得到 %v", err)
	}
	if _, err := b.Head(ctx, sibling); err != nil {
		t.Fatalf("同名前缀的其他目录被误删：%v", err)
	}
	if err := b.Delete(ctx, key); err != nil {
		t.Fatalf("删除不存在的对象不算错误：%v", err)
	}
}
