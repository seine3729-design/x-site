// Package feedback 是反馈社区的领域层（feedback/docs/01、03，adr/0008）。
// 社区用户（fb_user）与后台管理员（admin_user）是两套身份：运营靠 RBAC 权限点管理社区，不需要社区账号。
package feedback

import "time"

// ---------- 枚举 ----------

// Status 是反馈状态（feedback/docs/adr/0004）。
type Status string

const (
	StatusSubmitted  Status = "submitted"   // 已收到，尚未确认
	StatusTriaged    Status = "triaged"     // 已确认并完成初步分类
	StatusPlanned    Status = "planned"     // 已进入计划，不承诺日期
	StatusInProgress Status = "in_progress" // 正在处理
	StatusVerifying  Status = "verifying"   // 等待验证
	StatusShipped    Status = "shipped"     // 已随版本上线
	StatusClosed     Status = "closed"      // 流程结束
	StatusDuplicate  Status = "duplicate"   // 与已有反馈重复
	StatusDeclined   Status = "declined"    // 当前不采纳
)

// allowedTransitions 定义合法的状态流转。终态允许在有新证据时回到 triaged。
var allowedTransitions = map[Status][]Status{
	StatusSubmitted:  {StatusTriaged, StatusDuplicate, StatusDeclined, StatusClosed},
	StatusTriaged:    {StatusPlanned, StatusInProgress, StatusDuplicate, StatusDeclined, StatusClosed},
	StatusPlanned:    {StatusInProgress, StatusTriaged, StatusDeclined, StatusClosed},
	StatusInProgress: {StatusVerifying, StatusShipped, StatusPlanned, StatusClosed},
	StatusVerifying:  {StatusShipped, StatusInProgress, StatusClosed},
	StatusShipped:    {StatusClosed, StatusTriaged},
	StatusClosed:     {StatusTriaged},
	StatusDuplicate:  {StatusTriaged},
	StatusDeclined:   {StatusTriaged},
}

// CanTransition 返回 from → to 是否是合法流转（同状态视为合法，用于只改优先级等字段）。
func CanTransition(from, to Status) bool {
	if from == to {
		return true
	}
	for _, s := range allowedTransitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// StatusLabel 返回状态的中文说明，用于状态历史的默认公开文案。
func StatusLabel(s Status) string {
	switch s {
	case StatusSubmitted:
		return "已收到"
	case StatusTriaged:
		return "已确认"
	case StatusPlanned:
		return "已进入计划"
	case StatusInProgress:
		return "正在处理"
	case StatusVerifying:
		return "等待验证"
	case StatusShipped:
		return "已上线"
	case StatusClosed:
		return "已关闭"
	case StatusDuplicate:
		return "重复反馈"
	case StatusDeclined:
		return "暂不采纳"
	}
	return string(s)
}

// Kind 是反馈类型。
type Kind string

const (
	KindBug        Kind = "bug"
	KindExperience Kind = "experience"
	KindFeature    Kind = "feature"
	KindContent    Kind = "content"
	KindAccess     Kind = "access"
	KindCompetitor Kind = "competitor" // 竞品发现（00016 新增）
	KindOther      Kind = "other"
)

// Priority 是处理优先级。
type Priority string

const (
	PriorityCritical Priority = "critical"
	PriorityHigh     Priority = "high"
	PriorityMedium   Priority = "medium"
	PriorityLow      Priority = "low"
	PriorityUnset    Priority = "unset"
)

// Visibility 是内容可见性（feedback/docs/00 第 8.2 节）。
type Visibility string

const (
	VisibilityPublic  Visibility = "public"  // 所有人可见
	VisibilityMembers Visibility = "members" // 仅项目成员
	VisibilityPrivate Visibility = "private" // 仅作者与项目团队（只用于反馈）
)

// ProjectStatus 是项目生命周期。
type ProjectStatus string

const (
	ProjectDraft      ProjectStatus = "draft"
	ProjectRecruiting ProjectStatus = "recruiting"
	ProjectActive     ProjectStatus = "active"
	ProjectPaused     ProjectStatus = "paused"
	ProjectArchived   ProjectStatus = "archived"
)

// JoinMode 是项目的加入方式。
type JoinMode string

const (
	JoinOpen        JoinMode = "open"        // 登录用户可直接加入
	JoinApplication JoinMode = "application" // 需审核（首期按待审处理）
	JoinInviteOnly  JoinMode = "invite_only" // 只能通过邀请码
	JoinClosed      JoinMode = "closed"      // 不接受新成员
)

// MemberRole 是项目内角色；member 之外都属于项目团队。
type MemberRole string

const (
	RoleMember     MemberRole = "member"
	RoleOperator   MemberRole = "operator"
	RoleMaintainer MemberRole = "maintainer"
	RoleOwner      MemberRole = "owner"
)

// IsTeam 表示该角色属于项目团队（可见成员内容与私密反馈）。
func (r MemberRole) IsTeam() bool {
	return r == RoleOperator || r == RoleMaintainer || r == RoleOwner
}

// PostKind 是动态类型。
type PostKind string

const (
	PostAnnouncement PostKind = "announcement" // 公告
	PostTestTask     PostKind = "test_task"    // 测试任务
	PostChangelog    PostKind = "changelog"    // 上线小结
)

// UserStatus 是社区账号状态。
type UserStatus string

const (
	UserActive     UserStatus = "active"
	UserRestricted UserStatus = "restricted" // 只读
	UserSuspended  UserStatus = "suspended"  // 封禁
)

// ---------- 实体 ----------

// User 是社区用户。密码与恢复码哈希不出现在任何 DTO 中。
type User struct {
	ID             int64      `json:"id"`
	Handle         string     `json:"handle"`
	Nickname       string     `json:"nickname"`
	Bio            string     `json:"bio"`
	ShowLevel      bool       `json:"show_level"`
	Status         UserStatus `json:"status"`
	TesterNo       int64      `json:"tester_no"` // 内测编号，展示署名用，不暴露用户 ID
	Points         int32      `json:"points"`
	SessionVersion int32      `json:"-"`
	PasswordHash   string     `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
}

// DisplayName 返回昵称，没有昵称时退回用户名。
func (u User) DisplayName() string {
	if u.Nickname != "" {
		return u.Nickname
	}
	return u.Handle
}

// Project 是内测项目。
type Project struct {
	ID          int64         `json:"id"`
	Slug        string        `json:"slug"`
	KeyPrefix   string        `json:"key_prefix"`
	Name        string        `json:"name"`
	Summary     string        `json:"summary"`
	Description string        `json:"description"`
	Cover       string        `json:"cover"`
	TryURL      string        `json:"try_url"`
	Status      ProjectStatus `json:"status"`
	Visibility  Visibility    `json:"visibility"`
	JoinMode    JoinMode      `json:"join_mode"`
	Sort        int32         `json:"sort"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// AcceptsFeedback 表示项目当前允许提交反馈。
func (p Project) AcceptsFeedback() bool {
	return p.Status == ProjectRecruiting || p.Status == ProjectActive
}

// ProjectView 是前台看到的项目（带访问者视角的字段）。
type ProjectView struct {
	Project
	Joined     bool       `json:"joined"`
	MemberRole MemberRole `json:"member_role,omitempty"`
	// MemberStatus 是访问者在这个项目里的成员状态：active / pending / removed，
	// 空串表示还不是成员。前台用它区分「申请加入」与「申请中，等待确认」。
	MemberStatus  string `json:"member_status,omitempty"`
	MemberCount   int64  `json:"member_count"`
	FeedbackCount int64  `json:"feedback_count"`
}

// Member 是项目成员。
type Member struct {
	ProjectID  int64      `json:"project_id"`
	UserID     int64      `json:"user_id"`
	Handle     string     `json:"handle,omitempty"`
	Nickname   string     `json:"nickname,omitempty"`
	UserStatus UserStatus `json:"user_status,omitempty"`
	Role       MemberRole `json:"role"`
	Status     string     `json:"status"`
	JoinedAt   time.Time  `json:"joined_at"`
}

// Invite 是邀请码。明文只在创建时返回一次。
type Invite struct {
	ID          int64      `json:"id"`
	CodeHint    string     `json:"code_hint"`
	ProjectID   int64      `json:"project_id,omitempty"`
	ProjectName string     `json:"project_name,omitempty"`
	Role        MemberRole `json:"role"`
	MaxUses     int32      `json:"max_uses"`
	UsedCount   int32      `json:"used_count"`
	Note        string     `json:"note"`
	ExpiresAt   *time.Time `json:"expires_at"`
	RevokedAt   *time.Time `json:"revoked_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Usable 返回邀请码当前是否可用；不可用时给出面向运营的原因。
func (i Invite) Usable(now time.Time) (bool, string) {
	switch {
	case i.RevokedAt != nil:
		return false, "邀请码已作废"
	case i.ExpiresAt != nil && now.After(*i.ExpiresAt):
		return false, "邀请码已过期"
	case i.UsedCount >= i.MaxUses:
		return false, "邀请码使用次数已用完"
	}
	return true, ""
}

// Post 是项目动态。
type Post struct {
	ID          int64      `json:"id"`
	ProjectID   int64      `json:"project_id"`
	ProjectName string     `json:"project_name,omitempty"`
	ProjectSlug string     `json:"project_slug,omitempty"`
	Kind        PostKind   `json:"kind"`
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Status      string     `json:"status"`
	Visibility  Visibility `json:"visibility"`
	Pinned      bool       `json:"pinned"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Feedback 是一条反馈。Key 为 <项目前缀>-<编号>。
type Feedback struct {
	ID             int64      `json:"id"`
	ProjectID      int64      `json:"project_id"`
	ProjectSlug    string     `json:"project_slug,omitempty"`
	ProjectName    string     `json:"project_name,omitempty"`
	Number         int64      `json:"number"`
	Key            string     `json:"key"`
	AuthorID       int64      `json:"author_id,omitempty"`
	AuthorHandle   string     `json:"author_handle,omitempty"`
	AuthorNickname string     `json:"author_nickname,omitempty"`
	Kind           Kind       `json:"kind"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	Env            string     `json:"env"`
	Visibility     Visibility `json:"visibility"`
	Status         Status     `json:"status"`
	Priority       Priority   `json:"priority"`
	DuplicateOf    int64      `json:"duplicate_of,omitempty"`
	VoteCount      int32      `json:"vote_count"`
	Voted          bool       `json:"voted"`
	Source         Source     `json:"source"`
	APIKeyID       int64      `json:"api_key_id,omitempty"`
	PageURL        string     `json:"page_url,omitempty"`
	// Contact 是提交者自愿留的联系方式，只在后台响应里出现。
	Contact        string    `json:"contact,omitempty"`
	Version        int32     `json:"version"`
	LastActivityAt time.Time `json:"last_activity_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// Event 是反馈的状态历史。InternalNote 只能出现在后台响应里。
type Event struct {
	ID           int64     `json:"id"`
	FromStatus   string    `json:"from_status"`
	ToStatus     Status    `json:"to_status"`
	Note         string    `json:"note"`
	InternalNote string    `json:"internal_note,omitempty"`
	ActorName    string    `json:"actor_name,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// StatusCount 是按状态的反馈数量。
type StatusCount struct {
	Status Status `json:"status"`
	Total  int64  `json:"total"`
}

// LeaderboardEntry 是贡献榜条目（登录可见）。
type LeaderboardEntry struct {
	UserID   int64  `json:"user_id"`
	Handle   string `json:"handle"`
	Nickname string `json:"nickname"`
	Points   int32  `json:"points"`
}

// ---------- 分页与筛选 ----------

// Page 是页码分页结果（后台与前台列表共用）。
type Page[T any] struct {
	List     []T
	Total    int64
	Page     int
	PageSize int
}

// Viewer 是当前访问者；未登录时 UserID 为 0。
type Viewer struct {
	UserID int64
}

// LoggedIn 表示已登录。
func (v Viewer) LoggedIn() bool { return v.UserID > 0 }

// FeedbackFilter 是反馈列表的筛选条件。
type FeedbackFilter struct {
	ProjectID  int64
	Status     Status
	Kind       Kind
	Priority   Priority
	Visibility Visibility
	AuthorID   int64
	Source     Source
	Q          string
	Sort       string // latest（默认）| newest | votes；后台另支持 oldest
	Page       int
	PageSize   int
}

// ProjectFilter 是后台项目列表的筛选条件。
type ProjectFilter struct {
	Q        string
	Status   ProjectStatus
	Page     int
	PageSize int
}

// PostFilter 是动态列表的筛选条件。
type PostFilter struct {
	ProjectID int64
	Status    string
	Page      int
	PageSize  int
}

// UserFilter 是后台社区成员列表的筛选条件。
type UserFilter struct {
	Q        string
	Status   UserStatus
	Page     int
	PageSize int
}

// ---------- 输入 ----------

// RegisterInput 是注册请求。
type RegisterInput struct {
	Handle     string
	Password   string
	InviteCode string
	Nickname   string
}

// LoginInput 是登录请求。
type LoginInput struct {
	Handle   string
	Password string
}

// ChangePasswordInput 是用当前密码改密。
type ChangePasswordInput struct {
	OldPassword string
	NewPassword string
}

// ProfileInput 是修改公开资料。
type ProfileInput struct {
	Nickname  string
	Bio       string
	ShowLevel bool
}

// ProjectInput 是项目的整体保存内容。
type ProjectInput struct {
	Slug        string
	KeyPrefix   string
	Name        string
	Summary     string
	Description string
	Cover       string
	TryURL      string
	Status      ProjectStatus
	Visibility  Visibility
	JoinMode    JoinMode
	Sort        int32
}

// InviteInput 是创建邀请码的内容。
type InviteInput struct {
	ProjectID int64
	Role      MemberRole
	MaxUses   int32
	Note      string
	ExpiresAt *time.Time
	CreatedBy int64
}

// PostInput 是动态的整体保存内容。
type PostInput struct {
	ProjectID  int64
	Kind       PostKind
	Title      string
	Body       string
	Status     string
	Visibility Visibility
	Pinned     bool
}

// SubmitInput 是用户提交反馈的内容。
type SubmitInput struct {
	ProjectSlug string
	Kind        Kind
	Title       string
	Body        string
	Env         string
	PageURL     string
	Visibility  Visibility
}

// TriageInput 是后台分诊与状态流转的内容。
type TriageInput struct {
	Status          Status
	Priority        Priority
	Visibility      Visibility
	DuplicateOf     int64
	Note            string // 公开说明
	InternalNote    string // 只给后台
	ExpectedVersion int32
	ActorAdminID    int64
}

// AuthResult 是注册或登录成功后的结果。
type AuthResult struct {
	User      User   `json:"user"`
	Token     string `json:"token"`
	ExpiresIn int64  `json:"expires_in"`
}

// ---------- 议题区（迁移 00016） ----------

// TopicStatus 是议题状态。open 即「征集中」。
type TopicStatus string

const (
	TopicDraft    TopicStatus = "draft"    // 草稿，前台不可见
	TopicOpen     TopicStatus = "open"     // 征集中，可回答
	TopicClosed   TopicStatus = "closed"   // 已结束，只读
	TopicArchived TopicStatus = "archived" // 已归档，前台不列出
)

// TopicLabel 返回议题状态的中文文案。
func TopicLabel(s TopicStatus) string {
	switch s {
	case TopicDraft:
		return "草稿"
	case TopicOpen:
		return "征集中"
	case TopicClosed:
		return "已结束"
	case TopicArchived:
		return "已归档"
	}
	return string(s)
}

// Topic 是一个运营向议题。首期只支持自由回答。
type Topic struct {
	ID           int64       `json:"id"`
	ProjectID    int64       `json:"project_id"`
	ProjectName  string      `json:"project_name,omitempty"`
	ProjectSlug  string      `json:"project_slug,omitempty"`
	ProjectCover string      `json:"project_cover,omitempty"`
	Title        string      `json:"title"`
	Body         string      `json:"body"`
	AnswerHint   string      `json:"answer_hint"`
	Status       TopicStatus `json:"status"`
	Visibility   Visibility  `json:"visibility"`
	Pinned       bool        `json:"pinned"`
	AnswerCount  int32       `json:"answer_count"`
	EndsAt       *time.Time  `json:"ends_at"`
	PublishedAt  *time.Time  `json:"published_at"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	// CanAnswer 只在前台详情里有意义：议题处于征集中且访问者有权回答。
	CanAnswer bool `json:"can_answer,omitempty"`
}

// Answerable 表示议题当前还能回答。
func (t Topic) Answerable(now time.Time) bool {
	if t.Status != TopicOpen {
		return false
	}
	return t.EndsAt == nil || now.Before(*t.EndsAt)
}

// Answer 是一条议题回答。展示署名用内测编号，不暴露用户 ID。
type Answer struct {
	ID           int64     `json:"id"`
	TopicID      int64     `json:"topic_id"`
	ParentID     int64     `json:"parent_id,omitempty"`
	TesterNo     int64     `json:"tester_no,omitempty"`
	Handle       string    `json:"handle,omitempty"`
	Nickname     string    `json:"nickname,omitempty"`
	Body         string    `json:"body"`
	Featured     bool      `json:"featured"`
	HelpfulCount int32     `json:"helpful_count"`
	HelpfulByMe  bool      `json:"helpful_by_me"`
	Status       string    `json:"status,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// TopicInput 是议题的整体保存内容。
type TopicInput struct {
	ProjectID  int64
	Title      string
	Body       string
	AnswerHint string
	Status     TopicStatus
	Visibility Visibility
	Pinned     bool
	EndsAt     *time.Time
	CreatedBy  int64
}

// TopicFilter 是后台议题列表的筛选条件。
type TopicFilter struct {
	ProjectID int64
	Status    TopicStatus
	Q         string
	Page      int
	PageSize  int
}

// ---------- 开放提交接口（迁移 00016） ----------

// Source 是反馈来源。
type Source string

const (
	SourceCommunity Source = "community" // 社区用户自己提交
	SourceOpenAPI   Source = "open_api"  // 外部网站经 API Key 送入
)

// APIKey 是开放提交接口的密钥。明文只在创建时返回一次。
type APIKey struct {
	ID                int64      `json:"id"`
	ProjectID         int64      `json:"project_id"`
	ProjectName       string     `json:"project_name,omitempty"`
	ProjectSlug       string     `json:"project_slug,omitempty"`
	Name              string     `json:"name"`
	KeyHint           string     `json:"key_hint"`
	DefaultVisibility Visibility `json:"default_visibility"`
	AllowedOrigins    []string   `json:"allowed_origins"`
	DailyLimit        int32      `json:"daily_limit"`
	Status            string     `json:"status"`
	UsedCount         int64      `json:"used_count"`
	LastUsedAt        *time.Time `json:"last_used_at"`
	RevokedAt         *time.Time `json:"revoked_at"`
	CreatedAt         time.Time  `json:"created_at"`
}

// Usable 返回该 Key 当前是否可用。
func (k APIKey) Usable() bool { return k.RevokedAt == nil && k.Status == "active" }

// APIKeyInput 是 API Key 的保存内容。
type APIKeyInput struct {
	ProjectID         int64
	Name              string
	DefaultVisibility Visibility
	AllowedOrigins    []string
	DailyLimit        int32
	Status            string
	CreatedBy         int64
}

// IntakeInput 是外部网站提交的一条反馈。
type IntakeInput struct {
	Kind    Kind
	Title   string
	Body    string
	Env     string
	PageURL string
	Contact string
	// Origin 是浏览器请求的来源，用于校验 Key 的来源白名单；服务端对服务端调用可为空。
	Origin string
}

// IntakeResult 是外部提交成功后的回执。
type IntakeResult struct {
	Key         string `json:"key"`          // 展示编号，如 APP-1024
	Number      int64  `json:"number"`       // 项目内编号
	ProjectSlug string `json:"project_slug"` // 所属项目
	Status      Status `json:"status"`
}
