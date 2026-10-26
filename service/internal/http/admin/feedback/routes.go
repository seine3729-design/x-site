// Package feedback 是运营后台的反馈社区接口（/api/admin/feedback/*，feedback/docs/07）。
// 会话、Origin、CSRF 与审计中间件由 admin.Routes 返回的分组提供，这里按页面声明权限点。
package feedback

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/feedback"
	"xsite/service/internal/http/admin"
)

// Service 是反馈社区的后台管理服务（domain/feedback.ManageService）。
type Service interface {
	ListProjects(ctx context.Context, f dom.ProjectFilter) (dom.Page[dom.Project], error)
	CreateProject(ctx context.Context, in dom.ProjectInput) (dom.Project, error)
	UpdateProject(ctx context.Context, id int64, in dom.ProjectInput) (before, after dom.Project, err error)
	DeleteProject(ctx context.Context, id int64) (dom.Project, error)

	ListMembers(ctx context.Context, projectID int64, page, pageSize int) (dom.Page[dom.Member], error)
	SetMemberRole(ctx context.Context, projectID, userID int64, role dom.MemberRole, remove bool) (dom.Member, error)

	ListInvites(ctx context.Context, projectID int64, page, pageSize int) (dom.Page[dom.Invite], error)
	CreateInvite(ctx context.Context, in dom.InviteInput) (dom.Invite, string, error)
	RevokeInvite(ctx context.Context, id int64) (dom.Invite, error)

	ListPosts(ctx context.Context, f dom.PostFilter) (dom.Page[dom.Post], error)
	CreatePost(ctx context.Context, in dom.PostInput) (dom.Post, error)
	UpdatePost(ctx context.Context, id int64, in dom.PostInput) (before, after dom.Post, err error)
	DeletePost(ctx context.Context, id int64) (dom.Post, error)

	ListFeedback(ctx context.Context, f dom.FeedbackFilter) (dom.Page[dom.Feedback], error)
	FeedbackDetail(ctx context.Context, id int64) (dom.Feedback, []dom.Event, error)
	Triage(ctx context.Context, id int64, in dom.TriageInput) (before, after dom.Feedback, err error)
	Overview(ctx context.Context, projectID int64) ([]dom.StatusCount, error)

	ListUsers(ctx context.Context, f dom.UserFilter) (dom.Page[dom.User], error)
	SetUserStatus(ctx context.Context, id int64, status dom.UserStatus) (before, after dom.User, err error)
}

// TopicService 是议题区的后台管理服务（domain/feedback.TopicService）。
type TopicService interface {
	AdminTopics(ctx context.Context, f dom.TopicFilter) (dom.Page[dom.Topic], error)
	CreateTopic(ctx context.Context, in dom.TopicInput) (dom.Topic, error)
	UpdateTopic(ctx context.Context, id int64, in dom.TopicInput) (before, after dom.Topic, err error)
	DeleteTopic(ctx context.Context, id int64) (dom.Topic, error)
	TopicByID(ctx context.Context, id int64) (dom.Topic, error)
	AdminAnswers(ctx context.Context, topicID int64, page, pageSize int) (dom.Page[dom.Answer], error)
	SetFeatured(ctx context.Context, id int64, featured bool) (before, after dom.Answer, err error)
	SetAnswerStatus(ctx context.Context, id int64, status string) (before, after dom.Answer, err error)
}

// KeyService 是开放提交接口的密钥管理服务（domain/feedback.IntakeService）。
type KeyService interface {
	ListAPIKeys(ctx context.Context, projectID int64, page, pageSize int) (dom.Page[dom.APIKey], error)
	CreateAPIKey(ctx context.Context, in dom.APIKeyInput) (dom.APIKey, string, error)
	UpdateAPIKey(ctx context.Context, id int64, in dom.APIKeyInput) (before, after dom.APIKey, err error)
	RevokeAPIKey(ctx context.Context, id int64) (dom.APIKey, error)
}

// Handlers 聚合反馈社区的后台 handler。
type Handlers struct {
	Svc    Service
	Topics TopicService
	Keys   KeyService
}

// Routes 把反馈社区的后台接口注册到 admin.Routes 返回的分组上。
func (h *Handlers) Routes(authed gin.IRouter) {
	// 内测项目：只有平台管理员能建项目（权限点 fb_projects.edit），成员管理挂在同一页面下
	projects := admin.RequirePage("fb_projects")
	pg := authed.Group("/feedback/projects")
	pg.GET("", projects, h.ListProjects)
	pg.POST("", projects, h.CreateProject)
	pg.PATCH("/:id", projects, h.UpdateProject)
	pg.DELETE("/:id", projects, h.DeleteProject)
	pg.GET("/:id/members", projects, h.ListMembers)
	pg.PATCH("/:id/members/:user_id", projects, h.SetMemberRole)

	// 反馈分诊
	issues := admin.RequirePage("fb_issues")
	ig := authed.Group("/feedback/issues")
	ig.GET("", issues, h.ListFeedback)
	ig.GET("/overview", admin.RequirePermission("fb_issues.view"), h.Overview)
	ig.GET("/:id", issues, h.GetFeedback)
	ig.POST("/:id/triage", issues, h.Triage)

	// 项目动态
	posts := admin.RequirePage("fb_posts")
	og := authed.Group("/feedback/posts")
	og.GET("", posts, h.ListPosts)
	og.POST("", posts, h.CreatePost)
	og.PATCH("/:id", posts, h.UpdatePost)
	og.DELETE("/:id", posts, h.DeletePost)

	// 邀请码：明文只在创建响应里出现一次
	invites := admin.RequirePage("fb_invites")
	vg := authed.Group("/feedback/invites")
	vg.GET("", invites, h.ListInvites)
	vg.POST("", invites, h.CreateInvite)
	vg.POST("/:id/revoke", invites, h.RevokeInvite)

	// 社区成员：后台只能停用/恢复账号，不能重设用户密码（feedback/docs/adr/0007）
	users := admin.RequirePage("fb_users")
	ug := authed.Group("/feedback/users")
	ug.GET("", users, h.ListUsers)
	ug.POST("/:id/status", users, h.SetUserStatus)

	// 议题区：议题本体与其下回答的管理（精选、隐藏）共用同一个页面权限点
	topics := admin.RequirePage("fb_topics")
	tg := authed.Group("/feedback/topics")
	tg.GET("", topics, h.ListTopics)
	tg.POST("", topics, h.CreateTopic)
	tg.GET("/:id", topics, h.GetTopic)
	tg.PATCH("/:id", topics, h.UpdateTopic)
	tg.DELETE("/:id", topics, h.DeleteTopic)
	tg.GET("/:id/answers", topics, h.ListAnswers)
	ag := authed.Group("/feedback/answers")
	ag.POST("/:id/featured", topics, h.SetAnswerFeatured)
	ag.POST("/:id/status", topics, h.SetAnswerStatus)

	// 开放提交接口的密钥：明文只在创建响应里出现一次
	keys := admin.RequirePage("fb_api_keys")
	kg := authed.Group("/feedback/api-keys")
	kg.GET("", keys, h.ListAPIKeys)
	kg.POST("", keys, h.CreateAPIKey)
	kg.PATCH("/:id", keys, h.UpdateAPIKey)
	kg.POST("/:id/revoke", keys, h.RevokeAPIKey)
}
