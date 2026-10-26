package public

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/crypto/apienc"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/domain/site"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
)

type fakeSiteConfig struct {
	cfg platform.SiteConfig
	err error
}

func (f fakeSiteConfig) Get(context.Context) (platform.SiteConfig, error) { return f.cfg, f.err }

func newRouter(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/api/v1/site-config", h.GetSiteConfig)
	return r
}

func get(r http.Handler, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/site-config", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGetSiteConfig_OKWithETag(t *testing.T) {
	r := newRouter(&Handlers{SiteConfig: fakeSiteConfig{cfg: platform.SiteConfig{Version: 7, Site: json.RawMessage(`{"site_name":"短剧"}`)}}})

	w := get(r, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d", w.Code)
	}
	// ETag 由版本号 + 下发内容的哈希组成（菜单项的链接目标被别的模块改掉时版本号不变，但内容会变）
	if got := w.Header().Get("ETag"); !strings.HasPrefix(got, `W/"site-7-`) {
		t.Fatalf("ETag 错误：%q", got)
	}
	var body struct {
		Code string `json:"code"`
		Data struct {
			Version     int64             `json:"version"`
			Menus       []site.PublicMenu `json:"menus"`
			FooterPages []site.FooterPage `json:"footer_pages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "OK" || body.Data.Version != 7 {
		t.Fatalf("响应体不符合预期：%s", w.Body.String())
	}
	// 没接站点编排时也要有这两段（空数组而不是 null）
	if body.Data.Menus == nil || body.Data.FooterPages == nil {
		t.Fatalf("menus 与 footer_pages 应为空数组：%s", w.Body.String())
	}
}

// fakeSiteSections 提供随站点配置下发的菜单与页脚页。
type fakeSiteSections struct {
	menus   []site.PublicMenu
	footers []site.FooterPage
	err     error
}

func (f fakeSiteSections) Menus(context.Context) ([]site.PublicMenu, error) { return f.menus, f.err }

func (f fakeSiteSections) FooterPages(context.Context) ([]site.FooterPage, error) {
	return f.footers, f.err
}

func TestGetSiteConfig_WithMenusAndFooterPages(t *testing.T) {
	sections := fakeSiteSections{
		menus:   []site.PublicMenu{{ID: 1, Position: site.PositionTop, Label: "首页", LinkType: site.LinkInternal, LinkValue: "home"}},
		footers: []site.FooterPage{{Slug: "terms", Title: "用户协议"}},
	}
	h := &Handlers{SiteConfig: fakeSiteConfig{cfg: platform.SiteConfig{Version: 7}}, Site: sections}
	w := get(newRouter(h), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Menus       []site.PublicMenu `json:"menus"`
			FooterPages []site.FooterPage `json:"footer_pages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Menus) != 1 || body.Data.Menus[0].Label != "首页" || len(body.Data.FooterPages) != 1 || body.Data.FooterPages[0].Slug != "terms" {
		t.Fatalf("菜单与页脚页应随配置下发：%s", w.Body.String())
	}

	// 版本号不变但菜单变了：ETag 必须变（否则客户端一直拿到 304）
	etag := w.Header().Get("ETag")
	changed := sections
	changed.menus = nil
	h2 := &Handlers{SiteConfig: fakeSiteConfig{cfg: platform.SiteConfig{Version: 7}}, Site: changed}
	if got := get(newRouter(h2), nil).Header().Get("ETag"); got == etag {
		t.Fatalf("菜单变化后 ETag 应变化，两次都是 %q", got)
	}

	// 条件请求：同样的内容返回 304
	if w2 := get(newRouter(h), map[string]string{"If-None-Match": etag}); w2.Code != http.StatusNotModified || w2.Body.Len() != 0 {
		t.Fatalf("期望 304 且无响应体，得到 %d %q", w2.Code, w2.Body.String())
	}
}

func TestGetSiteConfig_NotModified(t *testing.T) {
	r := newRouter(&Handlers{SiteConfig: fakeSiteConfig{cfg: platform.SiteConfig{Version: 7}}})

	etag := get(r, nil).Header().Get("ETag")
	w := get(r, map[string]string{"If-None-Match": etag})
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("期望 304 且无响应体，得到 %d %q", w.Code, w.Body.String())
	}
}

func TestGetSiteConfig_ErrorMapping(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"业务错误按业务码返回", errs.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"未知错误不泄露细节", errors.New("pq: connection refused"), http.StatusInternalServerError, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRouter(&Handlers{SiteConfig: fakeSiteConfig{err: tt.err}})
			w := get(r, nil)
			var body struct {
				Code      string `json:"code"`
				Message   string `json:"message"`
				RequestID string `json:"request_id"`
			}
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if w.Code != tt.status || body.Code != tt.code || body.RequestID == "" {
				t.Fatalf("得到 %d %s", w.Code, w.Body.String())
			}
			if tt.code == "INTERNAL" && body.Message != errs.ErrInternal.Message {
				t.Fatalf("内部错误文案不应包含细节：%q", body.Message)
			}
		})
	}
}

func TestGetCryptoKeys(t *testing.T) {
	kr, err := apienc.ParseKeyring("k2:0123456789abcdef0123456789abcdef-new,k1:0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/crypto/keys", (&Handlers{APIKeys: kr}).GetCryptoKeys)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/crypto/keys", nil))
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("期望 200 且可缓存，得到 %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
	var body struct {
		Code string `json:"code"`
		Data struct {
			Current string             `json:"current"`
			Keys    []apienc.PublicKey `json:"keys"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.Current != "k2" || len(body.Data.Keys) != 2 || body.Data.Keys[0].AEAD != "AES-256-GCM" {
		t.Fatalf("公钥列表不符合预期：%s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "0123456789abcdef") {
		t.Fatal("响应中不能出现密钥 secret")
	}
}
