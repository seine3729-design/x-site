package pg

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
)

// 后台内容 CRUD（S1-3）走真实 PostgreSQL：领域服务 + 仓储，覆盖 SQL 筛选、排序、事务与级联。

type contentFixture struct {
	repo *ContentRepo
	svc  *content.Service
	mark string // 本次测试数据的标记：剧名包含它，slug 以它开头
}

func newContentFixture(t *testing.T) contentFixture {
	t.Helper()
	pool := testPool(t)
	f := contentFixture{repo: NewContentRepo(pool), mark: fmt.Sprintf("it%d", time.Now().UnixNano())}
	f.svc = content.NewService(f.repo)
	t.Cleanup(func() {
		bg, like := context.Background(), f.mark+"%"
		_, _ = pool.Exec(bg, `DELETE FROM topic WHERE slug LIKE $1`, like)
		_, _ = pool.Exec(bg, `DELETE FROM drama WHERE title LIKE $1`, "%"+f.mark+"%")
		_, _ = pool.Exec(bg, `UPDATE tag SET merged_into_id = NULL WHERE slug LIKE $1`, like)
		_, _ = pool.Exec(bg, `DELETE FROM tag WHERE slug LIKE $1`, like)
		_, _ = pool.Exec(bg, `DELETE FROM taxonomy WHERE slug LIKE $1`, like)
	})
	return f
}

// must 接住 (值, 错误) 两个返回值，用法：must(f.svc.CreateDrama(ctx, in))(t)。出错时测试失败。
func must[T any](v T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func ptrOf[T any](v T) *T { return &v }

func (f contentFixture) drama(t *testing.T, title string, in content.DramaInput) content.Drama {
	t.Helper()
	in.Title = f.mark + title
	return must(f.svc.CreateDrama(context.Background(), in))(t)
}

func (f contentFixture) ids(t *testing.T, filter content.DramaFilter) []int64 {
	t.Helper()
	if filter.Q == "" {
		filter.Q = f.mark
	}
	page := must(f.svc.ListDramas(context.Background(), filter))(t)
	out := make([]int64, len(page.List))
	for i, d := range page.List {
		out[i] = d.ID
	}
	return out
}

func TestContentRepo_DramaCRUDAndList(t *testing.T) {
	f := newContentFixture(t)
	ctx := context.Background()

	d := f.drama(t, "霸道总裁", content.DramaInput{
		Aliases: []string{"心动" + f.mark}, Slug: f.mark + "-ceo", Score: ptrOf(9.5), Year: ptrOf(int32(2024)), ViewBase: 100,
	})
	if d.Status != content.DramaDraft || d.HotScore != 100 || *d.Score != 9.5 || *d.Year != 2024 || d.Slug != f.mark+"-ceo" ||
		!slices.Equal(d.Aliases, []string{"心动" + f.mark}) || d.PublishAt != nil || d.LatestEpisode != 0 {
		t.Fatalf("新建为草稿，热度分 = 初始播放量：%+v", d)
	}
	if _, err := f.svc.CreateDrama(ctx, content.DramaInput{Title: f.mark + "重名", Slug: f.mark + "-CEO"}); !content.IsErr(err, content.ErrSlugTaken) {
		t.Fatalf("slug 冲突应返回 ErrSlugTaken，得到 %v", err)
	}
	// 剧的 slug 可以留空，多部剧留空不冲突
	noSlug1, noSlug2 := f.drama(t, "无别名一", content.DramaInput{}), f.drama(t, "无别名二", content.DramaInput{})
	if noSlug1.Slug != "" || noSlug2.Slug != "" {
		t.Fatal("剧的 slug 可以留空")
	}

	// 保存为已上架且没有上架时间：写入当前时间；热度分 = 初始播放量 + 真实播放量
	if _, err := f.repo.pool.Exec(ctx, `UPDATE drama SET view_count = 50 WHERE id = $1`, d.ID); err != nil {
		t.Fatal(err)
	}
	before, after, err := f.svc.UpdateDrama(ctx, d.ID, content.DramaInput{
		Title: f.mark + "霸道总裁（新）", Aliases: d.Aliases, Slug: "", Status: content.DramaPublished, ViewBase: 10, Score: nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if before.Title != d.Title || after.HotScore != 60 || after.PublishAt == nil || time.Since(*after.PublishAt) > time.Minute ||
		after.Slug != "" || after.Score != nil || !after.UpdatedAt.After(d.UpdatedAt) {
		t.Fatalf("保存结果不符合预期：%+v", after)
	}

	// 搜索：剧名、别名、#编号、纯数字
	if got := f.ids(t, content.DramaFilter{Q: "心动" + f.mark}); !slices.Equal(got, []int64{d.ID}) {
		t.Fatalf("按别名应命中：%v", got)
	}
	if got := f.ids(t, content.DramaFilter{Q: fmt.Sprintf("#%d", d.ID)}); !slices.Equal(got, []int64{d.ID}) {
		t.Fatalf("#编号只按编号：%v", got)
	}
	if got := f.ids(t, content.DramaFilter{Q: fmt.Sprint(d.ID)}); !slices.Contains(got, d.ID) {
		t.Fatalf("纯数字也按编号：%v", got)
	}
	if got := f.ids(t, content.DramaFilter{Q: f.mark + "%"}); len(got) != 0 {
		t.Fatalf("%% 按字面匹配，不是通配符：%v", got)
	}

	// 状态：已上架（已可见）、定时未到点、草稿、下架
	future := time.Now().Add(48 * time.Hour)
	scheduled := f.drama(t, "定时", content.DramaInput{})
	must2(f.svc.UpdateDrama(ctx, scheduled.ID, content.DramaInput{Title: scheduled.Title, Status: content.DramaPublished, PublishAt: &future}))(t)
	offline := f.drama(t, "下架", content.DramaInput{})
	must2(f.svc.UpdateDrama(ctx, offline.ID, content.DramaInput{Title: offline.Title, Status: content.DramaOffline}))(t)
	for status, want := range map[content.DramaStatus][]int64{
		content.DramaPublished: {d.ID},
		content.DramaScheduled: {scheduled.ID},
		content.DramaOffline:   {offline.ID},
		content.DramaDraft:     {noSlug2.ID, noSlug1.ID},
	} {
		if got := f.ids(t, content.DramaFilter{Status: status}); !slices.Equal(got, want) {
			t.Errorf("status=%s 应为 %v，得到 %v", status, want, got)
		}
	}

	// 排序与分页
	if _, err := f.repo.pool.Exec(ctx, `UPDATE drama SET sort_weight = CASE id WHEN $1 THEN 9 WHEN $2 THEN 5 ELSE 0 END,
		hot_score = CASE id WHEN $2 THEN 999 ELSE hot_score END WHERE title LIKE $3`, noSlug1.ID, offline.ID, "%"+f.mark+"%"); err != nil {
		t.Fatal(err)
	}
	all := []int64{offline.ID, scheduled.ID, noSlug2.ID, noSlug1.ID, d.ID} // 默认按编号倒序
	sorts := map[content.DramaSort][]int64{
		content.SortDefault:   all,
		content.SortRecommend: {noSlug1.ID, offline.ID, scheduled.ID, noSlug2.ID, d.ID},
		content.SortHot:       {offline.ID, d.ID, scheduled.ID, noSlug2.ID, noSlug1.ID},
		content.SortLatest:    {scheduled.ID, d.ID, offline.ID, noSlug2.ID, noSlug1.ID}, // 没有上架时间的在最后
	}
	for sort, want := range sorts {
		if got := f.ids(t, content.DramaFilter{Sort: sort}); !slices.Equal(got, want) {
			t.Errorf("sort=%q 应为 %v，得到 %v", sort, want, got)
		}
	}
	if got := f.ids(t, content.DramaFilter{Sort: content.SortUpdated}); got[0] != offline.ID {
		t.Errorf("sort=updated 最近修改的在前：%v", got)
	}
	page := must(f.svc.ListDramas(ctx, content.DramaFilter{Q: f.mark, PageRequest: content.PageRequest{Page: 2, PageSize: 2}}))(t)
	if page.Total != 5 || page.Page != 2 || page.PageSize != 2 || len(page.List) != 2 || page.List[0].ID != all[2] {
		t.Fatalf("第 2 页（每页 2 条）不符合预期：total=%d %+v", page.Total, page.List)
	}

	// 回收站：不能查看、修改，列表里没有
	deleted := must(f.svc.DeleteDrama(ctx, noSlug2.ID))(t)
	if deleted.ID != noSlug2.ID {
		t.Fatalf("删除应返回删除前的记录：%+v", deleted)
	}
	if _, err := f.svc.GetDrama(ctx, noSlug2.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("回收站中的剧应 404，得到 %v", err)
	}
	if _, _, err := f.svc.UpdateDrama(ctx, noSlug2.ID, content.DramaInput{Title: "x", Status: content.DramaDraft}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("回收站中的剧不能修改，得到 %v", err)
	}
	if _, err := f.svc.DeleteDrama(ctx, noSlug2.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不能重复删除，得到 %v", err)
	}
	if got := f.ids(t, content.DramaFilter{}); slices.Contains(got, noSlug2.ID) {
		t.Fatalf("列表不含回收站中的剧：%v", got)
	}
}

// must2 是三个返回值（修改前、修改后、错误）的 must。
func must2[A, B any](a A, b B, err error) func(*testing.T) (A, B) {
	return func(t *testing.T) (A, B) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return a, b
	}
}

// taxonomyPair 建一个单选维度（地区：r1、r2）与一个多选维度（类型：g1、g2）。
func (f contentFixture) taxonomyPair(t *testing.T) (region, genre content.Taxonomy, r1, r2, g1, g2 content.Term) {
	t.Helper()
	ctx := context.Background()
	region = must(f.svc.CreateTaxonomy(ctx, content.TaxonomyInput{Name: "地区", Slug: f.mark + "-region"}))(t)
	genre = must(f.svc.CreateTaxonomy(ctx, content.TaxonomyInput{Name: "类型", Slug: f.mark + "-genre", MultiSelect: true, ShowInExplore: true}))(t)
	r1 = must(f.svc.CreateTerm(ctx, region.ID, content.TermInput{Name: "大陆"}))(t)
	r2 = must(f.svc.CreateTerm(ctx, region.ID, content.TermInput{Name: "港台"}))(t)
	g1 = must(f.svc.CreateTerm(ctx, genre.ID, content.TermInput{Name: "都市"}))(t)
	g2 = must(f.svc.CreateTerm(ctx, genre.ID, content.TermInput{Name: "古装"}))(t)
	return
}

func TestContentRepo_RelationsAndBulk(t *testing.T) {
	f := newContentFixture(t)
	ctx := context.Background()
	d := f.drama(t, "关联", content.DramaInput{})
	d2 := f.drama(t, "批量二", content.DramaInput{})
	d3 := f.drama(t, "批量三", content.DramaInput{})
	gone := f.drama(t, "回收站", content.DramaInput{})
	must(f.svc.DeleteDrama(ctx, gone.ID))(t)

	// 标签与取值
	t1 := must(f.svc.CreateTag(ctx, content.TagInput{Name: "甜宠", Slug: f.mark + "-t1"}))(t)
	t2 := must(f.svc.CreateTag(ctx, content.TagInput{Name: "逆袭", Slug: f.mark + "-t2"}))(t)
	_, tags := must2(f.svc.SetDramaTags(ctx, d.ID, []int64{t2.ID, t1.ID}))(t)
	if !slices.Equal([]int64{tags[0].ID, tags[1].ID}, []int64{t1.ID, t2.ID}) {
		t.Fatalf("剧的标签：%+v", tags)
	}
	if _, _, err := f.svc.SetDramaTags(ctx, gone.ID, nil); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("回收站中的剧不能改标签，得到 %v", err)
	}
	region, genre, r1, r2, g1, g2 := f.taxonomyPair(t)
	_, terms := must2(f.svc.SetDramaTerms(ctx, d.ID, []int64{g2.ID, r1.ID, g1.ID}))(t)
	if len(terms) != 3 || terms[0].TaxonomyID != region.ID || terms[0].MultiSelect || terms[1].TaxonomyName != "类型" {
		t.Fatalf("取值按维度顺序返回并带维度信息：%+v", terms)
	}
	detail := must(f.svc.GetDrama(ctx, d.ID))(t)
	if len(detail.Tags) != 2 || len(detail.Terms) != 3 {
		t.Fatalf("剧集详情应带标签、取值：%+v", detail)
	}
	if got := f.ids(t, content.DramaFilter{TagID: t1.ID}); !slices.Equal(got, []int64{d.ID}) {
		t.Fatalf("按标签筛选：%v", got)
	}
	if got := f.ids(t, content.DramaFilter{TermID: g2.ID}); !slices.Equal(got, []int64{d.ID}) {
		t.Fatalf("按取值筛选：%v", got)
	}

	// 批量上架：只改状态不同的剧，回收站中的不动；上架时补上架时间
	ids := []int64{d2.ID, d3.ID, gone.ID}
	if n := must(f.svc.BulkDramas(ctx, content.BulkInput{IDs: ids, Action: content.BulkPublish}))(t); n != 2 {
		t.Fatalf("批量上架应影响 2 部，得到 %d", n)
	}
	if n := must(f.svc.BulkDramas(ctx, content.BulkInput{IDs: ids, Action: content.BulkPublish}))(t); n != 0 {
		t.Fatalf("已上架的不重复计数，得到 %d", n)
	}
	if got := must(f.repo.GetDrama(ctx, d2.ID))(t); got.Status != content.DramaPublished || got.PublishAt == nil {
		t.Fatalf("批量上架应补上架时间：%+v", got)
	}
	if n := must(f.svc.BulkDramas(ctx, content.BulkInput{IDs: []int64{d3.ID}, Action: content.BulkOffline}))(t); n != 1 {
		t.Fatalf("批量下架：%d", n)
	}

	// 批量追加标签
	if n := must(f.svc.BulkDramas(ctx, content.BulkInput{IDs: []int64{d.ID, d2.ID, gone.ID}, Action: content.BulkAddTags, TagIDs: []int64{t1.ID}}))(t); n != 1 {
		t.Fatalf("d 已有 t1，只有 d2 新加标签，得到 %d", n)
	}

	// 批量加入手选专题：追加在末尾，已在专题中的不动
	topic := must(f.svc.CreateTopic(ctx, content.TopicInput{Title: "手选", Slug: f.mark + "-topic"}))(t)
	if n := must(f.svc.BulkDramas(ctx, content.BulkInput{IDs: []int64{d3.ID, d2.ID}, Action: content.BulkAddToTopic, TopicID: topic.ID}))(t); n != 2 {
		t.Fatalf("加入专题：%d", n)
	}
	if n := must(f.svc.BulkDramas(ctx, content.BulkInput{IDs: []int64{d.ID, d2.ID, gone.ID}, Action: content.BulkAddToTopic, TopicID: topic.ID}))(t); n != 1 {
		t.Fatalf("已在专题中的不重复加入，回收站中的不加入：%d", n)
	}
	if got := topicDramaIDs(t, f, topic.ID); !slices.Equal(got, []int64{d3.ID, d2.ID, d.ID}) {
		t.Fatalf("按传入顺序追加在末尾：%v", got)
	}

	// 批量设取值：单选维度替换、多选维度追加
	if n := must(f.svc.BulkDramas(ctx, content.BulkInput{IDs: []int64{d.ID, d2.ID}, Action: content.BulkSetTerms, TermIDs: []int64{r2.ID, g1.ID}}))(t); n != 2 {
		t.Fatalf("d 换地区、d2 新加两个取值：%d", n)
	}
	got := must(f.repo.ListDramaTerms(ctx, d.ID))(t)
	if names := termNames(got); !slices.Equal(names, []string{"港台", "都市", "古装"}) {
		t.Fatalf("d 的地区应从大陆换成港台，类型保留都市、古装：%v", names)
	}
	if names := termNames(must(f.repo.ListDramaTerms(ctx, d2.ID))(t)); !slices.Equal(names, []string{"港台", "都市"}) {
		t.Fatalf("d2 的取值：%v", names)
	}
	if n := must(f.svc.BulkDramas(ctx, content.BulkInput{IDs: []int64{d.ID}, Action: content.BulkSetTerms, TermIDs: []int64{r2.ID}}))(t); n != 0 {
		t.Fatalf("取值没有变化时不计数：%d", n)
	}

	// 多选改单选：已有剧在该维度选了多个取值时拒绝
	_, _, err := f.svc.UpdateTaxonomy(ctx, genre.ID, content.TaxonomyInput{Name: "类型", Slug: genre.Slug})
	if errs.From(err).Code != errs.ErrConflict.Code {
		t.Fatalf("多选改单选应 409，得到 %v", err)
	}
	// 删除维度：取值与剧的关联一起删除
	deleted := must(f.svc.DeleteTaxonomy(ctx, genre.ID))(t)
	if len(deleted.Terms) != 2 {
		t.Fatalf("删除维度应返回删除前的取值：%+v", deleted)
	}
	if names := termNames(must(f.repo.ListDramaTerms(ctx, d.ID))(t)); !slices.Equal(names, []string{"港台"}) {
		t.Fatalf("删除维度后剧只剩地区取值：%v", names)
	}
}

func topicDramaIDs(t *testing.T, f contentFixture, topicID int64) []int64 {
	t.Helper()
	list := must(f.repo.ListTopicDramas(context.Background(), topicID))(t)
	out := make([]int64, len(list))
	for i, d := range list {
		out[i] = d.ID
	}
	return out
}

func termNames(terms []content.TermRef) []string {
	out := make([]string, len(terms))
	for i, t := range terms {
		out[i] = t.Name
	}
	return out
}

func TestContentRepo_Episodes(t *testing.T) {
	f := newContentFixture(t)
	ctx := context.Background()
	d := f.drama(t, "分集", content.DramaInput{})

	ep1 := must(f.svc.CreateEpisode(ctx, d.ID, content.EpisodeInput{Number: 1, DurationSec: 120, TrialSec: 30, Access: content.AccessVIP}))(t)
	ep3 := must(f.svc.CreateEpisode(ctx, d.ID, content.EpisodeInput{Number: 3, Title: "第三集"}))(t)
	if ep1.Access != content.AccessVIP || ep1.TrialSec != 30 || ep1.MediaStatus != "pending" || ep1.VideoObjectID != nil || ep3.Access != content.AccessFree {
		t.Fatalf("分集字段不符合预期：%+v %+v", ep1, ep3)
	}
	// 「更新至 N 集」只算可播放（转封装完成，media_status = ready）的分集（S1-5）
	latest := func() int32 { return must(f.repo.GetDrama(ctx, d.ID))(t).LatestEpisode }
	if latest() != 0 {
		t.Fatalf("还没有可播放的分集时更新至 0 集，得到 %d", latest())
	}
	if _, err := f.repo.pool.Exec(ctx, `UPDATE episode SET media_status = 'ready' WHERE id = ANY($1)`, []int64{ep1.ID, ep3.ID}); err != nil {
		t.Fatal(err)
	}
	must(f.svc.CreateEpisode(ctx, d.ID, content.EpisodeInput{Number: 5}))(t) // 还没转封装，不计入
	if latest() != 3 {
		t.Fatalf("更新至 N 集 = 可播放分集的最大集号，得到 %d", latest())
	}
	if _, err := f.svc.CreateEpisode(ctx, d.ID, content.EpisodeInput{Number: 3}); errs.From(err).Message != "第 3 集已存在" {
		t.Fatalf("集号重复应 409「第 3 集已存在」，得到 %v", err)
	}
	before, after := must2(f.svc.UpdateEpisode(ctx, ep3.ID, content.EpisodeInput{Number: 2, Title: "改为第二集", Access: content.AccessLogin}))(t)
	if before.Number != 3 || after.Number != 2 || after.Access != content.AccessLogin || latest() != 2 {
		t.Fatalf("改集号后刷新最新集号：%+v %+v %d", before, after, latest())
	}
	if _, _, err := f.svc.UpdateEpisode(ctx, ep3.ID, content.EpisodeInput{Number: 1}); errs.From(err).Message != "第 1 集已存在" {
		t.Fatalf("改成已有的集号应 409，得到 %v", err)
	}
	list := must(f.svc.ListEpisodes(ctx, d.ID))(t)
	if len(list) != 3 || list[0].Number != 1 || list[1].Number != 2 || list[2].Number != 5 {
		t.Fatalf("分集按集号排列：%+v", list)
	}
	must(f.svc.DeleteEpisode(ctx, ep3.ID))(t)
	if latest() != 1 {
		t.Fatalf("删除分集后刷新最新集号，得到 %d", latest())
	}
	if _, err := f.svc.DeleteEpisode(ctx, ep3.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删除不存在的分集应 404，得到 %v", err)
	}

	// 剧进了回收站：分集保留，但不能查看或修改
	must(f.svc.DeleteDrama(ctx, d.ID))(t)
	for name, err := range map[string]error{
		"列表": second(f.svc.ListEpisodes(ctx, d.ID)),
		"新增": second(f.svc.CreateEpisode(ctx, d.ID, content.EpisodeInput{Number: 9})),
		"修改": third(f.svc.UpdateEpisode(ctx, ep1.ID, content.EpisodeInput{Number: 1})),
		"删除": second(f.svc.DeleteEpisode(ctx, ep1.ID)),
	} {
		if !errors.Is(err, errs.ErrNotFound) {
			t.Errorf("剧在回收站时%s分集应 404，得到 %v", name, err)
		}
	}
	if n := count(t, f.repo.pool, `SELECT count(*) FROM episode WHERE drama_id = $1`, d.ID); n != 2 {
		t.Fatalf("放进回收站的剧保留分集（第 1、5 集），得到 %d", n)
	}
}

func second[A any](_ A, err error) error { return err }

func third[A, B any](_ A, _ B, err error) error { return err }

func TestContentRepo_TagMerge(t *testing.T) {
	f := newContentFixture(t)
	ctx := context.Background()
	d1, d2 := f.drama(t, "合并一", content.DramaInput{}), f.drama(t, "合并二", content.DramaInput{})
	src := must(f.svc.CreateTag(ctx, content.TagInput{Name: "甜甜", Slug: f.mark + "-src"}))(t)
	dst := must(f.svc.CreateTag(ctx, content.TagInput{Name: "甜宠", Slug: f.mark + "-dst"}))(t)
	old := must(f.svc.CreateTag(ctx, content.TagInput{Name: "甜", Slug: f.mark + "-old"}))(t)
	must2(f.svc.SetDramaTags(ctx, d1.ID, []int64{src.ID}))(t)
	must2(f.svc.SetDramaTags(ctx, d2.ID, []int64{src.ID, dst.ID}))(t)

	if moved := must(f.svc.MergeTag(ctx, old.ID, src.ID))(t); moved != 0 {
		t.Fatalf("old 没有剧：%d", moved)
	}
	if moved := must(f.svc.MergeTag(ctx, src.ID, dst.ID))(t); moved != 2 {
		t.Fatalf("src 原有 2 部剧，得到 %d", moved)
	}
	for _, d := range []int64{d1.ID, d2.ID} {
		tags := must(f.repo.ListDramaTags(ctx, d))(t)
		if len(tags) != 1 || tags[0].ID != dst.ID {
			t.Fatalf("剧 #%d 的标签应只剩目标标签：%+v", d, tags)
		}
	}
	for _, id := range []int64{src.ID, old.ID} {
		if got := must(f.repo.GetTag(ctx, id))(t); got.MergedIntoID == nil || *got.MergedIntoID != dst.ID {
			t.Fatalf("源标签与原先并入它的标签都应改指目标：%+v", got)
		}
	}
	if _, err := f.svc.MergeTag(ctx, src.ID, dst.ID); errs.From(err).Code != errs.ErrConflict.Code {
		t.Fatalf("已合并的标签不能再合并，得到 %v", err)
	}
	// 仓储在事务内再检查一次（领域层检查后被并发合并）
	if _, err := f.repo.MergeTag(ctx, src.ID, dst.ID); !content.IsErr(err, content.ErrStale) {
		t.Fatalf("事务内发现已合并应返回 ErrStale，得到 %v", err)
	}

	merged := true
	page := must(f.svc.ListTags(ctx, content.TagFilter{Q: f.mark, Merged: &merged}))(t)
	if page.Total != 2 {
		t.Fatalf("已合并的标签有 2 个，得到 %d", page.Total)
	}
	merged = false
	if page := must(f.svc.ListTags(ctx, content.TagFilter{Q: f.mark, Merged: &merged}))(t); page.Total != 1 || page.List[0].ID != dst.ID {
		t.Fatalf("未合并的只剩目标标签：%+v", page)
	}
	if _, _, err := f.svc.SetDramaTags(ctx, d1.ID, []int64{src.ID}); errs.From(err).Code != errs.ErrBadRequest.Code {
		t.Fatalf("不能给剧选已合并的标签，得到 %v", err)
	}
}

func TestContentRepo_TaxonomiesAndTerms(t *testing.T) {
	f := newContentFixture(t)
	ctx := context.Background()
	region, genre, r1, r2, _, _ := f.taxonomyPair(t)
	if genre.Sort <= region.Sort || r2.Sort <= r1.Sort || r1.Slug[:2] != "v-" {
		t.Fatalf("新维度、新取值排在最后；取值 slug 自动生成：%+v %+v %+v %+v", region, genre, r1, r2)
	}

	// slug 在维度内唯一；频道 slug 全站唯一；编号填写时全站唯一
	same := f.mark + "-city"
	must(f.svc.CreateTerm(ctx, region.ID, content.TermInput{Name: "城市", Slug: same, IsChannel: true, Code: f.mark}))(t)
	if _, err := f.svc.CreateTerm(ctx, region.ID, content.TermInput{Name: "重复", Slug: same}); !content.IsErr(err, content.ErrSlugTaken) {
		t.Fatalf("同一维度 slug 重复：%v", err)
	}
	other := must(f.svc.CreateTerm(ctx, genre.ID, content.TermInput{Name: "城市", Slug: same}))(t)
	if _, _, err := f.svc.UpdateTerm(ctx, genre.ID, other.ID, content.TermInput{Name: "城市", Slug: same, IsChannel: true}); !content.IsErr(err, content.ErrChannelSlugTaken) {
		t.Fatalf("频道 slug 全站唯一：%v", err)
	}
	if _, _, err := f.svc.UpdateTerm(ctx, genre.ID, other.ID, content.TermInput{Name: "城市", Slug: same, Code: f.mark}); !content.IsErr(err, content.ErrTermCodeTaken) {
		t.Fatalf("编号全站唯一：%v", err)
	}
	if _, _, err := f.svc.UpdateTerm(ctx, region.ID, other.ID, content.TermInput{Name: "x"}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("取值不属于该维度时 404：%v", err)
	}

	// 取值排序：按传入顺序；不是全部取值时 409
	ids := must(f.repo.TermIDs(ctx, region.ID))(t)
	slices.Sort(ids)
	slices.Reverse(ids)
	if err := f.svc.ReorderTerms(ctx, region.ID, ids[1:]); errs.From(err).Code != errs.ErrConflict.Code {
		t.Fatalf("排序列表缺项应 409：%v", err)
	}
	if err := f.svc.ReorderTerms(ctx, region.ID, ids); err != nil {
		t.Fatal(err)
	}
	tax := must(f.repo.GetTaxonomy(ctx, region.ID))(t)
	for i, term := range tax.Terms {
		if term.ID != ids[i] || term.Sort != int32(i) {
			t.Fatalf("取值应按传入顺序重排：%+v", tax.Terms)
		}
	}

	// 维度排序：库中全部维度倒过来，测试结束恢复
	orig := taxonomyOrder(t, f)
	t.Cleanup(func() { _ = f.repo.SetTaxonomySort(context.Background(), orig) })
	reversed := slices.Clone(orig)
	slices.Reverse(reversed)
	if err := f.svc.ReorderTaxonomies(ctx, reversed); err != nil {
		t.Fatal(err)
	}
	if got := taxonomyOrder(t, f); !slices.Equal(got, reversed) {
		t.Fatalf("维度应按传入顺序重排：%v", got)
	}

	// 删除取值
	must(f.svc.DeleteTerm(ctx, genre.ID, other.ID))(t)
	if _, err := f.svc.DeleteTerm(ctx, genre.ID, other.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("重复删除取值应 404：%v", err)
	}
}

func taxonomyOrder(t *testing.T, f contentFixture) []int64 {
	t.Helper()
	list := must(f.repo.ListTaxonomies(context.Background()))(t)
	out := make([]int64, len(list))
	for i, x := range list {
		out[i] = x.ID
	}
	return out
}

func TestContentRepo_Topics(t *testing.T) {
	f := newContentFixture(t)
	ctx := context.Background()
	_, _, r1, _, _, _ := f.taxonomyPair(t)
	code := f.mark + "c"
	must2(f.svc.UpdateTerm(ctx, r1.TaxonomyID, r1.ID, content.TermInput{Name: r1.Name, Slug: r1.Slug, Code: code}))(t)

	topic := must(f.svc.CreateTopic(ctx, content.TopicInput{
		Title: "年度好剧" + f.mark, Slug: f.mark + "-best", FAQ: []content.FAQ{{Question: "在哪看？", Answer: "本站"}},
	}))(t)
	got := must(f.svc.GetTopic(ctx, topic.ID))(t)
	if got.Status != content.TopicDraft || got.SourceType != content.SourceManual ||
		got.Rule.Sort != content.SortLatest || got.Rule.Limit != 24 || len(got.FAQ) != 1 || got.FAQ[0].Answer != "本站" || len(got.Dramas) != 0 {
		t.Fatalf("专题默认值与 FAQ 不符合预期：%+v", got)
	}

	d1, d2 := f.drama(t, "专题一", content.DramaInput{}), f.drama(t, "专题二", content.DramaInput{})
	before, after := must2(f.svc.SetTopicDramas(ctx, topic.ID, []int64{d2.ID, d1.ID}))(t)
	if len(before) != 0 || !slices.Equal(after, []int64{d2.ID, d1.ID}) || !slices.Equal(topicDramaIDs(t, f, topic.ID), []int64{d2.ID, d1.ID}) {
		t.Fatalf("手选剧按传入顺序保存：%v %v", before, after)
	}
	// 回收站中的剧：专题详情里不显示，整体替换时保留在专题里（还原后回到原位置）
	must(f.svc.DeleteDrama(ctx, d2.ID))(t)
	if got := topicDramaIDs(t, f, topic.ID); !slices.Equal(got, []int64{d1.ID}) {
		t.Fatalf("专题详情不含回收站中的剧：%v", got)
	}
	must2(f.svc.SetTopicDramas(ctx, topic.ID, []int64{d1.ID}))(t)
	if n := count(t, f.repo.pool, `SELECT count(*) FROM topic_drama WHERE topic_id = $1`, topic.ID); n != 2 {
		t.Fatalf("回收站中的剧应保留在专题里，得到 %d 行", n)
	}

	// 改为自动专题：规则引用的取值编号必须存在
	_, _, err := f.svc.UpdateTopic(ctx, topic.ID, content.TopicInput{Title: topic.Title, Slug: topic.Slug, SourceType: content.SourceAuto,
		Rule: content.TopicRule{TermCodes: []string{"gone" + f.mark}}})
	if errs.From(err).Code != errs.ErrBadRequest.Code {
		t.Fatalf("规则引用不存在的取值编号应 400，得到 %v", err)
	}
	rule := content.TopicRule{TermCodes: []string{code}, Sort: content.SortHot, Window: "7d", Limit: 12}
	_, updated := must2(f.svc.UpdateTopic(ctx, topic.ID, content.TopicInput{Title: topic.Title, Slug: topic.Slug,
		Status: content.TopicPublished, SourceType: content.SourceAuto, Rule: rule, ListInIndex: false}))(t)
	if updated.SourceType != content.SourceAuto || updated.Rule.Window != "7d" || updated.Rule.Limit != 12 ||
		!slices.Equal(updated.Rule.TermCodes, []string{code}) || updated.ListInIndex || len(updated.FAQ) != 0 {
		t.Fatalf("整体保存专题：%+v", updated)
	}
	if _, _, err := f.svc.SetTopicDramas(ctx, topic.ID, []int64{d1.ID}); errs.From(err).Code != errs.ErrConflict.Code {
		t.Fatalf("自动专题不能手选剧，得到 %v", err)
	}
	page := must(f.svc.ListTopics(ctx, content.TopicFilter{Q: f.mark, SourceType: content.SourceAuto, Status: content.TopicPublished}))(t)
	if page.Total != 1 || page.List[0].ID != topic.ID {
		t.Fatalf("专题列表筛选：%+v", page)
	}

	must(f.svc.DeleteTopic(ctx, topic.ID))(t)
	if n := count(t, f.repo.pool, `SELECT count(*) FROM topic_drama WHERE topic_id = $1`, topic.ID); n != 0 {
		t.Fatalf("删除专题后关联应删除，还剩 %d 行", n)
	}
}
