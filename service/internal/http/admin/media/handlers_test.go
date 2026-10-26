package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"uuid"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	adm "xsite/service/internal/domain/admin"
	dom "xsite/service/internal/domain/media"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/admin/content"
	"xsite/service/internal/http/middleware"
)

func init() {
	gin.SetMode(gin.TestMode)
	binding.EnableDecoderDisallowUnknownFields = true // 与 NewRouter 一致
}

const (
	origin = "http://localhost:5173"
	testUA = "Mozilla/5.0 Test"
)

var (
	now   = time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC)
	jobID = uuid.MustParse("0192f5a0-0000-7000-8000-000000000001")
)

type fakeAuth struct{ ids map[string]adm.Identity }

func (f fakeAuth) Login(context.Context, adm.LoginInput) (adm.LoginResult, error) {
	return adm.LoginResult{}, errs.ErrNeedLogin
}

func (f fakeAuth) LoginMFA(context.Context, adm.LoginMFAInput) (adm.LoginResult, error) {
	return adm.LoginResult{}, errs.ErrNeedLogin
}

func (f fakeAuth) Reauth(context.Context, adm.Identity, string, string, []byte, string) error {
	return nil
}

func (f fakeAuth) MFATicketAdmin(context.Context, string) (int64, error) { return 0, errs.ErrNeedLogin }

func (f fakeAuth) Authenticate(_ context.Context, token, _ string) (adm.Identity, error) {
	id, ok := f.ids[token]
	if !ok {
		return adm.Identity{}, errs.ErrNeedLogin
	}
	return id, nil
}

func (f fakeAuth) Logout(context.Context, string) error { return nil }

func who(name string, perms ...string) adm.Identity {
	id := adm.NewIdentity(int64(len(name)), name, adm.RoleRef{ID: 2, Code: "ops", Name: "运营", Active: true}, perms)
	id.CSRFToken = "csrf-" + name
	id.MFAEnabled = true
	return id
}

type fakeSvc struct {
	mu     sync.Mutex
	err    error
	in     dom.EnqueueInput
	ep     int64
	filter dom.JobFilter
	getID  string
}

func sampleJob() dom.Job {
	started := now.Add(time.Second)
	return dom.Job{
		ID: jobID, EpisodeID: 5, DramaID: 3, EpisodeNumber: 2, SourceKey: "fixtures/a.mp4", Status: dom.JobRunning, Progress: 40,
		Attempts: 1, KeyVer: 0, Renditions: []dom.Rendition{}, CreatedAt: now, StartedAt: &started, UpdatedAt: now,
	}
}

func (f *fakeSvc) Enqueue(_ context.Context, episodeID int64, in dom.EnqueueInput) (dom.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ep, f.in = episodeID, in
	j := sampleJob()
	j.EpisodeID, j.SourceKey, j.Force, j.Status = episodeID, in.SourceKey, in.Force, dom.JobQueued
	return j, f.err
}

func (f *fakeSvc) GetJob(_ context.Context, id string) (dom.Job, error) {
	f.getID = id
	j := sampleJob()
	j.Status, j.KeyVer, j.Poster = dom.JobSucceeded, 1, "posters/5/p.webp"
	j.Renditions = []dom.Rendition{{Name: "v360", Width: 360, Height: 640, Bitrate: 400000, PeakBitrate: 520000, Codecs: "avc1.64001e,mp4a.40.2"}}
	return j, f.err
}

func (f *fakeSvc) ListJobs(_ context.Context, filter dom.JobFilter) (dom.JobPage, error) {
	f.filter = filter
	return dom.JobPage{List: []dom.Job{sampleJob()}, Total: 21, Page: 2, PageSize: 20}, f.err
}

type memRecorder struct {
	mu      sync.Mutex
	entries []platform.AuditEntry
}

func (m *memRecorder) Record(e platform.AuditEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
}

type env struct {
	r     *gin.Engine
	svc   *fakeSvc
	audit *memRecorder
	ids   map[string]adm.Identity
}

// newEnv 与线上一样：admin.Routes 提供会话、CSRF、审计；同时注册内容接口，确认 /episodes 下的路由能共存。
func newEnv() *env {
	e := &env{svc: &fakeSvc{}, audit: &memRecorder{}, ids: map[string]adm.Identity{
		"viewer": who("viewer", "dramas.view"),
		"editor": who("editor", "dramas.view", "dramas.edit"),
		"nobody": who("nobody", "tags.view"),
	}}
	h := &admin.Handlers{Auth: fakeAuth{ids: e.ids}, Cookie: admin.NewSessionCookie(false)}
	e.r = gin.New()
	e.r.Use(middleware.RequestID())
	authed := h.Routes(e.r, []string{origin}, e.audit)
	(&content.Handlers{}).Routes(authed)
	(&Handlers{Svc: e.svc}).Routes(authed)
	return e
}

func (e *env) do(as, method, path, body string, csrf bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("User-Agent", testUA)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if as != "" {
		req.AddCookie(&http.Cookie{Name: "xs_admin", Value: as})
		if method != http.MethodGet {
			req.Header.Set("Origin", origin)
			if csrf {
				req.Header.Set(admin.HeaderCSRFToken, e.ids[as].CSRFToken)
			}
		}
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

type envelope struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func decode(t *testing.T, w *httptest.ResponseRecorder, status int, code string) envelope {
	t.Helper()
	var e envelope
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	if w.Code != status || e.Code != code {
		t.Fatalf("期望 %d %s，得到 %d %s：%s", status, code, w.Code, e.Code, w.Body.String())
	}
	return e
}

func TestEnqueue(t *testing.T) {
	e := newEnv()
	w := e.do("editor", http.MethodPost, "/api/admin/episodes/5/transcode", `{"source_key":"fixtures/a.mp4","force":true}`, true)
	res := decode(t, w, http.StatusOK, "OK")
	var got map[string]any
	_ = json.Unmarshal(res.Data, &got)
	if got["id"] != jobID.String() || got["episode_id"] != float64(5) || got["status"] != "queued" || got["force"] != true ||
		got["source_key"] != "fixtures/a.mp4" || got["started_at"] == nil || got["finished_at"] != nil {
		t.Fatalf("响应字段不符合预期：%v", got)
	}
	if rs, ok := got["renditions"].([]any); !ok || len(rs) != 0 {
		t.Fatalf("renditions 应为空数组而不是 null：%v", got["renditions"])
	}
	if e.svc.ep != 5 || e.svc.in != (dom.EnqueueInput{SourceKey: "fixtures/a.mp4", Force: true}) {
		t.Fatalf("参数没有传给服务：%d %+v", e.svc.ep, e.svc.in)
	}
	a := e.audit.entries[len(e.audit.entries)-1]
	if a.Action != "episode.transcode" || a.TargetType != "episode" || a.TargetID != "5" ||
		!strings.Contains(string(a.After), `"job_id":"`+jobID.String()+`"`) || !strings.Contains(string(a.After), `"source_key":"fixtures/a.mp4"`) {
		t.Fatalf("审计内容不符合预期：%+v %s", a, a.After)
	}

	// 不填源文件（重试）：空对象即可
	decode(t, e.do("editor", http.MethodPost, "/api/admin/episodes/5/transcode", `{}`, true), http.StatusOK, "OK")
	if e.svc.in != (dom.EnqueueInput{}) {
		t.Fatalf("空请求应沿用上次的源文件：%+v", e.svc.in)
	}
}

func TestEnqueue_Rejections(t *testing.T) {
	e := newEnv()
	path := "/api/admin/episodes/5/transcode"
	body := `{"source_key":"fixtures/a.mp4"}`
	decode(t, e.do("", http.MethodPost, path, body, true), http.StatusUnauthorized, "NEED_LOGIN")
	decode(t, e.do("viewer", http.MethodPost, path, body, true), http.StatusForbidden, "FORBIDDEN")
	decode(t, e.do("nobody", http.MethodPost, path, body, true), http.StatusForbidden, "FORBIDDEN")
	decode(t, e.do("editor", http.MethodPost, path, body, false), http.StatusForbidden, "FORBIDDEN") // 缺 CSRF
	decode(t, e.do("editor", http.MethodPost, path, `{"source":"x"}`, true), http.StatusBadRequest, "BAD_REQUEST")
	decode(t, e.do("editor", http.MethodPost, path, ``, true), http.StatusBadRequest, "BAD_REQUEST")
	decode(t, e.do("editor", http.MethodPost, "/api/admin/episodes/abc/transcode", body, true), http.StatusNotFound, "NOT_FOUND")

	e.svc.err = dom.ErrActive
	res := decode(t, e.do("editor", http.MethodPost, path, body, true), http.StatusConflict, "CONFLICT")
	if res.Message != dom.ErrActive.Message {
		t.Fatalf("409 应说明原因：%s", res.Message)
	}
	e.svc.err = errs.ErrNotFound
	decode(t, e.do("editor", http.MethodPost, path, body, true), http.StatusNotFound, "NOT_FOUND")
	if len(e.audit.entries) != 0 {
		t.Fatalf("失败的请求不记审计：%+v", e.audit.entries)
	}
}

func TestListAndGetJobs(t *testing.T) {
	e := newEnv()
	res := decode(t, e.do("viewer", http.MethodGet, "/api/admin/transcode-jobs?drama_id=3&episode_id=5&status=failed&page=2&page_size=20", "", false),
		http.StatusOK, "OK")
	var page struct {
		List     []map[string]any `json:"list"`
		Total    int64            `json:"total"`
		Page     int              `json:"page"`
		PageSize int              `json:"page_size"`
	}
	_ = json.Unmarshal(res.Data, &page)
	if page.Total != 21 || page.Page != 2 || len(page.List) != 1 || page.List[0]["drama_id"] != float64(3) {
		t.Fatalf("分页结果不符合预期：%+v", page)
	}
	if e.svc.filter != (dom.JobFilter{DramaID: 3, EpisodeID: 5, Status: dom.JobFailed, Page: 2, PageSize: 20}) {
		t.Fatalf("筛选参数不对：%+v", e.svc.filter)
	}
	decode(t, e.do("viewer", http.MethodGet, "/api/admin/transcode-jobs?page=abc", "", false), http.StatusBadRequest, "BAD_REQUEST")
	decode(t, e.do("nobody", http.MethodGet, "/api/admin/transcode-jobs", "", false), http.StatusForbidden, "FORBIDDEN")

	res = decode(t, e.do("viewer", http.MethodGet, "/api/admin/transcode-jobs/"+jobID.String(), "", false), http.StatusOK, "OK")
	var job map[string]any
	_ = json.Unmarshal(res.Data, &job)
	rs, _ := job["renditions"].([]any)
	if e.svc.getID != jobID.String() || job["status"] != "succeeded" || job["key_ver"] != float64(1) || len(rs) != 1 ||
		rs[0].(map[string]any)["codecs"] != "avc1.64001e,mp4a.40.2" {
		t.Fatalf("单个任务：%v", job)
	}
	e.svc.err = errs.ErrNotFound
	decode(t, e.do("viewer", http.MethodGet, "/api/admin/transcode-jobs/nope", "", false), http.StatusNotFound, "NOT_FOUND")
}
