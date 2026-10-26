package moderation

import (
	"context"
	"errors"
	"testing"

	"uuid"

	"xsite/service/internal/errs"
)

type fakeCommentRepo struct {
	created   NewComment
	owner     Author
	episodeID int64
	deleted   bool
	softDel   int64
	likedC    int64
	likedU    int64
	createErr error
}

func (r *fakeCommentRepo) Create(_ context.Context, in NewComment) (Comment, error) {
	if r.createErr != nil {
		return Comment{}, r.createErr
	}
	r.created = in
	return Comment{ID: 1, EpisodeID: in.EpisodeID, ParentID: in.ParentID, Content: in.Content, Status: in.Status}, nil
}
func (r *fakeCommentRepo) List(context.Context, int64, bool, string, int) ([]CommentView, string, error) {
	return []CommentView{{ID: 9, Content: "你好"}}, "", nil
}
func (r *fakeCommentRepo) AuthorOf(context.Context, int64) (Author, int64, bool, error) {
	return r.owner, r.episodeID, r.deleted, nil
}
func (r *fakeCommentRepo) SoftDelete(_ context.Context, id int64) error { r.softDel = id; return nil }
func (r *fakeCommentRepo) Like(_ context.Context, commentID, userID int64) (bool, error) {
	r.likedC, r.likedU = commentID, userID
	return true, nil
}

type fakeEval struct{ d Decision }

func (f fakeEval) Evaluate(text string) Decision {
	if f.d.Text == "" {
		return Decision{Text: text}
	}
	return f.d
}

type fakeFeatures struct{ off map[string]bool }

func (f fakeFeatures) Enabled(_ context.Context, name string) (bool, error) { return !f.off[name], nil }

type fakePolicy struct{ pre bool }

func (f fakePolicy) PreModeration(context.Context) (bool, error) { return f.pre, nil }

type fakeUsers struct{ muted bool }

func (f fakeUsers) Muted(context.Context, int64) (bool, error) { return f.muted, nil }

type fakeLimiter struct {
	dup  bool
	keys []string
}

func (l *fakeLimiter) FirstPost(_ context.Context, key string) (bool, error) {
	l.keys = append(l.keys, key)
	return !l.dup, nil
}

func newCommentSvc(repo *fakeCommentRepo, eval Evaluator, feat Features, pol Policy, usr Users, lim Limiter) *CommentService {
	if eval == nil {
		eval = fakeEval{}
	}
	if feat == nil {
		feat = fakeFeatures{}
	}
	if pol == nil {
		pol = fakePolicy{}
	}
	if usr == nil {
		usr = fakeUsers{}
	}
	if lim == nil {
		lim = &fakeLimiter{}
	}
	return NewCommentService(repo, eval, feat, pol, usr, lim, nil)
}

func userAuthor() Author  { return Author{UserID: 7} }
func guestAuthor() Author { return Author{GuestID: uuid.NewV7()} }

// fakeNotifier 记录投递的社区通知。
type fakeNotifier struct {
	calls []notifyCall
	err   error
}
type notifyCall struct {
	userID, commentID, episodeID int64
	typ                          string
}

func (f *fakeNotifier) Notify(_ context.Context, recipientUserID int64, typ string, commentID, episodeID int64) error {
	f.calls = append(f.calls, notifyCall{userID: recipientUserID, typ: typ, commentID: commentID, episodeID: episodeID})
	return f.err
}

func TestComment_NotifyReply(t *testing.T) {
	// 回复注册用户的评论 → 通知父评论作者
	repo := &fakeCommentRepo{owner: Author{UserID: 8}, episodeID: 5}
	nf := &fakeNotifier{}
	svc := NewCommentService(repo, fakeEval{}, fakeFeatures{}, fakePolicy{}, fakeUsers{}, &fakeLimiter{}, nf)
	if _, err := svc.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, ParentID: 3, Content: "回复你"}); err != nil {
		t.Fatal(err)
	}
	if len(nf.calls) != 1 || nf.calls[0].userID != 8 || nf.calls[0].typ != NotifyReply {
		t.Fatalf("应通知父评论作者 8：%+v", nf.calls)
	}

	// 顶层评论（无 parent）不通知
	nf2 := &fakeNotifier{}
	svc2 := NewCommentService(&fakeCommentRepo{owner: Author{UserID: 8}}, fakeEval{}, fakeFeatures{}, fakePolicy{}, fakeUsers{}, &fakeLimiter{}, nf2)
	_, _ = svc2.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, Content: "顶层"})
	if len(nf2.calls) != 0 {
		t.Fatalf("顶层评论不应通知：%+v", nf2.calls)
	}

	// 父评论作者是游客 → 不通知（游客无站内信）
	nf3 := &fakeNotifier{}
	svc3 := NewCommentService(&fakeCommentRepo{owner: guestAuthor()}, fakeEval{}, fakeFeatures{}, fakePolicy{}, fakeUsers{}, &fakeLimiter{}, nf3)
	_, _ = svc3.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, ParentID: 3, Content: "回复游客"})
	if len(nf3.calls) != 0 {
		t.Fatalf("回复游客评论不应通知：%+v", nf3.calls)
	}

	// 自己回复自己 → 不通知
	nf4 := &fakeNotifier{}
	svc4 := NewCommentService(&fakeCommentRepo{owner: Author{UserID: 7}}, fakeEval{}, fakeFeatures{}, fakePolicy{}, fakeUsers{}, &fakeLimiter{}, nf4)
	_, _ = svc4.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, ParentID: 3, Content: "自回复"})
	if len(nf4.calls) != 0 {
		t.Fatalf("自回复不应通知：%+v", nf4.calls)
	}

	// 回复进了待审（pending）→ 不通知（尚未可见）
	nf5 := &fakeNotifier{}
	svc5 := NewCommentService(&fakeCommentRepo{owner: Author{UserID: 8}}, fakeEval{}, fakeFeatures{}, fakePolicy{pre: true}, fakeUsers{}, &fakeLimiter{}, nf5)
	_, _ = svc5.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, ParentID: 3, Content: "待审回复"})
	if len(nf5.calls) != 0 {
		t.Fatalf("待审回复不应通知：%+v", nf5.calls)
	}
}

func TestComment_NotifyLike(t *testing.T) {
	// 点赞注册用户的评论 → 通知作者
	repo := &fakeCommentRepo{owner: Author{UserID: 8}, episodeID: 5}
	nf := &fakeNotifier{}
	svc := NewCommentService(repo, fakeEval{}, fakeFeatures{}, fakePolicy{}, fakeUsers{}, &fakeLimiter{}, nf)
	if err := svc.Like(context.Background(), 7, 9); err != nil {
		t.Fatal(err)
	}
	if len(nf.calls) != 1 || nf.calls[0].userID != 8 || nf.calls[0].typ != NotifyLike || nf.calls[0].commentID != 9 || nf.calls[0].episodeID != 5 {
		t.Fatalf("应通知被赞评论作者 8：%+v", nf.calls)
	}

	// 自赞 → 不通知
	nf2 := &fakeNotifier{}
	svc2 := NewCommentService(&fakeCommentRepo{owner: Author{UserID: 7}}, fakeEval{}, fakeFeatures{}, fakePolicy{}, fakeUsers{}, &fakeLimiter{}, nf2)
	_ = svc2.Like(context.Background(), 7, 9)
	if len(nf2.calls) != 0 {
		t.Fatalf("自赞不应通知：%+v", nf2.calls)
	}

	// 被赞评论作者是游客 → 不通知
	nf3 := &fakeNotifier{}
	svc3 := NewCommentService(&fakeCommentRepo{owner: guestAuthor()}, fakeEval{}, fakeFeatures{}, fakePolicy{}, fakeUsers{}, &fakeLimiter{}, nf3)
	_ = svc3.Like(context.Background(), 7, 9)
	if len(nf3.calls) != 0 {
		t.Fatalf("点赞游客评论不应通知：%+v", nf3.calls)
	}
}

func TestComment_CreateVisible(t *testing.T) {
	repo := &fakeCommentRepo{}
	svc := newCommentSvc(repo, nil, nil, nil, nil, nil)
	c, err := svc.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, Content: "  好评 \n"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != StatusVisible || repo.created.Content != "好评" {
		t.Fatalf("应清洗后可见：%+v", repo.created)
	}
}

func TestComment_Blocked(t *testing.T) {
	svc := newCommentSvc(&fakeCommentRepo{}, fakeEval{d: Decision{Blocked: true, Text: "x"}}, nil, nil, nil, nil)
	if _, err := svc.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, Content: "坏话"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("命中 block 应 400，得到 %v", err)
	}
}

func TestComment_Mask(t *testing.T) {
	repo := &fakeCommentRepo{}
	svc := newCommentSvc(repo, fakeEval{d: Decision{Text: "这是***"}}, nil, nil, nil, nil)
	c, err := svc.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, Content: "这是广告"})
	if err != nil || c.Status != StatusVisible || repo.created.Content != "这是***" {
		t.Fatalf("mask 后可见、存替换文本：%+v %v", repo.created, err)
	}
}

func TestComment_ReviewAndPreModeration(t *testing.T) {
	// 命中 review → pending
	repo := &fakeCommentRepo{}
	svc := newCommentSvc(repo, fakeEval{d: Decision{Review: true, Text: "x"}}, nil, nil, nil, nil)
	if c, _ := svc.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, Content: "x"}); c.Status != StatusPending {
		t.Fatalf("命中 review 应 pending，得到 %s", c.Status)
	}
	// 全局先审后显 → pending
	repo2 := &fakeCommentRepo{}
	svc2 := newCommentSvc(repo2, nil, nil, fakePolicy{pre: true}, nil, nil)
	if c, _ := svc2.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, Content: "正常"}); c.Status != StatusPending {
		t.Fatalf("先审后显应 pending，得到 %s", c.Status)
	}
}

func TestComment_Muted(t *testing.T) {
	svc := newCommentSvc(&fakeCommentRepo{}, nil, nil, nil, fakeUsers{muted: true}, nil)
	if _, err := svc.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, Content: "x"}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("禁言应 403，得到 %v", err)
	}
}

func TestComment_Guest(t *testing.T) {
	// 游客互动关闭 → 需登录
	svc := newCommentSvc(&fakeCommentRepo{}, nil, fakeFeatures{off: map[string]bool{FlagGuestInteraction: true}}, nil, nil, nil)
	if _, err := svc.Create(context.Background(), CreateCommentInput{Author: guestAuthor(), EpisodeID: 5, Content: "x"}); !errors.Is(err, errs.ErrNeedLogin) {
		t.Fatalf("游客互动关闭应 401，得到 %v", err)
	}
	// 游客互动开启 → 可发（游客不查禁言）
	repo := &fakeCommentRepo{}
	svc2 := newCommentSvc(repo, nil, nil, nil, fakeUsers{muted: true}, nil)
	if c, err := svc2.Create(context.Background(), CreateCommentInput{Author: guestAuthor(), EpisodeID: 5, Content: "游客发言"}); err != nil || c.Status != StatusVisible {
		t.Fatalf("游客应可发：%+v %v", c, err)
	}
}

func TestComment_FeatureOff(t *testing.T) {
	svc := newCommentSvc(&fakeCommentRepo{}, nil, fakeFeatures{off: map[string]bool{FlagComments: true}}, nil, nil, nil)
	if _, err := svc.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, Content: "x"}); !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("评论关闭应 403，得到 %v", err)
	}
}

func TestComment_LengthAndDup(t *testing.T) {
	// 空内容
	svc := newCommentSvc(&fakeCommentRepo{}, nil, nil, nil, nil, nil)
	if _, err := svc.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, Content: "   "}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("空内容应 400")
	}
	// 重复内容
	svc2 := newCommentSvc(&fakeCommentRepo{}, nil, nil, nil, nil, &fakeLimiter{dup: true})
	if _, err := svc2.Create(context.Background(), CreateCommentInput{Author: userAuthor(), EpisodeID: 5, Content: "重复"}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("重复内容应 409，得到 %v", err)
	}
}

func TestComment_Delete(t *testing.T) {
	// 本人删除
	repo := &fakeCommentRepo{owner: Author{UserID: 7}}
	svc := newCommentSvc(repo, nil, nil, nil, nil, nil)
	if err := svc.Delete(context.Background(), Author{UserID: 7}, 9); err != nil || repo.softDel != 9 {
		t.Fatalf("本人应可删：%v softDel=%d", err, repo.softDel)
	}
	// 他人删除 → 404
	repo2 := &fakeCommentRepo{owner: Author{UserID: 8}}
	svc2 := newCommentSvc(repo2, nil, nil, nil, nil, nil)
	if err := svc2.Delete(context.Background(), Author{UserID: 7}, 9); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删他人应 404，得到 %v", err)
	}
}

func TestComment_Like(t *testing.T) {
	repo := &fakeCommentRepo{}
	svc := newCommentSvc(repo, nil, nil, nil, nil, nil)
	if err := svc.Like(context.Background(), 7, 9); err != nil || repo.likedC != 9 || repo.likedU != 7 {
		t.Fatalf("点赞应落到仓储：%v c=%d u=%d", err, repo.likedC, repo.likedU)
	}
	if err := svc.Like(context.Background(), 0, 9); !errors.Is(err, errs.ErrNeedLogin) {
		t.Fatal("未登录点赞应 401")
	}
}

func TestComment_List(t *testing.T) {
	svc := newCommentSvc(&fakeCommentRepo{}, nil, nil, nil, nil, nil)
	p, err := svc.List(context.Background(), 5, "hot", "", 10)
	if err != nil || len(p.List) != 1 {
		t.Fatalf("列表：%+v %v", p, err)
	}
	if _, err := svc.List(context.Background(), 0, "latest", "", 10); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("分集 ID 无效应 400")
	}
}
