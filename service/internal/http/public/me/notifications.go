package me

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	meDom "xsite/service/internal/domain/me"
	"xsite/service/internal/http/respond"
)

// ListNotifications 是 GET /api/v1/me/notifications：消息列表（倒序游标分页，?before=&limit=）。
func (h *Handlers) ListNotifications(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	before, _ := strconv.ParseInt(c.Query("before"), 10, 64)
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := h.Me.ListNotifications(c.Request.Context(), currentUserID(c), before, limit)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if items == nil {
		items = []meDom.Notification{}
	}
	respond.OK(c, gin.H{"items": items})
}

// MarkNotificationsRead 是 POST /api/v1/me/notifications：把未读消息全部标记为已读。
func (h *Handlers) MarkNotificationsRead(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	if _, err := h.Me.MarkAllNotificationsRead(c.Request.Context(), currentUserID(c)); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
