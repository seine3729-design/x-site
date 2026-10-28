package upload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/upload"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
	"xsite/service/internal/store/blob"
)

// maxPartNumber 是 S3 协议允许的最大片号。
const maxPartNumber = 10000

var errMultipartGone = errs.ErrBadRequest.WithMessage("分片上传已失效，请重新选择文件")

type partDTO struct {
	ID         string `json:"id"`
	PartNumber int32  `json:"part_number"`
	SizeBytes  int64  `json:"size_bytes"`
}

// startMultipart 在私有入库桶上开启分片上传并登记到会话；失败时放弃已开启的分片上传并把会话置为失败。
func (h *Handlers) startMultipart(ctx context.Context, a dom.Asset) (dom.Asset, error) {
	key := dom.StageKey(a.ProviderID, a.SourceKey)
	uploadID, err := h.Source.CreateMultipart(ctx, key, blob.PutOptions{ContentType: a.MIME, CacheControl: "private, no-store"})
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err != nil {
		_ = h.Svc.Fail(cleanupCtx, a.ID, a.AdminID, "开启分片上传失败")
		return dom.Asset{}, err
	}
	if err := h.Svc.AttachMultipart(ctx, a.ID, a.AdminID, uploadID); err != nil {
		_ = h.Source.AbortMultipart(cleanupCtx, key, uploadID)
		_ = h.Svc.Fail(cleanupCtx, a.ID, a.AdminID, "登记分片上传失败")
		return dom.Asset{}, err
	}
	a.MultipartUploadID = uploadID
	return a, nil
}

// PutPart 是 PUT /api/admin/uploads/:id/parts/:number：接收视频的一片，写入私有入库桶的分片上传。
//
// 浏览器把视频切成 dom.VideoPartSize 的片并发提交，每片一个请求：单个请求不会撞上 Cloudflare 的请求体上限
// （Free / Pro 100 MB）与 Traefik 的 60 秒读超时，几路并发还能同时用上多条「边缘 → 源站」连接。
// 同一片可以重复提交（浏览器重试），以最后一次为准。分片只是数据传输，不单独记审计（建会话与完成各记一条）。
func (h *Handlers) PutPart(c *gin.Context) {
	admin.SkipAudit(c)
	id, ok := parseID(c)
	adminID, hasAdmin := current(c)
	if !ok || !hasAdmin {
		return
	}
	n, err := strconv.ParseInt(c.Param("number"), 10, 32)
	if err != nil || n < 1 || n > maxPartNumber {
		respond.Error(c, errs.ErrBadRequest.WithMessage("分片序号无效"))
		return
	}
	number := int32(n)
	ctx := c.Request.Context()
	a, err := h.Svc.GetOwned(ctx, id, adminID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if _, allowed := requirePurpose(c, a.Purpose); !allowed {
		return
	}
	if h.Source == nil {
		respond.Error(c, errs.ErrUnavailable.WithMessage("转码暂存桶未配置"))
		return
	}
	if a, err = h.Svc.ClaimPart(ctx, id, adminID, number, c.Request.ContentLength); err != nil {
		respond.Error(c, err)
		return
	}
	release, err := h.acquirePartSlot(ctx)
	if err != nil {
		respond.Error(c, err)
		return
	}
	defer release()

	started := time.Now()
	data, err := readPart(c.Request.Body, c.Request.ContentLength)
	if err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("分片内容没有收全，请重试"))
		return
	}
	received := time.Now()
	if _, err := h.Source.UploadPart(ctx, dom.StageKey(a.ProviderID, a.SourceKey), a.MultipartUploadID, number,
		bytes.NewReader(data), int64(len(data))); err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			respond.Error(c, errMultipartGone)
			return
		}
		slog.WarnContext(ctx, "后台视频分片写入失败", "upload_id", id.String(), "part", number, "err", err)
		respond.Error(c, err) // 5xx：浏览器会重试这一片
		return
	}
	// 读请求体的耗时反映「浏览器 → 源站」，写入耗时反映「源站 → 入库桶」：上传慢时按这两个数判断卡在哪一段。
	slog.InfoContext(ctx, "后台视频分片已写入", "upload_id", id.String(), "part", number, "bytes", len(data),
		"read_ms", received.Sub(started).Milliseconds(), "store_ms", time.Since(received).Milliseconds())
	respond.OK(c, partDTO{ID: id.String(), PartNumber: number, SizeBytes: int64(len(data))})
}

// acquirePartSlot 占用一个分片缓冲名额，等待时随请求取消而放弃。
func (h *Handlers) acquirePartSlot(ctx context.Context) (func(), error) {
	if h.PartSlots == nil {
		return func() {}, nil
	}
	select {
	case h.PartSlots <- struct{}{}:
		return func() { <-h.PartSlots }, nil
	case <-ctx.Done():
		return nil, errs.ErrUnavailable.WithMessage("上传通道繁忙，请稍后重试")
	}
}

// readPart 读满 size 字节并确认请求体没有多余内容。分片不大（VideoPartSize），整片读进内存再写对象存储：
// SDK 失败重试要能从头重读，而且不让一个慢速浏览器长时间占着一条到对象存储的连接。
func readPart(body io.Reader, size int64) ([]byte, error) {
	if size <= 0 || size > dom.VideoPartSize {
		return nil, errors.New("分片长度无效")
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(body, data); err != nil {
		return nil, err
	}
	var extra [1]byte
	if n, _ := body.Read(extra[:]); n > 0 {
		return nil, errors.New("分片内容超过声明的长度")
	}
	return data, nil
}

// finishParts 核对入库桶收到的分片是否齐全，再合并成一个对象。
// 缺片时只报错、不改会话状态：浏览器补传缺的那几片后可以再次提交完成。
func (h *Handlers) finishParts(ctx context.Context, a dom.Asset) error {
	if h.Source == nil {
		return errs.ErrUnavailable.WithMessage("转码暂存桶未配置")
	}
	key := dom.StageKey(a.ProviderID, a.SourceKey)
	parts, err := h.Source.ListParts(ctx, key, a.MultipartUploadID)
	if errors.Is(err, blob.ErrNotFound) {
		return errMultipartGone
	}
	if err != nil {
		return err
	}
	uploaded := make([]dom.UploadedPart, len(parts))
	for i, p := range parts {
		uploaded[i] = dom.UploadedPart{Number: p.Number, Size: p.Size}
	}
	if err := a.CheckParts(uploaded); err != nil {
		return err
	}
	started := time.Now()
	if err := h.Source.CompleteMultipart(ctx, key, a.MultipartUploadID, parts); err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			return errMultipartGone
		}
		return err
	}
	slog.InfoContext(ctx, "后台视频分片已合并", "upload_id", a.ID.String(), "parts", len(parts), "bytes", a.SizeBytes,
		"merge_ms", time.Since(started).Milliseconds(), "since_create_ms", time.Since(a.CreatedAt).Milliseconds())
	return nil
}

// discardStagedVideo 在分片已合并、会话状态却没能推进时删除暂存对象并把会话置为失败。
func (h *Handlers) discardStagedVideo(ctx context.Context, a dom.Asset, message string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := h.Source.Delete(cleanupCtx, dom.StageKey(a.ProviderID, a.SourceKey)); err != nil && !errors.Is(err, blob.ErrNotFound) {
		message += "；转码暂存清理待重试"
	}
	_ = h.Svc.Fail(cleanupCtx, a.ID, a.AdminID, message)
}
