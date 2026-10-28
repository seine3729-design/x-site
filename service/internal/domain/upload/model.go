// Package upload 管理运营后台媒体上传会话：受后台会话和用途权限约束的图片、视频上传。
package upload

import (
	"context"
	"errors"
	"time"

	"uuid"
)

// ErrJobGone 表示异步视频任务已取消、被其他 worker 完成或对应会话已不存在。
var ErrJobGone = errors.New("上传视频任务已结束")

// Purpose 表示一个媒体对象将被哪个运营字段引用。用途决定文件类型与所需权限。
type Purpose string

const (
	PurposeDramaCover           Purpose = "drama_cover"
	PurposeEpisodeCover         Purpose = "episode_cover"
	PurposeAnnouncementImage    Purpose = "announcement_image"
	PurposeAdImage              Purpose = "ad_image"
	PurposeAdPoster             Purpose = "ad_poster"
	PurposeFeedbackProjectCover Purpose = "feedback_project_cover"
	PurposeStaticPageImage      Purpose = "static_page_image"
	PurposeEpisodeVideo         Purpose = "episode_video"
	PurposeAdVideo              Purpose = "ad_video"
)

// Kind 区分图片和视频对象。
type Kind string

const (
	KindImage Kind = "image"
	KindVideo Kind = "video"
)

// ImageFormat 是上传前由浏览器编码出的图片格式。
type ImageFormat string

const (
	FormatWebP ImageFormat = "webp"
	FormatAVIF ImageFormat = "avif"
)

// Status 表示上传和异步转码的状态。
type Status string

const (
	StatusCreated   Status = "created"
	StatusUploading Status = "uploading"
	StatusUploaded  Status = "uploaded"
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusReady     Status = "ready"
	StatusFailed    Status = "failed"
)

// Asset 是一个后台媒体上传会话。SourceKey 对图片就是最终 key；视频是私有源文件 key。
type Asset struct {
	ID      uuid.UUID
	AdminID int64
	// Provider* 是创建会话时确定的物理目标快照；读写凭据仍只从当前 storage_provider 解密，绝不写入会话。
	ProviderID         int64
	ProviderEndpoint   string
	ProviderRegion     string
	ProviderBucket     string
	ProviderPathStyle  bool
	ProviderPathPrefix string
	Purpose            Purpose
	Kind               Kind
	MIME               string
	ImageFormat        ImageFormat
	SizeBytes          int64
	SourceKey          string
	OutputKey          string
	EpisodeID          int64
	TranscodeJobID     *uuid.UUID
	Status             Status
	Progress           int32
	Attempts           int32
	Error              string
	ExpiresAt          time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
	// PartSize 是分片上传的分片大小（字节），0 表示整段单次上传。除最后一片外每片都是这个大小。
	PartSize int64
	// MultipartUploadID 是私有入库桶上这次分片上传的 ID，建会话后登记；空串表示还没有或不用分片上传。
	MultipartUploadID string
}

// UploadedPart 是对象存储已经收到的一片，完成上传前用来核对是否齐全。
type UploadedPart struct {
	Number int32
	Size   int64
}

// Multipart 表示这个会话按分片上传。
func (a Asset) Multipart() bool { return a.PartSize > 0 }

// PartCount 返回分片总数；整段单次上传时为 0。
func (a Asset) PartCount() int32 {
	if a.PartSize <= 0 || a.SizeBytes <= 0 {
		return 0
	}
	return int32((a.SizeBytes + a.PartSize - 1) / a.PartSize) //nolint:gosec // 片数受 maxParts 约束，远小于 int32 上限
}

// PartLength 返回第 n 片（从 1 开始）应有的字节数；n 越界时返回 0。
func (a Asset) PartLength(n int32) int64 {
	count := a.PartCount()
	if n < 1 || n > count {
		return 0
	}
	if n < count {
		return a.PartSize
	}
	return a.SizeBytes - int64(count-1)*a.PartSize
}

// InitInput 是创建会话时的客户端元数据。真实字节、大小和文件头会在上传/完成阶段再次校验。
type InitInput struct {
	ProviderID         int64
	ProviderEndpoint   string
	ProviderRegion     string
	ProviderBucket     string
	ProviderPathStyle  bool
	ProviderPathPrefix string
	Purpose            Purpose
	MIME               string
	ImageFormat        ImageFormat
	SizeBytes          int64
	EpisodeID          int64
	// Multipart 为 true 时按分片上传（只用于视频），分片大小由服务端决定。
	Multipart bool
}

// Repository 是上传会话的持久化接口。
type Repository interface {
	Create(ctx context.Context, a Asset) (Asset, error)
	Get(ctx context.Context, id uuid.UUID) (Asset, error)
	ClaimWrite(ctx context.Context, id uuid.UUID, adminID int64) error
	// SetMultipart 登记分片上传 ID；只对声明了分片大小、尚未登记的新会话生效。
	SetMultipart(ctx context.Context, id uuid.UUID, adminID int64, uploadID string) error
	// TouchPart 在收到一片前把会话置为 uploading 并顺延过期时间；会话已结束或已过期时失败。
	TouchPart(ctx context.Context, id uuid.UUID, adminID int64, expiresAt time.Time) error
	MarkUploaded(ctx context.Context, id uuid.UUID, adminID int64) error
	Fail(ctx context.Context, id uuid.UUID, adminID int64, message string) error
	CompleteImage(ctx context.Context, id uuid.UUID, adminID int64) error
	LinkEpisodeTranscode(ctx context.Context, id uuid.UUID, adminID int64, jobID uuid.UUID) error
	QueueAdVideo(ctx context.Context, id uuid.UUID) error
	StartAdVideo(ctx context.Context, id uuid.UUID) (Asset, error)
	UpdateAdVideoProgress(ctx context.Context, id uuid.UUID, progress int) error
	RequeueAdVideo(ctx context.Context, id uuid.UUID, message string) error
	CompleteAdVideo(ctx context.Context, id uuid.UUID, outputKey string) error
	FailAdVideo(ctx context.Context, id uuid.UUID, message string) error
	ListExpired(ctx context.Context, limit int) ([]Asset, error)
	DeleteExpired(ctx context.Context, id uuid.UUID) error
}
