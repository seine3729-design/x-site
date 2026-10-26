package feedback

import (
	"strconv"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/feedback"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// ---------- 请求 ----------

type registerReq struct {
	Handle     string `json:"handle"`
	Password   string `json:"password"`
	InviteCode string `json:"invite_code"`
	Nickname   string `json:"nickname"`
}

type loginReq struct {
	Handle   string `json:"handle"`
	Password string `json:"password"`
}

type passwordReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

type profileReq struct {
	Nickname  string `json:"nickname"`
	Bio       string `json:"bio"`
	ShowLevel bool   `json:"show_level"`
}

type submitReq struct {
	ProjectSlug string `json:"project_slug"`
	Kind        string `json:"kind"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	Env         string `json:"env"`
	PageURL     string `json:"page_url"`
	Visibility  string `json:"visibility"`
}

type voteReq struct {
	Active bool `json:"active"`
}

// answerReq 是发布一条回答；parent_id 为 0 表示顶层回答。
type answerReq struct {
	ParentID int64  `json:"parent_id"`
	Body     string `json:"body"`
}

// helpfulReq 是设置「有道理」的目标状态，重复提交结果一致。
type helpfulReq struct {
	Active bool `json:"active"`
}

type listQuery struct {
	ProjectID int64  `form:"project_id"`
	Status    string `form:"status"`
	Kind      string `form:"kind"`
	Q         string `form:"q"`
	Sort      string `form:"sort"`
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
}

// ---------- 响应 ----------

type pageDTO[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

func pageOf[T any](p dom.Page[T]) pageDTO[T] {
	list := p.List
	if list == nil {
		list = []T{}
	}
	return pageDTO[T]{List: list, Total: p.Total, Page: p.Page, PageSize: p.PageSize}
}

// feedbackDetailDTO 是反馈详情：正文 + 公开的状态历史。
type feedbackDetailDTO struct {
	Feedback dom.Feedback `json:"feedback"`
	Events   []dom.Event  `json:"events"`
}

// bind 解析 JSON 请求体，失败时已写出 400。
func bind(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求内容格式错误"))
		return false
	}
	return true
}

func bindQuery(c *gin.Context, dst any) bool {
	if err := c.ShouldBindQuery(dst); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return false
	}
	return true
}

// pathNumber 解析反馈编号。
func pathNumber(c *gin.Context) (int64, bool) {
	n, err := strconv.ParseInt(c.Param("number"), 10, 64)
	if err != nil || n <= 0 {
		respond.Error(c, errs.ErrNotFound)
		return 0, false
	}
	return n, true
}

// pathID 解析路径上的 :id；非法 ID 直接按 404 处理，不泄露参数细节。
func pathID(c *gin.Context) (int64, bool) {
	n, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || n <= 0 {
		respond.Error(c, errs.ErrNotFound)
		return 0, false
	}
	return n, true
}

// ---------- 账号 ----------

// Register 是 POST /api/v1/feedback/auth/register：用邀请码注册，成功即登录。
func (h *Handlers) Register(c *gin.Context) {
	var req registerReq
	if !bind(c, &req) {
		return
	}
	res, err := h.Identity.Register(c.Request.Context(), dom.RegisterInput{
		Handle: req.Handle, Password: req.Password, InviteCode: req.InviteCode,
		Nickname: req.Nickname,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, res)
}

// Login 是 POST /api/v1/feedback/auth/login。
func (h *Handlers) Login(c *gin.Context) {
	var req loginReq
	if !bind(c, &req) {
		return
	}
	res, err := h.Identity.Login(c.Request.Context(), dom.LoginInput{Handle: req.Handle, Password: req.Password})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, res)
}

// Logout 是 POST /api/v1/feedback/auth/logout：没有令牌时也返回成功（幂等）。
func (h *Handlers) Logout(c *gin.Context) {
	if err := h.Identity.Logout(c.Request.Context(), bearerToken(c)); err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, nil)
}

// Me 是 GET /api/v1/feedback/me。
func (h *Handlers) Me(c *gin.Context) {
	u, _ := currentUser(c)
	respond.OK(c, u)
}

// UpdateProfile 是 PATCH /api/v1/feedback/me。
func (h *Handlers) UpdateProfile(c *gin.Context) {
	u, _ := currentUser(c)
	var req profileReq
	if !bind(c, &req) {
		return
	}
	out, err := h.Identity.UpdateProfile(c.Request.Context(), u.ID, dom.ProfileInput{
		Nickname: req.Nickname, Bio: req.Bio, ShowLevel: req.ShowLevel,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

// ChangePassword 是 POST /api/v1/feedback/me/password：成功后旧令牌失效，响应里返回新令牌与新恢复码。
func (h *Handlers) ChangePassword(c *gin.Context) {
	u, _ := currentUser(c)
	var req passwordReq
	if !bind(c, &req) {
		return
	}
	res, err := h.Identity.ChangePassword(c.Request.Context(), u.ID, dom.ChangePasswordInput{
		OldPassword: req.OldPassword, NewPassword: req.NewPassword,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, res)
}

// ---------- 项目 ----------

// ListProjects 是 GET /api/v1/feedback/projects。
func (h *Handlers) ListProjects(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Community.Projects(c.Request.Context(), viewerOf(c), q.Page, q.PageSize)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// GetProject 是 GET /api/v1/feedback/projects/:slug。
func (h *Handlers) GetProject(c *gin.Context) {
	p, err := h.Community.Project(c.Request.Context(), viewerOf(c), c.Param("slug"))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, p)
}

// MyProjects 是 GET /api/v1/feedback/me/projects。
func (h *Handlers) MyProjects(c *gin.Context) {
	list, err := h.Community.MyProjects(c.Request.Context(), viewerOf(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	if list == nil {
		list = []dom.ProjectView{}
	}
	respond.OK(c, gin.H{"list": list})
}

// JoinProject 是 POST /api/v1/feedback/projects/:slug/join。
func (h *Handlers) JoinProject(c *gin.Context) {
	u, _ := currentUser(c)
	p, err := h.Community.Join(c.Request.Context(), u, c.Param("slug"))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, p)
}

// ---------- 动态 ----------

// ListPosts 是 GET /api/v1/feedback/posts：动态流（置顶优先）。
func (h *Handlers) ListPosts(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Community.Feed(c.Request.Context(), viewerOf(c), q.ProjectID, q.Page, q.PageSize)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// GetPost 是 GET /api/v1/feedback/posts/:id。
func (h *Handlers) GetPost(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	p, err := h.Community.Post(c.Request.Context(), viewerOf(c), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, p)
}

// ---------- 反馈 ----------

// ListFeedback 是 GET /api/v1/feedback/issues。
func (h *Handlers) ListFeedback(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Community.ListFeedback(c.Request.Context(), viewerOf(c), dom.FeedbackFilter{
		ProjectID: q.ProjectID, Status: dom.Status(q.Status), Kind: dom.Kind(q.Kind),
		Q: q.Q, Sort: q.Sort, Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// GetFeedback 是 GET /api/v1/feedback/issues/:slug/:number。
func (h *Handlers) GetFeedback(c *gin.Context) {
	number, ok := pathNumber(c)
	if !ok {
		return
	}
	fb, events, err := h.Community.FeedbackDetail(c.Request.Context(), viewerOf(c), c.Param("slug"), number)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if events == nil {
		events = []dom.Event{}
	}
	respond.OK(c, feedbackDetailDTO{Feedback: fb, Events: events})
}

// Submit 是 POST /api/v1/feedback/issues。
func (h *Handlers) Submit(c *gin.Context) {
	u, _ := currentUser(c)
	var req submitReq
	if !bind(c, &req) {
		return
	}
	fb, err := h.Community.Submit(c.Request.Context(), u, dom.SubmitInput{
		ProjectSlug: req.ProjectSlug, Kind: dom.Kind(req.Kind), Title: req.Title,
		Body: req.Body, Env: req.Env, PageURL: req.PageURL,
		Visibility: dom.Visibility(req.Visibility),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, fb)
}

// Vote 是 PUT /api/v1/feedback/issues/:slug/:number/vote：设置目标状态，重复调用结果一致。
func (h *Handlers) Vote(c *gin.Context) {
	u, _ := currentUser(c)
	number, ok := pathNumber(c)
	if !ok {
		return
	}
	var req voteReq
	if !bind(c, &req) {
		return
	}
	fb, err := h.Community.Vote(c.Request.Context(), u, c.Param("slug"), number, req.Active)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, fb)
}

// MyFeedback 是 GET /api/v1/feedback/me/issues。
func (h *Handlers) MyFeedback(c *gin.Context) {
	u, _ := currentUser(c)
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Community.MyFeedback(c.Request.Context(), u, q.Page, q.PageSize)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// Leaderboard 是 GET /api/v1/feedback/leaderboard：贡献榜，登录可见。
func (h *Handlers) Leaderboard(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Community.Leaderboard(c.Request.Context(), limit)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if list == nil {
		list = []dom.LeaderboardEntry{}
	}
	respond.OK(c, gin.H{"list": list})
}

// ---------- 议题区 ----------

// ListTopics 是 GET /api/v1/feedback/topics：征集中优先、置顶优先的议题列表。
func (h *Handlers) ListTopics(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Topics.Topics(c.Request.Context(), viewerOf(c), dom.TopicFilter{
		ProjectID: q.ProjectID, Status: dom.TopicStatus(q.Status), Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// GetTopic 是 GET /api/v1/feedback/topics/:id：详情里的 can_answer 表示当前访问者能否回答。
func (h *Handlers) GetTopic(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	t, err := h.Topics.Topic(c.Request.Context(), viewerOf(c), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, t)
}

// ListAnswers 是 GET /api/v1/feedback/topics/:id/answers：精选优先、再按「有道理」数。
func (h *Handlers) ListAnswers(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Topics.Answers(c.Request.Context(), viewerOf(c), id, q.Page, q.PageSize)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// CreateAnswer 是 POST /api/v1/feedback/topics/:id/answers：发布回答或回复（只有一层回复）。
func (h *Handlers) CreateAnswer(c *gin.Context) {
	u, _ := currentUser(c)
	id, ok := pathID(c)
	var req answerReq
	if !ok || !bind(c, &req) {
		return
	}
	a, err := h.Topics.Reply(c.Request.Context(), u, id, req.ParentID, req.Body)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, a)
}

// SetHelpful 是 PUT /api/v1/feedback/answers/:id/helpful：设置目标状态，重复调用结果一致。
func (h *Handlers) SetHelpful(c *gin.Context) {
	u, _ := currentUser(c)
	id, ok := pathID(c)
	var req helpfulReq
	if !ok || !bind(c, &req) {
		return
	}
	a, err := h.Topics.SetHelpful(c.Request.Context(), u, id, req.Active)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, a)
}
