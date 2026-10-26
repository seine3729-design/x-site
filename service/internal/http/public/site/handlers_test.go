package site

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/site"
	"xsite/service/internal/errs"
)

func init() { gin.SetMode(gin.TestMode) }

type fakePages struct {
	slug string
	err  error
}

func (f *fakePages) Public(_ context.Context, slug string) (dom.PublicPage, error) {
	f.slug = slug
	if f.err != nil {
		return dom.PublicPage{}, f.err
	}
	return dom.PublicPage{
		Slug: slug, Title: "用户协议", Body: "<p>正文</p>", SEOTitle: "协议", SEODesc: "简介",
		UpdatedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC),
	}, nil
}

type fakeHome struct {
	page   string
	blocks []dom.PublicBlock
	err    error
}

func (f *fakeHome) Home(_ context.Context, page string) (dom.PublicHome, error) {
	f.page = page
	if f.err != nil {
		return dom.PublicHome{}, f.err
	}
	if page == "" {
		page = "home"
	}
	blocks := f.blocks
	if blocks == nil {
		blocks = []dom.PublicBlock{}
	}
	return dom.PublicHome{Page: page, Blocks: blocks}, nil
}

func newEngine(p *fakePages, h *fakeHome) *gin.Engine {
	r := gin.New()
	(&Handlers{Pages: p, Home: h}).Routes(r.Group("/api/v1"))
	return r
}

func TestPublicPage(t *testing.T) {
	p := &fakePages{}
	r := newEngine(p, &fakeHome{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pages/terms", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d %s", w.Code, w.Body.String())
	}
	if p.slug != "terms" {
		t.Fatalf("slug 透传：%q", p.slug)
	}
	var body struct {
		Code string         `json:"code"`
		Data dom.PublicPage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "OK" || body.Data.Title != "用户协议" || body.Data.Body != "<p>正文</p>" {
		t.Fatalf("响应内容：%+v", body)
	}
	etag := w.Header().Get("ETag")
	if etag == "" || w.Header().Get("Cache-Control") != "public, max-age=0, s-maxage=300, stale-while-revalidate=60" {
		t.Fatalf("缓存头：ETag=%q Cache-Control=%q", etag, w.Header().Get("Cache-Control"))
	}

	// 带上 ETag 再请求：304，没有响应体
	req = httptest.NewRequest(http.MethodGet, "/api/v1/pages/terms", nil)
	req.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("期望 304 且无响应体，得到 %d %q", w.Code, w.Body.String())
	}
}

func TestPublicPageNotFound(t *testing.T) {
	r := newEngine(&fakePages{err: errs.ErrNotFound}, &fakeHome{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pages/draft", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("草稿与不存在的页应 404，得到 %d %s", w.Code, w.Body.String())
	}
}

func TestPublicHome(t *testing.T) {
	h := &fakeHome{blocks: []dom.PublicBlock{
		{ID: 1, Type: dom.BlockCarousel, Title: "热播", ColsDesktop: 6, Device: dom.DeviceAll},
		{ID: 2, Type: dom.BlockAd, AdSlot: "home_top"},
	}}
	r := newEngine(&fakePages{}, h)

	// 不带 page 时按首页（具体兜底在领域层）
	req := httptest.NewRequest(http.MethodGet, "/api/v1/home", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d %s", w.Code, w.Body.String())
	}
	if h.page != "" {
		t.Fatalf("page 原样透传：%q", h.page)
	}
	var body struct {
		Code string         `json:"code"`
		Data dom.PublicHome `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "OK" || body.Data.Page != "home" || len(body.Data.Blocks) != 2 || body.Data.Blocks[1].AdSlot != "home_top" {
		t.Fatalf("响应内容：%+v", body)
	}
	etag := w.Header().Get("ETag")
	if etag == "" || w.Header().Get("Cache-Control") != "public, max-age=0, s-maxage=60, stale-while-revalidate=60" {
		t.Fatalf("缓存头：ETag=%q Cache-Control=%q", etag, w.Header().Get("Cache-Control"))
	}

	// 带上 ETag 再请求：304
	req = httptest.NewRequest(http.MethodGet, "/api/v1/home", nil)
	req.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("期望 304 且无响应体，得到 %d %q", w.Code, w.Body.String())
	}

	// 频道页：page 透传，内容不同则 ETag 不同
	req = httptest.NewRequest(http.MethodGet, "/api/v1/home?page=channel:city", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || h.page != "channel:city" {
		t.Fatalf("频道页：%d page=%q", w.Code, h.page)
	}
	if w.Header().Get("ETag") == etag {
		t.Fatalf("不同页面的 ETag 应不同：%q", etag)
	}

	// 没有区块时是空数组，不是 null
	h.blocks = nil
	req = httptest.NewRequest(http.MethodGet, "/api/v1/home", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"blocks":[]`) {
		t.Fatalf("空区块应为 []：%d %s", w.Code, w.Body.String())
	}
}

func TestPublicHomeUnknownPage(t *testing.T) {
	r := newEngine(&fakePages{}, &fakeHome{err: errs.ErrNotFound})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/home?page=channel:gone", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知页面应 404，得到 %d %s", w.Code, w.Body.String())
	}
}
