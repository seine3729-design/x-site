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
	AttachMultipart(ctx context.Context, id uuid.UUID, adminID int64, uploadID string) error
	ClaimPart(ctx context.Context, id uuid.UUID, adminID int64, number int32, size int64) (dom.Asset, error)
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

// sourceStore 是私有转码暂存桶：整段写入用 objectStore，视频分片并行上传用分片接口。
type sourceStore interface {
	objectStore
	blob.MultipartStore
}

// Handlers 由 app 注入上传会话、分集转封装服务及图片/视频源桶。
type Handlers struct {
	Svc    uploadService
	Media  episodeMediaService
	Stores uploadstore.Resolver // 后台配置的 storage_provider 主源 / 已固定源
	Source sourceStore          // 私有转码暂存桶（不经 CDN），同时是视频源文件的唯一落点
	Images ImageOptimizer       // 外部图片优化服务；为 nil 时优化接口回 503，浏览器退回本地压缩
	// PartSlots 限制全进程同时缓冲在内存里的视频分片数（每片 VideoPartSize）；为 nil 时不限（测试用）。
	PartSlots chan struct{}
}

// 使用编译期断言确保 app 只能注入具有所需能力的服务和桶。
var _ uploadService = (*dom.Service)(nil)
var _ sourceStore = (*blob.Bucket)(nil)

type initRequest struct {
	Purpose     string `json:"purpose" binding:"required,max=64"`
	MIME        string `json:"mime" binding:"required,max=128"`
	ImageFormat string `json:"image_format" binding:"max=16"`
	SizeBytes   int64  `json:"size_bytes" binding:"required"`
	EpisodeID   int64  `json:"episode_id"`
	// Multipart 为 true 时按分片并行上传（只用于视频）；不传保持整段单次上传，兼容旧页面。
	Multipart bool `json:"multipart"`
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
	// PartSize / PartCount 为 0 表示整段单次上传；否则浏览器按片号 1…PartCount 逐片 PUT。
	PartSize  int64 `json:"part_size"`
	PartCount int32 `json:"part_count"`
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
		PartSize: a.PartSize, PartCount: a.PartCount(),
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
	if req.Multipart && h.Source == nil {
		respond.Error(c, errs.ErrUnavailable.WithMessage("转码暂存桶未配置"))
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
		Multipart: req.Multipart,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	if a.Multipart() {
		if a, err = h.startMultipart(c.Request.Context(), a); err != nil {
			respond.Error(c, err)
			return
		}
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "media_upload.create", TargetType: "admin_media_upload", TargetID: a.ID.String(),
		After: gin.H{"provider_id": a.ProviderID, "purpose": a.Purpose, "kind": a.Kind, "size_bytes": a.SizeBytes, "image_format": a.ImageFormat,
			"part_size": a.PartSize}})
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
		// 已合并的分片上传会返回 ErrNotFound，按「已清理」处理；没合并的要放弃掉，否则残片一直计费
		if a.MultipartUploadID != "" {
			if err := h.Source.AbortMultipart(cleanupCtx, dom.StageKey(a.ProviderID, a.SourceKey), a.MultipartUploadID); err != nil && !errors.Is(err, blob.ErrNotFound) {
				message += "；分片清理待重试"
			}
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
// 图片写配置的主存储源（它们要经 CDN 分发给访客）。**视频源文件只写私有转码暂存桶**：
// 它是 `private, no-store`、不进 CDN，唯一的读者是转码 worker（`transcode_job.source_key` 就是暂存 key），
// 过去在主存储里留的那一份没有任何代码路径会读。为它多付一次完整上行，在跨公网的主存储上
// 实测直接把上传拖垮：30 MB 切 4 片并发传 Cloudflare R2，单个 UploadPart 在途超过两分钟被对端断连，
// 重试用尽后整个请求 500（见 docs/03 第 9.6a 节）。
func (h *Handlers) writeContent(ctx context.Context, store objectStore, a dom.Asset, body io.Reader) error {
	if a.Kind != dom.KindVideo {
		return store.PutStream(ctx, a.SourceKey, body, a.SizeBytes,
			blob.PutOptions{ContentType: a.MIME, CacheControl: cacheControl(a.Kind)})
	}
	if h.Source == nil {
		return errs.ErrUnavailable.WithMessage("转码暂存桶未配置")
	}
	return h.Source.PutStream(ctx, dom.StageKey(a.ProviderID, a.SourceKey), body, a.SizeBytes,
		blob.PutOptions{ContentType: a.MIME, CacheControl: "private, no-store"})
}

// stageVideo 确认转码源已在私有暂存桶里就位，并核对一次大小与类型。
// 正常情况下 writeContent 已经直接写在那里，这里只查元数据；
// 只有更早版本留下的会话（源文件写在主存储）才退回复制一次，让它们仍能完成提交。
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
		if errors.Is(err, blob.ErrNotFound) {
			return errs.ErrBadRequest.WithMessage("上传的视频文件已不存在，请重新上传")
		}
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
	// 分片上传：先核对并合并分片，之后与整段上传走同一条校验与入队路径（stageVideo 会再 Head 核对一次）。
	if a.Multipart() && a.Status == dom.StatusUploading {
		if err := h.finishParts(c.Request.Context(), a); err != nil {
			respond.Error(c, err)
			return
		}
		if err := h.Svc.MarkUploaded(c.Request.Context(), id, adminID); err != nil {
			h.discardStagedVideo(c.Request.Context(), a, "上传状态更新失败")
			respond.Error(c, err)
			return
		}
		a.Status = dom.StatusUploaded
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
	if a.Kind == dom.KindImage {
		// 图片写在主存储：再核对一次对象存储里的元数据，再验文件头。
		// 这里刻意不写 `info, err :=`：那样会在本块内遮蔽外层的 err，让后面 CompleteImage 的失败漏到成功分支。
		info, headErr := store.Head(c.Request.Context(), a.SourceKey)
		if headErr != nil {
			h.discardFailedObject(c.Request.Context(), store, a, "上传对象读取失败")
			respond.Error(c, headErr)
			return
		}
		if info.Size != a.SizeBytes || normalizedMIME(info.ContentType) != a.MIME {
			h.discardFailedObject(c.Request.Context(), store, a, "上传对象元数据不一致")
			respond.Error(c, errs.ErrBadRequest.WithMessage("对象存储中的文件与申请信息不一致"))
			return
		}
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
