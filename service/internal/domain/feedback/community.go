package feedback

import (
	"context"
	"errors"

	"xsite/service/internal/errs"
)

// CommunityService 是前台（H5）用到的读写能力。可见性过滤在 SQL 中完成，不在这里事后筛。
type CommunityService struct {
	repo Repo
}

func NewCommunityService(repo Repo) *CommunityService {
	return &CommunityService{repo: repo}
}

// Projects 返回访问者可见的项目目录。
func (s *CommunityService) Projects(ctx context.Context, v Viewer, page, pageSize int) (Page[ProjectView], error) {
	p, size, _, _, err := limitOffset(page, pageSize)
	if err != nil {
		return Page[ProjectView]{}, err
	}
	list, total, err := s.repo.VisibleProjects(ctx, v, p, size)
	if err != nil {
		return Page[ProjectView]{}, err
	}
	return Page[ProjectView]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// Project 返回项目详情；无权查看时返回 404，不暴露项目是否存在。
func (s *CommunityService) Project(ctx context.Context, v Viewer, slug string) (ProjectView, error) {
	return s.repo.VisibleProject(ctx, v, slug)
}

// Feed 返回动态流；projectID 为 0 表示全部可见项目。
func (s *CommunityService) Feed(ctx context.Context, v Viewer, projectID int64, page, pageSize int) (Page[Post], error) {
	p, size, _, _, err := limitOffset(page, pageSize)
	if err != nil {
		return Page[Post]{}, err
	}
	list, total, err := s.repo.VisiblePosts(ctx, v, projectID, p, size)
	if err != nil {
		return Page[Post]{}, err
	}
	return Page[Post]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// Post 返回一条动态。
func (s *CommunityService) Post(ctx context.Context, v Viewer, id int64) (Post, error) {
	if id <= 0 {
		return Post{}, errs.ErrNotFound
	}
	return s.repo.VisiblePost(ctx, v, id)
}

// ListFeedback 返回访问者可见的反馈列表。
func (s *CommunityService) ListFeedback(ctx context.Context, v Viewer, f FeedbackFilter) (Page[Feedback], error) {
	p, size, _, _, err := limitOffset(f.Page, f.PageSize)
	if err != nil {
		return Page[Feedback]{}, err
	}
	sort, err := sortKey(f.Sort, false)
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
	list, total, err := s.repo.VisibleFeedback(ctx, v, f)
	if err != nil {
		return Page[Feedback]{}, err
	}
	return Page[Feedback]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// FeedbackDetail 返回反馈详情与公开的状态历史（不含内部备注）。
func (s *CommunityService) FeedbackDetail(ctx context.Context, v Viewer, slug string, number int64) (Feedback, []Event, error) {
	if number <= 0 {
		return Feedback{}, nil, errs.ErrNotFound
	}
	// 查不到与无权查看都返回 404，避免探测私密反馈
	fb, err := s.repo.FeedbackByKey(ctx, v, slug, number)
	if err != nil {
		return Feedback{}, nil, err
	}
	events, err := s.repo.Events(ctx, fb.ID)
	if err != nil {
		return Feedback{}, nil, err
	}
	public := make([]Event, 0, len(events))
	for _, e := range events {
		e.InternalNote = "" // 内部备注只出现在后台响应
		public = append(public, e)
	}
	return fb, public, nil
}

// Submit 提交反馈。项目必须允许提交；成员可见与私密反馈要求已加入项目。
func (s *CommunityService) Submit(ctx context.Context, u User, in SubmitInput) (Feedback, error) {
	if u.Status != UserActive {
		return Feedback{}, errs.ErrForbidden.WithMessage("当前账号被限制提交，如有疑问请联系管理员")
	}
	in, err := validateSubmit(in)
	if err != nil {
		return Feedback{}, err
	}
	view, err := s.repo.VisibleProject(ctx, Viewer{UserID: u.ID}, in.ProjectSlug)
	if err != nil {
		return Feedback{}, err
	}
	if !view.AcceptsFeedback() {
		return Feedback{}, errs.ErrConflict.WithMessage("该项目暂不接受新反馈")
	}
	if !view.Joined {
		if view.JoinMode != JoinOpen {
			return Feedback{}, errs.ErrForbidden.WithMessage("请先加入该项目再提交反馈")
		}
		if _, err := s.repo.UpsertMember(ctx, view.ID, u.ID, RoleMember, "active"); err != nil {
			return Feedback{}, err
		}
	}
	return s.repo.CreateFeedback(ctx, view.ID, u.ID, in)
}

// Join 加入项目。只有 open 项目可以自助加入，其余要用邀请码。
func (s *CommunityService) Join(ctx context.Context, u User, slug string) (ProjectView, error) {
	view, err := s.repo.VisibleProject(ctx, Viewer{UserID: u.ID}, slug)
	if err != nil {
		return ProjectView{}, err
	}
	if view.Joined {
		return view, nil
	}
	if view.JoinMode == JoinInviteOnly || view.JoinMode == JoinClosed {
		return ProjectView{}, errs.ErrForbidden.WithMessage("该项目只能通过邀请码加入，或联系项目团队")
	}
	if view.Status == ProjectPaused || view.Status == ProjectArchived {
		return ProjectView{}, errs.ErrConflict.WithMessage("该项目暂停加入")
	}
	// 已经申请过就不用再写一次，直接把当前状态返回给前台
	if view.MemberStatus == "pending" {
		return view, nil
	}
	// 需审核的项目先落成待审，由运营在后台通过（把成员改成 active）或拒绝（移除）
	status := "active"
	if view.JoinMode == JoinApplication {
		status = "pending"
	}
	if _, err := s.repo.UpsertMember(ctx, view.ID, u.ID, RoleMember, status); err != nil {
		return ProjectView{}, err
	}
	return s.repo.VisibleProject(ctx, Viewer{UserID: u.ID}, slug)
}

// Vote 设置或取消对一条反馈的投票（同一用户一票，重复调用结果一致）。
func (s *CommunityService) Vote(ctx context.Context, u User, slug string, number int64, active bool) (Feedback, error) {
	if u.Status != UserActive {
		return Feedback{}, errs.ErrForbidden.WithMessage("当前账号被限制操作")
	}
	fb, err := s.repo.FeedbackByKey(ctx, Viewer{UserID: u.ID}, slug, number)
	if err != nil {
		return Feedback{}, err
	}
	if _, err := s.repo.SetVote(ctx, fb.ID, u.ID, active); err != nil {
		return Feedback{}, err
	}
	return s.repo.FeedbackByKey(ctx, Viewer{UserID: u.ID}, slug, number)
}

// MyFeedback 返回当前用户提交的反馈。
func (s *CommunityService) MyFeedback(ctx context.Context, u User, page, pageSize int) (Page[Feedback], error) {
	return s.ListFeedback(ctx, Viewer{UserID: u.ID}, FeedbackFilter{AuthorID: u.ID, Page: page, PageSize: pageSize})
}

// Leaderboard 返回贡献榜（登录可见，已隐藏等级的用户不出现）。
func (s *CommunityService) Leaderboard(ctx context.Context, limit int) ([]LeaderboardEntry, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.Leaderboard(ctx, limit)
}

// MyProjects 返回当前用户已加入的项目（取可见项目中 joined 的部分）。
func (s *CommunityService) MyProjects(ctx context.Context, v Viewer) ([]ProjectView, error) {
	if !v.LoggedIn() {
		return nil, errs.ErrNeedLogin
	}
	all, _, err := s.repo.VisibleProjects(ctx, v, 1, maxPageSize)
	if err != nil {
		return nil, err
	}
	mine := make([]ProjectView, 0, len(all))
	for _, p := range all {
		if p.Joined {
			mine = append(mine, p)
		}
	}
	return mine, nil
}

func knownStatus(s Status) bool {
	_, ok := allowedTransitions[s]
	return ok
}

func knownKind(k Kind) bool {
	switch k {
	case KindBug, KindExperience, KindFeature, KindContent, KindAccess, KindCompetitor, KindOther:
		return true
	}
	return false
}

// IsNotFound 便于 handler 区分「无权/不存在」与其他错误。
func IsNotFound(err error) bool { return errors.Is(err, errs.ErrNotFound) }
