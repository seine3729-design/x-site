package content

import (
	"context"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
	"xsite/service/internal/textnorm"
)

// 搜索（docs/03 第 3.3 节，S3-4，docs/specs/S3-4.md）。
// 结果只有剧集；匹配用 pg_bigm 的 LIKE（剧名或别名），排序沿用前台列表的白名单、默认最热。
// 相关性打分等迁到 Meilisearch 再做，MVP 不做。

const (
	searchCacheTTL  = 60 * time.Second
	suggestCacheTTL = 300 * time.Second
	suggestMaxItems = 10
	// searchSuggestFeature 是 site_config.features 里控制联想的开关，关闭时联想接口 404。
	searchSuggestFeature = "search_suggest"
)

// SearchRequest 是一次搜索。Sort 为空时按最热。
type SearchRequest struct {
	Q      string
	Sort   DramaSort
	Cursor string
	Limit  int
}

// SearchPage 是搜索结果的一页。Query 是规范化后的搜索词（前台可用它回显）。
type SearchPage struct {
	Query      string      `json:"query"`
	List       []DramaCard `json:"list"`
	NextCursor *string     `json:"next_cursor"`
	Total      int64       `json:"total"`
}

// SuggestItem 是一条联想结果。前台剧详情按编号访问（/d/:id），不需要 slug。
type SuggestItem struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// SuggestPage 是联想结果。
type SuggestPage struct {
	Query string        `json:"query"`
	List  []SuggestItem `json:"list"`
}

// SearchRepo 是搜索用到的只读查询（由 store/pg.PublicContentRepo 实现）。
type SearchRepo interface {
	PublicDramas(ctx context.Context, q PublicDramaQuery) ([]DramaCard, error)
	CountPublicDramas(ctx context.Context, f PublicDramaFilter) (int64, error)
	// SuggestDramas 返回剧名或别名命中的可见剧，剧名以搜索词开头的排在前面，其余按热度。
	SuggestDramas(ctx context.Context, pattern, prefix string, limit int) ([]SuggestItem, error)
}

// SearchRecorder 记一次搜索（实现见 domain/ops.HotsearchService.Record；由 app 层注入，避免内容域依赖运营域）。
type SearchRecorder interface {
	Record(ctx context.Context, query string, hits int) error
}

// FeatureFlags 读站点功能开关（site_config.features）；没设置的开关视为打开。
type FeatureFlags interface {
	Enabled(ctx context.Context, name string) (bool, error)
}

// SearchService 是前台搜索与联想。
type SearchService struct {
	repo     SearchRepo
	cache    Cache
	features FeatureFlags
	recorder SearchRecorder
}

// NewSearchService 创建搜索服务。
func NewSearchService(repo SearchRepo, cache Cache, features FeatureFlags, recorder SearchRecorder) *SearchService {
	return &SearchService{repo: repo, cache: cache, features: features, recorder: recorder}
}

// Search 返回搜索结果的一页，并把这次搜索计入热搜（只在第一页计，翻页不重复）。
func (s *SearchService) Search(ctx context.Context, in SearchRequest) (Doc[SearchPage], error) {
	q, err := searchWord(in.Q)
	if err != nil {
		return Doc[SearchPage]{}, err
	}
	if in.Sort == SortDefault {
		in.Sort = SortHot // 搜索默认按热度：没有相关性打分时热度是最合理的代理
	}
	sortBy, err := publicSort(in.Sort, false)
	if err != nil {
		return Doc[SearchPage]{}, err
	}
	limit, err := publicLimit(in.Limit)
	if err != nil {
		return Doc[SearchPage]{}, err
	}
	pos, err := decodeCursorInput(in.Cursor, sortBy)
	if err != nil {
		return Doc[SearchPage]{}, err
	}

	// 键里必须带排序：cursorKey 对第一页只给「0:条数」，不带排序的话换 sort 会读到上一种排序的缓存
	key := fmt.Sprintf("content:search:%s:%s:%s", q, sortBy, cursorKey(sortBy, pos, limit))
	doc, err := loadDoc(ctx, s.cache, key, searchCacheTTL, func(ctx context.Context) (SearchPage, error) {
		out := SearchPage{Query: q, List: []DramaCard{}}
		filter := PublicDramaFilter{Pattern: likePattern(q)}
		if out.Total, err = s.repo.CountPublicDramas(ctx, filter); err != nil {
			return SearchPage{}, err
		}
		cur, err := pos.dramaCursor()
		if err != nil {
			return SearchPage{}, err
		}
		list, err := s.repo.PublicDramas(ctx, PublicDramaQuery{
			PublicDramaFilter: filter, Sort: sortBy, Cursor: cur, Limit: limit,
		})
		if err != nil {
			return SearchPage{}, err
		}
		out.List = orEmpty(list)
		out.NextCursor = nextCursor(sortBy, pos.Seed, out.List, limit)
		return out, nil
	})
	if err != nil {
		return Doc[SearchPage]{}, err
	}
	// 命中缓存也要计数：用户确实搜了这一次。计数失败不影响结果（最多少统计一次）。
	if pos.ID == 0 && s.recorder != nil {
		if err := s.recorder.Record(ctx, q, int(doc.Payload.Total)); err != nil {
			slog.WarnContext(ctx, "热搜计数失败", "query", q, "err", err)
		}
	}
	return doc, nil
}

// Suggest 返回剧名联想。features.search_suggest 关闭时 404（前台据此隐藏实时联想）。
// 联想不计入热搜：输入过程中的半截词会污染热搜榜与「片库没有」的选品依据。
func (s *SearchService) Suggest(ctx context.Context, q string, limit int) (Doc[SuggestPage], error) {
	on, err := s.features.Enabled(ctx, searchSuggestFeature)
	if err != nil {
		return Doc[SuggestPage]{}, err
	}
	if !on {
		return Doc[SuggestPage]{}, errs.ErrNotFound
	}
	word, err := searchWord(q)
	if err != nil {
		return Doc[SuggestPage]{}, err
	}
	if limit == 0 {
		limit = suggestMaxItems
	}
	if limit < 1 || limit > suggestMaxItems {
		return Doc[SuggestPage]{}, badRequest("联想条数为 1–%d", suggestMaxItems)
	}
	key := fmt.Sprintf("content:suggest:%s:%d", word, limit)
	return loadDoc(ctx, s.cache, key, suggestCacheTTL, func(ctx context.Context) (SuggestPage, error) {
		list, err := s.repo.SuggestDramas(ctx, likePattern(word), prefixPattern(word), limit)
		if err != nil {
			return SuggestPage{}, err
		}
		return SuggestPage{Query: word, List: orEmpty(list)}, nil
	})
}

// searchWord 规范化并校验搜索词：与热搜计数用同一套规范化（internal/textnorm）。
func searchWord(q string) (string, error) {
	word := textnorm.Search(q)
	if word == "" {
		return "", badRequest("请输入搜索词")
	}
	if utf8.RuneCountInString(word) > maxQueryRunes {
		return "", badRequest("搜索词不能超过 %d 个字", maxQueryRunes)
	}
	return word, nil
}

// prefixPattern 把搜索词转为前缀匹配的 LIKE 模式（转义 \ % _）。
func prefixPattern(q string) string {
	if q == "" {
		return ""
	}
	return likeEscaper.Replace(q) + "%"
}
