package s3probe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
)

// 批量预签名（S1-8）：对本地 compose 的 SeaweedFS 生成地址并真实下载；未配置 S3_* 时跳过。
func TestPresignGetMany_SeaweedFS(t *testing.T) {
	endpoint, ak, sk := os.Getenv("S3_ENDPOINT"), os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_SECRET_KEY")
	bucket := os.Getenv("S3_BUCKET_MEDIA")
	if endpoint == "" || ak == "" || sk == "" || bucket == "" {
		t.Skip("无 S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY/S3_BUCKET_MEDIA，跳过批量预签名集成测试")
	}
	region := os.Getenv("S3_REGION")
	client, err := blob.New(blob.Config{Endpoint: endpoint, Region: region, AccessKey: ak, SecretKey: sk, PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	b := client.Bucket(bucket)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	prefix := fmt.Sprintf("it-presign-many/%d/", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = b.DeletePrefix(context.Background(), prefix) })

	keys := make([]string, 3)
	for i := range keys {
		keys[i] = fmt.Sprintf("%ss/%d.m4s", prefix, i)
		if err := b.PutBytes(ctx, keys[i], []byte(fmt.Sprintf("segment-%d", i)), blob.PutOptions{ContentType: "video/iso.segment"}); err != nil {
			t.Fatal(err)
		}
	}

	p := &Presigner{Egress: netguard.Policy{AllowPrivate: true}}
	urls, err := p.PresignGetMany(ctx, storage.PresignManyParams{
		Conn: storage.Conn{Endpoint: endpoint, Region: region, Bucket: bucket, PathStyle: true},
		Cred: storage.Credential{AccessKeyID: ak, SecretAccessKey: sk},
		Keys: keys, TTL: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != len(keys) {
		t.Fatalf("结果数量 %d", len(urls))
	}
	for i, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(u.Path, "/"+keys[i]) || u.Query().Get("X-Amz-Expires") != "120" {
			t.Fatalf("第 %d 个地址不对：%s", i, raw)
		}
		resp, err := http.Get(raw)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != fmt.Sprintf("segment-%d", i) {
			t.Fatalf("第 %d 个地址下载失败：%d %q", i, resp.StatusCode, body)
		}
	}

	// 凭据不全时报错（与逐个预签名一致）
	if _, err := p.PresignGetMany(ctx, storage.PresignManyParams{Conn: storage.Conn{Region: region}, Keys: keys}); err == nil {
		t.Fatal("缺少凭据应报错")
	}
}

// 一档清单 300 个切片逐个 SigV4 预签名的耗时（docs/specs/S1-8.md「预签名缓存先测量」）。
func BenchmarkPresignGetMany(b *testing.B) {
	p := &Presigner{}
	keys := make([]string, 300)
	for i := range keys {
		keys[i] = fmt.Sprintf("hls/42/job1/s/%032x.m4s", i)
	}
	in := storage.PresignManyParams{
		Conn: storage.Conn{Endpoint: "https://acct.r2.cloudflarestorage.com", Region: storage.R2Region, Bucket: "media"},
		Cred: storage.Credential{AccessKeyID: "ak", SecretAccessKey: "sk"},
		Keys: keys, TTL: 10 * time.Minute,
	}
	for b.Loop() {
		if _, err := p.PresignGetMany(context.Background(), in); err != nil {
			b.Fatal(err)
		}
	}
}
