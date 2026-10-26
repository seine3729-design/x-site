package me

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	meDom "xsite/service/internal/domain/me"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

type followRequest struct {
	DramaID int64 `json:"drama_id"`
}

// Follow 是 POST /api/v1/me/follow：收藏（追剧），幂等。
func (h *Handlers) Follow(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req followRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	if err := h.Me.Follow(c.Request.Context(), currentUserID(c), req.DramaID); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ListFollows 是 GET /api/v1/me/follows：追剧列表（最近收藏在前，带这个用户看到第几集）。
func (h *Handlers) ListFollows(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	list, err := h.Me.ListFollows(c.Request.Context(), currentUserID(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	if list == nil {
		list = []meDom.FollowView{}
	}
	respond.OK(c, gin.H{"list": list})
}

// Unfollow 是 DELETE /api/v1/me/follow?drama_id=：取消收藏，幂等。
func (h *Handlers) Unfollow(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	dramaID, err := strconv.ParseInt(c.Query("drama_id"), 10, 64)
	if err != nil || dramaID <= 0 {
		respond.Error(c, errs.ErrBadRequest.WithMessage("剧 ID 无效"))
		return
	}
	if err := h.Me.Unfollow(c.Request.Context(), currentUserID(c), dramaID); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type likeRequest struct {
	TargetType string `json:"target_type"`
	TargetID   int64  `json:"target_id"`
}

// Like 是 POST /api/v1/me/like：点赞剧 / 分集，幂等。
func (h *Handlers) Like(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req likeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	if err := h.Me.Like(c.Request.Context(), currentUserID(c), req.TargetType, req.TargetID); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Unlike 是 DELETE /api/v1/me/like?type=drama|episode&id=：取消点赞，幂等。
func (h *Handlers) Unlike(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	targetID, err := strconv.ParseInt(c.Query("id"), 10, 64)
	if err != nil || targetID <= 0 {
		respond.Error(c, errs.ErrBadRequest.WithMessage("点赞对象无效"))
		return
	}
	if err := h.Me.Unlike(c.Request.Context(), currentUserID(c), c.Query("type"), targetID); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
