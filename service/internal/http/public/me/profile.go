package me

import (
	"github.com/gin-gonic/gin"

	"xsite/service/internal/domain/user"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// GetProfile 是 GET /api/v1/me/profile：读取可编辑资料（头像、昵称、简介及审核状态）。
func (h *Handlers) GetProfile(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	p, err := h.Profiles.GetProfile(c.Request.Context(), currentUserID(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, p)
}

type profilePatchRequest struct {
	Nickname *string `json:"nickname"`
	Avatar   *string `json:"avatar"`
	Bio      *string `json:"bio"`
}

// UpdateProfile 是 PATCH /api/v1/me/profile：改头像 / 昵称 / 简介（只改传入字段）。
func (h *Handlers) UpdateProfile(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req profilePatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	p, err := h.Profiles.UpdateProfile(c.Request.Context(), currentUserID(c), user.ProfileUpdate{
		Nickname: req.Nickname, Avatar: req.Avatar, Bio: req.Bio,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, p)
}

// State 是 GET /api/v1/me/state：登录态 + 身份 + 未读数（SPA 加载时的轻量轮询端点）。
// 走 RequireUser：游客 / 未登录一律 401，客户端据此判定为未登录。
func (h *Handlers) State(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	ctx := c.Request.Context()
	uid := currentUserID(c)
	p, err := h.Profiles.GetProfile(ctx, uid)
	if err != nil {
		respond.Error(c, err)
		return
	}
	unread, err := h.Me.UnreadCount(ctx, uid)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{
		"user":   gin.H{"id": uid, "nickname": p.Nickname, "avatar": p.Avatar},
		"unread": unread,
	})
}
