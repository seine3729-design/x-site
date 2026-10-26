package site

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

func wantErr(t *testing.T, err error, code *errs.Error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误 %s「%s」，得到 nil", code.Code, msg)
	}
	e := errs.From(err)
	if e.Code != code.Code || !strings.Contains(e.Message, msg) {
		t.Fatalf("期望 %s 且包含「%s」，得到 %s「%s」", code.Code, msg, e.Code, e.Message)
	}
}

// memCache 是内存版读穿缓存，记录回源与删除。
type memCache struct {
	mu      sync.Mutex
	data    map[string]any
	loads   int
	deleted []string
}

func newMemCache() *memCache { return &memCache{data: map[string]any{}} }

func (m *memCache) GetOrLoadJSON(ctx context.Context, key string, _ time.Duration, dst any, load func(context.Context) (any, error)) error {
	m.mu.Lock()
	v, ok := m.data[key]
	m.mu.Unlock()
	if !ok {
		var err error
		if v, err = load(ctx); err != nil {
			return err
		}
		m.mu.Lock()
		m.data[key], m.loads = v, m.loads+1
		m.mu.Unlock()
	}
	// 与 Valkey 实现一样只支持「同类型赋值」：测试中 dst 与 load 的返回类型一致
	switch d := dst.(type) {
	case *PublicPage:
		*d = v.(PublicPage)
	case *[]FooterPage:
		*d = v.([]FooterPage)
	case *[]PublicMenu:
		*d = v.([]PublicMenu)
	case *PublicHome:
		*d = v.(PublicHome)
	default:
		return errs.ErrInternal
	}
	return nil
}

func (m *memCache) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	m.deleted = append(m.deleted, key)
	return nil
}

func (m *memCache) deletedKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.deleted...)
}

// fakePageRepo 只实现测试用到的部分。
type fakePageRepo struct {
	query   PageQuery
	input   PageInput
	page    StaticPage
	footers []FooterPage
	err     error
	gets    int
}

func (f *fakePageRepo) ListPages(_ context.Context, q PageQuery) ([]StaticPage, int64, error) {
	f.query = q
	return []StaticPage{f.page}, 1, f.err
}

func (f *fakePageRepo) GetPage(_ context.Context, id int64) (StaticPage, error) {
	p := f.page
	p.ID = id
	return p, f.err
}

func (f *fakePageRepo) CreatePage(_ context.Context, in PageInput) (StaticPage, error) {
	f.input = in
	return StaticPage{ID: 9, Title: in.Title, Slug: in.Slug, Body: in.Body, Status: in.Status}, f.err
}

func (f *fakePageRepo) UpdatePage(_ context.Context, id int64, in PageInput) (StaticPage, error) {
	f.input = in
	return StaticPage{ID: id, Title: in.Title, Slug: in.Slug, Body: in.Body, Status: in.Status}, f.err
}

func (f *fakePageRepo) DeletePage(_ context.Context, id int64) (StaticPage, error) {
	p := f.page
	p.ID = id
	return p, f.err
}

func (f *fakePageRepo) PublicPage(_ context.Context, slug string) (StaticPage, error) {
	f.gets++
	if f.err != nil {
		return StaticPage{}, f.err
	}
	p := f.page
	p.Slug = slug
	return p, nil
}

func (f *fakePageRepo) FooterPages(_ context.Context) ([]FooterPage, error) { return f.footers, f.err }

func newPageSvc() (*PageService, *fakePageRepo, *memCache) {
	repo := &fakePageRepo{page: StaticPage{
		ID: 1, Title: "用户协议", Slug: "terms", Body: "<p>正文</p>", BodyFormat: BodyRich, Status: PagePublished,
		UpdatedAt: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC),
	}}
	c := newMemCache()
	return NewPageService(repo, c, &fakeBumper{}), repo, c
}

func TestCleanPage(t *testing.T) {
	in, err := cleanPage(PageInput{Title: "  用户协议  ", Slug: " Terms-2026 ", Body: " <p>正文</p> "})
	if err != nil || in.Title != "用户协议" || in.Slug != "terms-2026" || in.Body != "<p>正文</p>" {
		t.Fatalf("去空白、slug 转小写：%+v %v", in, err)
	}
	if in.BodyFormat != BodyRich || in.Status != PageDraft {
		t.Fatalf("默认富文本、草稿：%+v", in)
	}

	// 正文按白名单清洗：脚本、事件属性与 javascript: 链接去掉，排版标签保留
	in, err = cleanPage(PageInput{
		Title: "条款", Slug: "t",
		Body: `<h2>标题</h2><script>alert(1)</script><p onclick="x()">正文<a href="javascript:alert(1)">点</a></p>` +
			`<iframe src="https://evil.example.com"></iframe><ul><li>一</li></ul>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"<script", "onclick", "javascript:", "<iframe"} {
		if strings.Contains(in.Body, bad) {
			t.Fatalf("清洗后仍有 %s：%s", bad, in.Body)
		}
	}
	for _, keep := range []string{"<h2>标题</h2>", "<ul>", "<li>一</li>"} {
		if !strings.Contains(in.Body, keep) {
			t.Fatalf("清洗后应保留 %s：%s", keep, in.Body)
		}
	}

	// 站外链接加 noopener 并新窗口打开
	in, _ = cleanPage(PageInput{Title: "条款", Slug: "t", Body: `<p><a href="https://example.com">站外</a></p>`})
	if !strings.Contains(in.Body, `rel="nofollow noopener"`) || !strings.Contains(in.Body, `target="_blank"`) {
		t.Fatalf("站外链接应加 noopener 并新窗口：%s", in.Body)
	}

	bad := []struct {
		in  PageInput
		msg string
	}{
		{PageInput{Slug: "t", Body: "x"}, "标题不能为空"},
		{PageInput{Title: strings.Repeat("标", 101), Slug: "t", Body: "x"}, "标题不能超过 100 个字"},
		{PageInput{Title: "条款", Body: "x"}, "请填写 slug"},
		{PageInput{Title: "条款", Slug: "有中文", Body: "x"}, "slug 只能用小写字母"},
		{PageInput{Title: "条款", Slug: "-bad", Body: "x"}, "slug 只能用小写字母"},
		{PageInput{Title: "条款", Slug: "t"}, "请填写正文"},
		{PageInput{Title: "条款", Slug: "t", Body: "<script>alert(1)</script>"}, "正文清洗后为空"},
		{PageInput{Title: "条款", Slug: "t", Body: strings.Repeat("字", maxPageBodyRunes+1)}, "正文不能超过"},
		{PageInput{Title: "条款", Slug: "t", Body: "x", BodyFormat: "markdown"}, "正文格式"},
		{PageInput{Title: "条款", Slug: "t", Body: "x", Status: "archived"}, "状态只能是草稿或已发布"},
		{PageInput{Title: "条款", Slug: "t", Body: "x", SEOTitle: strings.Repeat("标", 101)}, "SEO 标题"},
		{PageInput{Title: "条款", Slug: "t", Body: "x", SEODesc: strings.Repeat("标", 201)}, "SEO 简介"},
	}
	for _, b := range bad {
		_, err := cleanPage(b.in)
		wantErr(t, err, errs.ErrBadRequest, b.msg)
	}
}

func TestPageServiceList(t *testing.T) {
	svc, repo, _ := newPageSvc()
	ctx := context.Background()
	p, err := svc.List(ctx, PageFilter{Q: " 100%_条款 ", Status: PagePublished, PageRequest: PageRequest{Page: 2, PageSize: 10}})
	if err != nil || p.Page != 2 || p.PageSize != 10 || len(p.List) != 1 || p.Total != 1 {
		t.Fatalf("分页：%+v %v", p, err)
	}
	if repo.query.Pattern != `%100\%\_条款%` || repo.query.Status != PagePublished || repo.query.Limit != 10 || repo.query.Offset != 10 {
		t.Fatalf("LIKE 模式转义与分页参数：%+v", repo.query)
	}
	if _, err := svc.List(ctx, PageFilter{Status: "archived"}); err == nil {
		t.Fatal("状态筛选无效应报错")
	}
	if _, err := svc.List(ctx, PageFilter{Q: strings.Repeat("字", 51)}); err == nil {
		t.Fatal("搜索词过长应报错")
	}
}

func TestPageServiceWriteInvalidatesCache(t *testing.T) {
	svc, repo, c := newPageSvc()
	ctx := context.Background()

	// 页脚页与菜单都随站点配置下发，所以每次写都要连带清掉这两个缓存
	tail := []string{footerCacheKey, menuCacheKey}

	if _, err := svc.Create(ctx, PageInput{Title: "条款", Slug: "terms", Body: "<p>x</p>", Status: PagePublished}); err != nil {
		t.Fatal(err)
	}
	if got := c.deletedKeys(); !slices.Equal(got, append([]string{pageCacheKey("terms")}, tail...)) {
		t.Fatalf("新建后清掉该页、页脚页与菜单缓存：%v", got)
	}

	// 改 slug：新旧两个地址的缓存都要清
	c.deleted = nil
	repo.page.Slug = "old-terms"
	if _, _, err := svc.Update(ctx, 1, PageInput{Title: "条款", Slug: "new-terms", Body: "<p>x</p>"}); err != nil {
		t.Fatal(err)
	}
	if got := c.deletedKeys(); !slices.Equal(got, append([]string{pageCacheKey("old-terms"), pageCacheKey("new-terms")}, tail...)) {
		t.Fatalf("改 slug 后清掉两个地址：%v", got)
	}

	c.deleted = nil
	repo.page.Slug = "terms"
	if _, err := svc.Delete(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if got := c.deletedKeys(); !slices.Equal(got, append([]string{pageCacheKey("terms")}, tail...)) {
		t.Fatalf("删除后清缓存：%v", got)
	}
}

func TestPageServicePublic(t *testing.T) {
	svc, repo, c := newPageSvc()
	ctx := context.Background()

	// 格式不对的 slug 直接 404，不去查库
	for _, bad := range []string{"", "有中文", "-x", strings.Repeat("a", 65), "a/b"} {
		if _, err := svc.Public(ctx, bad); errs.From(err).Code != errs.ErrNotFound.Code {
			t.Fatalf("slug %q 应 404，得到 %v", bad, err)
		}
	}
	if repo.gets != 0 {
		t.Fatalf("非法 slug 不应查库，查了 %d 次", repo.gets)
	}

	p, err := svc.Public(ctx, "terms")
	if err != nil || p.Slug != "terms" || p.Title != "用户协议" || p.Body != "<p>正文</p>" {
		t.Fatalf("读静态页：%+v %v", p, err)
	}
	if _, err := svc.Public(ctx, "terms"); err != nil || c.loads != 1 || repo.gets != 1 {
		t.Fatalf("第二次应命中缓存：loads=%d gets=%d %v", c.loads, repo.gets, err)
	}

	// 草稿与不存在的 slug 由仓储返回 404
	repo.err = errs.ErrNotFound
	if _, err := svc.Public(ctx, "draft-page"); errs.From(err).Code != errs.ErrNotFound.Code {
		t.Fatalf("草稿应 404，得到 %v", err)
	}
}

func TestPageServiceFooterPages(t *testing.T) {
	svc, repo, c := newPageSvc()
	ctx := context.Background()

	// 没有页脚页时是空数组（不是 nil），JSON 下发为 []
	list, err := svc.FooterPages(ctx)
	if err != nil || list == nil || len(list) != 0 {
		t.Fatalf("没有页脚页：%#v %v", list, err)
	}

	c.data = map[string]any{}
	repo.footers = make([]FooterPage, maxFooterPages+5)
	for i := range repo.footers {
		repo.footers[i] = FooterPage{Slug: "p", Title: "页"}
	}
	if list, err = svc.FooterPages(ctx); err != nil || len(list) != maxFooterPages {
		t.Fatalf("最多 %d 个页脚页：%d %v", maxFooterPages, len(list), err)
	}
}
