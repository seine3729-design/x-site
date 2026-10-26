package community

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// DanmakuService 是弹幕服务（domain/moderation.DanmakuService）。
type DanmakuService interface {
	Send(ctx context.Context, in dom.SendDanmakuInput) (dom.Danmaku, error)
	Window(ctx context.Context, episodeID int64, fromMS, toMS int) ([]dom.DanmakuView, error)
}

type sendDanmakuRequest struct {
	TimeMS  int    `json:"time_ms"`
	Content string `json:"content"`
	Color   string `json:"color"`
}

// ListDanmaku 是 GET /api/v1/episodes/:id/danmaku?from_ms=&to_ms=：按窗口拉取显示中的弹幕。
// 公开读，强缓存 30s（CDN 可缓存，键含窗口参数）。
func (h *Handlers) ListDanmaku(c *gin.Context) {
	epID, ok := pathID(c)
	if !ok {
		return
	}
	fromMS, _ := strconv.Atoi(c.Query("from_ms"))
	toMS, _ := strconv.Atoi(c.Query("to_ms"))
	list, err := h.Danmaku.Window(c.Request.Context(), epID, fromMS, toMS)
	if err != nil {
		respond.Error(c, err)
		return
	}
	c.Header("Cache-Control", "public, max-age=30")
	respond.OK(c, gin.H{"list": list})
}

// SendDanmaku 是 POST /api/v1/episodes/:id/danmaku：发送弹幕（注册用户或游客）。
func (h *Handlers) SendDanmaku(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	epID, ok := pathID(c)
	if !ok {
		return
	}
	author, ok := currentAuthor(c)
	if !ok {
		respond.Error(c, errs.ErrNeedLogin)
		return
	}
	var req sendDanmakuRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	dm, err := h.Danmaku.Send(c.Request.Context(), dom.SendDanmakuInput{
		Author: author, EpisodeID: epID, TimeMS: req.TimeMS, Content: req.Content, Color: req.Color,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, dm)
}
