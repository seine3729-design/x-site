package app

import (
	"context"
	"fmt"
	"time"

	"xsite/service/internal/config"
	"xsite/service/internal/crypto/mediakey"
	"xsite/service/internal/crypto/playtoken"
	"xsite/service/internal/domain/moderation"
	"xsite/service/internal/domain/play"
	"xsite/service/internal/domain/resourcecheck"
	"xsite/service/internal/domain/resourcewarm"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
	"xsite/service/internal/geoip"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob/product"
	"xsite/service/internal/store/cache"
	"xsite/service/internal/store/pg"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"
)

// warmPlaylists 让资源预热复用播放链路取清单（resourcewarm.Playlists）。
//
// 这是预热正确性的关键：预热拉的必须是**播放器真正会请求的那些地址**。
// 自己拼地址一旦签名方式或路径与播放链路有偏差，边缘缓存的就是另一个 key，预热白做。
// 所以这里自签一个播放令牌、在进程内直接调 HLSService.Playlist，拿到的清单与观众拿到的是同一套。
type warmPlaylists struct {
	hls   *play.HLSService
	keys  *playtoken.Keyring
	lines WarmLineLookup
	// now 可替换，便于测试。
	now func() time.Time
}

// WarmLineLookup 为一集挑一条视频线路（domain/storage.LinesService）。
type WarmLineLookup interface {
	// WarmLine 返回预热这一集该用哪条线路。
	WarmLine(ctx context.Context, episodeID, objectID int64) (lineID int64, err error)
}

// warmLines 用选源那条路挑线路（resourcewarm 经 WarmLineLookup 使用）。
type warmLines struct {
	lines *storage.LinesService
	repo  warmEpisodeRepo
}

// warmEpisodeRepo 读一集的产物对象（pg.WarmPlayRepo）。
type warmEpisodeRepo interface {
	PlayableEpisode(ctx context.Context, id int64) (play.Episode, error)
}

// NewWarmLines 组装预热的选线。
func NewWarmLines(lines *storage.LinesService, repo warmEpisodeRepo) WarmLineLookup {
	return &warmLines{lines: lines, repo: repo}
}

// WarmLine 挑预热这一集用的线路。
//
// 走选源那条路（ForPlayback）而不是自己找 priority 最小的：它已经做了副本过滤
// （产物没复制到的存储源上的线路不能用）与健康排序，取第一条就是观众最可能拿到的主力线路。
// 不传 IP / Geo：预热不针对某个地区，热的是全局主力线路；逐地区热等于按线路数翻倍流量，
// 不是这个功能的目标。
func (w *warmLines) WarmLine(ctx context.Context, episodeID, objectID int64) (int64, error) {
	if objectID == 0 {
		ep, err := w.repo.PlayableEpisode(ctx, episodeID)
		if err != nil {
			return 0, err
		}
		objectID = ep.VideoObjectID
	}
	sel, err := w.lines.ForPlayback(ctx, storage.PlaybackInput{ObjectID: objectID})
	if err != nil {
		return 0, err
	}
	if len(sel.Lines) == 0 {
		return 0, errs.ErrUnavailable.WithMessage("没有可用的视频线路，无法预热")
	}
	return sel.Lines[0].LineID, nil
}

// NewWarmPlaylists 组装预热用的清单取数器。
func NewWarmPlaylists(hls *play.HLSService, keys *playtoken.Keyring, lines WarmLineLookup) resourcewarm.Playlists {
	return &warmPlaylists{hls: hls, keys: keys, lines: lines, now: time.Now}
}

// tokenTTL 是自签播放令牌的有效期。
//
// 比对外的 10 分钟（play.TokenTTL）短：令牌只在本进程内用一次，拿完清单就丢。
// 短有效期让它即使意外落进日志也很快失效。
const tokenTTL = 2 * time.Minute

// token 为一集自签一个播放令牌。
//
// TrialSec 为 0（完整清单，不是试看那几片）、Device 为空（不绑设备，预热没有设备）。
func (w *warmPlaylists) token(ctx context.Context, episodeID int64) (string, error) {
	lineID, err := w.lines.WarmLine(ctx, episodeID, 0)
	if err != nil {
		return "", err
	}
	return w.keys.Sign(playtoken.Claims{
		EpisodeID: episodeID, LineID: lineID, TrialSec: 0, ExpiresAt: w.now().Add(tokenTTL),
	})
}

func (w *warmPlaylists) Master(ctx context.Context, episodeID int64) ([]byte, error) {
	t, err := w.token(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	return w.hls.Playlist(ctx, play.PlaylistInput{Token: t, File: play.IndexFile})
}

func (w *warmPlaylists) Variant(ctx context.Context, episodeID int64, file string) ([]byte, error) {
	t, err := w.token(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	return w.hls.Playlist(ctx, play.PlaylistInput{Token: t, File: file})
}

// warmChecks 把预热失败项记进体检结果（resourcewarm.CheckRecorder）。
type warmChecks struct {
	svc *resourcecheck.Service
}

// NewWarmChecks 组装预热失败项的记录器。
func NewWarmChecks(svc *resourcecheck.Service) resourcewarm.CheckRecorder {
	return &warmChecks{svc: svc}
}

func (w *warmChecks) RecordWarmFailure(ctx context.Context, dramaID, episodeID int64, url string, lineID int64, res resourcewarm.FetchResult) error {
	if err := w.svc.RecordWarmFailure(ctx, dramaID, episodeID, url, lineID, resourcecheck.WarmFailure{
		HTTPStatus: res.HTTPStatus, Unreachable: res.Unreachable, Message: res.Message,
	}); err != nil {
		return fmt.Errorf("记录预热失败项: %w", err)
	}
	return nil
}

// NewWarmHLSService 为 worker 组一个清单服务，用于资源预热。
//
// 与 API 侧的 newHLSService 唯一的区别是 repo：这里传预热专用的（不要求剧已上架）。
// 其余依赖完全相同 —— 正因为同一套代码、同一个签名服务，预热拿到的地址才和观众的一致。
//
// tokens 必须传：预热虽然是自签令牌，但 Playlist 内部仍会用同一个密钥环验一遍。
// 这是有意的闭环，不是绕过校验 —— 自签的令牌和观众的令牌走同一条校验路径，
// 产物不可播（下架、副本撤掉、线路停用）时预热也会如实失败，而不是热出一批观众拿不到的地址。
func NewWarmHLSService(
	cfg config.Config, tokens play.TokenVerifier, repo play.HLSRepo, lines hlsStorage,
	creds storage.CredentialOpener, signer play.SegmentSigner, egress netguard.Policy, vk valkey.Client,
) (*play.HLSService, error) {
	keys, err := mediakey.ParseKeyring(cfg.Media.KEKs)
	if err != nil {
		return nil, fmt.Errorf("MEDIA_KEKS: %w", err)
	}
	// 清单原文的读穿缓存用的是和播放接口同一个键前缀：预热把它热上，观众那边直接命中
	files := product.NewReader(lines, creds, egress, defaultMediaBucket(cfg.Media), cache.New(vk, "xsite:play:manifest:"))
	return play.NewHLSService(tokens, repo, lines, files, signer, keys), nil
}

// NewWarmLineLookup 为 worker 组一个选线器（预热用）。
func NewWarmLineLookup(
	cfg config.Config, pool *pgxpool.Pool, vk valkey.Client, repo *pg.StorageRepo, eps warmEpisodeRepo,
) (WarmLineLookup, error) {
	geo, _, err := geoip.NewStorageResolver(cfg.Storage.GeoIPXDBPath)
	if err != nil {
		return nil, fmt.Errorf("GEOIP_XDB_PATH: %w", err)
	}
	health := cache.NewLineHealthStore(vk, "xsite:line:health:")
	quality := cache.NewLineQualityStore(vk, "xsite:line:q:")
	return NewWarmLines(storage.NewLinesService(repo, geo, health, quality), eps), nil
}

// blockwordMatcher 让 CSV 批量导入用上常驻的屏蔽词自动机（contentimport.Blockwords）。
//
// 只暴露 HasBlock：采集场景只认 block 类命中，mask 与 review 在这里不做任何事
// （docs/02 第 5.7 节「采集只认 block」）。窄接口也让导入的单测不必搭一整个 moderation.Service。
type blockwordMatcher struct {
	svc *moderation.Service
}

func (b blockwordMatcher) HasBlock(text string) bool {
	return b.svc.Matcher().HasBlock(text)
}
