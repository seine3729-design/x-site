package content

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"xsite/service/internal/errs"
)

// 前台读接口的缓存 TTL（docs/03 第 3.2 节）。列表类都 ≤ 60 秒，所以后台改动不需要主动失效；
// 剧详情 120 秒，按剧 ID 单独成键，S4-4 缓存管理可以精确清除。
const (
	dramaCacheTTL   = 120 * time.Second
	channelCacheTTL = 60 * time.Second
	tagCacheTTL     = 60 * time.Second
	exploreCacheTTL = 30 * time.Second
	topicCacheTTL   = 300 * time.Second
)

const (
	publicDefaultLimit = 24
	publicMaxLimit     = 60
	publicMaxPage      = 100
	maxCursorLen       = 128
	maxTermFilters     = 8
	relatedLimit       = 12
	manualTopicLimit   = 200
)

// 上新窗口（docs/03 第 3.2 节）到天数。
var windowDays = map[string]int{"7d": 7, "14d": 14, "30d": 30, "90d": 90}

// Doc 是带 ETag 的缓存文档：ETag 在回源时按载荷算一次，和载荷一起进缓存，命中缓存时不再重算。
type Doc[T any] struct {
	ETag    string `json:"etag"`
	Payload T      `json:"payload"`
}

// newDoc 用载荷的摘要作为 ETag，内容变了 ETag 一定变。
func newDoc[T any](payload T) (Doc[T], error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return Doc[T]{}, fmt.Errorf("计算 ETag: %w", err)
	}
	sum := sha256.Sum256(b)
	return Doc[T]{ETag: `W/"` + base64.RawURLEncoding.EncodeToString(sum[:16]) + `"`, Payload: payload}, nil
}

// loadDoc 读穿缓存：命中则直接返回缓存里的文档（含 ETag），未命中时 build 一次并写回。
func loadDoc[T any](ctx context.Context, c Cache, key string, ttl time.Duration, build func(context.Context) (T, error)) (Doc[T], error) {
	var doc Doc[T]
	err := c.GetOrLoadJSON(ctx, key, ttl, &doc, func(ctx context.Context) (any, error) {
		payload, err := build(ctx)
		if err != nil {
			return nil, err
		}
		return newDoc(payload)
	})
	if err != nil {
		return Doc[T]{}, err
	}
	return doc, nil
}

// PublicService 提供前台内容读接口（S1-4）。所有方法只返回前台可见的内容。
type PublicService struct {
	repo  PublicRepo
	cache Cache
	now   func() time.Time
}

func NewPublicService(repo PublicRepo, cache Cache) *PublicService {
	return &PublicService{repo: repo, cache: cache, now: func() time.Time { return time.Now().UTC() }}
}

// ListRequest 是游标分页列表的请求参数。
type ListRequest struct {
	Sort   DramaSort
	Cursor string
	Limit  int
}

// ChannelRequest 是频道页（页码分页）的请求参数。
type ChannelRequest struct {
	Sort     DramaSort
	Page     int
	PageSize int
}

// ExploreRequest 是探索页的请求参数。Terms 形如 `维度slug:取值slug,维度slug:取值slug`。
type ExploreRequest struct {
	Terms    string
	Tag      string
	Finished *bool
	Window   string
	Sort     DramaSort
	Cursor   string
	Limit    int
}

// ---------- 剧详情 ----------

// Drama 返回剧详情：资料、标签与取值、分集、相关推荐。不可见的剧返回 404。
func (s *PublicService) Drama(ctx context.Context, id int64) (Doc[DramaDetailView], error) {
	if id <= 0 {
		return Doc[DramaDetailView]{}, errs.ErrNotFound
	}
	return loadDoc(ctx, s.cache, "content:drama:"+strconv.FormatInt(id, 10), dramaCacheTTL,
		func(ctx context.Context) (DramaDetailView, error) { return s.buildDrama(ctx, id) })
}

func (s *PublicService) buildDrama(ctx context.Context, id int64) (DramaDetailView, error) {
	d, err := s.repo.PublicDrama(ctx, id)
	if err != nil {
		return DramaDetailView{}, err
	}
	tags, err := s.repo.PublicDramaTags(ctx, id)
	if err != nil {
		return DramaDetailView{}, err
	}
	terms, err := s.repo.PublicDramaTerms(ctx, id)
	if err != nil {
		return DramaDetailView{}, err
	}
	episodes, err := s.repo.PublicEpisodes(ctx, id)
	if err != nil {
		return DramaDetailView{}, err
	}

	out := DramaDetailView{
		DramaCard:    cardOfDrama(d),
		Aliases:      orEmpty(d.Aliases),
		Summary:      d.Summary,
		SEOTitle:     d.SEOTitle,
		SEODesc:      d.SEODesc,
		EpisodeCount: len(episodes),
		Tags:         make([]TagView, 0, len(tags)),
		Terms:        orEmpty(terms),
		Episodes:     orEmpty(episodes),
		Related:      []DramaCard{},
	}
	tagIDs := make([]int64, 0, len(tags))
	for _, t := range tags {
		out.Tags = append(out.Tags, TagView{ID: t.ID, Name: t.Name, Slug: t.Slug, IsSensitive: t.IsHidden})
		tagIDs = append(tagIDs, t.ID)
	}
	termIDs := make([]int64, 0, len(terms))
	for _, t := range terms {
		termIDs = append(termIDs, t.ID)
	}
	// 没有任何标签与取值时召回不到相关剧，省一次查询。
	if len(tagIDs) > 0 || len(termIDs) > 0 {
		related, err := s.repo.RelatedDramas(ctx, id, tagIDs, termIDs, relatedLimit)
		if err != nil {
			return DramaDetailView{}, err
		}
		out.Related = orEmpty(related)
	}
	return out, nil
}

// ---------- 频道 ----------

// Channel 返回频道页的一页。频道不存在或已停用时 404。
func (s *PublicService) Channel(ctx context.Context, slug string, in ChannelRequest) (Doc[ChannelPage], error) {
	if !validPublicSlug(slug) {
		return Doc[ChannelPage]{}, errs.ErrNotFound
	}
	sortBy, err := publicSort(in.Sort, false)
	if err != nil {
		return Doc[ChannelPage]{}, err
	}
	page, pageSize, err := publicPage(in.Page, in.PageSize)
	if err != nil {
		return Doc[ChannelPage]{}, err
	}
	key := fmt.Sprintf("content:channel:%s:%s:%d:%d", slug, sortBy, page, pageSize)
	return loadDoc(ctx, s.cache, key, channelCacheTTL, func(ctx context.Context) (ChannelPage, error) {
		ch, err := s.repo.PublicChannel(ctx, slug)
		if err != nil {
			return ChannelPage{}, err
		}
		out := ChannelPage{Channel: ch, List: []DramaCard{}, Page: page, PageSize: pageSize}
		filter := PublicDramaFilter{TermIDs: []int64{ch.ID}}
		if out.Total, err = s.repo.CountPublicDramas(ctx, filter); err != nil {
			return ChannelPage{}, err
		}
		list, err := s.repo.PublicDramas(ctx, PublicDramaQuery{
			PublicDramaFilter: filter, Sort: sortBy, Limit: pageSize, Offset: (page - 1) * pageSize,
		})
		if err != nil {
			return ChannelPage{}, err
		}
		out.List = orEmpty(list)
		return out, nil
	})
}

// ---------- 标签 ----------

// Tag 返回标签聚合列表的一页。标签已被合并时按目标标签取剧；不存在或已隐藏时 404。
func (s *PublicService) Tag(ctx context.Context, slug string, in ListRequest) (Doc[TagPage], error) {
	if !validPublicSlug(slug) {
		return Doc[TagPage]{}, errs.ErrNotFound
	}
	sortBy, limit, pos, err := s.listParams(in)
	if err != nil {
		return Doc[TagPage]{}, err
	}
	key := fmt.Sprintf("content:tag:%s:%s:%s", slug, sortBy, cursorKey(sortBy, pos, limit))
	return loadDoc(ctx, s.cache, key, tagCacheTTL, func(ctx context.Context) (TagPage, error) {
		tag, err := s.repo.PublicTagBySlug(ctx, slug)
		if err != nil {
			return TagPage{}, err
		}
		target := tag
		if tag.MergedIntoID != nil {
			if target, err = s.repo.PublicTagByID(ctx, *tag.MergedIntoID); err != nil {
				return TagPage{}, err
			}
		}
		out := TagPage{
			Tag: TagInfo{
				ID: target.ID, Name: target.Name, Slug: slug, CanonicalSlug: target.Slug,
				IsSensitive: tag.IsHidden || target.IsHidden,
			},
			List: []DramaCard{},
		}
		filter := PublicDramaFilter{TagID: target.ID}
		if out.Total, err = s.repo.CountPublicDramas(ctx, filter); err != nil {
			return TagPage{}, err
		}
		cur, err := pos.dramaCursor()
		if err != nil {
			return TagPage{}, err
		}
		list, err := s.repo.PublicDramas(ctx, PublicDramaQuery{
			PublicDramaFilter: filter, Sort: sortBy, Seed: pos.Seed, Cursor: cur, Limit: limit,
		})
		if err != nil {
			return TagPage{}, err
		}
		out.List = orEmpty(list)
		out.NextCursor = nextCursor(sortBy, pos.Seed, out.List, limit)
		return out, nil
	})
}

// ---------- 探索 ----------

// Explore 返回探索页的一页。筛选项之间是「与」；分面只在第一页返回。
func (s *PublicService) Explore(ctx context.Context, in ExploreRequest) (Doc[ExplorePage], error) {
	sortBy, limit, pos, err := s.listParams(ListRequest{Sort: in.Sort, Cursor: in.Cursor, Limit: in.Limit})
	if err != nil {
		return Doc[ExplorePage]{}, err
	}
	pairs, err := parseTermPairs(in.Terms)
	if err != nil {
		return Doc[ExplorePage]{}, err
	}
	if in.Tag != "" && !validPublicSlug(in.Tag) {
		return Doc[ExplorePage]{}, badRequest("标签筛选无效")
	}
	window, since, err := windowSince(in.Window, s.now())
	if err != nil {
		return Doc[ExplorePage]{}, err
	}
	key := fmt.Sprintf("content:explore:%s:%s:%s:%s:%s:%s", sortBy, strings.Join(pairs, ","), in.Tag, window,
		boolKey(in.Finished), cursorKey(sortBy, pos, limit))
	return loadDoc(ctx, s.cache, key, exploreCacheTTL, func(ctx context.Context) (ExplorePage, error) {
		out := ExplorePage{List: []DramaCard{}}
		if pos.ID == 0 {
			facets, err := s.repo.FacetTaxonomies(ctx)
			if err != nil {
				return ExplorePage{}, err
			}
			out.Facets = &ExploreFacets{Taxonomies: orEmpty(facets)}
		}
		filter := PublicDramaFilter{Finished: in.Finished, Since: since}
		if len(pairs) > 0 {
			ids, err := s.repo.ResolveTermIDs(ctx, pairs)
			if err != nil {
				return ExplorePage{}, err
			}
			// 有取值不存在或已停用：没有剧能同时满足全部条件。
			if len(ids) != len(pairs) {
				return out, nil
			}
			filter.TermIDs = ids
		}
		if in.Tag != "" {
			tag, err := s.repo.PublicTagBySlug(ctx, in.Tag)
			if errors.Is(err, errs.ErrNotFound) {
				return out, nil
			}
			if err != nil {
				return ExplorePage{}, err
			}
			filter.TagID = tag.ID
			if tag.MergedIntoID != nil {
				filter.TagID = *tag.MergedIntoID
			}
		}
		if out.Total, err = s.repo.CountPublicDramas(ctx, filter); err != nil {
			return ExplorePage{}, err
		}
		cur, err := pos.dramaCursor()
		if err != nil {
			return ExplorePage{}, err
		}
		list, err := s.repo.PublicDramas(ctx, PublicDramaQuery{
			PublicDramaFilter: filter, Sort: sortBy, Seed: pos.Seed, Cursor: cur, Limit: limit,
		})
		if err != nil {
			return ExplorePage{}, err
		}
		out.List = orEmpty(list)
		out.NextCursor = nextCursor(sortBy, pos.Seed, out.List, limit)
		return out, nil
	})
}

// ---------- 专题 ----------

// Topics 返回专题列表的一页（已发布且列在列表页的专题）。
func (s *PublicService) Topics(ctx context.Context, in ListRequest) (Doc[TopicListPage], error) {
	limit, err := publicLimit(in.Limit)
	if err != nil {
		return Doc[TopicListPage]{}, err
	}
	pos, err := decodeCursorInput(in.Cursor, sortTopics)
	if err != nil {
		return Doc[TopicListPage]{}, err
	}
	key := fmt.Sprintf("content:topics:%d:%d", pos.ID, limit)
	return loadDoc(ctx, s.cache, key, topicCacheTTL, func(ctx context.Context) (TopicListPage, error) {
		topics, total, err := s.repo.PublicTopics(ctx, pos.ID, limit)
		if err != nil {
			return TopicListPage{}, err
		}
		out := TopicListPage{List: make([]TopicCard, 0, len(topics)), Total: total}
		for _, t := range topics {
			out.List = append(out.List, TopicCard{ID: t.ID, Title: t.Title, Slug: t.Slug, Summary: t.Summary})
		}
		out.NextCursor = nextTopicCursor(out.List, limit)
		return out, nil
	})
}

// Topic 返回专题详情。手选专题按后台顺序，自动专题按规则选剧；未发布时 404。
func (s *PublicService) Topic(ctx context.Context, slug string) (Doc[TopicDetailView], error) {
	if !validPublicSlug(slug) {
		return Doc[TopicDetailView]{}, errs.ErrNotFound
	}
	return loadDoc(ctx, s.cache, "content:topic:"+slug, topicCacheTTL, func(ctx context.Context) (TopicDetailView, error) {
		t, err := s.repo.PublicTopicBySlug(ctx, slug)
		if err != nil {
			return TopicDetailView{}, err
		}
		out := TopicDetailView{
			TopicCard:  TopicCard{ID: t.ID, Title: t.Title, Slug: t.Slug, Summary: t.Summary},
			SourceType: t.SourceType,
			FAQ:        make([]FAQView, 0, len(t.FAQ)),
			Dramas:     []DramaCard{},
		}
		for _, f := range t.FAQ {
			out.FAQ = append(out.FAQ, FAQView{Question: f.Question, Answer: f.Answer})
		}
		var dramas []DramaCard
		if t.SourceType == SourceAuto {
			dramas, err = s.autoTopicDramas(ctx, t.Rule)
		} else {
			dramas, err = s.repo.PublicTopicDramas(ctx, t.ID, manualTopicLimit)
		}
		if err != nil {
			return TopicDetailView{}, err
		}
		out.Dramas = orEmpty(dramas)
		return out, nil
	})
}

// PickDramas 按规则选剧：自动专题与首页区块（S3-1，domain/site）共用同一套规则引擎。
func (s *PublicService) PickDramas(ctx context.Context, rule TopicRule) ([]DramaCard, error) {
	return s.autoTopicDramas(ctx, rule)
}

// autoTopicDramas 执行自动专题的选剧规则（docs/02 第 2.6 节）：同时满足全部取值编号与标签，按规则排序取前 limit 部。
// 规则来自库里已校验过的数据，取值不在白名单时按默认值兜底而不是报错。
func (s *PublicService) autoTopicDramas(ctx context.Context, rule TopicRule) ([]DramaCard, error) {
	rule = rule.WithDefaults()
	sortBy, err := publicSort(rule.Sort, false)
	if err != nil {
		sortBy = SortLatest
	}
	_, since, err := windowSince(rule.Window, s.now())
	if err != nil {
		since = nil
	}
	limit := min(max(rule.Limit, 1), maxTopicLimit)

	filter := PublicDramaFilter{Since: since}
	if codes := slices.Compact(slices.Sorted(slices.Values(rule.TermCodes))); len(codes) > 0 {
		ids, err := s.repo.ResolveTermIDsByCodes(ctx, codes)
		if err != nil {
			return nil, err
		}
		// 规则引用的取值编号已被删除：没有剧能满足全部条件。
		if len(ids) != len(codes) {
			return []DramaCard{}, nil
		}
		filter.TermIDs = ids
	}
	if rule.TagID != nil {
		tag, err := s.repo.PublicTagByID(ctx, *rule.TagID)
		if errors.Is(err, errs.ErrNotFound) {
			return []DramaCard{}, nil
		}
		if err != nil {
			return nil, err
		}
		filter.TagID = tag.ID
		if tag.MergedIntoID != nil {
			filter.TagID = *tag.MergedIntoID
		}
	}
	return s.repo.PublicDramas(ctx, PublicDramaQuery{PublicDramaFilter: filter, Sort: sortBy, Limit: limit})
}

// ---------- 参数校验 ----------

// listParams 校验游标分页的三个参数：排序、每页条数、游标位置（random 的第一页在这里取当前种子）。
func (s *PublicService) listParams(in ListRequest) (DramaSort, int, cursorPos, error) {
	sortBy, err := publicSort(in.Sort, true)
	if err != nil {
		return "", 0, cursorPos{}, err
	}
	limit, err := publicLimit(in.Limit)
	if err != nil {
		return "", 0, cursorPos{}, err
	}
	pos, err := decodeCursorInput(in.Cursor, sortBy)
	if err != nil {
		return "", 0, cursorPos{}, err
	}
	// 随机排序：第一页取当前 10 分钟窗口的种子，后续页沿用游标里的种子，保证一轮翻页顺序一致。
	if sortBy == SortRandom && pos.ID == 0 {
		pos.Seed = randomSeed(s.now())
	}
	return sortBy, limit, pos, nil
}

func decodeCursorInput(cursor string, sortBy DramaSort) (cursorPos, error) {
	if len(cursor) > maxCursorLen {
		return cursorPos{}, errBadCursor
	}
	return decodeCursor(cursor, sortBy)
}

// publicSort 校验前台排序；空值为「最新」。只有游标分页的列表支持 random。
func publicSort(s DramaSort, allowRandom bool) (DramaSort, error) {
	switch s {
	case SortDefault, SortLatest:
		return SortLatest, nil
	case SortHot, SortRecommend:
		return s, nil
	case SortRandom:
		if allowRandom {
			return s, nil
		}
	}
	return "", badRequest("排序方式无效")
}

// windowSince 校验上新窗口，返回规范化后的窗口与起始时间（all 为 nil）。
func windowSince(w string, now time.Time) (string, *time.Time, error) {
	if w == "" || w == WindowAll {
		return WindowAll, nil, nil
	}
	days, ok := windowDays[w]
	if !ok {
		return "", nil, badRequest("上新窗口无效")
	}
	t := now.AddDate(0, 0, -days)
	return w, &t, nil
}

func publicLimit(n int) (int, error) {
	if n == 0 {
		return publicDefaultLimit, nil
	}
	if n < 1 || n > publicMaxLimit {
		return 0, badRequest("每页条数为 1–%d", publicMaxLimit)
	}
	return n, nil
}

func publicPage(page, pageSize int) (int, int, error) {
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = publicDefaultLimit
	}
	if page < 1 || page > publicMaxPage || pageSize < 1 || pageSize > publicMaxLimit {
		return 0, 0, badRequest("分页参数错误：page 为 1–%d，page_size 为 1–%d", publicMaxPage, publicMaxLimit)
	}
	return page, pageSize, nil
}

// parseTermPairs 解析探索的取值筛选：`维度slug:取值slug`，去重后排序，使缓存键与书写顺序无关。
func parseTermPairs(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > maxTermFilters {
		return nil, badRequest("最多只能选 %d 个取值", maxTermFilters)
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		taxonomy, term, ok := strings.Cut(strings.TrimSpace(p), ":")
		if !ok || !validPublicSlug(taxonomy) || !validPublicSlug(term) {
			return nil, badRequest("取值筛选的格式应为「维度:取值」")
		}
		pair := taxonomy + ":" + term
		if !slices.Contains(out, pair) {
			out = append(out, pair)
		}
	}
	slices.Sort(out)
	return out, nil
}

func validPublicSlug(s string) bool { return slugRe.MatchString(s) }

// cursorKey 是缓存键里的游标片段：用解析后的规范值，与客户端传来的字符串写法无关。
func cursorKey(sortBy DramaSort, pos cursorPos, limit int) string {
	if pos.ID == 0 && sortBy != SortRandom {
		return fmt.Sprintf("0:%d", limit)
	}
	return fmt.Sprintf("%d:%s:%d:%d", pos.Seed, pos.Value, pos.ID, limit)
}

func boolKey(b *bool) string {
	switch {
	case b == nil:
		return ""
	case *b:
		return "1"
	default:
		return "0"
	}
}

func cardOfDrama(d Drama) DramaCard {
	return DramaCard{
		ID: d.ID, Title: d.Title, Slug: d.Slug, CoverPortrait: d.CoverPortrait, CoverLandscape: d.CoverLandscape,
		Badge: d.Badge, Score: d.Score, Year: d.Year, IsFinished: d.IsFinished, LatestEpisode: d.LatestEpisode,
		PlayCount: d.ViewBase + d.ViewCount, PublishAt: d.PublishAt,
		CreatedAt: d.CreatedAt, HotScore: d.HotScore, SortWeight: d.SortWeight,
	}
}

// orEmpty 把 nil 切片换成空切片，响应里一律是 [] 而不是 null。
func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
