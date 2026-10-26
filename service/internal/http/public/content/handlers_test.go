package content

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
)

// 前台内容读接口的 HTTP 层测试：参数解析、状态码与业务码、缓存头与条件请求。

type fakeService struct {
	err error

	// 记录收到的参数，供断言。
	dramaID  int64
	slug     string
	list     dom.ListRequest
	channel  dom.ChannelRequest
	explore  dom.ExploreRequest
	etag     string
	topicNum int
}

func (f *fakeService) Drama(_ context.Context, id int64) (dom.Doc[dom.DramaDetailView], error) {
	f.dramaID = id
	return doc(f, dom.DramaDetailView{DramaCard: dom.DramaCard{ID: id, Title: "总裁"}})
}

func (f *fakeService) Channel(_ context.Context, slug string, in dom.ChannelRequest) (dom.Doc[dom.ChannelPage], error) {
	f.slug, f.channel = slug, in
	return doc(f, dom.ChannelPage{Channel: dom.ChannelView{Slug: slug}, Page: in.Page})
}

func (f *fakeService) Tag(_ context.Context, slug string, in dom.ListRequest) (dom.Doc[dom.TagPage], error) {
	f.slug, f.list = slug, in
	return doc(f, dom.TagPage{Tag: dom.TagInfo{Slug: slug}})
}

func (f *fakeService) Explore(_ context.Context, in dom.ExploreRequest) (dom.Doc[dom.ExplorePage], error) {
	f.explore = in
	return doc(f, dom.ExplorePage{List: []dom.DramaCard{}})
}

func (f *fakeService) Topics(_ context.Context, in dom.ListRequest) (dom.Doc[dom.TopicListPage], error) {
	f.list = in
	f.topicNum++
	return doc(f, dom.TopicListPage{List: []dom.TopicCard{}})
}

func (f *fakeService) Topic(_ context.Context, slug string) (dom.Doc[dom.TopicDetailView], error) {
	f.slug = slug
	return doc(f, dom.TopicDetailView{TopicCard: dom.TopicCard{Slug: slug}})
}

func doc[T any](f *fakeService, payload T) (dom.Doc[T], error) {
	if f.err != nil {
		return dom.Doc[T]{}, f.err
	}
	etag := f.etag
	if etag == "" {
		etag = `W/"test"`
	}
	return dom.Doc[T]{ETag: etag, Payload: payload}, nil
}

func newRouter(h *Handlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RequestID())
	h.Routes(r.Group("/api/v1"))
	return r
}

func get(t *testing.T, h *Handlers, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	newRouter(h).ServeHTTP(w, req)
	return w
}

func body(t *testing.T, w *httptest.ResponseRecorder) struct {
	Code string          `json:"code"`
	Data json.RawMessage `json:"data"`
} {
	t.Helper()
	var out struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	return out
}

func TestHandlers_CacheHeadersAndETag(t *testing.T) {
	for _, tt := range []struct {
		path      string
		cacheCtrl string
	}{
		{"/api/v1/dramas/7", "public, max-age=0, s-maxage=120, stale-while-revalidate=300"},
		{"/api/v1/channels/duanju", "public, max-age=0, s-maxage=60, stale-while-revalidate=300"},
		{"/api/v1/tags/sweet", "public, max-age=0, s-maxage=60, stale-while-revalidate=300"},
		{"/api/v1/explore", "public, max-age=0, s-maxage=30, stale-while-revalidate=300"},
		{"/api/v1/topics", "public, max-age=0, s-maxage=300, stale-while-revalidate=300"},
		{"/api/v1/topics/sweet", "public, max-age=0, s-maxage=300, stale-while-revalidate=300"},
	} {
		svc := &fakeService{etag: `W/"abc"`}
		w := get(t, &Handlers{Svc: svc}, tt.path, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s：期望 200，得到 %d %s", tt.path, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Cache-Control"); got != tt.cacheCtrl {
			t.Errorf("%s：缓存头 %q，期望 %q", tt.path, got, tt.cacheCtrl)
		}
		if got := w.Header().Get("ETag"); got != `W/"abc"` {
			t.Errorf("%s：ETag %q", tt.path, got)
		}
		if body(t, w).Code != "OK" {
			t.Errorf("%s：业务码应为 OK：%s", tt.path, w.Body.String())
		}

		// 带上同样的 ETag 时返回 304 且无响应体
		w = get(t, &Handlers{Svc: svc}, tt.path, map[string]string{"If-None-Match": `W/"abc"`})
		if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
			t.Errorf("%s：期望 304 无响应体，得到 %d %q", tt.path, w.Code, w.Body.String())
		}
	}
}

func TestHandlers_PathParams(t *testing.T) {
	svc := &fakeService{}
	h := &Handlers{Svc: svc}

	if w := get(t, h, "/api/v1/dramas/42", nil); w.Code != http.StatusOK || svc.dramaID != 42 {
		t.Fatalf("剧编号未解析：%d（%d）", svc.dramaID, w.Code)
	}
	for _, path := range []string{"/api/v1/dramas/abc", "/api/v1/dramas/0", "/api/v1/dramas/-1"} {
		w := get(t, h, path, nil)
		if w.Code != http.StatusNotFound || body(t, w).Code != "NOT_FOUND" {
			t.Errorf("%s：期望 404 NOT_FOUND，得到 %d %s", path, w.Code, w.Body.String())
		}
	}
	if w := get(t, h, "/api/v1/tags/sweet", nil); w.Code != http.StatusOK || svc.slug != "sweet" {
		t.Fatalf("标签 slug 未解析：%q", svc.slug)
	}
	if w := get(t, h, "/api/v1/topics/my-topic", nil); w.Code != http.StatusOK || svc.slug != "my-topic" {
		t.Fatalf("专题 slug 未解析：%q", svc.slug)
	}
	// /topics 与 /topics/:slug 是两个接口，不能互相抢路由
	if w := get(t, h, "/api/v1/topics", nil); w.Code != http.StatusOK || svc.topicNum != 1 {
		t.Fatalf("专题列表未命中：%d %d", w.Code, svc.topicNum)
	}
}

func TestHandlers_QueryParams(t *testing.T) {
	svc := &fakeService{}
	h := &Handlers{Svc: svc}

	w := get(t, h, "/api/v1/explore?terms=genre:romance,region:cn&tag=sweet&finished=true&window=7d&sort=hot&limit=10&cursor=abc", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d %s", w.Code, w.Body.String())
	}
	in := svc.explore
	if in.Terms != "genre:romance,region:cn" || in.Tag != "sweet" || in.Window != "7d" ||
		in.Sort != dom.SortHot || in.Limit != 10 || in.Cursor != "abc" {
		t.Fatalf("探索参数未正确解析：%+v", in)
	}
	if in.Finished == nil || !*in.Finished {
		t.Fatalf("完结筛选未解析：%v", in.Finished)
	}
	// 不传完结筛选时为「不限」
	get(t, h, "/api/v1/explore", nil)
	if svc.explore.Finished != nil {
		t.Fatalf("不传时应为不限：%v", svc.explore.Finished)
	}

	get(t, h, "/api/v1/channels/duanju?sort=recommend&page=2&page_size=12", nil)
	if svc.channel.Sort != dom.SortRecommend || svc.channel.Page != 2 || svc.channel.PageSize != 12 {
		t.Fatalf("频道参数未正确解析：%+v", svc.channel)
	}

	get(t, h, "/api/v1/tags/sweet?sort=random&limit=5&cursor=xyz", nil)
	if svc.list.Sort != dom.SortRandom || svc.list.Limit != 5 || svc.list.Cursor != "xyz" {
		t.Fatalf("标签参数未正确解析：%+v", svc.list)
	}

	// 类型不对的查询参数由 HTTP 层拒绝
	for _, path := range []string{"/api/v1/explore?limit=abc", "/api/v1/channels/x?page=abc", "/api/v1/explore?finished=maybe"} {
		w := get(t, h, path, nil)
		if w.Code != http.StatusBadRequest || body(t, w).Code != "BAD_REQUEST" {
			t.Errorf("%s：期望 400 BAD_REQUEST，得到 %d %s", path, w.Code, w.Body.String())
		}
	}
}

func TestHandlers_ErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"不可见的内容", errs.ErrNotFound, http.StatusNotFound, "NOT_FOUND"},
		{"参数无效", errs.ErrBadRequest.WithMessage("排序方式无效"), http.StatusBadRequest, "BAD_REQUEST"},
		{"未知错误不泄露细节", errors.New("pq: connection refused"), http.StatusInternalServerError, "INTERNAL"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handlers{Svc: &fakeService{err: tt.err}}
			for _, path := range []string{"/api/v1/dramas/1", "/api/v1/channels/a", "/api/v1/tags/a",
				"/api/v1/explore", "/api/v1/topics", "/api/v1/topics/a"} {
				w := get(t, h, path, nil)
				got := body(t, w)
				if w.Code != tt.status || got.Code != tt.code {
					t.Errorf("%s：得到 %d %s", path, w.Code, w.Body.String())
				}
			}
		})
	}
}

// ---------- 搜索与联想（S3-4）----------

// fakeSearch 记录收到的参数，并可指定返回的错误。
type fakeSearch struct {
	req     dom.SearchRequest
	sugQ    string
	sugN    int
	etag    string
	err     error
	sugErr  error
	results int
}

func (f *fakeSearch) Search(_ context.Context, in dom.SearchRequest) (dom.Doc[dom.SearchPage], error) {
	f.req = in
	if f.err != nil {
		return dom.Doc[dom.SearchPage]{}, f.err
	}
	list := make([]dom.DramaCard, 0, f.results)
	for i := range f.results {
		list = append(list, dom.DramaCard{ID: int64(i + 1), Title: "总裁"})
	}
	return dom.Doc[dom.SearchPage]{ETag: f.tag(), Payload: dom.SearchPage{Query: in.Q, List: list, Total: int64(f.results)}}, nil
}

func (f *fakeSearch) Suggest(_ context.Context, q string, limit int) (dom.Doc[dom.SuggestPage], error) {
	f.sugQ, f.sugN = q, limit
	if f.sugErr != nil {
		return dom.Doc[dom.SuggestPage]{}, f.sugErr
	}
	return dom.Doc[dom.SuggestPage]{
		ETag:    f.tag(),
		Payload: dom.SuggestPage{Query: q, List: []dom.SuggestItem{{ID: 3, Title: "总裁的小娇妻"}}},
	}, nil
}

func (f *fakeSearch) tag() string {
	if f.etag == "" {
		return `W/"test"`
	}
	return f.etag
}

// countingLimiter 是固定窗口限流器的假实现：按 key 累加。
type countingLimiter struct {
	n    map[string]int64
	keys []string
	err  error
}

func newCountingLimiter() *countingLimiter { return &countingLimiter{n: map[string]int64{}} }

func (l *countingLimiter) Add(_ context.Context, key string, delta int64, _ time.Duration) (int64, error) {
	l.keys = append(l.keys, key)
	if l.err != nil {
		return 0, l.err
	}
	l.n[key] += delta
	return l.n[key], nil
}

func TestSearchHandler(t *testing.T) {
	search := &fakeSearch{etag: `W/"s1"`, results: 2}
	h := &Handlers{Svc: &fakeService{}, Search: search}

	w := get(t, h, "/api/v1/search?q=%E7%94%9C%E5%AE%A0&sort=latest&limit=2&cursor=abc", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d %s", w.Code, w.Body.String())
	}
	if search.req.Q != "甜宠" || search.req.Sort != dom.SortLatest || search.req.Limit != 2 || search.req.Cursor != "abc" {
		t.Fatalf("参数透传：%+v", search.req)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=0, s-maxage=60, stale-while-revalidate=300" {
		t.Fatalf("缓存头：%q", got)
	}
	var page dom.SearchPage
	if err := json.Unmarshal(body(t, w).Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Query != "甜宠" || len(page.List) != 2 || page.Total != 2 {
		t.Fatalf("响应内容：%+v", page)
	}
	// 条件请求
	w = get(t, h, "/api/v1/search?q=%E7%94%9C%E5%AE%A0", map[string]string{"If-None-Match": `W/"s1"`})
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("期望 304 无响应体，得到 %d %q", w.Code, w.Body.String())
	}
	// 查询参数类型不对
	if w = get(t, h, "/api/v1/search?q=%E7%94%9C%E5%AE%A0&limit=abc", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("limit 不是数字应 400，得到 %d", w.Code)
	}
	// 领域层的校验错误透传
	search.err = errs.ErrBadRequest.WithMessage("请输入搜索词")
	w = get(t, h, "/api/v1/search", nil)
	if w.Code != http.StatusBadRequest || body(t, w).Code != "BAD_REQUEST" {
		t.Fatalf("空词应 400，得到 %d %s", w.Code, w.Body.String())
	}
}

func TestSuggestHandler(t *testing.T) {
	search := &fakeSearch{etag: `W/"g1"`}
	limiter := newCountingLimiter()
	h := &Handlers{Svc: &fakeService{}, Search: search, SuggestLimiter: limiter}

	w := get(t, h, "/api/v1/search/suggest?q=%E6%80%BB%E8%A3%81&limit=5", map[string]string{"X-Device-Id": "dev-1"})
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d %s", w.Code, w.Body.String())
	}
	if search.sugQ != "总裁" || search.sugN != 5 {
		t.Fatalf("参数透传：%q %d", search.sugQ, search.sugN)
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=0, s-maxage=300, stale-while-revalidate=300" {
		t.Fatalf("缓存头：%q", got)
	}
	if len(limiter.keys) != 1 || !strings.HasPrefix(limiter.keys[0], "dev-1:") {
		t.Fatalf("按设备计数：%v", limiter.keys)
	}

	// 开关关闭时领域层返回 404
	search.sugErr = errs.ErrNotFound
	if w = get(t, h, "/api/v1/search/suggest?q=%E6%80%BB%E8%A3%81", nil); w.Code != http.StatusNotFound {
		t.Fatalf("开关关闭应 404，得到 %d", w.Code)
	}
	search.sugErr = nil
	// 没有设备标识时按 IP 计数
	limiter.keys = nil
	get(t, h, "/api/v1/search/suggest?q=%E6%80%BB%E8%A3%81", nil)
	if len(limiter.keys) != 1 || !strings.HasPrefix(limiter.keys[0], "ip:") {
		t.Fatalf("没有设备标识时按 IP 计数：%v", limiter.keys)
	}
}

func TestSuggestRateLimit(t *testing.T) {
	search := &fakeSearch{}
	limiter := newCountingLimiter()
	h := &Handlers{Svc: &fakeService{}, Search: search, SuggestLimiter: limiter}
	headers := map[string]string{"X-Device-Id": "dev-2"}

	for i := range suggestPerMinute {
		if w := get(t, h, "/api/v1/search/suggest?q=a", headers); w.Code != http.StatusOK {
			t.Fatalf("第 %d 次应放行，得到 %d", i+1, w.Code)
		}
	}
	w := get(t, h, "/api/v1/search/suggest?q=a", headers)
	if w.Code != http.StatusTooManyRequests || body(t, w).Code != "RATE_LIMITED" {
		t.Fatalf("超出配额应 429，得到 %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("429 应带 Retry-After")
	}
	// 换一个设备不受影响
	if w = get(t, h, "/api/v1/search/suggest?q=a", map[string]string{"X-Device-Id": "dev-3"}); w.Code != http.StatusOK {
		t.Fatalf("另一个设备应放行，得到 %d", w.Code)
	}

	// 计数器故障时放行（宁可放宽也不拒绝整个接口）
	limiter.err = errors.New("valkey 不可用")
	if w = get(t, h, "/api/v1/search/suggest?q=a", headers); w.Code != http.StatusOK {
		t.Fatalf("限流器故障应放行，得到 %d", w.Code)
	}
}

func TestSearchRoutesAbsentWithoutService(t *testing.T) {
	// Search 为空时不注册搜索路由（装配缺依赖时宁可 404，也不要 panic）
	h := &Handlers{Svc: &fakeService{}}
	for _, path := range []string{"/api/v1/search?q=a", "/api/v1/search/suggest?q=a"} {
		if w := get(t, h, path, nil); w.Code != http.StatusNotFound {
			t.Errorf("%s 未注册时应 404，得到 %d", path, w.Code)
		}
	}
}
