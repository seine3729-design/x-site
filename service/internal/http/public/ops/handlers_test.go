package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"xsite/service/internal/domain/content"
	dom "xsite/service/internal/domain/ops"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
	binding.EnableDecoderDisallowUnknownFields = true // 与 NewRouter 一致
}

type fakeAnnouncements struct {
	locale string
	list   []dom.PublicAnnouncement
	err    error
}

func (f *fakeAnnouncements) Live(_ context.Context, locale string) ([]dom.PublicAnnouncement, error) {
	f.locale = locale
	return f.list, f.err
}

func newRouter(h *Handlers) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	h.Routes(r.Group("/api/v1"))
	return r
}

func get(r *gin.Engine, path string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestListAnnouncements(t *testing.T) {
	svc := &fakeAnnouncements{list: []dom.PublicAnnouncement{
		{ID: 2, Kind: dom.AnnouncementPopup, Content: "弹窗", Frequency: dom.FrequencyOnce, Pages: []string{"home"}, Image: "/a.webp"},
	}}
	r := newRouter(&Handlers{Announcements: svc})

	w := get(r, "/api/v1/announcements?locale=en", nil)
	if w.Code != http.StatusOK || svc.locale != "en" {
		t.Fatalf("应按语言取公告：%d %q %s", w.Code, svc.locale, w.Body.String())
	}
	var body struct {
		Code string `json:"code"`
		Data struct {
			List []map[string]any `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != "OK" || len(body.Data.List) != 1 {
		t.Fatalf("响应包：%s %v", w.Body.String(), err)
	}
	a := body.Data.List[0]
	if a["kind"] != "popup" || a["frequency"] != "once" || a["image"] != "/a.webp" || a["click_url"] != "" {
		t.Fatalf("字段：%v", a)
	}
	if _, ok := a["locales"]; ok {
		t.Fatalf("前台不需要投放语言等后台字段：%v", a)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "s-maxage=30") {
		t.Fatalf("缓存头：%q", cc)
	}

	// 内容不变时 ETag 不变，带 If-None-Match 返回 304
	etag := w.Header().Get("ETag")
	if etag == "" || get(r, "/api/v1/announcements?locale=en", nil).Header().Get("ETag") != etag {
		t.Fatalf("同样的内容 ETag 应相同：%q", etag)
	}
	if w := get(r, "/api/v1/announcements?locale=en", http.Header{"If-None-Match": {etag}}); w.Code != http.StatusNotModified {
		t.Fatalf("应 304，得到 %d", w.Code)
	}
	svc.list = append(svc.list, dom.PublicAnnouncement{ID: 3, Kind: dom.AnnouncementTopbar, Content: "新", Pages: []string{}})
	if get(r, "/api/v1/announcements?locale=en", nil).Header().Get("ETag") == etag {
		t.Fatal("内容变了 ETag 应变化")
	}

	// 没有公告时为空数组
	svc.list = []dom.PublicAnnouncement{}
	if w := get(r, "/api/v1/announcements", nil); !strings.Contains(w.Body.String(), `"list":[]`) || svc.locale != "" {
		t.Fatalf("没有公告：%s（locale=%q）", w.Body.String(), svc.locale)
	}
	svc.err = errs.ErrUnavailable
	if w := get(r, "/api/v1/announcements", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("服务错误按业务码返回：%d", w.Code)
	}
}

type fakeAds struct {
	serve  dom.ServeRequest
	events []dom.AdEvent
	list   []dom.PublicAd
	err    error
}

func (f *fakeAds) Serve(_ context.Context, req dom.ServeRequest) ([]dom.PublicAd, error) {
	f.serve = req
	return f.list, f.err
}

func (f *fakeAds) Record(_ context.Context, ev dom.AdEvent) error {
	f.events = append(f.events, ev)
	return f.err
}

func send(r *gin.Engine, method, path, body string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "203.0.113.9:4321"
	for k, v := range header {
		req.Header[k] = v
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const deviceUUID = "0192F4C2-7A51-7D3E-9B1A-3C4D5E6F7A8B"

func TestListAds(t *testing.T) {
	svc := &fakeAds{list: []dom.PublicAd{{ID: 3, Slot: dom.SlotHomeTop, CreativeType: dom.CreativeImage, Image: "/a.webp", ClickURL: "/vip"}}}
	r := newRouter(&Handlers{Announcements: &fakeAnnouncements{}, Ads: svc})

	w := send(r, http.MethodGet, "/api/v1/ads?slots=home_top,pause&n=2&device=mobile&locale=en", "", http.Header{"X-Device-Id": {deviceUUID}})
	if w.Code != http.StatusOK {
		t.Fatalf("应 200：%d %s", w.Code, w.Body.String())
	}
	if s := svc.serve; s.Slots != "home_top,pause" || s.N != 2 || s.Device != "mobile" || s.Locale != "en" || s.DeviceKey == "" {
		t.Fatalf("参数应交给服务：%+v", s)
	}
	if strings.Contains(strings.ToLower(svc.serve.DeviceKey), strings.ToLower(deviceUUID)) {
		t.Fatalf("设备标识只交摘要，不交原文：%q", svc.serve.DeviceKey)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("按设备频控、随机挑选，不能共享缓存：%q", cc)
	}
	var body struct {
		Code string `json:"code"`
		Data struct {
			List []map[string]any `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != "OK" || len(body.Data.List) != 1 {
		t.Fatalf("响应包：%s %v", w.Body.String(), err)
	}
	if a := body.Data.List[0]; a["slot"] != "home_top" || a["creative_type"] != "image" || a["click_url"] != "/vip" || a["html"] != "" {
		t.Fatalf("字段：%v", a)
	}
	for _, f := range []string{"name", "weight", "freq_cap", "imp_count", "locales"} {
		if _, ok := body.Data.List[0][f]; ok {
			t.Fatalf("前台不需要后台字段 %s：%v", f, body.Data.List[0])
		}
	}

	if w := send(r, http.MethodGet, "/api/v1/ads?slots=home_top&n=abc", "", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("n 不是数字应 400：%d", w.Code)
	}
	svc.list, svc.err = nil, errs.ErrBadRequest.WithMessage("广告位置无效")
	if w := send(r, http.MethodGet, "/api/v1/ads?slots=x", "", nil); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "广告位置无效") {
		t.Fatalf("领域层的错误按业务码返回：%d %s", w.Code, w.Body.String())
	}
	svc.err = nil
	if w := send(r, http.MethodGet, "/api/v1/ads?slots=pause", "", nil); !strings.Contains(w.Body.String(), `"list":[]`) {
		t.Fatalf("没有广告时为空数组：%s", w.Body.String())
	}
}

func TestDeviceKey(t *testing.T) {
	svc := &fakeAds{}
	r := newRouter(&Handlers{Announcements: &fakeAnnouncements{}, Ads: svc})
	key := func(h http.Header) string {
		t.Helper()
		send(r, http.MethodGet, "/api/v1/ads?slots=home_top", "", h)
		return svc.serve.DeviceKey
	}
	a := key(http.Header{"X-Device-Id": {deviceUUID}})
	if b := key(http.Header{"X-Device-Id": {strings.ToLower(deviceUUID)}, "User-Agent": {"Other"}}); b != a {
		t.Fatal("同一个设备 ID（大小写无关）是同一台设备，与 UA 无关")
	}
	if b := key(http.Header{"X-Device-Id": {"0192f4c2-7a51-7d3e-9b1a-3c4d5e6f7a8c"}}); b == a {
		t.Fatal("不同设备 ID 是不同设备")
	}
	// 没有或不是 UUID 时退化为「IP 网段 + UA 摘要」
	ua1 := key(http.Header{"User-Agent": {"UA-1"}})
	if ua1 == "" || ua1 == a || key(http.Header{"User-Agent": {"UA-1"}, "X-Device-Id": {"not-a-uuid"}}) != ua1 {
		t.Fatal("没有合法设备 ID 时按 IP 网段 + UA 区分")
	}
	if key(http.Header{"User-Agent": {"UA-2"}}) == ua1 {
		t.Fatal("UA 不同是不同设备")
	}
}

func TestAdEvent(t *testing.T) {
	svc := &fakeAds{}
	r := newRouter(&Handlers{Announcements: &fakeAnnouncements{}, Ads: svc})

	w := send(r, http.MethodPost, "/api/v1/ads/event", `{"ad_id":3,"kind":"click"}`, http.Header{"X-Device-Id": {deviceUUID}})
	if w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("应 204 且没有响应体：%d %q", w.Code, w.Body.String())
	}
	if len(svc.events) != 1 || svc.events[0].AdID != 3 || svc.events[0].Kind != dom.AdClick || svc.events[0].DeviceKey == "" {
		t.Fatalf("事件应交给服务：%+v", svc.events)
	}
	for name, body := range map[string]string{
		"不是 JSON": `ad_id=3`,
		"未知字段":    `{"ad_id":3,"kind":"click","slot":"home_top"}`,
		"类型不对":    `{"ad_id":"3","kind":"click"}`,
		"没有请求体":   ``,
	} {
		if w := send(r, http.MethodPost, "/api/v1/ads/event", body, nil); w.Code != http.StatusBadRequest {
			t.Errorf("%s：应 400，得到 %d", name, w.Code)
		}
	}
	svc.err = errs.ErrBadRequest.WithMessage("kind 只能是 impression 或 click")
	if w := send(r, http.MethodPost, "/api/v1/ads/event", `{"ad_id":3,"kind":"view"}`, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("领域层的错误按业务码返回：%d", w.Code)
	}
}

type fakeHotBoard struct {
	list  []dom.PublicHotWord
	err   error
	calls int
}

func (f *fakeHotBoard) Board(context.Context) ([]dom.PublicHotWord, error) {
	f.calls++
	return f.list, f.err
}

func TestHotBoard(t *testing.T) {
	svc := &fakeHotBoard{list: []dom.PublicHotWord{{Word: "霸道总裁", Pinned: true}, {Word: "重生"}}}
	r := newRouter(&Handlers{Announcements: &fakeAnnouncements{}, Ads: &fakeAds{}, Hotsearch: svc})

	w := get(r, "/api/v1/search/hot", nil)
	if w.Code != http.StatusOK || svc.calls != 1 {
		t.Fatalf("应 200：%d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"list":[{"word":"霸道总裁","pinned":true},{"word":"重生","pinned":false}]`) {
		t.Fatalf("按顺序返回词与是否置顶：%s", w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "s-maxage=60") {
		t.Fatalf("热搜榜可以共享缓存：%q", cc)
	}
	etag := w.Header().Get("ETag")
	if w := get(r, "/api/v1/search/hot", http.Header{"If-None-Match": {etag}}); etag == "" || w.Code != http.StatusNotModified {
		t.Fatalf("内容不变时 304：%q %d", etag, w.Code)
	}
	svc.list = nil
	if w := get(r, "/api/v1/search/hot", nil); !strings.Contains(w.Body.String(), `"list":[]`) {
		t.Fatalf("没有热搜词时为空数组：%s", w.Body.String())
	}
	svc.err = errs.ErrUnavailable
	if w := get(r, "/api/v1/search/hot", nil); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("服务错误按业务码返回：%d", w.Code)
	}
}

type fakeRankBoard struct {
	board dom.RankBoard
	out   dom.PublicRankBoard
	err   error
}

func (f *fakeRankBoard) PublicBoard(_ context.Context, b dom.RankBoard) (dom.PublicRankBoard, error) {
	f.board = b
	return f.out, f.err
}

func TestRankBoard(t *testing.T) {
	at := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	svc := &fakeRankBoard{out: dom.PublicRankBoard{Board: dom.BoardDramaWeek, ComputedAt: &at, List: []dom.PublicRankItem{
		{Rank: 1, Heat: 12.5, Drama: content.DramaCard{ID: 7, Title: "霸道总裁", PlayCount: 99}},
	}}}
	r := newRouter(&Handlers{Announcements: &fakeAnnouncements{}, Ads: &fakeAds{}, Hotsearch: &fakeHotBoard{}, Ranks: svc})

	w := get(r, "/api/v1/rank?board=drama_week", nil)
	if w.Code != http.StatusOK || svc.board != dom.BoardDramaWeek {
		t.Fatalf("应 200 并把榜单名交给服务：%d %q %s", w.Code, svc.board, w.Body.String())
	}
	var body struct {
		Data struct {
			Board      string           `json:"board"`
			ComputedAt string           `json:"computed_at"`
			List       []map[string]any `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Data.Board != "drama_week" || body.Data.ComputedAt == "" || len(body.Data.List) != 1 {
		t.Fatalf("响应：%s %v", w.Body.String(), err)
	}
	item := body.Data.List[0]
	drama, _ := item["drama"].(map[string]any)
	if item["rank"] != float64(1) || item["heat"] != 12.5 || drama["id"] != float64(7) || drama["title"] != "霸道总裁" || drama["play_count"] != float64(99) {
		t.Fatalf("名次、热度值与剧卡片：%v", item)
	}
	if _, ok := item["pinned"]; ok {
		t.Fatalf("前台不暴露运营置顶：%v", item)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "s-maxage=60") {
		t.Fatalf("可以共享缓存：%q", cc)
	}
	etag := w.Header().Get("ETag")
	if w := get(r, "/api/v1/rank?board=drama_week", http.Header{"If-None-Match": {etag}}); etag == "" || w.Code != http.StatusNotModified {
		t.Fatalf("内容不变时 304：%q %d", etag, w.Code)
	}

	get(r, "/api/v1/rank", nil)
	if svc.board != "" {
		t.Fatalf("没带 board 时原样交给服务判断：%q", svc.board)
	}
	svc.err = errs.ErrNotFound
	if w := get(r, "/api/v1/rank?board=drama_week", nil); w.Code != http.StatusNotFound {
		t.Fatalf("榜单关闭时 404：%d", w.Code)
	}
	svc.err = errs.ErrBadRequest.WithMessage("board 只能是 drama_week 或 drama_month")
	if w := get(r, "/api/v1/rank?board=x", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("未知榜单 400：%d", w.Code)
	}
}
