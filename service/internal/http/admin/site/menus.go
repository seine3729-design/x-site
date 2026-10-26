package site

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/site"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// menuRequest 是新建与整体保存的请求体（未传的可选字段按默认值保存）。
type menuRequest struct {
	Position   string `json:"position"`
	Label      string `json:"label"`
	LinkType   string `json:"link_type"`
	LinkValue  string `json:"link_value"`
	GroupTitle string `json:"group_title"`
	Icon       string `json:"icon"`
	TabbarIcon string `json:"tabbar_icon"`
	Badge      string `json:"badge"`
	VisibleTo  string `json:"visible_to"`
	Device     string `json:"device"`
	NewTab     bool   `json:"new_tab"`
	Status     string `json:"status"`
}

func (r menuRequest) input() dom.MenuInput {
	return dom.MenuInput{
		Position: dom.MenuPosition(r.Position), Label: r.Label, LinkType: dom.LinkType(r.LinkType), LinkValue: r.LinkValue,
		GroupTitle: r.GroupTitle, Icon: r.Icon, TabbarIcon: r.TabbarIcon, Badge: r.Badge,
		VisibleTo: dom.MenuVisibleTo(r.VisibleTo), Device: dom.MenuDevice(r.Device), NewTab: r.NewTab, Status: dom.MenuStatus(r.Status),
	}
}

type menuDTO struct {
	ID         int64     `json:"id"`
	Position   string    `json:"position"`
	Label      string    `json:"label"`
	LinkType   string    `json:"link_type"`
	LinkValue  string    `json:"link_value"`
	GroupTitle string    `json:"group_title"`
	Icon       string    `json:"icon"`
	TabbarIcon string    `json:"tabbar_icon"`
	Badge      string    `json:"badge"`
	VisibleTo  string    `json:"visible_to"`
	Device     string    `json:"device"`
	NewTab     bool      `json:"new_tab"`
	Status     string    `json:"status"`
	Sort       int       `json:"sort"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	// TargetIssue 为空表示链接目标正常；否则是不可用的原因（前台不下发这一项，记录保留）
	TargetIssue string `json:"target_issue"`
}

func menuOf(m dom.Menu) menuDTO {
	return menuDTO{
		ID: m.ID, Position: string(m.Position), Label: m.Label, LinkType: string(m.LinkType), LinkValue: m.LinkValue,
		GroupTitle: m.GroupTitle, Icon: m.Icon, TabbarIcon: m.TabbarIcon, Badge: m.Badge, VisibleTo: string(m.VisibleTo),
		Device: string(m.Device), NewTab: m.NewTab, Status: string(m.Status), Sort: m.Sort,
		CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func menuViewOf(v dom.MenuView) menuDTO {
	d := menuOf(v.Menu)
	d.TargetIssue = v.TargetIssue
	return d
}

// ListMenus 是 GET /api/admin/menus：全部菜单（按位置与顺序），带链接目标的状态。菜单条数很少，一次返回全部。
func (h *Handlers) ListMenus(c *gin.Context) {
	list, err := h.Menus.List(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"list": mapSlice(list, menuViewOf)})
}

// CreateMenu 是 POST /api/admin/menus：新建后放在该位置的末尾，前台立即生效。
func (h *Handlers) CreateMenu(c *gin.Context) {
	var req menuRequest
	if !admin.Bind(c, &req) {
		return
	}
	m, err := h.Menus.Create(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := menuOf(m)
	admin.SetAudit(c, admin.AuditDetail{Action: "menu.create", TargetType: "menu", TargetID: itoa(m.ID), After: out})
	respond.OK(c, out)
}

// UpdateMenu 是 PATCH /api/admin/menus/:id：整体保存；改了位置时放到新位置的末尾。
func (h *Handlers) UpdateMenu(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req menuRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Menus.Update(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := menuOf(after)
	admin.SetAudit(c, admin.AuditDetail{
		Action: "menu.update", TargetType: "menu", TargetID: itoa(id), Before: menuOf(before), After: out,
	})
	respond.OK(c, out)
}

// DeleteMenu 是 DELETE /api/admin/menus/:id：删除不可恢复。
func (h *Handlers) DeleteMenu(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	m, err := h.Menus.Delete(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "menu.delete", TargetType: "menu", TargetID: itoa(id), Before: menuOf(m)})
	respond.OK(c, gin.H{"id": id})
}

// menuReorderRequest 是某个位置的新顺序：必须恰好是该位置现有的全部菜单。
type menuReorderRequest struct {
	Position string  `json:"position"`
	IDs      []int64 `json:"ids"`
}

// ReorderMenus 是 POST /api/admin/menus/reorder：按位置重排。
func (h *Handlers) ReorderMenus(c *gin.Context) {
	var req menuReorderRequest
	if !admin.Bind(c, &req) {
		return
	}
	if req.IDs == nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请提供 ids（该位置菜单的新顺序）"))
		return
	}
	pos := dom.MenuPosition(req.Position)
	if err := h.Menus.Reorder(c.Request.Context(), pos, req.IDs); err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "menu.reorder", TargetType: "menu", TargetID: req.Position, After: gin.H{"ids": req.IDs},
	})
	respond.OK(c, gin.H{"position": req.Position, "ids": req.IDs})
}

// ---------- 防丢地址（与菜单同页，走 menus.view / menus.edit）----------

type socialDTO struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type addressesDTO struct {
	Notice       string      `json:"notice"`
	LatestURL    string      `json:"latest_url"`
	PermanentURL string      `json:"permanent_url"`
	BackupURLs   []string    `json:"backup_urls"`
	PublishPage  string      `json:"publish_page"`
	SupportURL   string      `json:"support_url"`
	Telegram     string      `json:"telegram"`
	Socials      []socialDTO `json:"socials"`
}

func addressesOf(a dom.Addresses) addressesDTO {
	return addressesDTO{
		Notice: a.Notice, LatestURL: a.LatestURL, PermanentURL: a.PermanentURL, BackupURLs: strs(a.BackupURLs),
		PublishPage: a.PublishPage, SupportURL: a.SupportURL, Telegram: a.Telegram,
		Socials: mapSlice(a.Socials, func(s dom.Social) socialDTO { return socialDTO{Name: s.Name, URL: s.URL} }),
	}
}

func (r addressesDTO) input() dom.Addresses {
	return dom.Addresses{
		Notice: r.Notice, LatestURL: r.LatestURL, PermanentURL: r.PermanentURL, BackupURLs: r.BackupURLs,
		PublishPage: r.PublishPage, SupportURL: r.SupportURL, Telegram: r.Telegram,
		Socials: mapSlice(r.Socials, func(s socialDTO) dom.Social { return dom.Social{Name: s.Name, URL: s.URL} }),
	}
}

// GetAddresses 是 GET /api/admin/addresses：防丢地址「回家的路」。
func (h *Handlers) GetAddresses(c *gin.Context) {
	a, err := h.Addresses.Get(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, addressesOf(a))
}

// SetAddresses 是 PUT /api/admin/addresses：整体保存；保存后站点配置版本 +1，各实例秒级生效。
func (h *Handlers) SetAddresses(c *gin.Context) {
	var req addressesDTO
	if !admin.Bind(c, &req) {
		return
	}
	before, after, version, err := h.Addresses.Set(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := addressesOf(after)
	admin.SetAudit(c, admin.AuditDetail{
		Action: "addresses.update", TargetType: "site_config", TargetID: "addresses", Before: addressesOf(before), After: out,
	})
	respond.OK(c, gin.H{"addresses": out, "site_version": version})
}
