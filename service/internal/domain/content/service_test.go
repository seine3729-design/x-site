package content

import (
	"context"
	"errors"
	"math"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

// fakeRepo 只实现各测试用到的方法；嵌入的 Repo 为 nil，调用未实现的方法会 panic，说明走到了意外的分支。
type fakeRepo struct {
	Repo

	dramas     map[int64]Drama
	tags       map[int64]Tag
	terms      map[int64]TermRef
	topics     map[int64]Topic
	codes      map[string]bool
	taxonomies map[int64]Taxonomy
	termIDs    map[int64][]int64
	multiTerm  int64

	created     []DramaInput
	updated     []DramaInput
	tagIDs      []int64
	termSet     []int64
	bulk        []string
	bulkSingles []int64
	sorted      []int64
	topicDramas []int64
	merged      [2]int64
	slugs       []string
	slugTaken   int   // 前几次保存返回 slug 冲突
	createErr   error // CreateEpisode 返回的错误
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		dramas: map[int64]Drama{1: {ID: 1, Title: "霸道总裁", Status: DramaDraft}},
		tags: map[int64]Tag{
			20: {ID: 20, Name: "甜宠"},
			21: {ID: 21, Name: "逆袭"},
			22: {ID: 22, Name: "甜甜", MergedIntoID: ptr(int64(20))},
		},
		terms: map[int64]TermRef{
			30: {ID: 30, TaxonomyID: 3, TaxonomyName: "地区", Name: "大陆"},
			31: {ID: 31, TaxonomyID: 3, TaxonomyName: "地区", Name: "港台"},
			40: {ID: 40, TaxonomyID: 4, TaxonomyName: "类型", MultiSelect: true, Name: "都市"},
			41: {ID: 41, TaxonomyID: 4, TaxonomyName: "类型", MultiSelect: true, Name: "古装"},
		},
		topics: map[int64]Topic{
			50: {ID: 50, Title: "手选", SourceType: SourceManual},
			51: {ID: 51, Title: "自动", SourceType: SourceAuto},
		},
		codes: map[string]bool{"city": true, "ancient": true},
		taxonomies: map[int64]Taxonomy{
			3: {ID: 3, Name: "地区"},
			4: {ID: 4, Name: "类型", MultiSelect: true},
		},
		termIDs: map[int64][]int64{3: {30, 31}, 4: {40, 41}},
	}
}

func ptr[T any](v T) *T { return &v }

func (r *fakeRepo) GetDrama(_ context.Context, id int64) (Drama, error) {
	d, ok := r.dramas[id]
	if !ok {
		return Drama{}, errs.ErrNotFound
	}
	return d, nil
}

func (r *fakeRepo) CreateDrama(_ context.Context, in DramaInput) (Drama, error) {
	r.created = append(r.created, in)
	return Drama{ID: 2, Title: in.Title, Status: in.Status}, nil
}

func (r *fakeRepo) UpdateDrama(_ context.Context, id int64, in DramaInput) (Drama, error) {
	r.updated = append(r.updated, in)
	return Drama{ID: id, Title: in.Title, Status: in.Status, PublishAt: in.PublishAt}, nil
}

func (r *fakeRepo) TagsByIDs(_ context.Context, ids []int64) ([]Tag, error) {
	var out []Tag
	for _, id := range ids {
		if t, ok := r.tags[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *fakeRepo) ListDramaTags(context.Context, int64) ([]Tag, error) {
	out := make([]Tag, len(r.tagIDs))
	for i, id := range r.tagIDs {
		out[i] = r.tags[id]
	}
	return out, nil
}

func (r *fakeRepo) ReplaceDramaTags(_ context.Context, _ int64, ids []int64) error {
	r.tagIDs = ids
	return nil
}

func (r *fakeRepo) TermsByIDs(_ context.Context, ids []int64) ([]TermRef, error) {
	var out []TermRef
	for _, id := range ids {
		if t, ok := r.terms[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (r *fakeRepo) ListDramaTerms(context.Context, int64) ([]TermRef, error) {
	out := make([]TermRef, len(r.termSet))
	for i, id := range r.termSet {
		out[i] = r.terms[id]
	}
	return out, nil
}

func (r *fakeRepo) ReplaceDramaTerms(_ context.Context, _ int64, ids []int64) error {
	r.termSet = ids
	return nil
}

func (r *fakeRepo) BulkSetStatus(_ context.Context, ids []int64, status DramaStatus) (int64, error) {
	r.bulk = append(r.bulk, "status:"+string(status))
	return int64(len(ids)), nil
}

func (r *fakeRepo) BulkAddTags(_ context.Context, ids, _ []int64) (int64, error) {
	r.bulk = append(r.bulk, "add_tags")
	return int64(len(ids)), nil
}

func (r *fakeRepo) BulkAddToTopic(_ context.Context, topicID int64, ids []int64) (int64, error) {
	r.bulk = append(r.bulk, "add_to_topic")
	return int64(len(ids)), nil
}

func (r *fakeRepo) BulkSetTerms(_ context.Context, ids, _, singles []int64) (int64, error) {
	r.bulk = append(r.bulk, "set_terms")
	r.bulkSingles = singles
	return int64(len(ids)), nil
}

func (r *fakeRepo) GetTopic(_ context.Context, id int64) (Topic, error) {
	t, ok := r.topics[id]
	if !ok {
		return Topic{}, errs.ErrNotFound
	}
	return t, nil
}

func (r *fakeRepo) CreateEpisode(_ context.Context, dramaID int64, in EpisodeInput) (Episode, error) {
	return Episode{DramaID: dramaID, Number: in.Number, Access: in.Access}, r.createErr
}

// saveSlug 模拟带 slug 的保存：前 slugTaken 次返回冲突。
func (r *fakeRepo) saveSlug(slug string) error {
	r.slugs = append(r.slugs, slug)
	if r.slugTaken > 0 {
		r.slugTaken--
		return ErrSlugTaken
	}
	return nil
}

func (r *fakeRepo) CreateTag(_ context.Context, in TagInput) (Tag, error) {
	if err := r.saveSlug(in.Slug); err != nil {
		return Tag{}, err
	}
	return Tag{ID: 9, Name: in.Name, Slug: in.Slug, Status: in.Status}, nil
}

func (r *fakeRepo) GetTag(_ context.Context, id int64) (Tag, error) {
	t, ok := r.tags[id]
	if !ok {
		return Tag{}, errs.ErrNotFound
	}
	return t, nil
}

func (r *fakeRepo) MergeTag(_ context.Context, source, target int64) (int64, error) {
	r.merged = [2]int64{source, target}
	return 3, nil
}

func (r *fakeRepo) GetTaxonomy(_ context.Context, id int64) (Taxonomy, error) {
	t, ok := r.taxonomies[id]
	if !ok {
		return Taxonomy{}, errs.ErrNotFound
	}
	return t, nil
}

func (r *fakeRepo) CountMultiTermDramas(context.Context, int64) (int64, error) {
	return r.multiTerm, nil
}

func (r *fakeRepo) UpdateTaxonomy(_ context.Context, id int64, in TaxonomyInput) (Taxonomy, error) {
	if err := r.saveSlug(in.Slug); err != nil {
		return Taxonomy{}, err
	}
	return Taxonomy{ID: id, Name: in.Name, Slug: in.Slug, MultiSelect: in.MultiSelect}, nil
}

func (r *fakeRepo) TaxonomyIDs(context.Context) ([]int64, error) { return []int64{3, 4}, nil }

func (r *fakeRepo) SetTaxonomySort(_ context.Context, ids []int64) error {
	r.sorted = ids
	return nil
}

func (r *fakeRepo) TermIDs(_ context.Context, taxonomyID int64) ([]int64, error) {
	return r.termIDs[taxonomyID], nil
}

func (r *fakeRepo) SetTermSort(_ context.Context, _ int64, ids []int64) error {
	r.sorted = ids
	return nil
}

func (r *fakeRepo) ExistingTermCodes(_ context.Context, codes []string) ([]string, error) {
	var out []string
	for _, c := range codes {
		if r.codes[c] {
			out = append(out, c)
		}
	}
	return out, nil
}

func (r *fakeRepo) CreateTopic(_ context.Context, in TopicInput) (Topic, error) {
	if err := r.saveSlug(in.Slug); err != nil {
		return Topic{}, err
	}
	return Topic{ID: 60, Title: in.Title, Slug: in.Slug, Status: in.Status, SourceType: in.SourceType, Rule: in.Rule, FAQ: in.FAQ}, nil
}

func (r *fakeRepo) LiveDramaIDs(_ context.Context, ids []int64) ([]int64, error) {
	var out []int64
	for _, id := range ids {
		if _, ok := r.dramas[id]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}

func (r *fakeRepo) ListTopicDramas(context.Context, int64) ([]TopicDrama, error) {
	out := make([]TopicDrama, len(r.topicDramas))
	for i, id := range r.topicDramas {
		out[i] = TopicDrama{ID: id}
	}
	return out, nil
}

func (r *fakeRepo) ReplaceTopicDramas(_ context.Context, _ int64, ids []int64) error {
	r.topicDramas = ids
	return nil
}

// ---------- 断言工具 ----------

// wantErr 断言 err 的业务码与提示文案（文案只需包含 msg）。
func wantErr(t *testing.T, err error, code *errs.Error, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误 %s「%s」，得到 nil", code.Code, msg)
	}
	e := errs.From(err)
	if e.Code != code.Code || !strings.Contains(e.Message, msg) {
		t.Fatalf("期望 %s「%s」，得到 %s「%s」", code.Code, msg, e.Code, e.Message)
	}
}

func newTestService(r *fakeRepo) *Service {
	s := NewService(r)
	s.now = func() time.Time { return time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC) }
	return s
}

// ---------- 通用规则 ----------

func TestSlug(t *testing.T) {
	for s, want := range map[string]bool{
		"ceo-love": true, "a": true, "2024": true, "a-b-c9": true, strings.Repeat("a", 64): true,
		"": false, "-a": false, "a-": false, "A": false, "中文": false, "a_b": false, "a b": false, strings.Repeat("a", 65): false,
	} {
		if got := ValidSlug(s); got != want {
			t.Errorf("ValidSlug(%q) = %v，期望 %v", s, got, want)
		}
	}
	if got, err := cleanSlug("  CEO-Love "); err != nil || got != "ceo-love" {
		t.Fatalf("slug 应去掉首尾空白并转小写：%q %v", got, err)
	}
	re := regexp.MustCompile(`^t-[a-z2-7]{6}$`)
	seen := map[string]bool{}
	for range 50 {
		s := newSlug("t")
		if !re.MatchString(s) || !ValidSlug(s) {
			t.Fatalf("自动生成的 slug 格式不对：%q", s)
		}
		seen[s] = true
	}
	if len(seen) < 45 {
		t.Fatalf("自动生成的 slug 应基本不重复，50 次只有 %d 种", len(seen))
	}
}

func TestParseDramaQuery(t *testing.T) {
	tests := []struct {
		q       string
		pattern string
		id      int64
	}{
		{"#12", "", 12},
		{"12", "%12%", 12},
		{"总裁", "%总裁%", 0},
		{`50%_off\`, `%50\%\_off\\%`, 0},
		{"#0", "%#0%", 0},
		{"#12a", "%#12a%", 0},
		{"", "", 0},
	}
	for _, tt := range tests {
		pattern, id := parseDramaQuery(tt.q)
		if pattern != tt.pattern || id != tt.id {
			t.Errorf("parseDramaQuery(%q) = %q, %d；期望 %q, %d", tt.q, pattern, id, tt.pattern, tt.id)
		}
	}
	if q, err := cleanQuery(" 总\x00裁\n "); err != nil || q != "总裁" {
		t.Fatalf("搜索词应去掉控制字符与首尾空白：%q %v", q, err)
	}
	if _, err := cleanQuery(strings.Repeat("剧", 51)); err == nil {
		t.Fatal("搜索词超过 50 个字应拒绝")
	}
}

func TestLimitOffset(t *testing.T) {
	p, limit, offset, err := limitOffset(PageRequest{})
	if err != nil || p.Page != 1 || p.PageSize != 20 || limit != 20 || offset != 0 {
		t.Fatalf("默认第 1 页、每页 20 条：%+v %d %d %v", p, limit, offset, err)
	}
	if _, limit, offset, _ := limitOffset(PageRequest{Page: 3, PageSize: 50}); limit != 50 || offset != 100 {
		t.Fatalf("第 3 页每页 50 条应从第 100 条开始：%d %d", limit, offset)
	}
	for _, bad := range []PageRequest{{Page: -1}, {PageSize: 101}, {Page: 10001}, {PageSize: -5}} {
		if _, _, _, err := limitOffset(bad); err == nil {
			t.Errorf("分页参数 %+v 应拒绝", bad)
		}
	}
}

func TestDisplayStatus(t *testing.T) {
	now := time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	tests := []struct {
		d    Drama
		want DramaStatus
	}{
		{Drama{Status: DramaPublished, PublishAt: &future}, DramaScheduled},
		{Drama{Status: DramaPublished, PublishAt: &past}, DramaPublished},
		{Drama{Status: DramaPublished}, DramaPublished},
		{Drama{Status: DramaDraft, PublishAt: &future}, DramaDraft},
		{Drama{Status: DramaOffline}, DramaOffline},
	}
	for _, tt := range tests {
		if got := tt.d.DisplayStatus(now); got != tt.want {
			t.Errorf("%+v 的显示状态 = %s，期望 %s", tt.d, got, tt.want)
		}
	}
}

func TestWithAutoSlug(t *testing.T) {
	ctx := context.Background()
	r := newFakeRepo()
	s := newTestService(r)

	// 自动生成的 slug 碰巧冲突时换一个重试
	r.slugTaken = 2
	tag, err := s.CreateTag(ctx, TagInput{Name: " 甜宠 "})
	if err != nil || len(r.slugs) != 3 || !strings.HasPrefix(tag.Slug, "t-") || tag.Name != "甜宠" || tag.Status != TagActive {
		t.Fatalf("应重试到成功并使用 t- 前缀：%+v %v %v", tag, r.slugs, err)
	}

	// 用户填写的 slug 冲突直接返回 409，不重试
	r.slugs, r.slugTaken = nil, 1
	_, err = s.CreateTag(ctx, TagInput{Name: "逆袭", Slug: "ni-xi"})
	if !IsErr(err, ErrSlugTaken) || len(r.slugs) != 1 {
		t.Fatalf("填写的 slug 冲突应直接返回：%v %v", err, r.slugs)
	}

	// 一直冲突时最多试 3 次
	r.slugs, r.slugTaken = nil, 10
	if _, err := s.CreateTag(ctx, TagInput{Name: "复仇"}); !IsErr(err, ErrSlugTaken) || len(r.slugs) != 3 {
		t.Fatalf("自动 slug 最多重试 3 次：%v %v", err, r.slugs)
	}

	if _, err := s.CreateTag(ctx, TagInput{Name: "重生", Slug: "Bad_Slug"}); err == nil {
		t.Fatal("非法 slug 应拒绝")
	}
}

// ---------- 剧集 ----------

func TestCleanDrama(t *testing.T) {
	s := newTestService(newFakeRepo())
	in, err := s.cleanDrama(DramaInput{
		Title: "  霸道总裁  ", Aliases: []string{" 总裁 ", "", "总裁", "心动"}, Slug: " CEO ", Score: ptr(9.94), Year: ptr(int32(2024)),
		Status: DramaPublished,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "霸道总裁" || !slices.Equal(in.Aliases, []string{"总裁", "心动"}) || in.Slug != "ceo" || *in.Score != 9.9 {
		t.Fatalf("规范化结果不符合预期：%+v", in)
	}
	if in.Status != DramaDraft || in.PublishAt != nil {
		t.Fatalf("新建一律为草稿，不补上架时间：%+v", in)
	}
	if in, _ := s.cleanDrama(DramaInput{Title: "x", Score: ptr(-0.04)}, false); *in.Score != 0 || math.Signbit(*in.Score) {
		t.Fatalf("-0.04 四舍五入后应为 0（不是 -0）：%v", *in.Score)
	}

	bad := []struct {
		in  DramaInput
		msg string
	}{
		{DramaInput{Title: "  "}, "剧名不能为空"},
		{DramaInput{Title: strings.Repeat("剧", 201)}, "剧名不能超过 200 个字"},
		{DramaInput{Title: "x", Score: ptr(9.96)}, "评分"},
		{DramaInput{Title: "x", Score: ptr(-1.0)}, "评分"},
		{DramaInput{Title: "x", Year: ptr(int32(1899))}, "年份"},
		{DramaInput{Title: "x", ViewBase: -1}, "初始播放量"},
		{DramaInput{Title: "x", Slug: "-x"}, "slug"},
		{DramaInput{Title: "x", Badge: strings.Repeat("角", 17)}, "角标"},
		{DramaInput{Title: "x", Aliases: make21()}, "别名最多 20 个"},
	}
	for _, b := range bad {
		_, err := s.cleanDrama(b.in, false)
		wantErr(t, err, errs.ErrBadRequest, b.msg)
	}
}

func make21() []string {
	out := make([]string, 21)
	for i := range out {
		out[i] = strings.Repeat("别", i+1)
	}
	return out
}

func TestUpdateDrama_PublishAtAndStatus(t *testing.T) {
	ctx := context.Background()
	r := newFakeRepo()
	s := newTestService(r)

	before, after, err := s.UpdateDrama(ctx, 1, DramaInput{Title: "新名", Status: DramaPublished})
	if err != nil {
		t.Fatal(err)
	}
	if before.Title != "霸道总裁" || after.Title != "新名" {
		t.Fatalf("应返回修改前后的记录：%+v %+v", before, after)
	}
	if got := r.updated[0].PublishAt; got == nil || !got.Equal(s.now()) {
		t.Fatalf("保存为已上架且没有上架时间时写入当前时间：%v", got)
	}

	future := s.now().Add(48 * time.Hour)
	if _, _, err := s.UpdateDrama(ctx, 1, DramaInput{Title: "定时", Status: DramaPublished, PublishAt: &future}); err != nil {
		t.Fatal(err)
	}
	if got := r.updated[1].PublishAt; !got.Equal(future) {
		t.Fatalf("定时上架时间应原样保存：%v", got)
	}
	if _, _, err := s.UpdateDrama(ctx, 1, DramaInput{Title: "下架", Status: DramaOffline}); err != nil || r.updated[2].PublishAt != nil {
		t.Fatalf("下架不补上架时间：%v %v", r.updated[2].PublishAt, err)
	}

	_, _, err = s.UpdateDrama(ctx, 1, DramaInput{Title: "x", Status: DramaScheduled})
	wantErr(t, err, errs.ErrBadRequest, "状态只能是")
	_, _, err = s.UpdateDrama(ctx, 99, DramaInput{Title: "x", Status: DramaDraft})
	wantErr(t, err, errs.ErrNotFound, "")
}

func TestSetDramaTagsAndTerms(t *testing.T) {
	ctx := context.Background()
	r := newFakeRepo()
	s := newTestService(r)

	_, _, err := s.SetDramaTags(ctx, 1, []int64{20, 22})
	wantErr(t, err, errs.ErrBadRequest, "已并入 #20")
	_, _, err = s.SetDramaTags(ctx, 1, []int64{20, 99})
	wantErr(t, err, errs.ErrBadRequest, "标签 #99 不存在")
	_, after, err := s.SetDramaTags(ctx, 1, []int64{21, 20, 21})
	if err != nil || !slices.Equal(tagIDsOf(after), []int64{21, 20}) {
		t.Fatalf("重复的标签只保留一个，顺序不变：%+v %v", after, err)
	}

	_, _, err = s.SetDramaTerms(ctx, 1, []int64{30, 31})
	wantErr(t, err, errs.ErrBadRequest, "「地区」是单选维度")
	_, _, err = s.SetDramaTerms(ctx, 1, []int64{30, 98})
	wantErr(t, err, errs.ErrBadRequest, "取值 #98 不存在")
	if _, after, err := s.SetDramaTerms(ctx, 1, []int64{30, 40, 41}); err != nil || len(after) != 3 {
		t.Fatalf("单选维度一个、多选维度多个：%+v %v", after, err)
	}
}

func tagIDsOf(tags []Tag) []int64 {
	out := make([]int64, len(tags))
	for i, t := range tags {
		out[i] = t.ID
	}
	return out
}

func TestBulkDramas(t *testing.T) {
	ctx := context.Background()
	r := newFakeRepo()
	s := newTestService(r)

	tooMany := make([]int64, 201)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	bad := []struct {
		in   BulkInput
		code *errs.Error
		msg  string
	}{
		{BulkInput{Action: BulkPublish}, errs.ErrBadRequest, "请选择剧集"},
		{BulkInput{IDs: tooMany, Action: BulkPublish}, errs.ErrBadRequest, "最多 200 个"},
		{BulkInput{IDs: []int64{1}, Action: "delete"}, errs.ErrBadRequest, "批量操作无效"},
		{BulkInput{IDs: []int64{1}, Action: BulkAddTags}, errs.ErrBadRequest, "请选择标签"},
		{BulkInput{IDs: []int64{1}, Action: BulkAddTags, TagIDs: []int64{22}}, errs.ErrBadRequest, "已并入"},
		{BulkInput{IDs: []int64{1}, Action: BulkAddToTopic}, errs.ErrBadRequest, "请选择专题"},
		{BulkInput{IDs: []int64{1}, Action: BulkAddToTopic, TopicID: 51}, errs.ErrBadRequest, "只能加入手选专题"},
		{BulkInput{IDs: []int64{1}, Action: BulkAddToTopic, TopicID: 59}, errs.ErrBadRequest, "专题 #59 不存在"},
		{BulkInput{IDs: []int64{1}, Action: BulkSetTerms, TermIDs: []int64{30, 31}}, errs.ErrBadRequest, "单选维度"},
	}
	for _, b := range bad {
		_, err := s.BulkDramas(ctx, b.in)
		wantErr(t, err, b.code, b.msg)
	}
	if len(r.bulk) != 0 {
		t.Fatalf("校验失败时不应写库：%v", r.bulk)
	}

	for _, in := range []BulkInput{
		{IDs: []int64{1, 2, 1}, Action: BulkPublish},
		{IDs: []int64{1}, Action: BulkOffline},
		{IDs: []int64{1}, Action: BulkDraft},
		{IDs: []int64{1}, Action: BulkAddTags, TagIDs: []int64{20}},
		{IDs: []int64{1}, Action: BulkAddToTopic, TopicID: 50},
		{IDs: []int64{1}, Action: BulkSetTerms, TermIDs: []int64{31, 40}},
	} {
		if _, err := s.BulkDramas(ctx, in); err != nil {
			t.Fatalf("%s：%v", in.Action, err)
		}
	}
	want := []string{"status:published", "status:offline", "status:draft", "add_tags", "add_to_topic", "set_terms"}
	if !slices.Equal(r.bulk, want) {
		t.Fatalf("批量操作分派不对：%v", r.bulk)
	}
	if !slices.Equal(r.bulkSingles, []int64{3}) {
		t.Fatalf("set_terms 只替换单选维度（地区）的取值：%v", r.bulkSingles)
	}
}

// ---------- 分集 ----------

func TestCleanEpisode(t *testing.T) {
	in, err := cleanEpisode(EpisodeInput{Number: 3, Title: " 第三集 "})
	if err != nil || in.Access != AccessFree || in.Title != "第三集" {
		t.Fatalf("谁能看默认免费：%+v %v", in, err)
	}
	if _, err := cleanEpisode(EpisodeInput{Number: 1, DurationSec: 0, TrialSec: 30, Access: AccessVIP}); err != nil {
		t.Fatalf("时长未知（0）时试看秒数不限：%v", err)
	}
	bad := []struct {
		in  EpisodeInput
		msg string
	}{
		{EpisodeInput{Number: 0}, "集号"},
		{EpisodeInput{Number: 100001}, "集号"},
		{EpisodeInput{Number: 1, DurationSec: 60, TrialSec: 61}, "不能超过时长"},
		{EpisodeInput{Number: 1, DurationSec: -1}, "时长"},
		{EpisodeInput{Number: 1, TrialSec: -1}, "可试看秒数"},
		{EpisodeInput{Number: 1, Access: "paid"}, "谁能看"},
	}
	for _, b := range bad {
		_, err := cleanEpisode(b.in)
		wantErr(t, err, errs.ErrBadRequest, b.msg)
	}
}

func TestCreateEpisode_DuplicateNumber(t *testing.T) {
	r := newFakeRepo()
	r.createErr = ErrEpisodeNumberTaken
	_, err := newTestService(r).CreateEpisode(context.Background(), 1, EpisodeInput{Number: 3})
	wantErr(t, err, errs.ErrConflict, "第 3 集已存在")
}

// ---------- 标签 ----------

func TestMergeTag(t *testing.T) {
	ctx := context.Background()
	r := newFakeRepo()
	r.tags[23] = Tag{ID: 23, Name: "宠", MergedIntoID: ptr(int64(21))}
	s := newTestService(r)

	tests := []struct {
		source, target int64
		code           *errs.Error
		msg            string
	}{
		{20, 20, errs.ErrBadRequest, "不能并入自己"},
		{20, 0, errs.ErrBadRequest, "请选择要并入的标签"},
		{20, 99, errs.ErrBadRequest, "标签 #99 不存在"},
		{99, 20, errs.ErrNotFound, ""},
		{22, 21, errs.ErrConflict, "该标签已并入 #20"},
		{21, 22, errs.ErrConflict, "目标标签已并入 #20，请直接并入 #20"},
	}
	for _, tt := range tests {
		_, err := s.MergeTag(ctx, tt.source, tt.target)
		wantErr(t, err, tt.code, tt.msg)
	}
	moved, err := s.MergeTag(ctx, 21, 20)
	if err != nil || moved != 3 || r.merged != [2]int64{21, 20} {
		t.Fatalf("合并应交给仓储执行：%d %v %v", moved, r.merged, err)
	}
}

// ---------- 维度与取值 ----------

func TestUpdateTaxonomy_MultiToSingle(t *testing.T) {
	ctx := context.Background()
	r := newFakeRepo()
	s := newTestService(r)

	r.multiTerm = 2
	_, _, err := s.UpdateTaxonomy(ctx, 4, TaxonomyInput{Name: "类型", Slug: "genre", MultiSelect: false})
	wantErr(t, err, errs.ErrConflict, "有 2 部剧在该维度选了多个取值")

	r.multiTerm = 0
	before, after, err := s.UpdateTaxonomy(ctx, 4, TaxonomyInput{Name: "类型", MultiSelect: false})
	if err != nil || !before.MultiSelect || after.MultiSelect || !strings.HasPrefix(after.Slug, "f-") {
		t.Fatalf("没有剧选了多个取值时可以改为单选；slug 留空时重新生成：%+v %+v %v", before, after, err)
	}
	_, _, err = s.UpdateTaxonomy(ctx, 99, TaxonomyInput{Name: "x"})
	wantErr(t, err, errs.ErrNotFound, "")
}

func TestReorder(t *testing.T) {
	ctx := context.Background()
	r := newFakeRepo()
	s := newTestService(r)

	wantErr(t, s.ReorderTaxonomies(ctx, nil), errs.ErrBadRequest, "不能为空")
	wantErr(t, s.ReorderTaxonomies(ctx, []int64{3, 3}), errs.ErrBadRequest, "重复")
	wantErr(t, s.ReorderTaxonomies(ctx, []int64{4}), errs.ErrConflict, "列表已变化")
	wantErr(t, s.ReorderTaxonomies(ctx, []int64{4, 3, 5}), errs.ErrConflict, "列表已变化")
	if err := s.ReorderTaxonomies(ctx, []int64{4, 3}); err != nil || !slices.Equal(r.sorted, []int64{4, 3}) {
		t.Fatalf("应按传入顺序重排：%v %v", r.sorted, err)
	}

	wantErr(t, s.ReorderTerms(ctx, 3, []int64{40, 41}), errs.ErrConflict, "列表已变化")
	wantErr(t, s.ReorderTerms(ctx, 99, []int64{1}), errs.ErrNotFound, "")
	if err := s.ReorderTerms(ctx, 3, []int64{31, 30}); err != nil || !slices.Equal(r.sorted, []int64{31, 30}) {
		t.Fatalf("应按传入顺序重排取值：%v %v", r.sorted, err)
	}
}

func TestCleanTerm(t *testing.T) {
	in, err := cleanTerm(TermInput{Name: " 都市 ", Code: " City_01 "})
	if err != nil || in.Name != "都市" || in.Code != "City_01" || in.Status != StatusActive {
		t.Fatalf("规范化结果不符合预期：%+v %v", in, err)
	}
	for _, code := range []string{"城市", "a b", strings.Repeat("c", 33)} {
		_, err := cleanTerm(TermInput{Name: "x", Code: code})
		wantErr(t, err, errs.ErrBadRequest, "编号")
	}
	_, err = cleanTerm(TermInput{Name: "x", Status: "hidden"})
	wantErr(t, err, errs.ErrBadRequest, "状态")
}

// ---------- 专题 ----------

func TestCreateTopic(t *testing.T) {
	ctx := context.Background()
	r := newFakeRepo()
	s := newTestService(r)

	got, err := s.CreateTopic(ctx, TopicInput{Title: " 年度好剧 ", FAQ: []FAQ{{Question: " 在哪看？ ", Answer: " 本站 "}}})
	if err != nil {
		t.Fatal(err)
	}
	wantRule := TopicRule{TermCodes: []string{}, Sort: SortLatest, Window: "all", Limit: 24}
	if got.Status != TopicDraft || got.SourceType != SourceManual || !strings.HasPrefix(got.Slug, "s-") ||
		!rulesEqual(got.Rule, wantRule) || got.FAQ[0] != (FAQ{Question: "在哪看？", Answer: "本站"}) {
		t.Fatalf("默认草稿、手选、默认规则，FAQ 去掉首尾空白：%+v", got)
	}

	// 手选专题不校验规则引用（规则不生效）
	if _, err := s.CreateTopic(ctx, TopicInput{Title: "手选", Rule: TopicRule{TermCodes: []string{"gone"}}}); err != nil {
		t.Fatalf("手选专题保存时不检查规则引用：%v", err)
	}

	auto := TopicRule{TermCodes: []string{"city", "ancient"}, TagID: ptr(int64(20)), Sort: SortHot, Window: "30d", Limit: 60}
	got, err = s.CreateTopic(ctx, TopicInput{Title: "自动", SourceType: SourceAuto, Rule: auto})
	if err != nil || !rulesEqual(got.Rule, auto) {
		t.Fatalf("合法的自动规则原样保存：%+v %v", got.Rule, err)
	}

	bad := []struct {
		in  TopicInput
		msg string
	}{
		{TopicInput{Title: ""}, "标题不能为空"},
		{TopicInput{Title: "x", Status: "online"}, "状态"},
		{TopicInput{Title: "x", SourceType: "rss"}, "剧来源"},
		{TopicInput{Title: "x", FAQ: []FAQ{{Question: "问"}}}, "FAQ 回答不能为空"},
		{TopicInput{Title: "x", Rule: TopicRule{Limit: 61}}, "1–60"},
		{TopicInput{Title: "x", Rule: TopicRule{Window: "1y"}}, "上新时间"},
		{TopicInput{Title: "x", Rule: TopicRule{Sort: SortUpdated}}, "规则排序"},
		{TopicInput{Title: "x", SourceType: SourceAuto, Rule: TopicRule{TermCodes: []string{"gone"}}}, "取值编号「gone」不存在"},
		{TopicInput{Title: "x", SourceType: SourceAuto, Rule: TopicRule{TagID: ptr(int64(22))}}, "已并入"},
	}
	for _, b := range bad {
		_, err := s.CreateTopic(ctx, b.in)
		wantErr(t, err, errs.ErrBadRequest, b.msg)
	}
}

func rulesEqual(a, b TopicRule) bool {
	sameTag := (a.TagID == nil) == (b.TagID == nil) && (a.TagID == nil || *a.TagID == *b.TagID)
	return slices.Equal(a.TermCodes, b.TermCodes) && sameTag && a.Sort == b.Sort && a.Window == b.Window && a.Limit == b.Limit
}

func TestSetTopicDramas(t *testing.T) {
	ctx := context.Background()
	r := newFakeRepo()
	r.dramas[2] = Drama{ID: 2}
	r.topicDramas = []int64{2}
	s := newTestService(r)

	_, _, err := s.SetTopicDramas(ctx, 51, []int64{1})
	wantErr(t, err, errs.ErrConflict, "自动专题")
	_, _, err = s.SetTopicDramas(ctx, 50, []int64{1, 7})
	wantErr(t, err, errs.ErrBadRequest, "剧 #7 不存在")
	_, _, err = s.SetTopicDramas(ctx, 59, nil)
	wantErr(t, err, errs.ErrNotFound, "")

	before, after, err := s.SetTopicDramas(ctx, 50, []int64{1, 2, 1})
	if err != nil || !slices.Equal(before, []int64{2}) || !slices.Equal(after, []int64{1, 2}) || !slices.Equal(r.topicDramas, []int64{1, 2}) {
		t.Fatalf("应按传入顺序整体替换（去重）：%v %v %v", before, after, err)
	}
}

func TestIsErr(t *testing.T) {
	wrapped := errors.Join(errors.New("ctx"), ErrSlugTaken)
	if !IsErr(wrapped, ErrSlugTaken) || IsErr(ErrSlugTaken, ErrTermCodeTaken) || IsErr(errs.ErrConflict, ErrSlugTaken) {
		t.Fatal("IsErr 只认同一个哨兵错误")
	}
	if !errors.Is(ErrSlugTaken, errs.ErrConflict) {
		t.Fatal("哨兵错误仍是 CONFLICT，HTTP 层映射为 409")
	}
}
