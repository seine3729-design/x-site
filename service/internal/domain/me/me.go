// Package me 是用户端「我的」领域（docs/03 第 2 / 6 节，S2-3）：收藏、点赞、观看历史、通知。
// 只服务注册用户（游客的历史 / 收藏 / 点赞存本机，登录后经 /me/local-merge 并入，S2-10）。
package me

import (
	"context"

	"xsite/service/internal/errs"
)

// historyListLimit 是续播列表返回的最大条数（docs/03 第 6 节：最近观看）。
const historyListLimit = 500

// Repo 读写 me 相关表（follow / like / watch_history / notification，实现见 internal/store/pg）。
type Repo interface {
	AddFollow(ctx context.Context, userID, dramaID int64) error
	RemoveFollow(ctx context.Context, userID, dramaID int64) error
	ListFollows(ctx context.Context, userID int64, limit int) ([]FollowView, error)
	AddLike(ctx context.Context, userID int64, targetType string, targetID int64) error
	RemoveLike(ctx context.Context, userID int64, targetType string, targetID int64) error

	// 观看历史（Valkey 合并写缓冲由 worker 批量落库，见 history.go）。
	HistoryPaused(ctx context.Context, userID int64) (bool, error)
	UpsertHistory(ctx context.Context, rows []HistoryRow) error
	ListHistory(ctx context.Context, userID int64, limit int) ([]HistoryView, error)
	DeleteHistory(ctx context.Context, userID int64, dramaIDs []int64) error
	DeleteAllHistory(ctx context.Context, userID int64) error

	// 站内信（见 notification.go；来源 reply/like/update 后续里程碑接入）。
	ListNotifications(ctx context.Context, userID, before int64, limit int) ([]Notification, error)
	MarkAllNotificationsRead(ctx context.Context, userID int64) (int, error)
	CountUnreadNotifications(ctx context.Context, userID int64) (int, error)
}

// Service 是「我的」领域服务。
type Service struct {
	repo    Repo
	history HistoryStore
}

func NewService(repo Repo, history HistoryStore) *Service {
	return &Service{repo: repo, history: history}
}

func validLikeTarget(t string) bool { return t == "drama" || t == "episode" }

// Follow 收藏一部剧（幂等；剧不存在时外键失败映射为 400）。
func (s *Service) Follow(ctx context.Context, userID, dramaID int64) error {
	if dramaID <= 0 {
		return errs.ErrBadRequest.WithMessage("剧 ID 无效")
	}
	return s.repo.AddFollow(ctx, userID, dramaID)
}

// Unfollow 取消收藏（幂等：未收藏也返回成功）。
func (s *Service) Unfollow(ctx context.Context, userID, dramaID int64) error {
	if dramaID <= 0 {
		return errs.ErrBadRequest.WithMessage("剧 ID 无效")
	}
	return s.repo.RemoveFollow(ctx, userID, dramaID)
}

// Like 点赞剧 / 分集（幂等）。
func (s *Service) Like(ctx context.Context, userID int64, targetType string, targetID int64) error {
	if !validLikeTarget(targetType) || targetID <= 0 {
		return errs.ErrBadRequest.WithMessage("点赞对象无效")
	}
	return s.repo.AddLike(ctx, userID, targetType, targetID)
}

// Unlike 取消点赞（幂等）。
func (s *Service) Unlike(ctx context.Context, userID int64, targetType string, targetID int64) error {
	if !validLikeTarget(targetType) || targetID <= 0 {
		return errs.ErrBadRequest.WithMessage("点赞对象无效")
	}
	return s.repo.RemoveLike(ctx, userID, targetType, targetID)
}
