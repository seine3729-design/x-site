package blob

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"
)

// 批量预签名复用第一次的端点解析结果：得到的地址必须与逐个 PresignGet 相同（签名时间相关的参数除外）。
func TestPresignGetMany_MatchesPresignGet(t *testing.T) {
	ctx := context.Background()
	keys := []string{"hls/42/j1/s/0123456789abcdef0123456789abcdef.m4s", "hls/42/j1/s/init.mp4", "hls/42/j1/v720.m3u8", "中文/a b+c.m4s"}
	for name, cfg := range map[string]Config{
		"AWS 虚拟主机寻址":    {Region: "ap-southeast-1", AccessKey: "ak", SecretKey: "sk"},
		"Cloudflare R2": {Endpoint: "https://acct.r2.cloudflarestorage.com", Region: "auto", AccessKey: "ak", SecretKey: "sk"},
		"自建服务路径寻址":      {Endpoint: "http://seaweedfs:8333", Region: "us-east-1", AccessKey: "ak", SecretKey: "sk", PathStyle: true},
		"端点带路径":         {Endpoint: "https://s3.example.com/storage", Region: "us-east-1", AccessKey: "ak", SecretKey: "sk", PathStyle: true},
	} {
		c, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		b := c.Bucket("media-bucket")
		many, err := b.PresignGetMany(ctx, keys, 10*time.Minute)
		if err != nil {
			t.Fatalf("%s：%v", name, err)
		}
		if len(many) != len(keys) {
			t.Fatalf("%s：结果数量 %d", name, len(many))
		}
		for i, k := range keys {
			one, err := b.PresignGet(ctx, k, 10*time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if a, b := withoutSigningTime(t, one), withoutSigningTime(t, many[i]); a != b {
				t.Errorf("%s 第 %d 个：\n逐个 %s\n批量 %s", name, i, a, b)
			}
		}
	}

	// 取消后立即返回
	c, _ := New(Config{Region: "us-east-1", AccessKey: "ak", SecretKey: "sk"})
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Bucket("b").PresignGetMany(cctx, keys, time.Minute); err == nil {
		t.Fatal("取消的请求应报错")
	}
}

// withoutSigningTime 去掉与签名时刻相关的参数（时间与签名），其余（地址、路径编码、有效期、凭据范围）必须一致。
func withoutSigningTime(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("X-Amz-Signature") == "" || q.Get("X-Amz-Date") == "" {
		t.Fatalf("不是 SigV4 预签名地址：%s", raw)
	}
	q.Del("X-Amz-Signature")
	q.Del("X-Amz-Date")
	u.RawQuery = q.Encode()
	return u.String()
}

func BenchmarkPresignGetMany(b *testing.B) {
	c, _ := New(Config{Endpoint: "https://acct.r2.cloudflarestorage.com", Region: "auto", AccessKey: "ak", SecretKey: "sk"})
	bucket := c.Bucket("media")
	keys := make([]string, 300)
	for i := range keys {
		keys[i] = fmt.Sprintf("hls/42/job1/s/%032x.m4s", i)
	}
	b.Run("批量", func(b *testing.B) {
		for b.Loop() {
			if _, err := bucket.PresignGetMany(context.Background(), keys, 10*time.Minute); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("逐个", func(b *testing.B) {
		for b.Loop() {
			for _, k := range keys {
				if _, err := bucket.PresignGet(context.Background(), k, 10*time.Minute); err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}
