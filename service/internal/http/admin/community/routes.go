// Package community 是后台「社区」目录的接口（docs/06 第 24–27 节）：
// 评论 / 弹幕审核、举报队列、用户管理、屏蔽词、表情。
// 会话、CSRF、审计中间件由 admin.Routes 返回的分组提供，这里按页面声明权限点。
package community

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/moderation"
	usr "xsite/service/internal/domain/user"
	"xsite/service/internal/http/admin"
)

// BlockwordService 是屏蔽词管理（domain/moderation.Service）。
type BlockwordService interface {
	List(ctx context.Context, f dom.BlockwordFilter) (dom.Page[dom.Blockword], error)
	Groups(ctx context.Context) ([]string, error)
	Create(ctx context.Context, word, group, action string) (dom.Blockword, error)
	Delete(ctx context.Context, id int64) (dom.Blockword, error)
}

// ReportService 是举报队列（domain/moderation.ReportService 的后台部分）。
type ReportService interface {
	List(ctx context.Context, f dom.ReportFilter) (dom.Page[dom.ReportView], error)
	Handle(ctx context.Context, id int64, status, note string) (dom.ReportView, error)
}

// ModerationService 是评论 / 弹幕审核（domain/moderation.ModerationService）。
type ModerationService interface {
	List(ctx context.Context, kind string, f dom.ModerationFilter) (dom.Page[dom.ModerationItem], error)
	Act(ctx context.Context, kind, action string, ids []int64) (int64, error)
}

// PolicyService 是评论先审后显开关（domain/moderation.PolicyService）。
type PolicyService interface {
	Get(ctx context.Context) (dom.CommentPolicy, error)
	Set(ctx context.Context, p dom.CommentPolicy) (dom.CommentPolicy, error)
}

// UserAdminService 是用户管理（domain/user.AdminService）。
type UserAdminService interface {
	ListUsers(ctx context.Context, f usr.AdminFilter) (usr.Page[usr.AdminUser], error)
	ListGuests(ctx context.Context, f usr.AdminFilter) (usr.Page[usr.Guest], error)
	GetUser(ctx context.Context, id int64) (usr.AdminUserDetail, error)
	SetStatus(ctx context.Context, id int64, status string) (usr.AdminUser, error)
}

// EmojiService 是表情包 / 表情管理（domain/moderation.EmojiService）。
type EmojiService interface {
	List(ctx context.Context) ([]dom.PackWithEmojis, error)
	CreatePack(ctx context.Context, name string, sort int) (dom.EmojiPack, error)
	UpdatePack(ctx context.Context, id int64, name string, sort int) (dom.EmojiPack, error)
	DeletePack(ctx context.Context, id int64) (dom.EmojiPack, error)
	CreateEmoji(ctx context.Context, packID int64, code, image string, sort int) (dom.Emoji, error)
	UpdateEmoji(ctx context.Context, id int64, code, image string, sort int) (dom.Emoji, error)
	DeleteEmoji(ctx context.Context, id int64) (dom.Emoji, error)
}

// Handlers 聚合社区目录的 handler。
type Handlers struct {
	Blockwords BlockwordService
	Reports    ReportService
	Moderation ModerationService
	Policy     PolicyService
	Emojis     EmojiService
	Users      UserAdminService
}

// Routes 把社区接口注册到 admin.Routes 返回的分组上。读接口要 <页面>.view，写接口要 <页面>.edit。
func (h *Handlers) Routes(authed gin.IRouter) {
	bw := authed.Group("/blockwords", admin.RequirePage("blockwords"))
	bw.GET("", h.ListBlockwords)
	bw.GET("/groups", h.ListBlockwordGroups)
	bw.POST("", h.CreateBlockword)
	bw.DELETE("/:id", h.DeleteBlockword)

	rp := authed.Group("/reports", admin.RequirePage("reports"))
	rp.GET("", h.ListReports)
	rp.POST("/:id/handle", h.HandleReport)

	mod := authed.Group("/moderation", admin.RequirePage("moderation"))
	mod.GET("", h.ListModeration)
	mod.POST("/actions", h.ActModeration)
	cp := authed.Group("/comment-policy", admin.RequirePage("moderation"))
	cp.GET("", h.GetCommentPolicy)
	cp.PUT("", h.SetCommentPolicy)

	us := authed.Group("/users", admin.RequirePage("users"))
	us.GET("", h.ListUsers)
	us.GET("/:id", h.GetUser)
	us.POST("/:id/status", h.SetUserStatus)
	// 游客设备是用户管理页的另一个类型，但形状与注册用户差太多，单列一个路径、共用 users 权限点。
	gs := authed.Group("/guests", admin.RequirePage("users"))
	gs.GET("", h.ListGuests)

	em := authed.Group("/emojis", admin.RequirePage("emojis"))
	em.GET("", h.ListEmojis)
	em.POST("/packs", h.CreatePack)
	em.PATCH("/packs/:id", h.UpdatePack)
	em.DELETE("/packs/:id", h.DeletePack)
	em.POST("/packs/:id/emojis", h.CreateEmojiInPack)
	em.PATCH("/items/:id", h.UpdateEmoji)
	em.DELETE("/items/:id", h.DeleteEmoji)
}
