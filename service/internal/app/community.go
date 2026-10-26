package app

import (
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/moderation"
	"xsite/service/internal/store/cache"
	"xsite/service/internal/store/pg"
)

// newCommentService 装配评论服务（S2-5）：屏蔽词判定复用屏蔽词 Service，功能开关读 site_config.features，
// 禁言读 user.status，先审后显读 site_config.comment_policy（policy），防刷用 Valkey 去重（同作者相同内容 10 秒），
// 通知经 River 入队异步写站内信（notifier）。
func newCommentService(pool *pgxpool.Pool, vk valkey.Client, eval moderation.Evaluator, features moderation.Features, policy moderation.Policy, notifier moderation.Notifier) *moderation.CommentService {
	return moderation.NewCommentService(
		pg.NewCommentRepo(pool), eval, features, policy,
		pg.NewUserRepo(pool), cache.NewDedup(vk, "xsite:comment:dedup:", 10*time.Second), notifier,
	)
}

// NewCommunityNotifier 返回 worker 侧的社区通知落库实现（写 notification 表）。
func NewCommunityNotifier(pool *pgxpool.Pool) *pg.MeRepo {
	return pg.NewMeRepo(pool)
}

// newModerationService 装配评论 / 弹幕审核服务（S2-5，§24）：hit_words 扫描复用屏蔽词 Service。
func newModerationService(pool *pgxpool.Pool, eval moderation.Evaluator) *moderation.ModerationService {
	return moderation.NewModerationService(pg.NewModerationRepo(pool), eval)
}

// newPolicyService 装配评论先审后显策略（S2-5，§24）：后台读写 + 供评论服务判断。
func newPolicyService(pool *pgxpool.Pool) *moderation.PolicyService {
	return moderation.NewPolicyService(pg.NewCommentPolicyRepo(pool))
}

// newDanmakuService 装配弹幕服务（S2-5）：复用屏蔽词判定与功能开关 / 禁言依赖；弹幕不做重复内容去重。
func newDanmakuService(pool *pgxpool.Pool, eval moderation.Evaluator, features moderation.Features) *moderation.DanmakuService {
	return moderation.NewDanmakuService(pg.NewDanmakuRepo(pool), eval, features, pg.NewUserRepo(pool))
}

// newReportService 装配举报服务（S2-5）：同一举报人对同一对象 24 小时内只计一次（Valkey 去重）。
func newReportService(pool *pgxpool.Pool, vk valkey.Client) *moderation.ReportService {
	return moderation.NewReportService(pg.NewReportRepo(pool), cache.NewDedup(vk, "xsite:report:dedup:", 24*time.Hour))
}

// newEmojiService 装配表情服务（S2-5）：前台下发表情面板、后台维护表情包 / 表情。
func newEmojiService(pool *pgxpool.Pool) *moderation.EmojiService {
	return moderation.NewEmojiService(pg.NewEmojiRepo(pool))
}
