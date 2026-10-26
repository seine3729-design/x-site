package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/config"
	"xsite/service/internal/crypto/mediakey"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/jobs"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/pg"
	"xsite/service/internal/store/uploadstore"
	"xsite/service/internal/transcode"
)

// NewTranscodeWorker 装配转封装任务（S1-5）：校验配置、确认 FFmpeg 可用并连接对象存储。
// MEDIA_TRANSCODE_CONCURRENCY 为 0（本进程不转封装）时返回 nil。
//
// 产物的写入目标（S1-6f）：优先写 role=primary 的存储源（按用途区分视频与图片，见 docs/12 第 5 节），
// 主存储在**进程启动时解析一次** —— 运营在后台改了主存储需要重启 worker 才生效（换主存储是低频操作，
// 换成每个任务动态解析会给每个切片都加一次查询与客户端构造）。
// 没有配置可用主存储时回退到 config.Media 的默认桶，并且不登记媒体副本（与 S1-6f 之前的行为一致）。
// enqueueReplicate 在记录产物的同一事务内入队媒体复制任务；为 nil 时不入队（产物仍会登记主存储副本）。
func NewTranscodeWorker(
	ctx context.Context, cfg config.Config, pool *pgxpool.Pool, enqueueReplicate pg.ObjectEnqueuer,
) (*jobs.TranscodeWorker, error) {
	m := cfg.Media
	if !m.TranscodeEnabled() {
		return nil, nil
	}
	if err := cfg.ValidateTranscode(); err != nil {
		return nil, err
	}
	version, err := transcode.CheckTools(ctx, m.FFmpegPath, m.FFprobePath)
	if err != nil {
		return nil, fmt.Errorf("转封装需要 FFmpeg（线上用 service/Dockerfile 的 media-worker 镜像）: %w", err)
	}
	keys, err := mediakey.ParseKeyring(m.KEKs)
	if err != nil {
		return nil, fmt.Errorf("MEDIA_KEKS: %w", err)
	}
	store, err := blob.New(blob.Config{
		Endpoint: m.S3Endpoint, Region: m.S3Region, AccessKey: m.S3AccessKey, SecretKey: m.S3SecretKey, PathStyle: m.S3PathStyle,
	})
	if err != nil {
		return nil, err
	}

	// 默认桶（没有配置主存储时的回退）
	mediaStore, imageStore := transcode.ObjectStore(store.Bucket(m.BucketMedia)), transcode.ObjectStore(store.Bucket(m.BucketAssets))
	target := "默认桶 " + m.BucketMedia
	var providerID int64
	if primary, err := resolvePrimary(ctx, cfg, pool); err != nil {
		slog.Warn("没有可用的主存储，转封装产物写入默认桶（不登记媒体副本）", "err", err)
	} else {
		mediaStore, imageStore = primary.video, primary.image
		providerID = primary.videoID
		target = fmt.Sprintf("主存储 %s（视频 %d / 图片 %d）", primary.name, primary.videoID, primary.imageID)
	}

	slog.Info("转封装已启用", "concurrency", m.TranscodeConcurrency, "ffmpeg", version, "preset", m.X264Preset,
		"source_bucket", m.SourceBucket, "target", target, "kek", keys.CurrentKID())
	return &jobs.TranscodeWorker{
		// worker 只执行任务、不提交转封装任务；产物记录后入队复制任务（S1-6f）
		Repo: pg.NewMediaRepo(pool, nil).WithReplicateEnqueuer(enqueueReplicate),
		Packager: &transcode.Pipeline{
			FFmpeg: m.FFmpegPath, FFprobe: m.FFprobePath, Preset: m.X264Preset, WorkDir: m.WorkDir,
			Source: store.Bucket(m.SourceBucket), Media: mediaStore, Images: imageStore,
		},
		Keys:       keys,
		JobTimeout: m.TranscodeTimeout,
		ProviderID: providerID,
	}, nil
}

// primaryStores 是解析出的主存储写入目标。
type primaryStores struct {
	video, image     transcode.ObjectStore
	videoID, imageID int64
	name             string
}

// resolvePrimary 解析视频与图片的主存储并建好写入客户端。两者任一不可用就整体回退（避免产物一半写新一半写旧）。
func resolvePrimary(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (primaryStores, error) {
	keyring, err := cfg.Storage.Keyring()
	if err != nil {
		return primaryStores{}, fmt.Errorf("STORAGE_KEKS: %w", err)
	}
	repo := pg.NewStorageRepo(pool)
	svc := storage.NewService(repo, keyring, nil, storage.Policy{
		HTTPSOnly: !cfg.IsDev(), Egress: netguard.Policy{AllowPrivate: cfg.EgressPrivateAllowed()},
	})
	placement := storage.NewPlacement(repo)
	egress := netguard.Policy{AllowPrivate: cfg.EgressPrivateAllowed()}

	videoRow, err := placement.Primary(ctx, storage.LineVideo)
	if err != nil {
		return primaryStores{}, err
	}
	imageRow, err := placement.Primary(ctx, storage.LineImage)
	if err != nil {
		return primaryStores{}, err
	}
	videoStore, err := writeStore(svc, videoRow, egress)
	if err != nil {
		return primaryStores{}, err
	}
	imageStore, err := writeStore(svc, imageRow, egress)
	if err != nil {
		return primaryStores{}, err
	}
	return primaryStores{
		video: videoStore, image: imageStore,
		videoID: videoRow.ID, imageID: imageRow.ID, name: videoRow.Name,
	}, nil
}

// writeStore 用存储源的读写凭据建一个写入客户端，并套上它的 path_prefix。
func writeStore(svc *storage.Service, row storage.Row, egress netguard.Policy) (transcode.ObjectStore, error) {
	cred, err := svc.WriteCredential(row)
	if err != nil {
		return nil, err
	}
	client, err := blob.New(blob.Config{
		Endpoint: row.Endpoint, Region: row.Region, AccessKey: cred.AccessKeyID, SecretKey: cred.SecretAccessKey,
		PathStyle: row.PathStyle, DialContext: egress.DialContext(nil),
	})
	if err != nil {
		return nil, fmt.Errorf("连接存储源「%s」: %w", row.Name, err)
	}
	return prefixedStore{b: client.Bucket(row.Bucket), prefix: row.PathPrefix}, nil
}

// prefixedStore 在存储桶外面套上存储源的 path_prefix：转封装只认相对路径（hls/{id}/{job}），
// 存储源自己的前缀在这里拼上，产物的 media_object.key 仍然是不含前缀的相对路径。
type prefixedStore struct {
	b      *blob.Bucket
	prefix string
}

func (s prefixedStore) PutBytes(ctx context.Context, key string, data []byte, opt blob.PutOptions) error {
	return s.b.PutBytes(ctx, storage.JoinPrefix(s.prefix, key), data, opt)
}

func (s prefixedStore) PutFile(ctx context.Context, key, path string, opt blob.PutOptions) error {
	return s.b.PutFile(ctx, storage.JoinPrefix(s.prefix, key), path, opt)
}

func (s prefixedStore) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	// JoinPrefix 保留结尾的 /（blob.DeletePrefix 要求前缀以 / 结尾，防误删同名前缀）
	return s.b.DeletePrefix(ctx, storage.JoinPrefix(s.prefix, prefix))
}

// NewAdminVideoUploadWorker 装配后台广告视频转码。它复用媒体 worker 的 FFmpeg、受限源桶和写入目标，
// 但生成公开 HLS（video/assets/<upload_id>/master.m3u8），不创建分集内容密钥。
func NewAdminVideoUploadWorker(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (*jobs.AdminVideoUploadWorker, error) {
	m := cfg.Media
	if !m.TranscodeEnabled() {
		return nil, nil
	}
	if err := cfg.ValidateTranscode(); err != nil {
		return nil, err
	}
	if _, err := transcode.CheckTools(ctx, m.FFmpegPath, m.FFprobePath); err != nil {
		return nil, fmt.Errorf("后台视频转码需要 FFmpeg: %w", err)
	}
	store, err := blob.New(blob.Config{
		Endpoint: m.S3Endpoint, Region: m.S3Region, AccessKey: m.S3AccessKey, SecretKey: m.S3SecretKey, PathStyle: m.S3PathStyle,
	})
	if err != nil {
		return nil, err
	}
	mediaStore, imageStore := transcode.ObjectStore(store.Bucket(m.BucketMedia)), transcode.ObjectStore(store.Bucket(m.BucketAssets))
	if primary, err := resolvePrimary(ctx, cfg, pool); err != nil {
		slog.Warn("没有可用的主存储，后台视频产物写入默认桶", "err", err)
	} else {
		mediaStore, imageStore = primary.video, primary.image
	}
	return &jobs.AdminVideoUploadWorker{
		Repo: pg.NewAdminMediaUploadRepo(pool, nil),
		Packager: &transcode.Pipeline{
			FFmpeg: m.FFmpegPath, FFprobe: m.FFprobePath, Preset: m.X264Preset, WorkDir: m.WorkDir,
			Source: store.Bucket(m.SourceBucket), Media: mediaStore, Images: imageStore,
		},
		Source:     store.Bucket(m.SourceBucket),
		JobTimeout: m.TranscodeTimeout,
	}, nil
}

// NewAdminMediaUploadCleanupWorker 装配后台上传会话的过期清理。它不依赖 FFmpeg，
// 即使媒体转码未启用也会在 default 队列回收失败图片与私有广告视频源。
func NewAdminMediaUploadCleanupWorker(cfg config.Config, pool *pgxpool.Pool) (*jobs.AdminMediaUploadCleanupWorker, error) {
	m := cfg.Media
	stageStore, err := blob.New(blob.Config{
		Endpoint: m.S3Endpoint, Region: m.S3Region, AccessKey: m.S3AccessKey, SecretKey: m.S3SecretKey, PathStyle: m.S3PathStyle,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化后台上传暂存桶: %w", err)
	}
	keyring, err := cfg.Storage.Keyring()
	if err != nil {
		return nil, fmt.Errorf("STORAGE_KEKS: %w", err)
	}
	repo := pg.NewStorageRepo(pool)
	egress := netguard.Policy{AllowPrivate: cfg.EgressPrivateAllowed()}
	service := storage.NewService(repo, keyring, nil, storage.Policy{HTTPSOnly: !cfg.IsDev(), Egress: egress})
	return &jobs.AdminMediaUploadCleanupWorker{
		Repo: pg.NewAdminMediaUploadRepo(pool, nil), Stores: uploadstore.New(repo, service, egress),
		LegacyImages: stageStore.Bucket(m.BucketAssets), LegacySource: stageStore.Bucket(m.SourceBucket), Stage: stageStore.Bucket(m.SourceBucket),
	}, nil
}
