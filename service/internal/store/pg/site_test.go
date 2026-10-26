package pg

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/domain/site"
	"xsite/service/internal/errs"
)

// 站点编排域（静态页、前台菜单、页面区块）的 PostgreSQL 集成测试。测试数据带唯一标记，结束时按标记清理。

func siteMark() string { return fmt.Sprintf("it%d", time.Now().UnixNano()) }

func TestSiteRepo_Pages(t *testing.T) {
	pool := testPool(t)
	repo := NewSiteRepo(pool)
	ctx := context.Background()
	mark := siteMark()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM page WHERE slug LIKE $1`, mark+"%")
	})
	slug := func(s string) string { return mark + "-" + s }

	// 新建：正文与 SEO 原样入库，默认草稿不在前台
	draft, err := repo.CreatePage(ctx, site.PageInput{
		Title: "用户协议", Slug: slug("terms"), Body: "<p>协议正文</p>", BodyFormat: site.BodyRich, Status: site.PageDraft,
		SEOTitle: "协议", SEODesc: "简介", IsFooter: true,
	})
	if err != nil || draft.ID == 0 || draft.Status != site.PageDraft || !draft.IsFooter || draft.CreatedAt.IsZero() {
		t.Fatalf("新建静态页：%+v %v", draft, err)
	}
	if _, err := repo.PublicPage(ctx, draft.Slug); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("草稿不对前台下发，应 404：%v", err)
	}

	// slug 全站唯一：重复时 409
	_, err = repo.CreatePage(ctx, site.PageInput{Title: "重复", Slug: slug("terms"), Body: "<p>x</p>", BodyFormat: site.BodyRich, Status: site.PageDraft})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("slug 重复应 409：%v", err)
	}

	// 发布后前台可读；updated_at 变化
	time.Sleep(2 * time.Millisecond)
	published, err := repo.UpdatePage(ctx, draft.ID, site.PageInput{
		Title: "用户协议 v2", Slug: slug("terms"), Body: "<p>新正文</p>", BodyFormat: site.BodyHTML, Status: site.PagePublished,
		SEOTitle: "协议 v2", SEODesc: "简介 v2", IsFooter: true,
	})
	if err != nil || published.Title != "用户协议 v2" || published.BodyFormat != site.BodyHTML || !published.UpdatedAt.After(draft.UpdatedAt) {
		t.Fatalf("保存静态页：%+v %v", published, err)
	}
	pub, err := repo.PublicPage(ctx, published.Slug)
	if err != nil || pub.Body != "<p>新正文</p>" || pub.SEOTitle != "协议 v2" {
		t.Fatalf("前台读已发布的页：%+v %v", pub, err)
	}

	// 页脚页：只含已发布且标记为页脚的
	other, err := repo.CreatePage(ctx, site.PageInput{
		Title: "帮助", Slug: slug("help"), Body: "<p>帮助</p>", BodyFormat: site.BodyRich, Status: site.PagePublished, IsFooter: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	footers, err := repo.FooterPages(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sawPublished, sawOther bool
	for _, f := range footers {
		switch f.Slug {
		case published.Slug:
			sawPublished = true
		case other.Slug:
			sawOther = true
		}
	}
	if !sawPublished || sawOther {
		t.Fatalf("页脚页只含已发布且标记为页脚的：%+v", footers)
	}

	// 列表：搜标题或 slug，按状态筛选
	list, total, err := repo.ListPages(ctx, site.PageQuery{Pattern: "%" + mark + "%", Limit: 10})
	if err != nil || total != 2 || len(list) != 2 || list[0].ID < list[1].ID {
		t.Fatalf("列表按编号倒序：%d %+v %v", total, list, err)
	}
	list, total, err = repo.ListPages(ctx, site.PageQuery{Pattern: "%" + mark + "%", Status: site.PageDraft, Limit: 10})
	if err != nil || total != 0 || len(list) != 0 {
		t.Fatalf("按状态筛选：%d %+v %v", total, list, err)
	}
	if list, _, err = repo.ListPages(ctx, site.PageQuery{Pattern: "%" + mark + "-help%", Limit: 10}); err != nil || len(list) != 1 {
		t.Fatalf("按 slug 搜：%+v %v", list, err)
	}

	// 读取与删除
	if got, err := repo.GetPage(ctx, published.ID); err != nil || got.Slug != published.Slug {
		t.Fatalf("按编号读取：%+v %v", got, err)
	}
	if _, err := repo.GetPage(ctx, -1); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不存在的静态页应 404：%v", err)
	}
	if gone, err := repo.DeletePage(ctx, other.ID); err != nil || gone.Slug != other.Slug {
		t.Fatalf("删除返回删除前的记录：%+v %v", gone, err)
	}
	if _, err := repo.DeletePage(ctx, other.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("重复删除应 404：%v", err)
	}

	// 表约束：标题非空且不超长、未知格式与状态
	for name, sql := range map[string]string{
		"标题为空": `INSERT INTO page (title, slug, body) VALUES ('', '` + slug("c1") + `', 'x')`,
		"标题超长": `INSERT INTO page (title, slug, body) VALUES ('` + strings.Repeat("标", 101) + `', '` + slug("c2") + `', 'x')`,
		"未知格式": `INSERT INTO page (title, slug, body, body_format) VALUES ('t', '` + slug("c3") + `', 'x', 'markdown')`,
		"未知状态": `INSERT INTO page (title, slug, body, status) VALUES ('t', '` + slug("c4") + `', 'x', 'archived')`,
	} {
		if _, err := pool.Exec(ctx, sql); pgCode(err) != "23514" {
			t.Errorf("%s应违反 CHECK 约束，得到 %v", name, err)
		}
	}
}

func TestSiteRepo_Menus(t *testing.T) {
	pool := testPool(t)
	repo := NewSiteRepo(pool)
	ctx := context.Background()
	mark := siteMark()
	// 菜单名最长 20 个字，标记用短的（末 5 位）；slug 用完整标记
	short := "m" + mark[len(mark)-5:]
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM menu WHERE label LIKE $1`, short+"%")
		_, _ = pool.Exec(bg, `DELETE FROM page WHERE slug LIKE $1`, mark+"%")
		_, _ = pool.Exec(bg, `DELETE FROM topic WHERE slug LIKE $1`, mark+"%")
		_, _ = pool.Exec(bg, `DELETE FROM tag WHERE slug LIKE $1`, mark+"%")
		_, _ = pool.Exec(bg, `DELETE FROM taxonomy WHERE slug LIKE $1`, mark+"%") // 取值随维度级联删除
	})
	label := func(s string) string { return short + s }

	in := func(pos site.MenuPosition, name string, kind site.LinkType, value string) site.MenuInput {
		return site.MenuInput{
			Position: pos, Label: label(name), LinkType: kind, LinkValue: value,
			VisibleTo: site.VisibleAll, Device: site.DeviceAll, Status: site.MenuActive,
		}
	}

	// 共享开发库里这些位置可能已有菜单：断言都按「相对自己创建之前」来做
	baseTop, err := repo.CountActiveMenusAt(ctx, site.PositionTop, 0)
	if err != nil {
		t.Fatal(err)
	}

	// 新建：同一位置按新建顺序排在末尾
	first, err := repo.CreateMenu(ctx, in(site.PositionTop, "首页", site.LinkInternal, "home"))
	if err != nil || first.ID == 0 {
		t.Fatalf("新建第一条：%+v %v", first, err)
	}
	second, err := repo.CreateMenu(ctx, in(site.PositionTop, "探索", site.LinkInternal, "explore"))
	if err != nil || second.Sort != first.Sort+1 {
		t.Fatalf("新建排在末尾：%+v %v", second, err)
	}
	footer, err := repo.CreateMenu(ctx, func() site.MenuInput {
		m := in(site.PositionFooter, "条款", site.LinkPage, mark+"-terms")
		m.GroupTitle = "帮助与条款"
		return m
	}())
	if err != nil || footer.GroupTitle != "帮助与条款" {
		t.Fatalf("页脚分组标题：%+v %v", footer, err)
	}

	// 启用计数（底栏上限用）：exceptID 排除自己
	if n, err := repo.CountActiveMenusAt(ctx, site.PositionTop, 0); err != nil || n != baseTop+2 {
		t.Fatalf("顶部启用数应比创建前多 2：%d（创建前 %d）%v", n, baseTop, err)
	}
	if n, err := repo.CountActiveMenusAt(ctx, site.PositionTop, first.ID); err != nil || n != baseTop+1 {
		t.Fatalf("排除自己后少 1 条：%d（创建前 %d）%v", n, baseTop, err)
	}

	// 改位置：放到新位置的末尾
	moved, err := repo.UpdateMenu(ctx, second.ID, in(site.PositionFooter, "探索", site.LinkInternal, "explore"))
	if err != nil || moved.Position != site.PositionFooter || moved.Sort != footer.Sort+1 {
		t.Fatalf("改位置后排在新位置末尾：%+v %v", moved, err)
	}
	// 位置不变时顺序不动
	again, err := repo.UpdateMenu(ctx, first.ID, in(site.PositionTop, "首页 v2", site.LinkInternal, "home"))
	if err != nil || again.Sort != first.Sort || again.Label != label("首页 v2") {
		t.Fatalf("位置不变时顺序不动：%+v %v", again, err)
	}

	// 重排：只影响该位置。共享库里页脚可能已有菜单，所以只看自己这两条的相对顺序
	ids, err := repo.MenuIDsAt(ctx, site.PositionFooter)
	if err != nil || len(ids) < 2 || ids[len(ids)-2] != footer.ID || ids[len(ids)-1] != moved.ID {
		t.Fatalf("自己的两条在页脚末尾，条款在前：%v %v", ids, err)
	}
	// 整个位置重排：把自己的两条换个顺序，其余保持原位
	reordered := append([]int64(nil), ids[:len(ids)-2]...)
	reordered = append(reordered, moved.ID, footer.ID)
	if err := repo.SetMenuOrder(ctx, site.PositionFooter, reordered); err != nil {
		t.Fatal(err)
	}
	if ids, err = repo.MenuIDsAt(ctx, site.PositionFooter); err != nil || ids[len(ids)-2] != moved.ID || ids[len(ids)-1] != footer.ID {
		t.Fatalf("重排后顺序：%v %v", ids, err)
	}

	// 全部列表按位置、顺序排
	all, err := repo.ListMenus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mine []site.Menu
	for _, m := range all {
		if strings.HasPrefix(m.Label, short) {
			mine = append(mine, m)
		}
	}
	if len(mine) != 3 || mine[0].Position != site.PositionFooter || mine[2].Position != site.PositionTop {
		t.Fatalf("按位置排（footer 在 top 前）：%+v", mine)
	}

	// 链接目标：造一个静态页、频道取值、专题、标签
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO page (title, slug, body, status) VALUES ($1, $2, '<p>x</p>', 'published')`, label("条款"), mark+"-terms")
	mustExec(`INSERT INTO page (title, slug, body, status) VALUES ($1, $2, '<p>x</p>', 'draft')`, label("草稿"), mark+"-draft")
	var taxID int64
	if err := pool.QueryRow(ctx, `INSERT INTO taxonomy (name, slug) VALUES ($1, $2) RETURNING id`, label("地区"), mark+"-region").Scan(&taxID); err != nil {
		t.Fatal(err)
	}
	mustExec(`INSERT INTO taxonomy_term (taxonomy_id, name, slug, is_channel, status) VALUES ($1, $2, $3, true, 'active')`, taxID, label("都市"), mark+"-city")
	mustExec(`INSERT INTO taxonomy_term (taxonomy_id, name, slug, is_channel, status) VALUES ($1, $2, $3, true, 'inactive')`, taxID, label("停用"), mark+"-off")
	mustExec(`INSERT INTO taxonomy_term (taxonomy_id, name, slug, is_channel) VALUES ($1, $2, $3, false)`, taxID, label("非频道"), mark+"-plain")
	mustExec(`INSERT INTO topic (title, slug, status) VALUES ($1, $2, 'published')`, label("专题"), mark+"-hot")
	mustExec(`INSERT INTO tag (name, slug, status) VALUES ($1, $2, 'active')`, label("甜宠"), mark+"-sweet")

	targets := []site.MenuTarget{
		{Kind: site.LinkPage, Slug: mark + "-terms"},
		{Kind: site.LinkPage, Slug: mark + "-draft"},
		{Kind: site.LinkPage, Slug: mark + "-gone"},
		{Kind: site.LinkChannel, Slug: mark + "-city"},
		{Kind: site.LinkChannel, Slug: mark + "-off"},
		{Kind: site.LinkChannel, Slug: mark + "-plain"},
		{Kind: site.LinkTopic, Slug: mark + "-hot"},
		{Kind: site.LinkTag, Slug: mark + "-sweet"},
	}
	state, err := repo.MenuTargets(ctx, targets)
	if err != nil {
		t.Fatal(err)
	}
	want := map[site.MenuTarget]bool{
		{Kind: site.LinkPage, Slug: mark + "-terms"}:   true,
		{Kind: site.LinkPage, Slug: mark + "-draft"}:   false, // 存在但是草稿
		{Kind: site.LinkChannel, Slug: mark + "-city"}: true,
		{Kind: site.LinkChannel, Slug: mark + "-off"}:  false, // 频道取值停用
		{Kind: site.LinkTopic, Slug: mark + "-hot"}:    true,
		{Kind: site.LinkTag, Slug: mark + "-sweet"}:    true,
	}
	for k, v := range want {
		got, ok := state[k]
		if !ok || got != v {
			t.Errorf("目标 %+v 期望 visible=%v，得到 %v（存在 %v）", k, v, got, ok)
		}
	}
	for _, missing := range []site.MenuTarget{
		{Kind: site.LinkPage, Slug: mark + "-gone"},
		{Kind: site.LinkChannel, Slug: mark + "-plain"}, // 不是频道，按不存在处理
	} {
		if _, ok := state[missing]; ok {
			t.Errorf("目标 %+v 应按不存在处理", missing)
		}
	}

	// 删除
	if gone, err := repo.DeleteMenu(ctx, first.ID); err != nil || gone.ID != first.ID {
		t.Fatalf("删除返回删除前的记录：%+v %v", gone, err)
	}
	if _, err := repo.GetMenu(ctx, first.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删除后 404：%v", err)
	}

	// 表约束
	for name, sql := range map[string]string{
		"未知位置":   `INSERT INTO menu (position, label, link_type, link_value) VALUES ('sidebar', '` + label("c") + `', 'internal', 'home')`,
		"未知链接类型": `INSERT INTO menu (position, label, link_type, link_value) VALUES ('top', '` + label("c") + `', 'rss', 'home')`,
		"名称为空":   `INSERT INTO menu (position, label, link_type, link_value) VALUES ('top', '', 'internal', 'home')`,
		"链接为空":   `INSERT INTO menu (position, label, link_type, link_value) VALUES ('top', '` + label("c") + `', 'internal', '')`,
		"未知设备":   `INSERT INTO menu (position, label, link_type, link_value, device) VALUES ('top', '` + label("c") + `', 'internal', 'home', 'tv')`,
	} {
		if _, err := pool.Exec(ctx, sql); pgCode(err) != "23514" {
			t.Errorf("%s应违反 CHECK 约束，得到 %v", name, err)
		}
	}
}

func TestSiteRepo_Addresses(t *testing.T) {
	pool := testPool(t)
	repo := NewSiteRepo(pool)
	ctx := context.Background()
	var orig []byte
	if err := pool.QueryRow(ctx, `SELECT addresses FROM site_config WHERE id = 1`).Scan(&orig); err != nil {
		t.Fatal(err)
	}
	// 共享开发库：结束时把这一段原样还原
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `UPDATE site_config SET addresses = $1 WHERE id = 1`, orig)
	})

	var version int64
	if err := pool.QueryRow(ctx, `SELECT version FROM site_config WHERE id = 1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	in := site.Addresses{
		Notice: "请收藏永久地址", LatestURL: "https://new.example.com", PermanentURL: "https://perm.example.com",
		BackupURLs: []string{"https://b1.example.com"}, PublishPage: "https://pub.example.com",
		SupportURL: "https://t.me/support", Telegram: "https://t.me/channel",
		Socials: []site.Social{{Name: "微博", URL: "https://weibo.com/x"}},
	}
	saved, err := repo.SetSiteAddresses(ctx, in)
	if err != nil || saved.Notice != in.Notice || len(saved.BackupURLs) != 1 || saved.Socials[0].Name != "微博" {
		t.Fatalf("保存防丢地址：%+v %v", saved, err)
	}
	got, err := repo.SiteAddresses(ctx)
	if err != nil || got.LatestURL != in.LatestURL || got.Telegram != in.Telegram {
		t.Fatalf("读回防丢地址：%+v %v", got, err)
	}
	// 仓储只写这一段，版本号由领域层经 SiteConfigService.BumpVersion 处理
	var after int64
	if err := pool.QueryRow(ctx, `SELECT version FROM site_config WHERE id = 1`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != version {
		t.Fatalf("仓储不应改版本号：%d → %d", version, after)
	}

	// 空对象读出来是零值（库里初始就是 {}）
	if _, err := pool.Exec(ctx, `UPDATE site_config SET addresses = '{}' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if got, err = repo.SiteAddresses(ctx); err != nil || got.LatestURL != "" || got.Socials != nil {
		t.Fatalf("空段读出零值：%+v %v", got, err)
	}
}
func TestSiteRepo_Blocks(t *testing.T) {
	pool := testPool(t)
	repo := NewSiteRepo(pool)
	ctx := context.Background()
	mark := siteMark()
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM block WHERE page LIKE $1 OR title LIKE $2`, "channel:"+mark+"%", mark+"%")
		_, _ = pool.Exec(bg, `DELETE FROM drama WHERE title LIKE $1`, mark+"%")
		_, _ = pool.Exec(bg, `DELETE FROM topic WHERE slug LIKE $1`, mark+"%") // topic_drama 级联删除
		_, _ = pool.Exec(bg, `DELETE FROM tag WHERE slug LIKE $1`, mark+"%")
		_, _ = pool.Exec(bg, `DELETE FROM taxonomy WHERE slug LIKE $1`, mark+"%") // 取值随维度级联删除
	})

	homePg, chanPg := "home", "channel:"+mark
	in := func(page string, t site.BlockType) site.BlockInput {
		return site.BlockInput{
			Page: page, Type: t, Title: mark + "-" + string(t), ItemLimit: 24, Status: site.MenuActive,
			ColsDesktop: 6, Device: site.DeviceAll, SourceType: site.SourceAuto,
		}
	}

	// 共享开发库里首页可能已有区块：断言都相对「自己创建之前」
	baseHome, err := repo.BlockIDsAt(ctx, homePg)
	if err != nil {
		t.Fatal(err)
	}

	// 新建：排在该页面末尾，来源参数原样入库
	first, err := repo.CreateBlock(ctx, func() site.BlockInput {
		b := in(homePg, site.BlockCarousel)
		b.SourceType, b.Source = site.SourceManual, site.BlockSource{DramaIDs: []int64{3, 1}}
		b.Subtitle, b.MoreLink = "副标题", "/explore"
		return b
	}())
	if err != nil || first.ID == 0 || first.ItemLimit != 24 || first.CreatedAt.IsZero() {
		t.Fatalf("新建区块：%+v %v", first, err)
	}
	if first.SourceType != site.SourceManual || !slices.Equal(first.Source.DramaIDs, []int64{3, 1}) || first.MoreLink != "/explore" {
		t.Fatalf("来源参数原样入库：%+v", first)
	}
	second, err := repo.CreateBlock(ctx, in(homePg, site.BlockTagCloud))
	if err != nil || second.Sort != first.Sort+1 {
		t.Fatalf("新建排在末尾：%+v %v", second, err)
	}
	chanBlock, err := repo.CreateBlock(ctx, func() site.BlockInput {
		b := in(chanPg, site.BlockRank)
		b.Source = site.BlockSource{RankBoard: "drama_week"}
		return b
	}())
	if err != nil || chanBlock.Page != chanPg || chanBlock.Source.RankBoard != "drama_week" {
		t.Fatalf("频道页区块：%+v %v", chanBlock, err)
	}

	// 读取：按页面分组，按 sort 排
	list, err := repo.ListBlocks(ctx, homePg)
	if err != nil || len(list) != len(baseHome)+2 || list[len(list)-2].ID != first.ID || list[len(list)-1].ID != second.ID {
		t.Fatalf("首页区块按顺序（自己两条在末尾）：%d 条 %v", len(list), err)
	}
	if got, err := repo.GetBlock(ctx, first.ID); err != nil || got.Title != first.Title {
		t.Fatalf("按编号读取：%+v %v", got, err)
	}
	if _, err := repo.GetBlock(ctx, -1); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不存在的区块应 404：%v", err)
	}

	// 整体保存：页面不变时顺序不动
	time.Sleep(2 * time.Millisecond)
	kept, err := repo.UpdateBlock(ctx, first.ID, func() site.BlockInput {
		b := in(homePg, site.BlockPosterGrid)
		b.ItemLimit, b.ColsDesktop, b.Device, b.Status = 12, 4, site.DeviceMobile, site.MenuInactive
		b.Source = site.BlockSource{TermCodes: []string{"a", "b"}, Sort: "hot", Window: "7d"}
		return b
	}())
	if err != nil || kept.Sort != first.Sort || kept.Type != site.BlockPosterGrid || !kept.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("页面不变时顺序不动：%+v %v", kept, err)
	}
	if kept.ItemLimit != 12 || kept.ColsDesktop != 4 || kept.Device != site.DeviceMobile || kept.Status != site.MenuInactive {
		t.Fatalf("保存显示参数：%+v", kept)
	}
	if !slices.Equal(kept.Source.TermCodes, []string{"a", "b"}) || kept.Source.Window != "7d" || len(kept.Source.DramaIDs) != 0 {
		t.Fatalf("来源整体替换：%+v", kept.Source)
	}
	// 换页面：放到新页面末尾
	moved, err := repo.UpdateBlock(ctx, second.ID, in(chanPg, site.BlockTagCloud))
	if err != nil || moved.Page != chanPg || moved.Sort != chanBlock.Sort+1 {
		t.Fatalf("换页面后排在新页面末尾：%+v %v", moved, err)
	}

	// 重排：只影响该页面
	ids, err := repo.BlockIDsAt(ctx, chanPg)
	if err != nil || !slices.Equal(ids, []int64{chanBlock.ID, moved.ID}) {
		t.Fatalf("频道页顺序：%v %v", ids, err)
	}
	if err := repo.SetBlockOrder(ctx, chanPg, []int64{moved.ID, chanBlock.ID}); err != nil {
		t.Fatal(err)
	}
	if ids, err = repo.BlockIDsAt(ctx, chanPg); err != nil || !slices.Equal(ids, []int64{moved.ID, chanBlock.ID}) {
		t.Fatalf("重排后顺序：%v %v", ids, err)
	}
	// 页面对不上的编号不动（防止跨页面改顺序）
	if err := repo.SetBlockOrder(ctx, homePg, []int64{moved.ID}); err != nil {
		t.Fatal(err)
	}
	if ids, err = repo.BlockIDsAt(ctx, chanPg); err != nil || ids[0] != moved.ID {
		t.Fatalf("跨页面重排应无效：%v %v", ids, err)
	}

	// 删除
	if gone, err := repo.DeleteBlock(ctx, chanBlock.ID); err != nil || gone.ID != chanBlock.ID {
		t.Fatalf("删除返回删除前的记录：%+v %v", gone, err)
	}
	if _, err := repo.DeleteBlock(ctx, chanBlock.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("重复删除应 404：%v", err)
	}

	// ---------- 可编排页面：首页固定在前，其后是启用的频道 ----------
	taxID := insertID(t, pool, `INSERT INTO taxonomy (name, slug) VALUES ($1, $2) RETURNING id`, mark+"-类型", mark+"-genre")
	insertID(t, pool, `INSERT INTO taxonomy_term (taxonomy_id, name, slug, code, is_channel, status, sort)
		VALUES ($1, $2, $3, $4, true, 'active', 900) RETURNING id`, taxID, mark+"-都市", mark+"-city", mark+"-city")
	insertID(t, pool, `INSERT INTO taxonomy_term (taxonomy_id, name, slug, code, is_channel, status)
		VALUES ($1, $2, $3, $4, true, 'inactive') RETURNING id`, taxID, mark+"-停用", mark+"-off", mark+"-off")
	pages, err := repo.BlockPages(ctx)
	if err != nil || len(pages) < 2 || pages[0].Value != "home" || pages[0].Label != "首页" {
		t.Fatalf("首页固定在第一个：%+v %v", pages, err)
	}
	var sawCity, sawOff bool
	for _, p := range pages[1:] {
		switch p.Value {
		case "channel:" + mark + "-city":
			sawCity = p.Label == mark+"-都市"
		case "channel:" + mark + "-off":
			sawOff = true
		}
	}
	if !sawCity || sawOff {
		t.Fatalf("只含启用的频道页：%+v", pages)
	}

	// ---------- 区块取数 ----------
	pubA := insertID(t, pool, `INSERT INTO drama (title, slug, status, publish_at, view_base, view_count, latest_episode)
		VALUES ($1, $2, 'published', now() - interval '1 day', 100, 5, 30) RETURNING id`, mark+"-甲", mark+"-a")
	pubB := insertID(t, pool, `INSERT INTO drama (title, slug, status) VALUES ($1, $2, 'published') RETURNING id`, mark+"-乙", mark+"-b")
	draft := insertID(t, pool, `INSERT INTO drama (title, status) VALUES ($1, 'draft') RETURNING id`, mark+"-草稿")
	future := insertID(t, pool, `INSERT INTO drama (title, status, publish_at) VALUES ($1, 'published', now() + interval '1 day') RETURNING id`, mark+"-未到点")
	trashed := insertID(t, pool, `INSERT INTO drama (title, status, deleted_at) VALUES ($1, 'published', now()) RETURNING id`, mark+"-回收站")

	// 手选校验：只有不存在或在回收站的算缺失（草稿、未到点都算存在）
	missing, err := repo.MissingDramas(ctx, []int64{pubA, draft, future, trashed, -7})
	if err != nil || !slices.Equal(missing, []int64{trashed, -7}) {
		t.Fatalf("缺失的剧：%v %v", missing, err)
	}
	// 卡片：按给定顺序，过滤前台不可见的
	cards, err := repo.BlockDramaCards(ctx, []int64{pubB, pubA, draft, future, trashed})
	if err != nil || len(cards) != 2 || cards[0].ID != pubB || cards[1].ID != pubA {
		t.Fatalf("剧卡片按给定顺序且只含可见的：%+v %v", cards, err)
	}
	if cards[1].PlayCount != 105 || cards[1].LatestEpisode != 30 || cards[1].Slug != mark+"-a" {
		t.Fatalf("卡片字段：%+v", cards[1])
	}

	// 专题：只取已发布的；自动专题带规则
	manualTopic := insertID(t, pool, `INSERT INTO topic (title, slug, status) VALUES ($1, $2, 'published') RETURNING id`, mark+"-手选专题", mark+"-t1")
	autoTopic := insertID(t, pool, `INSERT INTO topic (title, slug, status, source_type, rule_json, list_in_index)
		VALUES ($1, $2, 'published', 'auto', $3, false) RETURNING id`, mark+"-自动专题", mark+"-t2",
		`{"term_codes":["`+mark+`-city"],"sort":"hot","window":"30d","limit":9}`)
	draftTopic := insertID(t, pool, `INSERT INTO topic (title, slug, status) VALUES ($1, $2, 'draft') RETURNING id`, mark+"-草稿专题", mark+"-t3")
	if _, err := pool.Exec(ctx, `INSERT INTO topic_drama (topic_id, drama_id, sort) VALUES ($1, $2, 1), ($1, $3, 0), ($1, $4, 2)`,
		manualTopic, pubA, pubB, trashed); err != nil {
		t.Fatal(err)
	}
	if ref, err := repo.BlockTopic(ctx, manualTopic); err != nil || ref.SourceType != "manual" || ref.Slug != mark+"-t1" {
		t.Fatalf("手选专题：%+v %v", ref, err)
	}
	ref, err := repo.BlockTopic(ctx, autoTopic)
	if err != nil || ref.SourceType != "auto" || ref.Rule.Sort != "hot" || ref.Rule.Window != "30d" || ref.Rule.Limit != 9 ||
		!slices.Equal(ref.Rule.TermCodes, []string{mark + "-city"}) {
		t.Fatalf("自动专题的规则：%+v %v", ref, err)
	}
	if _, err := repo.BlockTopic(ctx, draftTopic); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("草稿专题不对前台下发：%v", err)
	}
	// 手选专题的剧：按保存顺序，过滤不可见的，受条数限制
	topicCards, err := repo.BlockTopicDramas(ctx, manualTopic, 10)
	if err != nil || len(topicCards) != 2 || topicCards[0].ID != pubB || topicCards[1].ID != pubA {
		t.Fatalf("专题的剧按保存顺序：%+v %v", topicCards, err)
	}
	if topicCards, err = repo.BlockTopicDramas(ctx, manualTopic, 1); err != nil || len(topicCards) != 1 {
		t.Fatalf("专题的剧受条数限制：%+v %v", topicCards, err)
	}

	// 专题组：按给定顺序、只含已发布；不指定时取「列在专题列表页」的
	group, err := repo.BlockTopicCards(ctx, []int64{autoTopic, manualTopic, draftTopic})
	if err != nil || len(group) != 2 || group[0].ID != autoTopic || group[1].ID != manualTopic {
		t.Fatalf("专题卡片按给定顺序：%+v %v", group, err)
	}
	index, err := repo.BlockIndexTopics(ctx, 60)
	if err != nil {
		t.Fatal(err)
	}
	var sawManual, sawAuto, sawDraft bool
	for _, c := range index {
		switch c.ID {
		case manualTopic:
			sawManual = true
		case autoTopic: // list_in_index = false
			sawAuto = true
		case draftTopic:
			sawDraft = true
		}
	}
	if !sawManual || sawAuto || sawDraft {
		t.Fatalf("列表页专题只含已发布且列在列表页的：manual=%v auto=%v draft=%v", sawManual, sawAuto, sawDraft)
	}
	if index, err = repo.BlockIndexTopics(ctx, 1); err != nil || len(index) != 1 {
		t.Fatalf("专题条数限制：%+v %v", index, err)
	}

	// 标签云：按给定顺序，过滤隐藏、已合并的
	tagOK := insertID(t, pool, `INSERT INTO tag (name, slug) VALUES ($1, $2) RETURNING id`, mark+"-甜宠", mark+"-sweet")
	tagHidden := insertID(t, pool, `INSERT INTO tag (name, slug, is_hidden) VALUES ($1, $2, true) RETURNING id`, mark+"-隐藏", mark+"-hidden")
	tagOffline := insertID(t, pool, `INSERT INTO tag (name, slug, status) VALUES ($1, $2, 'hidden') RETURNING id`, mark+"-停用标签", mark+"-off")
	tagMerged := insertID(t, pool, `INSERT INTO tag (name, slug, merged_into_id) VALUES ($1, $2, $3) RETURNING id`, mark+"-已合并", mark+"-merged", tagOK)
	tags, err := repo.BlockTagsByIDs(ctx, []int64{tagMerged, tagOK, tagHidden, tagOffline})
	if err != nil || len(tags) != 1 || tags[0].ID != tagOK || tags[0].Name != mark+"-甜宠" {
		t.Fatalf("标签只含可用的：%+v %v", tags, err)
	}
	// 热门标签：按关联剧数倒序（共享库里有别的标签，只校验「条数不超、计数不递增」）
	if _, err := pool.Exec(ctx, `INSERT INTO drama_tag (drama_id, tag_id) VALUES ($1, $2), ($3, $2)`, pubA, tagOK, pubB); err != nil {
		t.Fatal(err)
	}
	top, err := repo.BlockTopTags(ctx, 5)
	if err != nil || len(top) > 5 {
		t.Fatalf("热门标签条数：%d %v", len(top), err)
	}
	last := -1
	for _, tg := range top {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM drama_tag WHERE tag_id = $1`, tg.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if last >= 0 && n > last {
			t.Fatalf("热门标签应按关联剧数倒序：%d 在 %d 之后", n, last)
		}
		last = n
	}

	// 存在性校验
	if found, err := repo.ExistingTopicIDs(ctx, []int64{manualTopic, draftTopic, -1}); err != nil || len(found) != 2 {
		t.Fatalf("专题存在性（草稿也算存在）：%v %v", found, err)
	}
	if found, err := repo.ExistingTagIDs(ctx, []int64{tagOK, -1}); err != nil || !slices.Equal(found, []int64{tagOK}) {
		t.Fatalf("标签存在性：%v %v", found, err)
	}
	if found, err := repo.ExistingTermCodes(ctx, []string{mark + "-city", mark + "-gone"}); err != nil ||
		!slices.Equal(found, []string{mark + "-city"}) {
		t.Fatalf("取值编号存在性：%v %v", found, err)
	}

	// ---------- 表约束 ----------
	ins := func(cols, vals string) string {
		return `INSERT INTO block (page, type, title, ` + cols + `) VALUES ('home', 'carousel', '` + mark + `-c', ` + vals + `)`
	}
	for name, sql := range map[string]string{
		"页面不合法":     `INSERT INTO block (page, type) VALUES ('explore', 'carousel')`,
		"频道页缺 slug": `INSERT INTO block (page, type) VALUES ('channel:', 'carousel')`,
		"未知类型":      `INSERT INTO block (page, type) VALUES ('home', 'banner')`,
		"数量超范围":     ins("item_limit", "61"),
		"数量为零":      ins("item_limit", "0"),
		"未知状态":      ins("status", "'archived'"),
		"每行个数超范围":   ins("cols_desktop", "9"),
		"未知设备":      ins("device", "'tv'"),
		"未知来源方式":    ins("source_type", "'rss'"),
		"来源不是对象":    ins("source_json", "'[]'::jsonb"),
	} {
		if _, err := pool.Exec(ctx, sql); pgCode(err) != "23514" {
			t.Errorf("%s应违反 CHECK 约束，得到 %v", name, err)
		}
	}
}
