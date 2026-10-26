package http

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/crypto/apienc"
	"xsite/service/internal/http/admin"
	adminbilling "xsite/service/internal/http/admin/billing"
	"xsite/service/internal/http/middleware"
	openfeedback "xsite/service/internal/http/open/feedback"
	"xsite/service/internal/http/public"
	publiccommunity "xsite/service/internal/http/public/community"
	publiccontent "xsite/service/internal/http/public/content"
	publicme "xsite/service/internal/http/public/me"
	publicpayment "xsite/service/internal/http/public/payment"
	publicvip "xsite/service/internal/http/public/vip"
)

type fakeKeys struct{}

func (fakeKeys) Current() string                { return "k1" }
func (fakeKeys) PublicKeys() []apienc.PublicKey { return nil }

const (
	testEdgeSecret   = "edge-secret"
	testServiceToken = "service-token"
)

// newTestRouter 按生产配置（强制回源鉴权、强制报文加密）构建完整路由；依赖只放不会被访问到的零值。
func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	r, err := NewRouter(Options{
		EdgeAuth: middleware.EdgeAuthOptions{Enforced: true, EdgeSecret: testEdgeSecret, ServiceToken: testServiceToken},
		APIEnc:   middleware.APIEncryptionOptions{Required: true},
		Public:   &public.Handlers{Env: "test", APIKeys: fakeKeys{}},
		Admin:    &admin.Handlers{Cookie: admin.NewSessionCookie(false)},
		// 开放提交接口要真实走到 handler，才能验证它既不被回源鉴权拦下也不要求报文加密
		OpenFeedback: &openfeedback.Handlers{},
		// Me.Routes 会读 Resolver 装身份中间件，必须给非 nil Handlers（Resolver 零值即可，测试不请求 /me/*）
		Me:            &publicme.Handlers{},
		PublicVIP:     &publicvip.Handlers{},
		PublicPayment: &publicpayment.Handlers{},
		AdminBilling:  &adminbilling.Handlers{},
		// 这两组的 Routes 在注册时就会读 h 的字段（PublicContent 读 h.Search，PublicCommunity 读 h.Resolver），
		// 必须给非 nil Handlers，否则 NewRouter 注册路由时空指针 panic。
		PublicContent:   &publiccontent.Handlers{},
		PublicCommunity: &publiccommunity.Handlers{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// 路径规范（docs/07 第 0.5 节）：前台接口都在 /api/v1 下，后台接口都在 /api/admin 下，/api 下没有其他命名空间；
// 给站外系统直连的接口单独放在 /open-api 下（feedback/docs/07 第 0.5 节）。
func TestRouter_PathConvention(t *testing.T) {
	r := newTestRouter(t)
	allowed := []string{"/api/v1/", "/api/admin/", "/internal/", "/open-api/"}
	got := map[string]bool{}
	for _, rt := range r.Routes() {
		got[rt.Method+" "+rt.Path] = true
		ok := false
		for _, prefix := range allowed {
			if strings.HasPrefix(rt.Path, prefix) {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("%s %s 不在 %s 下", rt.Method, rt.Path, strings.Join(allowed, "、"))
		}
	}
	for _, want := range []string{
		"GET /internal/ready",
		"GET /api/v1/health",
		"GET /api/v1/crypto/keys",
		"GET /api/v1/site-config",
		"GET /api/v1/dramas/:id",
		"GET /api/v1/channels/:slug",
		"GET /api/v1/tags/:slug",
		"GET /api/v1/explore",
		"GET /api/v1/topics",
		"GET /api/v1/topics/:slug",
		"GET /api/v1/episodes/:id/sources",
		"GET /api/v1/hls/:token/:file",
		"GET /api/v1/hls/key/:episode_id/:key_ver",
		"POST /api/v1/auth/guest",
		"POST /api/v1/auth/register",
		"POST /api/v1/auth/login",
		"GET /api/v1/auth/challenge",
		"POST /api/v1/auth/refresh",
		"POST /api/v1/auth/logout",
		"POST /api/v1/me/password",
		"GET /api/v1/me/sessions",
		"DELETE /api/v1/me/sessions/:id",
		"POST /api/admin/login",
		"GET /api/admin/me",
		"GET /api/admin/me/menus",
		"GET /api/admin/admin-menus",
		"GET /api/admin/ftp",
		"POST /api/admin/ftp/:id/test",
		"POST /api/admin/ftp/:id/list",
		"GET /api/v1/feedback/topics",
		"GET /api/v1/feedback/topics/:id/answers",
		"POST /api/v1/feedback/topics/:id/answers",
		"PUT /api/v1/feedback/answers/:id/helpful",
		"GET /api/admin/feedback/topics",
		"POST /api/admin/feedback/answers/:id/status",
		"GET /api/admin/feedback/api-keys",
		"POST /open-api/feedback",
		"OPTIONS /open-api/feedback",
	} {
		if !got[want] {
			t.Errorf("缺少路由 %s", want)
		}
	}
}

// 明文豁免、免回源鉴权按完整路径精确匹配，改前缀时必须一起改；后台分组不能落进 /api/v1 的加密分组。
func TestRouter_PrefixGroups(t *testing.T) {
	r := newTestRouter(t)
	edge := map[string]string{middleware.HeaderEdgeSecret: testEdgeSecret}

	tests := []struct {
		name       string
		method     string
		path       string
		headers    map[string]string
		wantStatus int
		wantCode   string
	}{
		{"存活检查免回源鉴权与加密", nethttp.MethodGet, "/api/v1/health", nil, 200, "OK"},
		{"就绪检查免回源鉴权", nethttp.MethodGet, "/internal/ready", nil, 200, "OK"},
		{"公钥接口免加密", nethttp.MethodGet, "/api/v1/crypto/keys", edge, 200, "OK"},
		{"其余前台接口强制加密", nethttp.MethodGet, "/api/v1/site-config", edge, 400, "ENC_REQUIRED"},
		{"内容读接口强制加密", nethttp.MethodGet, "/api/v1/explore", edge, 400, "ENC_REQUIRED"},
		{"剧详情强制加密", nethttp.MethodGet, "/api/v1/dramas/1", edge, 400, "ENC_REQUIRED"},
		{"选源强制加密", nethttp.MethodGet, "/api/v1/episodes/1/sources", edge, 400, "ENC_REQUIRED"},
		{"游客签发强制加密", nethttp.MethodPost, "/api/v1/auth/guest", edge, 400, "ENC_REQUIRED"},
		{"清单强制加密", nethttp.MethodGet, "/api/v1/hls/k1.e30.c2ln/index.m3u8", edge, 400, "ENC_REQUIRED"},
		{"密钥强制加密", nethttp.MethodGet, "/api/v1/hls/key/1/1?t=k1.e30.c2ln", edge, 400, "ENC_REQUIRED"},
		{"前台接口仍要求回源凭据", nethttp.MethodGet, "/api/v1/site-config", nil, 403, ""},
		{"后台接口不经过报文加密", nethttp.MethodGet, "/api/admin/me", edge, 401, "NEED_LOGIN"},
		{"旧的用户端路径已下线", nethttp.MethodGet, "/api/site-config", edge, 404, "NOT_FOUND"},
		{"旧的后台路径已下线", nethttp.MethodPost, "/admin-api/login", edge, 404, "NOT_FOUND"},
		{"/api 下没有其他命名空间", nethttp.MethodGet, "/api/me", edge, 404, "NOT_FOUND"},
		// 开放提交接口：站外网页直连，没有回源凭据也不加密，要能走到 handler（空请求体 → 400 BAD_REQUEST）
		{"开放提交接口免回源鉴权", nethttp.MethodPost, "/open-api/feedback", nil, 400, "BAD_REQUEST"},
		{"开放提交接口不要求报文加密", nethttp.MethodPost, "/open-api/feedback", edge, 400, "BAD_REQUEST"},
		{"/open-api 下没有未注册的路径", nethttp.MethodPost, "/open-api/whatever", nil, 403, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Fatalf("状态码 = %d，期望 %d；响应 %s", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantCode == "" {
				return
			}
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("响应不是 JSON：%s", w.Body.String())
			}
			if body.Code != tt.wantCode {
				t.Fatalf("业务码 = %q，期望 %q", body.Code, tt.wantCode)
			}
		})
	}
}

// 开放提交接口要支持跨域预检，否则站外网页的 fetch 发不出来；
// 同时必须不输出 Access-Control-Allow-Credentials——浏览器不该为这个接口带上任何会话。
func TestRouter_OpenFeedbackCORS(t *testing.T) {
	r := newTestRouter(t)
	const origin = "https://partner.example.com"

	req := httptest.NewRequest(nethttp.MethodOptions, "/open-api/feedback", nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", nethttp.MethodPost)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != nethttp.StatusNoContent {
		t.Fatalf("预检状态码 = %d，期望 204；响应 %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Access-Control-Allow-Origin = %q，期望 %q", got, origin)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("不应允许带凭据，实际 Access-Control-Allow-Credentials = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, openfeedback.HeaderAPIKey) {
		t.Errorf("Access-Control-Allow-Headers = %q，缺少 %s", got, openfeedback.HeaderAPIKey)
	}
	if got := w.Header().Values("Vary"); !slices.Contains(got, "Origin") {
		t.Errorf("Vary = %v，缺少 Origin", got)
	}
}
