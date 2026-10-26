package content

import (
	"time"

	dom "xsite/service/internal/domain/content"
)

// ---------- 通用 ----------

// pageDTO 是后台列表的一页（页码分页，docs/specs/S1-3.md）。
type pageDTO[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

func pageOf[S, T any](p dom.Page[S], f func(S) T) pageDTO[T] {
	return pageDTO[T]{List: mapSlice(p.List, f), Total: p.Total, Page: p.Page, PageSize: p.PageSize}
}

// listDTO 是一次返回全部的列表（分集、维度）。
type listDTO[T any] struct {
	List  []T `json:"list"`
	Total int `json:"total"`
}

func listOf[S, T any](items []S, f func(S) T) listDTO[T] {
	out := mapSlice(items, f)
	return listDTO[T]{List: out, Total: len(out)}
}

// mapSlice 转换切片；nil 也返回空数组，JSON 输出 [] 而不是 null。
func mapSlice[S, T any](items []S, f func(S) T) []T {
	out := make([]T, len(items))
	for i, it := range items {
		out[i] = f(it)
	}
	return out
}

func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func ids(s []int64) []int64 {
	if s == nil {
		return []int64{}
	}
	return s
}

// ---------- 剧集 ----------

type dramaDTO struct {
	ID             int64      `json:"id"`
	Title          string     `json:"title"`
	Aliases        []string   `json:"aliases"`
	Slug           string     `json:"slug"`
	Summary        string     `json:"summary"`
	CoverPortrait  string     `json:"cover_portrait"`
	CoverLandscape string     `json:"cover_landscape"`
	Badge          string     `json:"badge"`
	Score          *float64   `json:"score"`
	Year           *int32     `json:"year"`
	Status         string     `json:"status"`
	DisplayStatus  string     `json:"display_status"`
	PublishAt      *time.Time `json:"publish_at"`
	IsFinished     bool       `json:"is_finished"`
	SortWeight     int32      `json:"sort_weight"`
	ViewBase       int64      `json:"view_base"`
	ViewCount      int64      `json:"view_count"`
	HotScore       int64      `json:"hot_score"`
	LatestEpisode  int32      `json:"latest_episode"`
	SEOTitle       string     `json:"seo_title"`
	SEODesc        string     `json:"seo_desc"`
	Source         string     `json:"source"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func dramaOf(now time.Time) func(dom.Drama) dramaDTO {
	return func(d dom.Drama) dramaDTO {
		return dramaDTO{
			ID: d.ID, Title: d.Title, Aliases: strs(d.Aliases), Slug: d.Slug, Summary: d.Summary,
			CoverPortrait: d.CoverPortrait, CoverLandscape: d.CoverLandscape, Badge: d.Badge, Score: d.Score, Year: d.Year,
			Status: string(d.Status), DisplayStatus: string(d.DisplayStatus(now)), PublishAt: d.PublishAt,
			IsFinished: d.IsFinished, SortWeight: d.SortWeight, ViewBase: d.ViewBase, ViewCount: d.ViewCount,
			HotScore: d.HotScore, LatestEpisode: d.LatestEpisode, SEOTitle: d.SEOTitle, SEODesc: d.SEODesc, Source: d.Source,
			CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
		}
	}
}

type dramaTermDTO struct {
	ID           int64  `json:"id"`
	TaxonomyID   int64  `json:"taxonomy_id"`
	TaxonomyName string `json:"taxonomy_name"`
	MultiSelect  bool   `json:"multi_select"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Status       string `json:"status"`
}

func dramaTermOf(t dom.TermRef) dramaTermDTO {
	return dramaTermDTO{
		ID: t.ID, TaxonomyID: t.TaxonomyID, TaxonomyName: t.TaxonomyName, MultiSelect: t.MultiSelect,
		Name: t.Name, Slug: t.Slug, Status: string(t.Status),
	}
}

type dramaDetailDTO struct {
	dramaDTO
	Tags  []tagDTO       `json:"tags"`
	Terms []dramaTermDTO `json:"terms"`
}

// ---------- 分集 ----------

type episodeDTO struct {
	ID            int64     `json:"id"`
	DramaID       int64     `json:"drama_id"`
	Number        int32     `json:"number"`
	Title         string    `json:"title"`
	Cover         string    `json:"cover"`
	DurationSec   int32     `json:"duration_sec"`
	Access        string    `json:"access"`
	TrialSec      int32     `json:"trial_sec"`
	MediaStatus   string    `json:"media_status"`
	VideoObjectID *int64    `json:"video_object_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func episodeOf(e dom.Episode) episodeDTO {
	return episodeDTO{
		ID: e.ID, DramaID: e.DramaID, Number: e.Number, Title: e.Title, Cover: e.Cover, DurationSec: e.DurationSec,
		Access: string(e.Access), TrialSec: e.TrialSec, MediaStatus: e.MediaStatus, VideoObjectID: e.VideoObjectID,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
}

// ---------- 标签 ----------

type tagDTO struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	IsHidden     bool      `json:"is_hidden"`
	Status       string    `json:"status"`
	MergedIntoID *int64    `json:"merged_into_id"`
	CreatedAt    time.Time `json:"created_at"`
}

func tagOf(t dom.Tag) tagDTO {
	return tagDTO{
		ID: t.ID, Name: t.Name, Slug: t.Slug, IsHidden: t.IsHidden, Status: string(t.Status),
		MergedIntoID: t.MergedIntoID, CreatedAt: t.CreatedAt,
	}
}

// ---------- 维度与取值 ----------

type termDTO struct {
	ID         int64  `json:"id"`
	TaxonomyID int64  `json:"taxonomy_id"`
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Status     string `json:"status"`
	IsChannel  bool   `json:"is_channel"`
	Code       string `json:"code"`
	Sort       int32  `json:"sort"`
}

func termOf(t dom.Term) termDTO {
	return termDTO{
		ID: t.ID, TaxonomyID: t.TaxonomyID, Name: t.Name, Slug: t.Slug, Status: string(t.Status),
		IsChannel: t.IsChannel, Code: t.Code, Sort: t.Sort,
	}
}

type taxonomyDTO struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	MultiSelect   bool      `json:"multi_select"`
	ShowInExplore bool      `json:"show_in_explore"`
	Status        string    `json:"status"`
	Sort          int32     `json:"sort"`
	Terms         []termDTO `json:"terms"`
}

func taxonomyOf(t dom.Taxonomy) taxonomyDTO {
	return taxonomyDTO{
		ID: t.ID, Name: t.Name, Slug: t.Slug, MultiSelect: t.MultiSelect, ShowInExplore: t.ShowInExplore,
		Status: string(t.Status), Sort: t.Sort, Terms: mapSlice(t.Terms, termOf),
	}
}

// ---------- 专题 ----------

type ruleDTO struct {
	TermCodes []string `json:"term_codes"`
	TagID     *int64   `json:"tag_id"`
	Sort      string   `json:"sort"`
	Window    string   `json:"window"`
	Limit     int      `json:"limit"`
}

type faqDTO struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type topicDTO struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Slug        string    `json:"slug"`
	Summary     string    `json:"summary"`
	Status      string    `json:"status"`
	SourceType  string    `json:"source_type"`
	Rule        ruleDTO   `json:"rule"`
	FAQ         []faqDTO  `json:"faq"`
	ListInIndex bool      `json:"list_in_index"`
	CreatedAt   time.Time `json:"created_at"`
}

func topicOf(t dom.Topic) topicDTO {
	r := t.Rule.WithDefaults()
	return topicDTO{
		ID: t.ID, Title: t.Title, Slug: t.Slug, Summary: t.Summary, Status: string(t.Status), SourceType: string(t.SourceType),
		Rule:        ruleDTO{TermCodes: strs(r.TermCodes), TagID: r.TagID, Sort: string(r.Sort), Window: r.Window, Limit: r.Limit},
		FAQ:         mapSlice(t.FAQ, func(f dom.FAQ) faqDTO { return faqDTO(f) }),
		ListInIndex: t.ListInIndex, CreatedAt: t.CreatedAt,
	}
}

type topicDramaDTO struct {
	ID            int64      `json:"id"`
	Title         string     `json:"title"`
	Status        string     `json:"status"`
	DisplayStatus string     `json:"display_status"`
	PublishAt     *time.Time `json:"publish_at"`
	CoverPortrait string     `json:"cover_portrait"`
}

func topicDramaOf(now time.Time) func(dom.TopicDrama) topicDramaDTO {
	return func(d dom.TopicDrama) topicDramaDTO {
		display := dom.Drama{Status: d.Status, PublishAt: d.PublishAt}.DisplayStatus(now)
		return topicDramaDTO{
			ID: d.ID, Title: d.Title, Status: string(d.Status), DisplayStatus: string(display),
			PublishAt: d.PublishAt, CoverPortrait: d.CoverPortrait,
		}
	}
}

type topicDetailDTO struct {
	topicDTO
	Dramas []topicDramaDTO `json:"dramas"`
}
