package community

import (
	"context"
	"net/http"
	"testing"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/errs"
)

type fakeReports struct {
	got dom.SubmitReportInput
	err error
}

func (f *fakeReports) Submit(_ context.Context, in dom.SubmitReportInput) error {
	f.got = in
	return f.err
}

func TestSubmitReport_UserGuestAnon(t *testing.T) {
	// 注册用户举报
	fr := &fakeReports{}
	h := &Handlers{Reports: fr}
	w := do(t, h, fakeResolver{access: userAccess()}, http.MethodPost, "/api/v1/reports", `{"target_type":"comment","target_id":9,"reason":"ad","detail":"广告"}`, "tok")
	if w.Code != http.StatusNoContent || fr.got.Reporter.UserID != 7 || fr.got.TargetID != 9 || fr.got.Reason != "ad" {
		t.Fatalf("注册用户举报：%d %+v", w.Code, fr.got)
	}
	// 游客举报
	fr2 := &fakeReports{}
	h2 := &Handlers{Reports: fr2}
	if w := do(t, h2, fakeResolver{access: guestAccess()}, http.MethodPost, "/api/v1/reports", `{"target_type":"danmaku","target_id":3,"reason":"attack"}`, "tok"); w.Code != http.StatusNoContent || !fr2.got.Reporter.IsGuest() {
		t.Fatalf("游客举报应 204 且举报人为游客：%d %+v", w.Code, fr2.got)
	}
	// 匿名 → 401
	h3 := &Handlers{Reports: &fakeReports{}}
	if w := do(t, h3, fakeResolver{}, http.MethodPost, "/api/v1/reports", `{"target_type":"comment","target_id":1,"reason":"ad"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("匿名举报应 401，得到 %d", w.Code)
	}
}

func TestSubmitReport_DomainError(t *testing.T) {
	h := &Handlers{Reports: &fakeReports{err: errs.ErrBadRequest.WithMessage("举报原因不支持")}}
	w := do(t, h, fakeResolver{access: userAccess()}, http.MethodPost, "/api/v1/reports", `{"target_type":"comment","target_id":1,"reason":"nope"}`, "tok")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("领域 400 应透传，得到 %d", w.Code)
	}
}
