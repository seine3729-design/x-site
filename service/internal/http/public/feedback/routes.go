// Package feedback 是反馈社区的前台接口（/api/v1/feedback/*，feedback/docs/08）。
//
// 浏览器只与 H5 前台（Next.js）同源通信，由前台服务端带服务令牌转发到这里，
// 因此会话令牌走 Authorization: Bearer 而不是 Cookie，Go 侧不需要社区用户的 CSRF 机制
// （feedback/docs/adr/0008）。
package feedback

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/feedback"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

const ctxUser = "fb_user"

// IdentityService 是社区身份服务（domain/feedback.IdentityService）。
type IdentityService interface {
	Register(ctx context.Context, in dom.RegisterInput) (dom.AuthResult, error)
	Login(ctx context.Context, in dom.LoginInput) (dom.AuthResult, error)
	Authenticate(ctx context.Context, token string) (dom.User, error)
	Logout(ctx context.Context, token string) error

	ChangePassword(ctx context.Context, userID int64, in dom.ChangePasswordInput) (dom.AuthResult, error)
	UpdateProfile(ctx context.Context, userID int64, in dom.ProfileInput) (dom.User, error)
}

// CommunityService 是前台读写服务（domain/feedback.CommunityService）。
type CommunityService interface {
	Projects(ctx context.Context, v dom.Viewer, page, pageSize int) (dom.Page[dom.ProjectView], error)
	Project(ctx context.Context, v dom.Viewer, slug string) (dom.ProjectView, error)
	MyProjects(ctx context.Context, v dom.Viewer) ([]dom.ProjectView, error)
	Join(ctx context.Context, u dom.User, slug string) (dom.ProjectView, error)
	Feed(ctx context.Context, v dom.Viewer, projectID int64, page, pageSize int) (dom.Page[dom.Post], error)
	Post(ctx context.Context, v dom.Viewer, id int64) (dom.Post, error)
	ListFeedback(ctx context.Context, v dom.Viewer, f dom.FeedbackFilter) (dom.Page[dom.Feedback], error)
	FeedbackDetail(ctx context.Context, v dom.Viewer, slug string, number int64) (dom.Feedback, []dom.Event, error)
	Submit(ctx context.Context, u dom.User, in dom.SubmitInput) (dom.Feedback, error)
	Vote(ctx context.Context, u dom.User, slug string, number int64, active bool) (dom.Feedback, error)
	MyFeedback(ctx context.Context, u dom.User, page, pageSize int) (dom.Page[dom.Feedback], error)
	Leaderboard(ctx context.Context, limit int) ([]dom.LeaderboardEntry, error)
}

// TopicService 是议题区的前台读写服务（domain/feedback.TopicService）。
type TopicService interface {
	Topics(ctx context.Context, v dom.Viewer, f dom.TopicFilter) (dom.Page[dom.Topic], error)
	Topic(ctx context.Context, v dom.Viewer, id int64) (dom.Topic, error)
	Answers(ctx context.Context, v dom.Viewer, topicID int64, page, pageSize int) (dom.Page[dom.Answer], error)
	Reply(ctx context.Context, u dom.User, topicID, parentID int64, body string) (dom.Answer, error)
	SetHelpful(ctx context.Context, u dom.User, answerID int64, active bool) (dom.Answer, error)
}

// Handlers 聚合反馈社区前台 handler。
type Handlers struct {
	Identity  IdentityService
	Community CommunityService
	Topics    TopicService
}

// Routes 注册到 /api/v1 分组上（该分组已挂报文加密中间件）。
func (h *Handlers) Routes(v1 gin.IRouter) {
	g := v1.Group("/feedback")

	// 账号：注册与登录是公开接口，按用户名计数限流在领域层
	g.POST("/auth/register", h.Register)
	g.POST("/auth/login", h.Login)
	// 没有 /auth/recover：恢复码与「忘记密码」已删除（adr/0011）。
	// 唯一的改密入口是 POST /me/password，必须提供当前密码。
	g.POST("/auth/logout", h.Logout)

	// 读接口允许匿名：公开项目、公开动态与公开反馈
	anon := g.Group("", h.optionalUser())
	anon.GET("/projects", h.ListProjects)
	anon.GET("/projects/:slug", h.GetProject)
	anon.GET("/posts", h.ListPosts)
	anon.GET("/posts/:id", h.GetPost)
	anon.GET("/issues", h.ListFeedback)
	anon.GET("/issues/:slug/:number", h.GetFeedback)
	// 议题区：列表与回答允许匿名浏览，回答与「有道理」要登录
	anon.GET("/topics", h.ListTopics)
	anon.GET("/topics/:id", h.GetTopic)
	anon.GET("/topics/:id/answers", h.ListAnswers)

	// 写接口与个人数据必须登录
	auth := g.Group("", h.requireUser())
	auth.GET("/me", h.Me)
	auth.PATCH("/me", h.UpdateProfile)
	auth.POST("/me/password", h.ChangePassword)
	auth.GET("/me/projects", h.MyProjects)
	auth.GET("/me/issues", h.MyFeedback)
	auth.POST("/projects/:slug/join", h.JoinProject)
	auth.POST("/issues", h.Submit)
	auth.PUT("/issues/:slug/:number/vote", h.Vote)
	auth.POST("/topics/:id/answers", h.CreateAnswer)
	auth.PUT("/answers/:id/helpful", h.SetHelpful)
	auth.GET("/leaderboard", h.Leaderboard)
}

// bearerToken 取出 Authorization: Bearer <token> 中的令牌。
func bearerToken(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// optionalUser 在带有效令牌时写入当前用户；令牌无效时按匿名继续，不报错。
func (h *Handlers) optionalUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		if token := bearerToken(c); token != "" {
			if u, err := h.Identity.Authenticate(c.Request.Context(), token); err == nil {
				c.Set(ctxUser, u)
			}
		}
		c.Next()
	}
}

// requireUser 要求有效令牌，否则 401。
func (h *Handlers) requireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		if token == "" {
			respond.Error(c, errs.ErrNeedLogin)
			return
		}
		u, err := h.Identity.Authenticate(c.Request.Context(), token)
		if err != nil {
			respond.Error(c, err)
			return
		}
		c.Set(ctxUser, u)
		c.Next()
	}
}

// currentUser 返回当前登录用户；未登录时第二个返回值为 false。
func currentUser(c *gin.Context) (dom.User, bool) {
	v, ok := c.Get(ctxUser)
	if !ok {
		return dom.User{}, false
	}
	u, ok := v.(dom.User)
	return u, ok
}

// viewerOf 返回当前访问者（未登录时 UserID 为 0）。
func viewerOf(c *gin.Context) dom.Viewer {
	if u, ok := currentUser(c); ok {
		return dom.Viewer{UserID: u.ID}
	}
	return dom.Viewer{}
}
