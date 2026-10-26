package community

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	dom "xsite/service/internal/domain/moderation"
)

type fakeDanmaku struct {
	sent   dom.SendDanmakuInput
	window []dom.DanmakuView
}

func (f *fakeDanmaku) Send(_ context.Context, in dom.SendDanmakuInput) (dom.Danmaku, error) {
	f.sent = in
	return dom.Danmaku{ID: 1, EpisodeID: in.EpisodeID, TimeMS: in.TimeMS, Content: in.Content, Color: in.Color, Status: dom.StatusVisible}, nil
}
func (f *fakeDanmaku) Window(_ context.Context, _ int64, _, _ int) ([]dom.DanmakuView, error) {
	return f.window, nil
}

func TestListDanmaku_Public(t *testing.T) {
	fd := &fakeDanmaku{window: []dom.DanmakuView{{ID: 9, TimeMS: 100, Content: "hi", Color: "FFFFFF"}}}
	h := &Handlers{Danmaku: fd}
	w := do(t, h, fakeResolver{}, http.MethodGet, "/api/v1/episodes/5/danmaku?from_ms=0&to_ms=60000", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("公开读应 200，得到 %d %s", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=30" {
		t.Fatalf("应可缓存 30s，得到 %q", cc)
	}
	var body struct {
		Data struct {
			List []dom.DanmakuView `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Data.List) != 1 {
		t.Fatalf("弹幕列表数据：%s", w.Body.String())
	}
}

func TestSendDanmaku_UserAndGuestAndAnon(t *testing.T) {
	// 注册用户
	fd := &fakeDanmaku{}
	h := &Handlers{Danmaku: fd}
	w := do(t, h, fakeResolver{access: userAccess()}, http.MethodPost, "/api/v1/episodes/5/danmaku", `{"time_ms":1200,"content":"冲","color":"FF0000"}`, "tok")
	if w.Code != http.StatusOK || fd.sent.Author.UserID != 7 || fd.sent.EpisodeID != 5 || fd.sent.TimeMS != 1200 || fd.sent.Color != "FF0000" {
		t.Fatalf("注册用户发送：%d %+v", w.Code, fd.sent)
	}
	// 游客
	fd2 := &fakeDanmaku{}
	h2 := &Handlers{Danmaku: fd2}
	if w := do(t, h2, fakeResolver{access: guestAccess()}, http.MethodPost, "/api/v1/episodes/5/danmaku", `{"time_ms":0,"content":"路过"}`, "tok"); w.Code != http.StatusOK || !fd2.sent.Author.IsGuest() {
		t.Fatalf("游客发送应成功且作者为游客：%d %+v", w.Code, fd2.sent)
	}
	// 匿名（无令牌）→ 401
	h3 := &Handlers{Danmaku: &fakeDanmaku{}}
	if w := do(t, h3, fakeResolver{}, http.MethodPost, "/api/v1/episodes/5/danmaku", `{"content":"x"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("匿名发送应 401，得到 %d", w.Code)
	}
}
