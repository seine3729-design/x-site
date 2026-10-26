// Package uploadstore 把后台配置的 storage_provider 转成可写入的对象存储句柄。
// CDN/storage_line 仅负责读取分发；上传仍应对 S3/R2 源站签名写入，不能把 CDN 读域名当写端点。
package uploadstore

import (
	"context"
	"fmt"
	"io"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/domain/upload"
	"xsite/service/internal/errs"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
)

// Target 是一次上传所固定的主存储源、物理连接目标与已处理 path_prefix 的对象存储句柄。
type Target struct {
	ProviderID int64
	Endpoint   string
	Region     string
	Bucket     string
	PathStyle  bool
	PathPrefix string
	Store      blob.UploadStore
}

// Resolver 选择当前主存储源，或重新打开会话固定的物理目标。接口便于 HTTP、清理 worker 共享。
type Resolver interface {
	Primary(ctx context.Context, kind upload.Kind) (Target, error)
	Existing(ctx context.Context, a upload.Asset) (Target, error)
	Cleanup(ctx context.Context, a upload.Asset) (Target, error)
}

// ServiceResolver 复用 storage_provider、storage_settings 与凭据信封解密能力。
type ServiceResolver struct {
	repo      storage.Repo
	service   *storage.Service
	placement *storage.Placement
	egress    netguard.Policy
}

func New(repo storage.Repo, service *storage.Service, egress netguard.Policy) *ServiceResolver {
	return &ServiceResolver{repo: repo, service: service, placement: storage.NewPlacement(repo), egress: egress}
}

// Primary 按后台存储设置选择当前可写主源。未配置时明确拒绝上传，不回退到 .env 默认桶。
func (r *ServiceResolver) Primary(ctx context.Context, kind upload.Kind) (Target, error) {
	row, err := r.placement.Primary(ctx, lineKind(kind))
	if err != nil {
		return Target{}, errs.ErrBadRequest.WithMessage(fmt.Sprintf("没有可用的%s主存储，请先在「存储设置」配置主存储", kindLabel(kind)))
	}
	return r.open(row, targetFor(row))
}

// Existing 打开会话固定的 provider；draining 源允许完成已开始的上传，disabled / readonly / HTTP 源拒绝继续写入。
func (r *ServiceResolver) Existing(ctx context.Context, a upload.Asset) (Target, error) {
	if a.ProviderID <= 0 {
		return Target{}, errs.ErrBadRequest.WithMessage("上传会话没有绑定存储源，请重新选择文件")
	}
	row, err := r.repo.Get(ctx, a.ProviderID)
	if err != nil {
		return Target{}, err
	}
	if row.Status == storage.StatusDisabled || row.Role == storage.RoleReadonly || (row.Type == storage.TypeCustom && row.Subtype == storage.SubtypeHTTPOrigin) || !supports(row.Purpose, a.Kind) {
		return Target{}, errs.ErrBadRequest.WithMessage("上传会话绑定的存储源已不可写，请重新选择文件")
	}
	return r.open(row, targetForAsset(row, a))
}

// Cleanup 打开会话固定的物理目标以回收已过期对象。即使源已 disabled / readonly，也允许受信任 worker 使用当前凭据删除遗留文件。
func (r *ServiceResolver) Cleanup(ctx context.Context, a upload.Asset) (Target, error) {
	if a.ProviderID <= 0 {
		return Target{}, errs.ErrBadRequest.WithMessage("上传会话没有绑定存储源")
	}
	row, err := r.repo.Get(ctx, a.ProviderID)
	if err != nil {
		return Target{}, err
	}
	return r.open(row, targetForAsset(row, a))
}

func (r *ServiceResolver) open(row storage.Row, target Target) (Target, error) {
	cred, err := r.service.WriteCredential(row)
	if err != nil {
		return Target{}, err
	}
	client, err := blob.New(blob.Config{
		Endpoint: target.Endpoint, Region: target.Region, AccessKey: cred.AccessKeyID, SecretKey: cred.SecretAccessKey,
		PathStyle: target.PathStyle, DialContext: r.egress.DialContext(nil),
	})
	if err != nil {
		return Target{}, fmt.Errorf("连接上传存储源「%s」: %w", row.Name, err)
	}
	target.ProviderID = row.ID
	target.Store = prefixedStore{bucket: client.Bucket(target.Bucket), prefix: target.PathPrefix}
	return target, nil
}

func targetFor(row storage.Row) Target {
	return Target{
		ProviderID: row.ID, Endpoint: row.Endpoint, Region: row.Region, Bucket: row.Bucket,
		PathStyle: row.PathStyle, PathPrefix: row.PathPrefix,
	}
}

func targetForAsset(row storage.Row, a upload.Asset) Target {
	target := targetFor(row)
	// 00044 之前的会话没有快照，继续按 provider 当前目标兼容读取；新会话一律使用创建时快照。
	if a.ProviderBucket == "" {
		return target
	}
	target.Endpoint = a.ProviderEndpoint
	target.Region = a.ProviderRegion
	target.Bucket = a.ProviderBucket
	target.PathStyle = a.ProviderPathStyle
	target.PathPrefix = a.ProviderPathPrefix
	return target
}

func lineKind(kind upload.Kind) storage.LineKind {
	if kind == upload.KindImage {
		return storage.LineImage
	}
	return storage.LineVideo
}

func supports(purpose storage.Purpose, kind upload.Kind) bool {
	if kind == upload.KindImage {
		return purpose == storage.PurposeImage || purpose == storage.PurposeBoth
	}
	return purpose == storage.PurposeVideo || purpose == storage.PurposeBoth
}

func kindLabel(kind upload.Kind) string {
	if kind == upload.KindImage {
		return "图片"
	}
	return "视频"
}

type prefixedStore struct {
	bucket *blob.Bucket
	prefix string
}

func (s prefixedStore) PutStream(ctx context.Context, key string, body io.Reader, size int64, opt blob.PutOptions) error {
	return s.bucket.PutStream(ctx, storage.JoinPrefix(s.prefix, key), body, size, opt)
}

func (s prefixedStore) Head(ctx context.Context, key string) (blob.ObjectInfo, error) {
	return s.bucket.Head(ctx, storage.JoinPrefix(s.prefix, key))
}

func (s prefixedStore) Get(ctx context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error) {
	body, info, err := s.bucket.Get(ctx, storage.JoinPrefix(s.prefix, key))
	return body, info, err
}

func (s prefixedStore) Delete(ctx context.Context, key string) error {
	return s.bucket.Delete(ctx, storage.JoinPrefix(s.prefix, key))
}
