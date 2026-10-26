package play

import (
	"context"
	"time"

	"xsite/service/internal/crypto/playtoken"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
)

// 后台试播（docs/06 第 37 节、docs/specs/S1-8.md「范围外」那条）：运营在后台直接播一集，确认产物与线路真的通。
//
// 与前台选源的区别，三条都是刻意的：
//  1. 不判观看权限、不裁剪试看（TrialSec 为 0），运营要看完整的一集；
//  2. 允许未上架的剧（repo 用不要求上架的那套），运营要在上架前先验；
//  3. 令牌用 playtoken.AdminPreview 派生，与前台令牌互不通用，且只有后台会话保护的接口收它。

// PreviewTokenTTL 是后台试播令牌的有效期。与前台 TokenTTL 一致：够看完一集短剧，过期后重新点预览即可。
const PreviewTokenTTL = TokenTTL

// ErrPreviewNoLines 表示这一集没有可用的视频线路（docs/06 第 37 节要求提示去配置线路）。
var ErrPreviewNoLines = errs.ErrUnavailable.WithMessage("这一集没有可用的视频线路，请先到「存储 - 线路」为主存储配置并启用视频线路")

// Preview 是一次后台试播的结果。
type Preview struct {
	EpisodeID int64     `json:"episode_id"`
	DramaID   int64     `json:"drama_id"`
	LineID    int64     `json:"line_id"`
	LineName  string    `json:"line_name"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// PreviewService 签发后台试播地址。
type PreviewService struct {
	repo   Repo
	lines  LineSelector
	tokens TokenSigner
	now    func() time.Time
}

func NewPreviewService(repo Repo, lines LineSelector, tokens TokenSigner) *PreviewService {
	return &PreviewService{repo: repo, lines: lines, tokens: tokens, now: time.Now}
}

// Grant 为一集签发试播地址：选一条线路（不按地区，取打分最高的那条），签完整时长的预览令牌。
func (s *PreviewService) Grant(ctx context.Context, episodeID int64) (Preview, error) {
	ep, err := s.repo.PlayableEpisode(ctx, episodeID)
	if err != nil {
		return Preview{}, err
	}
	sel, err := s.lines.ForPlayback(ctx, storage.PlaybackInput{ObjectID: ep.VideoObjectID})
	if err != nil {
		return Preview{}, err
	}
	if len(sel.Lines) == 0 {
		return Preview{}, ErrPreviewNoLines
	}
	line := sel.Lines[0]
	exp := time.Unix(s.now().Add(PreviewTokenTTL).Unix(), 0).UTC()
	// Device 留空（后台没有设备标识，也不需要绑定）、TrialSec 为 0（完整时长）。
	token, err := s.tokens.Sign(playtoken.Claims{EpisodeID: ep.ID, LineID: line.LineID, TrialSec: 0, ExpiresAt: exp})
	if err != nil {
		return Preview{}, err
	}
	return Preview{
		EpisodeID: ep.ID, DramaID: ep.DramaID, LineID: line.LineID, LineName: line.Name,
		URL: PreviewManifestURL(token), ExpiresAt: exp,
	}, nil
}

// PreviewManifestURL 是后台试播的主清单地址（后台会话保护、不加密）。
func PreviewManifestURL(token string) string { return "/api/admin/preview/" + token + "/" + IndexFile }

// PreviewKeyURL 是后台试播清单里内容密钥的地址（与 AdminKeyBase 下的路由对应）。
func PreviewKeyURL(episodeID int64, keyVer int32, token string) string {
	return KeyURL(AdminKeyBase, episodeID, keyVer, token)
}
