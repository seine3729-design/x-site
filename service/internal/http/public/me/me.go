// Package me 是「我的」接口（docs/03 第 2 节，S2-1）：改密、我的设备（会话）列表与下线。
// 整组注册在 /api/v1/me 下，经身份中间件要求已登录的注册用户（role=user）；游客与未登录一律 401。
package me

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"uuid"

	"xsite/service/internal/domain/identity"
	meDom "xsite/service/internal/domain/me"
	"xsite/service/internal/domain/user"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
	"xsite/service/internal/http/respond"
)

const noStore = "private, no-store"

// Accounts 是改密服务（domain/user.Service）。
type Accounts interface {
	ChangePassword(ctx context.Context, userID int64, in user.ChangePasswordInput) error
}

// Sessions 是会话管理服务（domain/identity.Service）。
type Sessions interface {
	ListSessions(ctx context.Context, userID int64) ([]identity.SessionInfo, error)
	RevokeSession(ctx context.Context, userID int64, sessionID uuid.UUID) error
}

// Me 是「我的」领域服务（domain/me.Service，S2-3）：收藏、点赞、观看历史、站内信。
type Me interface {
	Follow(ctx context.Context, userID, dramaID int64) error
	Unfollow(ctx context.Context, userID, dramaID int64) error
	ListFollows(ctx context.Context, userID int64) ([]meDom.FollowView, error)
	Like(ctx context.Context, userID int64, targetType string, targetID int64) error
	Unlike(ctx context.Context, userID int64, targetType string, targetID int64) error
	ReportHistory(ctx context.Context, userID int64, entries []meDom.HistoryEntry) error
	ListHistory(ctx context.Context, userID int64) ([]meDom.HistoryView, error)
	DeleteHistory(ctx context.Context, userID int64, dramaIDs []int64) error
	ClearHistory(ctx context.Context, userID int64) error
	ListNotifications(ctx context.Context, userID, before int64, limit int) ([]meDom.Notification, error)
	MarkAllNotificationsRead(ctx context.Context, userID int64) (int, error)
	UnreadCount(ctx context.Context, userID int64) (int, error)
}

// Profiles 是资料服务（domain/user.Service，S2-3）。
type Profiles interface {
	GetProfile(ctx context.Context, userID int64) (user.Profile, error)
	UpdateProfile(ctx context.Context, userID int64, upd user.ProfileUpdate) (user.Profile, error)
}

// Handlers 聚合「我的」接口。
type Handlers struct {
	Accounts Accounts
	Sessions Sessions
	Me       Me
	Profiles Profiles
	Resolver middleware.AccessResolver
}

// Routes 注册 /api/v1/me/* 到 v1 分组；整组要求已登录的注册用户。
func (h *Handlers) Routes(v1 gin.IRouter) {
	g := v1.Group("/me", middleware.SetIdentity(h.Resolver), middleware.RequireUser())
	g.GET("/state", h.State)
	g.GET("/profile", h.GetProfile)
	g.PATCH("/profile", h.UpdateProfile)
	g.POST("/password", h.ChangePassword)
	g.GET("/sessions", h.ListSessions)
	g.DELETE("/sessions/:id", h.RevokeSession)
	g.GET("/history", h.ListHistory)
	g.POST("/history", h.ReportHistory)
	g.DELETE("/history", h.DeleteHistory)
	g.POST("/follow", h.Follow)
	g.DELETE("/follow", h.Unfollow)
	g.GET("/follows", h.ListFollows)
	g.POST("/like", h.Like)
	g.DELETE("/like", h.Unlike)
	g.GET("/notifications", h.ListNotifications)
	g.POST("/notifications", h.MarkNotificationsRead)
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// ChangePassword 是 POST /api/v1/me/password：用当前密码改密，成功后吊销全部会话（含当前），客户端需重新登录。
func (h *Handlers) ChangePassword(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	if err := h.Accounts.ChangePassword(c.Request.Context(), currentUserID(c), user.ChangePasswordInput{
		OldPassword: req.OldPassword, NewPassword: req.NewPassword,
	}); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type sessionItem struct {
	ID         string `json:"id"`
	Platform   string `json:"platform"`
	UADigest   string `json:"ua_digest,omitempty"`
	IPPrefix   string `json:"ip_prefix,omitempty"`
	Current    bool   `json:"current"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
}

// ListSessions 是 GET /api/v1/me/sessions：我的设备（会话）列表，标出当前会话。
func (h *Handlers) ListSessions(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	list, err := h.Sessions.ListSessions(c.Request.Context(), currentUserID(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	cur, _ := middleware.CurrentAccess(c)
	out := make([]sessionItem, len(list))
	for i, s := range list {
		id := s.ID.String()
		out[i] = sessionItem{
			ID: id, Platform: s.Platform, UADigest: s.UADigest, IPPrefix: s.IPPrefix,
			Current:    id == cur.SessionID,
			CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
			LastSeenAt: s.LastSeenAt.UTC().Format(time.RFC3339),
		}
	}
	respond.OK(c, gin.H{"sessions": out})
}

// RevokeSession 是 DELETE /api/v1/me/sessions/:id：下线指定设备（会话）；不属于本人返回 404。
func (h *Handlers) RevokeSession(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	sid, err := uuid.Parse(c.Param("id"))
	if err != nil {
		respond.Error(c, errs.ErrNotFound)
		return
	}
	if err := h.Sessions.RevokeSession(c.Request.Context(), currentUserID(c), sid); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func currentUserID(c *gin.Context) int64 {
	a, _ := middleware.CurrentAccess(c)
	return a.UserID()
}
