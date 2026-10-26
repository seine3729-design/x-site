package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

// 前台读接口（S1-4）的领域层测试：参数校验、游标、缓存键、规则执行；SQL 行为在 store/pg 的集成测试里。

// fakeCache 是读穿缓存的假实现：记录每次用的键，命中时不再回源。
type fakeCache struct {
	keys   []string
	values map[string][]byte
	loads  int
}

func newFakeCache() *fakeCache { return &fakeCache{values: map[string][]byte{}} }

func (c *fakeCache) GetOrLoadJSON(ctx context.Context, key string, _ time.Duration, dst any, load func(context.Context) (any, error)) error {
	c.keys = append(c.keys, key)
	if raw, ok := c.values[key]; ok {
		return json.Unmarshal(raw, dst)
	}
	c.loads++
	v, err := load(ctx)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.values[key] = raw
	return json.Unmarshal(raw, dst)
}

func (c *fakeCache) lastKey() string { return c.keys[len(c.keys)-1] }

type fakePublicRepo struct {
	drama    Drama
	tags     []Tag
	terms    []TermView
	episodes []EpisodeView
	related  []DramaCard
	channel  ChannelView

	tagsBySlug map[string]Tag
	tagsByID   map[int64]Tag
	facets     []FacetTaxonomy
	termIDs    map[string]int64
	codeIDs    map[string]int64

	cards  []DramaCard
	total  int64
	topics []Topic
	topic  Topic

	// 记录调用，供断言。
	queries      []PublicDramaQuery
	relatedCalls [][]int64
	facetCalls   int
}

func newFakePublicRepo() *fakePublicRepo {
	return &fakePublicRepo{
		tagsBySlug: map[string]Tag{}, tagsByID: map[int64]Tag{},
		termIDs: map[string]int64{}, codeIDs: map[string]int64{},
	}
}

func (r *fakePublicRepo) PublicDrama(_ context.Context, id int64) (Drama, error) {
	if r.drama.ID != id {
		return Drama{}, errs.ErrNotFound
	}
	return r.drama, nil
}

func (r *fakePublicRepo) PublicDramaTags(context.Context, int64) ([]Tag, error) { return r.tags, nil }

func (r *fakePublicRepo) PublicDramaTerms(context.Context, int64) ([]TermView, error) {
	return r.terms, nil
}

func (r *fakePublicRepo) PublicEpisodes(context.Context, int64) ([]EpisodeView, error) {
	return r.episodes, nil
}

func (r *fakePublicRepo) RelatedDramas(_ context.Context, _ int64, tagIDs, termIDs []int64, _ int) ([]DramaCard, error) {
	r.relatedCalls = append(r.relatedCalls, slices.Concat(tagIDs, termIDs))
	return r.related, nil
}

func (r *fakePublicRepo) PublicDramas(_ context.Context, q PublicDramaQuery) ([]DramaCard, error) {
	r.queries = append(r.queries, q)
	if q.Limit < len(r.cards) {
		return r.cards[:q.Limit], nil
	}
	return r.cards, nil
}

func (r *fakePublicRepo) CountPublicDramas(context.Context, PublicDramaFilter) (int64, error) {
	return r.total, nil
}

func (r *fakePublicRepo) PublicChannel(_ context.Context, slug string) (ChannelView, error) {
	if r.channel.Slug != slug {
		return ChannelView{}, errs.ErrNotFound
	}
	return r.channel, nil
}

func (r *fakePublicRepo) PublicTagBySlug(_ context.Context, slug string) (Tag, error) {
	t, ok := r.tagsBySlug[slug]
	if !ok {
		return Tag{}, errs.ErrNotFound
	}
	return t, nil
}

func (r *fakePublicRepo) PublicTagByID(_ context.Context, id int64) (Tag, error) {
	t, ok := r.tagsByID[id]
	if !ok {
		return Tag{}, errs.ErrNotFound
	}
	return t, nil
}

func (r *fakePublicRepo) FacetTaxonomies(context.Context) ([]FacetTaxonomy, error) {
	r.facetCalls++
	return r.facets, nil
}

func (r *fakePublicRepo) ResolveTermIDs(_ context.Context, pairs []string) ([]int64, error) {
	out := []int64{}
	for _, p := range pairs {
		if id, ok := r.termIDs[p]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

func (r *fakePublicRepo) ResolveTermIDsByCodes(_ context.Context, codes []string) ([]int64, error) {
	out := []int64{}
	for _, c := range codes {
		if id, ok := r.codeIDs[c]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

func (r *fakePublicRepo) PublicTopics(context.Context, int64, int) ([]Topic, int64, error) {
	return r.topics, int64(len(r.topics)), nil
}

func (r *fakePublicRepo) PublicTopicBySlug(_ context.Context, slug string) (Topic, error) {
	if r.topic.Slug != slug {
		return Topic{}, errs.ErrNotFound
	}
	return r.topic, nil
}

func (r *fakePublicRepo) PublicTopicDramas(context.Context, int64, int) ([]DramaCard, error) {
	return r.cards, nil
}

func newPublicFixture(t *testing.T) (*PublicService, *fakePublicRepo, *fakeCache) {
	t.Helper()
	repo, cache := newFakePublicRepo(), newFakeCache()
	svc := NewPublicService(repo, cache)
	svc.now = func() time.Time { return time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) }
	return svc, repo, cache
}

func cards(ids ...int64) []DramaCard {
	out := make([]DramaCard, 0, len(ids))
	for _, id := range ids {
		out = append(out, DramaCard{ID: id, Title: fmt.Sprintf("剧 %d", id), HotScore: id * 10, SortWeight: int32(id)})
	}
	return out
}

func TestPublicService_DramaDetail(t *testing.T) {
	svc, repo, cache := newPublicFixture(t)
	ctx := context.Background()
	repo.drama = Drama{ID: 7, Title: "总裁", Aliases: nil, ViewBase: 100, ViewCount: 23, LatestEpisode: 2}
	repo.tags = []Tag{{ID: 3, Name: "甜宠", Slug: "sweet", IsHidden: true}}
	repo.terms = []TermView{{ID: 5, Name: "现代", Slug: "modern", TaxonomyID: 1}}
	repo.episodes = []EpisodeView{{ID: 11, Number: 1, Playable: true}, {ID: 12, Number: 2}}
	repo.related = cards(8, 9)

	doc, err := svc.Drama(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	got := doc.Payload
	if got.ID != 7 || got.PlayCount != 123 || got.EpisodeCount != 2 || len(got.Episodes) != 2 || len(got.Related) != 2 {
		t.Fatalf("详情组装不符合预期：%+v", got)
	}
	if len(got.Tags) != 1 || !got.Tags[0].IsSensitive {
		t.Fatalf("敏感标签应标记 is_sensitive：%+v", got.Tags)
	}
	if got.Aliases == nil {
		t.Fatal("别名为空时应输出空数组而不是 null")
	}
	if !slices.Equal(repo.relatedCalls[0], []int64{3, 5}) {
		t.Fatalf("相关推荐应按标签与取值召回：%v", repo.relatedCalls)
	}
	if doc.ETag == "" || !strings.HasPrefix(doc.ETag, `W/"`) {
		t.Fatalf("应生成弱 ETag：%q", doc.ETag)
	}
	if cache.lastKey() != "content:drama:7" {
		t.Fatalf("缓存键应按剧 ID 单独成键：%q", cache.lastKey())
	}

	// 第二次读命中缓存，不再回源；ETag 不变
	again, err := svc.Drama(ctx, 7)
	if err != nil || again.ETag != doc.ETag {
		t.Fatalf("命中缓存后 ETag 应一致：%q %v", again.ETag, err)
	}
	if cache.loads != 1 {
		t.Fatalf("同一个键只应回源一次，实际 %d 次", cache.loads)
	}

	// 不存在或不可见的剧、非法编号都是 404
	if _, err := svc.Drama(ctx, 404); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不可见的剧应 404，得到 %v", err)
	}
	if _, err := svc.Drama(ctx, 0); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("非正整数编号应 404，得到 %v", err)
	}
}

func TestPublicService_DramaWithoutTagsSkipsRelated(t *testing.T) {
	svc, repo, _ := newPublicFixture(t)
	repo.drama = Drama{ID: 1, Title: "孤剧"}

	doc, err := svc.Drama(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.relatedCalls) != 0 {
		t.Fatal("没有标签与取值时不应查相关推荐")
	}
	if doc.Payload.Related == nil || doc.Payload.Tags == nil || doc.Payload.Terms == nil {
		t.Fatalf("空列表应输出空数组：%+v", doc.Payload)
	}
}

func TestPublicService_Explore(t *testing.T) {
	svc, repo, cache := newPublicFixture(t)
	ctx := context.Background()
	repo.facets = []FacetTaxonomy{{ID: 1, Name: "题材", Slug: "genre", Terms: []FacetTerm{{ID: 5, Slug: "romance"}}}}
	repo.termIDs["genre:romance"] = 5
	repo.termIDs["region:cn"] = 6
	repo.tagsBySlug["sweet"] = Tag{ID: 3, Slug: "sweet"}
	repo.cards = cards(1, 2)
	repo.total = 2

	doc, err := svc.Explore(ctx, ExploreRequest{Terms: "region:cn,genre:romance", Tag: "sweet", Window: "7d", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Payload.Facets == nil || len(doc.Payload.Facets.Taxonomies) != 1 {
		t.Fatalf("第一页应带分面：%+v", doc.Payload.Facets)
	}
	q := repo.queries[0]
	if !slices.Equal(q.TermIDs, []int64{5, 6}) || q.TagID != 3 || q.Sort != SortLatest || q.Limit != 2 {
		t.Fatalf("筛选未正确下传：%+v", q)
	}
	if q.Since == nil || !q.Since.Equal(svc.now().AddDate(0, 0, -7)) {
		t.Fatalf("上新窗口应换算为起始时间：%v", q.Since)
	}
	if doc.Payload.NextCursor == nil {
		t.Fatal("取满一页时应给出下一页游标")
	}

	// 取值的书写顺序不影响缓存键
	keyA := cache.lastKey()
	if _, err := svc.Explore(ctx, ExploreRequest{Terms: "genre:romance,region:cn", Tag: "sweet", Window: "7d", Limit: 2}); err != nil {
		t.Fatal(err)
	}
	if cache.lastKey() != keyA {
		t.Fatalf("筛选相同、书写顺序不同应命中同一个键：\n%s\n%s", keyA, cache.lastKey())
	}
	if cache.loads != 1 {
		t.Fatalf("应命中缓存，实际回源 %d 次", cache.loads)
	}

	// 第二页不再下发分面
	next, err := svc.Explore(ctx, ExploreRequest{Cursor: *doc.Payload.NextCursor, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if next.Payload.Facets != nil {
		t.Fatal("后续页不应重复下发分面")
	}
	if repo.facetCalls != 1 {
		t.Fatalf("分面只在第一页查一次，实际 %d 次", repo.facetCalls)
	}
}

func TestPublicService_ExploreUnknownFilterIsEmpty(t *testing.T) {
	svc, repo, _ := newPublicFixture(t)
	ctx := context.Background()
	repo.termIDs["genre:romance"] = 5
	repo.cards = cards(1)
	repo.total = 1

	// 其中一个取值不存在或已停用：没有剧能同时满足全部条件
	doc, err := svc.Explore(ctx, ExploreRequest{Terms: "genre:romance,genre:gone"})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Payload.List) != 0 || doc.Payload.Total != 0 || doc.Payload.NextCursor != nil {
		t.Fatalf("应返回空列表：%+v", doc.Payload)
	}
	// 标签不存在同理，但分面照常返回
	doc, err = svc.Explore(ctx, ExploreRequest{Tag: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Payload.List) != 0 || doc.Payload.Facets == nil {
		t.Fatalf("标签不存在时应空列表但有分面：%+v", doc.Payload)
	}
	if len(repo.queries) != 0 {
		t.Fatal("筛选无法解析时不应再查列表")
	}
}

func TestPublicService_ExploreBadParams(t *testing.T) {
	svc, _, _ := newPublicFixture(t)
	ctx := context.Background()
	for _, in := range []ExploreRequest{
		{Sort: "updated"},                  // 后台排序，前台不支持
		{Window: "3d"},                     // 窗口白名单外
		{Limit: 61},                        // 超过上限
		{Terms: "genre"},                   // 缺取值
		{Terms: "Genre:Romance"},           // 大写不是合法 slug
		{Terms: strings.Repeat("a:b,", 9)}, // 超过 8 项
		{Tag: "甜宠"},                        // 标签 slug 非法
		{Cursor: "not-base64!!"},
	} {
		if _, err := svc.Explore(ctx, in); !errors.Is(err, errs.ErrBadRequest) {
			t.Errorf("%+v 应返回 400，得到 %v", in, err)
		}
	}
}

func TestPublicService_TagFollowsMerge(t *testing.T) {
	svc, repo, _ := newPublicFixture(t)
	ctx := context.Background()
	repo.tagsBySlug["old"] = Tag{ID: 1, Name: "旧标签", Slug: "old", IsHidden: true, MergedIntoID: ptr(int64(2))}
	repo.tagsByID[2] = Tag{ID: 2, Name: "新标签", Slug: "new"}
	repo.cards = cards(1)
	repo.total = 1

	doc, err := svc.Tag(ctx, "old", ListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	tag := doc.Payload.Tag
	if tag.ID != 2 || tag.Name != "新标签" || tag.Slug != "old" || tag.CanonicalSlug != "new" || !tag.IsSensitive {
		t.Fatalf("已合并的标签应按目标标签展示并给出规范 slug：%+v", tag)
	}
	if repo.queries[0].TagID != 2 {
		t.Fatalf("应按目标标签取剧：%+v", repo.queries[0])
	}
	if _, err := svc.Tag(ctx, "missing", ListRequest{}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("标签不存在应 404，得到 %v", err)
	}
	if _, err := svc.Tag(ctx, "不是slug", ListRequest{}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("slug 非法应 404，得到 %v", err)
	}
}

func TestPublicService_Channel(t *testing.T) {
	svc, repo, cache := newPublicFixture(t)
	ctx := context.Background()
	repo.channel = ChannelView{ID: 9, Name: "短剧", Slug: "duanju"}
	repo.cards = cards(1, 2, 3)
	repo.total = 100

	doc, err := svc.Channel(ctx, "duanju", ChannelRequest{Sort: SortHot, Page: 3, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Payload.Page != 3 || doc.Payload.PageSize != 2 || doc.Payload.Total != 100 {
		t.Fatalf("页码分页信息不对：%+v", doc.Payload)
	}
	q := repo.queries[0]
	if q.Offset != 4 || q.Limit != 2 || q.Sort != SortHot || !slices.Equal(q.TermIDs, []int64{9}) {
		t.Fatalf("频道查询不对：%+v", q)
	}
	if !strings.Contains(cache.lastKey(), "content:channel:duanju:hot:3:2") {
		t.Fatalf("缓存键应含频道、排序与页码：%q", cache.lastKey())
	}

	// 频道页是页码分页，不支持随机排序；页码上限 100
	if _, err := svc.Channel(ctx, "duanju", ChannelRequest{Sort: SortRandom}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("频道页不应支持随机排序，得到 %v", err)
	}
	if _, err := svc.Channel(ctx, "duanju", ChannelRequest{Page: 101}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("超过最大页数应 400，得到 %v", err)
	}
	if _, err := svc.Channel(ctx, "missing", ChannelRequest{}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("频道不存在应 404，得到 %v", err)
	}
}

func TestPublicService_TopicManualAndAuto(t *testing.T) {
	svc, repo, _ := newPublicFixture(t)
	ctx := context.Background()
	repo.cards = cards(1, 2)
	repo.topic = Topic{
		ID: 4, Title: "甜宠专题", Slug: "sweet", Status: TopicPublished, SourceType: SourceManual,
		FAQ: []FAQ{{Question: "问", Answer: "答"}},
	}

	doc, err := svc.Topic(ctx, "sweet")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Payload.SourceType != SourceManual || len(doc.Payload.Dramas) != 2 || len(doc.Payload.FAQ) != 1 {
		t.Fatalf("手选专题应返回保存的剧与 FAQ：%+v", doc.Payload)
	}
	if len(repo.queries) != 0 {
		t.Fatal("手选专题不应执行规则查询")
	}

	// 自动专题：规则的默认值 + 取值编号解析 + 跟随标签合并
	repo.codeIDs["hot"] = 11
	repo.tagsByID[3] = Tag{ID: 3, Slug: "old", MergedIntoID: ptr(int64(4))}
	repo.topic = Topic{
		ID: 5, Title: "自动", Slug: "auto", Status: TopicPublished, SourceType: SourceAuto,
		Rule: TopicRule{TermCodes: []string{"hot", "hot"}, TagID: ptr(int64(3)), Limit: 999},
	}
	doc, err = svc.Topic(ctx, "auto")
	if err != nil {
		t.Fatal(err)
	}
	q := repo.queries[0]
	if !slices.Equal(q.TermIDs, []int64{11}) || q.TagID != 4 || q.Sort != SortLatest || q.Limit != maxTopicLimit {
		t.Fatalf("自动规则执行不对（去重取值、跟随合并、限制条数）：%+v", q)
	}
	if q.Since != nil {
		t.Fatalf("默认窗口为不限：%v", q.Since)
	}
	if len(doc.Payload.Dramas) == 0 {
		t.Fatal("自动专题应返回按规则选出的剧")
	}

	// 规则引用的取值编号已被删除：空列表
	repo.queries = nil
	repo.topic = Topic{ID: 6, Slug: "gone", Status: TopicPublished, SourceType: SourceAuto,
		Rule: TopicRule{TermCodes: []string{"missing"}}}
	doc, err = svc.Topic(ctx, "gone")
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Payload.Dramas) != 0 || len(repo.queries) != 0 {
		t.Fatalf("取值编号不存在时应空列表：%+v", doc.Payload.Dramas)
	}
}

func TestPublicService_TopicsCursor(t *testing.T) {
	svc, repo, _ := newPublicFixture(t)
	repo.topics = []Topic{{ID: 9, Title: "甲", Slug: "a"}, {ID: 8, Title: "乙", Slug: "b"}}

	doc, err := svc.Topics(context.Background(), ListRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Payload.List) != 2 || doc.Payload.Total != 2 || doc.Payload.NextCursor == nil {
		t.Fatalf("专题列表不对：%+v", doc.Payload)
	}
	pos, err := decodeCursor(*doc.Payload.NextCursor, sortTopics)
	if err != nil || pos.ID != 8 {
		t.Fatalf("专题游标应指向最后一条：%+v %v", pos, err)
	}
	// 专题列表的游标不能用在剧列表上
	if _, err := svc.Tag(context.Background(), "x", ListRequest{Cursor: *doc.Payload.NextCursor}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatalf("排序不一致的游标应 400，得到 %v", err)
	}
}

func TestCursor_RoundTripAndRejects(t *testing.T) {
	at := time.Date(2026, 3, 1, 8, 30, 0, 0, time.UTC)
	for _, c := range []struct {
		sort DramaSort
		card DramaCard
		want DramaCursor
	}{
		{SortLatest, DramaCard{ID: 5, PublishAt: &at}, DramaCursor{ID: 5, At: at}},
		{SortLatest, DramaCard{ID: 5, CreatedAt: at}, DramaCursor{ID: 5, At: at}}, // 没有上架时间时用创建时间
		{SortHot, DramaCard{ID: 6, HotScore: 1234}, DramaCursor{ID: 6, Num: 1234}},
		{SortRecommend, DramaCard{ID: 7, SortWeight: -3}, DramaCursor{ID: 7, Num: -3}},
		{SortRandom, DramaCard{ID: 8}, DramaCursor{ID: 8, Text: randomKey(8, 42)}},
	} {
		s := nextCursor(c.sort, 42, []DramaCard{c.card}, 1)
		if s == nil {
			t.Fatalf("%s：取满一页应有游标", c.sort)
		}
		pos, err := decodeCursor(*s, c.sort)
		if err != nil {
			t.Fatalf("%s：%v", c.sort, err)
		}
		got, err := pos.dramaCursor()
		if err != nil {
			t.Fatalf("%s：%v", c.sort, err)
		}
		if got.ID != c.want.ID || got.Num != c.want.Num || got.Text != c.want.Text || !got.At.Equal(c.want.At) {
			t.Fatalf("%s：游标往返不一致 %+v != %+v", c.sort, got, c.want)
		}
	}

	if nextCursor(SortHot, 0, cards(1), 2) != nil {
		t.Fatal("不足一页时没有下一页")
	}
	if _, err := decodeCursor("%%%", SortHot); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("非 base64 的游标应 400")
	}
	if _, err := decodeCursor(cursorPos{Sort: SortHot, Value: "1", ID: 2}.encode(), SortLatest); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("排序与游标不一致应 400")
	}
	if _, err := decodeCursor(cursorPos{Sort: SortHot, Value: "1", ID: 0}.encode(), SortHot); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("编号非正数的游标应 400")
	}
	bad := cursorPos{Sort: SortRandom, Value: "短", ID: 3}
	if _, err := mustDecode(t, bad).dramaCursor(); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("随机排序的游标值必须是 md5")
	}
	badNum := cursorPos{Sort: SortHot, Value: "abc", ID: 3}
	if _, err := mustDecode(t, badNum).dramaCursor(); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("热度游标值必须是整数")
	}
	if _, err := decodeCursor(strings.Repeat("A", maxCursorLen+1), SortHot); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("过长的游标应 400")
	}
}

func mustDecode(t *testing.T, p cursorPos) cursorPos {
	t.Helper()
	got, err := decodeCursor(p.encode(), p.Sort)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRandomSeed_StableWithin10Minutes(t *testing.T) {
	base := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	if randomSeed(base) != randomSeed(base.Add(9*time.Minute)) {
		t.Fatal("同一个 10 分钟窗口内种子应稳定")
	}
	if randomSeed(base) == randomSeed(base.Add(11*time.Minute)) {
		t.Fatal("下一个窗口应换种子")
	}
}

func TestPublicService_RandomKeepsSeedAcrossPages(t *testing.T) {
	svc, repo, _ := newPublicFixture(t)
	repo.cards = cards(1)
	doc, err := svc.Explore(context.Background(), ExploreRequest{Sort: SortRandom, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	seed := randomSeed(svc.now())
	if repo.queries[0].Seed != seed {
		t.Fatalf("第一页应用当前窗口的种子：%d", repo.queries[0].Seed)
	}
	// 翻页时即使跨过窗口边界也沿用游标里的种子，保证一轮翻页顺序一致
	svc.now = func() time.Time { return time.Date(2026, 10, 23, 13, 0, 0, 0, time.UTC) }
	if _, err := svc.Explore(context.Background(), ExploreRequest{Sort: SortRandom, Cursor: *doc.Payload.NextCursor, Limit: 1}); err != nil {
		t.Fatal(err)
	}
	if repo.queries[1].Seed != seed {
		t.Fatalf("后续页应沿用游标里的种子：%d != %d", repo.queries[1].Seed, seed)
	}
}

func TestParseTermPairs(t *testing.T) {
	got, err := parseTermPairs(" region:cn , genre:romance , genre:romance ")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"genre:romance", "region:cn"}) {
		t.Fatalf("应去重并排序：%v", got)
	}
	if got, err := parseTermPairs("  "); err != nil || got != nil {
		t.Fatalf("空筛选应为 nil：%v %v", got, err)
	}
}

func TestNewDoc_ETagFollowsPayload(t *testing.T) {
	a, err := newDoc(TopicCard{ID: 1, Title: "甲"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := newDoc(TopicCard{ID: 1, Title: "乙"})
	if err != nil {
		t.Fatal(err)
	}
	same, err := newDoc(TopicCard{ID: 1, Title: "甲"})
	if err != nil {
		t.Fatal(err)
	}
	if a.ETag == b.ETag {
		t.Fatal("内容不同 ETag 必须不同")
	}
	if a.ETag != same.ETag {
		t.Fatal("内容相同 ETag 必须相同")
	}
}
