package pg

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/store/pg/sqlcgen"
)

// PublicContentRepo 实现 content.PublicRepo（S1-4 前台只读查询，docs/specs/S1-4.md）。
type PublicContentRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewPublicContentRepo(pool *pgxpool.Pool) *PublicContentRepo {
	return &PublicContentRepo{pool: pool, q: sqlcgen.New(pool)}
}

// dramaVisible 是前台可见性（docs/03 第 3.1 节）：不在回收站、已上架、定时已到点。
const dramaVisible = `d.deleted_at IS NULL AND d.status = 'published' AND (d.publish_at IS NULL OR d.publish_at <= now())`

// dramaCardColumns 与 scanDramaCards、db/queries/content_public.sql 里的卡片列表保持一致。
const dramaCardColumns = `d.id, d.title, d.slug, d.cover_portrait, d.cover_landscape, d.badge, d.score, d.year,
	d.is_finished, d.latest_episode, (d.view_base + d.view_count)::bigint, d.publish_at, d.created_at,
	d.hot_score, d.sort_weight`

// ---------- 剧详情 ----------

func (r *PublicContentRepo) PublicDrama(ctx context.Context, id int64) (content.Drama, error) {
	row, err := r.q.GetPublicDrama(ctx, id)
	if err != nil {
		return content.Drama{}, dbErr(err, "读取剧集")
	}
	return dramaOf(row), nil
}

func (r *PublicContentRepo) PublicDramaTags(ctx context.Context, dramaID int64) ([]content.Tag, error) {
	rows, err := r.q.ListPublicDramaTags(ctx, dramaID)
	if err != nil {
		return nil, dbErr(err, "读取剧的标签")
	}
	out := make([]content.Tag, 0, len(rows))
	for _, t := range rows {
		out = append(out, tagOf(t))
	}
	return out, nil
}

func (r *PublicContentRepo) PublicDramaTerms(ctx context.Context, dramaID int64) ([]content.TermView, error) {
	rows, err := r.q.ListPublicDramaTerms(ctx, dramaID)
	if err != nil {
		return nil, dbErr(err, "读取剧的维度取值")
	}
	out := make([]content.TermView, 0, len(rows))
	for _, t := range rows {
		out = append(out, content.TermView{
			ID: t.ID, Name: t.Name, Slug: t.Slug, Code: t.Code, IsChannel: t.IsChannel,
			TaxonomyID: t.TaxonomyID, TaxonomyName: t.TaxonomyName, TaxonomySlug: t.TaxonomySlug,
		})
	}
	return out, nil
}

func (r *PublicContentRepo) PublicEpisodes(ctx context.Context, dramaID int64) ([]content.EpisodeView, error) {
	rows, err := r.q.ListPublicEpisodes(ctx, dramaID)
	if err != nil {
		return nil, dbErr(err, "读取分集")
	}
	out := make([]content.EpisodeView, 0, len(rows))
	for _, e := range rows {
		out = append(out, content.EpisodeView{
			ID: e.ID, Number: e.Number, Title: e.Title, Cover: e.Cover, DurationSec: e.DurationSec,
			Access: content.Access(e.Access), TrialSec: e.TrialSec, Playable: e.Playable,
		})
	}
	return out, nil
}

// relatedPoolLimit 是相关推荐每个召回源的候选上限（按编号倒序，新剧优先）。
const relatedPoolLimit = 400

func (r *PublicContentRepo) RelatedDramas(ctx context.Context, excludeID int64, tagIDs, termIDs []int64, limit int) ([]content.DramaCard, error) {
	rows, err := r.q.ListRelatedDramas(ctx, sqlcgen.ListRelatedDramasParams{
		TagIds: idsOrEmpty(tagIDs), TermIds: idsOrEmpty(termIDs), ExcludeID: excludeID,
		PoolLimit: relatedPoolLimit, PageLimit: int32(limit),
	})
	if err != nil {
		return nil, dbErr(err, "读取相关推荐")
	}
	out := make([]content.DramaCard, 0, len(rows))
	for _, d := range rows {
		out = append(out, cardOf(d.ID, d.Title, d.Slug, d.CoverPortrait, d.CoverLandscape, d.Badge, d.Score,
			d.Year, d.IsFinished, d.LatestEpisode, d.PlayCount, d.PublishAt, d.CreatedAt, d.HotScore, d.SortWeight))
	}
	return out, nil
}

// ---------- 列表 ----------

// PublicDramas 取一页剧卡片。排序与游标比较都来自白名单（见 dramaOrderExpr），参数一律用占位符。
func (r *PublicContentRepo) PublicDramas(ctx context.Context, q content.PublicDramaQuery) ([]content.DramaCard, error) {
	sql, args, err := publicDramaSQL(q, false)
	if err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, dbErr(err, "读取剧集列表")
	}
	cards, err := scanDramaCards(rows)
	if err != nil {
		return nil, dbErr(err, "读取剧集列表")
	}
	return cards, nil
}

func (r *PublicContentRepo) CountPublicDramas(ctx context.Context, f content.PublicDramaFilter) (int64, error) {
	sql, args, err := publicDramaSQL(content.PublicDramaQuery{PublicDramaFilter: f}, true)
	if err != nil {
		return 0, err
	}
	var total int64
	if err := r.pool.QueryRow(ctx, sql, args...).Scan(&total); err != nil {
		return 0, dbErr(err, "统计剧集数量")
	}
	return total, nil
}

// publicDramaSQL 按筛选与排序拼出前台剧列表查询。
// 不用 sqlc 的 CASE 排序：那种写法用不上 (status, 排序列, id) 复合索引，而前台列表是最热的查询。
// 排序表达式取自固定白名单，请求里的值全部走占位符。
func publicDramaSQL(q content.PublicDramaQuery, countOnly bool) (string, []any, error) {
	var b strings.Builder
	args := make([]any, 0, 8)
	add := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}

	if countOnly {
		b.WriteString("SELECT count(*) FROM drama d WHERE ")
	} else {
		b.WriteString("SELECT ")
		b.WriteString(dramaCardColumns)
		b.WriteString(" FROM drama d WHERE ")
	}
	b.WriteString(dramaVisible)

	if q.Finished != nil {
		b.WriteString(" AND d.is_finished = " + add(*q.Finished))
	}
	if q.Since != nil {
		b.WriteString(" AND coalesce(d.publish_at, d.created_at) >= " + add(*q.Since))
	}
	// 搜索：剧名或别名命中，大小写不敏感。必须按 lower(…) 写才能用上迁移 00035 的两条 pg_bigm 索引
	// （ILIKE 用不上索引，实测见该迁移的注释）。模式由领域层转小写并转义。
	if q.Pattern != "" {
		p := add(q.Pattern)
		b.WriteString(" AND (lower(d.title) LIKE " + p + " OR lower(aliases_text(d.aliases)) LIKE " + p + ")")
	}
	if q.TagID != 0 {
		b.WriteString(" AND EXISTS (SELECT 1 FROM drama_tag dt WHERE dt.drama_id = d.id AND dt.tag_id = " + add(q.TagID) + ")")
	}
	// 全部取值都要命中（与自动专题规则一致）。逐个 EXISTS 而不是一次 count 比较：
	// EXISTS 能走半连接 + drama_term_term_idx，取值最多 8 个，SQL 不会变长太多。
	for _, id := range q.TermIDs {
		b.WriteString(" AND EXISTS (SELECT 1 FROM drama_term dm WHERE dm.drama_id = d.id AND dm.term_id = " + add(id) + ")")
	}
	if countOnly {
		return b.String(), args, nil
	}

	order, err := dramaOrderExpr(q.Sort, q.Seed, add)
	if err != nil {
		return "", nil, err
	}
	if q.Cursor.ID != 0 {
		val, err := cursorValue(q.Sort, q.Cursor)
		if err != nil {
			return "", nil, err
		}
		b.WriteString(" AND (" + order + ", d.id) < (" + add(val) + ", " + add(q.Cursor.ID) + ")")
	}
	b.WriteString(" ORDER BY " + order + " DESC, d.id DESC LIMIT " + add(int32(q.Limit)))
	if q.Offset > 0 {
		b.WriteString(" OFFSET " + add(int32(q.Offset)))
	}
	return b.String(), args, nil
}

// dramaOrderExpr 返回排序列表达式。随机排序的种子也用占位符传入，表达式本身不含请求内容。
func dramaOrderExpr(sort content.DramaSort, seed int64, add func(any) string) (string, error) {
	switch sort {
	case content.SortHot:
		return "d.hot_score", nil
	case content.SortRecommend:
		return "d.sort_weight", nil
	case content.SortRandom:
		// 与领域层的 randomKey 一致：md5(编号 || '|' || 种子)。种子按 10 分钟窗口变化。
		return "md5(d.id::text || '|' || " + add(strconv.FormatInt(seed, 10)) + ")", nil
	case content.SortLatest, content.SortDefault:
		return "coalesce(d.publish_at, d.created_at)", nil
	}
	return "", fmt.Errorf("未支持的排序: %q", sort)
}

// cursorValue 返回游标比较用的值，类型要与排序列一致（sort_weight 是 int4）。
func cursorValue(sort content.DramaSort, c content.DramaCursor) (any, error) {
	switch sort {
	case content.SortHot:
		return c.Num, nil
	case content.SortRecommend:
		return int32(c.Num), nil
	case content.SortRandom:
		return c.Text, nil
	case content.SortLatest, content.SortDefault:
		return c.At, nil
	}
	return nil, fmt.Errorf("未支持的排序: %q", sort)
}

func scanDramaCards(rows pgx.Rows) ([]content.DramaCard, error) {
	defer rows.Close()
	out := []content.DramaCard{}
	for rows.Next() {
		var (
			id                   int64
			title                string
			slug                 pgtype.Text
			coverPortrait        string
			coverLandscape       string
			badge                string
			score                pgtype.Numeric
			year                 pgtype.Int4
			isFinished           bool
			latestEpisode        int32
			playCount, hotScore  int64
			publishAt, createdAt pgtype.Timestamptz
			sortWeight           int32
		)
		if err := rows.Scan(&id, &title, &slug, &coverPortrait, &coverLandscape, &badge, &score, &year,
			&isFinished, &latestEpisode, &playCount, &publishAt, &createdAt, &hotScore, &sortWeight); err != nil {
			return nil, err
		}
		out = append(out, cardOf(id, title, slug, coverPortrait, coverLandscape, badge, score, year, isFinished,
			latestEpisode, playCount, publishAt, createdAt, hotScore, sortWeight))
	}
	return out, rows.Err()
}

// cardOf 把一行卡片列转为领域模型；sqlc 生成的行结构与手写查询共用它。
func cardOf(id int64, title string, slug pgtype.Text, coverPortrait, coverLandscape, badge string,
	score pgtype.Numeric, year pgtype.Int4, isFinished bool, latestEpisode int32, playCount int64,
	publishAt, createdAt pgtype.Timestamptz, hotScore int64, sortWeight int32,
) content.DramaCard {
	return content.DramaCard{
		ID: id, Title: title, Slug: slug.String, CoverPortrait: coverPortrait, CoverLandscape: coverLandscape,
		Badge: badge, Score: scoreOf(score), Year: int4Of(year), IsFinished: isFinished,
		LatestEpisode: latestEpisode, PlayCount: playCount, PublishAt: utcPtr(publishAt),
		CreatedAt: createdAt.Time.UTC(), HotScore: hotScore, SortWeight: sortWeight,
	}
}

// SuggestDramas 返回剧名联想（S3-4）：剧名以搜索词开头的排在前面，其余按热度。
func (r *PublicContentRepo) SuggestDramas(ctx context.Context, pattern, prefix string, limit int) ([]content.SuggestItem, error) {
	rows, err := r.q.SuggestPublicDramas(ctx, sqlcgen.SuggestPublicDramasParams{
		Pattern: pattern, Prefix: prefix, PageLimit: int32(limit),
	})
	if err != nil {
		return nil, dbErr(err, "读取搜索联想")
	}
	out := make([]content.SuggestItem, len(rows))
	for i, row := range rows {
		out[i] = content.SuggestItem{ID: row.ID, Title: row.Title}
	}
	return out, nil
}

// ---------- 频道、标签、分面 ----------

func (r *PublicContentRepo) PublicChannel(ctx context.Context, slug string) (content.ChannelView, error) {
	row, err := r.q.GetPublicChannel(ctx, slug)
	if err != nil {
		return content.ChannelView{}, dbErr(err, "读取频道")
	}
	return content.ChannelView{
		ID: row.ID, Name: row.Name, Slug: row.Slug, Code: row.Code,
		TaxonomyID: row.TaxonomyID, TaxonomyName: row.TaxonomyName, TaxonomySlug: row.TaxonomySlug,
	}, nil
}

func (r *PublicContentRepo) PublicTagBySlug(ctx context.Context, slug string) (content.Tag, error) {
	row, err := r.q.GetPublicTagBySlug(ctx, slug)
	if err != nil {
		return content.Tag{}, dbErr(err, "读取标签")
	}
	return tagOf(row), nil
}

func (r *PublicContentRepo) PublicTagByID(ctx context.Context, id int64) (content.Tag, error) {
	row, err := r.q.GetPublicTagByID(ctx, id)
	if err != nil {
		return content.Tag{}, dbErr(err, "读取标签")
	}
	return tagOf(row), nil
}

func (r *PublicContentRepo) FacetTaxonomies(ctx context.Context) ([]content.FacetTaxonomy, error) {
	rows, err := r.q.ListExploreFacetTerms(ctx)
	if err != nil {
		return nil, dbErr(err, "读取筛选条")
	}
	out := []content.FacetTaxonomy{}
	for _, t := range rows {
		if len(out) == 0 || out[len(out)-1].ID != t.TaxonomyID {
			out = append(out, content.FacetTaxonomy{
				ID: t.TaxonomyID, Name: t.TaxonomyName, Slug: t.TaxonomySlug,
				MultiSelect: t.MultiSelect, Terms: []content.FacetTerm{},
			})
		}
		last := &out[len(out)-1]
		last.Terms = append(last.Terms, content.FacetTerm{
			ID: t.ID, Name: t.Name, Slug: t.Slug, Code: t.Code, IsChannel: t.IsChannel,
		})
	}
	return out, nil
}

func (r *PublicContentRepo) ResolveTermIDs(ctx context.Context, pairs []string) ([]int64, error) {
	ids, err := r.q.ResolveTermIDs(ctx, pairs)
	if err != nil {
		return nil, dbErr(err, "解析取值筛选")
	}
	return ids, nil
}

func (r *PublicContentRepo) ResolveTermIDsByCodes(ctx context.Context, codes []string) ([]int64, error) {
	ids, err := r.q.ResolveTermIDsByCodes(ctx, codes)
	if err != nil {
		return nil, dbErr(err, "解析取值编号")
	}
	return ids, nil
}

// ---------- 专题 ----------

func (r *PublicContentRepo) PublicTopics(ctx context.Context, cursorID int64, limit int) ([]content.Topic, int64, error) {
	rows, err := r.q.ListPublicTopics(ctx, sqlcgen.ListPublicTopicsParams{CursorID: cursorID, PageLimit: int32(limit)})
	if err != nil {
		return nil, 0, dbErr(err, "读取专题列表")
	}
	total, err := r.q.CountPublicTopics(ctx)
	if err != nil {
		return nil, 0, dbErr(err, "统计专题数量")
	}
	out := make([]content.Topic, 0, len(rows))
	for _, t := range rows {
		topic, err := topicOf(t)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, topic)
	}
	return out, total, nil
}

func (r *PublicContentRepo) PublicTopicBySlug(ctx context.Context, slug string) (content.Topic, error) {
	row, err := r.q.GetPublicTopic(ctx, slug)
	if err != nil {
		return content.Topic{}, dbErr(err, "读取专题")
	}
	return topicOf(row)
}

func (r *PublicContentRepo) PublicTopicDramas(ctx context.Context, topicID int64, limit int) ([]content.DramaCard, error) {
	rows, err := r.q.ListPublicTopicDramas(ctx, sqlcgen.ListPublicTopicDramasParams{
		TopicID: topicID, PageLimit: int32(limit),
	})
	if err != nil {
		return nil, dbErr(err, "读取专题的剧")
	}
	out := make([]content.DramaCard, 0, len(rows))
	for _, d := range rows {
		out = append(out, cardOf(d.ID, d.Title, d.Slug, d.CoverPortrait, d.CoverLandscape, d.Badge, d.Score,
			d.Year, d.IsFinished, d.LatestEpisode, d.PlayCount, d.PublishAt, d.CreatedAt, d.HotScore, d.SortWeight))
	}
	return out, nil
}

func idsOrEmpty(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}
