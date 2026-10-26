package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func newEngine(handlers ...gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(handlers...)
	r.GET("/api/v1/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/api/v1/x", func(c *gin.Context) { c.String(http.StatusOK, GetRequestID(c)) })
	return r
}

func do(r http.Handler, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRequestID(t *testing.T) {
	r := newEngine(RequestID())

	tests := []struct {
		name     string
		incoming string
		keep     bool
	}{
		{"无上游 id 时生成", "", false},
		{"合法上游 id 保留", "abc-123", true},
		{"含非法字符时重新生成", "abc\n123", false},
		{"超长时重新生成", string(make([]byte, 65)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := map[string]string{}
			if tt.incoming != "" {
				h[HeaderRequestID] = tt.incoming
			}
			w := do(r, "/api/v1/x", h)
			got := w.Header().Get(HeaderRequestID)
			if got == "" || w.Body.String() != got {
				t.Fatalf("响应头与上下文中的 id 应一致且非空，头=%q 体=%q", got, w.Body.String())
			}
			if tt.keep != (got == tt.incoming) {
				t.Fatalf("保留上游 id 期望 %v，得到 %q", tt.keep, got)
			}
		})
	}
}

func TestEdgeAuth(t *testing.T) {
	opt := EdgeAuthOptions{
		Enforced:     true,
		EdgeSecret:   "edge-secret",
		ServiceToken: "service-token",
		Exempt:       map[string]bool{"/api/v1/health": true},
	}
	r := newEngine(EdgeAuth(opt))

	tests := []struct {
		name    string
		path    string
		headers map[string]string
		status  int
	}{
		{"无凭据被拒绝", "/api/v1/x", nil, http.StatusForbidden},
		{"错误的回源密钥被拒绝", "/api/v1/x", map[string]string{HeaderEdgeSecret: "wrong"}, http.StatusForbidden},
		{"正确的回源密钥放行", "/api/v1/x", map[string]string{HeaderEdgeSecret: "edge-secret"}, http.StatusOK},
		{"BFF 服务令牌放行", "/api/v1/x", map[string]string{HeaderServiceToken: "service-token"}, http.StatusOK},
		{"健康检查豁免", "/api/v1/health", nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := do(r, tt.path, tt.headers).Code; got != tt.status {
				t.Fatalf("期望 %d，得到 %d", tt.status, got)
			}
		})
	}
}

func TestEdgeAuth_NotEnforced(t *testing.T) {
	r := newEngine(EdgeAuth(EdgeAuthOptions{Enforced: false, EdgeSecret: "s", ServiceToken: "t"}))
	if got := do(r, "/api/v1/x", nil).Code; got != http.StatusOK {
		t.Fatalf("未强制时应放行，得到 %d", got)
	}
}
