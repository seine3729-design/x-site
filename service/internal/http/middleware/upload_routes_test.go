package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 视频分片路由要拿到自己的请求体上限与连接期限：默认 1 MiB 会把 5 MiB 的分片拦在门外，
// 而误判成别的路由（比如带 /parts/ 的其他路径）又会把大请求体放进不该放的接口。
func TestIsAdminUploadPart(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		want         bool
	}{
		{http.MethodPut, "/api/admin/uploads/01a1-uuid/parts/1", true},
		{http.MethodPut, "/api/admin/uploads/01a1-uuid/parts/820", true},
		{http.MethodPost, "/api/admin/uploads/01a1-uuid/parts/1", false},
		{http.MethodPut, "/api/admin/uploads/01a1-uuid/content", false},
		{http.MethodPut, "/api/admin/uploads//parts/1", false},
		{http.MethodPut, "/api/admin/uploads/01a1-uuid/parts/", false},
		{http.MethodPut, "/api/admin/uploads/01a1-uuid/parts/1/extra", false},
		{http.MethodPut, "/api/admin/uploads/a/b/parts/1", false},
		{http.MethodPut, "/api/admin/other/01a1-uuid/parts/1", false},
		{http.MethodPut, "/api/v1/uploads/01a1-uuid/parts/1", false},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if got := isAdminUploadPart(r); got != tc.want {
			t.Errorf("isAdminUploadPart(%s %s) = %v，want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestBodyLimitAllowsVideoPartButNotOtherRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit(1 << 20))
	read := func(c *gin.Context) {
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.Status(http.StatusOK)
	}
	r.PUT("/api/admin/uploads/:id/parts/:number", read)
	r.PUT("/api/admin/drama/:id", read)

	part := bytes.Repeat([]byte{'v'}, 5<<20)
	for _, tc := range []struct {
		path string
		want int
	}{
		{"/api/admin/uploads/01a1-uuid/parts/1", http.StatusOK},
		{"/api/admin/drama/1", http.StatusRequestEntityTooLarge},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPut, tc.path, bytes.NewReader(part)))
		if w.Code != tc.want {
			t.Errorf("PUT %s 5 MiB 得到 %d，want %d", tc.path, w.Code, tc.want)
		}
	}
}
