package site

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
)

// MenuPosition 是菜单出现的位置（docs/06 第 12 节）。
type MenuPosition string

const (
	PositionTop    MenuPosition = "top"           // 顶部导航
	PositionTabbar MenuPosition = "mobile_tabbar" // 手机底栏
	PositionDrawer MenuPosition = "mobile_drawer" // 手机侧边抽屉
	PositionFooter MenuPosition = "footer"        // 页脚
)

// MenuPositions 是全部位置。
var MenuPositions = []MenuPosition{PositionTop, PositionTabbar, PositionDrawer, PositionFooter}

// LinkType 是菜单的链接类型。
type LinkType string

const (
	LinkInternal LinkType = "internal" // 站内页（白名单）
	LinkPage     LinkType = "page"     // 静态页 slug
	LinkChannel  LinkType = "channel"  // 频道（设为频道的取值）slug
	LinkTopic    LinkType = "topic"    // 专题 slug
	LinkTag      LinkType = "tag"      // 标签 slug
	LinkExternal LinkType = "external" // 外部网址
)

// LinkTypes 是全部链接类型。
var LinkTypes = []LinkType{LinkInternal, LinkPage, LinkChannel, LinkTopic, LinkTag, LinkExternal}

// InternalPages 是站内页白名单（docs/04 第 B 节的前台页面）。
var InternalPages = []string{"home", "shua", "explore", "rank", "topic", "search", "me", "way_home"}

// MenuVisibleTo 是「谁能看」。站点配置是共享缓存的公共数据，不能按身份裁剪，所以下发后由前台判断。
type MenuVisibleTo string

const (
	VisibleAll   MenuVisibleTo = "all"
	VisibleAuth  MenuVisibleTo = "auth"  // 只给已登录
	VisibleGuest MenuVisibleTo = "guest" // 只给未登录
)

// MenuDevice 是投放的端。
type MenuDevice string

const (
	DeviceAll     MenuDevice = "all"
	DeviceMobile  MenuDevice = "mobile"
	DeviceDesktop MenuDevice = "desktop"
)

// MenuStatus 是启用状态。
type MenuStatus string

const (
	MenuActive   MenuStatus = "active"
	MenuInactive MenuStatus = "inactive"
)

const (
	maxMenuLabelRunes = 20
	maxMenuGroupRunes = 20
	maxMenuBadgeRunes = 6
	maxMenuIconLen    = 40
	maxTabbarItems    = 5 // 手机底栏最多 5 项（docs/04 导航规范）
	maxMenusAt        = 50
)

// Menu 是一条前台菜单。
type Menu struct {
	ID         int64
	Position   MenuPosition
	Label      string
	LinkType   LinkType
	LinkValue  string
	GroupTitle string // 页脚分组标题，只有页脚用
	Icon       string
	TabbarIcon string // 只有手机底栏用
	Badge      string
	VisibleTo  MenuVisibleTo
	Device     MenuDevice
	NewTab     bool
	Status     MenuStatus
	Sort       int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// MenuInput 是菜单的新建与整体保存。
type MenuInput struct {
	Position   MenuPosition
	Label      string
	LinkType   LinkType
	LinkValue  string
	GroupTitle string
	Icon       string
	TabbarIcon string
	Badge      string
	VisibleTo  MenuVisibleTo
	Device     MenuDevice
	NewTab     bool
	Status     MenuStatus
}

// MenuView 是后台显示的菜单：带链接目标的状态。目标不可用时前台不下发，但记录保留（docs/06 第 12 节）。
type MenuView struct {
	Menu
	// TargetIssue 为空表示目标正常；否则是不可用的原因（后台显示「目标不可用」并说明）。
	TargetIssue string
}

// PublicMenu 是随站点配置下发的菜单项。visible_to 与 device 由前台按登录态与设备判断。
type PublicMenu struct {
	ID         int64         `json:"id"`
	Position   MenuPosition  `json:"position"`
	Label      string        `json:"label"`
	LinkType   LinkType      `json:"link_type"`
	LinkValue  string        `json:"link_value"`
	GroupTitle string        `json:"group_title"`
	Icon       string        `json:"icon"`
	TabbarIcon string        `json:"tabbar_icon"`
	Badge      string        `json:"badge"`
	VisibleTo  MenuVisibleTo `json:"visible_to"`
	Device     MenuDevice    `json:"device"`
	NewTab     bool          `json:"new_tab"`
}

// MenuTarget 是一个菜单链接目标（只有 page / channel / topic / tag 需要校验）。
type MenuTarget struct {
	Kind LinkType
	Slug string
}

// MenuRepo 读写前台菜单。查不到时返回 errs.ErrNotFound。
type MenuRepo interface {
	ListMenus(ctx context.Context) ([]Menu, error)
	GetMenu(ctx context.Context, id int64) (Menu, error)
	CreateMenu(ctx context.Context, in MenuInput) (Menu, error)
	UpdateMenu(ctx context.Context, id int64, in MenuInput) (Menu, error)
	DeleteMenu(ctx context.Context, id int64) (Menu, error)
	// CountActiveMenusAt 返回某位置启用中的条数，exceptID 排除正在编辑的那条。
	CountActiveMenusAt(ctx context.Context, pos MenuPosition, exceptID int64) (int64, error)
	// MenuIDsAt 返回某位置全部菜单的编号（按现有顺序）。
	MenuIDsAt(ctx context.Context, pos MenuPosition) ([]int64, error)
	// SetMenuOrder 按 ids 的顺序重排某位置。
	SetMenuOrder(ctx context.Context, pos MenuPosition, ids []int64) error
	// MenuTargets 返回这些目标的状态：在结果里表示目标存在，值为 true 表示前台当前可见。
	MenuTargets(ctx context.Context, targets []MenuTarget) (map[MenuTarget]bool, error)
}

// VersionBumper 令站点配置版本 +1 并广播失效（platform.SiteConfigService）。
// 菜单、页脚页与防丢地址都随站点配置下发，改动后必须调用，否则客户端会继续用旧的 ETag。
type VersionBumper interface {
	BumpVersion(ctx context.Context) (int64, error)
}

// 前台菜单的缓存：后台改动后立即删除，TTL 兜底的是「链接目标被别的模块改掉」（如静态页改回草稿、频道取值停用）。
const (
	menuCacheKey = "site:menus"
	menuCacheTTL = 5 * time.Minute
)

// MenuService 是前台菜单的后台管理与下发。
type MenuService struct {
	repo    MenuRepo
	cache   Cache
	version VersionBumper
}

// NewMenuService 创建菜单服务。
func NewMenuService(repo MenuRepo, cache Cache, version VersionBumper) *MenuService {
	return &MenuService{repo: repo, cache: cache, version: version}
}

// List 返回后台菜单列表（全部位置，按位置与顺序），带链接目标的状态。
func (s *MenuService) List(ctx context.Context) ([]MenuView, error) {
	menus, err := s.repo.ListMenus(ctx)
	if err != nil {
		return nil, err
	}
	state, err := s.targetState(ctx, menus)
	if err != nil {
		return nil, err
	}
	out := make([]MenuView, len(menus))
	for i, m := range menus {
		out[i] = MenuView{Menu: m, TargetIssue: targetIssue(m, state)}
	}
	return out, nil
}

// Create 新建菜单（放在该位置的末尾）。
func (s *MenuService) Create(ctx context.Context, in MenuInput) (Menu, error) {
	in, err := cleanMenu(in)
	if err != nil {
		return Menu{}, err
	}
	if err := s.checkTarget(ctx, in); err != nil {
		return Menu{}, err
	}
	if err := s.checkTabbarLimit(ctx, in, 0); err != nil {
		return Menu{}, err
	}
	m, err := s.repo.CreateMenu(ctx, in)
	if err == nil {
		s.invalidate(ctx)
	}
	return m, err
}

// Update 整体保存菜单，返回修改前后的记录。
func (s *MenuService) Update(ctx context.Context, id int64, in MenuInput) (before, after Menu, err error) {
	if in, err = cleanMenu(in); err != nil {
		return
	}
	if err = s.checkTarget(ctx, in); err != nil {
		return
	}
	if err = s.checkTabbarLimit(ctx, in, id); err != nil {
		return
	}
	if before, err = s.repo.GetMenu(ctx, id); err != nil {
		return
	}
	if after, err = s.repo.UpdateMenu(ctx, id, in); err == nil {
		s.invalidate(ctx)
	}
	return
}

// Delete 删除菜单（不可恢复），返回删除前的记录。
func (s *MenuService) Delete(ctx context.Context, id int64) (Menu, error) {
	m, err := s.repo.DeleteMenu(ctx, id)
	if err == nil {
		s.invalidate(ctx)
	}
	return m, err
}

// Reorder 按 ids 的顺序重排某个位置。ids 必须恰好是该位置现有的全部菜单，否则 409（别人刚增删过，让前端刷新）。
func (s *MenuService) Reorder(ctx context.Context, pos MenuPosition, ids []int64) error {
	if !slices.Contains(MenuPositions, pos) {
		return errs.ErrNotFound
	}
	current, err := s.repo.MenuIDsAt(ctx, pos)
	if err != nil {
		return err
	}
	if !sameSet(current, ids) {
		return errs.ErrConflict.WithMessage("菜单已被改动，请刷新后重新排序")
	}
	if err := s.repo.SetMenuOrder(ctx, pos, ids); err != nil {
		return err
	}
	s.invalidate(ctx)
	return nil
}

// Public 返回下发给前台的菜单：启用中、链接目标当前可见的，按位置与顺序。
func (s *MenuService) Public(ctx context.Context) ([]PublicMenu, error) {
	var out []PublicMenu
	err := s.cache.GetOrLoadJSON(ctx, menuCacheKey, menuCacheTTL, &out, func(ctx context.Context) (any, error) {
		menus, err := s.repo.ListMenus(ctx)
		if err != nil {
			return nil, err
		}
		state, err := s.targetState(ctx, menus)
		if err != nil {
			return nil, err
		}
		list := make([]PublicMenu, 0, len(menus))
		for _, m := range menus {
			if m.Status != MenuActive || targetIssue(m, state) != "" {
				continue
			}
			list = append(list, PublicMenu{
				ID: m.ID, Position: m.Position, Label: m.Label, LinkType: m.LinkType, LinkValue: m.LinkValue,
				GroupTitle: m.GroupTitle, Icon: m.Icon, TabbarIcon: m.TabbarIcon, Badge: m.Badge,
				VisibleTo: m.VisibleTo, Device: m.Device, NewTab: m.NewTab,
			})
		}
		return list, nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []PublicMenu{}
	}
	return out, nil
}

// invalidate 清菜单缓存并令站点配置版本 +1（菜单随站点配置下发）。
func (s *MenuService) invalidate(ctx context.Context) {
	if err := s.cache.Delete(ctx, menuCacheKey); err != nil {
		slog.WarnContext(ctx, "清除菜单缓存失败，稍后随 TTL 过期", "err", err)
	}
	if _, err := s.version.BumpVersion(ctx); err != nil {
		slog.ErrorContext(ctx, "菜单改动后更新站点配置版本失败，前台可能仍用旧菜单", "err", err)
	}
}

// targetState 查出菜单引用的目标现在的状态：在结果里表示存在，值为 true 表示前台可见。
func (s *MenuService) targetState(ctx context.Context, menus []Menu) (map[MenuTarget]bool, error) {
	var targets []MenuTarget
	for _, m := range menus {
		if t, ok := targetOf(m); ok && !slices.Contains(targets, t) {
			targets = append(targets, t)
		}
	}
	if len(targets) == 0 {
		return map[MenuTarget]bool{}, nil
	}
	return s.repo.MenuTargets(ctx, targets)
}

// targetOf 返回需要校验的链接目标；站内页与外部网址不需要。
func targetOf(m Menu) (MenuTarget, bool) {
	switch m.LinkType {
	case LinkPage, LinkChannel, LinkTopic, LinkTag:
		return MenuTarget{Kind: m.LinkType, Slug: m.LinkValue}, true
	}
	return MenuTarget{}, false
}

var targetNames = map[LinkType]string{LinkPage: "静态页", LinkChannel: "频道", LinkTopic: "专题", LinkTag: "标签"}

// targetIssue 返回链接目标不可用的原因；可用时为空字符串。
func targetIssue(m Menu, state map[MenuTarget]bool) string {
	t, ok := targetOf(m)
	if !ok {
		return ""
	}
	visible, exists := state[t]
	switch {
	case !exists:
		return fmt.Sprintf("%s「%s」已删除", targetNames[t.Kind], t.Slug)
	case !visible:
		return fmt.Sprintf("%s「%s」当前不对前台显示", targetNames[t.Kind], t.Slug)
	}
	return ""
}

// checkTarget 保存时校验链接目标存在（不要求当前可见：可以先建好菜单，目标发布后自动出现）。
func (s *MenuService) checkTarget(ctx context.Context, in MenuInput) error {
	t, ok := targetOf(Menu{LinkType: in.LinkType, LinkValue: in.LinkValue})
	if !ok {
		return nil
	}
	state, err := s.repo.MenuTargets(ctx, []MenuTarget{t})
	if err != nil {
		return err
	}
	if _, exists := state[t]; !exists {
		return badRequest("%s「%s」不存在，请确认 slug", targetNames[t.Kind], t.Slug)
	}
	return nil
}

// checkTabbarLimit 手机底栏最多 maxTabbarItems 项（只算启用的）。
func (s *MenuService) checkTabbarLimit(ctx context.Context, in MenuInput, exceptID int64) error {
	if in.Position != PositionTabbar || in.Status != MenuActive {
		return nil
	}
	n, err := s.repo.CountActiveMenusAt(ctx, PositionTabbar, exceptID)
	if err != nil {
		return err
	}
	if n >= maxTabbarItems {
		return badRequest("手机底栏最多 %d 项，请先停用或删除其他项", maxTabbarItems)
	}
	return nil
}

func cleanMenu(in MenuInput) (MenuInput, error) {
	var err error
	if !slices.Contains(MenuPositions, in.Position) {
		return in, badRequest("请选择菜单位置")
	}
	if in.Label, err = cleanText("名称", in.Label, maxMenuLabelRunes, true); err != nil {
		return in, err
	}
	if !slices.Contains(LinkTypes, in.LinkType) {
		return in, badRequest("请选择链接类型")
	}
	if in.LinkValue, err = cleanLinkValue(in.LinkType, in.LinkValue); err != nil {
		return in, err
	}
	// 分组标题只有页脚用，底栏图标只有手机底栏用
	if in.Position == PositionFooter {
		if in.GroupTitle, err = cleanText("分组标题", in.GroupTitle, maxMenuGroupRunes, false); err != nil {
			return in, err
		}
	} else {
		in.GroupTitle = ""
	}
	if in.Position == PositionTabbar {
		if in.TabbarIcon, err = cleanIcon("底栏图标", in.TabbarIcon); err != nil {
			return in, err
		}
	} else {
		in.TabbarIcon = ""
	}
	if in.Icon, err = cleanIcon("图标", in.Icon); err != nil {
		return in, err
	}
	if in.Badge, err = cleanText("角标", in.Badge, maxMenuBadgeRunes, false); err != nil {
		return in, err
	}
	switch in.VisibleTo {
	case "":
		in.VisibleTo = VisibleAll
	case VisibleAll, VisibleAuth, VisibleGuest:
	default:
		return in, badRequest("「谁能看」只能是所有人、已登录或未登录")
	}
	switch in.Device {
	case "":
		in.Device = DeviceAll
	case DeviceAll, DeviceMobile, DeviceDesktop:
	default:
		return in, badRequest("设备只能是全部、手机或电脑")
	}
	switch in.Status {
	case "":
		in.Status = MenuActive
	case MenuActive, MenuInactive:
	default:
		return in, badRequest("状态只能是启用或停用")
	}
	return in, nil
}

// cleanLinkValue 按链接类型校验链接内容。
func cleanLinkValue(kind LinkType, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	switch kind {
	case LinkInternal:
		if !slices.Contains(InternalPages, v) {
			return "", badRequest("请选择站内页")
		}
		return v, nil
	case LinkExternal:
		v, err := cleanLink("外部网址", v)
		if err != nil {
			return "", err
		}
		if v == "" || strings.HasPrefix(v, "/") {
			return "", badRequest("外部网址要填 http:// 或 https:// 开头的完整地址")
		}
		return v, nil
	default:
		v = strings.ToLower(v)
		if v == "" {
			return "", badRequest("请填写%s的 slug", targetNames[kind])
		}
		if !content.ValidSlug(v) {
			return "", badRequest("%s的 slug 只能用小写字母、数字和连字符", targetNames[kind])
		}
		return v, nil
	}
}

// cleanIcon 校验图标名（lucide 图标名：字母、数字与连字符）。
func cleanIcon(label, raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", nil
	}
	if len(s) > maxMenuIconLen {
		return "", badRequest("%s名不能超过 %d 个字符", label, maxMenuIconLen)
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return "", badRequest("%s名只能用字母、数字和连字符（lucide 图标名，如 house）", label)
		}
	}
	return s, nil
}

// sameSet 判断两组编号是否是同一集合（不看顺序，不允许重复）。
func sameSet(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := append([]int64(nil), a...), append([]int64(nil), b...)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}
