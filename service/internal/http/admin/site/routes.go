package site

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/site"
	"xsite/service/internal/http/admin"
)

// PageService 是静态页管理（domain/site.PageService）。
type PageService interface {
	List(ctx context.Context, f dom.PageFilter) (dom.Page[dom.StaticPage], error)
	Create(ctx context.Context, in dom.PageInput) (dom.StaticPage, error)
	Update(ctx context.Context, id int64, in dom.PageInput) (before, after dom.StaticPage, err error)
	Delete(ctx context.Context, id int64) (dom.StaticPage, error)
}

// MenuService 是前台菜单管理（domain/site.MenuService）。
type MenuService interface {
	List(ctx context.Context) ([]dom.MenuView, error)
	Create(ctx context.Context, in dom.MenuInput) (dom.Menu, error)
	Update(ctx context.Context, id int64, in dom.MenuInput) (before, after dom.Menu, err error)
	Delete(ctx context.Context, id int64) (dom.Menu, error)
	Reorder(ctx context.Context, pos dom.MenuPosition, ids []int64) error
}

// AddressService 是防丢地址（domain/site.AddressService）。
type AddressService interface {
	Get(ctx context.Context) (dom.Addresses, error)
	Set(ctx context.Context, in dom.Addresses) (before, after dom.Addresses, version int64, err error)
}

// BlockService 是页面区块管理（domain/site.BlockService）。customHTML 表示当前管理员有 blocks.custom_html 权限。
type BlockService interface {
	Pages(ctx context.Context) ([]dom.BlockPage, error)
	List(ctx context.Context, page string) ([]dom.Block, error)
	Create(ctx context.Context, in dom.BlockInput, customHTML bool) (dom.Block, error)
	Update(ctx context.Context, id int64, in dom.BlockInput, customHTML bool) (before, after dom.Block, err error)
	Delete(ctx context.Context, id int64) (dom.Block, error)
	Reorder(ctx context.Context, page string, ids []int64) error
}

// Handlers 聚合框架目录的 handler。
type Handlers struct {
	Pages     PageService
	Menus     MenuService
	Addresses AddressService
	Blocks    BlockService
}

// Routes 把框架目录的接口注册到 admin.Routes 返回的分组上。读接口要 <页面>.view，写接口要 <页面>.edit。
func (h *Handlers) Routes(authed gin.IRouter) {
	pages := authed.Group("/pages", admin.RequirePage("pages"))
	pages.GET("", h.ListPages)
	pages.POST("", h.CreatePage)
	pages.PATCH("/:id", h.UpdatePage)
	pages.DELETE("/:id", h.DeletePage)

	menus := authed.Group("/menus", admin.RequirePage("menus"))
	menus.GET("", h.ListMenus)
	menus.POST("", h.CreateMenu)
	menus.POST("/reorder", h.ReorderMenus)
	menus.PATCH("/:id", h.UpdateMenu)
	menus.DELETE("/:id", h.DeleteMenu)

	// 防丢地址「回家的路」与菜单同页（docs/06 第 12 节），用同一组权限点
	addr := authed.Group("/addresses", admin.RequirePage("menus"))
	addr.GET("", h.GetAddresses)
	addr.PUT("", h.SetAddresses)

	// 自定义 HTML 区块另要 blocks.custom_html：是否需要取决于请求体与原记录，由 handler 交给领域层判断
	admin.DeclarePermissions(permBlockCustomHTML)
	blocks := authed.Group("/blocks", admin.RequirePage("blocks"))
	blocks.GET("", h.ListBlocks)
	blocks.GET("/pages", h.BlockPages)
	blocks.POST("", h.CreateBlock)
	blocks.POST("/reorder", h.ReorderBlocks)
	blocks.PATCH("/:id", h.UpdateBlock)
	blocks.DELETE("/:id", h.DeleteBlock)
}
