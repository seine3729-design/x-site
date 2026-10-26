package play

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path"
	"slices"
	"strconv"
	"time"

	"xsite/service/internal/crypto/playtoken"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
)

// 清单与密钥（S1-8，docs/03 第 4.2 节、docs/specs/S1-8.md）：播放器拿选源返回的清单地址，经加密接口取主清单、
// 改写过的各档清单与内容密钥。本服务只发清单与密钥，切片由线路（CDN / 存储源）直接提供，不经过业务进程。

var (
	// ErrPlayTokenExpired 表示播放令牌已过期：客户端重新选源（不是登录过期，不走登录态刷新）。
	ErrPlayTokenExpired = errs.ErrTokenExpired.WithMessage("播放地址已过期，请重新获取")
	// ErrPlayTokenInvalid 表示播放令牌格式错误、签名不对、不是签给本设备的，或与请求的分集不符。不区分原因。
	ErrPlayTokenInvalid = errs.ErrForbidden.WithMessage("播放地址无效，请重新获取")
	// ErrLineUnavailable 表示令牌所指线路已不能提供这一集（停用、删除、不是视频线路、产物不在其存储源上，
	// 或读不到产物清单、签不出地址），播放器换下一条线路。
	ErrLineUnavailable = errs.ErrUnavailable.WithMessage("线路暂不可用，请切换线路")
)

// TokenVerifier 校验播放令牌（crypto/playtoken.Keyring）。
type TokenVerifier interface {
	Verify(token string, now time.Time) (playtoken.Claims, error)
}

// HLSRepo 读取清单与密钥接口需要的数据。
type HLSRepo interface {
	Repo
	// MediaKey 返回一集某个版本的内容密钥（信封加密的密文）；没有时返回 errs.ErrNotFound。
	MediaKey(ctx context.Context, episodeID int64, keyVer int32) ([]byte, error)
}

// LineLookup 读取线路与产物副本（domain/storage.Repo）。线路不存在时返回 errs.ErrNotFound。
type LineLookup interface {
	GetLine(ctx context.Context, id int64) (storage.Line, error)
	ReadyProviders(ctx context.Context, objectID int64) ([]int64, error)
}

// SegmentSigner 为产物里的一批对象生成线路上的访问地址（domain/storage.SignService.SignSegments）。
type SegmentSigner interface {
	SignSegments(ctx context.Context, line storage.Line, prefix string, keys []string, now time.Time) ([]string, error)
}

// KeyUnwrapper 解开内容密钥（crypto/mediakey.Keyring）。
type KeyUnwrapper interface {
	Unwrap(episodeID int64, keyVer int32, wrapped []byte) ([]byte, error)
}

// Product 是一集的当前产物。Key 是产物目录（存储源内的相对路径，不带结尾的 /）；
// Providers 是读取清单原文时依次尝试的存储源（已就绪副本所在，令牌所指线路的存储源在前），为空表示读默认存储。
type Product struct {
	ObjectID  int64
	Key       string
	Providers []int64
}

// ProductFiles 读取产物目录里的小文件（清单原文，store/blob/product）。
type ProductFiles interface {
	ReadFile(ctx context.Context, p Product, name string) ([]byte, error)
}

// PlaylistInput 是一次清单请求。File 为 IndexFile（主清单）或主清单中列出的一档。
type PlaylistInput struct {
	Token    string
	File     string
	DeviceID string
	// KeyBase 是改写后清单里内容密钥接口的前缀，留空用前台的 PublicKeyBase。
	// 后台试播要用 AdminKeyBase：它的播放器走后台会话、不加密，取不到前台的加密密钥接口。
	KeyBase string
}

// KeyInput 是一次密钥请求。
type KeyInput struct {
	Token     string
	EpisodeID int64
	KeyVer    int32
	DeviceID  string
}

// HLSService 是清单与密钥服务。
type HLSService struct {
	tokens TokenVerifier
	repo   HLSRepo
	lines  LineLookup
	files  ProductFiles
	signer SegmentSigner
	keys   KeyUnwrapper
	now    func() time.Time
}

func NewHLSService(tokens TokenVerifier, repo HLSRepo, lines LineLookup, files ProductFiles, signer SegmentSigner, keys KeyUnwrapper) *HLSService {
	return &HLSService{tokens: tokens, repo: repo, lines: lines, files: files, signer: signer, keys: keys, now: time.Now}
}

// Playlist 返回清单：主清单原样返回；一档清单改写切片与密钥地址，试看令牌只保留前 N 秒。
func (s *HLSService) Playlist(ctx context.Context, in PlaylistInput) ([]byte, error) {
	now := s.now()
	claims, err := s.verify(in.Token, in.DeviceID, now)
	if err != nil {
		return nil, err
	}
	if in.File != IndexFile && !variantNameRe.MatchString(in.File) {
		return nil, errs.ErrNotFound
	}
	// 每次都重新确认可播放：下架、删除、产物被撤下立即生效。观看权限以令牌为准（选源时已判定）
	ep, err := s.repo.PlayableEpisode(ctx, claims.EpisodeID)
	if err != nil {
		return nil, err
	}
	line, product, err := s.route(ctx, claims.LineID, ep)
	if err != nil {
		return nil, err
	}

	master, err := s.read(ctx, product, masterFile, claims)
	if err != nil {
		return nil, err
	}
	variants, err := parseMaster(master)
	if err != nil {
		s.warn(ctx, "主清单无效", claims, err)
		return nil, ErrLineUnavailable
	}
	if in.File == IndexFile {
		return master, nil
	}
	// 只能取主清单列出的档位，不能借此读产物目录里的其他文件
	if !slices.Contains(variants, in.File) {
		return nil, errs.ErrNotFound
	}

	raw, err := s.read(ctx, product, in.File, claims)
	if err != nil {
		return nil, err
	}
	pl, err := parseMedia(raw)
	if err != nil {
		s.warn(ctx, "清单无效", claims, err, "file", in.File)
		return nil, ErrLineUnavailable
	}
	keep := pl.trialCut(claims.TrialSec)
	uris := pl.objectURIs(keep)
	keys := make([]string, len(uris))
	for i, u := range uris {
		keys[i] = path.Join(ep.ObjectKey, u)
	}
	signed, err := s.signer.SignSegments(ctx, line, ep.ObjectKey+"/"+segmentDir, keys, now)
	if err != nil {
		s.warn(ctx, "签名切片地址失败", claims, err)
		return nil, ErrLineUnavailable
	}
	if len(signed) != len(uris) {
		return nil, fmt.Errorf("签名结果数量不符：%d / %d", len(signed), len(uris))
	}
	byURI := make(map[string]string, len(uris))
	for i, u := range uris {
		byURI[u] = signed[i]
	}
	out, err := pl.render(keep,
		func(uri string) string { return byURI[uri] },
		func(ver int32) string { return KeyURL(in.KeyBase, ep.ID, ver, in.Token) })
	if err != nil {
		return nil, fmt.Errorf("改写清单: %w", err)
	}
	return out, nil
}

// Key 返回内容密钥（16 字节）。令牌里的分集必须与请求一致；key_ver 可以是该集任何仍在库里的版本。
func (s *HLSService) Key(ctx context.Context, in KeyInput) ([]byte, error) {
	claims, err := s.verify(in.Token, in.DeviceID, s.now())
	if err != nil {
		return nil, err
	}
	if claims.EpisodeID != in.EpisodeID {
		return nil, ErrPlayTokenInvalid
	}
	if in.KeyVer <= 0 {
		return nil, errs.ErrNotFound
	}
	if _, err := s.repo.PlayableEpisode(ctx, in.EpisodeID); err != nil {
		return nil, err
	}
	wrapped, err := s.repo.MediaKey(ctx, in.EpisodeID, in.KeyVer)
	if err != nil {
		return nil, err
	}
	key, err := s.keys.Unwrap(in.EpisodeID, in.KeyVer, wrapped)
	if err != nil {
		return nil, fmt.Errorf("解开第 %d 集第 %d 版内容密钥: %w", in.EpisodeID, in.KeyVer, err)
	}
	return key, nil
}

// verify 校验令牌并核对设备：过期 401，其余问题一律 403（不区分原因）。
func (s *HLSService) verify(token, deviceID string, now time.Time) (playtoken.Claims, error) {
	if token == "" {
		return playtoken.Claims{}, ErrPlayTokenInvalid
	}
	claims, err := s.tokens.Verify(token, now)
	switch {
	case errors.Is(err, playtoken.ErrExpired):
		return playtoken.Claims{}, ErrPlayTokenExpired
	case err != nil:
		return playtoken.Claims{}, ErrPlayTokenInvalid
	case !claims.BoundTo(deviceID):
		return playtoken.Claims{}, ErrPlayTokenInvalid
	}
	return claims, nil
}

// route 确认令牌所指线路仍能提供这一集，返回线路与读取清单原文的产物位置。
// 产物有就绪副本时线路的存储源必须在其中（选源之后迁移或撤副本的，换线路）；没有任何副本记录时
// （S1-6f 之前写入默认存储的产物）不按副本过滤、从默认存储读清单，与选源一致。
func (s *HLSService) route(ctx context.Context, lineID int64, ep Episode) (storage.Line, Product, error) {
	line, err := s.lines.GetLine(ctx, lineID)
	if errors.Is(err, errs.ErrNotFound) {
		return storage.Line{}, Product{}, ErrLineUnavailable
	}
	if err != nil {
		return storage.Line{}, Product{}, err
	}
	if line.Kind != storage.LineVideo || line.Status == storage.LineDisabled {
		return storage.Line{}, Product{}, ErrLineUnavailable
	}
	ready, err := s.lines.ReadyProviders(ctx, ep.VideoObjectID)
	if err != nil {
		return storage.Line{}, Product{}, err
	}
	p := Product{ObjectID: ep.VideoObjectID, Key: ep.ObjectKey}
	if len(ready) > 0 {
		if !slices.Contains(ready, line.ProviderID) {
			return storage.Line{}, Product{}, ErrLineUnavailable
		}
		p.Providers = append(p.Providers, line.ProviderID)
		for _, id := range ready {
			if id != line.ProviderID {
				p.Providers = append(p.Providers, id)
			}
		}
	}
	return line, p, nil
}

// read 读取产物里的清单原文；读不到时按线路不可用处理（播放器换线路），原因记日志。
func (s *HLSService) read(ctx context.Context, p Product, name string, claims playtoken.Claims) ([]byte, error) {
	data, err := s.files.ReadFile(ctx, p, name)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		s.warn(ctx, "读取产物清单失败", claims, err, "file", name, "object_id", p.ObjectID)
		return nil, ErrLineUnavailable
	}
	return data, nil
}

// warn 记录导致 503 的原因（不记令牌本身）。
func (s *HLSService) warn(ctx context.Context, msg string, c playtoken.Claims, err error, kv ...any) {
	args := append([]any{"episode_id", c.EpisodeID, "line_id", c.LineID, "err", err}, kv...)
	slog.WarnContext(ctx, msg, args...)
}

const (
	// PublicKeyBase 是前台内容密钥接口的前缀（同源加密接口）。
	PublicKeyBase = "/api/v1/hls/key"
	// AdminKeyBase 是运营后台试播的内容密钥接口前缀（后台会话，不加密；docs/06 第 37 节）。
	AdminKeyBase = "/api/admin/preview/key"
)

// KeyURL 是清单里内容密钥的地址，令牌放在查询串（与清单地址中的令牌相同）。base 留空用前台接口。
func KeyURL(base string, episodeID int64, keyVer int32, token string) string {
	if base == "" {
		base = PublicKeyBase
	}
	return base + "/" + strconv.FormatInt(episodeID, 10) + "/" + strconv.FormatInt(int64(keyVer), 10) +
		"?t=" + url.QueryEscape(token)
}
