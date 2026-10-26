// Package blob 是 S3 兼容对象存储的基础客户端（S0-9）：写入、读取、查询、删除、按前缀删除、预签名与复制。
// AWS S3、Cloudflare R2、SeaweedFS 等 S3 兼容服务共用这一套实现；S1-6a 的存储源适配器在它上面实现。
// 凭据只在服务端使用，不下发给前台或 APP（docs/12 第 2 节）。
//
// 本包不替调用方设置整体超时：读写大文件的耗时取决于大小，由调用方的 ctx 控制；
// 查询、删除等元数据请求在 ctx 没有截止时间时套上 requestTimeout。
package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"golang.org/x/sync/errgroup"
)

// ErrNotFound 表示对象不存在。
var ErrNotFound = errors.New("blob: 对象不存在")

// 元数据请求（HEAD、DELETE、LIST、COPY）在调用方没有设置截止时间时的超时。
const requestTimeout = 30 * time.Second

// 一次 DeleteObjects 最多 1000 个键（S3 协议上限）。
const deleteBatch = 1000

// Config 是一个 S3 兼容服务的连接参数。
type Config struct {
	// Endpoint 为空时使用 AWS 默认地址；SeaweedFS、R2 等填写服务地址（如 http://seaweedfs:8333）。
	Endpoint  string
	Region    string
	AccessKey string
	SecretKey string
	// PathStyle 为 true 时用路径风格寻址（http://host/bucket/key），自建服务通常需要。
	PathStyle bool
	// DialContext 非 nil 时用于建立连接（SSRF 防护：连校验过的 IP，防 DNS 重绑定，docs/12 第 9 节）；
	// 为 nil 时用默认拨号。只替换 TCP 拨号，TLS 握手仍用原域名做 SNI 与证书校验。
	DialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Client 是线程安全的 S3 客户端，可在多个桶之间共用。
type Client struct {
	api *s3.Client
}

// New 创建客户端。只用静态凭据，不读取本机的 AWS 配置文件或实例凭据，避免意外用上其他身份。
func New(cfg Config) (*Client, error) {
	if cfg.Region == "" {
		return nil, errors.New("blob: 缺少 region")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("blob: 缺少访问凭据")
	}
	hc := awshttp.NewBuildableClient()
	if cfg.DialContext != nil {
		hc = hc.WithTransportOptions(func(tr *http.Transport) { tr.DialContext = cfg.DialContext })
	}
	opts := s3.Options{
		Region:       cfg.Region,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		UsePathStyle: cfg.PathStyle,
		// 只在协议要求时计算校验和：新版 SDK 默认使用的 aws-chunked 尾部校验和，部分 S3 兼容服务不支持
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		HTTPClient:                 hc,
		RetryMaxAttempts:           3,
	}
	if cfg.Endpoint != "" {
		opts.BaseEndpoint = aws.String(cfg.Endpoint)
	}
	return &Client{api: s3.New(opts)}, nil
}

// Bucket 返回某个桶的操作句柄。
func (c *Client) Bucket(name string) *Bucket {
	return &Bucket{api: c.api, name: name}
}

// Bucket 是一个桶上的对象操作。
type Bucket struct {
	api  *s3.Client
	name string
}

// Name 返回桶名。
func (b *Bucket) Name() string { return b.name }

// ObjectInfo 是对象的元数据。
type ObjectInfo struct {
	Key          string
	Size         int64
	ContentType  string
	CacheControl string
	ETag         string
}

// PutOptions 是写入对象时的响应头。
type PutOptions struct {
	ContentType  string
	CacheControl string
}

// UploadStore 是后台上传所需的最小对象存储能力。实现会把逻辑 key 映射到具体 bucket/path_prefix。
type UploadStore interface {
	PutStream(ctx context.Context, key string, body io.Reader, size int64, opt PutOptions) error
	Head(ctx context.Context, key string) (ObjectInfo, error)
	Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}

// Put 写入对象。body 必须可 Seek（签名需要计算载荷哈希、失败重试需要重读），size 为准确长度。
func (b *Bucket) Put(ctx context.Context, key string, body io.ReadSeeker, size int64, opt PutOptions) error {
	in := &s3.PutObjectInput{
		Bucket:        aws.String(b.name),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
	}
	if opt.ContentType != "" {
		in.ContentType = aws.String(opt.ContentType)
	}
	if opt.CacheControl != "" {
		in.CacheControl = aws.String(opt.CacheControl)
	}
	if _, err := b.api.PutObject(ctx, in); err != nil {
		return fmt.Errorf("blob: 写入 %s/%s: %w", b.name, key, err)
	}
	return nil
}

const (
	// UploadPartSize 是分片上传的分片大小。S3 / R2 要求除最后一片外每片至少 5 MiB；
	// 8 MiB 在 10000 片的协议上限下可支持 80 GiB，远超后台上传的 4 GiB 上限。
	UploadPartSize = 8 << 20
	// uploadConcurrency 是同时在传的分片数。进程内存占用约 UploadPartSize × (并发 + 1)。
	uploadConcurrency = 4
)

// PutStream 流式写入不可重读的 HTTP 请求体（后台媒体上传）。
//
// 边读边分片并发上传：浏览器上传与对象存储写入重叠进行，不先把整个文件落盘再上传
// （那样总耗时是两段之和，几百 MB 的视频会非常慢，还容易在网关超时上表现为 502）。
// 不超过一个分片的小文件走单次 PUT，省掉分片上传的三次额外往返。
func (b *Bucket) PutStream(ctx context.Context, key string, body io.Reader, size int64, opt PutOptions) error {
	if size <= 0 {
		return errors.New("blob: 流式写入大小必须大于 0")
	}
	if size <= UploadPartSize {
		data, err := readExactly(body, size)
		if err != nil {
			return err
		}
		return b.Put(ctx, key, bytes.NewReader(data), size, opt)
	}
	return b.putMultipart(ctx, key, body, size, opt)
}

// readExactly 读满 size 字节，并确认请求体没有多出内容（多出说明与会话登记的大小不符）。
func readExactly(body io.Reader, size int64) ([]byte, error) {
	data := make([]byte, size)
	if _, err := io.ReadFull(body, data); err != nil {
		return nil, fmt.Errorf("blob: 读取上传内容: %w", err)
	}
	if err := expectEOF(body, size); err != nil {
		return nil, err
	}
	return data, nil
}

// expectEOF 确认请求体已经读完。
func expectEOF(body io.Reader, size int64) error {
	var extra [1]byte
	if n, _ := body.Read(extra[:]); n > 0 {
		return fmt.Errorf("blob: 上传内容超过声明的 %d 字节", size)
	}
	return nil
}

// putMultipart 用 S3 分片上传写入对象；中途失败时放弃本次分片上传，不留下计费的残片。
func (b *Bucket) putMultipart(ctx context.Context, key string, body io.Reader, size int64, opt PutOptions) error {
	in := &s3.CreateMultipartUploadInput{Bucket: aws.String(b.name), Key: aws.String(key)}
	if opt.ContentType != "" {
		in.ContentType = aws.String(opt.ContentType)
	}
	if opt.CacheControl != "" {
		in.CacheControl = aws.String(opt.CacheControl)
	}
	created, err := b.api.CreateMultipartUpload(ctx, in)
	if err != nil {
		return fmt.Errorf("blob: 创建分片上传 %s/%s: %w", b.name, key, err)
	}

	parts, err := b.uploadParts(ctx, key, created.UploadId, body, size)
	if err != nil {
		// 用独立 ctx 收尾：请求的 ctx 可能已经断开，但残片必须清掉（R2 / S3 对未完成分片计费）
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestTimeout)
		defer cancel()
		if _, aerr := b.api.AbortMultipartUpload(abortCtx, &s3.AbortMultipartUploadInput{
			Bucket: aws.String(b.name), Key: aws.String(key), UploadId: created.UploadId,
		}); aerr != nil {
			slog.WarnContext(ctx, "放弃分片上传失败，等存储生命周期规则回收", "bucket", b.name, "err", aerr)
		}
		return err
	}
	if _, err := b.api.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(b.name), Key: aws.String(key), UploadId: created.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	}); err != nil {
		return fmt.Errorf("blob: 完成分片上传 %s/%s: %w", b.name, key, err)
	}
	return nil
}

// uploadParts 顺序读请求体、并发上传分片。HTTP 请求体只能顺序读，所以读满一片就交给并发上传；
// errgroup 的并发上限同时起到背压作用，内存里最多只有「并发数 + 1」个分片。
func (b *Bucket) uploadParts(ctx context.Context, key string, uploadID *string, body io.Reader, size int64) ([]types.CompletedPart, error) {
	total := int((size + UploadPartSize - 1) / UploadPartSize)
	parts := make([]types.CompletedPart, total)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(uploadConcurrency)
	var read int64
	for i := range total {
		buf := make([]byte, min(int64(UploadPartSize), size-read))
		if _, err := io.ReadFull(body, buf); err != nil {
			_ = g.Wait()
			return nil, fmt.Errorf("blob: 读取第 %d 片: %w", i+1, err)
		}
		read += int64(len(buf))
		number := int32(i + 1) //nolint:gosec // 分片数由 size 与 UploadPartSize 推出，远小于 int32 上限
		g.Go(func() error {
			out, err := b.api.UploadPart(gctx, &s3.UploadPartInput{
				Bucket: aws.String(b.name), Key: aws.String(key), UploadId: uploadID,
				PartNumber: aws.Int32(number), Body: bytes.NewReader(buf), ContentLength: aws.Int64(int64(len(buf))),
			})
			if err != nil {
				return fmt.Errorf("blob: 上传第 %d 片: %w", number, err)
			}
			// 每个 goroutine 只写自己的下标，不需要加锁
			parts[number-1] = types.CompletedPart{ETag: out.ETag, PartNumber: aws.Int32(number)}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	if err := expectEOF(body, size); err != nil {
		return nil, err
	}
	return parts, nil
}

// PutBytes 写入内存中的数据。
func (b *Bucket) PutBytes(ctx context.Context, key string, data []byte, opt PutOptions) error {
	return b.Put(ctx, key, bytes.NewReader(data), int64(len(data)), opt)
}

// PutFile 写入本地文件。
func (b *Bucket) PutFile(ctx context.Context, key, path string, opt PutOptions) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("blob: 打开 %s: %w", path, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("blob: 读取 %s: %w", path, err)
	}
	return b.Put(ctx, key, f, st.Size(), opt)
}

// Get 读取对象，调用方负责关闭返回的 Body。对象不存在时返回 ErrNotFound。
func (b *Bucket) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	out, err := b.api.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(b.name), Key: aws.String(key)})
	if err != nil {
		return nil, ObjectInfo{}, b.wrap("读取", key, err)
	}
	return out.Body, ObjectInfo{
		Key: key, Size: aws.ToInt64(out.ContentLength), ContentType: aws.ToString(out.ContentType),
		CacheControl: aws.ToString(out.CacheControl), ETag: aws.ToString(out.ETag),
	}, nil
}

// Head 查询对象元数据。对象不存在时返回 ErrNotFound。
func (b *Bucket) Head(ctx context.Context, key string) (ObjectInfo, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	out, err := b.api.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(b.name), Key: aws.String(key)})
	if err != nil {
		return ObjectInfo{}, b.wrap("查询", key, err)
	}
	return ObjectInfo{
		Key: key, Size: aws.ToInt64(out.ContentLength), ContentType: aws.ToString(out.ContentType),
		CacheControl: aws.ToString(out.CacheControl), ETag: aws.ToString(out.ETag),
	}, nil
}

// Delete 删除对象；对象不存在不算错误（S3 语义）。
func (b *Bucket) Delete(ctx context.Context, key string) error {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	if _, err := b.api.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(b.name), Key: aws.String(key)}); err != nil {
		return b.wrap("删除", key, err)
	}
	return nil
}

// Copy 在同一存储服务内复制对象（同一 Client，可跨桶）。跨存储源复制走 Get+Put（S1-6f）。
func (b *Bucket) Copy(ctx context.Context, srcKey, dstBucket, dstKey string) error {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	// CopySource 要求对 bucket/key 做 URL 路径转义
	source := (&url.URL{Path: b.name + "/" + srcKey}).EscapedPath()
	if _, err := b.api.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket: aws.String(dstBucket), Key: aws.String(dstKey), CopySource: aws.String(source),
	}); err != nil {
		return b.wrap("复制", srcKey, err)
	}
	return nil
}

// PresignGet 生成对象的预签名下载地址，ttl 为有效期。预签名用只读凭据的客户端调用（docs/12 第 2 节）。
func (b *Bucket) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	out, err := s3.NewPresignClient(b.api).PresignGetObject(ctx,
		&s3.GetObjectInput{Bucket: aws.String(b.name), Key: aws.String(key)},
		s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("blob: 预签名 %s/%s: %w", b.name, key, err)
	}
	return out.URL, nil
}

// List 列出前缀下的全部对象（按键排序）。
func (b *Bucket) List(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	var out []ObjectInfo
	err := b.eachPage(ctx, prefix, func(objs []types.Object) error {
		for _, o := range objs {
			out = append(out, ObjectInfo{Key: aws.ToString(o.Key), Size: aws.ToInt64(o.Size), ETag: aws.ToString(o.ETag)})
		}
		return nil
	})
	return out, err
}

// DeletePrefix 删除前缀下的全部对象，返回删除的数量。prefix 必须以 / 结尾，
// 防止误删整个桶或同名前缀的其他目录（如 hls/1/ab 误删 hls/1/abc…）。
func (b *Bucket) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if prefix == "" || !strings.HasSuffix(prefix, "/") {
		return 0, fmt.Errorf("blob: 按前缀删除时前缀必须以 / 结尾，得到 %q", prefix)
	}
	n := 0
	err := b.eachPage(ctx, prefix, func(objs []types.Object) error {
		for start := 0; start < len(objs); start += deleteBatch {
			batch := objs[start:min(start+deleteBatch, len(objs))]
			ids := make([]types.ObjectIdentifier, len(batch))
			for i, o := range batch {
				ids[i] = types.ObjectIdentifier{Key: o.Key}
			}
			rctx, cancel := withDefaultTimeout(ctx)
			out, err := b.api.DeleteObjects(rctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(b.name),
				Delete: &types.Delete{Objects: ids, Quiet: aws.Bool(true)},
			})
			cancel()
			if err != nil {
				return b.wrap("批量删除", prefix, err)
			}
			if len(out.Errors) > 0 {
				e := out.Errors[0]
				return fmt.Errorf("blob: 批量删除 %s/%s 有 %d 个失败，首个 %s：%s",
					b.name, prefix, len(out.Errors), aws.ToString(e.Key), aws.ToString(e.Message))
			}
			n += len(batch)
		}
		return nil
	})
	return n, err
}

// eachPage 逐页列出前缀下的对象。
func (b *Bucket) eachPage(ctx context.Context, prefix string, fn func([]types.Object) error) error {
	p := s3.NewListObjectsV2Paginator(b.api, &s3.ListObjectsV2Input{Bucket: aws.String(b.name), Prefix: aws.String(prefix)})
	for p.HasMorePages() {
		rctx, cancel := withDefaultTimeout(ctx)
		page, err := p.NextPage(rctx)
		cancel()
		if err != nil {
			return b.wrap("列出", prefix, err)
		}
		if err := fn(page.Contents); err != nil {
			return err
		}
	}
	return nil
}

// wrap 给错误加上下文；对象不存在映射为 ErrNotFound（HEAD 的 404 没有响应体，只能看状态码）。
// 桶不存在是配置错误，不映射为 ErrNotFound。
func (b *Bucket) wrap(op, key string, err error) error {
	var noKey *types.NoSuchKey
	var notFound *types.NotFound
	var resp *awshttp.ResponseError
	var apiErr smithy.APIError
	noBucket := errors.As(err, &apiErr) && apiErr.ErrorCode() == "NoSuchBucket"
	if !noBucket && (errors.As(err, &noKey) || errors.As(err, &notFound) || (errors.As(err, &resp) && resp.HTTPStatusCode() == 404)) {
		return fmt.Errorf("blob: %s %s/%s: %w", op, b.name, key, ErrNotFound)
	}
	return fmt.Errorf("blob: %s %s/%s: %w", op, b.name, key, err)
}

func withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, requestTimeout)
}
