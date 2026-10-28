package upload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"uuid"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	adm "xsite/service/internal/domain/admin"
	mediadom "xsite/service/internal/domain/media"
	"xsite/service/internal/domain/platform"
	dom "xsite/service/internal/domain/upload"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/middleware"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/uploadstore"
)

func init() {
	gin.SetMode(gin.TestMode)
	binding.EnableDecoderDisallowUnknownFields = true // 与 NewRouter 一致
}

// ---------- recordingStore 的分片上传实现（S3 语义：同一片号后写覆盖先写） ----------

func (s *recordingStore) CreateMultipart(_ context.Context, key string, _ blob.PutOptions) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := fmt.Sprintf("mp-%d", s.nextID)
	s.uploads[id] = map[int32][]byte{}
	s.uploadKey[id] = key
	return id, nil
}

func (s *recordingStore) UploadPart(_ context.Context, key, uploadID string, number int32, body io.ReadSeeker, size int64) (string, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.partErr; err != nil {
		s.partErr = nil
		return "", err
	}
	parts, ok := s.uploads[uploadID]
	if !ok || s.uploadKey[uploadID] != key {
		return "", fmt.Errorf("blob: 上传第 %d 片 %s: %w", number, key, blob.ErrNotFound)
	}
	if int64(len(data)) != size {
		return "", fmt.Errorf("第 %d 片长度 %d 与声明的 %d 不一致", number, len(data), size)
	}
	parts[number] = data
	return fmt.Sprintf(`"etag-%d-%d"`, number, len(data)), nil
}

func (s *recordingStore) ListParts(_ context.Context, key, uploadID string) ([]blob.Part, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parts, ok := s.uploads[uploadID]
	if !ok || s.uploadKey[uploadID] != key {
		return nil, fmt.Errorf("blob: 列出分片 %s: %w", key, blob.ErrNotFound)
	}
	out := make([]blob.Part, 0, len(parts))
	for n, data := range parts {
		out = append(out, blob.Part{Number: n, ETag: fmt.Sprintf(`"etag-%d-%d"`, n, len(data)), Size: int64(len(data))})
	}
	slices.SortFunc(out, func(a, b blob.Part) int { return int(a.Number - b.Number) })
	return out, nil
}

func (s *recordingStore) CompleteMultipart(_ context.Context, key, uploadID string, parts []blob.Part) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.uploads[uploadID]
	if !ok || s.uploadKey[uploadID] != key {
		return fmt.Errorf("blob: 完成分片上传 %s: %w", key, blob.ErrNotFound)
	}
	var buf bytes.Buffer
	for _, p := range parts {
		buf.Write(stored[p.Number])
	}
	s.objects[key] = buf.Bytes()
	delete(s.uploads, uploadID)
	return nil
}

func (s *recordingStore) AbortMultipart(_ context.Context, key, uploadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.uploads[uploadID]; !ok {
		return fmt.Errorf("blob: 放弃分片上传 %s: %w", key, blob.ErrNotFound)
	}
	delete(s.uploads, uploadID)
	s.aborted = append(s.aborted, uploadID)
	return nil
}

// ---------- 内存版会话仓储：语义照抄 db/queries/admin_media_upload.sql 的 WHERE 条件 ----------

type memUploadRepo struct {
	mu   sync.Mutex
	rows map[uuid.UUID]dom.Asset
}

func newMemUploadRepo() *memUploadRepo { return &memUploadRepo{rows: map[uuid.UUID]dom.Asset{}} }

func (r *memUploadRepo) update(id uuid.UUID, ok func(dom.Asset) bool, apply func(*dom.Asset)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, found := r.rows[id]
	if !found || !ok(a) {
		return errs.ErrBadRequest.WithMessage("上传会话状态已变化，请重新选择文件")
	}
	apply(&a)
	a.UpdatedAt = time.Now()
	r.rows[id] = a
	return nil
}

func (r *memUploadRepo) live(a dom.Asset) bool { return a.ExpiresAt.After(time.Now()) }

func (r *memUploadRepo) Create(_ context.Context, a dom.Asset) (dom.Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows[a.ID] = a
	return a, nil
}

func (r *memUploadRepo) Get(_ context.Context, id uuid.UUID) (dom.Asset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.rows[id]
	if !ok {
		return dom.Asset{}, errs.ErrNotFound
	}
	return a, nil
}

func (r *memUploadRepo) ClaimWrite(_ context.Context, id uuid.UUID, adminID int64) error {
	return r.update(id, func(a dom.Asset) bool { return a.AdminID == adminID && a.Status == dom.StatusCreated && r.live(a) },
		func(a *dom.Asset) { a.Status = dom.StatusUploading })
}

func (r *memUploadRepo) SetMultipart(_ context.Context, id uuid.UUID, adminID int64, uploadID string) error {
	return r.update(id, func(a dom.Asset) bool {
		return a.AdminID == adminID && a.Status == dom.StatusCreated && a.PartSize > 0 && a.MultipartUploadID == ""
	}, func(a *dom.Asset) { a.MultipartUploadID = uploadID })
}

func (r *memUploadRepo) TouchPart(_ context.Context, id uuid.UUID, adminID int64, expiresAt time.Time) error {
	return r.update(id, func(a dom.Asset) bool {
		return a.AdminID == adminID && (a.Status == dom.StatusCreated || a.Status == dom.StatusUploading) && a.MultipartUploadID != "" && r.live(a)
	}, func(a *dom.Asset) { a.Status, a.ExpiresAt = dom.StatusUploading, expiresAt })
}

func (r *memUploadRepo) MarkUploaded(_ context.Context, id uuid.UUID, adminID int64) error {
	return r.update(id, func(a dom.Asset) bool { return a.AdminID == adminID && a.Status == dom.StatusUploading && r.live(a) },
		func(a *dom.Asset) { a.Status, a.Progress = dom.StatusUploaded, 100 })
}

func (r *memUploadRepo) Fail(_ context.Context, id uuid.UUID, adminID int64, message string) error {
	return r.update(id, func(a dom.Asset) bool {
		return a.AdminID == adminID && slices.Contains([]dom.Status{dom.StatusCreated, dom.StatusUploading, dom.StatusUploaded}, a.Status)
	}, func(a *dom.Asset) { a.Status, a.Error = dom.StatusFailed, message })
}

func (r *memUploadRepo) CompleteImage(_ context.Context, id uuid.UUID, adminID int64) error {
	return r.update(id, func(a dom.Asset) bool {
		return a.AdminID == adminID && a.Kind == dom.KindImage && a.Status == dom.StatusUploaded
	},
		func(a *dom.Asset) { a.Status, a.OutputKey = dom.StatusReady, a.SourceKey })
}

func (r *memUploadRepo) LinkEpisodeTranscode(_ context.Context, id uuid.UUID, adminID int64, jobID uuid.UUID) error {
	return r.update(id, func(a dom.Asset) bool {
		return a.AdminID == adminID && a.Purpose == dom.PurposeEpisodeVideo && a.Status == dom.StatusUploaded
	}, func(a *dom.Asset) { a.Status, a.TranscodeJobID = dom.StatusRunning, &jobID })
}

func (r *memUploadRepo) QueueAdVideo(_ context.Context, id uuid.UUID) error {
	return r.update(id, func(a dom.Asset) bool { return a.Purpose == dom.PurposeAdVideo && a.Status == dom.StatusUploaded },
		func(a *dom.Asset) { a.Status = dom.StatusQueued })
}

func (r *memUploadRepo) StartAdVideo(context.Context, uuid.UUID) (dom.Asset, error) {
	return dom.Asset{}, dom.ErrJobGone
}
func (r *memUploadRepo) UpdateAdVideoProgress(context.Context, uuid.UUID, int) error { return nil }
func (r *memUploadRepo) RequeueAdVideo(context.Context, uuid.UUID, string) error     { return nil }
func (r *memUploadRepo) CompleteAdVideo(context.Context, uuid.UUID, string) error    { return nil }
func (r *memUploadRepo) FailAdVideo(context.Context, uuid.UUID, string) error        { return nil }
func (r *memUploadRepo) ListExpired(context.Context, int) ([]dom.Asset, error)       { return nil, nil }
func (r *memUploadRepo) DeleteExpired(context.Context, uuid.UUID) error              { return nil }
func (r *memUploadRepo) get(id uuid.UUID) dom.Asset {
	a, _ := r.Get(context.Background(), id)
	return a
}

// ---------- 其余依赖的桩 ----------

type primaryResolverStub struct{ store blob.UploadStore }

func (s primaryResolverStub) Primary(context.Context, dom.Kind) (uploadstore.Target, error) {
	return uploadstore.Target{ProviderID: 7, Endpoint: "https://r2.example", Region: "auto", Bucket: "media", Store: s.store}, nil
}
func (s primaryResolverStub) Existing(context.Context, dom.Asset) (uploadstore.Target, error) {
	return uploadstore.Target{ProviderID: 7, Store: s.store}, nil
}
func (s primaryResolverStub) Cleanup(context.Context, dom.Asset) (uploadstore.Target, error) {
	return uploadstore.Target{ProviderID: 7, Store: s.store}, nil
}

type enqueueStub struct {
	mu   sync.Mutex
	in   []mediadom.EnqueueInput
	jobs []uuid.UUID
}

func (e *enqueueStub) Enqueue(_ context.Context, episodeID int64, in mediadom.EnqueueInput) (mediadom.Job, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	id := uuid.NewV7()
	e.in, e.jobs = append(e.in, in), append(e.jobs, id)
	return mediadom.Job{ID: id, EpisodeID: episodeID, SourceKey: in.SourceKey, Status: mediadom.JobQueued}, nil
}

type uploadAuthStub struct{ ids map[string]adm.Identity }

func (f uploadAuthStub) Login(context.Context, adm.LoginInput) (adm.LoginResult, error) {
	return adm.LoginResult{}, errs.ErrNeedLogin
}
func (f uploadAuthStub) LoginMFA(context.Context, adm.LoginMFAInput) (adm.LoginResult, error) {
	return adm.LoginResult{}, errs.ErrNeedLogin
}
func (f uploadAuthStub) Reauth(context.Context, adm.Identity, string, string, []byte, string) error {
	return nil
}
func (f uploadAuthStub) MFATicketAdmin(context.Context, string) (int64, error) {
	return 0, errs.ErrNeedLogin
}
func (f uploadAuthStub) Authenticate(_ context.Context, token, _ string) (adm.Identity, error) {
	if id, ok := f.ids[token]; ok {
		return id, nil
	}
	return adm.Identity{}, errs.ErrNeedLogin
}
func (f uploadAuthStub) Logout(context.Context, string) error { return nil }

type uploadAuditStub struct {
	mu      sync.Mutex
	entries []platform.AuditEntry
}

func (m *uploadAuditStub) Record(e platform.AuditEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
}

func (m *uploadAuditStub) actions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.entries))
	for i, e := range m.entries {
		out[i] = e.Action
	}
	return out
}

const uploadTestOrigin = "http://localhost:5173"

type uploadEnv struct {
	r       *gin.Engine
	repo    *memUploadRepo
	stage   *recordingStore
	primary *recordingStore
	media   *enqueueStub
	audit   *uploadAuditStub
	editor  adm.Identity
}

// newUploadEnv 与线上一样：admin.Routes 提供会话、CSRF、审计，再挂上传接口；
// BodyLimit / LongRequest 也照 NewRouter 的顺序装上，分片路由的请求体上限是这里要验的一部分。
func newUploadEnv() *uploadEnv {
	editor := adm.NewIdentity(9, "editor", adm.RoleRef{ID: 2, Code: "ops", Name: "运营", Active: true}, []string{"dramas.view", "dramas.edit", "ads.edit"})
	editor.CSRFToken, editor.MFAEnabled = "csrf-editor", true
	e := &uploadEnv{repo: newMemUploadRepo(), stage: newRecordingStore(), primary: newRecordingStore(), media: &enqueueStub{},
		audit: &uploadAuditStub{}, editor: editor}
	auth := &admin.Handlers{Auth: uploadAuthStub{ids: map[string]adm.Identity{"editor": editor}}, Cookie: admin.NewSessionCookie(false)}
	e.r = gin.New()
	e.r.Use(middleware.RequestID(), middleware.LongRequest(), middleware.BodyLimit(1<<20))
	authed := auth.Routes(e.r, []string{uploadTestOrigin}, e.audit)
	(&Handlers{Svc: dom.NewService(e.repo), Media: e.media, Stores: primaryResolverStub{store: e.primary}, Source: e.stage,
		PartSlots: make(chan struct{}, 2)}).Routes(authed)
	return e
}

func (e *uploadEnv) call(method, path string, body []byte, contentType string) (int, map[string]any) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("User-Agent", "Mozilla/5.0 Test")
	req.Header.Set("Origin", uploadTestOrigin)
	req.Header.Set(admin.HeaderCSRFToken, e.editor.CSRFToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.AddCookie(&http.Cookie{Name: "xs_admin", Value: "editor"})
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	var env struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &env)
	out := env.Data
	if out == nil {
		out = map[string]any{}
	}
	out["_code"], out["_message"] = env.Code, env.Message
	return w.Code, out
}

func (e *uploadEnv) create(t *testing.T, body string) (uuid.UUID, map[string]any) {
	t.Helper()
	status, got := e.call(http.MethodPost, "/api/admin/uploads", []byte(body), "application/json")
	if status != http.StatusOK {
		t.Fatalf("建会话失败：%d %v", status, got)
	}
	id, err := uuid.Parse(got["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return id, got
}

func (e *uploadEnv) putPart(id uuid.UUID, n int, data []byte) (int, map[string]any) {
	return e.call(http.MethodPut, fmt.Sprintf("/api/admin/uploads/%s/parts/%d", id, n), data, "application/octet-stream")
}

func videoPayload(size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i*7 + i/4096)
	}
	return data
}

func partOf(data []byte, n int) []byte {
	ps := int(dom.VideoPartSize)
	start := (n - 1) * ps
	return data[start:min(start+ps, len(data))]
}

// 完整链路：建会话声明 multipart → 乱序并发提交分片（含重传）→ 完成 → 暂存对象逐字节等于原文件、
// 转封装任务拿到的正是这个暂存 key；审计只记建会话与完成两条，820 片的大文件也不会刷出 820 条审计。
func TestMultipartVideoUploadEndToEnd(t *testing.T) {
	e := newUploadEnv()
	data := videoPayload(2*int(dom.VideoPartSize) + 4321)
	id, created := e.create(t, fmt.Sprintf(`{"purpose":"episode_video","mime":"video/mp4","size_bytes":%d,"episode_id":11,"multipart":true}`, len(data)))
	if created["part_size"] != float64(dom.VideoPartSize) || created["part_count"] != float64(3) {
		t.Fatalf("应返回分片大小与片数：%v", created)
	}

	var wg sync.WaitGroup
	results := make([]int, 4)
	for i, n := range []int{3, 1, 2, 2} {
		wg.Go(func() { results[i], _ = e.putPart(id, n, partOf(data, n)) })
	}
	wg.Wait()
	for i, status := range results {
		if status != http.StatusOK {
			t.Fatalf("第 %d 个分片请求得到 %d", i, status)
		}
	}

	status, done := e.call(http.MethodPost, fmt.Sprintf("/api/admin/uploads/%s/complete", id), nil, "")
	if status != http.StatusOK || done["status"] != "running" || done["transcode_job_id"] == nil {
		t.Fatalf("完成后应进入转封装：%d %v", status, done)
	}
	stageKey := dom.StageKey(7, e.repo.get(id).SourceKey)
	if got, ok := e.stage.object(stageKey); !ok || !bytes.Equal(got, data) {
		t.Fatalf("暂存对象应逐字节等于原文件：存在=%v 长度=%d/%d", ok, len(got), len(data))
	}
	if len(e.media.in) != 1 || e.media.in[0].SourceKey != stageKey {
		t.Fatalf("转封装任务应读暂存 key：%+v", e.media.in)
	}
	if len(e.primary.objects) != 0 {
		t.Fatal("视频源不应写入主存储")
	}
	if got := e.audit.actions(); !slices.Equal(got, []string{"media_upload.create", "media_upload.complete"}) {
		t.Fatalf("分片不应单独记审计：%v", got)
	}
}

func TestPutPartRejectsWrongNumberLengthAndSession(t *testing.T) {
	e := newUploadEnv()
	data := videoPayload(int(dom.VideoPartSize) + 10)
	id, _ := e.create(t, fmt.Sprintf(`{"purpose":"ad_video","mime":"video/mp4","size_bytes":%d,"multipart":true}`, len(data)))
	for _, tc := range []struct {
		name string
		n    int
		body []byte
		want string
	}{
		{"片号 0", 0, partOf(data, 1), "分片序号"},
		{"片号越界", 3, data[:10], "分片序号"},
		{"第一片少一个字节", 1, partOf(data, 1)[1:], "应为"},
		{"最后一片按整片提交", 2, partOf(data, 1), "应为"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if status, got := e.putPart(id, tc.n, tc.body); status != http.StatusBadRequest || !strings.Contains(got["_message"].(string), tc.want) {
				t.Fatalf("应 400「%s」，得到 %d %v", tc.want, status, got)
			}
		})
	}
	if a := e.repo.get(id); a.Status != dom.StatusCreated {
		t.Fatalf("被拒绝的分片不应改变会话：%s", a.Status)
	}

	plain, _ := e.create(t, `{"purpose":"ad_video","mime":"video/mp4","size_bytes":10}`)
	if status, got := e.putPart(plain, 1, data[:10]); status != http.StatusBadRequest || !strings.Contains(got["_message"].(string), "不是分片上传") {
		t.Fatalf("整段上传的会话不接受分片：%d %v", status, got)
	}
	// 反过来，分片会话也不能再走整段 PUT
	status, got := e.call(http.MethodPut, fmt.Sprintf("/api/admin/uploads/%s/content", id), data, "video/mp4")
	if status != http.StatusBadRequest || !strings.Contains(got["_message"].(string), "分片上传") {
		t.Fatalf("分片会话不接受整段 PUT：%d %v", status, got)
	}
	if status, _ := e.call(http.MethodPost, "/api/admin/uploads", []byte(`{"purpose":"drama_cover","mime":"image/webp","image_format":"webp","size_bytes":10,"multipart":true}`), "application/json"); status != http.StatusBadRequest {
		t.Fatalf("图片不接受分片上传，得到 %d", status)
	}
}

// 缺片时完成只报错、不改会话：浏览器补传后再次完成即可，不必从头重传。
func TestCompleteReportsMissingPartsThenSucceedsAfterResend(t *testing.T) {
	e := newUploadEnv()
	data := videoPayload(2*int(dom.VideoPartSize) + 1)
	id, _ := e.create(t, fmt.Sprintf(`{"purpose":"ad_video","mime":"video/mp4","size_bytes":%d,"multipart":true}`, len(data)))
	for _, n := range []int{1, 3} {
		if status, got := e.putPart(id, n, partOf(data, n)); status != http.StatusOK {
			t.Fatalf("第 %d 片：%d %v", n, status, got)
		}
	}
	complete := fmt.Sprintf("/api/admin/uploads/%s/complete", id)
	if status, got := e.call(http.MethodPost, complete, nil, ""); status != http.StatusBadRequest || !strings.Contains(got["_message"].(string), "第 2 片") {
		t.Fatalf("缺第 2 片应 400 并指明片号：%d %v", status, got)
	}
	if a := e.repo.get(id); a.Status != dom.StatusUploading {
		t.Fatalf("缺片时会话应保持 uploading：%s", a.Status)
	}
	if status, _ := e.putPart(id, 2, partOf(data, 2)); status != http.StatusOK {
		t.Fatal("补传第 2 片失败")
	}
	if status, got := e.call(http.MethodPost, complete, nil, ""); status != http.StatusOK || got["status"] != "queued" {
		t.Fatalf("补齐后广告视频应进入转码队列：%d %v", status, got)
	}
}

// 对象存储偶发失败返回 5xx（浏览器据此重试这一片），重试成功后照常完成。
func TestPutPartStorageErrorIsRetryable(t *testing.T) {
	e := newUploadEnv()
	data := videoPayload(1000)
	id, _ := e.create(t, `{"purpose":"ad_video","mime":"video/mp4","size_bytes":1000,"multipart":true}`)
	e.stage.partErr = errors.New("R2 503 SlowDown")
	if status, got := e.putPart(id, 1, data); status != http.StatusInternalServerError {
		t.Fatalf("对象存储失败应 500（可重试），得到 %d %v", status, got)
	}
	if status, _ := e.putPart(id, 1, data); status != http.StatusOK {
		t.Fatal("重试应成功")
	}
	if status, got := e.call(http.MethodPost, fmt.Sprintf("/api/admin/uploads/%s/complete", id), nil, ""); status != http.StatusOK {
		t.Fatalf("重试后应能完成：%d %v", status, got)
	}
}

// 分片上传在存储侧已失效（被清理任务放弃）时返回 400，浏览器不再无谓重试。
func TestPutPartAfterMultipartAbortedIsFinal(t *testing.T) {
	e := newUploadEnv()
	id, _ := e.create(t, `{"purpose":"ad_video","mime":"video/mp4","size_bytes":100,"multipart":true}`)
	a := e.repo.get(id)
	if err := e.stage.AbortMultipart(context.Background(), dom.StageKey(a.ProviderID, a.SourceKey), a.MultipartUploadID); err != nil {
		t.Fatal(err)
	}
	if status, got := e.putPart(id, 1, videoPayload(100)); status != http.StatusBadRequest || !strings.Contains(got["_message"].(string), "已失效") {
		t.Fatalf("分片上传失效后应 400：%d %v", status, got)
	}
}

// 缓冲名额满时，新的分片在请求取消后放弃等待，不会无限挂住。
func TestAcquirePartSlotGivesUpWhenRequestCancelled(t *testing.T) {
	h := &Handlers{PartSlots: make(chan struct{}, 1)}
	release, err := h.acquirePartSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := h.acquirePartSlot(ctx); err == nil {
		t.Fatal("名额已满且请求已取消时应放弃")
	}
	release()
	if release2, err := h.acquirePartSlot(context.Background()); err != nil {
		t.Fatal("释放后应能再次占用")
	} else {
		release2()
	}
}
