package community

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/http/middleware"

	"github.com/gin-gonic/gin"
)

type fakeEmojis struct {
	packs []dom.EmojiPackView
}

func (f *fakeEmojis) Front(context.Context) ([]dom.EmojiPackView, error) { return f.packs, nil }

func TestListEmojis_PublicAndETag(t *testing.T) {
	fe := &fakeEmojis{packs: []dom.EmojiPackView{{ID: 1, Name: "默认", Emojis: []dom.EmojiView{{Code: "[微笑]", Image: "/img/a.png"}}}}}
	h := &Handlers{Emojis: fe}
	w := do(t, h, fakeResolver{}, http.MethodGet, "/api/v1/emojis", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("公开读应 200，得到 %d %s", w.Code, w.Body.String())
	}
	cc := w.Header().Get("Cache-Control")
	if cc != "public, max-age=0, s-maxage=86400, stale-while-revalidate=60" {
		t.Fatalf("缓存头：%q", cc)
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("应带 ETag")
	}
	var body struct {
		Data struct {
			Packs []dom.EmojiPackView `json:"packs"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Data.Packs) != 1 || len(body.Data.Packs[0].Emojis) != 1 {
		t.Fatalf("表情数据：%s", w.Body.String())
	}

	// 条件请求：带上同样的 ETag 返回 304、无响应体
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	h.Resolver = fakeResolver{}
	h.Routes(r.Group("/api/v1"))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/emojis", nil)
	req.Header.Set("If-None-Match", etag)
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req)
	if w3.Code != http.StatusNotModified || w3.Body.Len() != 0 {
		t.Fatalf("同 ETag 应 304 无响应体，得到 %d %q", w3.Code, w3.Body.String())
	}
}
