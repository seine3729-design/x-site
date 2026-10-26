package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
)

// 前台内容读接口（S1-4）走真实 PostgreSQL：可见性、游标分页、筛选、分面、相关推荐、专题规则与索引使用。

// noCache 让每次调用都回源，同时仍走一遍 JSON 编解码（与真实缓存的序列化路径一致）。
type noCache struct{}

func (noCache) GetOrLoadJSON(ctx context.Context, _ string, _ time.Duration, dst any, load func(context.Context) (any, error)) error {
	v, err := load(ctx)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}

type pubFixture struct {
	pool  *pgxpool.Pool
	admin *content.Service
	svc   *content.PublicService
	mark  string
	now   time.Time

	taxonomy content.Taxonomy
	termA    content.Term // 频道取值，所有测试数据都挂上它，便于与其他数据隔离
	termB    content.Term
	termOff  content.Term // 已停用
	tagA     content.Tag
	tagS     content.Tag // 敏感标签
	tagH     content.Tag // 已隐藏
	tagM     content.Tag // 已并入 tagA
}

func newPubFixture(t *testing.T) *pubFixture {
	t.Helper()
	pool := testPool(t)
	mark := fmt.Sprintf("ps%d", time.Now().UnixNano())
	f := &pubFixture{
		pool:  pool,
		admin: content.NewService(NewContentRepo(pool)),
		svc:   content.NewPublicService(NewPublicContentRepo(pool), noCache{}),
		mark:  mark,
		now:   time.Now().UTC(),
	}
	t.Cleanup(func() {
		bg, like := context.Background(), f.mark+"%"
		_, _ = pool.Exec(bg, `DELETE FROM topic WHERE slug LIKE $1`, like)
		_, _ = pool.Exec(bg, `DELETE FROM drama WHERE title LIKE $1`, "%"+f.mark+"%")
		_, _ = pool.Exec(bg, `UPDATE tag SET merged_into_id = NULL WHERE slug LIKE $1`, like)
		_, _ = pool.Exec(bg, `DELETE FROM tag WHERE slug LIKE $1`, like)
		_, _ = pool.Exec(bg, `DELETE FROM taxonomy WHERE slug LIKE $1`, like)
	})

	ctx := context.Background()
	f.taxonomy = must(f.admin.CreateTaxonomy(ctx, content.TaxonomyInput{
		Name: f.mark + "维度", Slug: f.mark + "-tax", MultiSelect: true, ShowInExplore: true, Status: content.StatusActive,
	}))(t)
	f.termA = must(f.admin.CreateTerm(ctx, f.taxonomy.ID, content.TermInput{
		Name: f.mark + "甲", Slug: f.mark + "-a", Status: content.StatusActive, IsChannel: true, Code: f.mark + "-ca",
	}))(t)
	f.termB = must(f.admin.CreateTerm(ctx, f.taxonomy.ID, content.TermInput{
		Name: f.mark + "乙", Slug: f.mark + "-b", Status: content.StatusActive, Code: f.mark + "-cb",
	}))(t)
	f.termOff = must(f.admin.CreateTerm(ctx, f.taxonomy.ID, content.TermInput{
		Name: f.mark + "停", Slug: f.mark + "-off", Status: content.StatusInactive,
	}))(t)
	f.tagA = must(f.admin.CreateTag(ctx, content.TagInput{Name: f.mark + "普通", Slug: f.mark + "-ta", Status: content.TagActive}))(t)
	f.tagS = must(f.admin.CreateTag(ctx, content.TagInput{Name: f.mark + "敏感", Slug: f.mark + "-ts", IsHidden: true, Status: content.TagActive}))(t)
	f.tagH = must(f.admin.CreateTag(ctx, content.TagInput{Name: f.mark + "隐藏", Slug: f.mark + "-th", Status: content.TagHidden}))(t)
	f.tagM = must(f.admin.CreateTag(ctx, content.TagInput{Name: f.mark + "被并", Slug: f.mark + "-tm", Status: content.TagActive}))(t)
	return f
}

// drama 造一部剧：status 为 published 时按 publishAt 上架（零值表示立即）。
func (f *pubFixture) drama(t *testing.T, title string, in content.DramaInput) content.Drama {
	t.Helper()
	ctx := context.Background()
	in.Title = f.mark + title
	d := must(f.admin.CreateDrama(ctx, content.DramaInput{Title: in.Title}))(t)
	_, after, err := f.admin.UpdateDrama(ctx, d.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	return after
}

func (f *pubFixture) setTerms(t *testing.T, dramaID int64, termIDs ...int64) {
	t.Helper()
	if _, _, err := f.admin.SetDramaTerms(context.Background(), dramaID, termIDs); err != nil {
		t.Fatal(err)
	}
}

func (f *pubFixture) setTags(t *testing.T, dramaID int64, tagIDs ...int64) {
	t.Helper()
	if _, _, err := f.admin.SetDramaTags(context.Background(), dramaID, tagIDs); err != nil {
		t.Fatal(err)
	}
}

// visibleIn 返回探索接口在「本次测试的取值」下返回的剧编号（与其他数据隔离）。
func (f *pubFixture) explore(t *testing.T, in content.ExploreRequest) content.ExplorePage {
	t.Helper()
	if in.Terms == "" {
		in.Terms = f.taxonomy.Slug + ":" + f.termA.Slug
	}
	return must(f.svc.Explore(context.Background(), in))(t).Payload
}

func idsOf(list []content.DramaCard) []int64 {
	out := make([]int64, len(list))
	for i, c := range list {
		out[i] = c.ID
	}
	return out
}

func TestPublicContent_Visibility(t *testing.T) {
	f := newPubFixture(t)
	ctx := context.Background()
	future, past := f.now.Add(48*time.Hour), f.now.Add(-time.Hour)

	visible := f.drama(t, "可见", content.DramaInput{Status: content.DramaPublished, PublishAt: &past})
	draft := f.drama(t, "草稿", content.DramaInput{Status: content.DramaDraft})
	offline := f.drama(t, "下架", content.DramaInput{Status: content.DramaOffline})
	scheduled := f.drama(t, "定时", content.DramaInput{Status: content.DramaPublished, PublishAt: &future})
	deleted := f.drama(t, "回收站", content.DramaInput{Status: content.DramaPublished, PublishAt: &past})
	for _, id := range []int64{visible.ID, draft.ID, offline.ID, scheduled.ID, deleted.ID} {
		f.setTerms(t, id, f.termA.ID)
	}
	if _, err := f.admin.DeleteDrama(ctx, deleted.ID); err != nil {
		t.Fatal(err)
	}

	// 详情：只有可见的剧能读到
	if got := must(f.svc.Drama(ctx, visible.ID))(t).Payload; got.ID != visible.ID {
		t.Fatalf("可见的剧应能读到：%+v", got)
	}
	for name, id := range map[string]int64{"草稿": draft.ID, "下架": offline.ID, "定时未到点": scheduled.ID, "回收站": deleted.ID} {
		if _, err := f.svc.Drama(ctx, id); !errors.Is(err, errs.ErrNotFound) {
			t.Errorf("%s 的剧应 404，得到 %v", name, err)
		}
	}

	// 列表：同样只含可见的剧
	page := f.explore(t, content.ExploreRequest{})
	if !slices.Equal(idsOf(page.List), []int64{visible.ID}) || page.Total != 1 {
		t.Fatalf("列表只应含可见的剧：%v（共 %d）", idsOf(page.List), page.Total)
	}
}

func TestPublicContent_CursorPagingAllSorts(t *testing.T) {
	f := newPubFixture(t)
	const n = 7
	ids := make([]int64, 0, n+2) // 按创建顺序：上架时间递减、热度递增、排序值递减
	for i := range n {
		at := f.now.Add(-time.Duration(i+1) * time.Hour)
		d := f.drama(t, fmt.Sprintf("翻页%d", i), content.DramaInput{
			Status: content.DramaPublished, PublishAt: &at, ViewBase: int64((i + 1) * 10), SortWeight: int32(n - i),
		})
		f.setTerms(t, d.ID, f.termA.ID)
		ids = append(ids, d.ID)
	}
	// 再加两部上架时间只差一微秒的剧：游标精度必须与 timestamptz 一致，否则「最新」翻页会漏剧。
	// 它们的热度与排序值都是 0，排在最后，由编号倒序分出先后。
	close1 := f.now.Add(-30 * time.Minute)
	for i, at := range []time.Time{close1, close1.Add(time.Microsecond)} {
		d := f.drama(t, fmt.Sprintf("同刻%d", i), content.DramaInput{Status: content.DramaPublished, PublishAt: &at})
		f.setTerms(t, d.ID, f.termA.ID)
		ids = append(ids, d.ID)
	}
	total := len(ids)
	all := slices.Sorted(slices.Values(ids))

	// pageAll 按游标翻完所有页，返回拿到的编号顺序。
	pageAll := func(t *testing.T, sortBy content.DramaSort, limit int) []int64 {
		t.Helper()
		var got []int64
		cursor := ""
		for page := 0; ; page++ {
			if page > total {
				t.Fatalf("%s：翻页没有终止", sortBy)
			}
			p := f.explore(t, content.ExploreRequest{Sort: sortBy, Cursor: cursor, Limit: limit})
			got = append(got, idsOf(p.List)...)
			if p.Total != int64(total) {
				t.Fatalf("%s：总数应为 %d，得到 %d", sortBy, total, p.Total)
			}
			if p.NextCursor == nil {
				return got
			}
			cursor = *p.NextCursor
		}
	}

	for _, sortBy := range []content.DramaSort{content.SortLatest, content.SortHot, content.SortRecommend, content.SortRandom} {
		for _, limit := range []int{3, 1} {
			// 每页 1 条时游标会落在上架时间只差一微秒的两部剧之间：游标精度不足就会漏掉后一部。
			got := pageAll(t, sortBy, limit)
			// 不重不漏：重复或遗漏说明游标与 SQL 排序不一致（random 还顺带验证 Go 与 PostgreSQL 的 md5 一致）
			if ordered := slices.Sorted(slices.Values(got)); !slices.Equal(ordered, all) {
				t.Fatalf("%s（每页 %d 条）：翻完应覆盖全部 %d 部且不重复，得到 %v", sortBy, limit, total, got)
			}
		}
	}

	// 确定性排序的顺序
	wantLatest := slices.Concat([]int64{ids[8], ids[7]}, ids[:n])
	if got := idsOf(f.explore(t, content.ExploreRequest{Sort: content.SortLatest, Limit: 60}).List); !slices.Equal(got, wantLatest) {
		t.Fatalf("最新排序应按上架时间倒序（同一微秒内按编号倒序）：%v，期望 %v", got, wantLatest)
	}
	byHot := slices.Clone(ids[:n])
	slices.Reverse(byHot)
	wantHot := slices.Concat(byHot, []int64{ids[8], ids[7]})
	if got := idsOf(f.explore(t, content.ExploreRequest{Sort: content.SortHot, Limit: 60}).List); !slices.Equal(got, wantHot) {
		t.Fatalf("热度排序应按热度倒序：%v，期望 %v", got, wantHot)
	}
	wantRecommend := slices.Concat(ids[:n], []int64{ids[8], ids[7]})
	if got := idsOf(f.explore(t, content.ExploreRequest{Sort: content.SortRecommend, Limit: 60}).List); !slices.Equal(got, wantRecommend) {
		t.Fatalf("推荐排序应按排序值倒序：%v，期望 %v", got, wantRecommend)
	}
}

func TestPublicContent_Filters(t *testing.T) {
	f := newPubFixture(t)
	old, recent := f.now.Add(-60*24*time.Hour), f.now.Add(-2*time.Hour)

	both := f.drama(t, "甲乙", content.DramaInput{Status: content.DramaPublished, PublishAt: &recent, IsFinished: true})
	f.setTerms(t, both.ID, f.termA.ID, f.termB.ID)
	f.setTags(t, both.ID, f.tagA.ID)
	onlyA := f.drama(t, "只甲", content.DramaInput{Status: content.DramaPublished, PublishAt: &old})
	f.setTerms(t, onlyA.ID, f.termA.ID)

	tax := f.taxonomy.Slug
	tests := []struct {
		name string
		in   content.ExploreRequest
		want []int64
	}{
		{"单个取值", content.ExploreRequest{}, []int64{both.ID, onlyA.ID}},
		{"两个取值要同时满足", content.ExploreRequest{Terms: tax + ":" + f.termA.Slug + "," + tax + ":" + f.termB.Slug}, []int64{both.ID}},
		{"已停用的取值查不到", content.ExploreRequest{Terms: tax + ":" + f.termOff.Slug}, nil},
		{"不存在的取值查不到", content.ExploreRequest{Terms: tax + ":" + f.mark + "-none"}, nil},
		{"按标签", content.ExploreRequest{Tag: f.tagA.Slug}, []int64{both.ID}},
		{"完结", content.ExploreRequest{Finished: ptrOf(true)}, []int64{both.ID}},
		{"连载", content.ExploreRequest{Finished: ptrOf(false)}, []int64{onlyA.ID}},
		{"上新 30 天", content.ExploreRequest{Window: "30d"}, []int64{both.ID}},
		{"上新不限", content.ExploreRequest{Window: "all"}, []int64{both.ID, onlyA.ID}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page := f.explore(t, tt.in)
			got := idsOf(page.List)
			if len(tt.want) == 0 {
				if len(got) != 0 || page.Total != 0 {
					t.Fatalf("应为空：%v（共 %d）", got, page.Total)
				}
				return
			}
			slices.Sort(got)
			wantSorted := slices.Clone(tt.want)
			slices.Sort(wantSorted)
			if !slices.Equal(got, wantSorted) || page.Total != int64(len(wantSorted)) {
				t.Fatalf("得到 %v（共 %d），期望 %v", got, page.Total, wantSorted)
			}
		})
	}

	// 按标签筛选时也走标签合并后的目标标签
	if _, err := f.admin.MergeTag(context.Background(), f.tagM.ID, f.tagA.ID); err != nil {
		t.Fatal(err)
	}
	page := f.explore(t, content.ExploreRequest{Tag: f.tagM.Slug})
	if !slices.Equal(idsOf(page.List), []int64{both.ID}) {
		t.Fatalf("已合并的标签应按目标标签筛选：%v", idsOf(page.List))
	}
}

func TestPublicContent_Facets(t *testing.T) {
	f := newPubFixture(t)
	hidden := must(f.admin.CreateTaxonomy(context.Background(), content.TaxonomyInput{
		Name: f.mark + "不展示", Slug: f.mark + "-hid", ShowInExplore: false, Status: content.StatusActive,
	}))(t)
	must(f.admin.CreateTerm(context.Background(), hidden.ID, content.TermInput{
		Name: f.mark + "x", Slug: f.mark + "-x", Status: content.StatusActive,
	}))(t)

	page := f.explore(t, content.ExploreRequest{})
	if page.Facets == nil {
		t.Fatal("第一页应返回分面")
	}
	var mine *content.FacetTaxonomy
	for i, tx := range page.Facets.Taxonomies {
		if tx.ID == hidden.ID {
			t.Error("不在探索页显示的维度不应出现在分面里")
		}
		if tx.ID == f.taxonomy.ID {
			mine = &page.Facets.Taxonomies[i]
		}
	}
	if mine == nil {
		t.Fatal("启用且在探索页显示的维度应出现在分面里")
	}
	if !mine.MultiSelect {
		t.Error("分面应带是否可多选")
	}
	got := make([]string, 0, len(mine.Terms))
	for _, term := range mine.Terms {
		got = append(got, term.Slug)
	}
	if !slices.Equal(got, []string{f.termA.Slug, f.termB.Slug}) {
		t.Fatalf("分面只含启用的取值且按顺序：%v", got)
	}
	if !mine.Terms[0].IsChannel || mine.Terms[0].Code != f.termA.Code {
		t.Fatalf("分面应带频道标记与取值编号：%+v", mine.Terms[0])
	}
}

func TestPublicContent_DramaDetail(t *testing.T) {
	f := newPubFixture(t)
	ctx := context.Background()
	at := f.now.Add(-time.Hour)

	d := f.drama(t, "详情", content.DramaInput{
		Status: content.DramaPublished, PublishAt: &at, Aliases: []string{f.mark + "别名"},
		Summary: "简介", ViewBase: 1000, Score: ptrOf(8.5), Year: ptrOf(int32(2026)), SEOTitle: "SEO",
	})
	f.setTerms(t, d.ID, f.termA.ID, f.termOff.ID)
	f.setTags(t, d.ID, f.tagA.ID, f.tagS.ID, f.tagH.ID)
	// 合并后关联行会挪到目标标签，但历史数据里可能还留着指向已合并标签的行，前台不显示它
	if _, err := f.admin.MergeTag(ctx, f.tagM.ID, f.tagA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO drama_tag (drama_id, tag_id) VALUES ($1, $2)`, d.ID, f.tagM.ID); err != nil {
		t.Fatal(err)
	}
	ep1 := must(f.admin.CreateEpisode(ctx, d.ID, content.EpisodeInput{
		Number: 1, Title: "第一集", Access: content.AccessFree, DurationSec: 120,
	}))(t)
	must(f.admin.CreateEpisode(ctx, d.ID, content.EpisodeInput{
		Number: 2, Title: "第二集", Access: content.AccessVIP, TrialSec: 30, Cover: "c2.jpg", DurationSec: 100,
	}))(t)
	// 转封装完成（S1-5 写入）：可播放 + 首帧图；分集封面为空时用首帧图
	if _, err := f.pool.Exec(ctx, `UPDATE episode SET media_status = 'ready', poster = 'p1.jpg' WHERE id = $1`, ep1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE drama SET view_count = 23 WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	// 相关推荐：共享两项的排在共享一项的前面，不可见的不出现
	two := f.drama(t, "相关二", content.DramaInput{Status: content.DramaPublished, PublishAt: &at})
	f.setTerms(t, two.ID, f.termA.ID)
	f.setTags(t, two.ID, f.tagA.ID)
	one := f.drama(t, "相关一", content.DramaInput{Status: content.DramaPublished, PublishAt: &at, ViewBase: 5000})
	f.setTerms(t, one.ID, f.termA.ID)
	hiddenRelated := f.drama(t, "相关草稿", content.DramaInput{Status: content.DramaDraft})
	f.setTerms(t, hiddenRelated.ID, f.termA.ID)

	got := must(f.svc.Drama(ctx, d.ID))(t).Payload
	if got.PlayCount != 1023 {
		t.Errorf("播放次数应为初始播放量 + 真实播放量：%d", got.PlayCount)
	}
	if got.Score == nil || *got.Score != 8.5 || got.Year == nil || *got.Year != 2026 || got.SEOTitle != "SEO" {
		t.Errorf("资料字段不对：%+v", got)
	}
	if len(got.Tags) != 2 || got.Tags[0].ID != f.tagA.ID || got.Tags[1].ID != f.tagS.ID {
		t.Errorf("隐藏与已合并的标签不显示：%+v", got.Tags)
	}
	for _, tag := range got.Tags {
		if (tag.ID == f.tagS.ID) != tag.IsSensitive {
			t.Errorf("敏感标签标记不对：%+v", tag)
		}
	}
	if len(got.Terms) != 1 || got.Terms[0].ID != f.termA.ID || got.Terms[0].TaxonomyName != f.taxonomy.Name {
		t.Errorf("已停用的取值不显示，且取值要带所属维度：%+v", got.Terms)
	}
	if got.EpisodeCount != 2 || len(got.Episodes) != 2 {
		t.Fatalf("分集应全部返回：%+v", got.Episodes)
	}
	if !got.Episodes[0].Playable || got.Episodes[0].Cover != "p1.jpg" {
		t.Errorf("转封装完成的分集可播放，封面为空时用首帧图：%+v", got.Episodes[0])
	}
	if got.Episodes[1].Playable || got.Episodes[1].Cover != "c2.jpg" ||
		got.Episodes[1].Access != content.AccessVIP || got.Episodes[1].TrialSec != 30 {
		t.Errorf("未转封装的分集不可播放，并带 access 与试看秒数：%+v", got.Episodes[1])
	}
	related := idsOf(got.Related)
	if !slices.Equal(related, []int64{two.ID, one.ID}) {
		t.Fatalf("相关推荐应按共同项数排序且排除不可见的剧：%v", related)
	}
}

func TestPublicContent_ChannelAndTagPages(t *testing.T) {
	f := newPubFixture(t)
	ctx := context.Background()
	at := f.now.Add(-time.Hour)

	var ids []int64
	for i := range 3 {
		d := f.drama(t, fmt.Sprintf("频道%d", i), content.DramaInput{
			Status: content.DramaPublished, PublishAt: &at, ViewBase: int64(10 * (3 - i)),
		})
		f.setTerms(t, d.ID, f.termA.ID)
		f.setTags(t, d.ID, f.tagA.ID)
		ids = append(ids, d.ID)
	}

	// 频道页：页码分页 + 总数
	first := must(f.svc.Channel(ctx, f.termA.Slug, content.ChannelRequest{Sort: content.SortHot, PageSize: 2}))(t).Payload
	if first.Channel.ID != f.termA.ID || first.Channel.TaxonomySlug != f.taxonomy.Slug || first.Total != 3 {
		t.Fatalf("频道信息或总数不对：%+v", first)
	}
	if !slices.Equal(idsOf(first.List), []int64{ids[0], ids[1]}) {
		t.Fatalf("第一页应按热度倒序取两条：%v", idsOf(first.List))
	}
	second := must(f.svc.Channel(ctx, f.termA.Slug, content.ChannelRequest{Sort: content.SortHot, Page: 2, PageSize: 2}))(t).Payload
	if !slices.Equal(idsOf(second.List), []int64{ids[2]}) {
		t.Fatalf("第二页应是剩下的一条：%v", idsOf(second.List))
	}
	if page := must(f.svc.Channel(ctx, f.termA.Slug, content.ChannelRequest{Page: 50, PageSize: 2}))(t).Payload; len(page.List) != 0 {
		t.Fatalf("超出范围的页码返回空列表：%v", idsOf(page.List))
	}
	// 不是频道的取值、已停用的取值都 404
	if _, err := f.svc.Channel(ctx, f.termB.Slug, content.ChannelRequest{}); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("不是频道的取值应 404，得到 %v", err)
	}

	// 标签页：游标分页
	tagPage := must(f.svc.Tag(ctx, f.tagA.Slug, content.ListRequest{Sort: content.SortHot, Limit: 2}))(t).Payload
	if tagPage.Tag.ID != f.tagA.ID || tagPage.Tag.CanonicalSlug != f.tagA.Slug || tagPage.Tag.IsSensitive {
		t.Fatalf("标签信息不对：%+v", tagPage.Tag)
	}
	if tagPage.Total != 3 || len(tagPage.List) != 2 || tagPage.NextCursor == nil {
		t.Fatalf("标签页第一页不对：%+v", tagPage)
	}
	rest := must(f.svc.Tag(ctx, f.tagA.Slug, content.ListRequest{Sort: content.SortHot, Limit: 2, Cursor: *tagPage.NextCursor}))(t).Payload
	if !slices.Equal(slices.Concat(idsOf(tagPage.List), idsOf(rest.List)), ids) {
		t.Fatalf("两页合起来应是全部：%v + %v", idsOf(tagPage.List), idsOf(rest.List))
	}

	// 已隐藏的标签 404；已合并的标签跟随到目标
	if _, err := f.svc.Tag(ctx, f.tagH.Slug, content.ListRequest{}); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("已隐藏的标签应 404，得到 %v", err)
	}
	if _, err := f.admin.MergeTag(ctx, f.tagM.ID, f.tagA.ID); err != nil {
		t.Fatal(err)
	}
	merged := must(f.svc.Tag(ctx, f.tagM.Slug, content.ListRequest{Sort: content.SortHot}))(t).Payload
	if merged.Tag.ID != f.tagA.ID || merged.Tag.CanonicalSlug != f.tagA.Slug || merged.Tag.Slug != f.tagM.Slug || merged.Total != 3 {
		t.Fatalf("已合并的标签应按目标标签展示：%+v", merged.Tag)
	}
	// 敏感标签：可以访问，但要标出来
	f.setTags(t, ids[0], f.tagS.ID)
	sensitive := must(f.svc.Tag(ctx, f.tagS.Slug, content.ListRequest{}))(t).Payload
	if !sensitive.Tag.IsSensitive || sensitive.Total != 1 {
		t.Fatalf("敏感标签应标记 is_sensitive：%+v", sensitive.Tag)
	}
}

func TestPublicContent_Topics(t *testing.T) {
	f := newPubFixture(t)
	ctx := context.Background()
	at, old := f.now.Add(-time.Hour), f.now.Add(-60*24*time.Hour)

	hot := f.drama(t, "专题热", content.DramaInput{Status: content.DramaPublished, PublishAt: &at, ViewBase: 900})
	cold := f.drama(t, "专题冷", content.DramaInput{Status: content.DramaPublished, PublishAt: &at, ViewBase: 100})
	stale := f.drama(t, "专题旧", content.DramaInput{Status: content.DramaPublished, PublishAt: &old, ViewBase: 800})
	gone := f.drama(t, "专题删", content.DramaInput{Status: content.DramaPublished, PublishAt: &at})
	for _, id := range []int64{hot.ID, cold.ID, stale.ID, gone.ID} {
		f.setTerms(t, id, f.termB.ID)
		f.setTags(t, id, f.tagA.ID)
	}

	manual := must(f.admin.CreateTopic(ctx, content.TopicInput{
		Title: f.mark + "手选", Slug: f.mark + "-manual", Summary: "简介", Status: content.TopicPublished,
		SourceType: content.SourceManual, ListInIndex: true, FAQ: []content.FAQ{{Question: "问", Answer: "答"}},
	}))(t)
	if _, _, err := f.admin.SetTopicDramas(ctx, manual.ID, []int64{cold.ID, hot.ID, gone.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.DeleteDrama(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	auto := must(f.admin.CreateTopic(ctx, content.TopicInput{
		Title: f.mark + "自动", Slug: f.mark + "-auto", Status: content.TopicPublished, SourceType: content.SourceAuto,
		ListInIndex: true,
		Rule: content.TopicRule{
			TermCodes: []string{f.termB.Code}, TagID: ptrOf(f.tagA.ID), Sort: content.SortHot, Window: "30d", Limit: 2,
		},
	}))(t)
	draft := must(f.admin.CreateTopic(ctx, content.TopicInput{
		Title: f.mark + "草稿", Slug: f.mark + "-draft", Status: content.TopicDraft, SourceType: content.SourceManual, ListInIndex: true,
	}))(t)
	unlisted := must(f.admin.CreateTopic(ctx, content.TopicInput{
		Title: f.mark + "不收录", Slug: f.mark + "-unlisted", Status: content.TopicPublished, SourceType: content.SourceManual,
	}))(t)

	// 手选：按保存顺序，回收站中的剧不出现
	got := must(f.svc.Topic(ctx, manual.Slug))(t).Payload
	if !slices.Equal(idsOf(got.Dramas), []int64{cold.ID, hot.ID}) {
		t.Fatalf("手选专题应按保存顺序并排除回收站中的剧：%v", idsOf(got.Dramas))
	}
	if got.Summary != "简介" || len(got.FAQ) != 1 || got.FAQ[0].Question != "问" || got.SourceType != content.SourceManual {
		t.Fatalf("专题详情不对：%+v", got)
	}

	// 自动：按规则（取值编号 + 标签 + 30 天窗口 + 热度 + 取 2 部），超出窗口的旧剧不算
	got = must(f.svc.Topic(ctx, auto.Slug))(t).Payload
	if !slices.Equal(idsOf(got.Dramas), []int64{hot.ID, cold.ID}) {
		t.Fatalf("自动专题规则执行不对（上新窗口外的 #%d 不应入选）：%v", stale.ID, idsOf(got.Dramas))
	}

	// 未发布的专题 404；不收录的专题可以按 slug 打开但不在列表里
	if _, err := f.svc.Topic(ctx, draft.Slug); !errors.Is(err, errs.ErrNotFound) {
		t.Errorf("未发布的专题应 404，得到 %v", err)
	}
	if got := must(f.svc.Topic(ctx, unlisted.Slug))(t).Payload; got.ID != unlisted.ID {
		t.Errorf("不收录的专题应能按 slug 打开：%+v", got)
	}
	list := must(f.svc.Topics(ctx, content.ListRequest{Limit: 60}))(t).Payload
	inList := map[int64]bool{}
	for _, c := range list.List {
		inList[c.ID] = true
	}
	if !inList[manual.ID] || !inList[auto.ID] {
		t.Errorf("已发布且收录的专题应在列表里：%v", inList)
	}
	if inList[draft.ID] || inList[unlisted.ID] {
		t.Errorf("未发布或不收录的专题不应在列表里：%v", inList)
	}
}

// ---------- 搜索与联想（S3-4）----------

// setHot 直接写热度（生产里由排名任务 S3-5 写入）。
func (f *pubFixture) setHot(t *testing.T, dramaID int64, score int64) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), `UPDATE drama SET hot_score = $2 WHERE id = $1`, dramaID, score); err != nil {
		t.Fatal(err)
	}
}

// allOnFlags 让所有功能开关都算打开（与生产「没设置就是打开」一致）。
type allOnFlags struct{}

func (allOnFlags) Enabled(context.Context, string) (bool, error) { return true, nil }

// TestPublicContent_Search 走真实 PostgreSQL 验证搜索与联想的 SQL：
// 可见性、剧名与别名命中、大小写不敏感（迁移 00035 的 lower(...) bigm 索引）、通配符转义、游标分页与联想排序。
func TestPublicContent_Search(t *testing.T) {
	f := newPubFixture(t)
	ctx := context.Background()
	svc := content.NewSearchService(NewPublicContentRepo(f.pool), noCache{}, allOnFlags{}, nil)
	word := f.mark + "甜宠" // 夹具会给标题加上本次测试的标记，搜它就与其他数据隔离

	// 三部剧名以搜索词开头，一部只有别名命中（但热度最高，用来验证联想里前缀优先于热度）
	a := f.drama(t, "甜宠Sweet 日记", content.DramaInput{Status: content.DramaPublished})
	b := f.drama(t, "甜宠反转", content.DramaInput{Status: content.DramaPublished, Aliases: []string{f.mark + "Alias 100%"}})
	c := f.drama(t, "甜宠日常", content.DramaInput{Status: content.DramaPublished})
	d := f.drama(t, "反转剧", content.DramaInput{Status: content.DramaPublished, Aliases: []string{f.mark + "甜宠外传"}})
	// 热度由排名任务（S3-5）写入，测试里直接给值
	f.setHot(t, d.ID, 9999)
	f.setHot(t, a.ID, 500)
	f.setHot(t, b.ID, 300)
	f.setHot(t, c.ID, 100)
	// 不可见的三种：草稿、定时未到点、回收站
	f.drama(t, "甜宠草稿", content.DramaInput{Status: content.DramaDraft})
	f.drama(t, "甜宠定时", content.DramaInput{Status: content.DramaPublished, PublishAt: ptrOf(f.now.Add(48 * time.Hour))})
	trashed := f.drama(t, "甜宠回收站", content.DramaInput{Status: content.DramaPublished})
	if _, err := f.admin.DeleteDrama(ctx, trashed.ID); err != nil {
		t.Fatal(err)
	}

	// 默认按热度，只含可见的剧（别名命中的也算）
	page := must(svc.Search(ctx, content.SearchRequest{Q: word}))(t).Payload
	if got, want := idsOf(page.List), []int64{d.ID, a.ID, b.ID, c.ID}; !slices.Equal(got, want) {
		t.Fatalf("默认按热度且只含可见的：%v（期望 %v）", got, want)
	}
	if page.Total != 4 {
		t.Fatalf("总数应为 4：%d", page.Total)
	}
	if page.Query != word {
		t.Fatalf("回显搜索词：%q", page.Query)
	}

	// 大小写不敏感：标题里是 Sweet，用小写也能搜到（搜索词统一转小写，靠 lower(...) 索引匹配）
	if got := must(svc.Search(ctx, content.SearchRequest{Q: f.mark + "甜宠sweet"}))(t).Payload; len(got.List) != 1 || got.List[0].ID != a.ID {
		t.Fatalf("大小写不敏感命中：%v", idsOf(got.List))
	}
	// 别名命中（别名里也有大写）
	if got := must(svc.Search(ctx, content.SearchRequest{Q: f.mark + "alias"}))(t).Payload; len(got.List) != 1 || got.List[0].ID != b.ID {
		t.Fatalf("别名命中：%v", idsOf(got.List))
	}
	// 通配符按字面匹配：% 不是「任意字符」，只命中别名里真的有 % 的那部
	if got := must(svc.Search(ctx, content.SearchRequest{Q: f.mark + "alias 100%"}))(t).Payload; len(got.List) != 1 || got.List[0].ID != b.ID {
		t.Fatalf("%% 应按字面匹配：%v", idsOf(got.List))
	}
	if got := must(svc.Search(ctx, content.SearchRequest{Q: f.mark + "a%"}))(t).Payload; len(got.List) != 0 {
		t.Fatalf("a%% 不应当通配符用：%v", idsOf(got.List))
	}
	if got := must(svc.Search(ctx, content.SearchRequest{Q: "查无此剧" + f.mark}))(t).Payload; len(got.List) != 0 || got.Total != 0 {
		t.Fatalf("搜不到时是空结果：%+v", got)
	}

	// 游标分页：每页 2 部，总数不随分页变化
	first := must(svc.Search(ctx, content.SearchRequest{Q: word, Limit: 2}))(t).Payload
	if got, want := idsOf(first.List), []int64{d.ID, a.ID}; !slices.Equal(got, want) || first.NextCursor == nil || first.Total != 4 {
		t.Fatalf("第一页：%v total=%d", got, first.Total)
	}
	second := must(svc.Search(ctx, content.SearchRequest{Q: word, Limit: 2, Cursor: *first.NextCursor}))(t).Payload
	if got, want := idsOf(second.List), []int64{b.ID, c.ID}; !slices.Equal(got, want) {
		t.Fatalf("第二页：%v", got)
	}
	// 排序不一致的游标要被拒绝（与其他前台列表一致）
	if _, err := svc.Search(ctx, content.SearchRequest{Q: word, Sort: content.SortLatest, Cursor: *first.NextCursor}); err == nil {
		t.Fatal("换排序后旧游标应被拒绝")
	}
	// 换排序：最新上架在前（四部都在）
	if got := must(svc.Search(ctx, content.SearchRequest{Q: word, Sort: content.SortLatest}))(t).Payload; len(got.List) != 4 {
		t.Fatalf("按最新排序：%v", idsOf(got.List))
	}

	// 联想：剧名以搜索词开头的排在前面（即使别名命中的那部热度高得多），其余按热度
	sug := must(svc.Suggest(ctx, word, 0))(t).Payload
	if got, want := suggestIDs(sug.List), []int64{a.ID, b.ID, c.ID, d.ID}; !slices.Equal(got, want) {
		t.Fatalf("联想顺序（前缀优先，再按热度）：%v（期望 %v）", got, want)
	}
	if sug.List[0].Title != f.mark+"甜宠Sweet 日记" {
		t.Fatalf("联想返回原始标题：%q", sug.List[0].Title)
	}
	// 条数限制生效
	if got := must(svc.Suggest(ctx, word, 2))(t).Payload; len(got.List) != 2 {
		t.Fatalf("联想条数限制：%+v", got.List)
	}
	// 联想也只含可见的剧
	if got := must(svc.Suggest(ctx, f.mark+"甜宠草稿", 5))(t).Payload; len(got.List) != 0 {
		t.Fatalf("草稿不应出现在联想里：%+v", got.List)
	}
}

func suggestIDs(list []content.SuggestItem) []int64 {
	out := make([]int64, len(list))
	for i, s := range list {
		out[i] = s.ID
	}
	return out
}
