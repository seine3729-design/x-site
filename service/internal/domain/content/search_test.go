package content

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xsite/service/internal/errs"
)

// 前台搜索（S3-4）的领域层测试：规范化与校验、默认排序、游标、热搜计数、联想开关与限制。

// fakeSearchRepo 只实现 SearchRepo，记录收到的查询。
type fakeSearchRepo struct {
	cards     []DramaCard
	total     int64
	suggests  []SuggestItem
	queries   []PublicDramaQuery
	filters   []PublicDramaFilter
	sugPat    string
	sugPrefix string
	sugLimit  int
	err       error
}

func (r *fakeSearchRepo) PublicDramas(_ context.Context, q PublicDramaQuery) ([]DramaCard, error) {
	r.queries = append(r.queries, q)
	if r.err != nil {
		return nil, r.err
	}
	if q.Limit < len(r.cards) {
		return r.cards[:q.Limit], nil
	}
	return r.cards, nil
}

func (r *fakeSearchRepo) CountPublicDramas(_ context.Context, f PublicDramaFilter) (int64, error) {
	r.filters = append(r.filters, f)
	return r.total, r.err
}

func (r *fakeSearchRepo) SuggestDramas(_ context.Context, pattern, prefix string, limit int) ([]SuggestItem, error) {
	r.sugPat, r.sugPrefix, r.sugLimit = pattern, prefix, limit
	return r.suggests, r.err
}

// fakeFlags 按名字返回开关；没列出的按打开（与生产语义一致）。
type fakeFlags struct {
	off map[string]bool
	err error
}

func (f fakeFlags) Enabled(_ context.Context, name string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return !f.off[name], nil
}

// fakeRecorder 记录热搜计数的调用。
type fakeRecorder struct {
	words []string
	hits  []int
	err   error
}

func (r *fakeRecorder) Record(_ context.Context, q string, hits int) error {
	r.words, r.hits = append(r.words, q), append(r.hits, hits)
	return r.err
}

func newSearchSvc() (*SearchService, *fakeSearchRepo, *fakeCache, *fakeRecorder, *fakeFlags) {
	repo := &fakeSearchRepo{
		cards: []DramaCard{{ID: 3, Title: "总裁的小娇妻"}, {ID: 1, Title: "甜宠日记"}},
		total: 2,
		suggests: []SuggestItem{
			{ID: 3, Title: "总裁的小娇妻"}, {ID: 1, Title: "甜宠日记"},
		},
	}
	c, rec, flags := newFakeCache(), &fakeRecorder{}, &fakeFlags{off: map[string]bool{}}
	return NewSearchService(repo, c, flags, rec), repo, c, rec, flags
}

func TestSearchValidatesQuery(t *testing.T) {
	svc, _, _, _, _ := newSearchSvc()
	ctx := context.Background()

	for name, in := range map[string]SearchRequest{
		"空词":     {Q: "   "},
		"只有控制字符": {Q: "\u200b\t"},
		"超长":     {Q: strings.Repeat("甜", 51)},
		"条数越界":   {Q: "甜宠", Limit: 61},
		"未知排序":   {Q: "甜宠", Sort: "random"}, // 搜索不支持随机
	} {
		if _, err := svc.Search(ctx, in); errs.From(err).Code != errs.ErrBadRequest.Code {
			t.Errorf("%s 应 400：%v", name, err)
		}
	}
}

func TestSearchNormalizesWord(t *testing.T) {
	svc, repo, c, rec, _ := newSearchSvc()
	ctx := context.Background()

	// 全角、大小写、空白都按 textnorm 规范化；回显与计数用的是规范化后的词
	doc, err := svc.Search(ctx, SearchRequest{Q: "  ＡＢＣ　 甜宠  "})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Payload.Query != "abc 甜宠" {
		t.Fatalf("回显规范化后的词：%q", doc.Payload.Query)
	}
	if len(rec.words) != 1 || rec.words[0] != "abc 甜宠" {
		t.Fatalf("热搜计数用规范化后的词：%v", rec.words)
	}
	if !strings.HasPrefix(c.lastKey(), "content:search:abc 甜宠:") {
		t.Fatalf("缓存键用规范化后的词：%q", c.lastKey())
	}
	// 搜索词进 LIKE 模式（转义后包含匹配）
	if got := repo.filters[0].Pattern; got != "%abc 甜宠%" {
		t.Fatalf("LIKE 模式：%q", got)
	}
	// 特殊字符要转义，不能当通配符
	if _, err := svc.Search(ctx, SearchRequest{Q: "100%_a"}); err != nil {
		t.Fatal(err)
	}
	if got := repo.filters[1].Pattern; got != `%100\%\_a%` {
		t.Fatalf("通配符应被转义：%q", got)
	}
}

func TestSearchDefaultsToHot(t *testing.T) {
	svc, repo, _, _, _ := newSearchSvc()
	ctx := context.Background()

	if _, err := svc.Search(ctx, SearchRequest{Q: "甜宠"}); err != nil {
		t.Fatal(err)
	}
	if got := repo.queries[0]; got.Sort != SortHot || got.Limit != publicDefaultLimit {
		t.Fatalf("默认按最热、每页 24：%+v", got)
	}
	// 显式指定的排序照用
	if _, err := svc.Search(ctx, SearchRequest{Q: "甜宠", Sort: SortLatest, Limit: 2}); err != nil {
		t.Fatal(err)
	}
	if got := repo.queries[1]; got.Sort != SortLatest || got.Limit != 2 {
		t.Fatalf("排序与条数透传：%+v", got)
	}
}

// TestSearchCacheKeyIncludesSort 守住「换排序不能读到上一种排序的缓存」：
// cursorKey 对第一页只给「0:条数」，键里不带排序就会串（curl 实测踩到过）。
func TestSearchCacheKeyIncludesSort(t *testing.T) {
	svc, repo, c, _, _ := newSearchSvc()
	ctx := context.Background()

	if _, err := svc.Search(ctx, SearchRequest{Q: "甜宠"}); err != nil { // 默认 hot
		t.Fatal(err)
	}
	if _, err := svc.Search(ctx, SearchRequest{Q: "甜宠", Sort: SortLatest}); err != nil {
		t.Fatal(err)
	}
	if c.keys[0] == c.keys[1] {
		t.Fatalf("不同排序必须是不同的键：%v", c.keys)
	}
	if c.loads != 2 {
		t.Fatalf("换排序应重新回源：loads=%d", c.loads)
	}
	if len(repo.queries) != 2 || repo.queries[1].Sort != SortLatest {
		t.Fatalf("第二次应按最新查询：%+v", repo.queries)
	}
}

func TestSearchPagesAndCountsOnce(t *testing.T) {
	svc, repo, c, rec, _ := newSearchSvc()
	ctx := context.Background()
	repo.total = 100

	// 第一页：满页时给下一页游标，计一次热搜
	doc, err := svc.Search(ctx, SearchRequest{Q: "甜宠", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Payload.Total != 100 || len(doc.Payload.List) != 2 || doc.Payload.NextCursor == nil {
		t.Fatalf("第一页：%+v", doc.Payload)
	}
	if len(rec.words) != 1 || rec.hits[0] != 100 {
		t.Fatalf("第一页计一次热搜，次数带结果数：%v %v", rec.words, rec.hits)
	}

	// 第二页：游标进查询，不再计数
	if _, err = svc.Search(ctx, SearchRequest{Q: "甜宠", Limit: 2, Cursor: *doc.Payload.NextCursor}); err != nil {
		t.Fatal(err)
	}
	if got := repo.queries[1].Cursor; got.ID != 1 {
		t.Fatalf("第二页应带游标：%+v", got)
	}
	if len(rec.words) != 1 {
		t.Fatalf("翻页不应重复计数：%v", rec.words)
	}
	// 两页各自成键
	if c.keys[0] == c.keys[1] {
		t.Fatalf("两页缓存键不应相同：%v", c.keys)
	}

	// 命中缓存也要计数：用户确实又搜了一次
	loads := c.loads
	if _, err = svc.Search(ctx, SearchRequest{Q: "甜宠", Limit: 2}); err != nil {
		t.Fatal(err)
	}
	if c.loads != loads {
		t.Fatalf("第二次应命中缓存：loads=%d", c.loads)
	}
	if len(rec.words) != 2 {
		t.Fatalf("命中缓存时也要计数：%v", rec.words)
	}
}

func TestSearchRecordFailureDoesNotBreakResult(t *testing.T) {
	svc, _, _, rec, _ := newSearchSvc()
	rec.err = errors.New("valkey 不可用")
	doc, err := svc.Search(context.Background(), SearchRequest{Q: "甜宠"})
	if err != nil || len(doc.Payload.List) != 2 {
		t.Fatalf("计数失败不应影响搜索结果：%+v %v", doc.Payload, err)
	}
}

func TestSearchNoResults(t *testing.T) {
	svc, repo, _, rec, _ := newSearchSvc()
	repo.cards, repo.total = nil, 0

	doc, err := svc.Search(context.Background(), SearchRequest{Q: "查无此剧"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Payload.List == nil || len(doc.Payload.List) != 0 || doc.Payload.NextCursor != nil {
		t.Fatalf("空结果是空数组且没有下一页：%#v", doc.Payload)
	}
	// 片库没有的词也要计数（0 条），后台「片库没有」的选品依据就靠它
	if len(rec.hits) != 1 || rec.hits[0] != 0 {
		t.Fatalf("没有结果时计数应为 0：%v", rec.hits)
	}
}

func TestSuggest(t *testing.T) {
	svc, repo, c, rec, flags := newSearchSvc()
	ctx := context.Background()

	doc, err := svc.Suggest(ctx, "  总裁 ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Payload.Query != "总裁" || len(doc.Payload.List) != 2 || doc.Payload.List[0].ID != 3 {
		t.Fatalf("联想结果：%+v", doc.Payload)
	}
	if repo.sugPat != "%总裁%" || repo.sugPrefix != "总裁%" || repo.sugLimit != suggestMaxItems {
		t.Fatalf("模式与条数：%q %q %d", repo.sugPat, repo.sugPrefix, repo.sugLimit)
	}
	if c.lastKey() != "content:suggest:总裁:10" {
		t.Fatalf("缓存键：%q", c.lastKey())
	}
	// 联想不计热搜
	if len(rec.words) != 0 {
		t.Fatalf("联想不应计入热搜：%v", rec.words)
	}

	// 条数与词的校验
	for name, in := range map[string]struct {
		q     string
		limit int
	}{
		"空词":   {"  ", 0},
		"条数为负": {"总裁", -1},
		"条数越界": {"总裁", 11},
	} {
		if _, err := svc.Suggest(ctx, in.q, in.limit); errs.From(err).Code != errs.ErrBadRequest.Code {
			t.Errorf("%s 应 400：%v", name, err)
		}
	}

	// 开关关闭：整个接口 404
	flags.off[searchSuggestFeature] = true
	if _, err := svc.Suggest(ctx, "总裁", 0); errs.From(err).Code != errs.ErrNotFound.Code {
		t.Fatalf("开关关闭应 404：%v", err)
	}
	// 读开关失败时不能当成「关闭」掉接口，要把错误抛出去
	flags.off[searchSuggestFeature] = false
	flags.err = errors.New("配置读取失败")
	if _, err := svc.Suggest(ctx, "总裁", 0); err == nil {
		t.Fatal("读开关失败应返回错误")
	}
}

func TestSuggestEmptyList(t *testing.T) {
	svc, repo, _, _, _ := newSearchSvc()
	repo.suggests = nil
	doc, err := svc.Suggest(context.Background(), "查无此剧", 5)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Payload.List == nil || len(doc.Payload.List) != 0 {
		t.Fatalf("空联想是空数组：%#v", doc.Payload.List)
	}
}
