package pg

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"xsite/service/internal/domain/ops"
	"xsite/service/internal/errs"
)

// 运营域（公告、广告、热搜词、榜单）的 PostgreSQL 集成测试。测试数据带唯一标记，结束时按标记清理。

func opsMark() string { return fmt.Sprintf("it%d", time.Now().UnixNano()) }

func TestOpsRepo_Announcements(t *testing.T) {
	pool := testPool(t)
	repo := NewOpsRepo(pool)
	ctx := context.Background()
	mark := opsMark()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM announcement WHERE content LIKE $1`, "%"+mark+"%")
	})

	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	end := start.Add(48 * time.Hour)
	live, err := repo.CreateAnnouncement(ctx, ops.AnnouncementInput{
		Kind: ops.AnnouncementPopup, Content: "弹窗" + mark, ClickURL: "/vip", Frequency: ops.FrequencyDaily,
		Pages: []string{"home", "play"}, Locales: []string{"en"}, Image: "/a.webp", StartAt: &start, EndAt: &end, Status: ops.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if live.ID == 0 || live.Kind != ops.AnnouncementPopup || !slices.Equal(live.Pages, []string{"home", "play"}) ||
		!slices.Equal(live.Locales, []string{"en"}) || !live.StartAt.Equal(start) || !live.EndAt.Equal(end) || live.CreatedAt.IsZero() {
		t.Fatalf("新建应原样保存：%+v", live)
	}
	topbar, err := repo.CreateAnnouncement(ctx, ops.AnnouncementInput{
		Kind: ops.AnnouncementTopbar, Content: "滚动" + mark, Frequency: ops.FrequencyAlways, Status: ops.StatusActive,
	})
	if err != nil || topbar.Pages == nil || len(topbar.Pages) != 0 || topbar.StartAt != nil || topbar.EndAt != nil {
		t.Fatalf("不选页面为空数组、不限时间为 nil：%+v %v", topbar, err)
	}
	ended := end.Add(-72 * time.Hour)
	expired, _ := repo.CreateAnnouncement(ctx, ops.AnnouncementInput{
		Kind: ops.AnnouncementTopbar, Content: "过期" + mark, Frequency: ops.FrequencyAlways, Status: ops.StatusActive, EndAt: &ended,
	})
	off, _ := repo.CreateAnnouncement(ctx, ops.AnnouncementInput{
		Kind: ops.AnnouncementTopbar, Content: "停用" + mark, Frequency: ops.FrequencyAlways, Status: ops.StatusInactive,
	})

	// 列表：按内容搜、按类型与状态筛选，新的在前
	list, total, err := repo.ListAnnouncements(ctx, ops.AnnouncementQuery{Pattern: "%" + mark + "%", Limit: 20})
	if err != nil || total != 4 || len(list) != 4 || list[0].ID != off.ID {
		t.Fatalf("按标记搜应有 4 条且新的在前：%d %+v %v", total, list, err)
	}
	if _, n, _ := repo.ListAnnouncements(ctx, ops.AnnouncementQuery{Pattern: "%" + mark + "%", Kind: ops.AnnouncementPopup, Limit: 20}); n != 1 {
		t.Fatalf("按类型筛选：%d", n)
	}
	if _, n, _ := repo.ListAnnouncements(ctx, ops.AnnouncementQuery{Pattern: "%" + mark + "%", Status: ops.StatusInactive, Limit: 20}); n != 1 {
		t.Fatalf("按状态筛选：%d", n)
	}
	if _, n, _ := repo.ListAnnouncements(ctx, ops.AnnouncementQuery{Pattern: "%/vip%", Limit: 20}); n < 1 {
		t.Fatalf("也按跳转网址搜：%d", n)
	}

	// 前台候选：启用、未结束的滚动条与弹窗（是否已开始由领域层判断）
	cand, err := repo.ListAnnouncementCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for _, a := range cand {
		ids = append(ids, a.ID)
	}
	if !slices.Contains(ids, live.ID) || !slices.Contains(ids, topbar.ID) || slices.Contains(ids, expired.ID) || slices.Contains(ids, off.ID) {
		t.Fatalf("候选只含启用且未结束的：%v（live %d topbar %d expired %d off %d）", ids, live.ID, topbar.ID, expired.ID, off.ID)
	}

	// 整体保存：未传的可选字段清空
	upd, err := repo.UpdateAnnouncement(ctx, live.ID, ops.AnnouncementInput{
		Kind: ops.AnnouncementTopbar, Content: "改为滚动" + mark, Frequency: ops.FrequencyOnce, Status: ops.StatusInactive,
	})
	if err != nil || upd.Kind != ops.AnnouncementTopbar || len(upd.Pages) != 0 || len(upd.Locales) != 0 || upd.StartAt != nil ||
		upd.Image != "" || !upd.UpdatedAt.After(upd.CreatedAt) {
		t.Fatalf("整体保存：%+v %v", upd, err)
	}
	if _, err := repo.UpdateAnnouncement(ctx, -1, ops.AnnouncementInput{Kind: ops.AnnouncementTopbar, Content: "x", Frequency: ops.FrequencyAlways, Status: ops.StatusActive}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("保存不存在的公告应 404，得到 %v", err)
	}
	if got, err := repo.DeleteAnnouncement(ctx, upd.ID); err != nil || got.ID != upd.ID {
		t.Fatalf("删除应返回删除前的记录：%+v %v", got, err)
	}
	if _, err := repo.GetAnnouncement(ctx, upd.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删除后应 404，得到 %v", err)
	}

	// 表约束：结束时间要晚于开始时间、类型与频率只能是枚举值
	for name, sql := range map[string]string{
		"结束早于开始": `INSERT INTO announcement (kind, content, start_at, end_at) VALUES ('topbar', 'x` + mark + `', now(), now() - interval '1 hour')`,
		"未知类型":   `INSERT INTO announcement (kind, content) VALUES ('banner', 'x` + mark + `')`,
		"未知频率":   `INSERT INTO announcement (kind, content, frequency) VALUES ('topbar', 'x` + mark + `', 'hourly')`,
		"内容为空":   `INSERT INTO announcement (kind, content) VALUES ('topbar', '')`,
	} {
		if _, err := pool.Exec(ctx, sql); pgCode(err) != "23514" {
			t.Errorf("%s应违反 CHECK 约束，得到 %v", name, err)
		}
	}
}

func TestOpsRepo_Ads(t *testing.T) {
	pool := testPool(t)
	repo := NewOpsRepo(pool)
	ctx := context.Background()
	mark := opsMark()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM ad WHERE name LIKE $1`, "%"+mark+"%")
	})

	end := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Microsecond)
	img, err := repo.CreateAd(ctx, ops.AdInput{
		Name: "顶部" + mark, Slot: ops.SlotHomeTop, CreativeType: ops.CreativeImage, Image: "/ads/top.webp", Alt: "会员",
		ClickURL: "/vip", Device: ops.AdDeviceMobile, Locales: []string{"en"}, Weight: 30, EndAt: &end, FreqCap: 3, Status: ops.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if img.ID == 0 || img.Slot != ops.SlotHomeTop || img.Device != ops.AdDeviceMobile || !slices.Equal(img.Locales, []string{"en"}) ||
		img.Weight != 30 || img.FreqCap != 3 || !img.EndAt.Equal(end) || img.ImpCount != 0 || img.CreatedAt.IsZero() {
		t.Fatalf("新建应原样保存：%+v", img)
	}
	html, err := repo.CreateAd(ctx, ops.AdInput{
		Name: "HTML" + mark, Slot: ops.SlotHomeMid, CreativeType: ops.CreativeHTML, HTML: "<p>x</p>", Device: ops.AdDeviceAll,
		Weight: 10, Status: ops.StatusActive,
	})
	if err != nil || html.Locales == nil || len(html.Locales) != 0 || html.EndAt != nil {
		t.Fatalf("不选语言为空数组、不限时间为 nil：%+v %v", html, err)
	}
	past := end.Add(-96 * time.Hour)
	ended, _ := repo.CreateAd(ctx, ops.AdInput{
		Name: "过期" + mark, Slot: ops.SlotHomeTop, CreativeType: ops.CreativeText, Text: "文字", Device: ops.AdDeviceAll,
		Weight: 10, EndAt: &past, Status: ops.StatusActive,
	})
	off, _ := repo.CreateAd(ctx, ops.AdInput{
		Name: "停用" + mark, Slot: ops.SlotFooterBanner, CreativeType: ops.CreativeText, Text: "文字", Device: ops.AdDeviceAll,
		Weight: 10, Status: ops.StatusInactive,
	})

	// 列表：按广告名搜、按位置与状态筛选，新的在前
	list, total, err := repo.ListAds(ctx, ops.AdQuery{Pattern: "%" + mark + "%", Limit: 20})
	if err != nil || total != 4 || len(list) != 4 || list[0].ID != off.ID {
		t.Fatalf("按标记搜应有 4 条且新的在前：%d %+v %v", total, list, err)
	}
	if _, n, _ := repo.ListAds(ctx, ops.AdQuery{Pattern: "%" + mark + "%", Slot: ops.SlotHomeTop, Limit: 20}); n != 2 {
		t.Fatalf("按位置筛选：%d", n)
	}
	if _, n, _ := repo.ListAds(ctx, ops.AdQuery{Pattern: "%" + mark + "%", Status: ops.StatusInactive, Limit: 20}); n != 1 {
		t.Fatalf("按状态筛选：%d", n)
	}
	if page, _, _ := repo.ListAds(ctx, ops.AdQuery{Pattern: "%" + mark + "%", Limit: 2, Offset: 2}); len(page) != 2 || page[0].ID != html.ID {
		t.Fatalf("第 2 页：%+v", page)
	}

	// 前台候选：投放中且未结束（端、语言、频控由领域层判断）
	cand, err := repo.ListAdCandidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for _, a := range cand {
		ids = append(ids, a.ID)
	}
	if !slices.Contains(ids, img.ID) || !slices.Contains(ids, html.ID) || slices.Contains(ids, ended.ID) || slices.Contains(ids, off.ID) {
		t.Fatalf("候选只含投放中且未结束的：%v（img %d html %d ended %d off %d）", ids, img.ID, html.ID, ended.ID, off.ID)
	}

	// 计数落库：累加；不存在的广告跳过；不改编辑时间
	if err := repo.AddAdCounts(ctx, map[int64]ops.AdCounts{img.ID: {Impressions: 5, Clicks: 2}, html.ID: {Impressions: 1}, -1: {Clicks: 9}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddAdCounts(ctx, map[int64]ops.AdCounts{img.ID: {Impressions: 3}}); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.GetAd(ctx, img.ID)
	if got.ImpCount != 8 || got.ClickCount != 2 || !got.UpdatedAt.Equal(img.UpdatedAt) {
		t.Fatalf("曝光 5+3、点击 2，编辑时间不变：%+v", got)
	}

	// 整体保存：计数保留，换类型后其他素材字段按输入清空
	upd, err := repo.UpdateAd(ctx, img.ID, ops.AdInput{
		Name: "改为文字" + mark, Slot: ops.SlotTextLink, CreativeType: ops.CreativeText, Text: "新剧上线", Device: ops.AdDeviceAll,
		Weight: 1, Status: ops.StatusInactive,
	})
	if err != nil || upd.CreativeType != ops.CreativeText || upd.Image != "" || len(upd.Locales) != 0 || upd.EndAt != nil ||
		upd.ImpCount != 8 || upd.ClickCount != 2 || !upd.UpdatedAt.After(upd.CreatedAt) {
		t.Fatalf("整体保存：%+v %v", upd, err)
	}
	if _, err := repo.UpdateAd(ctx, -1, ops.AdInput{Name: "x", Slot: ops.SlotHomeTop, CreativeType: ops.CreativeText, Text: "x", Device: ops.AdDeviceAll, Weight: 1, Status: ops.StatusActive}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("保存不存在的广告应 404，得到 %v", err)
	}
	if got, err := repo.DeleteAd(ctx, upd.ID); err != nil || got.ID != upd.ID {
		t.Fatalf("删除应返回删除前的记录：%+v %v", got, err)
	}
	if _, err := repo.GetAd(ctx, upd.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删除后应 404，得到 %v", err)
	}

	// 表约束：素材按类型必填、位置与端只能是枚举值、权重与频控有范围
	for name, sql := range map[string]string{
		"图片素材为空":  `INSERT INTO ad (name, slot, creative_type) VALUES ('x` + mark + `', 'home_top', 'image')`,
		"HTML 为空": `INSERT INTO ad (name, slot, creative_type, image) VALUES ('x` + mark + `', 'home_top', 'html', '/a.webp')`,
		"未知位置":    `INSERT INTO ad (name, slot, creative_type, text) VALUES ('x` + mark + `', 'sidebar', 'text', 't')`,
		"未知端":     `INSERT INTO ad (name, slot, creative_type, text, device) VALUES ('x` + mark + `', 'home_top', 'text', 't', 'tv')`,
		"权重为 0":   `INSERT INTO ad (name, slot, creative_type, text, weight) VALUES ('x` + mark + `', 'home_top', 'text', 't', 0)`,
		"频控为负":    `INSERT INTO ad (name, slot, creative_type, text, freq_cap) VALUES ('x` + mark + `', 'home_top', 'text', 't', -1)`,
		"广告名为空":   `INSERT INTO ad (name, slot, creative_type, text) VALUES ('', 'home_top', 'text', 't')`,
	} {
		if _, err := pool.Exec(ctx, sql); pgCode(err) != "23514" {
			t.Errorf("%s应违反 CHECK 约束，得到 %v", name, err)
		}
	}
}

func TestOpsRepo_HotWords(t *testing.T) {
	pool := testPool(t)
	repo := NewOpsRepo(pool)
	ctx := context.Background()
	mark := opsMark()
	w := func(s string) string { return mark + s }
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM hotsearch WHERE word LIKE $1`, mark+"%")
	})
	// 置顶上限按全表计算：共享库里可能已有别人的置顶词，测试里的上限在此基础上加
	var basePinned int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM hotsearch WHERE pinned`).Scan(&basePinned); err != nil {
		t.Fatal(err)
	}

	manual, err := repo.CreateHotWord(ctx, w("甜宠"))
	if err != nil || manual.Source != ops.HotManual || !manual.Visible || manual.Pinned || manual.SearchCount != 0 || manual.LastSearchedAt != nil {
		t.Fatalf("手动加词默认显示：%+v %v", manual, err)
	}
	if _, err := repo.CreateHotWord(ctx, w("甜宠")); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("重复的词 409：%v", err)
	}

	// 计数落库：新词自动收录（不显示），已有的词累加
	if err := repo.AddHotCounts(ctx, map[string]ops.HotCounts{w("甜宠"): {Searches: 3}, w("重生"): {Searches: 5, Misses: 5}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.AddHotCounts(ctx, map[string]ops.HotCounts{w("甜宠"): {Searches: 2, Misses: 1}, w("闪婚"): {Searches: 9}}); err != nil {
		t.Fatal(err)
	}
	list, total, err := repo.ListHotWords(ctx, ops.HotQuery{Pattern: mark + "%", Limit: 20})
	if err != nil || total != 3 || len(list) != 3 {
		t.Fatalf("应有 3 个词：%d %+v %v", total, list, err)
	}
	byWord := map[string]ops.HotWord{}
	for _, x := range list {
		byWord[x.Word] = x
	}
	if x := byWord[w("甜宠")]; x.SearchCount != 5 || x.MissCount != 1 || x.Source != ops.HotManual || !x.Visible || x.LastSearchedAt == nil || !x.UpdatedAt.Equal(manual.UpdatedAt) {
		t.Fatalf("已有的词累加，来源与显示不变，编辑时间不变：%+v", x)
	}
	auto := byWord[w("重生")]
	if auto.Source != ops.HotAuto || auto.Visible || auto.SearchCount != 5 || auto.MissCount != 5 {
		t.Fatalf("新词自动收录、默认不显示：%+v", auto)
	}
	if list[0].Word != w("闪婚") || list[1].Word != w("重生") {
		t.Fatalf("没有置顶时按搜索次数倒序：%v %v", list[0].Word, list[1].Word)
	}

	// 筛选：显示、置顶、片库没有
	if _, n, _ := repo.ListHotWords(ctx, ops.HotQuery{Pattern: mark + "%", Visible: ptrTo(false), Limit: 20}); n != 2 {
		t.Fatalf("不显示的有 2 个：%d", n)
	}
	if _, n, _ := repo.ListHotWords(ctx, ops.HotQuery{Pattern: mark + "%", Missing: true, Limit: 20}); n != 2 {
		t.Fatalf("片库没有的有 2 个：%d", n)
	}

	// 置顶：新置顶的排最后，取消置顶清掉顺序；上限按全表
	shan := byWord[w("闪婚")]
	p1, err := repo.SetHotWordFlags(ctx, auto.ID, true, true, basePinned+2)
	if err != nil || !p1.Pinned || !p1.Visible {
		t.Fatalf("置顶：%+v %v", p1, err)
	}
	p2, err := repo.SetHotWordFlags(ctx, shan.ID, true, true, basePinned+2)
	if err != nil || p2.PinSort <= p1.PinSort {
		t.Fatalf("新置顶的排到最后：%d → %d %v", p1.PinSort, p2.PinSort, err)
	}
	if again, err := repo.SetHotWordFlags(ctx, auto.ID, true, true, basePinned+2); err != nil || again.PinSort != p1.PinSort {
		t.Fatalf("已置顶的再保存不改顺序、不受上限影响：%+v %v", again, err)
	}
	if _, err := repo.SetHotWordFlags(ctx, manual.ID, true, true, basePinned+2); !errors.Is(err, ops.ErrTooManyPinned) {
		t.Fatalf("超过上限：%v", err)
	}
	list, _, _ = repo.ListHotWords(ctx, ops.HotQuery{Pattern: mark + "%", Limit: 20})
	if list[0].ID != auto.ID || list[1].ID != shan.ID || list[2].ID != manual.ID {
		t.Fatalf("置顶的按顺序在前：%v", []string{list[0].Word, list[1].Word, list[2].Word})
	}

	// 重排：必须恰好是全部置顶词
	pinnedIDs, err := repo.q.ListPinnedHotWordIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reordered := append([]int64{shan.ID}, slices.DeleteFunc(slices.Clone(pinnedIDs), func(id int64) bool { return id == shan.ID })...)
	if err := repo.ReorderPinned(ctx, reordered); err != nil {
		t.Fatal(err)
	}
	if ids, _ := repo.q.ListPinnedHotWordIDs(ctx); !slices.Equal(ids, reordered) {
		t.Fatalf("按给定顺序：%v，期望 %v", ids, reordered)
	}
	if err := repo.ReorderPinned(ctx, []int64{shan.ID, manual.ID}); !errors.Is(err, ops.ErrPinsChanged) {
		t.Fatalf("与当前置顶词不一致时 409：%v", err)
	}

	// 热搜榜：只含显示中的，置顶在前
	board, err := repo.ListHotBoard(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[int64]int{}
	for i, x := range board {
		pos[x.ID] = i
		if !x.Visible {
			t.Fatalf("热搜榜只含显示中的词：%+v", x)
		}
	}
	if pos[shan.ID] > pos[auto.ID] || pos[auto.ID] > pos[manual.ID] {
		t.Fatalf("热搜榜顺序：%v", pos)
	}

	// 取消置顶、删除
	if un, err := repo.SetHotWordFlags(ctx, shan.ID, false, false, basePinned+2); err != nil || un.Pinned || un.Visible || un.PinSort != 0 {
		t.Fatalf("取消置顶并隐藏：%+v %v", un, err)
	}
	if got, err := repo.DeleteHotWord(ctx, manual.ID); err != nil || got.ID != manual.ID {
		t.Fatalf("删除：%+v %v", got, err)
	}
	if _, err := repo.GetHotWord(ctx, manual.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删除后 404：%v", err)
	}
	if _, err := repo.SetHotWordFlags(ctx, -1, true, false, 10); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不存在的词 404：%v", err)
	}

	// 表约束：置顶一定显示、词不能为空、来源只能是枚举值
	for name, sql := range map[string]string{
		"置顶但不显示": `INSERT INTO hotsearch (word, source, pinned, visible) VALUES ('` + w("x") + `', 'manual', true, false)`,
		"空词":     `INSERT INTO hotsearch (word, source) VALUES ('', 'manual')`,
		"未知来源":   `INSERT INTO hotsearch (word, source) VALUES ('` + w("y") + `', 'import')`,
	} {
		if _, err := pool.Exec(ctx, sql); pgCode(err) != "23514" {
			t.Errorf("%s应违反 CHECK 约束，得到 %v", name, err)
		}
	}
}

func ptrTo[T any](v T) *T { return &v }

func TestOpsRepo_Ranking(t *testing.T) {
	pool := testPool(t)
	repo := NewOpsRepo(pool)
	ctx := context.Background()
	mark := opsMark()
	var origWeights []byte
	if err := pool.QueryRow(ctx, `SELECT ranking FROM site_config WHERE id = 1`).Scan(&origWeights); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM drama WHERE title LIKE $1`, mark+"%") // 调整随剧级联删除
		_, _ = pool.Exec(bg, `UPDATE site_config SET ranking = $1 WHERE id = 1`, origWeights)
	})
	// 整体替换会动到整张榜：先记下共享开发库里已有的调整，结束时原样还原（先于上面删测试剧执行）
	saved := map[ops.RankBoard][]ops.RankOverride{}
	for _, b := range ops.RankBoards {
		rules, err := repo.RankOverrideRules(ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		saved[b] = rules
	}
	t.Cleanup(func() {
		for b, rules := range saved {
			if err := repo.ReplaceRankOverrides(context.Background(), b, rules); err != nil {
				t.Errorf("还原%s的人工调整：%v", b, err)
			}
		}
	})
	drama := func(name, status string, viewBase, viewCount int64, publishAt string, trash bool) int64 {
		t.Helper()
		var id int64
		err := pool.QueryRow(ctx, `INSERT INTO drama (title, status, view_base, view_count, publish_at, deleted_at)
			VALUES ($1, $2, $3, $4, $5::timestamptz, CASE WHEN $6 THEN now() END) RETURNING id`,
			mark+name, status, viewBase, viewCount, nullText(publishAt), trash).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	hot := drama("热", "published", 100, 23, "", false)
	cold := drama("冷", "published", 0, 5, "2020-01-01T00:00:00Z", false)
	draft := drama("草稿", "draft", 900, 0, "", false)
	future := drama("定时", "published", 900, 0, "2099-01-01T00:00:00Z", false)
	trashed := drama("回收站", "published", 900, 0, "", true)

	// 计分数据：只含前台可见的剧，播放 = 初始播放量 + 真实播放量
	signals, err := repo.RankSignals(ctx, ops.BoardDramaWeek)
	if err != nil {
		t.Fatal(err)
	}
	plays := map[int64]int64{}
	for _, s := range signals {
		plays[s.DramaID] = s.Plays
		if s.Follows != 0 || s.Likes != 0 || s.Finishes != 0 {
			t.Fatalf("追剧、点赞、看完的数据就绪前为 0：%+v", s)
		}
	}
	if plays[hot] != 123 || plays[cold] != 5 {
		t.Fatalf("播放 = view_base + view_count：%v", plays)
	}
	for _, id := range []int64{draft, future, trashed} {
		if _, ok := plays[id]; ok {
			t.Fatalf("草稿、定时未到、回收站的剧不计分：%d", id)
		}
	}

	// 人工调整：整体替换、只影响该榜、带剧名
	if err := repo.ReplaceRankOverrides(ctx, ops.BoardDramaWeek, []ops.RankOverride{
		{DramaID: hot, Pinned: true}, {DramaID: draft, Boost: -50}, {DramaID: cold, Blocked: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceRankOverrides(ctx, ops.BoardDramaMonth, []ops.RankOverride{{DramaID: cold, Boost: 7}}); err != nil {
		t.Fatal(err)
	}
	views, err := repo.ListRankOverrides(ctx, ops.BoardDramaWeek)
	if err != nil || len(views) != 3 || views[0].DramaID != hot || !views[0].Pinned || views[0].Title != mark+"热" ||
		views[1].DramaID != cold || !views[1].Blocked || views[2].Boost != -50 || views[2].DramaStatus != "draft" || views[0].UpdatedAt.IsZero() {
		t.Fatalf("置顶的在前、其次不让上榜，再按加分：%+v %v", views, err)
	}
	if err := repo.ReplaceRankOverrides(ctx, ops.BoardDramaWeek, []ops.RankOverride{{DramaID: cold, Boost: 3}}); err != nil {
		t.Fatal(err)
	}
	rules, _ := repo.RankOverrideRules(ctx, ops.BoardDramaWeek)
	month, _ := repo.RankOverrideRules(ctx, ops.BoardDramaMonth)
	if len(rules) != 1 || rules[0] != (ops.RankOverride{DramaID: cold, Boost: 3}) || len(month) != 1 || month[0].Boost != 7 {
		t.Fatalf("整体替换只影响该榜：%+v %+v", rules, month)
	}

	// 目标校验、剧名、前台卡片（按给定顺序，只含可见的）
	missing, err := repo.MissingDramas(ctx, []int64{hot, trashed, -1})
	if err != nil || !slices.Equal(missing, []int64{trashed, -1}) {
		t.Fatalf("不存在或在回收站的剧：%v %v", missing, err)
	}
	titles, _ := repo.DramaTitles(ctx, []int64{hot, draft, -1})
	if titles[hot] != mark+"热" || titles[draft] != mark+"草稿" || len(titles) != 2 {
		t.Fatalf("剧名：%v", titles)
	}
	cards, err := repo.RankCards(ctx, []int64{cold, draft, hot, future})
	if err != nil || len(cards) != 2 || cards[0].ID != cold || cards[1].ID != hot || cards[1].PlayCount != 123 || cards[1].Title != mark+"热" {
		t.Fatalf("卡片按给定顺序、不可见的跳过：%+v %v", cards, err)
	}

	// 计分系数：读写 site_config.ranking，不改前台配置的版本
	var version int64
	_ = pool.QueryRow(ctx, `SELECT version FROM site_config WHERE id = 1`).Scan(&version)
	w := ops.RankWeights{Play: 1.5, Follow: 0, Like: 2, Finish: 0.25}
	if got, err := repo.SetRankingWeights(ctx, w); err != nil || got != w {
		t.Fatalf("保存系数：%+v %v", got, err)
	}
	if got, err := repo.RankingWeights(ctx); err != nil || got != w {
		t.Fatalf("读回系数：%+v %v", got, err)
	}
	var after int64
	_ = pool.QueryRow(ctx, `SELECT version FROM site_config WHERE id = 1`).Scan(&after)
	if after != version {
		t.Fatalf("系数只给后台读，不应改前台配置版本：%d → %d", version, after)
	}

	// 表约束：置顶与不让上榜互斥、空调整不保存、未知榜单；删剧级联删除调整
	for name, sql := range map[string]string{
		"置顶又不让上榜": `INSERT INTO rank_override (board, drama_id, pinned, blocked) VALUES ('drama_week', ` + itoaTest(hot) + `, true, true)`,
		"没有调整":    `INSERT INTO rank_override (board, drama_id) VALUES ('drama_week', ` + itoaTest(hot) + `)`,
		"未知榜单":    `INSERT INTO rank_override (board, drama_id, pinned) VALUES ('tag_week', ` + itoaTest(hot) + `, true)`,
	} {
		if _, err := pool.Exec(ctx, sql); pgCode(err) != "23514" {
			t.Errorf("%s应违反 CHECK 约束，得到 %v", name, err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM drama WHERE id = $1`, cold); err != nil {
		t.Fatal(err)
	}
	if rules, _ := repo.RankOverrideRules(ctx, ops.BoardDramaMonth); len(rules) != 0 {
		t.Fatalf("删剧时调整一起删除：%+v", rules)
	}
}

func itoaTest(v int64) string { return fmt.Sprint(v) }
