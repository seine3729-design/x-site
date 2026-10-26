package feedback

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

// stubTopicRepo 只实现用到的方法；嵌入接口让未实现的方法在被误用时直接 panic，暴露测试假设。
type stubTopicRepo struct {
	TopicRepo
	topics  map[int64]Topic
	members map[int64]bool // topicID → 访问者是否是该项目成员
	answers map[int64]Answer
	created struct {
		topicID, userID, parentID int64
		body                      string
	}
	helpful struct {
		answerID, userID int64
		active           bool
	}
	publishedAt *time.Time
}

func newStubTopicRepo() *stubTopicRepo {
	return &stubTopicRepo{
		topics:  map[int64]Topic{},
		members: map[int64]bool{},
		answers: map[int64]Answer{},
	}
}

func (r *stubTopicRepo) VisibleTopic(_ context.Context, _ Viewer, id int64) (Topic, bool, error) {
	t, ok := r.topics[id]
	if !ok {
		return Topic{}, false, errs.ErrNotFound
	}
	return t, r.members[id], nil
}

func (r *stubTopicRepo) Topic(_ context.Context, id int64) (Topic, error) {
	t, ok := r.topics[id]
	if !ok {
		return Topic{}, errs.ErrNotFound
	}
	return t, nil
}

func (r *stubTopicRepo) Answer(_ context.Context, id int64) (Answer, error) {
	a, ok := r.answers[id]
	if !ok {
		return Answer{}, errs.ErrNotFound
	}
	return a, nil
}

func (r *stubTopicRepo) CreateAnswer(_ context.Context, topicID, userID, parentID int64, body string) (Answer, error) {
	r.created.topicID, r.created.userID, r.created.parentID, r.created.body = topicID, userID, parentID, body
	return Answer{ID: 99, TopicID: topicID, ParentID: parentID, Body: body, Status: "visible"}, nil
}

func (r *stubTopicRepo) SetHelpful(_ context.Context, answerID, userID int64, active bool) (Answer, error) {
	r.helpful.answerID, r.helpful.userID, r.helpful.active = answerID, userID, active
	a := r.answers[answerID]
	if active {
		a.HelpfulCount, a.HelpfulByMe = a.HelpfulCount+1, true
	}
	return a, nil
}

func (r *stubTopicRepo) CreateTopic(_ context.Context, in TopicInput, publishedAt *time.Time) (Topic, error) {
	r.publishedAt = publishedAt
	return Topic{ID: 1, Title: in.Title, Status: in.Status, PublishedAt: publishedAt}, nil
}

func (r *stubTopicRepo) UpdateTopic(_ context.Context, id int64, in TopicInput, publishedAt *time.Time) (Topic, error) {
	r.publishedAt = publishedAt
	t := r.topics[id]
	t.Title, t.Status, t.PublishedAt = in.Title, in.Status, publishedAt
	return t, nil
}

func (r *stubTopicRepo) DeleteTopic(_ context.Context, id int64) (Topic, error) {
	return r.topics[id], nil
}

var fixedNow = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func newTestTopicService(repo TopicRepo) *TopicService {
	s := NewTopicService(repo)
	s.now = func() time.Time { return fixedNow }
	return s
}

func activeUser() User { return User{ID: 7, Handle: "tester", Status: UserActive} }

// 征集中且未到截止时间才可回答。
func TestTopicAnswerable(t *testing.T) {
	past := fixedNow.Add(-time.Hour)
	future := fixedNow.Add(time.Hour)
	tests := []struct {
		name   string
		topic  Topic
		expect bool
	}{
		{"征集中且没有截止时间", Topic{Status: TopicOpen}, true},
		{"征集中且未到截止时间", Topic{Status: TopicOpen, EndsAt: &future}, true},
		{"征集中但已过截止时间", Topic{Status: TopicOpen, EndsAt: &past}, false},
		{"草稿", Topic{Status: TopicDraft}, false},
		{"已结束", Topic{Status: TopicClosed}, false},
		{"已归档", Topic{Status: TopicArchived}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.topic.Answerable(fixedNow); got != tt.expect {
				t.Fatalf("Answerable = %v，期望 %v", got, tt.expect)
			}
		})
	}
}

// 详情里的 can_answer：要登录，且仅成员议题要求已加入项目。
func TestTopicService_CanAnswer(t *testing.T) {
	tests := []struct {
		name       string
		visibility Visibility
		isMember   bool
		viewer     Viewer
		expect     bool
	}{
		{"匿名访问公开议题", VisibilityPublic, false, Viewer{}, false},
		{"登录用户可答公开议题", VisibilityPublic, false, Viewer{UserID: 7}, true},
		{"非成员不能答仅成员议题", VisibilityMembers, false, Viewer{UserID: 7}, false},
		{"成员可答仅成员议题", VisibilityMembers, true, Viewer{UserID: 7}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newStubTopicRepo()
			repo.topics[1] = Topic{ID: 1, Status: TopicOpen, Visibility: tt.visibility}
			repo.members[1] = tt.isMember
			got, err := newTestTopicService(repo).Topic(context.Background(), tt.viewer, 1)
			if err != nil {
				t.Fatal(err)
			}
			if got.CanAnswer != tt.expect {
				t.Fatalf("CanAnswer = %v，期望 %v", got.CanAnswer, tt.expect)
			}
		})
	}
}

// 非法 ID 不应打到仓储，直接 404。
func TestTopicService_TopicRejectsBadID(t *testing.T) {
	// 仓储是零值 stub：任何调用都会 panic，所以这里能证明没走到仓储
	s := newTestTopicService(&stubTopicRepo{})
	if _, err := s.Topic(context.Background(), Viewer{}, 0); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("err = %v，期望 ErrNotFound", err)
	}
}

func TestTopicService_Reply(t *testing.T) {
	ctx := context.Background()
	past := fixedNow.Add(-time.Hour)

	t.Run("被限制发言的账号不能回答", func(t *testing.T) {
		s := newTestTopicService(&stubTopicRepo{})
		u := activeUser()
		u.Status = UserRestricted
		if _, err := s.Reply(ctx, u, 1, 0, "这是一条回答"); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("err = %v，期望 ErrForbidden", err)
		}
	})

	t.Run("已结束的议题不能回答", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicClosed, Visibility: VisibilityPublic}
		if _, err := newTestTopicService(repo).Reply(ctx, activeUser(), 1, 0, "这是一条回答"); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("err = %v，期望 ErrConflict", err)
		}
	})

	t.Run("过了截止时间不能回答", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, Visibility: VisibilityPublic, EndsAt: &past}
		if _, err := newTestTopicService(repo).Reply(ctx, activeUser(), 1, 0, "这是一条回答"); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("err = %v，期望 ErrConflict", err)
		}
	})

	t.Run("非成员不能回答仅成员议题", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, Visibility: VisibilityMembers}
		if _, err := newTestTopicService(repo).Reply(ctx, activeUser(), 1, 0, "这是一条回答"); !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("err = %v，期望 ErrForbidden", err)
		}
	})

	t.Run("正文为空被拒绝且不写库", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, Visibility: VisibilityPublic}
		if _, err := newTestTopicService(repo).Reply(ctx, activeUser(), 1, 0, "  "); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("err = %v，期望 ErrBadRequest", err)
		}
		if repo.created.topicID != 0 {
			t.Fatal("校验失败却写了库")
		}
	})

	t.Run("顶层回答", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, Visibility: VisibilityPublic}
		a, err := newTestTopicService(repo).Reply(ctx, activeUser(), 1, 0, " 这是一条回答 ")
		if err != nil {
			t.Fatal(err)
		}
		if a.ParentID != 0 {
			t.Fatalf("ParentID = %d，期望 0", a.ParentID)
		}
		if repo.created.body != "这是一条回答" {
			t.Fatalf("写入正文 = %q，期望已去掉首尾空白", repo.created.body)
		}
		if repo.created.userID != 7 {
			t.Fatalf("写入作者 = %d，期望 7", repo.created.userID)
		}
	})

	t.Run("回复的回复被压平到同一个顶层回答下", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, Visibility: VisibilityPublic}
		repo.answers[10] = Answer{ID: 10, TopicID: 1, Status: "visible"}
		repo.answers[11] = Answer{ID: 11, TopicID: 1, ParentID: 10, Status: "visible"}
		if _, err := newTestTopicService(repo).Reply(ctx, activeUser(), 1, 11, "再补一句"); err != nil {
			t.Fatal(err)
		}
		if repo.created.parentID != 10 {
			t.Fatalf("ParentID = %d，期望压平到 10", repo.created.parentID)
		}
	})

	t.Run("不能跨议题回复", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, Visibility: VisibilityPublic}
		repo.answers[10] = Answer{ID: 10, TopicID: 2, Status: "visible"}
		if _, err := newTestTopicService(repo).Reply(ctx, activeUser(), 1, 10, "换个议题回复"); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("err = %v，期望 ErrBadRequest", err)
		}
	})

	t.Run("不能回复已隐藏的回答", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, Visibility: VisibilityPublic}
		repo.answers[10] = Answer{ID: 10, TopicID: 1, Status: "hidden"}
		if _, err := newTestTopicService(repo).Reply(ctx, activeUser(), 1, 10, "回复被隐藏的"); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("err = %v，期望 ErrConflict", err)
		}
	})

	t.Run("回复不存在的内容按 400 处理", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, Visibility: VisibilityPublic}
		if _, err := newTestTopicService(repo).Reply(ctx, activeUser(), 1, 777, "回复幽灵"); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("err = %v，期望 ErrBadRequest", err)
		}
	})
}

func TestTopicService_SetHelpful(t *testing.T) {
	ctx := context.Background()

	t.Run("已隐藏的回答不能点有道理", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.answers[10] = Answer{ID: 10, TopicID: 1, Status: "hidden"}
		if _, err := newTestTopicService(repo).SetHelpful(ctx, activeUser(), 10, true); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("err = %v，期望 ErrNotFound", err)
		}
	})

	// 防止借回答 ID 给不可见议题下的内容点赞
	t.Run("所属议题不可见时拒绝", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.answers[10] = Answer{ID: 10, TopicID: 404, Status: "visible"}
		if _, err := newTestTopicService(repo).SetHelpful(ctx, activeUser(), 10, true); !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("err = %v，期望 ErrNotFound", err)
		}
		if repo.helpful.answerID != 0 {
			t.Fatal("议题不可见却写了库")
		}
	})

	t.Run("可见议题下正常点赞", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, Visibility: VisibilityPublic}
		repo.answers[10] = Answer{ID: 10, TopicID: 1, Status: "visible"}
		a, err := newTestTopicService(repo).SetHelpful(ctx, activeUser(), 10, true)
		if err != nil {
			t.Fatal(err)
		}
		if !a.HelpfulByMe || a.HelpfulCount != 1 {
			t.Fatalf("结果 = %+v，期望已点赞且计数为 1", a)
		}
		if repo.helpful.userID != 7 || !repo.helpful.active {
			t.Fatalf("写入参数 = %+v", repo.helpful)
		}
	})
}

// 已经有回答的议题只能归档，不能删除：否则结论连带回答一起消失。
func TestTopicService_DeleteTopic(t *testing.T) {
	ctx := context.Background()
	repo := newStubTopicRepo()
	repo.topics[1] = Topic{ID: 1, AnswerCount: 3}
	repo.topics[2] = Topic{ID: 2, AnswerCount: 0}
	s := newTestTopicService(repo)

	if _, err := s.DeleteTopic(ctx, 1); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("err = %v，期望 ErrConflict", err)
	}
	if _, err := s.DeleteTopic(ctx, 2); err != nil {
		t.Fatalf("没有回答的议题应允许删除，实际 %v", err)
	}
}

// 发布时间：草稿不写，首次发布写当前时间，已发布过的保留原值。
func TestTopicService_PublishedAt(t *testing.T) {
	ctx := context.Background()
	old := fixedNow.Add(-72 * time.Hour)
	in := TopicInput{ProjectID: 1, Title: "要不要加夜间模式"}

	t.Run("草稿不写发布时间", func(t *testing.T) {
		repo := newStubTopicRepo()
		in := in
		in.Status = TopicDraft
		if _, err := newTestTopicService(repo).CreateTopic(ctx, in); err != nil {
			t.Fatal(err)
		}
		if repo.publishedAt != nil {
			t.Fatalf("publishedAt = %v，期望 nil", repo.publishedAt)
		}
	})

	t.Run("首次发布写当前时间", func(t *testing.T) {
		repo := newStubTopicRepo()
		in := in
		in.Status = TopicOpen
		if _, err := newTestTopicService(repo).CreateTopic(ctx, in); err != nil {
			t.Fatal(err)
		}
		if repo.publishedAt == nil || !repo.publishedAt.Equal(fixedNow) {
			t.Fatalf("publishedAt = %v，期望 %v", repo.publishedAt, fixedNow)
		}
	})

	t.Run("已发布过的保留原发布时间", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, PublishedAt: &old}
		in := in
		in.Status = TopicClosed
		if _, _, err := newTestTopicService(repo).UpdateTopic(ctx, 1, in); err != nil {
			t.Fatal(err)
		}
		if repo.publishedAt == nil || !repo.publishedAt.Equal(old) {
			t.Fatalf("publishedAt = %v，期望保留 %v", repo.publishedAt, old)
		}
	})

	t.Run("草稿改回时保留已有发布时间", func(t *testing.T) {
		repo := newStubTopicRepo()
		repo.topics[1] = Topic{ID: 1, Status: TopicOpen, PublishedAt: &old}
		in := in
		in.Status = TopicDraft
		if _, _, err := newTestTopicService(repo).UpdateTopic(ctx, 1, in); err != nil {
			t.Fatal(err)
		}
		if repo.publishedAt == nil || !repo.publishedAt.Equal(old) {
			t.Fatalf("publishedAt = %v，期望保留 %v", repo.publishedAt, old)
		}
	})
}

// 前台与后台的状态筛选范围不同：前台不接受 draft/archived，避免借筛选探测未发布内容。
func TestTopicService_StatusFilter(t *testing.T) {
	ctx := context.Background()
	s := newTestTopicService(&stubTopicRepo{})

	for _, status := range []TopicStatus{TopicDraft, TopicArchived, "whatever"} {
		if _, err := s.Topics(ctx, Viewer{}, TopicFilter{Status: status}); !errors.Is(err, errs.ErrBadRequest) {
			t.Errorf("前台筛选 %q：err = %v，期望 ErrBadRequest", status, err)
		}
	}
	if _, err := s.AdminTopics(ctx, TopicFilter{Status: "whatever"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Errorf("后台筛选非法状态：err = %v，期望 ErrBadRequest", err)
	}
}

// 回答状态只能是 visible / hidden。
func TestTopicService_SetAnswerStatus(t *testing.T) {
	s := newTestTopicService(&stubTopicRepo{})
	if _, _, err := s.SetAnswerStatus(context.Background(), 1, "deleted"); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("err = %v，期望 ErrBadRequest", err)
	}
}

func TestValidateAnswerBody(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"正常", "我觉得应该先做夜间模式", true},
		{"去首尾空白后仍够长", "  可以  ", true},
		{"空", "   ", false},
		{"只有一个字", "好", false},
		{"超长", strings.Repeat("字", 5001), false},
		{"刚好 5000 字", strings.Repeat("字", 5000), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateAnswerBody(tt.in)
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v，期望 ok=%v", err, tt.ok)
			}
		})
	}
}

func TestValidateTopicInput(t *testing.T) {
	base := TopicInput{ProjectID: 1, Title: "要不要加夜间模式"}

	t.Run("补默认值", func(t *testing.T) {
		got, err := validateTopicInput(base)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != TopicDraft {
			t.Errorf("Status = %q，期望默认草稿", got.Status)
		}
		if got.Visibility != VisibilityPublic {
			t.Errorf("Visibility = %q，期望默认公开", got.Visibility)
		}
	})

	t.Run("不接受 private 可见性", func(t *testing.T) {
		in := base
		in.Visibility = VisibilityPrivate
		if _, err := validateTopicInput(in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("err = %v，期望 ErrBadRequest", err)
		}
	})

	t.Run("必须选项目", func(t *testing.T) {
		in := base
		in.ProjectID = 0
		if _, err := validateTopicInput(in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("err = %v，期望 ErrBadRequest", err)
		}
	})

	t.Run("标题不能为空", func(t *testing.T) {
		in := base
		in.Title = "   "
		if _, err := validateTopicInput(in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("err = %v，期望 ErrBadRequest", err)
		}
	})
}

func TestTopicLabel(t *testing.T) {
	for _, s := range []TopicStatus{TopicDraft, TopicOpen, TopicClosed, TopicArchived} {
		if TopicLabel(s) == string(s) {
			t.Errorf("%q 缺少中文文案", s)
		}
	}
	if got := TopicLabel("unknown"); got != "unknown" {
		t.Errorf("未知状态应原样返回，实际 %q", got)
	}
}
