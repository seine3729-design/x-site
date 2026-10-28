package blob

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// completeTimeout 是完成分片上传的超时：存储要把几百片拼成一个对象，比普通元数据请求慢得多。
const completeTimeout = 5 * time.Minute

// Part 是一次分片上传里已经收到的一片。
type Part struct {
	Number int32
	ETag   string
	Size   int64
}

// MultipartStore 是由调用方逐片驱动的分片上传（后台视频分片并行上传）。
//
// 与 PutStream 的区别：PutStream 在一次请求里自己读、自己切片；这里每一片来自浏览器的一次独立请求，
// UploadId 由调用方保存，可以跨请求、乱序、并发、重传同一片（同一片号后写覆盖先写）。
// 分片上传不存在（已完成、已放弃或从未创建）时，各方法返回 ErrNotFound。
type MultipartStore interface {
	CreateMultipart(ctx context.Context, key string, opt PutOptions) (string, error)
	UploadPart(ctx context.Context, key, uploadID string, number int32, body io.ReadSeeker, size int64) (string, error)
	ListParts(ctx context.Context, key, uploadID string) ([]Part, error)
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error
	AbortMultipart(ctx context.Context, key, uploadID string) error
}

var _ MultipartStore = (*Bucket)(nil)

// CreateMultipart 开启一次分片上传并返回 UploadId。对象的 Content-Type 与 Cache-Control 在这里确定。
func (b *Bucket) CreateMultipart(ctx context.Context, key string, opt PutOptions) (string, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	in := &s3.CreateMultipartUploadInput{Bucket: aws.String(b.name), Key: aws.String(key)}
	if opt.ContentType != "" {
		in.ContentType = aws.String(opt.ContentType)
	}
	if opt.CacheControl != "" {
		in.CacheControl = aws.String(opt.CacheControl)
	}
	out, err := b.api.CreateMultipartUpload(ctx, in)
	if err != nil {
		return "", b.wrap("创建分片上传", key, err)
	}
	id := aws.ToString(out.UploadId)
	if id == "" {
		return "", fmt.Errorf("blob: 创建分片上传 %s/%s: 存储没有返回 UploadId", b.name, key)
	}
	return id, nil
}

// UploadPart 上传一片并返回 ETag。body 必须可 Seek：签名要计算载荷哈希，SDK 重试时要从头重读。
// 片号从 1 开始；除最后一片外，各片大小必须一致且不小于 5 MiB（R2 的要求，S3 只要求下限）。
func (b *Bucket) UploadPart(ctx context.Context, key, uploadID string, number int32, body io.ReadSeeker, size int64) (string, error) {
	out, err := b.api.UploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(b.name), Key: aws.String(key), UploadId: aws.String(uploadID),
		PartNumber: aws.Int32(number), Body: body, ContentLength: aws.Int64(size),
	})
	if err != nil {
		return "", b.wrap(fmt.Sprintf("上传第 %d 片", number), key, err)
	}
	return aws.ToString(out.ETag), nil
}

// ListParts 列出已收到的分片（每个片号一条，按片号升序，自动翻页）。完成前用它核对片数与长度，
// 并拿到合并所需的 ETag —— 服务端因此不必自己保存每一片的 ETag。
//
// 同一片号重传过时，S3 / R2 只列最后一次，SeaweedFS 却会把每次都列出来（实测）；
// 浏览器重传分片是常态，所以这里按片号去重、保留最后一条，调用方看到的始终是 S3 语义。
func (b *Bucket) ListParts(ctx context.Context, key, uploadID string) ([]Part, error) {
	var parts []Part
	index := map[int32]int{}
	p := s3.NewListPartsPaginator(b.api, &s3.ListPartsInput{
		Bucket: aws.String(b.name), Key: aws.String(key), UploadId: aws.String(uploadID),
	})
	for p.HasMorePages() {
		rctx, cancel := withDefaultTimeout(ctx)
		page, err := p.NextPage(rctx)
		cancel()
		if err != nil {
			return nil, b.wrap("列出分片", key, err)
		}
		for _, part := range page.Parts {
			item := Part{Number: aws.ToInt32(part.PartNumber), ETag: aws.ToString(part.ETag), Size: aws.ToInt64(part.Size)}
			if i, seen := index[item.Number]; seen {
				parts[i] = item
				continue
			}
			index[item.Number] = len(parts)
			parts = append(parts, item)
		}
	}
	slices.SortFunc(parts, func(a, b Part) int { return cmp.Compare(a.Number, b.Number) })
	return parts, nil
}

// CompleteMultipart 按给定分片（须按片号升序）合并成最终对象。
func (b *Bucket) CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, completeTimeout)
		defer cancel()
	}
	completed := make([]types.CompletedPart, len(parts))
	for i, p := range parts {
		completed[i] = types.CompletedPart{PartNumber: aws.Int32(p.Number), ETag: aws.String(p.ETag)}
	}
	if _, err := b.api.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(b.name), Key: aws.String(key), UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: completed},
	}); err != nil {
		return b.wrap("完成分片上传", key, err)
	}
	return nil
}

// AbortMultipart 放弃分片上传并释放已上传的分片（R2 / S3 对未完成的分片计费）。
// 已完成或已放弃时返回 ErrNotFound，调用方按「已经清理过」处理即可。
func (b *Bucket) AbortMultipart(ctx context.Context, key, uploadID string) error {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	if _, err := b.api.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(b.name), Key: aws.String(key), UploadId: aws.String(uploadID),
	}); err != nil {
		return b.wrap("放弃分片上传", key, err)
	}
	return nil
}
