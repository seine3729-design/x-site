package blob

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"golang.org/x/sync/errgroup"
)

// 逐片驱动的分片上传：乱序、并发、重传同一片后，ListParts 给出的分片能合并出与原文逐字节相同的对象。
// 这正是后台视频分片并行上传的用法（每一片来自浏览器的一次独立请求）。
func TestBucket_MultipartIntegration(t *testing.T) {
	b := testBucket(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	prefix := fmt.Sprintf("it-blob-mp/%d/", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = b.DeletePrefix(context.Background(), prefix) })

	const partSize = 5 << 20
	data := make([]byte, 2*partSize+123_456)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	ranges := map[int32][2]int{1: {0, partSize}, 2: {partSize, 2 * partSize}, 3: {2 * partSize, len(data)}}

	key := prefix + "video.mp4"
	id, err := b.CreateMultipart(ctx, key, PutOptions{ContentType: "video/mp4", CacheControl: "private, no-store"})
	if err != nil {
		t.Fatal(err)
	}
	var g errgroup.Group
	for _, n := range []int32{3, 1, 2, 2} { // 乱序 + 第 2 片传两次（浏览器重试）
		g.Go(func() error {
			r := ranges[n]
			etag, err := b.UploadPart(ctx, key, id, n, bytes.NewReader(data[r[0]:r[1]]), int64(r[1]-r[0]))
			if err == nil && etag == "" {
				return fmt.Errorf("第 %d 片没有返回 ETag", n)
			}
			return err
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}

	parts, err := b.ListParts(ctx, key, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 3 {
		t.Fatalf("应列出 3 片，得到 %+v", parts)
	}
	for i, p := range parts {
		r := ranges[int32(i+1)]
		if p.Number != int32(i+1) || p.Size != int64(r[1]-r[0]) || p.ETag == "" {
			t.Fatalf("第 %d 项分片信息不对：%+v", i, p)
		}
	}
	if err := b.CompleteMultipart(ctx, key, id, parts); err != nil {
		t.Fatal(err)
	}

	info, err := b.Head(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(data)) || info.ContentType != "video/mp4" {
		t.Fatalf("合并后的对象元数据不对：%+v", info)
	}
	body, _, err := b.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("合并后的内容与原文不一致：长度 %d / %d", len(got), len(data))
	}

	// 放弃：已传的分片被释放，对象不会出现；重复放弃不算错误（ErrNotFound 或成功都可以）。
	abandoned := prefix + "abandoned.mp4"
	id2, err := b.CreateMultipart(ctx, abandoned, PutOptions{ContentType: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.UploadPart(ctx, abandoned, id2, 1, bytes.NewReader(data[:1024]), 1024); err != nil {
		t.Fatal(err)
	}
	if err := b.AbortMultipart(ctx, abandoned, id2); err != nil {
		t.Fatal(err)
	}
	if err := b.AbortMultipart(ctx, abandoned, id2); err != nil && !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复放弃应视为已清理，得到 %v", err)
	}
	if _, err := b.Head(ctx, abandoned); !errors.Is(err, ErrNotFound) {
		t.Fatalf("放弃后不应留下对象，得到 %v", err)
	}
}
