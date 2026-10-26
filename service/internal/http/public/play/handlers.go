// Package play 是用户端播放接口（docs/03 第 4 节）：选源 GET /api/v1/episodes/:id/sources（S1-7），
// 清单 GET /api/v1/hls/:token/:file 与内容密钥 GET /api/v1/hls/key/:episode_id/:key_ver（S1-8）。
// 注册在 /api/v1 分组上，因此自动走报文加密中间件；handler 只读写明文。
package play

import (
	"context"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/play"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
	"xsite/service/internal/http/respond"
)

const (
	headerDeviceID = "X-Device-Id"
	headerDebugGeo = "X-Debug-Geo" // 仅 dev：模拟地区，形如 CN/广东/电信

	// 选源桶（docs/03 第 1.1 节）：每设备每分钟 60 次，防批量解析播放地址。S0-7 接入通用限流前用固定窗口计数。
	sourcesPerMinute = 60
	// 密钥桶（docs/03 第 4.2 节「有限次」，S1-8）：每设备每集每分钟 20 次。hls.js 同一个密钥地址只取一次，
	// 换档、切线、重新选源也只多取几次，正常播放远低于这个数。
	keyPerMinute = 20
	limitWindow  = 2 * time.Minute // 分钟桶 key 的存活时间（略长于 1 分钟）

	contentTypePlaylist = "application/vnd.apple.mpegurl"
	contentTypeKey      = "application/octet-stream"
	// noStore 用于含签名或令牌的个人化响应：任何缓存都不能复用（加密响应也会被中间件改成 no-store）。
	noStore = "private, no-store"
)

// deviceIDRe 是可接受的设备标识：S1-10 起由服务端签发 UUIDv7，在此之前 APP 自生成。
var deviceIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

var errBadDevice = errs.ErrBadRequest.WithMessage("设备标识格式错误")

// Service 是选源服务（domain/play.Service）。
type Service interface {
	Sources(ctx context.Context, in dom.SourcesInput) (dom.Sources, error)
}

// HLS 是清单与密钥服务（domain/play.HLSService）。
type HLS interface {
	Playlist(ctx context.Context, in dom.PlaylistInput) ([]byte, error)
	Key(ctx context.Context, in dom.KeyInput) ([]byte, error)
}

// Limiter 是按 key 的固定窗口计数器（cache.Counter）。
type Limiter interface {
	Add(ctx context.Context, key string, delta int64, window time.Duration) (int64, error)
}

// Handlers 聚合用户端播放接口。
type Handlers struct {
	Svc Service
	HLS HLS
	// Limiter 是选源桶的计数器，KeyLimiter 是密钥桶的计数器（键前缀不同）。
	Limiter    Limiter
	KeyLimiter Limiter
	Dev        bool
	// Identify 解析观看者。S1-10（游客身份）/ S2-1（登录）接入身份中间件前为 nil：观看者一律是游客，
	// 设备标识取 X-Device-Id（APP 发送；Web 的 BFF 不透传该头，W1-1 接入游客 Cookie 后由身份中间件提供）。
	Identify func(c *gin.Context) (dom.Viewer, error)
}

// Routes 注册用户端播放接口到 /api/v1 分组。
func (h *Handlers) Routes(v1 gin.IRouter) {
	// 公开接口：游客也能选源（免费分集直接看，其余按试看或返回登录墙 / VIP 墙），观看权限在领域层判定。
	v1.GET("/episodes/:id/sources", h.Sources)
	// 清单与密钥只收加密请求（handler 内检查，不受 API_ENCRYPTION_REQUIRED 影响），授权来自路径或查询串里的播放令牌。
	v1.GET("/hls/:token/:file", h.Playlist)
	v1.GET("/hls/key/:episode_id/:key_ver", h.Key)
}

type sourcesQuery struct {
	LineID int64 `form:"line_id"`
}

// Sources 是 GET /api/v1/episodes/:id/sources：观看权限判定 + 候选视频线路与清单地址。
func (h *Handlers) Sources(c *gin.Context) {
	c.Header("Cache-Control", noStore)

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(c, errs.ErrNotFound)
		return
	}
	var q sourcesQuery
	if err := c.ShouldBindQuery(&q); err != nil || q.LineID < 0 {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	viewer, err := h.viewer(c)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if wait := overLimit(c, h.Limiter, limiterSubject(c, viewer.DeviceID), sourcesPerMinute); wait > 0 {
		respond.Error(c, errs.WithRetryAfter(errs.ErrRateLimited, wait))
		return
	}
	ip, geo := h.resolveGeo(c)
	res, err := h.Svc.Sources(c.Request.Context(), dom.SourcesInput{
		EpisodeID: id, Viewer: viewer, IP: ip, Geo: geo, LineID: q.LineID,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, res)
}

// Playlist 是 GET /api/v1/hls/:token/:file：index.m3u8 为主清单，其余为主清单列出的一档（改写过的媒体清单）。
func (h *Handlers) Playlist(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	if !middleware.IsEncrypted(c) {
		respond.Error(c, errs.ErrEncRequired)
		return
	}
	viewer, err := h.viewer(c)
	if err != nil {
		respond.Error(c, err)
		return
	}
	body, err := h.HLS.Playlist(c.Request.Context(), dom.PlaylistInput{
		Token: c.Param("token"), File: c.Param("file"), DeviceID: viewer.DeviceID,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	c.Data(http.StatusOK, contentTypePlaylist, body)
}

// Key 是 GET /api/v1/hls/key/:episode_id/:key_ver?t=：16 字节内容密钥。
func (h *Handlers) Key(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	if !middleware.IsEncrypted(c) {
		respond.Error(c, errs.ErrEncRequired)
		return
	}
	episodeID, err := strconv.ParseInt(c.Param("episode_id"), 10, 64)
	if err != nil || episodeID <= 0 {
		respond.Error(c, errs.ErrNotFound)
		return
	}
	keyVer, err := strconv.ParseInt(c.Param("key_ver"), 10, 32)
	if err != nil || keyVer <= 0 {
		respond.Error(c, errs.ErrNotFound)
		return
	}
	viewer, err := h.viewer(c)
	if err != nil {
		respond.Error(c, err)
		return
	}
	subject := limiterSubject(c, viewer.DeviceID) + ":e" + strconv.FormatInt(episodeID, 10)
	if wait := overLimit(c, h.KeyLimiter, subject, keyPerMinute); wait > 0 {
		respond.Error(c, errs.WithRetryAfter(errs.ErrRateLimited, wait))
		return
	}
	key, err := h.HLS.Key(c.Request.Context(), dom.KeyInput{
		Token: c.Query("t"), EpisodeID: episodeID, KeyVer: int32(keyVer), DeviceID: viewer.DeviceID,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	c.Data(http.StatusOK, contentTypeKey, key)
}

func (h *Handlers) viewer(c *gin.Context) (dom.Viewer, error) {
	if h.Identify != nil {
		return h.Identify(c)
	}
	device := strings.TrimSpace(c.GetHeader(headerDeviceID))
	if device != "" && !deviceIDRe.MatchString(device) {
		return dom.Viewer{}, errBadDevice
	}
	return dom.Viewer{DeviceID: device}, nil
}

// limiterSubject 是限流的计数对象：有设备标识时按设备，没有时按 IP（IPv6 按所在 /64，换地址不能绕过）。
func limiterSubject(c *gin.Context, device string) string {
	if device == "" {
		return "ip:" + limiterIP(c.ClientIP())
	}
	return "d:" + device
}

// overLimit 按自然分钟的固定窗口计数，超出配额时返回到下一分钟的等待时长；
// 计数器故障时放行（docs/03 第 1.1 节：宁可放宽也不全站拒绝）。
func overLimit(c *gin.Context, l Limiter, subject string, limit int64) time.Duration {
	now := time.Now()
	minute := now.Unix() / 60
	n, err := l.Add(c.Request.Context(), subject+":"+strconv.FormatInt(minute, 10), 1, limitWindow)
	if err != nil || n <= limit {
		return 0
	}
	return time.Duration(60-now.Unix()%60) * time.Second
}

// limiterIP 是按 IP 计数的键：IPv4 用完整地址，IPv6 用所在 /64（与后台登录的失败计数一致）。
func limiterIP(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	p, err := addr.Prefix(64)
	if err != nil {
		return addr.String()
	}
	return p.String()
}

// resolveGeo 与 /api/v1/lines 一致：dev 环境优先用 X-Debug-Geo 模拟地区，否则用真实客户端 IP（交 GeoIP 解析）。
func (h *Handlers) resolveGeo(c *gin.Context) (string, storage.Geo) {
	if h.Dev {
		if dbg := strings.TrimSpace(c.GetHeader(headerDebugGeo)); dbg != "" {
			return "", parseDebugGeo(dbg)
		}
	}
	return c.ClientIP(), storage.Geo{}
}

// parseDebugGeo 解析调试地区头 CN/广东/电信（国家/省份/运营商，后两段可省略）。
func parseDebugGeo(s string) storage.Geo {
	parts := strings.Split(s, "/")
	var g storage.Geo
	if len(parts) > 0 {
		g.Country = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		g.Province = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		g.ISP = strings.TrimSpace(parts[2])
	}
	return g
}
