package respond

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/errs"
)

func TestError_RetryAfterHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		err        error
		status     int
		retryAfter string
	}{
		{"带等待时长时输出 Retry-After（秒，向上取整）", errs.WithRetryAfter(errs.ErrRateLimited, 1500*time.Millisecond), http.StatusTooManyRequests, "2"},
		{"普通业务错误不输出 Retry-After", errs.ErrForbidden, http.StatusForbidden, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/x", nil)
			Error(c, tt.err)

			if w.Code != tt.status || w.Header().Get("Retry-After") != tt.retryAfter {
				t.Fatalf("得到 %d Retry-After=%q", w.Code, w.Header().Get("Retry-After"))
			}
			var body Envelope
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if want := errs.From(tt.err).Code; body.Code != want {
				t.Fatalf("业务码应为 %s，得到 %s", want, body.Code)
			}
		})
	}
}
