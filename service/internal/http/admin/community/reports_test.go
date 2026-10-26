package community

import (
	"context"
	"strings"
	"testing"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/errs"
)

// fakeReports 记录调用并返回可控结果。
type fakeReports struct {
	lastFilter dom.ReportFilter
	handledID  int64
	handledS   string
	handledN   string
	handleErr  error
}

func (f *fakeReports) List(_ context.Context, filter dom.ReportFilter) (dom.Page[dom.ReportView], error) {
	f.lastFilter = filter
	return dom.Page[dom.ReportView]{
		List: []dom.ReportView{
			{ID: 3, TargetType: "comment", TargetID: 9, Reason: "ad", Detail: "垃圾广告", Status: dom.ReportPending, Reporter: dom.AuthorView{IsGuest: true, Nickname: "游客"}},
		},
		Total: 1, Page: 1, PageSize: 20,
	}, nil
}
func (f *fakeReports) Handle(_ context.Context, id int64, status, note string) (dom.ReportView, error) {
	if f.handleErr != nil {
		return dom.ReportView{}, f.handleErr
	}
	f.handledID, f.handledS, f.handledN = id, status, note
	return dom.ReportView{ID: id, Status: status, HandleNote: note}, nil
}

func TestReportPermissions(t *testing.T) {
	e := newEnv()
	for _, tt := range []struct {
		as, method, path, body string
		want                   int
	}{
		{"viewer", "GET", "/api/admin/reports", "", 200},
		{"viewer", "POST", "/api/admin/reports/3/handle", `{"status":"resolved"}`, 403},
		{"editor", "POST", "/api/admin/reports/3/handle", `{"status":"resolved","note":"已处理"}`, 200},
		{"nobody", "GET", "/api/admin/reports", "", 403},
		{"", "GET", "/api/admin/reports", "", 401},
	} {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}
}

func TestListReports(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("viewer", "GET", "/api/admin/reports?q=%E5%B9%BF%E5%91%8A&status=pending&target_type=comment&reason=ad&page=2&page_size=10", ""), 200, "OK")
	f := e.rep.lastFilter
	if f.Q != "广告" || f.Status != "pending" || f.TargetType != "comment" || f.Reason != "ad" || f.Page != 2 || f.PageSize != 10 {
		t.Fatalf("筛选参数应交给服务：%+v", f)
	}
	if !strings.Contains(string(b.Data), `"reason":"ad"`) || !strings.Contains(string(b.Data), `"is_guest":true`) {
		t.Fatalf("列表数据不对：%s", b.Data)
	}
}

func TestHandleReport(t *testing.T) {
	e := newEnv()
	expect(t, e.do("editor", "POST", "/api/admin/reports/3/handle", `{"status":"resolved","note":"已隐藏该评论"}`), 200, "OK")
	if e.rep.handledID != 3 || e.rep.handledS != "resolved" || e.rep.handledN != "已隐藏该评论" {
		t.Fatalf("处理入参应交给服务：id=%d status=%q note=%q", e.rep.handledID, e.rep.handledS, e.rep.handledN)
	}
	if a := e.audit.last(t); a.Action != "report.handle" || a.TargetType != "report" || a.TargetID != "3" {
		t.Fatalf("处理审计：%+v", a)
	}
	// 未知字段 400
	expect(t, e.do("editor", "POST", "/api/admin/reports/3/handle", `{"status":"resolved","extra":1}`), 400, "BAD_REQUEST")
	// 非正整数 id → 404
	expect(t, e.do("editor", "POST", "/api/admin/reports/abc/handle", `{"status":"resolved"}`), 404, "NOT_FOUND")
	// 领域校验错误透传（非法状态）
	e.rep.handleErr = errs.ErrBadRequest.WithMessage("处理结果只能是已处理或已驳回")
	expect(t, e.do("editor", "POST", "/api/admin/reports/3/handle", `{"status":"pending"}`), 400, "BAD_REQUEST")
}
