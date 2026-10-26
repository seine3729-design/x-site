package site

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/ops"
	"xsite/service/internal/errs"
)

// TestBlockEnumsMatchOps 守住「区块里写死的榜单与广告位和运营域一致」：
// 站点编排域不依赖运营域（只在测试里引用，生产代码没有这条边）。
func TestBlockEnumsMatchOps(t *testing.T) {
	var boards []string
	for _, b := range ops.RankBoards {
		boards = append(boards, string(b))
	}
	if !slices.Equal(RankBoards, boards) {
		t.Fatalf("榜单列表与 domain/ops 不一致：%v vs %v", RankBoards, boards)
	}
	var slots []string
	for _, s := range ops.AdSlots {
		slots = append(slots, string(s))
	}
	if !slices.Equal(AdSlots, slots) {
		t.Fatalf("广告位列表与 domain/ops 不一致：%v vs %v", AdSlots, slots)
	}
}

// fakeBlockRepo 只实现测试用到的部分。
type fakeBlockRepo struct {
	blocks     []Block
	input      BlockInput
	order      []int64
	orderPage  string
	channels   []string // 启用的频道 slug
	offChannel string   // 存在但已停用的频道 slug
	dramaIDs   []int64  // 存在且不在回收站的剧
	topicIDs   []int64
	tagIDs     []int64
	termCodes  []string
	topic      BlockTopicRef
	topicErr   error
	cards      []content.DramaCard
	topicCards []content.TopicCard
	tags       []BlockTag
	cardsErr   error
}

func (f *fakeBlockRepo) ListBlocks(_ context.Context, page string) ([]Block, error) {
	var out []Block
	for _, b := range f.blocks {
		if b.Page == page {
			out = append(out, b)
		}
	}
	return out, nil
}

func (f *fakeBlockRepo) GetBlock(_ context.Context, id int64) (Block, error) {
	for _, b := range f.blocks {
		if b.ID == id {
			return b, nil
		}
	}
	return Block{}, errs.ErrNotFound
}

func (f *fakeBlockRepo) CreateBlock(_ context.Context, in BlockInput) (Block, error) {
	f.input = in
	return Block{ID: 9, Page: in.Page, Type: in.Type, Status: in.Status, SourceType: in.SourceType, Source: in.Source,
		ItemLimit: in.ItemLimit, ColsDesktop: in.ColsDesktop, CustomHTML: in.CustomHTML}, nil
}

func (f *fakeBlockRepo) UpdateBlock(_ context.Context, id int64, in BlockInput) (Block, error) {
	f.input = in
	return Block{ID: id, Page: in.Page, Type: in.Type, Status: in.Status, SourceType: in.SourceType, Source: in.Source}, nil
}

func (f *fakeBlockRepo) DeleteBlock(_ context.Context, id int64) (Block, error) {
	return Block{ID: id, Page: homePage}, nil
}

func (f *fakeBlockRepo) BlockIDsAt(ctx context.Context, page string) ([]int64, error) {
	list, _ := f.ListBlocks(ctx, page)
	ids := make([]int64, len(list))
	for i, b := range list {
		ids[i] = b.ID
	}
	return ids, nil
}

func (f *fakeBlockRepo) SetBlockOrder(_ context.Context, page string, ids []int64) error {
	f.orderPage, f.order = page, ids
	return nil
}

func (f *fakeBlockRepo) BlockPages(context.Context) ([]BlockPage, error) {
	return []BlockPage{{Value: homePage, Label: "首页"}, {Value: "channel:city", Label: "都市"}}, nil
}

func (f *fakeBlockRepo) MissingDramas(_ context.Context, ids []int64) ([]int64, error) {
	var missing []int64
	for _, id := range ids {
		if !slices.Contains(f.dramaIDs, id) {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

func (f *fakeBlockRepo) BlockDramaCards(_ context.Context, ids []int64) ([]content.DramaCard, error) {
	if f.cardsErr != nil {
		return nil, f.cardsErr
	}
	out := make([]content.DramaCard, 0, len(ids))
	for _, id := range ids {
		for _, c := range f.cards {
			if c.ID == id {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (f *fakeBlockRepo) BlockTopic(_ context.Context, id int64) (BlockTopicRef, error) {
	if f.topicErr != nil {
		return BlockTopicRef{}, f.topicErr
	}
	t := f.topic
	t.ID = id
	return t, nil
}

func (f *fakeBlockRepo) BlockTopicDramas(_ context.Context, _ int64, limit int) ([]content.DramaCard, error) {
	return truncate(f.cards, limit), nil
}

func (f *fakeBlockRepo) BlockTopicCards(_ context.Context, ids []int64) ([]content.TopicCard, error) {
	out := make([]content.TopicCard, 0, len(ids))
	for _, id := range ids {
		for _, c := range f.topicCards {
			if c.ID == id {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (f *fakeBlockRepo) BlockIndexTopics(_ context.Context, limit int) ([]content.TopicCard, error) {
	return truncate(f.topicCards, limit), nil
}

func (f *fakeBlockRepo) BlockTagsByIDs(_ context.Context, ids []int64) ([]BlockTag, error) {
	out := make([]BlockTag, 0, len(ids))
	for _, id := range ids {
		for _, tg := range f.tags {
			if tg.ID == id {
				out = append(out, tg)
			}
		}
	}
	return out, nil
}

func (f *fakeBlockRepo) BlockTopTags(_ context.Context, limit int) ([]BlockTag, error) {
	return truncate(f.tags, limit), nil
}

func (f *fakeBlockRepo) ExistingTopicIDs(_ context.Context, ids []int64) ([]int64, error) {
	return intersect(ids, f.topicIDs), nil
}

func (f *fakeBlockRepo) ExistingTagIDs(_ context.Context, ids []int64) ([]int64, error) {
	return intersect(ids, f.tagIDs), nil
}

func (f *fakeBlockRepo) ExistingTermCodes(_ context.Context, codes []string) ([]string, error) {
	var out []string
	for _, c := range codes {
		if slices.Contains(f.termCodes, c) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeBlockRepo) MenuTargets(_ context.Context, targets []MenuTarget) (map[MenuTarget]bool, error) {
	out := map[MenuTarget]bool{}
	for _, t := range targets {
		if t.Kind != LinkChannel {
			continue
		}
		switch {
		case slices.Contains(f.channels, t.Slug):
			out[t] = true
		case t.Slug == f.offChannel:
			out[t] = false // 存在但停用：没有前台频道页
		}
	}
	return out, nil
}

func intersect(ids, have []int64) []int64 {
	var out []int64
	for _, id := range ids {
		if slices.Contains(have, id) {
			out = append(out, id)
		}
	}
	return out
}

// fakePicker 记录收到的规则。
type fakePicker struct {
	rule  content.TopicRule
	cards []content.DramaCard
	err   error
}

func (f *fakePicker) PickDramas(_ context.Context, rule content.TopicRule) ([]content.DramaCard, error) {
	f.rule = rule
	return truncate(f.cards, rule.Limit), f.err
}

// fakeRanks 记录收到的榜单与条数。
type fakeRanks struct {
	board string
	limit int
	cards []content.DramaCard
	err   error
}

func (f *fakeRanks) RankDramas(_ context.Context, board string, limit int) ([]content.DramaCard, error) {
	f.board, f.limit = board, limit
	return truncate(f.cards, limit), f.err
}

func card(id int64) content.DramaCard { return content.DramaCard{ID: id, Title: "剧" + itoaTest(id)} }

func itoaTest(v int64) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune { return r }, string(rune('0'+v%10))))
}

func newBlockSvc() (*BlockService, *fakeBlockRepo, *memCache, *fakePicker, *fakeRanks) {
	repo := &fakeBlockRepo{
		channels:   []string{"city"},
		offChannel: "off",
		dramaIDs:   []int64{1, 2, 3},
		topicIDs:   []int64{10, 11},
		tagIDs:     []int64{20, 21},
		termCodes:  []string{"hot", "city"},
		cards:      []content.DramaCard{card(1), card(2), card(3)},
		topicCards: []content.TopicCard{
			{ID: 10, Title: "专题甲", Slug: "a"}, {ID: 11, Title: "专题乙", Slug: "b"},
		},
		tags: []BlockTag{{ID: 20, Name: "甜宠", Slug: "sweet"}, {ID: 21, Name: "逆袭", Slug: "rise"}},
	}
	c, picker, ranks := newMemCache(), &fakePicker{cards: []content.DramaCard{card(1), card(2)}}, &fakeRanks{cards: []content.DramaCard{card(3)}}
	return NewBlockService(repo, c, picker, ranks), repo, c, picker, ranks
}

func TestCleanBlockPageAndCommonFields(t *testing.T) {
	svc, _, _, _, _ := newBlockSvc()
	ctx := context.Background()
	base := BlockInput{Page: "", Type: BlockTagCloud}

	// 页面：空按首页，channel:{启用的频道}，其余 400
	in, err := svc.cleanBlock(ctx, base, false)
	if err != nil || in.Page != homePage {
		t.Fatalf("空页面按首页：%+v %v", in, err)
	}
	if in, err = svc.cleanBlock(ctx, BlockInput{Page: "channel:CITY", Type: BlockTagCloud}, false); err != nil || in.Page != "channel:city" {
		t.Fatalf("频道页转小写：%+v %v", in, err)
	}
	// 停用的频道（channel:off）没有前台页面，也要拒绝
	for _, bad := range []string{"channel:gone", "channel:", "explore", "channel:有中文", "channel:off"} {
		if _, err := svc.cleanBlock(ctx, BlockInput{Page: bad, Type: BlockTagCloud}, false); err == nil {
			t.Errorf("页面 %q 应拒绝", bad)
		}
	}
	if _, err := svc.Home(ctx, "channel:off"); errs.From(err).Code != errs.ErrNotFound.Code {
		t.Errorf("停用的频道页前台应 404：%v", err)
	}

	// 默认值：数量 24、每行 6、全部设备、启用
	if in.ItemLimit != 24 || in.ColsDesktop != 6 || in.Device != DeviceAll || in.Status != MenuActive {
		t.Fatalf("默认值：%+v", in)
	}

	bad := []struct {
		in  BlockInput
		msg string
	}{
		{BlockInput{Type: "banner"}, "请选择区块类型"},
		{BlockInput{Type: BlockTagCloud, Title: strings.Repeat("字", 51)}, "标题不能超过"},
		{BlockInput{Type: BlockTagCloud, Subtitle: strings.Repeat("字", 101)}, "副标题不能超过"},
		{BlockInput{Type: BlockTagCloud, ItemLimit: 61}, "数量为 1–60"},
		{BlockInput{Type: BlockTagCloud, ColsDesktop: 9}, "电脑每行个数为 2–8"},
		{BlockInput{Type: BlockTagCloud, MoreLink: "javascript:alert(1)"}, "查看全部链接"},
		{BlockInput{Type: BlockTagCloud, Device: "tv"}, "设备只能是"},
		{BlockInput{Type: BlockTagCloud, Status: "archived"}, "状态只能是启用或停用"},
	}
	for _, b := range bad {
		_, err := svc.cleanBlock(ctx, b.in, false)
		wantErr(t, err, errs.ErrBadRequest, b.msg)
	}
}

func TestCleanBlockSourceByType(t *testing.T) {
	svc, _, _, _, _ := newBlockSvc()
	ctx := context.Background()

	// 手选：编号去重、必须存在
	in, err := svc.cleanBlock(ctx, BlockInput{
		Type: BlockCarousel, SourceType: SourceManual, Source: BlockSource{DramaIDs: []int64{2, 1, 2}},
	}, false)
	if err != nil || !slices.Equal(in.Source.DramaIDs, []int64{2, 1}) {
		t.Fatalf("手选去重保序：%+v %v", in.Source, err)
	}
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockCarousel, SourceType: SourceManual, Source: BlockSource{DramaIDs: []int64{1, 99}}}, false)
	wantErr(t, err, errs.ErrBadRequest, "剧集 #99 不存在或在回收站")
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockCarousel, SourceType: SourceManual}, false)
	wantErr(t, err, errs.ErrBadRequest, "请至少选一个剧集编号")

	// 专题来源
	if in, err = svc.cleanBlock(ctx, BlockInput{Type: BlockHScroll, SourceType: SourceTopic, Source: BlockSource{TopicID: 10}}, false); err != nil || in.Source.TopicID != 10 {
		t.Fatalf("专题来源：%+v %v", in.Source, err)
	}
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockHScroll, SourceType: SourceTopic, Source: BlockSource{TopicID: 99}}, false)
	wantErr(t, err, errs.ErrBadRequest, "专题 #99 不存在")
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockHScroll, SourceType: SourceTopic}, false)
	wantErr(t, err, errs.ErrBadRequest, "请选择专题")

	// 自动来源：默认最新 + 不限，取值编号与标签要存在
	tagID := int64(20)
	in, err = svc.cleanBlock(ctx, BlockInput{
		Type: BlockPosterGrid, SourceType: SourceAuto,
		Source: BlockSource{TermCodes: []string{"hot", " city ", "hot"}, TagID: &tagID},
	}, false)
	if err != nil || !slices.Equal(in.Source.TermCodes, []string{"hot", "city"}) || in.Source.Sort != content.SortLatest || in.Source.Window != "all" {
		t.Fatalf("自动来源：%+v %v", in.Source, err)
	}
	for _, c := range []struct {
		src BlockSource
		msg string
	}{
		{BlockSource{TermCodes: []string{"gone"}}, "维度取值编号「gone」不存在"},
		{BlockSource{TagID: ptrInt64(99)}, "标签 #99 不存在"},
		{BlockSource{Sort: "random"}, "排序只能是"},
		{BlockSource{Window: "1y"}, "上新时间只能是"},
	} {
		_, err := svc.cleanBlock(ctx, BlockInput{Type: BlockPosterGrid, SourceType: SourceAuto, Source: c.src}, false)
		wantErr(t, err, errs.ErrBadRequest, c.msg)
	}
	// 剧类区块的来源方式必须是三种之一
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockCarousel, SourceType: "rss"}, false)
	wantErr(t, err, errs.ErrBadRequest, "剧来源只能是")

	// 非剧类：各自的参数
	if in, err = svc.cleanBlock(ctx, BlockInput{Type: BlockRank, Source: BlockSource{RankBoard: "drama_week"}}, false); err != nil || in.Source.RankBoard != "drama_week" {
		t.Fatalf("排行榜区块：%+v %v", in.Source, err)
	}
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockRank, Source: BlockSource{RankBoard: "tag_week"}}, false)
	wantErr(t, err, errs.ErrBadRequest, "请选择榜单")
	if in, err = svc.cleanBlock(ctx, BlockInput{Type: BlockAd, Source: BlockSource{AdSlot: "home_top"}}, false); err != nil || in.Source.AdSlot != "home_top" {
		t.Fatalf("广告位区块：%+v %v", in.Source, err)
	}
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockAd, Source: BlockSource{AdSlot: "nowhere"}}, false)
	wantErr(t, err, errs.ErrBadRequest, "请选择广告位置")
	if in, err = svc.cleanBlock(ctx, BlockInput{Type: BlockAnnounce}, false); err != nil || in.Source.AnnounceKind != "topbar" {
		t.Fatalf("公告条默认顶部滚动条：%+v %v", in.Source, err)
	}
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockAnnounce, Source: BlockSource{AnnounceKind: "inbox"}}, false)
	wantErr(t, err, errs.ErrBadRequest, "公告方式只能是")
	if in, err = svc.cleanBlock(ctx, BlockInput{Type: BlockTopicGroup, Source: BlockSource{TopicIDs: []int64{10, 11}}}, false); err != nil || len(in.Source.TopicIDs) != 2 {
		t.Fatalf("专题组：%+v %v", in.Source, err)
	}
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockTopicGroup, Source: BlockSource{TopicIDs: []int64{10, 99}}}, false)
	wantErr(t, err, errs.ErrBadRequest, "有专题编号不存在")
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockTagCloud, Source: BlockSource{TagIDs: []int64{20, 99}}}, false)
	wantErr(t, err, errs.ErrBadRequest, "有标签编号不存在")

	// 只保留该类型用得到的字段：排行榜区块不会存下剧集编号
	in, err = svc.cleanBlock(ctx, BlockInput{
		Type: BlockRank, Source: BlockSource{RankBoard: "drama_month", DramaIDs: []int64{1}, TopicID: 10, AdSlot: "home_top"},
	}, false)
	if err != nil || len(in.Source.DramaIDs) != 0 || in.Source.TopicID != 0 || in.Source.AdSlot != "" {
		t.Fatalf("只保留该类型的参数：%+v %v", in.Source, err)
	}
}

func TestCleanBlockCustomHTML(t *testing.T) {
	svc, _, _, _, _ := newBlockSvc()
	ctx := context.Background()
	html := `<p onclick="x()">内容<script>alert(1)</script></p>`

	// 没有权限：拒绝
	_, err := svc.cleanBlock(ctx, BlockInput{Type: BlockCustomHTML, CustomHTML: html}, false)
	if !errors.Is(err, ErrBlockCustomHTMLForbidden) {
		t.Fatalf("没有权限应 403：%v", err)
	}
	// 有权限：清洗后入库
	in, err := svc.cleanBlock(ctx, BlockInput{Type: BlockCustomHTML, CustomHTML: html}, true)
	if err != nil || strings.Contains(in.CustomHTML, "script") || strings.Contains(in.CustomHTML, "onclick") || !strings.Contains(in.CustomHTML, "内容") {
		t.Fatalf("清洗自定义 HTML：%q %v", in.CustomHTML, err)
	}
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockCustomHTML, CustomHTML: "<script>alert(1)</script>"}, true)
	wantErr(t, err, errs.ErrBadRequest, "清洗后为空")
	_, err = svc.cleanBlock(ctx, BlockInput{Type: BlockCustomHTML}, true)
	wantErr(t, err, errs.ErrBadRequest, "请填写自定义 HTML")
	// 其他类型不保存 HTML
	in, _ = svc.cleanBlock(ctx, BlockInput{Type: BlockTagCloud, CustomHTML: "<p>x</p>"}, true)
	if in.CustomHTML != "" {
		t.Fatalf("非自定义 HTML 区块不保存 HTML：%q", in.CustomHTML)
	}
}

func TestBlockCreateCapsPerPage(t *testing.T) {
	svc, repo, c, _, _ := newBlockSvc()
	ctx := context.Background()
	in := BlockInput{Page: homePage, Type: BlockTagCloud}

	// 新建成功后清掉该页面的缓存
	if _, err := svc.Create(ctx, in, false); err != nil {
		t.Fatalf("新建区块：%v", err)
	}
	if got := c.deletedKeys(); len(got) != 1 || got[0] != blockCacheKey(homePage) {
		t.Fatalf("新建后清该页面的缓存：%v", got)
	}

	// 页面已满 50 个：再建 400
	for i := range maxBlocksPerPage {
		repo.blocks = append(repo.blocks, Block{ID: int64(i + 1), Page: homePage, Type: BlockTagCloud})
	}
	_, err := svc.Create(ctx, in, false)
	wantErr(t, err, errs.ErrBadRequest, "单个页面最多 50 个区块")
	// 另一个页面不受影响
	if _, err := svc.Create(ctx, BlockInput{Page: "channel:city", Type: BlockTagCloud}, false); err != nil {
		t.Fatalf("别的页面不受影响：%v", err)
	}
}

func TestBlockUpdateKeepsCustomHTMLPermission(t *testing.T) {
	svc, repo, _, _, _ := newBlockSvc()
	ctx := context.Background()
	repo.blocks = []Block{{ID: 5, Page: homePage, Type: BlockCustomHTML, CustomHTML: "<p>老内容</p>"}}

	// 原本是自定义 HTML 的区块，没有权限连改成别的类型都不行
	_, _, err := svc.Update(ctx, 5, BlockInput{Type: BlockTagCloud}, false)
	if !errors.Is(err, ErrBlockCustomHTMLForbidden) {
		t.Fatalf("改原本是 HTML 的区块要权限：%v", err)
	}
	if _, _, err = svc.Update(ctx, 5, BlockInput{Type: BlockTagCloud}, true); err != nil {
		t.Fatalf("有权限可以改：%v", err)
	}
}

func TestBlockHomeBuildsEachType(t *testing.T) {
	svc, repo, c, picker, ranks := newBlockSvc()
	ctx := context.Background()
	tagID := int64(20)
	repo.blocks = []Block{
		{ID: 1, Page: homePage, Type: BlockCarousel, Title: "手选", Status: MenuActive, ItemLimit: 2,
			SourceType: SourceManual, Source: BlockSource{DramaIDs: []int64{3, 1, 2}}},
		{ID: 2, Page: homePage, Type: BlockPosterGrid, Title: "自动", Status: MenuActive, ItemLimit: 5,
			SourceType: SourceAuto, Source: BlockSource{TermCodes: []string{"hot"}, TagID: &tagID, Sort: content.SortHot, Window: "7d"}},
		{ID: 3, Page: homePage, Type: BlockRank, Title: "周榜", Status: MenuActive, ItemLimit: 3, Source: BlockSource{RankBoard: "drama_week"}},
		{ID: 4, Page: homePage, Type: BlockTopicGroup, Title: "专题", Status: MenuActive, ItemLimit: 1, Source: BlockSource{TopicIDs: []int64{11, 10}}},
		{ID: 5, Page: homePage, Type: BlockTagCloud, Title: "标签", Status: MenuActive, ItemLimit: 9},
		{ID: 6, Page: homePage, Type: BlockAd, Status: MenuActive, Source: BlockSource{AdSlot: "home_top"}},
		{ID: 7, Page: homePage, Type: BlockAnnounce, Status: MenuActive, Source: BlockSource{AnnounceKind: "topbar"}},
		{ID: 8, Page: homePage, Type: BlockCustomHTML, Status: MenuActive, CustomHTML: "<p>自定义</p>"},
		{ID: 9, Page: homePage, Type: BlockCarousel, Title: "停用", Status: MenuInactive, SourceType: SourceManual, Source: BlockSource{DramaIDs: []int64{1}}},
		{ID: 10, Page: "channel:city", Type: BlockTagCloud, Status: MenuActive, ItemLimit: 5},
	}

	home, err := svc.Home(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if home.Page != homePage || len(home.Blocks) != 8 {
		t.Fatalf("首页应有 8 个区块（停用与频道页的不算）：%d %+v", len(home.Blocks), home.Page)
	}
	byID := map[int64]PublicBlock{}
	for _, b := range home.Blocks {
		byID[b.ID] = b
	}
	// 手选：按给定顺序，截到 item_limit
	if got := byID[1].Dramas; len(got) != 2 || got[0].ID != 3 || got[1].ID != 1 {
		t.Fatalf("手选区块按顺序取前 2 部：%+v", got)
	}
	// 自动：规则与条数传给选剧引擎
	if picker.rule.Limit != 5 || picker.rule.Sort != content.SortHot || picker.rule.Window != "7d" || len(picker.rule.TermCodes) != 1 {
		t.Fatalf("自动区块的规则：%+v", picker.rule)
	}
	// 排行榜：榜单名下发，剧由榜单服务给
	if byID[3].RankBoard != "drama_week" || ranks.board != "drama_week" || ranks.limit != 3 || len(byID[3].Dramas) != 1 {
		t.Fatalf("排行榜区块：%+v（board=%s limit=%d）", byID[3], ranks.board, ranks.limit)
	}
	// 专题组：按给定顺序，截断
	if got := byID[4].Topics; len(got) != 1 || got[0].ID != 11 {
		t.Fatalf("专题组按顺序取前 1 个：%+v", got)
	}
	// 标签云：没指定时取热门标签
	if got := byID[5].Tags; len(got) != 2 || got[0].Name != "甜宠" {
		t.Fatalf("标签云：%+v", got)
	}
	// 广告位与公告条只给参数，自定义 HTML 给清洗后的内容
	if byID[6].AdSlot != "home_top" || len(byID[6].Dramas) != 0 {
		t.Fatalf("广告位区块只给位置：%+v", byID[6])
	}
	if byID[7].AnnounceKind != "topbar" || byID[8].HTML != "<p>自定义</p>" {
		t.Fatalf("公告条与自定义 HTML：%+v %+v", byID[7], byID[8])
	}
	// 第二次命中缓存
	if _, err := svc.Home(ctx, homePage); err != nil || c.loads != 1 {
		t.Fatalf("第二次应命中缓存：loads=%d %v", c.loads, err)
	}

	// 频道页单独一份
	ch, err := svc.Home(ctx, "channel:city")
	if err != nil || ch.Page != "channel:city" || len(ch.Blocks) != 1 {
		t.Fatalf("频道页区块：%+v %v", ch, err)
	}
	// 未知页面 404
	if _, err := svc.Home(ctx, "channel:gone"); errs.From(err).Code != errs.ErrNotFound.Code {
		t.Fatalf("未知频道页应 404：%v", err)
	}
}

func TestBlockHomeSkipsEmptyAndBrokenBlocks(t *testing.T) {
	svc, repo, _, picker, _ := newBlockSvc()
	ctx := context.Background()
	repo.blocks = []Block{
		{ID: 1, Page: homePage, Type: BlockCarousel, Status: MenuActive, ItemLimit: 6, SourceType: SourceManual, Source: BlockSource{DramaIDs: []int64{1}}},
		{ID: 2, Page: homePage, Type: BlockPosterGrid, Status: MenuActive, ItemLimit: 6, SourceType: SourceAuto},
		{ID: 3, Page: homePage, Type: BlockHScroll, Status: MenuActive, ItemLimit: 6, SourceType: SourceTopic, Source: BlockSource{TopicID: 10}},
	}
	// 2 号区块取数报错、3 号的专题已被删除：都跳过，1 号照常下发
	picker.err = errors.New("数据库超时")
	repo.topicErr = errs.ErrNotFound

	home, err := svc.Home(ctx, homePage)
	if err != nil {
		t.Fatalf("单个区块出错不应让整页失败：%v", err)
	}
	if len(home.Blocks) != 1 || home.Blocks[0].ID != 1 {
		t.Fatalf("只下发有内容的区块：%+v", home.Blocks)
	}

	// 没有区块时是空数组（不是 null）
	repo.blocks = nil
	if home, err = svc.Home(ctx, "channel:city"); err != nil || home.Blocks == nil || len(home.Blocks) != 0 {
		t.Fatalf("没有区块：%#v %v", home.Blocks, err)
	}
}

func TestBlockHomeTopicSource(t *testing.T) {
	svc, repo, _, picker, _ := newBlockSvc()
	ctx := context.Background()
	repo.blocks = []Block{
		{ID: 1, Page: homePage, Type: BlockHScroll, Status: MenuActive, ItemLimit: 4, SourceType: SourceTopic, Source: BlockSource{TopicID: 10}},
	}

	// 手选专题：用专题保存的顺序
	repo.topic = BlockTopicRef{SourceType: content.SourceManual}
	home, err := svc.Home(ctx, homePage)
	if err != nil || len(home.Blocks[0].Dramas) != 3 {
		t.Fatalf("手选专题的剧：%+v %v", home.Blocks, err)
	}

	// 自动专题：用专题的规则，但条数按区块的设置
	svc2, repo2, _, picker2, _ := newBlockSvc()
	repo2.blocks = repo.blocks
	repo2.topic = BlockTopicRef{SourceType: content.SourceAuto, Rule: content.TopicRule{TermCodes: []string{"city"}, Limit: 60, Sort: content.SortRecommend}}
	if _, err = svc2.Home(ctx, homePage); err != nil {
		t.Fatal(err)
	}
	if picker2.rule.Limit != 4 || picker2.rule.Sort != content.SortRecommend || !slices.Equal(picker2.rule.TermCodes, []string{"city"}) {
		t.Fatalf("自动专题的规则 + 区块的条数：%+v", picker2.rule)
	}
	_ = picker
}

func TestBlockReorderAndInvalidate(t *testing.T) {
	svc, repo, c, _, _ := newBlockSvc()
	ctx := context.Background()
	repo.blocks = []Block{
		{ID: 1, Page: homePage, Type: BlockTagCloud, Status: MenuActive, ItemLimit: 5},
		{ID: 2, Page: homePage, Type: BlockTagCloud, Status: MenuActive, ItemLimit: 5},
	}
	if err := svc.Reorder(ctx, homePage, []int64{2, 1}); err != nil {
		t.Fatal(err)
	}
	if repo.orderPage != homePage || !slices.Equal(repo.order, []int64{2, 1}) {
		t.Fatalf("按给定顺序重排：%v %v", repo.orderPage, repo.order)
	}
	if got := c.deletedKeys(); len(got) != 1 || got[0] != blockCacheKey(homePage) {
		t.Fatalf("重排后清该页面的缓存：%v", got)
	}
	for _, bad := range [][]int64{{1}, {1, 2, 3}, {}} {
		if err := svc.Reorder(ctx, homePage, bad); errs.From(err).Code != errs.ErrConflict.Code {
			t.Fatalf("%v 应 409：%v", bad, err)
		}
	}
	if err := svc.Reorder(ctx, "channel:gone", []int64{1}); errs.From(err).Code != errs.ErrNotFound.Code {
		t.Fatalf("未知页面应 404：%v", err)
	}

	// 换页面时清掉两个页面的缓存
	c.deleted = nil
	repo.blocks = []Block{{ID: 3, Page: homePage, Type: BlockTagCloud}}
	if _, _, err := svc.Update(ctx, 3, BlockInput{Page: "channel:city", Type: BlockTagCloud}, false); err != nil {
		t.Fatal(err)
	}
	if got := c.deletedKeys(); len(got) != 2 {
		t.Fatalf("换页面后两个页面的缓存都要清：%v", got)
	}
}

func ptrInt64(v int64) *int64 { return &v }
