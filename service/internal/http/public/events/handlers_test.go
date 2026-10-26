package events

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/analytics"
)

type fakeSvc struct {
	got dom.CollectInput
}

func (f *fakeSvc) Collect(_ context.Context, in dom.CollectInput) (int, error) {
	f.got = in
	return len(in.Events), nil
}

type fakeLimiter struct{ total int64 }

func (f *fakeLimiter) Add(_ context.Context, _ string, delta int64, _ time.Duration) (int64, error) {
	f.total += delta
	return f.total, nil
}

func setup(svc Service, lim Limiter) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	(&Handlers{Svc: svc, Limiter: lim}).Routes(r)
	return r
}

func post(r *gin.Engine, body, device string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("content-type", "application/json")
	if device != "" {
		req.Header.Set("X-Device-Id", device)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCollectHandler_OK(t *testing.T) {
	svc := &fakeSvc{}
	w := post(setup(svc, &fakeLimiter{}), `{"client":"web","events":[{"type":"play_start","drama_id":7}]}`, "dev-1")
	if w.Code != http.StatusNoContent {
		t.Fatalf("应返回 204，得 %d", w.Code)
	}
	if svc.got.DeviceID != "dev-1" || svc.got.Client != "web" || len(svc.got.Events) != 1 {
		t.Fatalf("透传给 service 的输入不符：%+v", svc.got)
	}
	if svc.got.Events[0].DramaID == nil || *svc.got.Events[0].DramaID != 7 {
		t.Error("drama_id 未透传")
	}
}

func TestCollectHandler_BadJSON(t *testing.T) {
	w := post(setup(&fakeSvc{}, &fakeLimiter{}), `not json`, "dev")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("坏 JSON 应返回 400，得 %d", w.Code)
	}
}

func TestCollectHandler_EmptyEvents(t *testing.T) {
	w := post(setup(&fakeSvc{}, &fakeLimiter{}), `{"events":[]}`, "dev")
	if w.Code != http.StatusNoContent {
		t.Fatalf("空事件应返回 204，得 %d", w.Code)
	}
}

func TestCollectHandler_RateLimited(t *testing.T) {
	svc := &fakeSvc{}
	// 限流器已达配额上限：本批被整体静默丢弃，返回 204 且不调用 service
	w := post(setup(svc, &fakeLimiter{total: eventsPerMinute}), `{"events":[{"type":"visitor"}]}`, "dev")
	if w.Code != http.StatusNoContent {
		t.Fatalf("超配额应返回 204，得 %d", w.Code)
	}
	if len(svc.got.Events) != 0 {
		t.Error("超配额时不应调用 service")
	}
}
