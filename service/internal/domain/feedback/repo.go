package feedback

import (
	"context"
	"time"
)

// Repo 是反馈社区的仓储接口。
// 约定：查不到返回 errs.ErrNotFound；传入的数据已由领域层校验；时间由数据库生成。
type Repo interface {
	// ---------- 用户与注册 ----------

	// Register 在一个事务里核销邀请码并建用户：锁邀请行 → 调 check 判定业务规则 →
	// 建用户 → 写核销记录 → 递增使用次数 →（邀请码带项目时）写入项目成员。
	// check 返回错误时整个事务回滚，错误原样返回给调用方。
	Register(ctx context.Context, in NewUser, check func(Invite) error) (User, error)
	UserByHandle(ctx context.Context, handle string) (User, error)
	User(ctx context.Context, id int64) (User, error)
	UpdateSecret(ctx context.Context, id int64, passwordHash string) (User, error)
	UpdateProfile(ctx context.Context, id int64, in ProfileInput) (User, error)
	UpdateUserStatus(ctx context.Context, id int64, status UserStatus) (User, error)
	ListUsers(ctx context.Context, f UserFilter) ([]User, int64, error)
	Leaderboard(ctx context.Context, limit int) ([]LeaderboardEntry, error)

	// ---------- 项目 ----------

	CreateProject(ctx context.Context, in ProjectInput) (Project, error)
	UpdateProject(ctx context.Context, id int64, in ProjectInput) (Project, error)
	DeleteProject(ctx context.Context, id int64) (Project, error)
	Project(ctx context.Context, id int64) (Project, error)
	ProjectBySlug(ctx context.Context, slug string) (Project, error)
	ListProjects(ctx context.Context, f ProjectFilter) ([]Project, int64, error)
	// VisibleProjects / VisibleProject 带访问者视角：公开项目对所有人可见，成员项目只对已加入的用户可见。
	// 第二个返回值是可见项目总数（不是本页条数），前台据此分页与显示计数。
	VisibleProjects(ctx context.Context, v Viewer, page, pageSize int) ([]ProjectView, int64, error)
	VisibleProject(ctx context.Context, v Viewer, slug string) (ProjectView, error)

	// ---------- 成员 ----------

	UpsertMember(ctx context.Context, projectID, userID int64, role MemberRole, status string) (Member, error)
	Member(ctx context.Context, projectID, userID int64) (Member, error)
	ListMembers(ctx context.Context, projectID int64, page, pageSize int) ([]Member, int64, error)

	// ---------- 邀请码 ----------

	CreateInvite(ctx context.Context, codeHash, hint string, in InviteInput) (Invite, error)
	Invite(ctx context.Context, id int64) (Invite, error)
	ListInvites(ctx context.Context, projectID int64, page, pageSize int) ([]Invite, int64, error)
	RevokeInvite(ctx context.Context, id int64) (Invite, error)

	// ---------- 动态 ----------

	CreatePost(ctx context.Context, in PostInput, publishedAt *time.Time) (Post, error)
	UpdatePost(ctx context.Context, id int64, in PostInput, publishedAt *time.Time) (Post, error)
	DeletePost(ctx context.Context, id int64) (Post, error)
	Post(ctx context.Context, id int64) (Post, error)
	ListPosts(ctx context.Context, f PostFilter) ([]Post, int64, error)
	VisiblePosts(ctx context.Context, v Viewer, projectID int64, page, pageSize int) ([]Post, int64, error)
	VisiblePost(ctx context.Context, v Viewer, id int64) (Post, error)

	// ---------- 反馈 ----------

	// CreateFeedback 在一个事务里分配项目内编号、写反馈并写首条状态历史。
	CreateFeedback(ctx context.Context, projectID, authorID int64, in SubmitInput) (Feedback, error)
	Feedback(ctx context.Context, id int64) (Feedback, error)
	// FeedbackByKey 按 <项目标识>/<编号> 读取；可见性过滤在 SQL 中完成，无权查看返回 errs.ErrNotFound。
	FeedbackByKey(ctx context.Context, v Viewer, slug string, number int64) (Feedback, error)
	VisibleFeedback(ctx context.Context, v Viewer, f FeedbackFilter) ([]Feedback, int64, error)
	AdminFeedback(ctx context.Context, f FeedbackFilter) ([]Feedback, int64, error)
	StatusCounts(ctx context.Context, projectID int64) ([]StatusCount, error)
	// Transition 在一个事务里带乐观锁更新反馈、写状态历史，并在需要时记一次贡献。
	// version 不匹配返回 errs.ErrConflict。award 为 nil 时不记贡献。
	Transition(ctx context.Context, id int64, in TriageInput, award *Award) (Feedback, error)
	Events(ctx context.Context, feedbackID int64) ([]Event, error)

	// ---------- 投票 ----------

	SetVote(ctx context.Context, feedbackID, userID int64, active bool) (Feedback, error)
}

// NewUser 是建用户所需的数据（密码与恢复码已哈希）。
type NewUser struct {
	Handle       string
	Nickname     string
	PasswordHash string
	InviteHash   string
}

// Award 是一次贡献记账。SourceKey 保证同一来源只记一次。
type Award struct {
	UserID    int64
	ProjectID int64
	Kind      string
	Points    int32
	Reason    string
	SourceKey string
}

// TopicRepo 是议题区的仓储接口（迁移 00016）。
type TopicRepo interface {
	CreateTopic(ctx context.Context, in TopicInput, publishedAt *time.Time) (Topic, error)
	UpdateTopic(ctx context.Context, id int64, in TopicInput, publishedAt *time.Time) (Topic, error)
	DeleteTopic(ctx context.Context, id int64) (Topic, error)
	Topic(ctx context.Context, id int64) (Topic, error)
	ListTopics(ctx context.Context, f TopicFilter) ([]Topic, int64, error)
	// VisibleTopics / VisibleTopic 带访问者视角，草稿与归档不下发。
	VisibleTopics(ctx context.Context, v Viewer, f TopicFilter) ([]Topic, int64, error)
	VisibleTopic(ctx context.Context, v Viewer, id int64) (Topic, bool, error)

	// CreateAnswer 在一个事务里写回答并重算议题回答数。
	CreateAnswer(ctx context.Context, topicID, userID, parentID int64, body string) (Answer, error)
	Answer(ctx context.Context, id int64) (Answer, error)
	VisibleAnswers(ctx context.Context, v Viewer, topicID int64, page, pageSize int) ([]Answer, int64, error)
	AdminAnswers(ctx context.Context, topicID int64, page, pageSize int) ([]Answer, int64, error)
	SetAnswerFeatured(ctx context.Context, id int64, featured bool) (Answer, error)
	// SetAnswerStatus 在一个事务里改可见状态并重算议题回答数。
	SetAnswerStatus(ctx context.Context, id int64, status string) (Answer, error)
	// SetHelpful 设置或取消「有道理」，并按明细重算计数。
	SetHelpful(ctx context.Context, answerID, userID int64, active bool) (Answer, error)
}

// APIKeyRepo 是开放提交接口的仓储接口（迁移 00016）。
type APIKeyRepo interface {
	CreateAPIKey(ctx context.Context, keyHash, hint string, in APIKeyInput) (APIKey, error)
	UpdateAPIKey(ctx context.Context, id int64, in APIKeyInput) (APIKey, error)
	RevokeAPIKey(ctx context.Context, id int64) (APIKey, error)
	APIKey(ctx context.Context, id int64) (APIKey, error)
	ListAPIKeys(ctx context.Context, projectID int64, page, pageSize int) ([]APIKey, int64, error)
	// APIKeyByHash 只返回可用的 Key，并带上项目状态供业务判定。
	APIKeyByHash(ctx context.Context, keyHash string) (key APIKey, projectStatus ProjectStatus, err error)
	// CreateFeedbackFromAPI 在一个事务里分配编号、写反馈与首条状态历史，并累计 Key 的使用次数。
	CreateFeedbackFromAPI(ctx context.Context, key APIKey, in IntakeInput) (Feedback, error)
}
