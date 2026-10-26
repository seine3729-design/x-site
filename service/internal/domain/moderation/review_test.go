package moderation

import (
	"context"
	"errors"
	"testing"

	"xsite/service/internal/errs"
)

type fakePolicyRepo struct {
	p   CommentPolicy
	set CommentPolicy
}

func (r *fakePolicyRepo) GetCommentPolicy(context.Context) (CommentPolicy, error) { return r.p, nil }
func (r *fakePolicyRepo) SetCommentPolicy(_ context.Context, p CommentPolicy) (CommentPolicy, error) {
	r.set = p
	r.p = p
	return p, nil
}

func TestPolicyService(t *testing.T) {
	repo := &fakePolicyRepo{p: CommentPolicy{PreModeration: true}}
	svc := NewPolicyService(repo)
	// PreModeration 实现 Policy
	if on, err := svc.PreModeration(context.Background()); err != nil || !on {
		t.Fatalf("应读到 pre_moderation=true：%v %v", on, err)
	}
	// Set
	if _, err := svc.Set(context.Background(), CommentPolicy{PreModeration: false}); err != nil {
		t.Fatal(err)
	}
	if repo.set.PreModeration {
		t.Fatal("应保存 false")
	}
	if g, _ := svc.Get(context.Background()); g.PreModeration {
		t.Fatal("Get 应返回 false")
	}
}

type fakeModerationRepo struct {
	comments  []ModerationItem
	danmaku   []ModerationItem
	lastQ     ModerationQuery
	actKind   string // "setC"/"delC"/"setD"/"delD"
	actIDs    []int64
	actStatus string
}

func (r *fakeModerationRepo) ListComments(_ context.Context, q ModerationQuery) ([]ModerationItem, error) {
	r.lastQ = q
	return r.comments, nil
}
func (r *fakeModerationRepo) CountComments(context.Context, ModerationQuery) (int64, error) {
	return int64(len(r.comments)), nil
}
func (r *fakeModerationRepo) ListDanmaku(_ context.Context, q ModerationQuery) ([]ModerationItem, error) {
	r.lastQ = q
	return r.danmaku, nil
}
func (r *fakeModerationRepo) CountDanmaku(context.Context, ModerationQuery) (int64, error) {
	return int64(len(r.danmaku)), nil
}
func (r *fakeModerationRepo) SetCommentsStatus(_ context.Context, ids []int64, status string) (int64, error) {
	r.actKind, r.actIDs, r.actStatus = "setC", ids, status
	return int64(len(ids)), nil
}
func (r *fakeModerationRepo) SoftDeleteComments(_ context.Context, ids []int64) (int64, error) {
	r.actKind, r.actIDs = "delC", ids
	return int64(len(ids)), nil
}
func (r *fakeModerationRepo) SetDanmakuStatus(_ context.Context, ids []int64, status string) (int64, error) {
	r.actKind, r.actIDs, r.actStatus = "setD", ids, status
	return int64(len(ids)), nil
}
func (r *fakeModerationRepo) DeleteDanmaku(_ context.Context, ids []int64) (int64, error) {
	r.actKind, r.actIDs = "delD", ids
	return int64(len(ids)), nil
}

func TestModeration_ListAttachesHitWords(t *testing.T) {
	repo := &fakeModerationRepo{comments: []ModerationItem{{ID: 1, Content: "正常", Status: StatusPending}, {ID: 2, Content: "有坏词", Status: StatusPending}}}
	// fakeEval 命中：Decision.Hits 由 fakeEval 返回。用自定义 eval 对含「坏词」的返回 hits。
	svc := NewModerationService(repo, hitEval{word: "坏词"})
	page, err := svc.List(context.Background(), KindComment, ModerationFilter{Status: StatusPending, PageRequest: PageRequest{Page: 1, PageSize: 20}})
	if err != nil || len(page.List) != 2 {
		t.Fatalf("列表：%+v %v", page, err)
	}
	// 没命中词的那条也必须是空数组：nil 会序列化成 null，后台页面对它做 .map() 会整页白屏
	if page.List[0].HitWords == nil {
		t.Fatalf("没有命中词时 hit_words 应是空数组而不是 nil：%+v", page.List[0])
	}
	if len(page.List[1].HitWords) != 1 || page.List[1].HitWords[0] != "坏词" {
		t.Fatalf("第二条应带命中词：%+v", page.List[1])
	}
	if repo.lastQ.Status != StatusPending || repo.lastQ.Limit != 20 || repo.lastQ.Offset != 0 {
		t.Fatalf("查询参数应交给仓储：%+v", repo.lastQ)
	}
}

func TestModeration_BlockwordFilter(t *testing.T) {
	repo := &fakeModerationRepo{comments: []ModerationItem{
		{ID: 1, Content: "正常"}, {ID: 2, Content: "有坏词"}, {ID: 3, Content: "也有坏词"},
	}}
	svc := NewModerationService(repo, hitEval{word: "坏词"})
	page, err := svc.List(context.Background(), KindComment, ModerationFilter{Blockword: true, PageRequest: PageRequest{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.List) != 2 {
		t.Fatalf("只应保留含屏蔽词的 2 条：total=%d list=%d", page.Total, len(page.List))
	}
	// 扫描模式下用上限拉候选、服务层分页
	if repo.lastQ.Limit != moderationScanCap || repo.lastQ.Offset != 0 {
		t.Fatalf("扫描模式应按上限拉候选：%+v", repo.lastQ)
	}
}

func TestModeration_Act(t *testing.T) {
	// 评论通过
	repo := &fakeModerationRepo{}
	svc := NewModerationService(repo, fakeEval{})
	if n, err := svc.Act(context.Background(), KindComment, ActionApprove, []int64{1, 2, 2, 0, -1}); err != nil || n != 2 {
		t.Fatalf("通过应去重后 2 条：n=%d err=%v", n, err)
	}
	if repo.actKind != "setC" || repo.actStatus != StatusVisible || len(repo.actIDs) != 2 {
		t.Fatalf("应改评论状态为 visible：%+v", repo)
	}
	// 评论删除 → 软删
	if _, err := svc.Act(context.Background(), KindComment, ActionDelete, []int64{5}); err != nil || repo.actKind != "delC" {
		t.Fatalf("评论删除应软删：%v %s", err, repo.actKind)
	}
	// 弹幕删除 → 硬删
	if _, err := svc.Act(context.Background(), KindDanmaku, ActionDelete, []int64{9}); err != nil || repo.actKind != "delD" {
		t.Fatalf("弹幕删除应硬删：%v %s", err, repo.actKind)
	}
	// 弹幕隐藏
	if _, err := svc.Act(context.Background(), KindDanmaku, ActionHide, []int64{9}); err != nil || repo.actStatus != StatusHidden {
		t.Fatalf("弹幕隐藏：%v %s", err, repo.actStatus)
	}
	// 非法 kind / action / 空 ids
	if _, err := svc.Act(context.Background(), "x", ActionApprove, []int64{1}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("非法 kind 应 400")
	}
	if _, err := svc.Act(context.Background(), KindComment, "x", []int64{1}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("非法 action 应 400")
	}
	if _, err := svc.Act(context.Background(), KindComment, ActionApprove, []int64{0, -1}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("空 ids 应 400")
	}
}

func TestModeration_ListRejectsBadInput(t *testing.T) {
	svc := NewModerationService(&fakeModerationRepo{}, fakeEval{})
	if _, err := svc.List(context.Background(), "x", ModerationFilter{}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("非法 kind 应 400")
	}
	if _, err := svc.List(context.Background(), KindComment, ModerationFilter{Status: "bad"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("非法状态应 400")
	}
}

// hitEval 是一个测试用判定器：内容包含 word 时返回命中。
type hitEval struct{ word string }

func (h hitEval) Evaluate(text string) Decision {
	d := Decision{Text: text}
	if h.word != "" && containsSub(text, h.word) {
		d.Hits = []string{h.word}
	}
	return d
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
