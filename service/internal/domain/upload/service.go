package upload

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"uuid"

	"xsite/service/internal/errs"
)

const (
	imageMaxBytes = 15 << 20
	videoMaxBytes = 4 << 30
	sessionTTL    = 15 * time.Minute
)

var (
	errNotOwner = errs.ErrForbidden.WithMessage("该上传会话不属于当前管理员")
	errExpired  = errs.ErrBadRequest.WithMessage("上传会话已过期，请重新选择文件")
)

// Service 负责会话的用途、尺寸和格式校验；对象存储写入由 HTTP 层以流式方式完成。
type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service { return &Service{repo: repo, now: time.Now} }

// Init 创建一次仅能写入服务器生成 key 的上传会话。
func (s *Service) Init(ctx context.Context, adminID int64, in InitInput) (Asset, error) {
	if adminID <= 0 {
		return Asset{}, errs.ErrForbidden
	}
	if in.ProviderID <= 0 {
		return Asset{}, errs.ErrBadRequest.WithMessage("没有可用的存储源，请先配置图片或视频主存储")
	}
	if strings.TrimSpace(in.ProviderBucket) == "" {
		return Asset{}, errs.ErrBadRequest.WithMessage("主存储目标信息不完整，请重新选择文件")
	}
	kind, err := kindFor(in.Purpose)
	if err != nil {
		return Asset{}, err
	}
	if err := validateInput(kind, in); err != nil {
		return Asset{}, err
	}
	id := uuid.NewV7()
	now := s.now().UTC()
	a := Asset{
		ID: id, AdminID: adminID,
		ProviderID: in.ProviderID, ProviderEndpoint: in.ProviderEndpoint, ProviderRegion: in.ProviderRegion, ProviderBucket: in.ProviderBucket,
		ProviderPathStyle: in.ProviderPathStyle, ProviderPathPrefix: in.ProviderPathPrefix,
		Purpose: in.Purpose, Kind: kind, MIME: normalizedMIME(in.MIME), ImageFormat: in.ImageFormat,
		SizeBytes: in.SizeBytes, EpisodeID: in.EpisodeID, Status: StatusCreated, ExpiresAt: now.Add(sessionTTL), CreatedAt: now, UpdatedAt: now,
	}
	if kind == KindImage {
		a.SourceKey = path.Join("img", string(a.Purpose), a.ID.String()+"."+string(a.ImageFormat))
		a.OutputKey = a.SourceKey
	} else {
		a.SourceKey = path.Join("video", "source", a.ID.String()+videoExtension(a.MIME))
	}
	return s.repo.Create(ctx, a)
}

// GetOwned 返回当前管理员自己的会话，禁止猜测 UUID 读取其他运营人员的上传状态。
func (s *Service) GetOwned(ctx context.Context, id uuid.UUID, adminID int64) (Asset, error) {
	a, err := s.repo.Get(ctx, id)
	if err != nil {
		return Asset{}, err
	}
	if a.AdminID != adminID {
		return Asset{}, errNotOwner
	}
	return a, nil
}

// ClaimWrite 原子领取一次文件写入权。领取成功后状态为 uploading，其他并发请求不能覆盖同一对象 key。
func (s *Service) ClaimWrite(ctx context.Context, id uuid.UUID, adminID int64) (Asset, error) {
	a, err := s.GetOwned(ctx, id, adminID)
	if err != nil {
		return Asset{}, err
	}
	if a.Status != StatusCreated {
		return Asset{}, errs.ErrBadRequest.WithMessage("该上传会话已经使用")
	}
	if !a.ExpiresAt.After(s.now()) {
		return Asset{}, errExpired
	}
	if err := s.repo.ClaimWrite(ctx, id, adminID); err != nil {
		return Asset{}, err
	}
	a.Status = StatusUploading
	return a, nil
}

func (s *Service) MarkUploaded(ctx context.Context, id uuid.UUID, adminID int64) error {
	return s.repo.MarkUploaded(ctx, id, adminID)
}

// Fail 记录上传失败，供周期清理器回收遗留对象。message 只传给后台，不包含底层凭据。
func (s *Service) Fail(ctx context.Context, id uuid.UUID, adminID int64, message string) error {
	return s.repo.Fail(ctx, id, adminID, message)
}

func (s *Service) CompleteImage(ctx context.Context, id uuid.UUID, adminID int64) (Asset, error) {
	a, err := s.GetOwned(ctx, id, adminID)
	if err != nil {
		return Asset{}, err
	}
	if a.Kind != KindImage || a.Status != StatusUploaded {
		return Asset{}, errs.ErrBadRequest.WithMessage("图片尚未上传完成")
	}
	if !a.ExpiresAt.After(s.now()) {
		return Asset{}, errExpired
	}
	if err := s.repo.CompleteImage(ctx, id, adminID); err != nil {
		return Asset{}, err
	}
	return s.GetOwned(ctx, id, adminID)
}

func (s *Service) LinkEpisodeTranscode(ctx context.Context, id uuid.UUID, adminID int64, jobID uuid.UUID) (Asset, error) {
	a, err := s.GetOwned(ctx, id, adminID)
	if err != nil {
		return Asset{}, err
	}
	if a.Purpose != PurposeEpisodeVideo || a.Status != StatusUploaded {
		return Asset{}, errs.ErrBadRequest.WithMessage("视频尚未上传完成")
	}
	if !a.ExpiresAt.After(s.now()) {
		return Asset{}, errExpired
	}
	if err := s.repo.LinkEpisodeTranscode(ctx, id, adminID, jobID); err != nil {
		return Asset{}, err
	}
	return s.GetOwned(ctx, id, adminID)
}

func (s *Service) QueueAdVideo(ctx context.Context, id uuid.UUID, adminID int64) (Asset, error) {
	a, err := s.GetOwned(ctx, id, adminID)
	if err != nil {
		return Asset{}, err
	}
	if a.Purpose != PurposeAdVideo || a.Status != StatusUploaded {
		return Asset{}, errs.ErrBadRequest.WithMessage("视频尚未上传完成")
	}
	if !a.ExpiresAt.After(s.now()) {
		return Asset{}, errExpired
	}
	if err := s.repo.QueueAdVideo(ctx, id); err != nil {
		return Asset{}, err
	}
	return s.GetOwned(ctx, id, adminID)
}

// KindForPurpose 返回上传用途对应的存储类型，供 HTTP 层选择后台配置的图片/视频主存储。
func KindForPurpose(p Purpose) (Kind, error) { return kindFor(p) }

func kindFor(p Purpose) (Kind, error) {
	switch p {
	case PurposeDramaCover, PurposeEpisodeCover, PurposeAnnouncementImage, PurposeAdImage, PurposeAdPoster, PurposeFeedbackProjectCover, PurposeStaticPageImage:
		return KindImage, nil
	case PurposeEpisodeVideo, PurposeAdVideo:
		return KindVideo, nil
	default:
		return "", errs.ErrBadRequest.WithMessage("不支持的上传用途")
	}
}

func PermissionFor(p Purpose) string {
	switch p {
	case PurposeDramaCover, PurposeEpisodeCover, PurposeEpisodeVideo:
		return "dramas.edit"
	case PurposeAnnouncementImage:
		return "announcements.edit"
	case PurposeAdImage, PurposeAdPoster, PurposeAdVideo:
		return "ads.edit"
	case PurposeFeedbackProjectCover:
		return "fb_projects.edit"
	case PurposeStaticPageImage:
		return "pages.edit"
	default:
		return ""
	}
}

func validateInput(kind Kind, in InitInput) error {
	mime := normalizedMIME(in.MIME)
	if in.SizeBytes <= 0 {
		return errs.ErrBadRequest.WithMessage("文件不能为空")
	}
	switch kind {
	case KindImage:
		if in.SizeBytes > imageMaxBytes {
			return errs.ErrBadRequest.WithMessage("图片压缩后不能超过 15 MiB")
		}
		switch in.ImageFormat {
		case FormatWebP:
			if mime != "image/webp" {
				return errs.ErrBadRequest.WithMessage("WebP 图片的类型必须为 image/webp")
			}
		case FormatAVIF:
			if mime != "image/avif" {
				return errs.ErrBadRequest.WithMessage("AVIF 图片的类型必须为 image/avif")
			}
		default:
			return errs.ErrBadRequest.WithMessage("图片格式只能选择 WebP 或 AVIF")
		}
		if in.EpisodeID != 0 {
			return errs.ErrBadRequest.WithMessage("图片上传不能指定分集")
		}
	case KindVideo:
		if in.SizeBytes > videoMaxBytes {
			return errs.ErrBadRequest.WithMessage("视频不能超过 4 GiB")
		}
		switch mime {
		case "video/mp4", "video/quicktime", "video/webm", "video/x-matroska":
		default:
			return errs.ErrBadRequest.WithMessage("视频仅支持 MP4、MOV、WebM 或 MKV")
		}
		if in.Purpose == PurposeEpisodeVideo && in.EpisodeID <= 0 {
			return errs.ErrBadRequest.WithMessage("分集视频必须指定分集")
		}
		if in.Purpose != PurposeEpisodeVideo && in.EpisodeID != 0 {
			return errs.ErrBadRequest.WithMessage("该视频用途不能指定分集")
		}
	}
	return nil
}

func normalizedMIME(v string) string {
	mime, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(v)), ";")
	return strings.TrimSpace(mime)
}

func videoExtension(mime string) string {
	switch normalizedMIME(mime) {
	case "video/quicktime":
		return ".mov"
	case "video/webm":
		return ".webm"
	case "video/x-matroska":
		return ".mkv"
	default:
		return ".mp4"
	}
}

func (a Asset) PublicError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%s", err)
}

// StageKey 是视频从配置存储源复制到私有转码暂存桶后的对象 key。
// 历史会话（ProviderID=0）继续使用原 source_key，保持既有转封装任务可读。
func StageKey(providerID int64, sourceKey string) string {
	if providerID <= 0 {
		return sourceKey
	}
	return path.Join("ingest", sourceKey)
}
