package site

import (
	"context"
	"strings"
	"testing"

	"xsite/service/internal/errs"
)

// fakeBumper 记录站点配置版本 +1 的次数。
type fakeBumper struct {
	calls   int
	version int64
	err     error
}

func (f *fakeBumper) BumpVersion(context.Context) (int64, error) {
	f.calls++
	f.version++
	return f.version, f.err
}

// fakeMenuRepo 只实现测试用到的部分。
type fakeMenuRepo struct {
	menus    []Menu
	targets  map[MenuTarget]bool
	input    MenuInput
	activeAt int64
	exceptID int64
	order    []int64
	orderPos MenuPosition
	err      error
}

func (f *fakeMenuRepo) ListMenus(context.Context) ([]Menu, error) { return f.menus, f.err }

func (f *fakeMenuRepo) GetMenu(_ context.Context, id int64) (Menu, error) {
	for _, m := range f.menus {
		if m.ID == id {
			return m, nil
		}
	}
	return Menu{}, errs.ErrNotFound
}

func (f *fakeMenuRepo) CreateMenu(_ context.Context, in MenuInput) (Menu, error) {
	f.input = in
	return Menu{ID: 9, Position: in.Position, Label: in.Label, LinkType: in.LinkType, LinkValue: in.LinkValue, Status: in.Status}, f.err
}

func (f *fakeMenuRepo) UpdateMenu(_ context.Context, id int64, in MenuInput) (Menu, error) {
	f.input = in
	return Menu{ID: id, Position: in.Position, Label: in.Label, LinkType: in.LinkType, LinkValue: in.LinkValue, Status: in.Status}, f.err
}

func (f *fakeMenuRepo) DeleteMenu(_ context.Context, id int64) (Menu, error) {
	return Menu{ID: id, Label: "被删除"}, f.err
}

func (f *fakeMenuRepo) CountActiveMenusAt(_ context.Context, _ MenuPosition, exceptID int64) (int64, error) {
	f.exceptID = exceptID
	return f.activeAt, f.err
}

func (f *fakeMenuRepo) MenuIDsAt(_ context.Context, pos MenuPosition) ([]int64, error) {
	var ids []int64
	for _, m := range f.menus {
		if m.Position == pos {
			ids = append(ids, m.ID)
		}
	}
	return ids, f.err
}

func (f *fakeMenuRepo) SetMenuOrder(_ context.Context, pos MenuPosition, ids []int64) error {
	f.orderPos, f.order = pos, ids
	return f.err
}

func (f *fakeMenuRepo) MenuTargets(_ context.Context, targets []MenuTarget) (map[MenuTarget]bool, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[MenuTarget]bool{}
	for _, t := range targets {
		if visible, ok := f.targets[t]; ok {
			out[t] = visible
		}
	}
	return out, nil
}

func newMenuSvc() (*MenuService, *fakeMenuRepo, *memCache, *fakeBumper) {
	repo := &fakeMenuRepo{targets: map[MenuTarget]bool{
		{Kind: LinkPage, Slug: "terms"}:   true,
		{Kind: LinkPage, Slug: "draft"}:   false, // 存在但是草稿
		{Kind: LinkChannel, Slug: "city"}: true,
		{Kind: LinkTopic, Slug: "hot"}:    true,
		{Kind: LinkTag, Slug: "sweet"}:    false, // 已隐藏或已合并
	}}
	c, b := newMemCache(), &fakeBumper{}
	return NewMenuService(repo, c, b), repo, c, b
}

func TestCleanMenu(t *testing.T) {
	in, err := cleanMenu(MenuInput{Position: PositionTop, Label: "  首页  ", LinkType: LinkInternal, LinkValue: " home "})
	if err != nil || in.Label != "首页" || in.LinkValue != "home" {
		t.Fatalf("去空白：%+v %v", in, err)
	}
	if in.VisibleTo != VisibleAll || in.Device != DeviceAll || in.Status != MenuActive {
		t.Fatalf("默认所有人、全部设备、启用：%+v", in)
	}

	// slug 类链接转小写并校验格式
	if in, err = cleanMenu(MenuInput{Position: PositionTop, Label: "条款", LinkType: LinkPage, LinkValue: " Terms "}); err != nil || in.LinkValue != "terms" {
		t.Fatalf("slug 转小写：%+v %v", in, err)
	}
	// 外部网址只接受 http(s)
	if in, err = cleanMenu(MenuInput{Position: PositionFooter, Label: "外链", LinkType: LinkExternal, LinkValue: "https://example.com/a"}); err != nil {
		t.Fatal(err)
	}
	// 分组标题只有页脚保留，底栏图标只有手机底栏保留
	in, _ = cleanMenu(MenuInput{Position: PositionTop, Label: "首页", LinkType: LinkInternal, LinkValue: "home", GroupTitle: "帮助", TabbarIcon: "house"})
	if in.GroupTitle != "" || in.TabbarIcon != "" {
		t.Fatalf("非页脚不保存分组标题、非底栏不保存底栏图标：%+v", in)
	}
	in, _ = cleanMenu(MenuInput{Position: PositionFooter, Label: "条款", LinkType: LinkPage, LinkValue: "terms", GroupTitle: "帮助与条款"})
	if in.GroupTitle != "帮助与条款" {
		t.Fatalf("页脚保留分组标题：%+v", in)
	}
	in, _ = cleanMenu(MenuInput{Position: PositionTabbar, Label: "我的", LinkType: LinkInternal, LinkValue: "me", TabbarIcon: "user-round"})
	if in.TabbarIcon != "user-round" {
		t.Fatalf("手机底栏保留底栏图标：%+v", in)
	}

	base := MenuInput{Position: PositionTop, Label: "首页", LinkType: LinkInternal, LinkValue: "home"}
	bad := []struct {
		in  MenuInput
		msg string
	}{
		{MenuInput{Label: "x", LinkType: LinkInternal, LinkValue: "home"}, "请选择菜单位置"},
		{MenuInput{Position: "sidebar", Label: "x", LinkType: LinkInternal, LinkValue: "home"}, "请选择菜单位置"},
		{MenuInput{Position: PositionTop, LinkType: LinkInternal, LinkValue: "home"}, "名称不能为空"},
		{MenuInput{Position: PositionTop, Label: strings.Repeat("字", 21), LinkType: LinkInternal, LinkValue: "home"}, "名称不能超过 20 个字"},
		{MenuInput{Position: PositionTop, Label: "x", LinkType: "rss", LinkValue: "a"}, "请选择链接类型"},
		{MenuInput{Position: PositionTop, Label: "x", LinkType: LinkInternal, LinkValue: "dashboard"}, "请选择站内页"},
		{MenuInput{Position: PositionTop, Label: "x", LinkType: LinkPage, LinkValue: ""}, "请填写静态页的 slug"},
		{MenuInput{Position: PositionTop, Label: "x", LinkType: LinkChannel, LinkValue: "有中文"}, "频道的 slug"},
		{MenuInput{Position: PositionTop, Label: "x", LinkType: LinkExternal, LinkValue: "/vip"}, "外部网址要填"},
		{MenuInput{Position: PositionTop, Label: "x", LinkType: LinkExternal, LinkValue: "javascript:alert(1)"}, "外部网址"},
		{MenuInput{Position: PositionTop, Label: "x", LinkType: LinkExternal, LinkValue: ""}, "外部网址要填"},
		{withIcon(base, "房子图标"), "图标名只能用字母"},
		{withIcon(base, strings.Repeat("a", 41)), "图标名不能超过"},
		{withBadge(base, "太长的角标文字"), "角标不能超过 6 个字"},
		{withVisible(base, "vip"), "「谁能看」"},
		{withDevice(base, "tv"), "设备只能是"},
		{withStatus(base, "archived"), "状态只能是启用或停用"},
	}
	for _, b := range bad {
		_, err := cleanMenu(b.in)
		wantErr(t, err, errs.ErrBadRequest, b.msg)
	}
}

func withIcon(in MenuInput, v string) MenuInput    { in.Icon = v; return in }
func withBadge(in MenuInput, v string) MenuInput   { in.Badge = v; return in }
func withVisible(in MenuInput, v string) MenuInput { in.VisibleTo = MenuVisibleTo(v); return in }
func withDevice(in MenuInput, v string) MenuInput  { in.Device = MenuDevice(v); return in }
func withStatus(in MenuInput, v string) MenuInput  { in.Status = MenuStatus(v); return in }

func TestMenuServiceCreateChecksTargetAndTabbarLimit(t *testing.T) {
	svc, repo, _, bumper := newMenuSvc()
	ctx := context.Background()

	// 目标存在（即使当前是草稿也允许保存：发布后自动出现）
	if _, err := svc.Create(ctx, MenuInput{Position: PositionTop, Label: "条款", LinkType: LinkPage, LinkValue: "draft"}); err != nil {
		t.Fatalf("目标存在但未发布时也能保存：%v", err)
	}
	if bumper.calls != 1 {
		t.Fatalf("保存后站点配置版本要 +1：%d", bumper.calls)
	}
	// 目标不存在
	_, err := svc.Create(ctx, MenuInput{Position: PositionTop, Label: "条款", LinkType: LinkPage, LinkValue: "gone"})
	wantErr(t, err, errs.ErrBadRequest, "静态页「gone」不存在")
	_, err = svc.Create(ctx, MenuInput{Position: PositionTop, Label: "标签", LinkType: LinkTag, LinkValue: "missing"})
	wantErr(t, err, errs.ErrBadRequest, "标签「missing」不存在")

	// 手机底栏最多 5 项（只算启用的）
	repo.activeAt = maxTabbarItems
	_, err = svc.Create(ctx, MenuInput{Position: PositionTabbar, Label: "我的", LinkType: LinkInternal, LinkValue: "me"})
	wantErr(t, err, errs.ErrBadRequest, "手机底栏最多 5 项")
	// 停用的不受限制
	if _, err := svc.Create(ctx, MenuInput{
		Position: PositionTabbar, Label: "我的", LinkType: LinkInternal, LinkValue: "me", Status: MenuInactive,
	}); err != nil {
		t.Fatalf("停用的不算进底栏上限：%v", err)
	}
	// 编辑时排除自己
	repo.activeAt = maxTabbarItems - 1
	if _, _, err := svc.Update(ctx, 7, MenuInput{Position: PositionTabbar, Label: "我的", LinkType: LinkInternal, LinkValue: "me"}); err == nil && repo.exceptID != 7 {
		t.Fatalf("编辑时按自己的编号排除：%d", repo.exceptID)
	}
}

func TestMenuServiceListMarksTargetIssue(t *testing.T) {
	svc, repo, _, _ := newMenuSvc()
	repo.menus = []Menu{
		{ID: 1, Position: PositionTop, Label: "首页", LinkType: LinkInternal, LinkValue: "home", Status: MenuActive},
		{ID: 2, Position: PositionTop, Label: "条款", LinkType: LinkPage, LinkValue: "terms", Status: MenuActive},
		{ID: 3, Position: PositionTop, Label: "草稿页", LinkType: LinkPage, LinkValue: "draft", Status: MenuActive},
		{ID: 4, Position: PositionTop, Label: "没了", LinkType: LinkPage, LinkValue: "gone", Status: MenuActive},
		{ID: 5, Position: PositionFooter, Label: "甜宠", LinkType: LinkTag, LinkValue: "sweet", Status: MenuInactive},
	}
	list, err := svc.List(context.Background())
	if err != nil || len(list) != 5 {
		t.Fatalf("列表：%d %v", len(list), err)
	}
	issues := map[int64]string{}
	for _, v := range list {
		issues[v.ID] = v.TargetIssue
	}
	if issues[1] != "" || issues[2] != "" {
		t.Fatalf("站内页与已发布的静态页没有问题：%v", issues)
	}
	if !strings.Contains(issues[3], "当前不对前台显示") || !strings.Contains(issues[4], "已删除") {
		t.Fatalf("草稿与已删除的目标要标出原因：%v", issues)
	}
	if !strings.Contains(issues[5], "当前不对前台显示") {
		t.Fatalf("标签隐藏或已合并也要标出：%v", issues)
	}
}

func TestMenuServicePublicFilters(t *testing.T) {
	svc, repo, c, _ := newMenuSvc()
	ctx := context.Background()
	repo.menus = []Menu{
		{ID: 1, Position: PositionTop, Label: "首页", LinkType: LinkInternal, LinkValue: "home", Status: MenuActive, VisibleTo: VisibleAll, Device: DeviceAll},
		{ID: 2, Position: PositionTop, Label: "停用", LinkType: LinkInternal, LinkValue: "explore", Status: MenuInactive},
		{ID: 3, Position: PositionTop, Label: "草稿页", LinkType: LinkPage, LinkValue: "draft", Status: MenuActive},
		{ID: 4, Position: PositionFooter, Label: "条款", LinkType: LinkPage, LinkValue: "terms", Status: MenuActive, GroupTitle: "帮助与条款"},
		{ID: 5, Position: PositionTabbar, Label: "我的", LinkType: LinkInternal, LinkValue: "me", Status: MenuActive, VisibleTo: VisibleAuth},
	}
	list, err := svc.Public(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, m := range list {
		ids = append(ids, m.ID)
	}
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 4 || ids[2] != 5 {
		t.Fatalf("只下发启用且目标可见的（停用、草稿目标去掉）：%v", ids)
	}
	// 「谁能看」原样下发，由前台判断
	if list[2].VisibleTo != VisibleAuth || list[1].GroupTitle != "帮助与条款" {
		t.Fatalf("visible_to 与分组标题原样下发：%+v", list)
	}
	if _, err := svc.Public(ctx); err != nil || c.loads != 1 {
		t.Fatalf("第二次应命中缓存：loads=%d %v", c.loads, err)
	}

	// 没有菜单时下发空数组
	c.data = map[string]any{}
	repo.menus = nil
	if list, err = svc.Public(ctx); err != nil || list == nil || len(list) != 0 {
		t.Fatalf("没有菜单：%#v %v", list, err)
	}
}

func TestMenuServiceReorder(t *testing.T) {
	svc, repo, c, bumper := newMenuSvc()
	ctx := context.Background()
	repo.menus = []Menu{
		{ID: 1, Position: PositionTop}, {ID: 2, Position: PositionTop}, {ID: 3, Position: PositionFooter},
	}
	if err := svc.Reorder(ctx, PositionTop, []int64{2, 1}); err != nil {
		t.Fatal(err)
	}
	if repo.orderPos != PositionTop || len(repo.order) != 2 || repo.order[0] != 2 {
		t.Fatalf("按给定顺序重排：%v %v", repo.orderPos, repo.order)
	}
	if bumper.calls != 1 || len(c.deletedKeys()) == 0 {
		t.Fatalf("重排后清缓存并 +1 版本：%d %v", bumper.calls, c.deletedKeys())
	}
	// 必须恰好是该位置现有的全部菜单
	for _, bad := range [][]int64{{1}, {1, 2, 3}, {1, 1}, {}} {
		if err := svc.Reorder(ctx, PositionTop, bad); errs.From(err).Code != errs.ErrConflict.Code {
			t.Fatalf("%v 应 409，得到 %v", bad, err)
		}
	}
	if err := svc.Reorder(ctx, "sidebar", []int64{1}); errs.From(err).Code != errs.ErrNotFound.Code {
		t.Fatalf("未知位置应 404：%v", err)
	}
}

func TestMenuServiceDeleteInvalidates(t *testing.T) {
	svc, _, c, bumper := newMenuSvc()
	if _, err := svc.Delete(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if bumper.calls != 1 || c.deletedKeys()[0] != menuCacheKey {
		t.Fatalf("删除后清菜单缓存并 +1 版本：%d %v", bumper.calls, c.deletedKeys())
	}
}
