// Package community 是前台社区接口（S2-5，docs/03 第 5 节）：评论、弹幕、举报、表情。
// 注册在 /api/v1 分组上（走报文加密）；评论 / 弹幕支持注册用户与游客，身份由 SetIdentity 解析。
package community

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"uuid"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
	"xsite/service/internal/http/respond"
)

const noStore = "private, no-store"

// CommentService 是评论服务（domain/moderation.CommentService）。
type CommentService interface {
	Create(ctx context.Context, in dom.CreateCommentInput) (dom.Comment, error)
	List(ctx context.Context, episodeID int64, sort, cursor string, limit int) (dom.CommentPage, error)
	Delete(ctx context.Context, author dom.Author, id int64) error
	Like(ctx context.Context, userID, commentID int64) error
}

// Handlers 聚合前台社区接口。
type Handlers struct {
	Comments CommentService
	Danmaku  DanmakuService
	Reports  ReportService
	Emojis   EmojiService
	Resolver middleware.AccessResolver
}

// Routes 注册前台社区接口到 v1 分组。整组经 SetIdentity 解析身份（匿名放行，写接口在 handler 内要求身份）。
func (h *Handlers) Routes(v1 gin.IRouter) {
	g := v1.Group("", middleware.SetIdentity(h.Resolver))
	g.GET("/episodes/:id/comments", h.ListComments)
	g.POST("/episodes/:id/comments", h.CreateComment)
	g.DELETE("/comments/:id", h.DeleteComment)
	g.POST("/comments/:id/like", h.LikeComment)
	g.GET("/episodes/:id/danmaku", h.ListDanmaku)
	g.POST("/episodes/:id/danmaku", h.SendDanmaku)
	g.POST("/reports", h.SubmitReport)
	g.GET("/emojis", h.ListEmojis)
}

type createCommentRequest struct {
	Content  string `json:"content"`
	ParentID int64  `json:"parent_id"`
}

// ListComments 是 GET /api/v1/episodes/:id/comments：评论列表（游标分页，sort=latest|hot）。公开读。
func (h *Handlers) ListComments(c *gin.Context) {
	epID, ok := pathID(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	page, err := h.Comments.List(c.Request.Context(), epID, c.Query("sort"), c.Query("cursor"), limit)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, page)
}

// CreateComment 是 POST /api/v1/episodes/:id/comments：发表评论（注册用户或游客）。
func (h *Handlers) CreateComment(c *gin.Context) {
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
	var req createCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	cm, err := h.Comments.Create(c.Request.Context(), dom.CreateCommentInput{
		Author: author, EpisodeID: epID, ParentID: req.ParentID, Content: req.Content,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, cm)
}

// DeleteComment 是 DELETE /api/v1/comments/:id：删除本人评论。
func (h *Handlers) DeleteComment(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	id, ok := pathID(c)
	if !ok {
		return
	}
	author, ok := currentAuthor(c)
	if !ok {
		respond.Error(c, errs.ErrNeedLogin)
		return
	}
	if err := h.Comments.Delete(c.Request.Context(), author, id); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// LikeComment 是 POST /api/v1/comments/:id/like：点赞评论（仅注册用户）。
func (h *Handlers) LikeComment(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	id, ok := pathID(c)
	if !ok {
		return
	}
	a, ok := middleware.CurrentAccess(c)
	if !ok || !a.IsUser() {
		respond.Error(c, errs.ErrNeedLogin)
		return
	}
	if err := h.Comments.Like(c.Request.Context(), a.UserID(), id); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// currentAuthor 把当前身份转成评论作者（注册用户或游客）；匿名（无令牌）返回 false。
func currentAuthor(c *gin.Context) (dom.Author, bool) {
	a, ok := middleware.CurrentAccess(c)
	if !ok {
		return dom.Author{}, false
	}
	if a.IsUser() {
		return dom.Author{UserID: a.UserID()}, true
	}
	gid, err := uuid.Parse(a.Subject)
	if err != nil {
		return dom.Author{}, false
	}
	return dom.Author{GuestID: gid}, true
}

// pathID 解析路径上的正整数编号；非正整数按「内容不存在」处理。
func pathID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		respond.Error(c, errs.ErrNotFound)
		return 0, false
	}
	return id, true
}
