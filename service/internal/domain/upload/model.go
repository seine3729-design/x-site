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
}

// Repository 是上传会话的持久化接口。
type Repository interface {
	Create(ctx context.Context, a Asset) (Asset, error)
	Get(ctx context.Context, id uuid.UUID) (Asset, error)
	ClaimWrite(ctx context.Context, id uuid.UUID, adminID int64) error
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
