// Package play 是播放服务（docs/03 第 4 节）：选源鉴权（S1-7，docs/specs/S1-7.md）、
// 清单改写与内容密钥（S1-8，docs/specs/S1-8.md，hls.go / manifest.go）。
package play

import (
	"context"
	"fmt"
	"time"

	"xsite/service/internal/crypto/playtoken"
	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
)

// TokenTTL 是播放令牌的有效期（docs/10 第 4 节）。客户端在过期前 60 秒重新选源，会员在播放中到期时本令牌内不中断。
const TokenTTL = 10 * time.Minute

// ErrNoLines 表示没有可用的视频线路（都停用、都被地区规则排除，或还没有配置线路）。
var ErrNoLines = errs.ErrUnavailable.WithMessage("暂无可用线路，请稍后重试")

// Viewer 是本次请求的观看者。UserID 为 0 表示游客；DeviceID 是设备标识，播放令牌绑定它的哈希。
type Viewer struct {
	UserID   int64
	DeviceID string
}

// Episode 是一集可播放的分集：剧前台可见，分集已转封装完成。VideoObjectID 是当前产物（选线按它的副本挑存储源），
// ObjectKey 是产物目录（存储源内的相对路径，形如 hls/{集}/{任务}，不带结尾的 /；清单接口据此读清单、拼切片地址）。
type Episode struct {
	ID            int64
	DramaID       int64
	Access        content.Access
	TrialSec      int32
	VideoObjectID int64
	ObjectKey     string
}

// Repo 读取可播放的分集。剧不可见（草稿、下架、定时未到点、回收站）或分集未转封装时返回 errs.ErrNotFound。
type Repo interface {
	PlayableEpisode(ctx context.Context, id int64) (Episode, error)
}

// Membership 查询会员到期时间（S3-10：user_vip.expire_at，读 Valkey 缓存）；不是会员时返回零值。
type Membership interface {
	VIPUntil(ctx context.Context, userID int64) (time.Time, error)
}

type noMembership struct{}

func (noMembership) VIPUntil(context.Context, int64) (time.Time, error) { return time.Time{}, nil }

// NoMembership 是隔离测试或尚未注入会员服务时的安全默认：vip 分集按试看或 402 处理。
// API 进程自 S3-10 起注入 billing.Service，不再使用此实现。
var NoMembership Membership = noMembership{}

// LineSelector 是选源用的选线（domain/storage.LinesService.ForPlayback）。
type LineSelector interface {
	ForPlayback(ctx context.Context, in storage.PlaybackInput) (storage.PlaybackSelection, error)
}

// TokenSigner 签发播放令牌（crypto/playtoken.Keyring）。
type TokenSigner interface {
	Sign(c playtoken.Claims) (string, error)
}

// SourcesInput 是一次选源请求。IP 非空时按 IP 解析地区，否则用 Geo（dev 的 X-Debug-Geo）；LineID 为手选线路，0 表示没有。
type SourcesInput struct {
	EpisodeID int64
	Viewer    Viewer
	IP        string
	Geo       storage.Geo
	LineID    int64
}

// SourceLine 是一条候选线路及其清单地址。
type SourceLine struct {
	LineID      int64     `json:"line_id"`
	Name        string    `json:"name"`
	RegionTag   string    `json:"region_tag"`
	Recommended bool      `json:"recommended"`
	Manual      bool      `json:"manual"`
	URL         string    `json:"url"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Sources 是选源结果。TrialSec 为 0 表示完整播放，大于 0 表示试看秒数；试看结束后前台按 Access 显示登录墙或 VIP 墙。
type Sources struct {
	EpisodeID int64          `json:"episode_id"`
	Region    string         `json:"region"`
	Access    content.Access `json:"access"`
	TrialSec  int32          `json:"trial_sec"`
	Lines     []SourceLine   `json:"lines"`
}

// Service 是选源服务。
type Service struct {
	repo    Repo
	lines   LineSelector
	tokens  TokenSigner
	members Membership
	now     func() time.Time
}

func NewService(repo Repo, lines LineSelector, tokens TokenSigner, members Membership) *Service {
	return &Service{repo: repo, lines: lines, tokens: tokens, members: members, now: time.Now}
}

// Sources 依次判定分集可播放（404）、观看权限（401 / 402）、候选线路（503），再为每条线路签发播放令牌。
func (s *Service) Sources(ctx context.Context, in SourcesInput) (Sources, error) {
	if in.EpisodeID <= 0 {
		return Sources{}, errs.ErrNotFound
	}
	ep, err := s.repo.PlayableEpisode(ctx, in.EpisodeID)
	if err != nil {
		return Sources{}, err
	}
	trial, err := s.grant(ctx, ep, in.Viewer)
	if err != nil {
		return Sources{}, err
	}
	sel, err := s.lines.ForPlayback(ctx, storage.PlaybackInput{
		IP: in.IP, Geo: in.Geo, DeviceID: in.Viewer.DeviceID, Preferred: in.LineID, ObjectID: ep.VideoObjectID,
	})
	if err != nil {
		return Sources{}, err
	}
	if len(sel.Lines) == 0 {
		return Sources{}, ErrNoLines
	}

	// 令牌是秒精度，响应里的过期时间与令牌里的一致
	exp := time.Unix(s.now().Add(TokenTTL).Unix(), 0).UTC()
	device := playtoken.DeviceHash(in.Viewer.DeviceID)
	out := Sources{EpisodeID: ep.ID, Region: sel.Region, Access: ep.Access, TrialSec: trial,
		Lines: make([]SourceLine, 0, len(sel.Lines))}
	for _, l := range sel.Lines {
		tok, err := s.tokens.Sign(playtoken.Claims{
			EpisodeID: ep.ID, LineID: l.LineID, Device: device, TrialSec: trial, ExpiresAt: exp,
		})
		if err != nil {
			return Sources{}, fmt.Errorf("签发播放令牌: %w", err)
		}
		out.Lines = append(out.Lines, SourceLine{
			LineID: l.LineID, Name: l.Name, RegionTag: l.RegionTag, Recommended: l.Recommended, Manual: l.Manual,
			URL: ManifestURL(tok), ExpiresAt: exp,
		})
	}
	return out, nil
}

// grant 判定观看者能看多少：返回试看秒数（0 = 完整播放）。只有 vip 分集且观看者已登录时才查会员（游客一律视为非会员）。
func (s *Service) grant(ctx context.Context, ep Episode, v Viewer) (int32, error) {
	loggedIn := v.UserID > 0
	vip := false
	if ep.Access == content.AccessVIP && loggedIn {
		until, err := s.members.VIPUntil(ctx, v.UserID)
		if err != nil {
			return 0, fmt.Errorf("查询会员状态: %w", err)
		}
		vip = until.After(s.now())
	}
	return decide(ep.Access, ep.TrialSec, loggedIn, vip)
}

// decide 是观看权限规则（docs/03 第 4.1 节）：免费直接看；登录可看的分集游客有试看就试看，否则 401；
// 会员可看的分集非会员有试看就试看，否则 402。返回试看秒数，0 表示完整播放。
func decide(access content.Access, trialSec int32, loggedIn, vip bool) (int32, error) {
	switch access {
	case content.AccessFree:
		return 0, nil
	case content.AccessLogin:
		if loggedIn {
			return 0, nil
		}
		if trialSec > 0 {
			return trialSec, nil
		}
		return 0, errs.ErrNeedLogin
	case content.AccessVIP:
		if vip {
			return 0, nil
		}
		if trialSec > 0 {
			return trialSec, nil
		}
		return 0, errs.ErrNeedVIP
	}
	return 0, fmt.Errorf("分集的观看权限无效: %q", access)
}

// ManifestURL 是播放令牌对应的清单地址：同源加密接口（S1-8），播放器经加密客户端获取。
func ManifestURL(token string) string { return "/api/v1/hls/" + token + "/index.m3u8" }
