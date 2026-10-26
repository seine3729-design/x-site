package feedback

import (
	"context"
	"errors"
	"strconv"
	"time"

	"xsite/service/internal/errs"
)

// ManageService 是运营后台用到的管理能力。权限由 HTTP 层的 RBAC 中间件判定，这里只管业务规则。
type ManageService struct {
	repo Repo
	now  func() time.Time
}

func NewManageService(repo Repo) *ManageService {
	return &ManageService{repo: repo, now: time.Now}
}

// ---------- 项目 ----------

// ListProjects 返回后台项目列表（页码分页）。
func (s *ManageService) ListProjects(ctx context.Context, f ProjectFilter) (Page[Project], error) {
	p, size, _, _, err := limitOffset(f.Page, f.PageSize)
	if err != nil {
		return Page[Project]{}, err
	}
	f.Page, f.PageSize, f.Q = p, size, cleanQuery(f.Q)
	list, total, err := s.repo.ListProjects(ctx, f)
	if err != nil {
		return Page[Project]{}, err
	}
	return Page[Project]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// CreateProject 新建项目。只有平台管理员能调用（权限点 fb_projects.edit）。
func (s *ManageService) CreateProject(ctx context.Context, in ProjectInput) (Project, error) {
	in, err := validateProjectInput(in)
	if err != nil {
		return Project{}, err
	}
	return s.repo.CreateProject(ctx, in)
}

// UpdateProject 整体保存项目。
func (s *ManageService) UpdateProject(ctx context.Context, id int64, in ProjectInput) (before, after Project, err error) {
	in, err = validateProjectInput(in)
	if err != nil {
		return Project{}, Project{}, err
	}
	before, err = s.repo.Project(ctx, id)
	if err != nil {
		return Project{}, Project{}, err
	}
	after, err = s.repo.UpdateProject(ctx, id, in)
	return before, after, err
}

// DeleteProject 删除项目。只允许删除还没有任何反馈的项目，避免误删社区内容。
func (s *ManageService) DeleteProject(ctx context.Context, id int64) (Project, error) {
	counts, err := s.repo.StatusCounts(ctx, id)
	if err != nil {
		return Project{}, err
	}
	var total int64
	for _, c := range counts {
		total += c.Total
	}
	if total > 0 {
		return Project{}, errs.ErrConflict.WithMessage("项目下已有反馈，不能删除；可以改为「已归档」")
	}
	return s.repo.DeleteProject(ctx, id)
}

// ProjectByID 供其他后台接口校验项目存在。
func (s *ManageService) ProjectByID(ctx context.Context, id int64) (Project, error) {
	return s.repo.Project(ctx, id)
}

// ---------- 成员 ----------

// ListMembers 返回项目成员。
func (s *ManageService) ListMembers(ctx context.Context, projectID int64, page, pageSize int) (Page[Member], error) {
	p, size, _, _, err := limitOffset(page, pageSize)
	if err != nil {
		return Page[Member]{}, err
	}
	if projectID <= 0 {
		return Page[Member]{}, badRequest("请选择项目")
	}
	list, total, err := s.repo.ListMembers(ctx, projectID, p, size)
	if err != nil {
		return Page[Member]{}, err
	}
	return Page[Member]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// SetMemberRole 调整成员角色或移除成员。
func (s *ManageService) SetMemberRole(ctx context.Context, projectID, userID int64, role MemberRole, remove bool) (Member, error) {
	switch role {
	case RoleMember, RoleOperator, RoleMaintainer, RoleOwner:
	default:
		return Member{}, badRequest("成员角色不合法")
	}
	// 成员行不存在时这次调用是「添加成员」。
	// 这是运营把已注册账号拉进项目的唯一入口：邀请码只在注册时核销，
	// 已有账号没有别的路进「仅邀请」项目（feedback/docs/11 第 13.3 节）。
	cur, err := s.repo.Member(ctx, projectID, userID)
	switch {
	case err == nil:
	case errors.Is(err, errs.ErrNotFound):
		if remove {
			// 本来就不是成员，没有可移除的东西
			return Member{}, errs.ErrNotFound
		}
		// 先确认这个社区账号真的存在，否则会插出一条指向幽灵用户的成员记录
		if _, uerr := s.repo.User(ctx, userID); uerr != nil {
			return Member{}, uerr
		}
	default:
		return Member{}, err
	}
	if remove && cur.Role == RoleOwner {
		return Member{}, errs.ErrConflict.WithMessage("不能移除项目所有者，请先转移所有权")
	}
	status := "active"
	if remove {
		status = "removed"
	}
	return s.repo.UpsertMember(ctx, projectID, userID, role, status)
}

// ---------- 邀请码 ----------

// ListInvites 返回邀请码列表。
func (s *ManageService) ListInvites(ctx context.Context, projectID int64, page, pageSize int) (Page[Invite], error) {
	p, size, _, _, err := limitOffset(page, pageSize)
	if err != nil {
		return Page[Invite]{}, err
	}
	list, total, err := s.repo.ListInvites(ctx, projectID, p, size)
	if err != nil {
		return Page[Invite]{}, err
	}
	return Page[Invite]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// CreateInvite 生成邀请码。明文只在这里返回一次，库里只存哈希。
func (s *ManageService) CreateInvite(ctx context.Context, in InviteInput) (Invite, string, error) {
	switch in.Role {
	case RoleMember, RoleOperator, RoleMaintainer:
	case "":
		in.Role = RoleMember
	default:
		return Invite{}, "", badRequest("邀请角色只能是成员、运营或维护者")
	}
	if in.MaxUses == 0 {
		in.MaxUses = 1
	}
	if in.MaxUses < 1 || in.MaxUses > 10000 {
		return Invite{}, "", badRequest("可用次数需在 1 到 10000 之间")
	}
	in.Note = cleanText(in.Note)
	if runeLen(in.Note) > 100 {
		return Invite{}, "", badRequest("备注最多 100 个字")
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(s.now()) {
		return Invite{}, "", badRequest("过期时间必须晚于当前时间")
	}
	if in.ProjectID > 0 {
		if _, err := s.repo.Project(ctx, in.ProjectID); err != nil {
			return Invite{}, "", err
		}
	}
	code, err := NewInviteCode()
	if err != nil {
		return Invite{}, "", err
	}
	hint := code
	if len(hint) > 4 {
		hint = hint[:4]
	}
	inv, err := s.repo.CreateInvite(ctx, HashInviteCode(code), hint, in)
	if err != nil {
		return Invite{}, "", err
	}
	return inv, code, nil
}

// RevokeInvite 作废邀请码。
func (s *ManageService) RevokeInvite(ctx context.Context, id int64) (Invite, error) {
	return s.repo.RevokeInvite(ctx, id)
}

// ---------- 动态 ----------

// ListPosts 返回后台动态列表。
func (s *ManageService) ListPosts(ctx context.Context, f PostFilter) (Page[Post], error) {
	p, size, _, _, err := limitOffset(f.Page, f.PageSize)
	if err != nil {
		return Page[Post]{}, err
	}
	switch f.Status {
	case "", "draft", "published", "hidden":
	default:
		return Page[Post]{}, badRequest("状态筛选不合法")
	}
	f.Page, f.PageSize = p, size
	list, total, err := s.repo.ListPosts(ctx, f)
	if err != nil {
		return Page[Post]{}, err
	}
	return Page[Post]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// CreatePost 新建动态。发布时自动写入发布时间。
func (s *ManageService) CreatePost(ctx context.Context, in PostInput) (Post, error) {
	in, err := validatePostInput(in)
	if err != nil {
		return Post{}, err
	}
	if _, err := s.repo.Project(ctx, in.ProjectID); err != nil {
		return Post{}, err
	}
	return s.repo.CreatePost(ctx, in, s.publishedAt(in.Status, nil))
}

// UpdatePost 整体保存动态。已发布过的动态保留原发布时间。
func (s *ManageService) UpdatePost(ctx context.Context, id int64, in PostInput) (before, after Post, err error) {
	in, err = validatePostInput(in)
	if err != nil {
		return Post{}, Post{}, err
	}
	before, err = s.repo.Post(ctx, id)
	if err != nil {
		return Post{}, Post{}, err
	}
	if in.ProjectID != before.ProjectID {
		if _, err := s.repo.Project(ctx, in.ProjectID); err != nil {
			return Post{}, Post{}, err
		}
	}
	after, err = s.repo.UpdatePost(ctx, id, in, s.publishedAt(in.Status, before.PublishedAt))
	return before, after, err
}

// DeletePost 删除动态。
func (s *ManageService) DeletePost(ctx context.Context, id int64) (Post, error) {
	return s.repo.DeletePost(ctx, id)
}

// publishedAt 决定要写入的发布时间：发布且从未发布过时取当前时间，否则沿用原值。
func (s *ManageService) publishedAt(status string, cur *time.Time) *time.Time {
	if status != "published" {
		return cur
	}
	if cur != nil {
		return cur
	}
	now := s.now()
	return &now
}

// ---------- 反馈分诊 ----------

// ListFeedback 返回后台反馈列表（可按状态、类型、优先级、可见性筛选）。
func (s *ManageService) ListFeedback(ctx context.Context, f FeedbackFilter) (Page[Feedback], error) {
	p, size, _, _, err := limitOffset(f.Page, f.PageSize)
	if err != nil {
		return Page[Feedback]{}, err
	}
	sort, err := sortKey(f.Sort, true)
	if err != nil {
		return Page[Feedback]{}, err
	}
	if f.Status != "" && !knownStatus(f.Status) {
		return Page[Feedback]{}, badRequest("状态筛选不合法")
	}
	if f.Kind != "" && !knownKind(f.Kind) {
		return Page[Feedback]{}, badRequest("类型筛选不合法")
	}
	f.Page, f.PageSize, f.Sort, f.Q = p, size, sort, cleanQuery(f.Q)
	list, total, err := s.repo.AdminFeedback(ctx, f)
	if err != nil {
		return Page[Feedback]{}, err
	}
	return Page[Feedback]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// FeedbackDetail 返回反馈详情与完整状态历史（含内部备注）。
func (s *ManageService) FeedbackDetail(ctx context.Context, id int64) (Feedback, []Event, error) {
	fb, err := s.repo.Feedback(ctx, id)
	if err != nil {
		return Feedback{}, nil, err
	}
	events, err := s.repo.Events(ctx, id)
	if err != nil {
		return Feedback{}, nil, err
	}
	return fb, events, nil
}

// Triage 分诊并流转状态：校验状态机与乐观锁，写状态历史，必要时记一次贡献。
func (s *ManageService) Triage(ctx context.Context, id int64, in TriageInput) (before, after Feedback, err error) {
	before, err = s.repo.Feedback(ctx, id)
	if err != nil {
		return Feedback{}, Feedback{}, err
	}
	in, err = validateTriage(in, before)
	if err != nil {
		return Feedback{}, Feedback{}, err
	}
	if in.DuplicateOf > 0 {
		main, err := s.repo.Feedback(ctx, in.DuplicateOf)
		if err != nil {
			return Feedback{}, Feedback{}, badRequest("主反馈不存在")
		}
		if main.ProjectID != before.ProjectID {
			return Feedback{}, Feedback{}, badRequest("只能与同一项目内的反馈合并")
		}
		if main.Status == StatusDuplicate {
			return Feedback{}, Feedback{}, errs.ErrConflict.WithMessage("主反馈本身已被标记为重复，请选择最终的主反馈")
		}
	}
	if in.Note == "" {
		in.Note = defaultNote(before.Status, in.Status)
	}
	after, err = s.repo.Transition(ctx, id, in, awardFor(before, in))
	return before, after, err
}

// Overview 返回按状态的反馈数量，供后台概览使用。
func (s *ManageService) Overview(ctx context.Context, projectID int64) ([]StatusCount, error) {
	return s.repo.StatusCounts(ctx, projectID)
}

// ---------- 社区成员 ----------

// ListUsers 返回社区用户列表。
func (s *ManageService) ListUsers(ctx context.Context, f UserFilter) (Page[User], error) {
	p, size, _, _, err := limitOffset(f.Page, f.PageSize)
	if err != nil {
		return Page[User]{}, err
	}
	switch f.Status {
	case "", UserActive, UserRestricted, UserSuspended:
	default:
		return Page[User]{}, badRequest("状态筛选不合法")
	}
	f.Page, f.PageSize, f.Q = p, size, cleanQuery(f.Q)
	list, total, err := s.repo.ListUsers(ctx, f)
	if err != nil {
		return Page[User]{}, err
	}
	return Page[User]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// SetUserStatus 停用或恢复社区账号。后台不能重设用户密码（feedback/docs/adr/0007）。
func (s *ManageService) SetUserStatus(ctx context.Context, id int64, status UserStatus) (before, after User, err error) {
	switch status {
	case UserActive, UserRestricted, UserSuspended:
	default:
		return User{}, User{}, badRequest("账号状态不合法")
	}
	before, err = s.repo.User(ctx, id)
	if err != nil {
		return User{}, User{}, err
	}
	after, err = s.repo.UpdateUserStatus(ctx, id, status)
	return before, after, err
}

// defaultNote 在运营没写公开说明时生成一句中性文案。
func defaultNote(from, to Status) string {
	if from == to {
		return ""
	}
	return "状态已更新为「" + StatusLabel(to) + "」"
}

// awardFor 决定本次流转是否记贡献：确认 +1，上线 +4；同一来源只记一次。
func awardFor(cur Feedback, in TriageInput) *Award {
	if cur.AuthorID == 0 {
		return nil
	}
	id := strconv.FormatInt(cur.ID, 10)
	switch {
	case in.Status == StatusTriaged && cur.Status == StatusSubmitted:
		return &Award{UserID: cur.AuthorID, ProjectID: cur.ProjectID, Kind: "triaged", Points: 1,
			Reason: "反馈被确认", SourceKey: "triaged:" + id}
	case in.Status == StatusShipped:
		return &Award{UserID: cur.AuthorID, ProjectID: cur.ProjectID, Kind: "shipped", Points: 4,
			Reason: "反馈已随版本上线", SourceKey: "shipped:" + id}
	}
	return nil
}
