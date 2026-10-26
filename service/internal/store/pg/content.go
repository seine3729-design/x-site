package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// ContentRepo 实现 content.Repo。
type ContentRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewContentRepo(pool *pgxpool.Pool) *ContentRepo {
	return &ContentRepo{pool: pool, q: sqlcgen.New(pool)}
}

// 引用的记录在校验后被并发删除（外键失败）。
var errRefGone = errs.ErrBadRequest.WithMessage("引用的标签、取值或剧已不存在，请刷新后重试")

// dbErr 把查询错误映射为领域错误：查不到为 ErrNotFound，唯一键冲突按约束名区分，外键失败为 400；其他错误加上下文。
func dbErr(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505":
			switch pgErr.ConstraintName {
			case "drama_slug_key", "tag_slug_key", "taxonomy_slug_key", "topic_slug_key",
				"taxonomy_term_taxonomy_id_slug_key":
				return content.ErrSlugTaken
			case "taxonomy_term_channel_slug_key":
				return content.ErrChannelSlugTaken
			case "taxonomy_term_code_key":
				return content.ErrTermCodeTaken
			case "episode_drama_id_number_key":
				return content.ErrEpisodeNumberTaken
			}
			return errs.ErrConflict
		case "23503":
			return errRefGone
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

// ---------- 类型转换 ----------

func nullText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

func nullID(id int64) pgtype.Int8 { return pgtype.Int8{Int64: id, Valid: id != 0} }

func optInt8Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	x := v.Int64
	return &x
}

func tsOf(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// utcPtr 返回 UTC 时间，便于输出一致的 ISO 8601。
func utcPtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

// scoreParam 把评分（已保留一位小数）转为 numeric(2,1)。
func scoreParam(p *float64) pgtype.Numeric {
	if p == nil {
		return pgtype.Numeric{}
	}
	return pgtype.Numeric{Int: big.NewInt(int64(math.Round(*p * 10))), Exp: -1, Valid: true}
}

func scoreOf(n pgtype.Numeric) *float64 {
	if !n.Valid {
		return nil
	}
	f, err := n.Float64Value()
	if err != nil || !f.Valid {
		return nil
	}
	v := math.Round(f.Float64*10) / 10
	return &v
}

func int4Param(p *int32) pgtype.Int4 {
	if p == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *p, Valid: true}
}

func int4Of(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	x := v.Int32
	return &x
}

func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func dramaOf(r sqlcgen.Drama) content.Drama {
	return content.Drama{
		ID: r.ID, Title: r.Title, Aliases: strs(r.Aliases), Slug: r.Slug.String, Summary: r.Summary,
		CoverPortrait: r.CoverPortrait, CoverLandscape: r.CoverLandscape, Badge: r.Badge,
		Score: scoreOf(r.Score), Year: int4Of(r.Year), Status: content.DramaStatus(r.Status), PublishAt: utcPtr(r.PublishAt),
		IsFinished: r.IsFinished, SortWeight: r.SortWeight, ViewBase: r.ViewBase, ViewCount: r.ViewCount,
		HotScore: r.HotScore, LatestEpisode: r.LatestEpisode, SEOTitle: r.SeoTitle, SEODesc: r.SeoDesc, Source: r.Source,
		CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func dramasOf(rows []sqlcgen.Drama) []content.Drama {
	out := make([]content.Drama, len(rows))
	for i, r := range rows {
		out[i] = dramaOf(r)
	}
	return out
}

func episodeOf(r sqlcgen.Episode) content.Episode {
	return content.Episode{
		ID: r.ID, DramaID: r.DramaID, Number: r.Number, Title: r.Title, VideoObjectID: optInt8Ptr(r.VideoObjectID),
		Cover: r.Cover, DurationSec: r.DurationSec, Access: content.Access(r.Access), TrialSec: r.TrialSec,
		MediaStatus: r.MediaStatus, CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func tagOf(r sqlcgen.Tag) content.Tag {
	return content.Tag{
		ID: r.ID, Name: r.Name, Slug: r.Slug, IsHidden: r.IsHidden, Status: content.TagStatus(r.Status),
		MergedIntoID: optInt8Ptr(r.MergedIntoID), CreatedAt: r.CreatedAt.Time.UTC(),
	}
}

func tagsOf(rows []sqlcgen.Tag) []content.Tag {
	out := make([]content.Tag, len(rows))
	for i, r := range rows {
		out[i] = tagOf(r)
	}
	return out
}

func taxonomyOf(r sqlcgen.Taxonomy) content.Taxonomy {
	return content.Taxonomy{
		ID: r.ID, Name: r.Name, Slug: r.Slug, MultiSelect: r.MultiSelect, ShowInExplore: r.ShowInExplore,
		Status: content.ActiveStatus(r.Status), Sort: r.Sort, Terms: []content.Term{},
	}
}

func termOf(r sqlcgen.TaxonomyTerm) content.Term {
	return content.Term{
		ID: r.ID, TaxonomyID: r.TaxonomyID, Name: r.Name, Slug: r.Slug, Status: content.ActiveStatus(r.Status),
		IsChannel: r.IsChannel, Code: r.Code, Sort: r.Sort,
	}
}

func termsOf(rows []sqlcgen.TaxonomyTerm) []content.Term {
	out := make([]content.Term, len(rows))
	for i, r := range rows {
		out[i] = termOf(r)
	}
	return out
}

// 专题规则与 FAQ 在库中的 JSON 形态（topic.rule_json / faq_json）。
type ruleJSON struct {
	TermCodes []string `json:"term_codes"`
	TagID     *int64   `json:"tag_id"`
	Sort      string   `json:"sort"`
	Window    string   `json:"window"`
	Limit     int      `json:"limit"`
}

type faqJSON struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

func topicJSON(in content.TopicInput) (rule, faq []byte, err error) {
	r := in.Rule.WithDefaults()
	if rule, err = json.Marshal(ruleJSON{TermCodes: r.TermCodes, TagID: r.TagID, Sort: string(r.Sort), Window: r.Window, Limit: r.Limit}); err != nil {
		return nil, nil, fmt.Errorf("编码专题规则: %w", err)
	}
	items := make([]faqJSON, len(in.FAQ))
	for i, f := range in.FAQ {
		items[i] = faqJSON(f)
	}
	if faq, err = json.Marshal(items); err != nil {
		return nil, nil, fmt.Errorf("编码专题 FAQ: %w", err)
	}
	return rule, faq, nil
}

func topicOf(r sqlcgen.Topic) (content.Topic, error) {
	var rule ruleJSON
	if err := json.Unmarshal(r.RuleJson, &rule); err != nil {
		return content.Topic{}, fmt.Errorf("解析专题 #%d 的规则: %w", r.ID, err)
	}
	var items []faqJSON
	if err := json.Unmarshal(r.FaqJson, &items); err != nil {
		return content.Topic{}, fmt.Errorf("解析专题 #%d 的 FAQ: %w", r.ID, err)
	}
	faq := make([]content.FAQ, len(items))
	for i, f := range items {
		faq[i] = content.FAQ(f)
	}
	return content.Topic{
		ID: r.ID, Title: r.Title, Slug: r.Slug, Summary: r.Summary, Status: content.TopicStatus(r.Status),
		SourceType: content.SourceType(r.SourceType),
		Rule: content.TopicRule{
			TermCodes: rule.TermCodes, TagID: rule.TagID, Sort: content.DramaSort(rule.Sort), Window: rule.Window, Limit: rule.Limit,
		}.WithDefaults(),
		FAQ: faq, ListInIndex: r.ListInIndex, CreatedAt: r.CreatedAt.Time.UTC(),
	}, nil
}

// ---------- 剧集 ----------

func (r *ContentRepo) ListDramas(ctx context.Context, q content.DramaQuery) ([]content.Drama, int64, error) {
	status := nullText(string(q.Status))
	rows, err := r.q.ListDramas(ctx, sqlcgen.ListDramasParams{
		Pattern: nullText(q.Pattern), IDEq: nullID(q.IDEq), Status: status, TagID: nullID(q.TagID), TermID: nullID(q.TermID),
		Sort: string(q.Sort), PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取剧集列表: %w", err)
	}
	total, err := r.q.CountDramas(ctx, sqlcgen.CountDramasParams{
		Pattern: nullText(q.Pattern), IDEq: nullID(q.IDEq), Status: status, TagID: nullID(q.TagID), TermID: nullID(q.TermID),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("统计剧集: %w", err)
	}
	return dramasOf(rows), total, nil
}

func (r *ContentRepo) GetDrama(ctx context.Context, id int64) (content.Drama, error) {
	row, err := r.q.GetDrama(ctx, id)
	if err != nil {
		return content.Drama{}, dbErr(err, "读取剧集")
	}
	return dramaOf(row), nil
}

func (r *ContentRepo) CreateDrama(ctx context.Context, in content.DramaInput) (content.Drama, error) {
	row, err := r.q.CreateDrama(ctx, sqlcgen.CreateDramaParams{
		Title: in.Title, Aliases: strs(in.Aliases), Slug: nullText(in.Slug), Summary: in.Summary,
		CoverPortrait: in.CoverPortrait, CoverLandscape: in.CoverLandscape, Badge: in.Badge,
		Score: scoreParam(in.Score), Year: int4Param(in.Year), PublishAt: tsOf(in.PublishAt),
		IsFinished: in.IsFinished, SortWeight: in.SortWeight, ViewBase: in.ViewBase, SeoTitle: in.SEOTitle, SeoDesc: in.SEODesc,
	})
	if err != nil {
		return content.Drama{}, dbErr(err, "创建剧集")
	}
	return dramaOf(row), nil
}

func (r *ContentRepo) UpdateDrama(ctx context.Context, id int64, in content.DramaInput) (content.Drama, error) {
	row, err := r.q.UpdateDrama(ctx, sqlcgen.UpdateDramaParams{
		ID: id, Title: in.Title, Aliases: strs(in.Aliases), Slug: nullText(in.Slug), Summary: in.Summary,
		CoverPortrait: in.CoverPortrait, CoverLandscape: in.CoverLandscape, Badge: in.Badge,
		Score: scoreParam(in.Score), Year: int4Param(in.Year), Status: string(in.Status), PublishAt: tsOf(in.PublishAt),
		IsFinished: in.IsFinished, SortWeight: in.SortWeight, ViewBase: in.ViewBase, SeoTitle: in.SEOTitle, SeoDesc: in.SEODesc,
	})
	if err != nil {
		return content.Drama{}, dbErr(err, "修改剧集")
	}
	return dramaOf(row), nil
}

func (r *ContentRepo) SoftDeleteDrama(ctx context.Context, id int64) (content.Drama, error) {
	row, err := r.q.SoftDeleteDrama(ctx, id)
	if err != nil {
		return content.Drama{}, dbErr(err, "删除剧集")
	}
	return dramaOf(row), nil
}

func (r *ContentRepo) ListDramaTags(ctx context.Context, dramaID int64) ([]content.Tag, error) {
	rows, err := r.q.ListDramaTags(ctx, dramaID)
	if err != nil {
		return nil, fmt.Errorf("读取剧的标签: %w", err)
	}
	return tagsOf(rows), nil
}

func termRefOf(id, taxonomyID int64, taxonomyName string, multi bool, name, slug, status string) content.TermRef {
	return content.TermRef{
		ID: id, TaxonomyID: taxonomyID, TaxonomyName: taxonomyName, MultiSelect: multi, Name: name, Slug: slug,
		Status: content.ActiveStatus(status),
	}
}

func (r *ContentRepo) ListDramaTerms(ctx context.Context, dramaID int64) ([]content.TermRef, error) {
	rows, err := r.q.ListDramaTerms(ctx, dramaID)
	if err != nil {
		return nil, fmt.Errorf("读取剧的维度取值: %w", err)
	}
	out := make([]content.TermRef, len(rows))
	for i, x := range rows {
		out[i] = termRefOf(x.ID, x.TaxonomyID, x.TaxonomyName, x.MultiSelect, x.Name, x.Slug, x.Status)
	}
	return out, nil
}

// replaceDramaRelation 在事务内锁住剧（回收站中的剧返回 ErrNotFound），执行替换并更新剧的修改时间。
func (r *ContentRepo) replaceDramaRelation(ctx context.Context, dramaID int64, what string, fn func(q *sqlcgen.Queries) error) error {
	return InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockLiveDrama(ctx, dramaID); err != nil {
			return dbErr(err, "锁定剧集")
		}
		if err := fn(q); err != nil {
			return dbErr(err, what)
		}
		return dbErr(q.TouchDramas(ctx, []int64{dramaID}), "更新剧集修改时间")
	})
}

func (r *ContentRepo) ReplaceDramaTags(ctx context.Context, dramaID int64, tagIDs []int64) error {
	return r.replaceDramaRelation(ctx, dramaID, "保存剧的标签", func(q *sqlcgen.Queries) error {
		if err := q.DeleteDramaTags(ctx, dramaID); err != nil {
			return err
		}
		return q.InsertDramaTags(ctx, sqlcgen.InsertDramaTagsParams{DramaID: dramaID, TagIds: tagIDs})
	})
}

func (r *ContentRepo) ReplaceDramaTerms(ctx context.Context, dramaID int64, termIDs []int64) error {
	return r.replaceDramaRelation(ctx, dramaID, "保存剧的维度取值", func(q *sqlcgen.Queries) error {
		if err := q.DeleteDramaTerms(ctx, dramaID); err != nil {
			return err
		}
		return q.InsertDramaTerms(ctx, sqlcgen.InsertDramaTermsParams{DramaID: dramaID, TermIds: termIDs})
	})
}

// ---------- 批量 ----------

func (r *ContentRepo) BulkSetStatus(ctx context.Context, ids []int64, status content.DramaStatus) (int64, error) {
	n, err := r.q.BulkSetDramaStatus(ctx, sqlcgen.BulkSetDramaStatusParams{Ids: ids, Status: string(status)})
	if err != nil {
		return 0, fmt.Errorf("批量修改剧集状态: %w", err)
	}
	return n, nil
}

// distinct 返回去重后的 ID（保留首次出现的顺序）；批量操作的 RETURNING 中同一部剧可能出现多次。
func distinct(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (r *ContentRepo) BulkAddTags(ctx context.Context, ids, tagIDs []int64) (int64, error) {
	var affected []int64
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		changed, err := q.BulkAddDramaTags(ctx, sqlcgen.BulkAddDramaTagsParams{DramaIds: ids, TagIds: tagIDs})
		if err != nil {
			return dbErr(err, "批量追加标签")
		}
		affected = distinct(changed)
		return dbErr(q.TouchDramas(ctx, affected), "更新剧集修改时间")
	})
	return int64(len(affected)), err
}

func (r *ContentRepo) BulkAddToTopic(ctx context.Context, topicID int64, ids []int64) (int64, error) {
	var added []int64
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockTopic(ctx, topicID); err != nil {
			return dbErr(err, "锁定专题")
		}
		var err error
		added, err = q.AppendTopicDramas(ctx, sqlcgen.AppendTopicDramasParams{TopicID: topicID, DramaIds: ids})
		return dbErr(err, "加入专题")
	})
	return int64(len(added)), err
}

func (r *ContentRepo) BulkSetTerms(ctx context.Context, ids, termIDs, singleTaxonomyIDs []int64) (int64, error) {
	var affected []int64
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		live, err := q.LiveDramaIDs(ctx, ids)
		if err != nil {
			return dbErr(err, "读取剧集")
		}
		var changed []int64
		if len(singleTaxonomyIDs) > 0 {
			removed, err := q.DeleteOtherDramaTerms(ctx, sqlcgen.DeleteOtherDramaTermsParams{
				DramaIds: live, TaxonomyIds: singleTaxonomyIDs, KeepTermIds: termIDs,
			})
			if err != nil {
				return dbErr(err, "替换单选维度的取值")
			}
			changed = append(changed, removed...)
		}
		inserted, err := q.BulkAddDramaTerms(ctx, sqlcgen.BulkAddDramaTermsParams{DramaIds: live, TermIds: termIDs})
		if err != nil {
			return dbErr(err, "批量设置取值")
		}
		affected = distinct(append(changed, inserted...))
		return dbErr(q.TouchDramas(ctx, affected), "更新剧集修改时间")
	})
	return int64(len(affected)), err
}

// ---------- 引用校验 ----------

func (r *ContentRepo) LiveDramaIDs(ctx context.Context, ids []int64) ([]int64, error) {
	out, err := r.q.LiveDramaIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("读取剧集: %w", err)
	}
	return out, nil
}

func (r *ContentRepo) TagsByIDs(ctx context.Context, ids []int64) ([]content.Tag, error) {
	rows, err := r.q.GetTagsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("读取标签: %w", err)
	}
	return tagsOf(rows), nil
}

func (r *ContentRepo) TermsByIDs(ctx context.Context, ids []int64) ([]content.TermRef, error) {
	rows, err := r.q.GetTermsByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("读取取值: %w", err)
	}
	out := make([]content.TermRef, len(rows))
	for i, x := range rows {
		out[i] = termRefOf(x.ID, x.TaxonomyID, x.TaxonomyName, x.MultiSelect, x.Name, x.Slug, x.Status)
	}
	return out, nil
}

func (r *ContentRepo) ExistingTermCodes(ctx context.Context, codes []string) ([]string, error) {
	out, err := r.q.ExistingTermCodes(ctx, codes)
	if err != nil {
		return nil, fmt.Errorf("读取取值编号: %w", err)
	}
	return out, nil
}

// ---------- 分集 ----------

func (r *ContentRepo) ListEpisodes(ctx context.Context, dramaID int64) ([]content.Episode, error) {
	rows, err := r.q.ListEpisodes(ctx, dramaID)
	if err != nil {
		return nil, fmt.Errorf("读取分集: %w", err)
	}
	out := make([]content.Episode, len(rows))
	for i, x := range rows {
		out[i] = episodeOf(x)
	}
	return out, nil
}

func (r *ContentRepo) GetEpisode(ctx context.Context, id int64) (content.Episode, error) {
	row, err := r.q.GetEpisode(ctx, id)
	if err != nil {
		return content.Episode{}, dbErr(err, "读取分集")
	}
	return episodeOf(row), nil
}

// episodeTx 在事务内锁住剧（回收站中的剧返回 ErrNotFound），执行分集修改后刷新「更新至 N 集」。
func (r *ContentRepo) episodeTx(ctx context.Context, dramaID int64, fn func(q *sqlcgen.Queries) (sqlcgen.Episode, error)) (content.Episode, error) {
	var ep sqlcgen.Episode
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockLiveDrama(ctx, dramaID); err != nil {
			return dbErr(err, "锁定剧集")
		}
		var err error
		if ep, err = fn(q); err != nil {
			return dbErr(err, "保存分集")
		}
		return dbErr(q.RefreshLatestEpisode(ctx, dramaID), "刷新最新集号")
	})
	if err != nil {
		return content.Episode{}, err
	}
	return episodeOf(ep), nil
}

func (r *ContentRepo) CreateEpisode(ctx context.Context, dramaID int64, in content.EpisodeInput) (content.Episode, error) {
	return r.episodeTx(ctx, dramaID, func(q *sqlcgen.Queries) (sqlcgen.Episode, error) {
		return q.CreateEpisode(ctx, sqlcgen.CreateEpisodeParams{
			DramaID: dramaID, Number: in.Number, Title: in.Title, Cover: in.Cover, DurationSec: in.DurationSec,
			Access: string(in.Access), TrialSec: in.TrialSec,
		})
	})
}

func (r *ContentRepo) UpdateEpisode(ctx context.Context, id int64, in content.EpisodeInput) (content.Episode, error) {
	dramaID, err := r.q.GetEpisodeDramaID(ctx, id)
	if err != nil {
		return content.Episode{}, dbErr(err, "读取分集")
	}
	return r.episodeTx(ctx, dramaID, func(q *sqlcgen.Queries) (sqlcgen.Episode, error) {
		return q.UpdateEpisode(ctx, sqlcgen.UpdateEpisodeParams{
			ID: id, Number: in.Number, Title: in.Title, Cover: in.Cover, DurationSec: in.DurationSec,
			Access: string(in.Access), TrialSec: in.TrialSec,
		})
	})
}

func (r *ContentRepo) DeleteEpisode(ctx context.Context, id int64) (content.Episode, error) {
	dramaID, err := r.q.GetEpisodeDramaID(ctx, id)
	if err != nil {
		return content.Episode{}, dbErr(err, "读取分集")
	}
	return r.episodeTx(ctx, dramaID, func(q *sqlcgen.Queries) (sqlcgen.Episode, error) {
		return q.DeleteEpisode(ctx, id)
	})
}

// ---------- 标签 ----------

func (r *ContentRepo) ListTags(ctx context.Context, q content.TagQuery) ([]content.Tag, int64, error) {
	pattern, status := nullText(q.Pattern), nullText(string(q.Status))
	var merged pgtype.Bool
	if q.Merged != nil {
		merged = pgtype.Bool{Bool: *q.Merged, Valid: true}
	}
	rows, err := r.q.ListTags(ctx, sqlcgen.ListTagsParams{
		Pattern: pattern, Status: status, Merged: merged, PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取标签列表: %w", err)
	}
	total, err := r.q.CountTags(ctx, sqlcgen.CountTagsParams{Pattern: pattern, Status: status, Merged: merged})
	if err != nil {
		return nil, 0, fmt.Errorf("统计标签: %w", err)
	}
	return tagsOf(rows), total, nil
}

func (r *ContentRepo) GetTag(ctx context.Context, id int64) (content.Tag, error) {
	row, err := r.q.GetTag(ctx, id)
	if err != nil {
		return content.Tag{}, dbErr(err, "读取标签")
	}
	return tagOf(row), nil
}

func (r *ContentRepo) CreateTag(ctx context.Context, in content.TagInput) (content.Tag, error) {
	row, err := r.q.CreateTag(ctx, sqlcgen.CreateTagParams{Name: in.Name, Slug: in.Slug, IsHidden: in.IsHidden, Status: string(in.Status)})
	if err != nil {
		return content.Tag{}, dbErr(err, "创建标签")
	}
	return tagOf(row), nil
}

func (r *ContentRepo) UpdateTag(ctx context.Context, id int64, in content.TagInput) (content.Tag, error) {
	row, err := r.q.UpdateTag(ctx, sqlcgen.UpdateTagParams{ID: id, Name: in.Name, Slug: in.Slug, IsHidden: in.IsHidden, Status: string(in.Status)})
	if err != nil {
		return content.Tag{}, dbErr(err, "修改标签")
	}
	return tagOf(row), nil
}

func (r *ContentRepo) MergeTag(ctx context.Context, sourceID, targetID int64) (int64, error) {
	var moved int64
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		locked, err := q.LockTagsForMerge(ctx, []int64{sourceID, targetID})
		if err != nil {
			return dbErr(err, "锁定标签")
		}
		if len(locked) != 2 {
			return errs.ErrNotFound
		}
		for _, t := range locked {
			if t.MergedIntoID.Valid {
				return content.ErrStale
			}
		}
		if err := q.CopyTagDramas(ctx, sqlcgen.CopyTagDramasParams{SourceID: sourceID, TargetID: targetID}); err != nil {
			return dbErr(err, "把剧挂到目标标签")
		}
		if moved, err = q.DeleteTagDramas(ctx, sourceID); err != nil {
			return dbErr(err, "移除源标签的剧")
		}
		return dbErr(q.MergeTagInto(ctx, sqlcgen.MergeTagIntoParams{SourceID: sourceID, TargetID: targetID}), "合并标签")
	})
	return moved, err
}

// ---------- 维度与取值 ----------

func (r *ContentRepo) ListTaxonomies(ctx context.Context) ([]content.Taxonomy, error) {
	rows, err := r.q.ListTaxonomies(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取维度: %w", err)
	}
	terms, err := r.q.ListAllTerms(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取取值: %w", err)
	}
	out := make([]content.Taxonomy, len(rows))
	index := make(map[int64]int, len(rows))
	for i, x := range rows {
		out[i] = taxonomyOf(x)
		index[x.ID] = i
	}
	for _, t := range terms {
		if i, ok := index[t.TaxonomyID]; ok {
			out[i].Terms = append(out[i].Terms, termOf(t))
		}
	}
	return out, nil
}

func (r *ContentRepo) GetTaxonomy(ctx context.Context, id int64) (content.Taxonomy, error) {
	row, err := r.q.GetTaxonomy(ctx, id)
	if err != nil {
		return content.Taxonomy{}, dbErr(err, "读取维度")
	}
	terms, err := r.q.ListTerms(ctx, id)
	if err != nil {
		return content.Taxonomy{}, fmt.Errorf("读取取值: %w", err)
	}
	t := taxonomyOf(row)
	t.Terms = termsOf(terms)
	return t, nil
}

func (r *ContentRepo) CreateTaxonomy(ctx context.Context, in content.TaxonomyInput) (content.Taxonomy, error) {
	row, err := r.q.CreateTaxonomy(ctx, sqlcgen.CreateTaxonomyParams{
		Name: in.Name, Slug: in.Slug, MultiSelect: in.MultiSelect, ShowInExplore: in.ShowInExplore, Status: string(in.Status),
	})
	if err != nil {
		return content.Taxonomy{}, dbErr(err, "创建维度")
	}
	return taxonomyOf(row), nil
}

func (r *ContentRepo) UpdateTaxonomy(ctx context.Context, id int64, in content.TaxonomyInput) (content.Taxonomy, error) {
	row, err := r.q.UpdateTaxonomy(ctx, sqlcgen.UpdateTaxonomyParams{
		ID: id, Name: in.Name, Slug: in.Slug, MultiSelect: in.MultiSelect, ShowInExplore: in.ShowInExplore, Status: string(in.Status),
	})
	if err != nil {
		return content.Taxonomy{}, dbErr(err, "修改维度")
	}
	t := taxonomyOf(row)
	terms, err := r.q.ListTerms(ctx, id)
	if err != nil {
		return content.Taxonomy{}, fmt.Errorf("读取取值: %w", err)
	}
	t.Terms = termsOf(terms)
	return t, nil
}

func (r *ContentRepo) DeleteTaxonomy(ctx context.Context, id int64) (content.Taxonomy, error) {
	var out content.Taxonomy
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		terms, err := q.ListTerms(ctx, id)
		if err != nil {
			return dbErr(err, "读取取值")
		}
		row, err := q.DeleteTaxonomy(ctx, id)
		if err != nil {
			return dbErr(err, "删除维度")
		}
		out = taxonomyOf(row)
		out.Terms = termsOf(terms)
		return nil
	})
	return out, err
}

func (r *ContentRepo) CountMultiTermDramas(ctx context.Context, taxonomyID int64) (int64, error) {
	n, err := r.q.CountMultiTermDramas(ctx, taxonomyID)
	if err != nil {
		return 0, fmt.Errorf("统计多选的剧: %w", err)
	}
	return n, nil
}

func (r *ContentRepo) TaxonomyIDs(ctx context.Context) ([]int64, error) {
	ids, err := r.q.TaxonomyIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取维度: %w", err)
	}
	return ids, nil
}

func (r *ContentRepo) SetTaxonomySort(ctx context.Context, ids []int64) error {
	if err := r.q.SetTaxonomySort(ctx, ids); err != nil {
		return fmt.Errorf("维度排序: %w", err)
	}
	return nil
}

func (r *ContentRepo) GetTerm(ctx context.Context, taxonomyID, termID int64) (content.Term, error) {
	row, err := r.q.GetTerm(ctx, sqlcgen.GetTermParams{ID: termID, TaxonomyID: taxonomyID})
	if err != nil {
		return content.Term{}, dbErr(err, "读取取值")
	}
	return termOf(row), nil
}

func (r *ContentRepo) CreateTerm(ctx context.Context, taxonomyID int64, in content.TermInput) (content.Term, error) {
	row, err := r.q.CreateTerm(ctx, sqlcgen.CreateTermParams{
		TaxonomyID: taxonomyID, Name: in.Name, Slug: in.Slug, Status: string(in.Status), IsChannel: in.IsChannel, Code: in.Code,
	})
	if err != nil {
		return content.Term{}, dbErr(err, "创建取值")
	}
	return termOf(row), nil
}

func (r *ContentRepo) UpdateTerm(ctx context.Context, taxonomyID, termID int64, in content.TermInput) (content.Term, error) {
	row, err := r.q.UpdateTerm(ctx, sqlcgen.UpdateTermParams{
		ID: termID, TaxonomyID: taxonomyID, Name: in.Name, Slug: in.Slug, Status: string(in.Status), IsChannel: in.IsChannel, Code: in.Code,
	})
	if err != nil {
		return content.Term{}, dbErr(err, "修改取值")
	}
	return termOf(row), nil
}

func (r *ContentRepo) DeleteTerm(ctx context.Context, taxonomyID, termID int64) (content.Term, error) {
	row, err := r.q.DeleteTerm(ctx, sqlcgen.DeleteTermParams{ID: termID, TaxonomyID: taxonomyID})
	if err != nil {
		return content.Term{}, dbErr(err, "删除取值")
	}
	return termOf(row), nil
}

func (r *ContentRepo) TermIDs(ctx context.Context, taxonomyID int64) ([]int64, error) {
	ids, err := r.q.TermIDs(ctx, taxonomyID)
	if err != nil {
		return nil, fmt.Errorf("读取取值: %w", err)
	}
	return ids, nil
}

func (r *ContentRepo) SetTermSort(ctx context.Context, taxonomyID int64, ids []int64) error {
	if err := r.q.SetTermSort(ctx, sqlcgen.SetTermSortParams{TaxonomyID: taxonomyID, Ids: ids}); err != nil {
		return fmt.Errorf("取值排序: %w", err)
	}
	return nil
}

// ---------- 专题 ----------

func (r *ContentRepo) ListTopics(ctx context.Context, q content.TopicQuery) ([]content.Topic, int64, error) {
	pattern, status, source := nullText(q.Pattern), nullText(string(q.Status)), nullText(string(q.SourceType))
	rows, err := r.q.ListTopics(ctx, sqlcgen.ListTopicsParams{
		Pattern: pattern, Status: status, SourceType: source, PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取专题列表: %w", err)
	}
	total, err := r.q.CountTopics(ctx, sqlcgen.CountTopicsParams{Pattern: pattern, Status: status, SourceType: source})
	if err != nil {
		return nil, 0, fmt.Errorf("统计专题: %w", err)
	}
	out := make([]content.Topic, len(rows))
	for i, x := range rows {
		if out[i], err = topicOf(x); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

func (r *ContentRepo) GetTopic(ctx context.Context, id int64) (content.Topic, error) {
	row, err := r.q.GetTopic(ctx, id)
	if err != nil {
		return content.Topic{}, dbErr(err, "读取专题")
	}
	return topicOf(row)
}

func (r *ContentRepo) CreateTopic(ctx context.Context, in content.TopicInput) (content.Topic, error) {
	rule, faq, err := topicJSON(in)
	if err != nil {
		return content.Topic{}, err
	}
	row, err := r.q.CreateTopic(ctx, sqlcgen.CreateTopicParams{
		Title: in.Title, Slug: in.Slug, Summary: in.Summary, Status: string(in.Status), SourceType: string(in.SourceType),
		RuleJson: rule, FaqJson: faq, ListInIndex: in.ListInIndex,
	})
	if err != nil {
		return content.Topic{}, dbErr(err, "创建专题")
	}
	return topicOf(row)
}

func (r *ContentRepo) UpdateTopic(ctx context.Context, id int64, in content.TopicInput) (content.Topic, error) {
	rule, faq, err := topicJSON(in)
	if err != nil {
		return content.Topic{}, err
	}
	row, err := r.q.UpdateTopic(ctx, sqlcgen.UpdateTopicParams{
		ID: id, Title: in.Title, Slug: in.Slug, Summary: in.Summary, Status: string(in.Status), SourceType: string(in.SourceType),
		RuleJson: rule, FaqJson: faq, ListInIndex: in.ListInIndex,
	})
	if err != nil {
		return content.Topic{}, dbErr(err, "修改专题")
	}
	return topicOf(row)
}

func (r *ContentRepo) DeleteTopic(ctx context.Context, id int64) (content.Topic, error) {
	row, err := r.q.DeleteTopic(ctx, id)
	if err != nil {
		return content.Topic{}, dbErr(err, "删除专题")
	}
	return topicOf(row)
}

func (r *ContentRepo) ListTopicDramas(ctx context.Context, topicID int64) ([]content.TopicDrama, error) {
	rows, err := r.q.ListTopicDramas(ctx, topicID)
	if err != nil {
		return nil, fmt.Errorf("读取专题的剧: %w", err)
	}
	out := make([]content.TopicDrama, len(rows))
	for i, x := range rows {
		out[i] = content.TopicDrama{
			ID: x.ID, Title: x.Title, Status: content.DramaStatus(x.Status), PublishAt: utcPtr(x.PublishAt), CoverPortrait: x.CoverPortrait,
		}
	}
	return out, nil
}

func (r *ContentRepo) ReplaceTopicDramas(ctx context.Context, topicID int64, dramaIDs []int64) error {
	sorts := make([]int32, len(dramaIDs))
	for i := range sorts {
		sorts[i] = int32(i)
	}
	return InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if _, err := q.LockTopic(ctx, topicID); err != nil {
			return dbErr(err, "锁定专题")
		}
		if err := q.DeleteLiveTopicDramas(ctx, topicID); err != nil {
			return dbErr(err, "清除专题的剧")
		}
		return dbErr(q.InsertTopicDramas(ctx, sqlcgen.InsertTopicDramasParams{TopicID: topicID, DramaIds: dramaIDs, Sorts: sorts}), "保存专题的剧")
	})
}

var _ content.Repo = (*ContentRepo)(nil)
