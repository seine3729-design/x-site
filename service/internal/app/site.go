package app

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/ops"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/domain/site"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg"
)

// siteServices 是站点编排域的服务（静态页、前台菜单、防丢地址、页面区块，docs/specs/A3-1.md）。
// 静态页、菜单与地址都随站点配置下发或影响下发内容，所以都拿到同一个 VersionBumper（platform.SiteConfigService）。
type siteServices struct {
	pages     *site.PageService
	menus     *site.MenuService
	addresses *site.AddressService
	blocks    *site.BlockService
}

func newSiteServices(
	pool *pgxpool.Pool, cache site.Cache, version site.VersionBumper,
	picker site.DramaPicker, ranks site.RankBoardReader,
) siteServices {
	repo := pg.NewSiteRepo(pool)
	return siteServices{
		pages:     site.NewPageService(repo, cache, version),
		menus:     site.NewMenuService(repo, cache, version),
		addresses: site.NewAddressService(repo, version),
		blocks:    site.NewBlockService(repo, cache, picker, ranks),
	}
}

// sections 返回随站点配置下发的站点编排内容（实现 http/public.SiteSections）。
func (s siteServices) sections() siteSections { return siteSections(s) }

type siteSections siteServices

func (s siteSections) Menus(ctx context.Context) ([]site.PublicMenu, error) {
	return s.menus.Public(ctx)
}

func (s siteSections) FooterPages(ctx context.Context) ([]site.FooterPage, error) {
	return s.pages.FooterPages(ctx)
}

// rankBlocks 把榜单接到排行榜区块上（实现 site.RankBoardReader）：榜单功能关闭或还没算过时当作没有内容，
// 这样该区块不下发，而不是整页报错。
type rankBlocks struct{ svc *ops.RankService }

func (r rankBlocks) RankDramas(ctx context.Context, board string, limit int) ([]content.DramaCard, error) {
	b, err := r.svc.PublicBoard(ctx, ops.RankBoard(board))
	if errs.From(err).Code == errs.ErrNotFound.Code {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]content.DramaCard, 0, min(len(b.List), limit))
	for i, item := range b.List {
		if i >= limit {
			break
		}
		out = append(out, item.Drama)
	}
	return out, nil
}

// mediaDomains 把存储域的线路域名池接到站点配置上（实现 platform.MediaDomainReader）。
// 两个域互不依赖，字段一对一，转换放在装配层（docs/04 F：域名池随 site-config 下发，
// 前台用它拼图片相对 key 并按请求生成 CSP）。
type mediaDomains struct{ pool *storage.PoolService }

func (m mediaDomains) MediaDomains(ctx context.Context) (platform.MediaDomains, error) {
	p, err := m.pool.DomainPool(ctx)
	if err != nil {
		return platform.MediaDomains{}, err
	}
	return platform.MediaDomains{ImageBases: p.ImageBases, VideoOrigins: p.VideoOrigins}, nil
}
