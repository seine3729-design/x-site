package media

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	playdom "xsite/service/internal/domain/play"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// 后台试播（docs/06 第 37 节）：运营在剧集页直接播一集，确认产物与线路真的通。
//
// 为什么不复用前台那三条路由：前台 /api/v1/* 强制报文加密（HPKE），后台播放器给不出密文；
// 而且前台只播已上架的剧、会按试看秒数裁剪。所以这里另开一组走后台会话、不加密的路由，
// 令牌也另派生一套（playtoken.AdminPreview），两边互不通用。

const (
	contentTypePlaylist = "application/vnd.apple.mpegurl"
	contentTypeKey      = "application/octet-stream"
	// noStore：响应里带签名地址与内容密钥，任何缓存都不能复用。
	noStore = "private, no-store"
)

// PreviewService 签发后台试播地址（domain/play.PreviewService）。
type PreviewService interface {
	Grant(ctx context.Context, episodeID int64) (playdom.Preview, error)
}

// PreviewHLS 返回试播的清单与内容密钥（domain/play.HLSService，装配时用不要求上架的仓储）。
type PreviewHLS interface {
	Playlist(ctx context.Context, in playdom.PlaylistInput) ([]byte, error)
	Key(ctx context.Context, in playdom.KeyInput) ([]byte, error)
}

// Preview 是 POST /api/admin/episodes/:id/preview：签发一个后台可直接播放的清单地址。
func (h *Handlers) Preview(c *gin.Context) {
	if h.PreviewSvc == nil {
		respond.Error(c, errs.ErrUnavailable.WithMessage("试播未启用"))
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(c, errs.ErrNotFound)
		return
	}
	c.Header("Cache-Control", noStore)
	p, err := h.PreviewSvc.Grant(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, p)
}

// PreviewPlaylist 是 GET /api/admin/preview/:token/:file：试播的主清单与各档清单。
func (h *Handlers) PreviewPlaylist(c *gin.Context) {
	if h.PreviewHLS == nil {
		respond.Error(c, errs.ErrUnavailable.WithMessage("试播未启用"))
		return
	}
	c.Header("Cache-Control", noStore)
	body, err := h.PreviewHLS.Playlist(c.Request.Context(), playdom.PlaylistInput{
		Token: c.Param("token"), File: c.Param("file"), KeyBase: playdom.AdminKeyBase,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	c.Data(http.StatusOK, contentTypePlaylist, body)
}

// PreviewKey 是 GET /api/admin/preview/key/:episode_id/:key_ver?t=：试播的 16 字节内容密钥。
// 不限流：调用方是后台会话内的播放器，已经由会话与权限点限制；前台那条路才需要按设备限流。
func (h *Handlers) PreviewKey(c *gin.Context) {
	if h.PreviewHLS == nil {
		respond.Error(c, errs.ErrUnavailable.WithMessage("试播未启用"))
		return
	}
	c.Header("Cache-Control", noStore)
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
	key, err := h.PreviewHLS.Key(c.Request.Context(), playdom.KeyInput{
		Token: c.Query("t"), EpisodeID: episodeID, KeyVer: int32(keyVer),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	c.Data(http.StatusOK, contentTypeKey, key)
}
