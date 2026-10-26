package feedback

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/feedback"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// ---------- 请求 ----------

type listQuery struct {
	Q          string `form:"q"`
	ProjectID  int64  `form:"project_id"`
	Status     string `form:"status"`
	Kind       string `form:"kind"`
	Priority   string `form:"priority"`
	Visibility string `form:"visibility"`
	Source     string `form:"source"`
	Sort       string `form:"sort"`
	Page       int    `form:"page"`
	PageSize   int    `form:"page_size"`
}

type projectReq struct {
	Slug        string `json:"slug"`
	KeyPrefix   string `json:"key_prefix"`
	Name        string `json:"name"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Cover       string `json:"cover"`
	TryURL      string `json:"try_url"`
	Status      string `json:"status"`
	Visibility  string `json:"visibility"`
	JoinMode    string `json:"join_mode"`
	Sort        int32  `json:"sort"`
}

func (r projectReq) input() dom.ProjectInput {
	return dom.ProjectInput{
		Slug: r.Slug, KeyPrefix: r.KeyPrefix, Name: r.Name, Summary: r.Summary, Description: r.Description,
		Cover: r.Cover, TryURL: r.TryURL, Status: dom.ProjectStatus(r.Status),
		Visibility: dom.Visibility(r.Visibility), JoinMode: dom.JoinMode(r.JoinMode), Sort: r.Sort,
	}
}

type memberReq struct {
	Role   string `json:"role"`
	Remove bool   `json:"remove"`
}

type inviteReq struct {
	ProjectID int64      `json:"project_id"`
	Role      string     `json:"role"`
	MaxUses   int32      `json:"max_uses"`
	Note      string     `json:"note"`
	ExpiresAt *time.Time `json:"expires_at"`
}

type postReq struct {
	ProjectID  int64  `json:"project_id"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	Status     string `json:"status"`
	Visibility string `json:"visibility"`
	Pinned     bool   `json:"pinned"`
}

func (r postReq) input() dom.PostInput {
	return dom.PostInput{
		ProjectID: r.ProjectID, Kind: dom.PostKind(r.Kind), Title: r.Title, Body: r.Body,
		Status: r.Status, Visibility: dom.Visibility(r.Visibility), Pinned: r.Pinned,
	}
}

type triageReq struct {
	Status       string `json:"status"`
	Priority     string `json:"priority"`
	Visibility   string `json:"visibility"`
	DuplicateOf  int64  `json:"duplicate_of"`
	Note         string `json:"note"`
	InternalNote string `json:"internal_note"`
	Version      int32  `json:"version"`
}

type statusReq struct {
	Status string `json:"status"`
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

// inviteCreatedDTO 是新建邀请码的响应：明文只在这里出现一次。
type inviteCreatedDTO struct {
	Invite dom.Invite `json:"invite"`
	Code   string     `json:"code"`
}

// feedbackDetailDTO 是后台反馈详情：含内部备注的完整状态历史。
type feedbackDetailDTO struct {
	Feedback dom.Feedback `json:"feedback"`
	Events   []dom.Event  `json:"events"`
}

func bindQuery(c *gin.Context, dst any) bool {
	if err := c.ShouldBindQuery(dst); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return false
	}
	return true
}

// ---------- 项目 ----------

// ListProjects 是 GET /api/admin/feedback/projects。
func (h *Handlers) ListProjects(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Svc.ListProjects(c.Request.Context(), dom.ProjectFilter{
		Q: q.Q, Status: dom.ProjectStatus(q.Status), Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// CreateProject 是 POST /api/admin/feedback/projects。
func (h *Handlers) CreateProject(c *gin.Context) {
	var req projectReq
	if !admin.Bind(c, &req) {
		return
	}
	p, err := h.Svc.CreateProject(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_project.create", TargetType: "fb_project", TargetID: itoa(p.ID), After: p})
	respond.OK(c, p)
}

// UpdateProject 是 PATCH /api/admin/feedback/projects/:id：整体保存。
func (h *Handlers) UpdateProject(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req projectReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.UpdateProject(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_project.update", TargetType: "fb_project", TargetID: itoa(id), Before: before, After: after})
	respond.OK(c, after)
}

// DeleteProject 是 DELETE /api/admin/feedback/projects/:id：只允许删除还没有反馈的项目。
func (h *Handlers) DeleteProject(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Svc.DeleteProject(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_project.delete", TargetType: "fb_project", TargetID: itoa(id), Before: before})
	respond.OK(c, nil)
}

// ListMembers 是 GET /api/admin/feedback/projects/:id/members。
func (h *Handlers) ListMembers(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Svc.ListMembers(c.Request.Context(), id, q.Page, q.PageSize)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// SetMemberRole 是 PATCH /api/admin/feedback/projects/:id/members/:user_id：改角色或移除成员。
func (h *Handlers) SetMemberRole(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	userID, ok := admin.ParamID(c, "user_id")
	var req memberReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	m, err := h.Svc.SetMemberRole(c.Request.Context(), id, userID, dom.MemberRole(req.Role), req.Remove)
	if err != nil {
		respond.Error(c, err)
		return
	}
	action := "fb_member.update"
	if req.Remove {
		action = "fb_member.remove"
	}
	admin.SetAudit(c, admin.AuditDetail{Action: action, TargetType: "fb_member", TargetID: itoa(id) + ":" + itoa(userID), After: m})
	respond.OK(c, m)
}

// ---------- 邀请码 ----------

// ListInvites 是 GET /api/admin/feedback/invites。
func (h *Handlers) ListInvites(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Svc.ListInvites(c.Request.Context(), q.ProjectID, q.Page, q.PageSize)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// CreateInvite 是 POST /api/admin/feedback/invites：明文邀请码只在响应里出现一次，不写审计。
func (h *Handlers) CreateInvite(c *gin.Context) {
	var req inviteReq
	if !admin.Bind(c, &req) {
		return
	}
	who, _ := admin.CurrentAdmin(c)
	inv, code, err := h.Svc.CreateInvite(c.Request.Context(), dom.InviteInput{
		ProjectID: req.ProjectID, Role: dom.MemberRole(req.Role), MaxUses: req.MaxUses,
		Note: req.Note, ExpiresAt: req.ExpiresAt, CreatedBy: who.AdminID,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	// 审计里只记邀请码的元信息（inv 不含明文），明文不进日志与审计
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_invite.create", TargetType: "fb_invite", TargetID: itoa(inv.ID), After: inv})
	respond.OK(c, inviteCreatedDTO{Invite: inv, Code: code})
}

// RevokeInvite 是 POST /api/admin/feedback/invites/:id/revoke。
func (h *Handlers) RevokeInvite(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	inv, err := h.Svc.RevokeInvite(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_invite.revoke", TargetType: "fb_invite", TargetID: itoa(id), After: inv})
	respond.OK(c, inv)
}

// ---------- 动态 ----------

// ListPosts 是 GET /api/admin/feedback/posts。
func (h *Handlers) ListPosts(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Svc.ListPosts(c.Request.Context(), dom.PostFilter{
		ProjectID: q.ProjectID, Status: q.Status, Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// CreatePost 是 POST /api/admin/feedback/posts。
func (h *Handlers) CreatePost(c *gin.Context) {
	var req postReq
	if !admin.Bind(c, &req) {
		return
	}
	p, err := h.Svc.CreatePost(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_post.create", TargetType: "fb_post", TargetID: itoa(p.ID), After: p})
	respond.OK(c, p)
}

// UpdatePost 是 PATCH /api/admin/feedback/posts/:id：整体保存。
func (h *Handlers) UpdatePost(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req postReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.UpdatePost(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_post.update", TargetType: "fb_post", TargetID: itoa(id), Before: before, After: after})
	respond.OK(c, after)
}

// DeletePost 是 DELETE /api/admin/feedback/posts/:id。
func (h *Handlers) DeletePost(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Svc.DeletePost(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_post.delete", TargetType: "fb_post", TargetID: itoa(id), Before: before})
	respond.OK(c, nil)
}

// ---------- 反馈分诊 ----------

// ListFeedback 是 GET /api/admin/feedback/issues。
func (h *Handlers) ListFeedback(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Svc.ListFeedback(c.Request.Context(), dom.FeedbackFilter{
		ProjectID: q.ProjectID, Status: dom.Status(q.Status), Kind: dom.Kind(q.Kind),
		Priority: dom.Priority(q.Priority), Visibility: dom.Visibility(q.Visibility),
		Source: dom.Source(q.Source),
		Q:      q.Q, Sort: q.Sort, Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// GetFeedback 是 GET /api/admin/feedback/issues/:id：含内部备注的完整状态历史。
func (h *Handlers) GetFeedback(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	fb, events, err := h.Svc.FeedbackDetail(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if events == nil {
		events = []dom.Event{}
	}
	respond.OK(c, feedbackDetailDTO{Feedback: fb, Events: events})
}

// Triage 是 POST /api/admin/feedback/issues/:id/triage：分诊并流转状态（带乐观锁）。
func (h *Handlers) Triage(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req triageReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	who, _ := admin.CurrentAdmin(c)
	before, after, err := h.Svc.Triage(c.Request.Context(), id, dom.TriageInput{
		Status: dom.Status(req.Status), Priority: dom.Priority(req.Priority),
		Visibility: dom.Visibility(req.Visibility), DuplicateOf: req.DuplicateOf,
		Note: req.Note, InternalNote: req.InternalNote,
		ExpectedVersion: req.Version, ActorAdminID: who.AdminID,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_feedback.triage", TargetType: "fb_feedback", TargetID: itoa(id), Before: before, After: after})
	respond.OK(c, after)
}

// Overview 是 GET /api/admin/feedback/issues/overview：按状态的反馈数量。
func (h *Handlers) Overview(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	counts, err := h.Svc.Overview(c.Request.Context(), q.ProjectID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	if counts == nil {
		counts = []dom.StatusCount{}
	}
	respond.OK(c, gin.H{"list": counts})
}

// ---------- 社区成员 ----------

// ListUsers 是 GET /api/admin/feedback/users。
func (h *Handlers) ListUsers(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Svc.ListUsers(c.Request.Context(), dom.UserFilter{
		Q: q.Q, Status: dom.UserStatus(q.Status), Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// SetUserStatus 是 POST /api/admin/feedback/users/:id/status：停用或恢复账号。
func (h *Handlers) SetUserStatus(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req statusReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.SetUserStatus(c.Request.Context(), id, dom.UserStatus(req.Status))
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_user.status", TargetType: "fb_user", TargetID: itoa(id), Before: before, After: after})
	respond.OK(c, after)
}

// ---------- 议题区 ----------

// topicReq 是议题的整体保存内容。
type topicReq struct {
	ProjectID  int64      `json:"project_id"`
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	AnswerHint string     `json:"answer_hint"`
	Status     string     `json:"status"`
	Visibility string     `json:"visibility"`
	Pinned     bool       `json:"pinned"`
	EndsAt     *time.Time `json:"ends_at"`
}

func (r topicReq) input() dom.TopicInput {
	return dom.TopicInput{
		ProjectID: r.ProjectID, Title: r.Title, Body: r.Body, AnswerHint: r.AnswerHint,
		Status: dom.TopicStatus(r.Status), Visibility: dom.Visibility(r.Visibility),
		Pinned: r.Pinned, EndsAt: r.EndsAt,
	}
}

// featuredReq 是设置「精选」回答的目标状态。
type featuredReq struct {
	Featured bool `json:"featured"`
}

// ListTopics 是 GET /api/admin/feedback/topics。
func (h *Handlers) ListTopics(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Topics.AdminTopics(c.Request.Context(), dom.TopicFilter{
		ProjectID: q.ProjectID, Status: dom.TopicStatus(q.Status), Q: q.Q,
		Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// CreateTopic 是 POST /api/admin/feedback/topics。
func (h *Handlers) CreateTopic(c *gin.Context) {
	var req topicReq
	if !admin.Bind(c, &req) {
		return
	}
	who, _ := admin.CurrentAdmin(c)
	in := req.input()
	in.CreatedBy = who.AdminID
	t, err := h.Topics.CreateTopic(c.Request.Context(), in)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_topic.create", TargetType: "fb_topic", TargetID: itoa(t.ID), After: t})
	respond.OK(c, t)
}

// GetTopic 是 GET /api/admin/feedback/topics/:id。
func (h *Handlers) GetTopic(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	t, err := h.Topics.TopicByID(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, t)
}

// UpdateTopic 是 PATCH /api/admin/feedback/topics/:id：整体保存。
func (h *Handlers) UpdateTopic(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req topicReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Topics.UpdateTopic(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_topic.update", TargetType: "fb_topic", TargetID: itoa(id), Before: before, After: after})
	respond.OK(c, after)
}

// DeleteTopic 是 DELETE /api/admin/feedback/topics/:id：已有回答的议题只能归档，不能删除。
func (h *Handlers) DeleteTopic(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Topics.DeleteTopic(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_topic.delete", TargetType: "fb_topic", TargetID: itoa(id), Before: before})
	respond.OK(c, nil)
}

// ListAnswers 是 GET /api/admin/feedback/topics/:id/answers：含已隐藏的回答。
func (h *Handlers) ListAnswers(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Topics.AdminAnswers(c.Request.Context(), id, q.Page, q.PageSize)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// SetAnswerFeatured 是 POST /api/admin/feedback/answers/:id/featured。
func (h *Handlers) SetAnswerFeatured(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req featuredReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Topics.SetFeatured(c.Request.Context(), id, req.Featured)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_answer.featured", TargetType: "fb_topic_answer", TargetID: itoa(id), Before: before, After: after})
	respond.OK(c, after)
}

// SetAnswerStatus 是 POST /api/admin/feedback/answers/:id/status：隐藏或恢复一条回答。
func (h *Handlers) SetAnswerStatus(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req statusReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Topics.SetAnswerStatus(c.Request.Context(), id, req.Status)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_answer.status", TargetType: "fb_topic_answer", TargetID: itoa(id), Before: before, After: after})
	respond.OK(c, after)
}

// ---------- 开放提交接口的密钥 ----------

// apiKeyReq 是 API Key 的保存内容。改时不接受 project_id：换项目会让历史反馈归属错乱。
type apiKeyReq struct {
	ProjectID         int64    `json:"project_id"`
	Name              string   `json:"name"`
	DefaultVisibility string   `json:"default_visibility"`
	AllowedOrigins    []string `json:"allowed_origins"`
	DailyLimit        int32    `json:"daily_limit"`
	Status            string   `json:"status"`
}

func (r apiKeyReq) input() dom.APIKeyInput {
	return dom.APIKeyInput{
		ProjectID: r.ProjectID, Name: r.Name,
		DefaultVisibility: dom.Visibility(r.DefaultVisibility),
		AllowedOrigins:    r.AllowedOrigins, DailyLimit: r.DailyLimit, Status: r.Status,
	}
}

// apiKeyCreatedDTO 是新建 Key 的响应：明文只在这里出现一次。
type apiKeyCreatedDTO struct {
	Key dom.APIKey `json:"key"`
	Raw string     `json:"raw_key"`
}

// ListAPIKeys 是 GET /api/admin/feedback/api-keys。
func (h *Handlers) ListAPIKeys(c *gin.Context) {
	var q listQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Keys.ListAPIKeys(c.Request.Context(), q.ProjectID, q.Page, q.PageSize)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page))
}

// CreateAPIKey 是 POST /api/admin/feedback/api-keys：明文 Key 只在响应里出现一次，不写审计、不进日志。
func (h *Handlers) CreateAPIKey(c *gin.Context) {
	var req apiKeyReq
	if !admin.Bind(c, &req) {
		return
	}
	who, _ := admin.CurrentAdmin(c)
	in := req.input()
	in.CreatedBy = who.AdminID
	key, raw, err := h.Keys.CreateAPIKey(c.Request.Context(), in)
	if err != nil {
		respond.Error(c, err)
		return
	}
	// key 不含明文，可以安全进审计
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_api_key.create", TargetType: "fb_api_key", TargetID: itoa(key.ID), After: key})
	respond.OK(c, apiKeyCreatedDTO{Key: key, Raw: raw})
}

// UpdateAPIKey 是 PATCH /api/admin/feedback/api-keys/:id：整体保存配置（不换项目）。
func (h *Handlers) UpdateAPIKey(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req apiKeyReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Keys.UpdateAPIKey(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_api_key.update", TargetType: "fb_api_key", TargetID: itoa(id), Before: before, After: after})
	respond.OK(c, after)
}

// RevokeAPIKey 是 POST /api/admin/feedback/api-keys/:id/revoke：作废后该 Key 立即不可用。
func (h *Handlers) RevokeAPIKey(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	key, err := h.Keys.RevokeAPIKey(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "fb_api_key.revoke", TargetType: "fb_api_key", TargetID: itoa(id), After: key})
	respond.OK(c, key)
}
