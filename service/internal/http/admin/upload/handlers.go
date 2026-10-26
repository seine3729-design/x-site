package upload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"uuid"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	mediadom "xsite/service/internal/domain/media"
	dom "xsite/service/internal/domain/upload"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/uploadstore"
)

// 这里使用真实 UUID；routes.go 的别名仅为接口文档遗留，实际 handler 直接依赖 UUID。
type uploadService interface {
	Init(ctx context.Context, adminID int64, in dom.InitInput) (dom.Asset, error)
	GetOwned(ctx context.Context, id uuid.UUID, adminID int64) (dom.Asset, error)
	ClaimWrite(ctx context.Context, id uuid.UUID, adminID int64) (dom.Asset, error)
	MarkUploaded(ctx context.Context, id uuid.UUID, adminID int64) error
	Fail(ctx context.Context, id uuid.UUID, adminID int64, message string) error
	CompleteImage(ctx context.Context, id uuid.UUID, adminID int64) (dom.Asset, error)
	LinkEpisodeTranscode(ctx context.Context, id uuid.UUID, adminID int64, jobID uuid.UUID) (dom.Asset, error)
	QueueAdVideo(ctx context.Context, id uuid.UUID, adminID int64) (dom.Asset, error)
}

type episodeMediaService interface {
	Enqueue(ctx context.Context, episodeID int64, in mediadom.EnqueueInput) (mediadom.Job, error)
}

type objectStore interface {
	PutStream(ctx context.Context, key string, body io.Reader, size int64, opt blob.PutOptions) error
	Head(ctx context.Context, key string) (blob.ObjectInfo, error)
	Get(ctx context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error)
	Delete(ctx context.Context, key string) error
}

// Handlers 由 app 注入上传会话、分集转封装服务及图片/视频源桶。
type Handlers struct {
	Svc    uploadService
	Media  episodeMediaService
	Stores uploadstore.Resolver // 后台配置的 storage_provider 主源 / 已固定源
	Source objectStore          // 私有转码暂存桶（不经 CDN）
}

// 使用编译期断言确保 app 只能注入具有所需能力的服务和桶。
var _ uploadService = (*dom.Service)(nil)
var _ objectStore = (*blob.Bucket)(nil)

type initRequest struct {
	Purpose     string `json:"purpose" binding:"required,max=64"`
	MIME        string `json:"mime" binding:"required,max=128"`
	ImageFormat string `json:"image_format" binding:"max=16"`
	SizeBytes   int64  `json:"size_bytes" binding:"required"`
	EpisodeID   int64  `json:"episode_id"`
}

type assetDTO struct {
	ID             string  `json:"id"`
	ProviderID     int64   `json:"provider_id"`
	Purpose        string  `json:"purpose"`
	Kind           string  `json:"kind"`
	MIME           string  `json:"mime"`
	ImageFormat    string  `json:"image_format"`
	SizeBytes      int64   `json:"size_bytes"`
	SourceKey      string  `json:"source_key"`
	OutputKey      string  `json:"output_key"`
	EpisodeID      int64   `json:"episode_id"`
	TranscodeJobID *string `json:"transcode_job_id"`
	Status         string  `json:"status"`
	Progress       int32   `json:"progress"`
	Error          string  `json:"error"`
	ExpiresAt      string  `json:"expires_at"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

func assetOf(a dom.Asset) assetDTO {
	var jobID *string
	if a.TranscodeJobID != nil {
		value := a.TranscodeJobID.String()
		jobID = &value
	}
	return assetDTO{
		ID: a.ID.String(), ProviderID: a.ProviderID, Purpose: string(a.Purpose), Kind: string(a.Kind), MIME: a.MIME, ImageFormat: string(a.ImageFormat),
		SizeBytes: a.SizeBytes, SourceKey: a.SourceKey, OutputKey: a.OutputKey, EpisodeID: a.EpisodeID, TranscodeJobID: jobID,
		Status: string(a.Status), Progress: a.Progress, Error: a.Error,
		ExpiresAt: a.ExpiresAt.UTC().Format(time.RFC3339), CreatedAt: a.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: a.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func current(c *gin.Context) (int64, bool) {
	id, ok := admin.CurrentAdmin(c)
	return id.AdminID, ok
}

func requirePurpose(c *gin.Context, purpose dom.Purpose) (int64, bool) {
	identity, ok := admin.CurrentAdmin(c)
	if !ok {
		respond.Error(c, errs.ErrNeedLogin)
		return 0, false
	}
	permission := dom.PermissionFor(purpose)
	if permission == "" || !identity.Can(permission) {
		respond.Error(c, errs.ErrForbidden)
		return 0, false
	}
	return identity.AdminID, true
}

func parseID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.Error(c, errs.ErrNotFound)
		return uuid.Nil(), false
	}
	return id, true
}

// Init 是 POST /api/admin/uploads：创建只属于当前管理员且用途受权限约束的上传会话。
func (h *Handlers) Init(c *gin.Context) {
	var req initRequest
	if !admin.Bind(c, &req) {
		return
	}
	adminID, ok := requirePurpose(c, dom.Purpose(req.Purpose))
	if !ok {
		return
	}
	kind, err := dom.KindForPurpose(dom.Purpose(req.Purpose))
	if err != nil {
		respond.Error(c, err)
		return
	}
	target, err := h.Stores.Primary(c.Request.Context(), kind)
	if err != nil {
		respond.Error(c, err)
		return
	}
	a, err := h.Svc.Init(c.Request.Context(), adminID, dom.InitInput{
		ProviderID: target.ProviderID, ProviderEndpoint: target.Endpoint, ProviderRegion: target.Region, ProviderBucket: target.Bucket,
		ProviderPathStyle: target.PathStyle, ProviderPathPrefix: target.PathPrefix,
		Purpose: dom.Purpose(req.Purpose), MIME: req.MIME, ImageFormat: dom.ImageFormat(req.ImageFormat), SizeBytes: req.SizeBytes, EpisodeID: req.EpisodeID,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "media_upload.create", TargetType: "admin_media_upload", TargetID: a.ID.String(),
		After: gin.H{"provider_id": a.ProviderID, "purpose": a.Purpose, "kind": a.Kind, "size_bytes": a.SizeBytes, "image_format": a.ImageFormat}})
	respond.OK(c, assetOf(a))
}

func (h *Handlers) storeFor(ctx context.Context, a dom.Asset) (objectStore, error) {
	target, err := h.Stores.Existing(ctx, a)
	if err != nil {
		return nil, err
	}
	return target.Store, nil
}

// discardFailedObject 在内容已写入但后续状态/格式/入队失败时补偿删除对象，并把会话置为 failed。
// 删除失败交给周期清理器兜底；原始错误仍返回给调用方，避免掩盖真正失败原因。
func (h *Handlers) discardFailedObject(ctx context.Context, store objectStore, a dom.Asset, message string) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := store.Delete(cleanupCtx, a.SourceKey); err != nil && !errors.Is(err, blob.ErrNotFound) {
		// 不把对象 key 或底层存储错误回显；过期会话清理任务会重试删除。
		message += "；对象清理待重试"
	}
	if a.Kind == dom.KindVideo && h.Source != nil {
		if err := h.Source.Delete(cleanupCtx, dom.StageKey(a.ProviderID, a.SourceKey)); err != nil && !errors.Is(err, blob.ErrNotFound) {
			message += "；转码暂存清理待重试"
		}
	}
	_ = h.Svc.Fail(cleanupCtx, a.ID, a.AdminID, message)
}

// PutContent 是 PUT /api/admin/uploads/:id/content：以流式方式写入服务器生成的 key。
func (h *Handlers) PutContent(c *gin.Context) {
	id, ok := parseID(c)
	adminID, hasAdmin := current(c)
	if !ok || !hasAdmin {
		return
	}
	a, err := h.Svc.GetOwned(c.Request.Context(), id, adminID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if _, allowed := requirePurpose(c, a.Purpose); !allowed {
		return
	}
	a, err = h.Svc.ClaimWrite(c.Request.Context(), id, adminID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	store, err := h.storeFor(c.Request.Context(), a)
	if err != nil {
		_ = h.Svc.Fail(c.Request.Context(), a.ID, a.AdminID, "上传存储源不可用")
		respond.Error(c, err)
		return
	}
	if c.Request.ContentLength != a.SizeBytes {
		h.discardFailedObject(c.Request.Context(), store, a, "上传文件大小不一致")
		respond.Error(c, errs.ErrBadRequest.WithMessage("上传文件大小与申请信息不一致"))
		return
	}
	if normalizedMIME(c.GetHeader("Content-Type")) != a.MIME {
		h.discardFailedObject(c.Request.Context(), store, a, "上传文件类型不一致")
		respond.Error(c, errs.ErrBadRequest.WithMessage("上传文件类型与申请信息不一致"))
		return
	}
	if err := h.writeContent(c.Request.Context(), store, a, c.Request.Body); err != nil {
		h.discardFailedObject(c.Request.Context(), store, a, "上传内容写入失败")
		respond.Error(c, err)
		return
	}
	if err := h.Svc.MarkUploaded(c.Request.Context(), id, adminID); err != nil {
		h.discardFailedObject(c.Request.Context(), store, a, "上传状态更新失败")
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"id": id.String(), "status": "uploaded"})
}

// writeContent 把请求体写入会话登记的对象 key。
//
// 视频在收请求体的同时并行写入私有转码暂存桶：请求体只读一遍，两路分片上传各自进行，
// 总耗时是两者中较慢的那个。原先的做法是先只写主存储，再在完成阶段把整个源文件下载回来重新上传，
// 一个 70 MB 的分集因此要跑「上行 → 下行 → 上行」三趟，完成阶段还会撞上前端与网关的超时。
func (h *Handlers) writeContent(ctx context.Context, store objectStore, a dom.Asset, body io.Reader) error {
	opt := blob.PutOptions{ContentType: a.MIME, CacheControl: cacheControl(a.Kind)}
	if a.Kind != dom.KindVideo || h.Source == nil {
		return store.PutStream(ctx, a.SourceKey, body, a.SizeBytes, opt)
	}
	pr, pw := io.Pipe()
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		err := store.PutStream(gctx, a.SourceKey, io.TeeReader(body, pw), a.SizeBytes, opt)
		// 关掉写端：成功时让暂存那一路读到 EOF，失败时让它带着同一个错误立刻收尾，不会卡在等数据上。
		_ = pw.CloseWithError(err)
		return err
	})
	g.Go(func() error {
		// 关掉读端：暂存这一路失败时让 TeeReader 的写入立即报错，主存储那一路不会一直阻塞。
		defer pr.Close()
		return h.Source.PutStream(gctx, dom.StageKey(a.ProviderID, a.SourceKey), pr, a.SizeBytes,
			blob.PutOptions{ContentType: a.MIME, CacheControl: "private, no-store"})
	})
	return g.Wait()
}

// stageVideo 确认转码源已在私有暂存桶里就位。
// 正常情况下 writeContent 在收请求体时已经写好，这里只核对元数据；
// 只有历史会话（上传时还没有并行写暂存）或暂存对象已被清理时，才退回从主存储复制一遍。
func (h *Handlers) stageVideo(ctx context.Context, source objectStore, a dom.Asset) error {
	if h.Source == nil {
		return errs.ErrUnavailable.WithMessage("转码暂存桶未配置")
	}
	stageKey := dom.StageKey(a.ProviderID, a.SourceKey)
	staged, err := h.Source.Head(ctx, stageKey)
	switch {
	case err == nil:
		if staged.Size != a.SizeBytes || normalizedMIME(staged.ContentType) != a.MIME {
			return errs.ErrBadRequest.WithMessage("上传视频对象与申请信息不一致")
		}
		return nil
	case !errors.Is(err, blob.ErrNotFound):
		return err
	}
	body, info, err := source.Get(ctx, a.SourceKey)
	if err != nil {
		return err
	}
	defer body.Close()
	if info.Size != a.SizeBytes || normalizedMIME(info.ContentType) != a.MIME {
		return errs.ErrBadRequest.WithMessage("上传视频对象与申请信息不一致")
	}
	return h.Source.PutStream(ctx, stageKey, body, a.SizeBytes, blob.PutOptions{ContentType: a.MIME, CacheControl: "private, no-store"})
}

// Complete 是 POST /api/admin/uploads/:id/complete：再次校验对象大小、类型和图片魔数，再进入最终状态或转码队列。
func (h *Handlers) Complete(c *gin.Context) {
	id, ok := parseID(c)
	adminID, hasAdmin := current(c)
	if !ok || !hasAdmin {
		return
	}
	a, err := h.Svc.GetOwned(c.Request.Context(), id, adminID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if _, allowed := requirePurpose(c, a.Purpose); !allowed {
		return
	}
	if a.Status != dom.StatusUploaded {
		respond.Error(c, errs.ErrBadRequest.WithMessage("文件尚未上传完成"))
		return
	}
	store, err := h.storeFor(c.Request.Context(), a)
	if err != nil {
		respond.Error(c, err)
		return
	}
	info, err := store.Head(c.Request.Context(), a.SourceKey)
	if err != nil {
		h.discardFailedObject(c.Request.Context(), store, a, "上传对象读取失败")
		respond.Error(c, err)
		return
	}
	if info.Size != a.SizeBytes || normalizedMIME(info.ContentType) != a.MIME {
		h.discardFailedObject(c.Request.Context(), store, a, "上传对象元数据不一致")
		respond.Error(c, errs.ErrBadRequest.WithMessage("对象存储中的文件与申请信息不一致"))
		return
	}
	if a.Kind == dom.KindImage {
		if err := verifyImage(c.Request.Context(), store, a); err != nil {
			h.discardFailedObject(c.Request.Context(), store, a, "图片文件头校验失败")
			respond.Error(c, err)
			return
		}
		asset := a
		a, err = h.Svc.CompleteImage(c.Request.Context(), id, adminID)
		if err != nil {
			h.discardFailedObject(c.Request.Context(), store, asset, "完成图片上传失败")
		}
	} else {
		if err := h.stageVideo(c.Request.Context(), store, a); err != nil {
			respond.Error(c, err)
			return
		}
		stageKey := dom.StageKey(a.ProviderID, a.SourceKey)
		if a.Purpose == dom.PurposeEpisodeVideo {
			job, enqueueErr := h.Media.Enqueue(c.Request.Context(), a.EpisodeID, mediadom.EnqueueInput{SourceKey: stageKey})
			if enqueueErr != nil {
				h.discardFailedObject(c.Request.Context(), store, a, "提交分集转码失败")
				respond.Error(c, enqueueErr)
				return
			}
			a, err = h.Svc.LinkEpisodeTranscode(c.Request.Context(), id, adminID, job.ID)
		} else {
			asset := a
			a, err = h.Svc.QueueAdVideo(c.Request.Context(), id, adminID)
			if err != nil {
				h.discardFailedObject(c.Request.Context(), store, asset, "提交广告视频转码失败")
			}
		}
	}
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "media_upload.complete", TargetType: "admin_media_upload", TargetID: a.ID.String(),
		After: gin.H{"purpose": a.Purpose, "kind": a.Kind, "status": a.Status, "output_key": a.OutputKey}})
	respond.OK(c, assetOf(a))
}

// Get 是 GET /api/admin/uploads/:id：仅返回当前管理员自己的异步视频状态。
func (h *Handlers) Get(c *gin.Context) {
	id, ok := parseID(c)
	adminID, hasAdmin := current(c)
	if !ok || !hasAdmin {
		return
	}
	a, err := h.Svc.GetOwned(c.Request.Context(), id, adminID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if _, allowed := requirePurpose(c, a.Purpose); !allowed {
		return
	}
	respond.OK(c, assetOf(a))
}

func verifyImage(ctx context.Context, store objectStore, a dom.Asset) error {
	body, _, err := store.Get(ctx, a.SourceKey)
	if err != nil {
		return err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 64))
	if err != nil {
		return err
	}
	switch a.ImageFormat {
	case dom.FormatWebP:
		if len(data) < 12 || !bytes.Equal(data[:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WEBP")) {
			return errs.ErrBadRequest.WithMessage("上传内容不是有效的 WebP 图片")
		}
	case dom.FormatAVIF:
		if len(data) < 16 || !bytes.Equal(data[4:8], []byte("ftyp")) || (!bytes.Equal(data[8:12], []byte("avif")) && !bytes.Equal(data[8:12], []byte("avis"))) {
			return errs.ErrBadRequest.WithMessage("上传内容不是有效的 AVIF 图片")
		}
	default:
		return errs.ErrBadRequest.WithMessage("不支持的图片格式")
	}
	return nil
}

func normalizedMIME(value string) string {
	mime, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")
	return strings.TrimSpace(mime)
}

func cacheControl(kind dom.Kind) string {
	if kind == dom.KindImage {
		return "public, max-age=31536000, immutable"
	}
	return "private, no-store"
}
