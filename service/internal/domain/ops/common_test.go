package ops

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

func TestCleanLink(t *testing.T) {
	ok := map[string]string{
		"":                              "",
		"  /d/12  ":                     "/d/12",
		"/vip?from=announce":            "/vip?from=announce",
		"https://example.com/a?b=1":     "https://example.com/a?b=1",
		"http://cdn.example.com/1.webp": "http://cdn.example.com/1.webp",
		"/covers/1001-v.jpg":            "/covers/1001-v.jpg",
	}
	for in, want := range ok {
		if got, err := cleanLink("跳转网址", in); err != nil || got != want {
			t.Errorf("cleanLink(%q) = %q, %v；期望 %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		"javascript:alert(1)", "//evil.example.com/x", `/\evil.example.com`, "ftp://example.com/a", "example.com/a",
		"https://user:pw@example.com/", "https:///nohost", "/a b", "/a\tb", "data:text/html,1", "https://" + strings.Repeat("a", 500) + ".com",
	} {
		if _, err := cleanLink("跳转网址", bad); err == nil {
			t.Errorf("cleanLink(%q) 应拒绝", bad)
		}
	}
}

func TestCleanSet(t *testing.T) {
	got, err := cleanSet("投放语言", []string{"ar", " en ", "zh-CN", "en"}, Locales)
	if err != nil || !slices.Equal(got, []string{"zh-CN", "en", "ar"}) {
		t.Fatalf("去重并按支持列表的顺序返回：%v %v", got, err)
	}
	if got, err := cleanSet("投放语言", nil, Locales); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("不选为空列表（不是 nil）：%#v %v", got, err)
	}
	_, err = cleanSet("投放语言", []string{"zh-TW"}, Locales)
	wantErr(t, err, errs.ErrBadRequest, "投放语言无效")
}

func TestNormalizeLocaleAndTargets(t *testing.T) {
	if NormalizeLocale("ja") != "ja" || NormalizeLocale("") != DefaultLocale || NormalizeLocale("fr") != DefaultLocale {
		t.Fatal("支持的语言原样返回，其余按默认语言")
	}
	if !targets(nil, "ar") || !targets([]string{"ar", "en"}, "ar") || targets([]string{"en"}, "ar") {
		t.Fatal("空列表投给全部语言，否则只投给列出的语言")
	}
}

func TestLimitOffset(t *testing.T) {
	p, limit, offset, err := limitOffset(PageRequest{})
	if err != nil || p.Page != 1 || p.PageSize != defaultPageSize || limit != defaultPageSize || offset != 0 {
		t.Fatalf("默认第 1 页每页 20 条：%+v %d %d %v", p, limit, offset, err)
	}
	if _, limit, offset, _ := limitOffset(PageRequest{Page: 3, PageSize: 50}); limit != 50 || offset != 100 {
		t.Fatalf("第 3 页偏移 100：%d %d", limit, offset)
	}
	for _, bad := range []PageRequest{{Page: -1}, {PageSize: 101}, {Page: maxPage + 1}} {
		if _, _, _, err := limitOffset(bad); err == nil {
			t.Errorf("%+v 应拒绝", bad)
		}
	}
}

// memCache 是内存版读穿缓存，记录回源次数。
type memCache struct {
	mu    sync.Mutex
	data  map[string]any
	loads int
	err   error // Delete 返回的错误
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
	case *[]Announcement:
		*d = v.([]Announcement)
	case *[]Ad:
		*d = v.([]Ad)
	case *[]PublicHotWord:
		*d = v.([]PublicHotWord)
	case *PublicRankBoard:
		*d = v.(PublicRankBoard)
	default:
		return errs.ErrInternal
	}
	return nil
}

func (m *memCache) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return m.err
}
