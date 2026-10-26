package site

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
	"xsite/service/internal/htmlsan"
)

// PageStatus 是静态页的发布状态；草稿不对前台下发。
type PageStatus string

const (
	PageDraft     PageStatus = "draft"
	PagePublished PageStatus = "published"
)

// BodyFormat 是正文格式：rich 由后台富文本编辑器产出，html 直接写 HTML。两种都按同一白名单清洗后入库。
type BodyFormat string

const (
	BodyRich BodyFormat = "rich"
	BodyHTML BodyFormat = "html"
)

const (
	maxPageTitleRunes = 100
	maxPageSEOTitle   = 100
	maxPageSEODesc    = 200
	maxPageBodyRunes  = 100_000 // 提交上限（清洗前）
	maxPageBodyStored = 50_000  // 清洗后入库上限
	maxFooterPages    = 20
)

// StaticPage 是一个静态页（条款、隐私、FAQ 等），前台地址 /p/:slug。
type StaticPage struct {
	ID         int64
	Title      string
	Slug       string
	Body       string // 清洗后的 HTML
	BodyFormat BodyFormat
	Status     PageStatus
	SEOTitle   string
	SEODesc    string
	IsFooter   bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// PageInput 是静态页的新建与整体保存。
type PageInput struct {
	Title      string
	Slug       string
	Body       string
	BodyFormat BodyFormat
	Status     PageStatus
	SEOTitle   string
	SEODesc    string
	IsFooter   bool
}

// PageFilter 是后台列表的筛选：Q 匹配标题或 slug。
type PageFilter struct {
	Q      string
	Status PageStatus
	PageRequest
}

// PageQuery 是校验后交给仓储的查询。Pattern 是已转义的 LIKE 模式，空表示不按文本搜。
type PageQuery struct {
	Pattern string
	Status  PageStatus
	Limit   int
	Offset  int
}

// FooterPage 是随站点配置下发的页脚页（docs/04 第 13 节，页脚「帮助与条款」）。
type FooterPage struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// PublicPage 是前台 /p/:slug 的内容。
type PublicPage struct {
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	SEOTitle  string    `json:"seo_title"`
	SEODesc   string    `json:"seo_desc"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PageRepo 读写静态页。查不到时返回 errs.ErrNotFound；slug 重复时返回 errs.ErrConflict。
type PageRepo interface {
	ListPages(ctx context.Context, q PageQuery) ([]StaticPage, int64, error)
	GetPage(ctx context.Context, id int64) (StaticPage, error)
	CreatePage(ctx context.Context, in PageInput) (StaticPage, error)
	UpdatePage(ctx context.Context, id int64, in PageInput) (StaticPage, error)
	// DeletePage 删除静态页（不进回收站），返回删除前的记录。
	DeletePage(ctx context.Context, id int64) (StaticPage, error)
	// PublicPage 按 slug 返回已发布的静态页。
	PublicPage(ctx context.Context, slug string) (StaticPage, error)
	// FooterPages 返回已发布且标记为页脚页的静态页（按创建顺序）。
	FooterPages(ctx context.Context) ([]FooterPage, error)
}

// 前台静态页与页脚页的缓存：后台改动后立即删除，TTL 只是兜底。
const (
	pageCacheTTL   = 5 * time.Minute
	footerCacheKey = "site:footer-pages"
)

func pageCacheKey(slug string) string { return "site:page:" + slug }

// PageService 是静态页的后台管理与前台读取。
type PageService struct {
	repo    PageRepo
	cache   Cache
	version VersionBumper
}

// NewPageService 创建静态页服务。页脚页随站点配置下发，所以写操作也要令配置版本 +1。
func NewPageService(repo PageRepo, cache Cache, version VersionBumper) *PageService {
	return &PageService{repo: repo, cache: cache, version: version}
}

// List 返回后台静态页列表的一页（按编号倒序）。
func (s *PageService) List(ctx context.Context, f PageFilter) (Page[StaticPage], error) {
	q, err := cleanQuery(f.Q)
	if err != nil {
		return Page[StaticPage]{}, err
	}
	if f.Status != "" && f.Status != PageDraft && f.Status != PagePublished {
		return Page[StaticPage]{}, badRequest("状态筛选无效")
	}
	p, limit, offset, err := limitOffset(f.PageRequest)
	if err != nil {
		return Page[StaticPage]{}, err
	}
	list, total, err := s.repo.ListPages(ctx, PageQuery{Pattern: likePattern(q), Status: f.Status, Limit: limit, Offset: offset})
	if err != nil {
		return Page[StaticPage]{}, err
	}
	return Page[StaticPage]{List: list, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// Create 新建静态页。
func (s *PageService) Create(ctx context.Context, in PageInput) (StaticPage, error) {
	in, err := cleanPage(in)
	if err != nil {
		return StaticPage{}, err
	}
	p, err := s.repo.CreatePage(ctx, in)
	if err == nil {
		s.invalidate(ctx, p.Slug)
	}
	return p, err
}

// Update 整体保存静态页，返回修改前后的记录。改了 slug 时两个地址的缓存都清掉。
func (s *PageService) Update(ctx context.Context, id int64, in PageInput) (before, after StaticPage, err error) {
	if in, err = cleanPage(in); err != nil {
		return
	}
	if before, err = s.repo.GetPage(ctx, id); err != nil {
		return
	}
	if after, err = s.repo.UpdatePage(ctx, id, in); err == nil {
		s.invalidate(ctx, before.Slug, after.Slug)
	}
	return
}

// Delete 删除静态页（不可恢复），返回删除前的记录。指向它的前台菜单项随后自动隐藏。
func (s *PageService) Delete(ctx context.Context, id int64) (StaticPage, error) {
	p, err := s.repo.DeletePage(ctx, id)
	if err == nil {
		s.invalidate(ctx, p.Slug)
	}
	return p, err
}

// Public 返回前台静态页；草稿与不存在的 slug 都是 404。
func (s *PageService) Public(ctx context.Context, slug string) (PublicPage, error) {
	if !content.ValidSlug(slug) {
		return PublicPage{}, errs.ErrNotFound
	}
	var out PublicPage
	err := s.cache.GetOrLoadJSON(ctx, pageCacheKey(slug), pageCacheTTL, &out, func(ctx context.Context) (any, error) {
		p, err := s.repo.PublicPage(ctx, slug)
		if err != nil {
			return nil, err
		}
		return PublicPage{
			Slug: p.Slug, Title: p.Title, Body: p.Body, SEOTitle: p.SEOTitle, SEODesc: p.SEODesc, UpdatedAt: p.UpdatedAt,
		}, nil
	})
	if err != nil {
		return PublicPage{}, err
	}
	return out, nil
}

// FooterPages 返回随站点配置下发的页脚页。
func (s *PageService) FooterPages(ctx context.Context) ([]FooterPage, error) {
	var out []FooterPage
	err := s.cache.GetOrLoadJSON(ctx, footerCacheKey, pageCacheTTL, &out, func(ctx context.Context) (any, error) {
		list, err := s.repo.FooterPages(ctx)
		if err != nil {
			return nil, err
		}
		if len(list) > maxFooterPages {
			list = list[:maxFooterPages]
		}
		if list == nil {
			list = []FooterPage{}
		}
		return list, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// invalidate 清掉受影响的静态页缓存、页脚页缓存与菜单缓存（指向该页的菜单项可见性可能变了），
// 并令站点配置版本 +1（页脚页随站点配置下发）。清缓存失败只记日志：最多 pageCacheTTL 后自然过期。
func (s *PageService) invalidate(ctx context.Context, slugs ...string) {
	keys := make([]string, 0, len(slugs)+2)
	for _, slug := range slugs {
		if slug != "" && !containsStr(keys, pageCacheKey(slug)) {
			keys = append(keys, pageCacheKey(slug))
		}
	}
	keys = append(keys, footerCacheKey, menuCacheKey)
	for _, k := range keys {
		if err := s.cache.Delete(ctx, k); err != nil {
			slog.WarnContext(ctx, "清除静态页缓存失败，稍后随 TTL 过期", "key", k, "err", err)
		}
	}
	if _, err := s.version.BumpVersion(ctx); err != nil {
		slog.ErrorContext(ctx, "静态页改动后更新站点配置版本失败，前台可能仍用旧的页脚页与菜单", "err", err)
	}
}

func containsStr(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// cleanPage 校验并规范化静态页的输入：正文按白名单清洗后入库（两种格式同一套白名单）。
func cleanPage(in PageInput) (PageInput, error) {
	var err error
	if in.Title, err = cleanText("标题", in.Title, maxPageTitleRunes, true); err != nil {
		return in, err
	}
	in.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	if in.Slug == "" {
		return in, badRequest("请填写 slug（前台地址 /p/<slug>）")
	}
	if !content.ValidSlug(in.Slug) {
		return in, badRequest("slug 只能用小写字母、数字和连字符，首尾为字母或数字，最长 64 个字符")
	}
	switch in.BodyFormat {
	case "":
		in.BodyFormat = BodyRich
	case BodyRich, BodyHTML:
	default:
		return in, badRequest("正文格式只能是普通或 HTML")
	}
	if in.Body, err = sanitizePageBody(in.Body); err != nil {
		return in, err
	}
	switch in.Status {
	case "":
		in.Status = PageDraft
	case PageDraft, PagePublished:
	default:
		return in, badRequest("状态只能是草稿或已发布")
	}
	if in.SEOTitle, err = cleanText("SEO 标题", in.SEOTitle, maxPageSEOTitle, false); err != nil {
		return in, err
	}
	if in.SEODesc, err = cleanText("SEO 简介", in.SEODesc, maxPageSEODesc, false); err != nil {
		return in, err
	}
	return in, nil
}

// sanitizePageBody 清洗正文；清洗后什么都不剩时拒绝保存（例如整篇只有脚本）。
func sanitizePageBody(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", badRequest("请填写正文")
	}
	if utf8.RuneCountInString(raw) > maxPageBodyRunes {
		return "", badRequest("正文不能超过 %d 个字符", maxPageBodyRunes)
	}
	out := htmlsan.Sanitize(raw)
	if out == "" {
		return "", badRequest("正文清洗后为空：只允许常见排版标签、链接与图片，脚本、样式表、iframe 与事件属性会被去掉")
	}
	if utf8.RuneCountInString(out) > maxPageBodyStored {
		return "", badRequest("正文清洗后超过 %d 个字符，请精简", maxPageBodyStored)
	}
	return out, nil
}
