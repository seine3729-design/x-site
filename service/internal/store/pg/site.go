package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/site"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// SiteRepo 读写站点编排域：静态页、前台菜单、页面区块（docs/specs/A3-1.md）。
type SiteRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewSiteRepo(pool *pgxpool.Pool) *SiteRepo {
	return &SiteRepo{pool: pool, q: sqlcgen.New(pool)}
}

// siteErr 把查询错误映射为领域错误：查不到为 ErrNotFound，slug 重复为 ErrConflict，其他错误加上下文。
func siteErr(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if isUniqueViolation(err) {
		return errs.ErrConflict.WithMessage("这个 slug 已经被用了，请换一个")
	}
	return fmt.Errorf("%s: %w", what, err)
}

// ---------- 静态页 ----------

func pageOf(r sqlcgen.Page) site.StaticPage {
	return site.StaticPage{
		ID: r.ID, Title: r.Title, Slug: r.Slug, Body: r.Body, BodyFormat: site.BodyFormat(r.BodyFormat),
		Status: site.PageStatus(r.Status), SEOTitle: r.SeoTitle, SEODesc: r.SeoDesc, IsFooter: r.IsFooter,
		CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func (r *SiteRepo) ListPages(ctx context.Context, q site.PageQuery) ([]site.StaticPage, int64, error) {
	pattern, status := nullText(q.Pattern), nullText(string(q.Status))
	rows, err := r.q.ListPages(ctx, sqlcgen.ListPagesParams{
		Pattern: pattern, Status: status, PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取静态页列表: %w", err)
	}
	total, err := r.q.CountPages(ctx, sqlcgen.CountPagesParams{Pattern: pattern, Status: status})
	if err != nil {
		return nil, 0, fmt.Errorf("统计静态页: %w", err)
	}
	out := make([]site.StaticPage, len(rows))
	for i, row := range rows {
		out[i] = pageOf(row)
	}
	return out, total, nil
}

func (r *SiteRepo) GetPage(ctx context.Context, id int64) (site.StaticPage, error) {
	row, err := r.q.GetPage(ctx, id)
	if err != nil {
		return site.StaticPage{}, siteErr(err, "读取静态页")
	}
	return pageOf(row), nil
}

func (r *SiteRepo) CreatePage(ctx context.Context, in site.PageInput) (site.StaticPage, error) {
	row, err := r.q.CreatePage(ctx, sqlcgen.CreatePageParams{
		Title: in.Title, Slug: in.Slug, Body: in.Body, BodyFormat: string(in.BodyFormat), Status: string(in.Status),
		SeoTitle: in.SEOTitle, SeoDesc: in.SEODesc, IsFooter: in.IsFooter,
	})
	if err != nil {
		return site.StaticPage{}, siteErr(err, "创建静态页")
	}
	return pageOf(row), nil
}

func (r *SiteRepo) UpdatePage(ctx context.Context, id int64, in site.PageInput) (site.StaticPage, error) {
	row, err := r.q.UpdatePage(ctx, sqlcgen.UpdatePageParams{
		ID: id, Title: in.Title, Slug: in.Slug, Body: in.Body, BodyFormat: string(in.BodyFormat), Status: string(in.Status),
		SeoTitle: in.SEOTitle, SeoDesc: in.SEODesc, IsFooter: in.IsFooter,
	})
	if err != nil {
		return site.StaticPage{}, siteErr(err, "保存静态页")
	}
	return pageOf(row), nil
}

func (r *SiteRepo) DeletePage(ctx context.Context, id int64) (site.StaticPage, error) {
	row, err := r.q.DeletePage(ctx, id)
	if err != nil {
		return site.StaticPage{}, siteErr(err, "删除静态页")
	}
	return pageOf(row), nil
}

func (r *SiteRepo) PublicPage(ctx context.Context, slug string) (site.StaticPage, error) {
	row, err := r.q.GetPublicPage(ctx, slug)
	if err != nil {
		return site.StaticPage{}, siteErr(err, "读取静态页")
	}
	return pageOf(row), nil
}

func (r *SiteRepo) FooterPages(ctx context.Context) ([]site.FooterPage, error) {
	rows, err := r.q.ListFooterPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取页脚页: %w", err)
	}
	out := make([]site.FooterPage, len(rows))
	for i, row := range rows {
		out[i] = site.FooterPage{Slug: row.Slug, Title: row.Title}
	}
	return out, nil
}

// ---------- 前台菜单 ----------

func siteMenuOf(r sqlcgen.Menu) site.Menu {
	return site.Menu{
		ID: r.ID, Position: site.MenuPosition(r.Position), Label: r.Label, LinkType: site.LinkType(r.LinkType),
		LinkValue: r.LinkValue, GroupTitle: r.GroupTitle, Icon: r.Icon, TabbarIcon: r.TabbarIcon, Badge: r.Badge,
		VisibleTo: site.MenuVisibleTo(r.VisibleTo), Device: site.MenuDevice(r.Device), NewTab: r.NewTab,
		Status: site.MenuStatus(r.Status), Sort: int(r.Sort),
		CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func (r *SiteRepo) ListMenus(ctx context.Context) ([]site.Menu, error) {
	rows, err := r.q.ListSiteMenus(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取前台菜单: %w", err)
	}
	out := make([]site.Menu, len(rows))
	for i, row := range rows {
		out[i] = siteMenuOf(row)
	}
	return out, nil
}

func (r *SiteRepo) GetMenu(ctx context.Context, id int64) (site.Menu, error) {
	row, err := r.q.GetSiteMenu(ctx, id)
	if err != nil {
		return site.Menu{}, siteErr(err, "读取前台菜单")
	}
	return siteMenuOf(row), nil
}

func (r *SiteRepo) CreateMenu(ctx context.Context, in site.MenuInput) (site.Menu, error) {
	row, err := r.q.CreateSiteMenu(ctx, sqlcgen.CreateSiteMenuParams{
		Position: string(in.Position), Label: in.Label, LinkType: string(in.LinkType), LinkValue: in.LinkValue,
		GroupTitle: in.GroupTitle, Icon: in.Icon, TabbarIcon: in.TabbarIcon, Badge: in.Badge,
		VisibleTo: string(in.VisibleTo), Device: string(in.Device), NewTab: in.NewTab, Status: string(in.Status),
	})
	if err != nil {
		return site.Menu{}, siteErr(err, "创建前台菜单")
	}
	return siteMenuOf(row), nil
}

func (r *SiteRepo) UpdateMenu(ctx context.Context, id int64, in site.MenuInput) (site.Menu, error) {
	row, err := r.q.UpdateSiteMenu(ctx, sqlcgen.UpdateSiteMenuParams{
		ID: id, Position: string(in.Position), Label: in.Label, LinkType: string(in.LinkType), LinkValue: in.LinkValue,
		GroupTitle: in.GroupTitle, Icon: in.Icon, TabbarIcon: in.TabbarIcon, Badge: in.Badge,
		VisibleTo: string(in.VisibleTo), Device: string(in.Device), NewTab: in.NewTab, Status: string(in.Status),
	})
	if err != nil {
		return site.Menu{}, siteErr(err, "保存前台菜单")
	}
	return siteMenuOf(row), nil
}

func (r *SiteRepo) DeleteMenu(ctx context.Context, id int64) (site.Menu, error) {
	row, err := r.q.DeleteSiteMenu(ctx, id)
	if err != nil {
		return site.Menu{}, siteErr(err, "删除前台菜单")
	}
	return siteMenuOf(row), nil
}

func (r *SiteRepo) CountActiveMenusAt(ctx context.Context, pos site.MenuPosition, exceptID int64) (int64, error) {
	n, err := r.q.CountActiveSiteMenusAt(ctx, sqlcgen.CountActiveSiteMenusAtParams{Position: string(pos), ExceptID: exceptID})
	if err != nil {
		return 0, fmt.Errorf("统计前台菜单: %w", err)
	}
	return n, nil
}

func (r *SiteRepo) MenuIDsAt(ctx context.Context, pos site.MenuPosition) ([]int64, error) {
	ids, err := r.q.SiteMenuIDsAt(ctx, string(pos))
	if err != nil {
		return nil, fmt.Errorf("读取前台菜单顺序: %w", err)
	}
	return ids, nil
}

func (r *SiteRepo) SetMenuOrder(ctx context.Context, pos site.MenuPosition, ids []int64) error {
	if err := r.q.SetSiteMenuSort(ctx, sqlcgen.SetSiteMenuSortParams{Ids: ids, Position: string(pos)}); err != nil {
		return fmt.Errorf("保存前台菜单顺序: %w", err)
	}
	return nil
}

// menuTargetRow 是四张目标表查询结果的统一形态。
type menuTargetRow struct {
	slug    string
	visible bool
}

// MenuTargets 按类型分组查链接目标：查到即存在，值为 true 表示前台当前可见。
func (r *SiteRepo) MenuTargets(ctx context.Context, targets []site.MenuTarget) (map[site.MenuTarget]bool, error) {
	bySlug := map[site.LinkType][]string{}
	for _, t := range targets {
		bySlug[t.Kind] = append(bySlug[t.Kind], t.Slug)
	}
	out := make(map[site.MenuTarget]bool, len(targets))
	for kind, slugs := range bySlug {
		rows, err := r.menuTargetRows(ctx, kind, slugs)
		if err != nil {
			return nil, fmt.Errorf("读取菜单链接目标: %w", err)
		}
		for _, row := range rows {
			t := site.MenuTarget{Kind: kind, Slug: row.slug}
			// 同一 slug 可能查到多行（如多个维度下的同名取值）：任一可见即可见
			out[t] = out[t] || row.visible
		}
	}
	return out, nil
}

// menuTargetRows 查一种类型的链接目标；站内页与外部网址不需要校验，返回空。
func (r *SiteRepo) menuTargetRows(ctx context.Context, kind site.LinkType, slugs []string) ([]menuTargetRow, error) {
	var out []menuTargetRow
	switch kind {
	case site.LinkPage:
		rows, err := r.q.MenuPageTargets(ctx, slugs)
		if err != nil {
			return nil, err
		}
		for _, x := range rows {
			out = append(out, menuTargetRow{x.Slug, x.Visible})
		}
	case site.LinkChannel:
		rows, err := r.q.MenuChannelTargets(ctx, slugs)
		if err != nil {
			return nil, err
		}
		for _, x := range rows {
			out = append(out, menuTargetRow{x.Slug, x.Visible})
		}
	case site.LinkTopic:
		rows, err := r.q.MenuTopicTargets(ctx, slugs)
		if err != nil {
			return nil, err
		}
		for _, x := range rows {
			out = append(out, menuTargetRow{x.Slug, x.Visible})
		}
	case site.LinkTag:
		rows, err := r.q.MenuTagTargets(ctx, slugs)
		if err != nil {
			return nil, err
		}
		for _, x := range rows {
			out = append(out, menuTargetRow{x.Slug, x.Visible})
		}
	}
	return out, nil
}

// ---------- 防丢地址（site_config.addresses）----------

func (r *SiteRepo) SiteAddresses(ctx context.Context) (site.Addresses, error) {
	raw, err := r.q.GetSiteAddresses(ctx)
	if err != nil {
		return site.Addresses{}, siteErr(err, "读取防丢地址")
	}
	return decodeAddresses(raw)
}

func (r *SiteRepo) SetSiteAddresses(ctx context.Context, a site.Addresses) (site.Addresses, error) {
	b, err := a.JSON()
	if err != nil {
		return site.Addresses{}, err
	}
	raw, err := r.q.SetSiteAddresses(ctx, b)
	if err != nil {
		return site.Addresses{}, siteErr(err, "保存防丢地址")
	}
	return decodeAddresses(raw)
}

func decodeAddresses(raw []byte) (site.Addresses, error) {
	var a site.Addresses
	if len(raw) == 0 {
		return a, nil
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return site.Addresses{}, fmt.Errorf("解析防丢地址: %w", err)
	}
	return a, nil
}

// ---------- 页面区块 ----------

func blockOf(r sqlcgen.Block) (site.Block, error) {
	src, err := site.ParseBlockSource(r.SourceJson)
	if err != nil {
		return site.Block{}, err
	}
	return site.Block{
		ID: r.ID, Page: r.Page, Type: site.BlockType(r.Type), Title: r.Title, Subtitle: r.Subtitle,
		ItemLimit: int(r.ItemLimit), Status: site.MenuStatus(r.Status), MoreLink: r.MoreLink,
		ColsDesktop: int(r.ColsDesktop), Device: site.MenuDevice(r.Device), SourceType: site.BlockSourceType(r.SourceType),
		Source: src, CustomHTML: r.CustomHtml, Sort: int(r.Sort),
		CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}, nil
}

func (r *SiteRepo) ListBlocks(ctx context.Context, page string) ([]site.Block, error) {
	rows, err := r.q.ListBlocks(ctx, page)
	if err != nil {
		return nil, fmt.Errorf("读取页面区块: %w", err)
	}
	out := make([]site.Block, len(rows))
	for i, row := range rows {
		if out[i], err = blockOf(row); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *SiteRepo) GetBlock(ctx context.Context, id int64) (site.Block, error) {
	row, err := r.q.GetBlock(ctx, id)
	if err != nil {
		return site.Block{}, siteErr(err, "读取页面区块")
	}
	return blockOf(row)
}

func (r *SiteRepo) CreateBlock(ctx context.Context, in site.BlockInput) (site.Block, error) {
	src, err := in.Source.JSON()
	if err != nil {
		return site.Block{}, err
	}
	row, err := r.q.CreateBlock(ctx, sqlcgen.CreateBlockParams{
		Page: in.Page, Type: string(in.Type), Title: in.Title, Subtitle: in.Subtitle, ItemLimit: int32(in.ItemLimit),
		Status: string(in.Status), MoreLink: in.MoreLink, ColsDesktop: int32(in.ColsDesktop), Device: string(in.Device),
		SourceType: string(in.SourceType), SourceJson: src, CustomHtml: in.CustomHTML,
	})
	if err != nil {
		return site.Block{}, siteErr(err, "创建页面区块")
	}
	return blockOf(row)
}

func (r *SiteRepo) UpdateBlock(ctx context.Context, id int64, in site.BlockInput) (site.Block, error) {
	src, err := in.Source.JSON()
	if err != nil {
		return site.Block{}, err
	}
	row, err := r.q.UpdateBlock(ctx, sqlcgen.UpdateBlockParams{
		ID: id, Page: in.Page, Type: string(in.Type), Title: in.Title, Subtitle: in.Subtitle, ItemLimit: int32(in.ItemLimit),
		Status: string(in.Status), MoreLink: in.MoreLink, ColsDesktop: int32(in.ColsDesktop), Device: string(in.Device),
		SourceType: string(in.SourceType), SourceJson: src, CustomHtml: in.CustomHTML,
	})
	if err != nil {
		return site.Block{}, siteErr(err, "保存页面区块")
	}
	return blockOf(row)
}

func (r *SiteRepo) DeleteBlock(ctx context.Context, id int64) (site.Block, error) {
	row, err := r.q.DeleteBlock(ctx, id)
	if err != nil {
		return site.Block{}, siteErr(err, "删除页面区块")
	}
	return blockOf(row)
}

func (r *SiteRepo) BlockIDsAt(ctx context.Context, page string) ([]int64, error) {
	ids, err := r.q.BlockIDsAt(ctx, page)
	if err != nil {
		return nil, fmt.Errorf("读取区块顺序: %w", err)
	}
	return ids, nil
}

func (r *SiteRepo) SetBlockOrder(ctx context.Context, page string, ids []int64) error {
	if err := r.q.SetBlockSort(ctx, sqlcgen.SetBlockSortParams{Ids: ids, Page: page}); err != nil {
		return fmt.Errorf("保存区块顺序: %w", err)
	}
	return nil
}

// BlockPages 返回可编排的页面：首页固定在前，其后是启用的频道页。
func (r *SiteRepo) BlockPages(ctx context.Context) ([]site.BlockPage, error) {
	rows, err := r.q.ListBlockPages(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取可编排页面: %w", err)
	}
	out := make([]site.BlockPage, 0, len(rows)+1)
	out = append(out, site.BlockPage{Value: "home", Label: "首页"})
	for _, row := range rows {
		out = append(out, site.BlockPage{Value: "channel:" + row.Slug, Label: row.Name})
	}
	return out, nil
}

func (r *SiteRepo) MissingDramas(ctx context.Context, ids []int64) ([]int64, error) {
	missing, err := r.q.BlockMissingDramas(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("校验剧集: %w", err)
	}
	return missing, nil
}

func (r *SiteRepo) BlockDramaCards(ctx context.Context, ids []int64) ([]content.DramaCard, error) {
	rows, err := r.q.BlockDramaCards(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("读取区块剧卡片: %w", err)
	}
	out := make([]content.DramaCard, len(rows))
	for i, row := range rows {
		out[i] = cardOf(row.ID, row.Title, row.Slug, row.CoverPortrait, row.CoverLandscape, row.Badge, row.Score, row.Year,
			row.IsFinished, row.LatestEpisode, row.PlayCount, row.PublishAt, row.CreatedAt, row.HotScore, row.SortWeight)
	}
	return out, nil
}

func (r *SiteRepo) BlockTopic(ctx context.Context, id int64) (site.BlockTopicRef, error) {
	row, err := r.q.BlockTopic(ctx, id)
	if err != nil {
		return site.BlockTopicRef{}, siteErr(err, "读取专题")
	}
	var rule content.TopicRule
	if len(row.RuleJson) > 0 {
		var raw struct {
			TermCodes []string `json:"term_codes"`
			TagID     *int64   `json:"tag_id"`
			Sort      string   `json:"sort"`
			Window    string   `json:"window"`
			Limit     int      `json:"limit"`
		}
		if err := json.Unmarshal(row.RuleJson, &raw); err != nil {
			return site.BlockTopicRef{}, fmt.Errorf("解析专题规则: %w", err)
		}
		rule = content.TopicRule{
			TermCodes: raw.TermCodes, TagID: raw.TagID, Sort: content.DramaSort(raw.Sort), Window: raw.Window, Limit: raw.Limit,
		}
	}
	return site.BlockTopicRef{
		ID: row.ID, Title: row.Title, Slug: row.Slug, Summary: row.Summary,
		SourceType: content.SourceType(row.SourceType), Rule: rule,
	}, nil
}

func (r *SiteRepo) BlockTopicDramas(ctx context.Context, topicID int64, limit int) ([]content.DramaCard, error) {
	rows, err := r.q.BlockTopicDramas(ctx, sqlcgen.BlockTopicDramasParams{TopicID: topicID, PageLimit: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("读取专题的剧: %w", err)
	}
	out := make([]content.DramaCard, len(rows))
	for i, row := range rows {
		out[i] = cardOf(row.ID, row.Title, row.Slug, row.CoverPortrait, row.CoverLandscape, row.Badge, row.Score, row.Year,
			row.IsFinished, row.LatestEpisode, row.PlayCount, row.PublishAt, row.CreatedAt, row.HotScore, row.SortWeight)
	}
	return out, nil
}

func (r *SiteRepo) BlockTopicCards(ctx context.Context, ids []int64) ([]content.TopicCard, error) {
	rows, err := r.q.BlockTopicCards(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("读取专题卡片: %w", err)
	}
	out := make([]content.TopicCard, len(rows))
	for i, row := range rows {
		out[i] = content.TopicCard{ID: row.ID, Title: row.Title, Slug: row.Slug, Summary: row.Summary}
	}
	return out, nil
}

func (r *SiteRepo) BlockIndexTopics(ctx context.Context, limit int) ([]content.TopicCard, error) {
	rows, err := r.q.BlockIndexTopics(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("读取专题卡片: %w", err)
	}
	out := make([]content.TopicCard, len(rows))
	for i, row := range rows {
		out[i] = content.TopicCard{ID: row.ID, Title: row.Title, Slug: row.Slug, Summary: row.Summary}
	}
	return out, nil
}

func (r *SiteRepo) BlockTagsByIDs(ctx context.Context, ids []int64) ([]site.BlockTag, error) {
	rows, err := r.q.BlockTagsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("读取标签: %w", err)
	}
	out := make([]site.BlockTag, len(rows))
	for i, row := range rows {
		out[i] = site.BlockTag{ID: row.ID, Name: row.Name, Slug: row.Slug}
	}
	return out, nil
}

func (r *SiteRepo) BlockTopTags(ctx context.Context, limit int) ([]site.BlockTag, error) {
	rows, err := r.q.BlockTopTags(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("读取热门标签: %w", err)
	}
	out := make([]site.BlockTag, len(rows))
	for i, row := range rows {
		out[i] = site.BlockTag{ID: row.ID, Name: row.Name, Slug: row.Slug}
	}
	return out, nil
}

func (r *SiteRepo) ExistingTopicIDs(ctx context.Context, ids []int64) ([]int64, error) {
	found, err := r.q.BlockExistingTopicIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("校验专题: %w", err)
	}
	return found, nil
}

func (r *SiteRepo) ExistingTagIDs(ctx context.Context, ids []int64) ([]int64, error) {
	found, err := r.q.BlockExistingTagIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("校验标签: %w", err)
	}
	return found, nil
}

func (r *SiteRepo) ExistingTermCodes(ctx context.Context, codes []string) ([]string, error) {
	found, err := r.q.ExistingTermCodes(ctx, codes)
	if err != nil {
		return nil, fmt.Errorf("校验维度取值编号: %w", err)
	}
	return found, nil
}
