package feedback

import (
	"context"
	"time"

	"xsite/service/internal/errs"
)

// TopicService 是议题区的读写能力：前台列表/详情/回答/「有道理」，以及后台的议题与回答管理。
// 可见性过滤在 SQL 中完成，不在这里事后筛。
type TopicService struct {
	repo TopicRepo
	now  func() time.Time
}

func NewTopicService(repo TopicRepo) *TopicService {
	return &TopicService{repo: repo, now: time.Now}
}

// ---------- 前台 ----------

// Topics 返回访问者可见的议题列表：征集中优先、置顶优先，再按发布时间。
func (s *TopicService) Topics(ctx context.Context, v Viewer, f TopicFilter) (Page[Topic], error) {
	p, size, _, _, err := limitOffset(f.Page, f.PageSize)
	if err != nil {
		return Page[Topic]{}, err
	}
	switch f.Status {
	case "", TopicOpen, TopicClosed:
	default:
		return Page[Topic]{}, badRequest("议题状态筛选不合法")
	}
	f.Page, f.PageSize = p, size
	list, total, err := s.repo.VisibleTopics(ctx, v, f)
	if err != nil {
		return Page[Topic]{}, err
	}
	return Page[Topic]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// Topic 返回议题详情。查不到与无权查看都返回 404，不暴露仅成员议题是否存在。
func (s *TopicService) Topic(ctx context.Context, v Viewer, id int64) (Topic, error) {
	if id <= 0 {
		return Topic{}, errs.ErrNotFound
	}
	t, isMember, err := s.repo.VisibleTopic(ctx, v, id)
	if err != nil {
		return Topic{}, err
	}
	// 仅成员议题要求已加入项目；公开议题任何登录用户都能回答
	t.CanAnswer = t.Answerable(s.now()) && v.LoggedIn() && (t.Visibility == VisibilityPublic || isMember)
	return t, nil
}

// Answers 返回一个议题的回答：精选优先、再按「有道理」数。
func (s *TopicService) Answers(ctx context.Context, v Viewer, topicID int64, page, pageSize int) (Page[Answer], error) {
	p, size, _, _, err := limitOffset(page, pageSize)
	if err != nil {
		return Page[Answer]{}, err
	}
	// 先确认议题本身可见，否则不能通过回答列表探测内容
	if _, _, err := s.repo.VisibleTopic(ctx, v, topicID); err != nil {
		return Page[Answer]{}, err
	}
	list, total, err := s.repo.VisibleAnswers(ctx, v, topicID, p, size)
	if err != nil {
		return Page[Answer]{}, err
	}
	return Page[Answer]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// Reply 发布一条回答或回复。parentID 为 0 表示顶层回答。
func (s *TopicService) Reply(ctx context.Context, u User, topicID, parentID int64, body string) (Answer, error) {
	if u.Status != UserActive {
		return Answer{}, errs.ErrForbidden.WithMessage("当前账号被限制发言，如有疑问请联系管理员")
	}
	body, err := validateAnswerBody(body)
	if err != nil {
		return Answer{}, err
	}
	t, isMember, err := s.repo.VisibleTopic(ctx, Viewer{UserID: u.ID}, topicID)
	if err != nil {
		return Answer{}, err
	}
	if !t.Answerable(s.now()) {
		return Answer{}, errs.ErrConflict.WithMessage("这个议题已经结束征集")
	}
	if t.Visibility == VisibilityMembers && !isMember {
		return Answer{}, errs.ErrForbidden.WithMessage("请先加入该项目再回答")
	}
	if parentID > 0 {
		parent, err := s.repo.Answer(ctx, parentID)
		if err != nil {
			return Answer{}, badRequest("要回复的内容不存在")
		}
		if parent.TopicID != topicID {
			return Answer{}, badRequest("只能回复同一个议题下的回答")
		}
		if parent.Status != "visible" {
			return Answer{}, errs.ErrConflict.WithMessage("这条回答已被隐藏，不能回复")
		}
		// 只允许一层回复：回复的回复挂到同一个顶层回答下
		if parent.ParentID > 0 {
			parentID = parent.ParentID
		}
	}
	return s.repo.CreateAnswer(ctx, topicID, u.ID, parentID, body)
}

// SetHelpful 设置或取消「有道理」，重复调用结果一致。
func (s *TopicService) SetHelpful(ctx context.Context, u User, answerID int64, active bool) (Answer, error) {
	if u.Status != UserActive {
		return Answer{}, errs.ErrForbidden.WithMessage("当前账号被限制操作")
	}
	a, err := s.repo.Answer(ctx, answerID)
	if err != nil {
		return Answer{}, err
	}
	if a.Status != "visible" {
		return Answer{}, errs.ErrNotFound
	}
	// 确认所属议题对该用户可见，避免借回答 ID 给不可见内容点赞
	if _, _, err := s.repo.VisibleTopic(ctx, Viewer{UserID: u.ID}, a.TopicID); err != nil {
		return Answer{}, err
	}
	return s.repo.SetHelpful(ctx, answerID, u.ID, active)
}

// ---------- 后台 ----------

// AdminTopics 返回后台议题列表。
func (s *TopicService) AdminTopics(ctx context.Context, f TopicFilter) (Page[Topic], error) {
	p, size, _, _, err := limitOffset(f.Page, f.PageSize)
	if err != nil {
		return Page[Topic]{}, err
	}
	switch f.Status {
	case "", TopicDraft, TopicOpen, TopicClosed, TopicArchived:
	default:
		return Page[Topic]{}, badRequest("议题状态筛选不合法")
	}
	f.Page, f.PageSize, f.Q = p, size, cleanQuery(f.Q)
	list, total, err := s.repo.ListTopics(ctx, f)
	if err != nil {
		return Page[Topic]{}, err
	}
	return Page[Topic]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// CreateTopic 新建议题。状态设为非草稿时自动写入发布时间。
func (s *TopicService) CreateTopic(ctx context.Context, in TopicInput) (Topic, error) {
	in, err := validateTopicInput(in)
	if err != nil {
		return Topic{}, err
	}
	return s.repo.CreateTopic(ctx, in, s.publishedAt(in.Status, nil))
}

// UpdateTopic 整体保存议题。已发布过的保留原发布时间。
func (s *TopicService) UpdateTopic(ctx context.Context, id int64, in TopicInput) (before, after Topic, err error) {
	in, err = validateTopicInput(in)
	if err != nil {
		return Topic{}, Topic{}, err
	}
	before, err = s.repo.Topic(ctx, id)
	if err != nil {
		return Topic{}, Topic{}, err
	}
	after, err = s.repo.UpdateTopic(ctx, id, in, s.publishedAt(in.Status, before.PublishedAt))
	return before, after, err
}

// DeleteTopic 删除议题。已经有回答的议题不允许删除，改为「已归档」保留结论。
func (s *TopicService) DeleteTopic(ctx context.Context, id int64) (Topic, error) {
	t, err := s.repo.Topic(ctx, id)
	if err != nil {
		return Topic{}, err
	}
	if t.AnswerCount > 0 {
		return Topic{}, errs.ErrConflict.WithMessage("这个议题已经有回答，不能删除；可以改为「已归档」")
	}
	return s.repo.DeleteTopic(ctx, id)
}

// TopicByID 供后台读取单个议题。
func (s *TopicService) TopicByID(ctx context.Context, id int64) (Topic, error) {
	return s.repo.Topic(ctx, id)
}

// AdminAnswers 返回一个议题下的全部回答（含已隐藏）。
func (s *TopicService) AdminAnswers(ctx context.Context, topicID int64, page, pageSize int) (Page[Answer], error) {
	p, size, _, _, err := limitOffset(page, pageSize)
	if err != nil {
		return Page[Answer]{}, err
	}
	if topicID <= 0 {
		return Page[Answer]{}, badRequest("请选择议题")
	}
	list, total, err := s.repo.AdminAnswers(ctx, topicID, p, size)
	if err != nil {
		return Page[Answer]{}, err
	}
	return Page[Answer]{List: list, Total: total, Page: p, PageSize: size}, nil
}

// SetFeatured 标记或取消「精选」回答。
func (s *TopicService) SetFeatured(ctx context.Context, id int64, featured bool) (before, after Answer, err error) {
	before, err = s.repo.Answer(ctx, id)
	if err != nil {
		return Answer{}, Answer{}, err
	}
	after, err = s.repo.SetAnswerFeatured(ctx, id, featured)
	return before, after, err
}

// SetAnswerStatus 隐藏或恢复一条回答；隐藏后前台不再显示，议题回答数同步重算。
func (s *TopicService) SetAnswerStatus(ctx context.Context, id int64, status string) (before, after Answer, err error) {
	switch status {
	case "visible", "hidden":
	default:
		return Answer{}, Answer{}, badRequest("回答状态不合法")
	}
	before, err = s.repo.Answer(ctx, id)
	if err != nil {
		return Answer{}, Answer{}, err
	}
	after, err = s.repo.SetAnswerStatus(ctx, id, status)
	return before, after, err
}

// publishedAt 决定要写入的发布时间：非草稿且从未发布过时取当前时间，否则沿用原值。
func (s *TopicService) publishedAt(status TopicStatus, cur *time.Time) *time.Time {
	if status == TopicDraft {
		return cur
	}
	if cur != nil {
		return cur
	}
	now := s.now()
	return &now
}
