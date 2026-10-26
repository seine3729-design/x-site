package me

import (
	"context"
	"time"
)

// followListLimit 是追剧列表返回的最大条数：与续播列表一样一次给全，前台不分页。
const followListLimit = 500

// FollowView 是追剧列表的一项：剧卡片字段（与前台 PublicDramaCard 同名）+ 这个用户的观看进度。
type FollowView struct {
	ID             int64      `json:"id"`
	Title          string     `json:"title"`
	Slug           string     `json:"slug"`
	CoverPortrait  string     `json:"cover_portrait"`
	CoverLandscape string     `json:"cover_landscape"`
	Badge          string     `json:"badge"`
	Score          *float64   `json:"score"`
	Year           *int32     `json:"year"`
	IsFinished     bool       `json:"is_finished"`
	LatestEpisode  int32      `json:"latest_episode"`
	PlayCount      int64      `json:"play_count"`
	PublishAt      *time.Time `json:"publish_at"`
	// WatchedEpisode 是这个用户在该剧看到的集号，0 表示还没看过；前台据此显示「还有 N 集未看」。
	WatchedEpisode int32 `json:"watched_episode"`
	// FollowedAt 是收藏时间，列表按它倒序。
	FollowedAt time.Time `json:"followed_at"`
}

// ListFollows 返回追剧列表（最近收藏在前；已下架 / 回收站中的剧不返回）。
func (s *Service) ListFollows(ctx context.Context, userID int64) ([]FollowView, error) {
	return s.repo.ListFollows(ctx, userID, followListLimit)
}
