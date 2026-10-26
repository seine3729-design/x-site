package s3probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
)

// Copier 实现 storage.Copier：把一个媒体对象从源存储源复制到目标存储源（docs/12 第 5 节第 3 条）。
//
// 同一存储服务（endpoint 与凭据相同）内优先用服务端复制 API（CopyObject，不过网络搬数据）；
// 跨存储源时只能拉取再写入 —— blob.Put 要求 io.ReadSeeker（SigV4 要算载荷哈希、重试要重读），
// 所以不能把 Get 的流直接接到 Put，这里先落盘到临时文件再 PutFile。
// HLS 切片都是几 MB 的小文件，落盘的开销可以接受，而且避免了大对象占满内存。
type Copier struct {
	Egress netguard.Policy
	// WorkDir 是临时文件目录，为空用系统临时目录。
	WorkDir string
	// Concurrency 是单个对象内部的并发复制数，为 0 用 defaultCopyConcurrency。
	Concurrency int
}

var _ storage.Copier = (*Copier)(nil)

const (
	defaultCopyConcurrency = 8
	// 单个文件的复制超时：切片通常几 MB，给足余量。
	fileCopyTimeout = 5 * time.Minute
)

func (c *Copier) CopyObject(ctx context.Context, req storage.CopyRequest) (storage.CopyResult, error) {
	src, err := c.bucket(req.From, req.FromCred)
	if err != nil {
		return storage.CopyResult{}, fmt.Errorf("连接源存储源: %w", err)
	}
	dst, err := c.bucket(req.To, req.ToCred)
	if err != nil {
		return storage.CopyResult{}, fmt.Errorf("连接目标存储源: %w", err)
	}

	keys, err := c.listKeys(ctx, src, req)
	if err != nil {
		return storage.CopyResult{}, err
	}
	if len(keys) == 0 {
		return storage.CopyResult{}, fmt.Errorf("源存储源上没有找到对象 %s", req.Key)
	}

	limit := c.Concurrency
	if limit <= 0 {
		limit = defaultCopyConcurrency
	}
	var objects, bytes atomic.Int64
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(limit)
	for _, k := range keys {
		g.Go(func() error {
			n, err := c.copyOne(gctx, src, dst, req, k)
			if err != nil {
				return err
			}
			objects.Add(1)
			bytes.Add(n)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return storage.CopyResult{}, err
	}
	return storage.CopyResult{Objects: int(objects.Load()), Bytes: bytes.Load()}, nil
}

// listKeys 列出要复制的相对路径（相对于存储源的 path_prefix）。
// hls_dir 列目录下的全部文件，file 就是它自己。
func (c *Copier) listKeys(ctx context.Context, src *blob.Bucket, req storage.CopyRequest) ([]string, error) {
	srcKey := storage.JoinPrefix(req.From.PathPrefix, req.Key)
	if req.Kind != storage.ObjectHLSDir {
		if _, err := src.Head(ctx, srcKey); err != nil {
			return nil, fmt.Errorf("查询源对象: %w", err)
		}
		return []string{req.Key}, nil
	}
	objs, err := src.List(ctx, srcKey+"/")
	if err != nil {
		return nil, fmt.Errorf("列出源目录: %w", err)
	}
	out := make([]string, 0, len(objs))
	for _, o := range objs {
		// 转回相对路径：去掉源存储源的 path_prefix
		rel := strings.TrimPrefix(o.Key, storage.JoinPrefix(req.From.PathPrefix, ""))
		out = append(out, strings.TrimPrefix(rel, "/"))
	}
	return out, nil
}

// copyOne 复制一个文件，返回字节数。
func (c *Copier) copyOne(ctx context.Context, src, dst *blob.Bucket, req storage.CopyRequest, relKey string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, fileCopyTimeout)
	defer cancel()

	srcKey := storage.JoinPrefix(req.From.PathPrefix, relKey)
	dstKey := storage.JoinPrefix(req.To.PathPrefix, relKey)

	// 同一存储服务：服务端复制，不经过本进程
	if req.SameStore {
		if err := src.Copy(ctx, srcKey, dst.Name(), dstKey); err != nil {
			return 0, fmt.Errorf("服务端复制 %s: %w", relKey, err)
		}
		info, err := dst.Head(ctx, dstKey)
		if err != nil {
			return 0, nil // 复制已成功，拿不到大小不算失败
		}
		return info.Size, nil
	}

	body, info, err := src.Get(ctx, srcKey)
	if err != nil {
		return 0, fmt.Errorf("读取 %s: %w", relKey, err)
	}
	defer body.Close()

	f, err := os.CreateTemp(c.WorkDir, "replica-*")
	if err != nil {
		return 0, fmt.Errorf("创建临时文件: %w", err)
	}
	tmp := f.Name()
	defer func() {
		f.Close()
		_ = os.Remove(tmp)
	}()
	n, err := io.Copy(f, body)
	if err != nil {
		return 0, fmt.Errorf("落盘 %s: %w", relKey, err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("写入临时文件 %s: %w", relKey, err)
	}

	// List 不返回 ContentType / CacheControl，按后缀重建，保持与转封装写入时一致
	if err := dst.PutFile(ctx, dstKey, tmp, putOptionsFor(relKey, info)); err != nil {
		return 0, fmt.Errorf("写入 %s: %w", relKey, err)
	}
	return n, nil
}

func (c *Copier) bucket(conn storage.Conn, cred storage.Credential) (*blob.Bucket, error) {
	client, err := blob.New(blob.Config{
		Endpoint: conn.Endpoint, Region: conn.Region, AccessKey: cred.AccessKeyID,
		SecretKey: cred.SecretAccessKey, PathStyle: conn.PathStyle, DialContext: c.Egress.DialContext(nil),
	})
	if err != nil {
		return nil, err
	}
	return client.Bucket(conn.Bucket), nil
}

// 产物的响应头（与 transcode/pipeline.go 的写入保持一致）。
const immutableCache = "public, max-age=31536000, immutable"

// putOptionsFor 按文件后缀推断写入时的响应头；Get 带回的类型优先。
func putOptionsFor(key string, info blob.ObjectInfo) blob.PutOptions {
	opt := blob.PutOptions{ContentType: info.ContentType, CacheControl: info.CacheControl}
	if opt.ContentType != "" && opt.CacheControl != "" {
		return opt
	}
	switch strings.ToLower(filepath.Ext(path.Base(key))) {
	case ".m3u8":
		fill(&opt, "application/vnd.apple.mpegurl", "no-cache")
	case ".m4s":
		fill(&opt, "video/iso.segment", immutableCache)
	case ".ts":
		fill(&opt, "video/mp2t", immutableCache)
	case ".mp4":
		fill(&opt, "video/mp4", immutableCache)
	case ".webp":
		fill(&opt, "image/webp", immutableCache)
	default:
		fill(&opt, "application/octet-stream", immutableCache)
	}
	return opt
}

func fill(opt *blob.PutOptions, contentType, cacheControl string) {
	if opt.ContentType == "" {
		opt.ContentType = contentType
	}
	if opt.CacheControl == "" {
		opt.CacheControl = cacheControl
	}
}

// VerifyObject 实现 storage.ObjectVerifier：检查对象是否存在于某个存储源（接管历史产物前的校验）。
// hls_dir 看目录下有没有文件，file 用 HEAD。对象不存在不是错误，返回 Exists=false。
func (c *Copier) VerifyObject(
	ctx context.Context, conn storage.Conn, cred storage.Credential, kind storage.ObjectKind, key string,
) (storage.VerifyResult, error) {
	bucket, err := c.bucket(conn, cred)
	if err != nil {
		return storage.VerifyResult{}, fmt.Errorf("连接存储源: %w", err)
	}
	full := storage.JoinPrefix(conn.PathPrefix, key)

	if kind != storage.ObjectHLSDir {
		info, err := bucket.Head(ctx, full)
		if errors.Is(err, blob.ErrNotFound) {
			return storage.VerifyResult{}, nil
		}
		if err != nil {
			return storage.VerifyResult{}, err
		}
		return storage.VerifyResult{Exists: true, Objects: 1, Bytes: info.Size}, nil
	}

	objs, err := bucket.List(ctx, full+"/")
	if err != nil {
		return storage.VerifyResult{}, err
	}
	if len(objs) == 0 {
		return storage.VerifyResult{}, nil
	}
	// HLS 目录至少要有主清单，否则是残留的半成品，不该当成可用副本
	var bytes int64
	var hasMaster bool
	for _, o := range objs {
		bytes += o.Size
		if path.Base(o.Key) == "master.m3u8" {
			hasMaster = true
		}
	}
	if !hasMaster {
		return storage.VerifyResult{}, nil
	}
	return storage.VerifyResult{Exists: true, Objects: len(objs), Bytes: bytes}, nil
}

var _ storage.ObjectVerifier = (*Copier)(nil)

// RemoveObject 实现 storage.ObjectRemover：删除一个对象在某存储源上的全部文件（S1-6f 清理孤立产物）。
// hls_dir 按目录前缀批量删除，file 删单个对象。对象本来就不存在不算错误（S3 语义，清理要幂等）。
func (c *Copier) RemoveObject(
	ctx context.Context, conn storage.Conn, cred storage.Credential, kind storage.ObjectKind, key string,
) (int, error) {
	bucket, err := c.bucket(conn, cred)
	if err != nil {
		return 0, fmt.Errorf("连接存储源: %w", err)
	}
	full := storage.JoinPrefix(conn.PathPrefix, key)
	if kind != storage.ObjectHLSDir {
		if err := bucket.Delete(ctx, full); err != nil {
			return 0, err
		}
		return 1, nil
	}
	// 结尾的 / 是 blob.DeletePrefix 的硬性要求：防止 hls/1/ab 误删 hls/1/abc
	return bucket.DeletePrefix(ctx, full+"/")
}

var _ storage.ObjectRemover = (*Copier)(nil)
