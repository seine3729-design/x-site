package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"time"

	"uuid"

	"xsite/service/internal/domain/upload"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/uploadstore"
)

const (
	maxImageBytes      = 15 << 20
	maxVideoBytes      = 4 << 30
	maxRobotsBytes     = 1 << 20
	defaultRequestWait = 30 * time.Minute
	defaultInterval    = 500 * time.Millisecond
)

// StoreResolver 是测试采集器使用的最小存储能力。它复用后台主存储选择与会话快照清理逻辑。
type StoreResolver interface {
	Primary(ctx context.Context, kind upload.Kind) (uploadstore.Target, error)
	Cleanup(ctx context.Context, a upload.Asset) (uploadstore.Target, error)
}

// Options 控制下载节流和超时。AllowHTTP 只能由测试直接注入，collectortool 永远保持关闭。
type Options struct {
	AllowHTTP          bool
	RequestTimeout     time.Duration
	MinRequestInterval time.Duration
	Now                func() time.Time
}

// DefaultOptions 返回命令应使用的保守下载参数。
func DefaultOptions() Options {
	return Options{RequestTimeout: defaultRequestWait, MinRequestInterval: defaultInterval, Now: time.Now}
}

// Service 负责“授权 manifest → robots 校验 → 下载 → 主存储暂存”的单次采集。
// 这一步不创建剧集、分集或发布状态，避免没有人工预览时把远端内容直接推到前台。
type Service struct {
	stores  StoreResolver
	fetcher Fetcher
	options Options
}

func NewService(stores StoreResolver, fetcher Fetcher, options Options) *Service {
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = defaultRequestWait
	}
	if options.MinRequestInterval < 0 {
		options.MinRequestInterval = 0
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{stores: stores, fetcher: fetcher, options: options}
}

// Collect 暂存 manifest 中的封面和视频。任何一步失败都尽力删除此前已写入的对象。
func (s *Service) Collect(ctx context.Context, manifest Manifest) (Result, error) {
	if s.stores == nil || s.fetcher == nil {
		return Result{}, errors.New("采集器依赖未配置")
	}
	normalized, err := validateManifest(manifest, s.options.AllowHTTP, s.options.Now().UTC())
	if err != nil {
		return Result{}, err
	}
	run := collectionRun{service: s}
	robots, err := run.loadRobots(ctx, normalized.Source)
	if err != nil {
		return Result{}, err
	}
	images, err := s.stores.Primary(ctx, upload.KindImage)
	if err != nil {
		return Result{}, err
	}
	videos, err := s.stores.Primary(ctx, upload.KindVideo)
	if err != nil {
		return Result{}, err
	}

	out := Result{RunID: uuid.NewV7().String(), SourceID: normalized.Source.ID}
	for _, drama := range normalized.Dramas {
		coverKey := path.Join("img", "collector", out.RunID, drama.ID, "cover"+extensionForMIME(drama.Cover.MIME))
		cover, err := run.stage(ctx, images, drama.Cover, upload.KindImage, coverKey, robots)
		if err != nil {
			return s.collectFailed(ctx, out, fmt.Errorf("暂存短剧 %s 封面: %w", drama.ID, err))
		}
		out.Objects = append(out.Objects, cover)
		collected := CollectedDrama{ID: drama.ID, Title: drama.Title, Summary: drama.Summary, CoverKey: coverKey}
		for _, episode := range drama.Episodes {
			videoKey := path.Join("video", "source", "collector", out.RunID, drama.ID, episode.ID+extensionForMIME(episode.Video.MIME))
			video, err := run.stage(ctx, videos, episode.Video, upload.KindVideo, videoKey, robots)
			if err != nil {
				return s.collectFailed(ctx, out, fmt.Errorf("暂存短剧 %s 分集 %s 视频: %w", drama.ID, episode.ID, err))
			}
			out.Objects = append(out.Objects, video)
			collected.Episodes = append(collected.Episodes, CollectedEpisode{
				ID: episode.ID, Number: episode.Number, Title: episode.Title, SourceKey: videoKey,
			})
		}
		out.Dramas = append(out.Dramas, collected)
	}
	return out, nil
}

func (s *Service) collectFailed(ctx context.Context, partial Result, cause error) (Result, error) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancel()
	if cleanupErr := s.Cleanup(cleanupCtx, partial); cleanupErr != nil {
		partial.CleanupPending = true
		return partial, errors.Join(cause, fmt.Errorf("已写入对象清理失败，需要用报告执行 cleanup: %w", cleanupErr))
	}
	return Result{}, cause
}

// Cleanup 按 Result 内的存储目标快照删除一次测试采集写入的对象。
func (s *Service) Cleanup(ctx context.Context, result Result) error {
	if result.RunID == "" {
		return badRequest("采集报告缺少 run_id")
	}
	var all []error
	for i := len(result.Objects) - 1; i >= 0; i-- {
		object := result.Objects[i]
		if !collectorKey(result.RunID, object.Key) {
			all = append(all, badRequest("报告中的对象不属于本次采集，拒绝删除"))
			continue
		}
		asset := upload.Asset{
			ProviderID: object.ProviderID, ProviderEndpoint: object.ProviderEndpoint, ProviderRegion: object.ProviderRegion,
			ProviderBucket: object.ProviderBucket, ProviderPathStyle: object.ProviderPathStyle, ProviderPathPrefix: object.ProviderPathPrefix,
			Kind: object.Kind,
		}
		target, err := s.stores.Cleanup(ctx, asset)
		if err != nil {
			all = append(all, err)
			continue
		}
		if err := target.Store.Delete(ctx, object.Key); err != nil && !errors.Is(err, blob.ErrNotFound) {
			all = append(all, err)
		}
	}
	return errors.Join(all...)
}

func (r *collectionRun) loadRobots(ctx context.Context, source Source) (map[string]Robots, error) {
	out := make(map[string]Robots, len(source.Origins))
	for _, origin := range source.Origins {
		response, err := r.fetch(ctx, origin.RobotsURL, maxRobotsBytes)
		if err != nil {
			return nil, fmt.Errorf("读取下载域名 %s 的 robots 失败: %w", origin.Host, err)
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, maxRobotsBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("读取下载域名 %s 的 robots 失败", origin.Host)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("关闭下载域名 %s 的 robots 响应失败", origin.Host)
		}
		if len(data) > maxRobotsBytes {
			return nil, badRequest("下载域名 %s 的 robots 文件过大", origin.Host)
		}
		policy, err := ParseRobots(data, defaultCollectorUserAgent)
		if err != nil {
			return nil, fmt.Errorf("下载域名 %s 的 robots 规则不支持: %w", origin.Host, err)
		}
		out[origin.Host] = policy
	}
	return out, nil
}

func (r *collectionRun) stage(ctx context.Context, target uploadstore.Target, asset Asset, kind upload.Kind, key string, robots map[string]Robots) (StoredObject, error) {
	u, err := url.Parse(asset.URL)
	if err != nil {
		return StoredObject{}, badRequest("媒体 URL 无效")
	}
	policy, ok := robots[strings.ToLower(u.Hostname())]
	if !ok || !policy.Allowed(asset.URL) {
		return StoredObject{}, badRequest("robots 规则不允许采集该媒体")
	}
	limit := int64(maxImageBytes)
	if kind == upload.KindVideo {
		limit = maxVideoBytes
	}
	response, err := r.fetch(ctx, asset.URL, limit)
	if err != nil {
		return StoredObject{}, err
	}
	defer response.Body.Close()
	if response.Size <= 0 || response.Size > limit {
		return StoredObject{}, badRequest("远端媒体没有可信的 Content-Length 或超过允许大小")
	}
	if response.MIME != asset.MIME {
		return StoredObject{}, badRequest("远端媒体 Content-Type 与授权 manifest 不一致")
	}
	headerSize := min(response.Size, int64(64))
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(response.Body, header); err != nil {
		return StoredObject{}, badRequest("远端媒体内容长度与声明不一致")
	}
	if err := validateMagic(kind, asset.MIME, header); err != nil {
		return StoredObject{}, err
	}
	stream := io.MultiReader(bytes.NewReader(header), response.Body)
	cacheControl := "private, no-store"
	if kind == upload.KindImage {
		cacheControl = "public, max-age=31536000, immutable"
	}
	if err := target.Store.PutStream(ctx, key, stream, response.Size, blob.PutOptions{ContentType: asset.MIME, CacheControl: cacheControl}); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		if cleanupErr := target.Store.Delete(cleanupCtx, key); cleanupErr != nil && !errors.Is(cleanupErr, blob.ErrNotFound) {
			return StoredObject{}, errors.Join(fmt.Errorf("写入主存储失败: %w", err), fmt.Errorf("删除可能残留的对象失败: %w", cleanupErr))
		}
		return StoredObject{}, fmt.Errorf("写入主存储失败: %w", err)
	}
	return StoredObject{
		Kind: kind, Key: key, MIME: asset.MIME, SizeBytes: response.Size,
		ProviderID: target.ProviderID, ProviderEndpoint: target.Endpoint, ProviderRegion: target.Region,
		ProviderBucket: target.Bucket, ProviderPathStyle: target.PathStyle, ProviderPathPrefix: target.PathPrefix,
	}, nil
}

type collectionRun struct {
	service   *Service
	lastFetch time.Time
}

func (r *collectionRun) fetch(ctx context.Context, rawURL string, maxBytes int64) (Fetched, error) {
	if wait := r.lastFetch.Add(r.service.options.MinRequestInterval).Sub(r.service.options.Now()); !r.lastFetch.IsZero() && wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return Fetched{}, ctx.Err()
		case <-timer.C:
		}
	}
	requestCtx, cancel := context.WithTimeout(ctx, r.service.options.RequestTimeout)
	defer cancel()
	response, err := r.service.fetcher.Fetch(requestCtx, rawURL, maxBytes)
	r.lastFetch = r.service.options.Now()
	return response, err
}

func validateMagic(kind upload.Kind, mime string, header []byte) error {
	switch kind {
	case upload.KindImage:
		switch mime {
		case "image/webp":
			if len(header) < 12 || !bytes.Equal(header[:4], []byte("RIFF")) || !bytes.Equal(header[8:12], []byte("WEBP")) {
				return badRequest("远端封面不是有效 WebP")
			}
		case "image/avif":
			if len(header) < 12 || !bytes.Equal(header[4:8], []byte("ftyp")) || (!bytes.Equal(header[8:12], []byte("avif")) && !bytes.Equal(header[8:12], []byte("avis"))) {
				return badRequest("远端封面不是有效 AVIF")
			}
		}
	case upload.KindVideo:
		switch mime {
		case "video/mp4", "video/quicktime":
			if len(header) < 12 || !bytes.Equal(header[4:8], []byte("ftyp")) {
				return badRequest("远端视频不是有效 MP4/MOV")
			}
		case "video/webm", "video/x-matroska":
			if len(header) < 4 || !bytes.Equal(header[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) {
				return badRequest("远端视频不是有效 WebM/MKV")
			}
		}
	}
	return nil
}

func extensionForMIME(mime string) string {
	switch mime {
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
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

func collectorKey(runID, key string) bool {
	return strings.HasPrefix(key, "img/collector/"+runID+"/") || strings.HasPrefix(key, "video/source/collector/"+runID+"/")
}
