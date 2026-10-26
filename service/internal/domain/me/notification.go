package me

import (
	"context"
	"encoding/json"
)

// notificationListLimit 是消息列表单页上限（S2-3：列表 + 全部已读；来源 reply/like/update 后续接入）。
const notificationListLimit = 50

// Notification 是一条站内信（docs/02 第 5.9 节）。
type Notification struct {
	ID        int64           `json:"id"`
	Type      string          `json:"type"` // reply / like / update
	Payload   json.RawMessage `json:"payload"`
	IsRead    bool            `json:"is_read"`
	CreatedAt int64           `json:"created_at"` // unix 秒
}

// ListNotifications 返回消息列表（倒序游标分页，before=0 从最新开始）。
func (s *Service) ListNotifications(ctx context.Context, uid, before int64, limit int) ([]Notification, error) {
	if limit <= 0 || limit > notificationListLimit {
		limit = notificationListLimit
	}
	if before < 0 {
		before = 0
	}
	return s.repo.ListNotifications(ctx, uid, before, limit)
}

// MarkAllNotificationsRead 把该用户的未读消息全部标记为已读，返回标记条数。
func (s *Service) MarkAllNotificationsRead(ctx context.Context, uid int64) (int, error) {
	return s.repo.MarkAllNotificationsRead(ctx, uid)
}

// UnreadCount 返回未读消息数（me/state 用）。
func (s *Service) UnreadCount(ctx context.Context, uid int64) (int, error) {
	return s.repo.CountUnreadNotifications(ctx, uid)
}
