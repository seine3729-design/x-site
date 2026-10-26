package moderation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xsite/service/internal/errs"
)

type fakeReportRepo struct {
	created  NewReport
	calls    int
	lastQ    ReportQuery
	handled  int64
	handledS string
	handledN string
}

func (r *fakeReportRepo) Create(_ context.Context, in NewReport) error {
	r.created = in
	r.calls++
	return nil
}
func (r *fakeReportRepo) List(_ context.Context, q ReportQuery) ([]ReportView, int64, error) {
	r.lastQ = q
	return []ReportView{{ID: 1, TargetType: "comment", TargetID: 9, Reason: "ad", Status: ReportPending}}, 1, nil
}
func (r *fakeReportRepo) Handle(_ context.Context, id int64, status, note string) (ReportView, error) {
	r.handled, r.handledS, r.handledN = id, status, note
	return ReportView{ID: id, Status: status, HandleNote: note}, nil
}

func newReportSvc(repo *fakeReportRepo, lim Limiter) *ReportService {
	if lim == nil {
		lim = &fakeLimiter{}
	}
	return NewReportService(repo, lim)
}

func TestReport_SubmitOK(t *testing.T) {
	repo := &fakeReportRepo{}
	svc := newReportSvc(repo, nil)
	err := svc.Submit(context.Background(), SubmitReportInput{
		Reporter: userAuthor(), TargetType: "comment", TargetID: 9, Reason: "ad", Detail: "  垃圾广告 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.calls != 1 || repo.created.TargetID != 9 || repo.created.Reason != "ad" || repo.created.Detail != "垃圾广告" {
		t.Fatalf("举报落库入参：%+v calls=%d", repo.created, repo.calls)
	}
}

func TestReport_GuestAndAnon(t *testing.T) {
	// 游客可举报
	repo := &fakeReportRepo{}
	svc := newReportSvc(repo, nil)
	if err := svc.Submit(context.Background(), SubmitReportInput{Reporter: guestAuthor(), TargetType: "danmaku", TargetID: 3, Reason: "attack"}); err != nil {
		t.Fatalf("游客应可举报：%v", err)
	}
	// 匿名（身份无效）→ 401
	if err := svc.Submit(context.Background(), SubmitReportInput{TargetType: "comment", TargetID: 1, Reason: "ad"}); !errors.Is(err, errs.ErrNeedLogin) {
		t.Fatalf("匿名应 401，得到 %v", err)
	}
}

func TestReport_Validation(t *testing.T) {
	svc := newReportSvc(&fakeReportRepo{}, nil)
	// 对象类型非法
	if err := svc.Submit(context.Background(), SubmitReportInput{Reporter: userAuthor(), TargetType: "x", TargetID: 1, Reason: "ad"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("对象类型非法应 400")
	}
	// 对象 ID 非法
	if err := svc.Submit(context.Background(), SubmitReportInput{Reporter: userAuthor(), TargetType: "comment", TargetID: 0, Reason: "ad"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("对象 ID 非法应 400")
	}
	// 原因非法
	if err := svc.Submit(context.Background(), SubmitReportInput{Reporter: userAuthor(), TargetType: "comment", TargetID: 1, Reason: "nope"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("原因非法应 400")
	}
	// 详情超长
	long := strings.Repeat("字", maxReportDetailRunes+1)
	if err := svc.Submit(context.Background(), SubmitReportInput{Reporter: userAuthor(), TargetType: "comment", TargetID: 1, Reason: "ad", Detail: long}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("详情超长应 400")
	}
}

func TestReport_Dedup(t *testing.T) {
	// 24 小时内重复 → 幂等放行、不重复落库
	repo := &fakeReportRepo{}
	svc := newReportSvc(repo, &fakeLimiter{dup: true})
	if err := svc.Submit(context.Background(), SubmitReportInput{Reporter: userAuthor(), TargetType: "comment", TargetID: 9, Reason: "ad"}); err != nil {
		t.Fatalf("重复举报应幂等放行：%v", err)
	}
	if repo.calls != 0 {
		t.Fatalf("重复举报不应落库，calls=%d", repo.calls)
	}
}

func TestReport_List(t *testing.T) {
	repo := &fakeReportRepo{}
	svc := newReportSvc(repo, nil)
	page, err := svc.List(context.Background(), ReportFilter{Q: "广告", Status: ReportPending, TargetType: "comment", Reason: "ad", PageRequest: PageRequest{Page: 2, PageSize: 10}})
	if err != nil || len(page.List) != 1 || page.Total != 1 || page.Page != 2 {
		t.Fatalf("列表：%+v %v", page, err)
	}
	if repo.lastQ.Status != ReportPending || repo.lastQ.TargetType != "comment" || repo.lastQ.Reason != "ad" || repo.lastQ.Pattern == "" || repo.lastQ.Limit != 10 || repo.lastQ.Offset != 10 {
		t.Fatalf("筛选 / 分页应交给仓储：%+v", repo.lastQ)
	}
	// 非法状态 / 对象 / 原因
	if _, err := svc.List(context.Background(), ReportFilter{Status: "x"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("非法状态应 400")
	}
	if _, err := svc.List(context.Background(), ReportFilter{TargetType: "x"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("非法对象类型应 400")
	}
	if _, err := svc.List(context.Background(), ReportFilter{Reason: "x"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("非法原因应 400")
	}
}

func TestReport_Handle(t *testing.T) {
	repo := &fakeReportRepo{}
	svc := newReportSvc(repo, nil)
	// 已处理
	if v, err := svc.Handle(context.Background(), 9, ReportResolved, "  已隐藏该评论 "); err != nil || v.Status != ReportResolved || repo.handled != 9 || repo.handledN != "已隐藏该评论" {
		t.Fatalf("处理：%+v %v (note=%q)", v, err, repo.handledN)
	}
	// 已驳回
	if _, err := svc.Handle(context.Background(), 9, ReportRejected, ""); err != nil || repo.handledS != ReportRejected {
		t.Fatalf("驳回：%v", err)
	}
	// 非法状态（不能置 pending 或其它）
	if _, err := svc.Handle(context.Background(), 9, "pending", ""); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("非法处理结果应 400")
	}
}
