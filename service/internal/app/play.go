package app

import (
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/config"
	"xsite/service/internal/crypto/mediakey"
	"xsite/service/internal/crypto/playtoken"
	"xsite/service/internal/domain/play"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/blob/product"
	"xsite/service/internal/store/cache"
	"xsite/service/internal/store/pg"
)

// hlsStorage 是清单服务需要的存储源仓储能力（pg.StorageRepo）：读线路与副本、读存储源。
type hlsStorage interface {
	play.LineLookup
	product.ProviderRepo
}

// newHLSService 组装清单与密钥服务（S1-8，docs/specs/S1-8.md）。API 进程从 S1-8 起需要 MEDIA_KEKS（解开内容密钥，
// 缺失或格式错误时启动失败）；清单原文从产物的就绪副本所在存储源读取（只读凭据 + 出站校验），
// 没有副本记录的老产物读默认存储 S3_BUCKET_MEDIA。清单原文的读穿缓存与其他缓存分开键前缀。
func newHLSService(
	cfg config.Config, pool *pgxpool.Pool, vk valkey.Client, tokens play.TokenVerifier,
	repo hlsStorage, creds storage.CredentialOpener, signer play.SegmentSigner, egress netguard.Policy,
) (*play.HLSService, error) {
	keys, err := mediakey.ParseKeyring(cfg.Media.KEKs)
	if err != nil {
		return nil, fmt.Errorf("MEDIA_KEKS: %w", err)
	}
	files := product.NewReader(repo, creds, egress, defaultMediaBucket(cfg.Media), cache.New(vk, "xsite:play:manifest:"))
	return play.NewHLSService(tokens, pg.NewPlayRepo(pool), repo, files, signer, keys), nil
}

// newPreviewServices 组装运营后台的分集试播（docs/06 第 37 节，S1-8 的后台部分）。
//
// 复用播放链路的三件事是有意的：同一个 HLSService 改写清单、同一个线路签名器签切片、同一套内容密钥 ——
// 后台看到的就是观众会看到的东西，试播通过才说明产物与线路真的可用。
//
// 刻意不同的两处：
//  1. 仓储用 pg.WarmPlayRepo（不要求剧已上架），运营要在上架前先验；
//  2. 令牌用 playtoken.AdminPreview 派生的密钥环，与前台令牌互不通用（前台密钥环验不过它，反之亦然），
//     而且这组路由挂在后台会话 + dramas 权限点之后。
func newPreviewServices(
	cfg config.Config, pool *pgxpool.Pool, vk valkey.Client, lines play.LineSelector,
	repo hlsStorage, creds storage.CredentialOpener, signer play.SegmentSigner, egress netguard.Policy,
) (*play.PreviewService, *play.HLSService, error) {
	tokens, err := playtoken.ParseKeyringFor(cfg.Play.TokenKeys, playtoken.AdminPreview)
	if err != nil {
		return nil, nil, fmt.Errorf("PLAY_TOKEN_KEYS（后台试播）: %w", err)
	}
	keys, err := mediakey.ParseKeyring(cfg.Media.KEKs)
	if err != nil {
		return nil, nil, fmt.Errorf("MEDIA_KEKS: %w", err)
	}
	previewRepo := pg.NewWarmPlayRepo(pool)
	// 与播放接口同一个清单缓存键前缀：后台试播过的清单，观众那边直接命中。
	files := product.NewReader(repo, creds, egress, defaultMediaBucket(cfg.Media), cache.New(vk, "xsite:play:manifest:"))
	hls := play.NewHLSService(tokens, previewRepo, repo, files, signer, keys)
	return play.NewPreviewService(previewRepo, lines, tokens), hls, nil
}

// defaultMediaBucket 是默认存储的产物桶（S3_BUCKET_MEDIA，运维配置的端点，不经出站校验）。
// 没有配置时返回 nil：此时只能播放有就绪副本的产物。
func defaultMediaBucket(m config.Media) product.ObjectGetter {
	if m.BucketMedia == "" || m.S3AccessKey == "" || m.S3SecretKey == "" {
		slog.Warn("没有配置默认存储（S3_BUCKET_MEDIA、S3_ACCESS_KEY、S3_SECRET_KEY），没有副本记录的产物将无法播放")
		return nil
	}
	client, err := blob.New(blob.Config{
		Endpoint: m.S3Endpoint, Region: m.S3Region, AccessKey: m.S3AccessKey, SecretKey: m.S3SecretKey, PathStyle: m.S3PathStyle,
	})
	if err != nil {
		slog.Warn("默认存储不可用，没有副本记录的产物将无法播放", "err", err)
		return nil
	}
	return client.Bucket(m.BucketMedia)
}
