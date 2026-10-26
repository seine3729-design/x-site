package content

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"xsite/service/internal/errs"
)

// Service 是后台内容管理（docs/06 第 4、5、7、11、14 节）。权限点由 HTTP 层判定，这里只管业务规则。
type Service struct {
	repo Repo
	now  func() time.Time
}

// NewService 创建内容服务。
func NewService(repo Repo) *Service {
	return &Service{repo: repo, now: time.Now}
}

// 自动生成 slug 的前缀：标签 t、维度 f、取值 v、专题 s。
const (
	slugPrefixTag      = "t"
	slugPrefixTaxonomy = "f"
	slugPrefixTerm     = "v"
	slugPrefixTopic    = "s"
)

const (
	msgTagMerged    = "标签「%s」已并入 #%d，请改用合并后的标签"
	msgSingleSelect = "「%s」是单选维度，只能选一个取值"
)

var errAutoTopicDramas = errs.ErrConflict.WithMessage("自动专题按规则选剧，不能手选；请先把剧来源改为手选")

// ---------- 剧集 ----------

// ListDramas 返回后台剧集列表的一页（不含回收站中的剧）。
func (s *Service) ListDramas(ctx context.Context, f DramaFilter) (Page[Drama], error) {
	q, err := cleanQuery(f.Q)
	if err != nil {
		return Page[Drama]{}, err
	}
	switch f.Status {
	case "", DramaDraft, DramaPublished, DramaScheduled, DramaOffline:
	default:
		return Page[Drama]{}, badRequest("状态筛选无效")
	}
	switch f.Sort {
	case SortDefault, SortLatest, SortUpdated, SortHot, SortRecommend:
	default:
		return Page[Drama]{}, badRequest("排序方式无效")
	}
	if f.TagID < 0 || f.TermID < 0 {
		return Page[Drama]{}, badRequest("筛选条件无效")
	}
	p, limit, offset, err := limitOffset(f.PageRequest)
	if err != nil {
		return Page[Drama]{}, err
	}
	pattern, id := parseDramaQuery(q)
	list, total, err := s.repo.ListDramas(ctx, DramaQuery{
		Pattern: pattern, IDEq: id, Status: f.Status, TagID: f.TagID, TermID: f.TermID, Sort: f.Sort,
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return Page[Drama]{}, err
	}
	return Page[Drama]{List: list, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// GetDrama 返回剧资料与标签、维度取值。回收站中的剧返回 404。
func (s *Service) GetDrama(ctx context.Context, id int64) (DramaDetail, error) {
	d, err := s.repo.GetDrama(ctx, id)
	if err != nil {
		return DramaDetail{}, err
	}
	out := DramaDetail{Drama: d}
	if out.Tags, err = s.repo.ListDramaTags(ctx, id); err != nil {
		return DramaDetail{}, err
	}
	if out.Terms, err = s.repo.ListDramaTerms(ctx, id); err != nil {
		return DramaDetail{}, err
	}
	return out, nil
}

// CreateDrama 新建草稿。
func (s *Service) CreateDrama(ctx context.Context, in DramaInput) (Drama, error) {
	in, err := s.cleanDrama(in, false)
	if err != nil {
		return Drama{}, err
	}
	return s.repo.CreateDrama(ctx, in)
}

// UpdateDrama 整体保存剧资料，返回修改前后的记录。
func (s *Service) UpdateDrama(ctx context.Context, id int64, in DramaInput) (before, after Drama, err error) {
	if in, err = s.cleanDrama(in, true); err != nil {
		return
	}
	if before, err = s.repo.GetDrama(ctx, id); err != nil {
		return
	}
	after, err = s.repo.UpdateDrama(ctx, id, in)
	return
}

// DeleteDrama 把剧放进回收站（30 天内可在回收站还原，S4-7），返回删除前的记录。
func (s *Service) DeleteDrama(ctx context.Context, id int64) (Drama, error) {
	return s.repo.SoftDeleteDrama(ctx, id)
}

// cleanDrama 校验并规范化剧资料。新建时状态固定为草稿；保存为已上架且没有上架时间时写入当前时间。
func (s *Service) cleanDrama(in DramaInput, update bool) (DramaInput, error) {
	var err error
	if in.Title, err = cleanText("剧名", in.Title, 200, true); err != nil {
		return in, err
	}
	if in.Aliases, err = cleanList("别名", in.Aliases, 20, 100); err != nil {
		return in, err
	}
	if in.Slug, err = cleanSlug(in.Slug); err != nil {
		return in, err
	}
	fields := []struct {
		label string
		value *string
		max   int
	}{
		{"简介", &in.Summary, 5000},
		{"竖封面", &in.CoverPortrait, 500},
		{"横封面", &in.CoverLandscape, 500},
		{"角标", &in.Badge, 16},
		{"SEO 标题", &in.SEOTitle, 200},
		{"SEO 描述", &in.SEODesc, 500},
	}
	for _, f := range fields {
		if *f.value, err = cleanText(f.label, *f.value, f.max, false); err != nil {
			return in, err
		}
	}
	if in.Score != nil {
		// numeric(2,1)：保留一位小数后 0–9.9
		tenths := math.Round(*in.Score * 10)
		if !(tenths >= 0 && tenths <= 99) {
			return in, badRequest("评分需在 0–9.9 之间")
		}
		if tenths == 0 {
			tenths = 0 // -0.04 四舍五入得到 -0，统一为 0
		}
		v := tenths / 10
		in.Score = &v
	}
	if in.Year != nil && (*in.Year < 1900 || *in.Year > 2100) {
		return in, badRequest("年份需在 1900–2100 之间")
	}
	if in.ViewBase < 0 {
		return in, badRequest("初始播放量不能为负数")
	}
	if !update {
		in.Status = DramaDraft
		return in, nil
	}
	switch in.Status {
	case DramaDraft, DramaOffline:
	case DramaPublished:
		if in.PublishAt == nil {
			now := s.now()
			in.PublishAt = &now
		}
	default:
		return in, badRequest("状态只能是草稿、已上架或已下架")
	}
	return in, nil
}

// SetDramaTags 整体替换剧的标签，返回替换前后的标签。不能选已并入其他标签的标签。
func (s *Service) SetDramaTags(ctx context.Context, id int64, tagIDs []int64) (before, after []Tag, err error) {
	if tagIDs, err = uniqueIDs("标签", tagIDs, 100); err != nil {
		return
	}
	if _, err = s.repo.GetDrama(ctx, id); err != nil {
		return
	}
	if err = s.checkTags(ctx, tagIDs); err != nil {
		return
	}
	if before, err = s.repo.ListDramaTags(ctx, id); err != nil {
		return
	}
	if err = s.repo.ReplaceDramaTags(ctx, id, tagIDs); err != nil {
		return
	}
	after, err = s.repo.ListDramaTags(ctx, id)
	return
}

// SetDramaTerms 整体替换剧的维度取值，返回替换前后的取值。单选维度最多一个取值。
func (s *Service) SetDramaTerms(ctx context.Context, id int64, termIDs []int64) (before, after []TermRef, err error) {
	if termIDs, err = uniqueIDs("取值", termIDs, 100); err != nil {
		return
	}
	if _, err = s.repo.GetDrama(ctx, id); err != nil {
		return
	}
	if _, err = s.checkTerms(ctx, termIDs); err != nil {
		return
	}
	if before, err = s.repo.ListDramaTerms(ctx, id); err != nil {
		return
	}
	if err = s.repo.ReplaceDramaTerms(ctx, id, termIDs); err != nil {
		return
	}
	after, err = s.repo.ListDramaTerms(ctx, id)
	return
}

// BulkDramas 执行批量操作，返回受影响的剧数。不存在或在回收站中的剧忽略。
func (s *Service) BulkDramas(ctx context.Context, in BulkInput) (int64, error) {
	ids, err := uniqueIDs("剧集", in.IDs, maxBulkIDs)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, badRequest("请选择剧集")
	}
	switch in.Action {
	case BulkPublish:
		return s.repo.BulkSetStatus(ctx, ids, DramaPublished)
	case BulkOffline:
		return s.repo.BulkSetStatus(ctx, ids, DramaOffline)
	case BulkDraft:
		return s.repo.BulkSetStatus(ctx, ids, DramaDraft)
	case BulkAddTags:
		tagIDs, err := uniqueIDs("标签", in.TagIDs, 50)
		if err != nil {
			return 0, err
		}
		if len(tagIDs) == 0 {
			return 0, badRequest("请选择标签")
		}
		if err := s.checkTags(ctx, tagIDs); err != nil {
			return 0, err
		}
		return s.repo.BulkAddTags(ctx, ids, tagIDs)
	case BulkAddToTopic:
		if in.TopicID <= 0 {
			return 0, badRequest("请选择专题")
		}
		t, err := s.repo.GetTopic(ctx, in.TopicID)
		if errors.Is(err, errs.ErrNotFound) {
			return 0, badRequest("专题 #%d 不存在", in.TopicID)
		}
		if err != nil {
			return 0, err
		}
		if t.SourceType != SourceManual {
			return 0, badRequest("只能加入手选专题")
		}
		return s.repo.BulkAddToTopic(ctx, in.TopicID, ids)
	case BulkSetTerms:
		termIDs, err := uniqueIDs("取值", in.TermIDs, 50)
		if err != nil {
			return 0, err
		}
		if len(termIDs) == 0 {
			return 0, badRequest("请选择取值")
		}
		terms, err := s.checkTerms(ctx, termIDs)
		if err != nil {
			return 0, err
		}
		var singles []int64
		for _, t := range terms {
			if !t.MultiSelect {
				singles = append(singles, t.TaxonomyID)
			}
		}
		return s.repo.BulkSetTerms(ctx, ids, termIDs, singles)
	default:
		return 0, badRequest("批量操作无效")
	}
}

// checkTags 确认标签都存在且未被合并。
func (s *Service) checkTags(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tags, err := s.repo.TagsByIDs(ctx, ids)
	if err != nil {
		return err
	}
	found := make([]int64, len(tags))
	for i, t := range tags {
		found[i] = t.ID
		if t.MergedIntoID != nil {
			return badRequest(msgTagMerged, t.Name, *t.MergedIntoID)
		}
	}
	if m := missing(ids, found); m != 0 {
		return badRequest("标签 #%d 不存在", m)
	}
	return nil
}

// checkTerms 确认取值都存在，且每个单选维度最多一个；返回这些取值。
func (s *Service) checkTerms(ctx context.Context, ids []int64) ([]TermRef, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	terms, err := s.repo.TermsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	found := make([]int64, len(terms))
	perTaxonomy := map[int64]int{}
	for i, t := range terms {
		found[i] = t.ID
		perTaxonomy[t.TaxonomyID]++
		if !t.MultiSelect && perTaxonomy[t.TaxonomyID] > 1 {
			return nil, badRequest(msgSingleSelect, t.TaxonomyName)
		}
	}
	if m := missing(ids, found); m != 0 {
		return nil, badRequest("取值 #%d 不存在", m)
	}
	return terms, nil
}

// ---------- 分集 ----------

// ListEpisodes 返回一部剧的全部分集（按集号）。
func (s *Service) ListEpisodes(ctx context.Context, dramaID int64) ([]Episode, error) {
	if _, err := s.repo.GetDrama(ctx, dramaID); err != nil {
		return nil, err
	}
	return s.repo.ListEpisodes(ctx, dramaID)
}

// CreateEpisode 新增分集。同一部剧内集号重复返回 409。
func (s *Service) CreateEpisode(ctx context.Context, dramaID int64, in EpisodeInput) (Episode, error) {
	in, err := cleanEpisode(in)
	if err != nil {
		return Episode{}, err
	}
	ep, err := s.repo.CreateEpisode(ctx, dramaID, in)
	return ep, episodeConflict(err, in.Number)
}

// UpdateEpisode 整体保存分集，返回修改前后的记录。
func (s *Service) UpdateEpisode(ctx context.Context, id int64, in EpisodeInput) (before, after Episode, err error) {
	if in, err = cleanEpisode(in); err != nil {
		return
	}
	if before, err = s.repo.GetEpisode(ctx, id); err != nil {
		return
	}
	after, err = s.repo.UpdateEpisode(ctx, id, in)
	err = episodeConflict(err, in.Number)
	return
}

// DeleteEpisode 删除分集（不进回收站），返回删除前的记录。
func (s *Service) DeleteEpisode(ctx context.Context, id int64) (Episode, error) {
	return s.repo.DeleteEpisode(ctx, id)
}

func episodeConflict(err error, number int32) error {
	if IsErr(err, ErrEpisodeNumberTaken) {
		return errs.ErrConflict.WithMessage(fmt.Sprintf("第 %d 集已存在", number))
	}
	return err
}

func cleanEpisode(in EpisodeInput) (EpisodeInput, error) {
	var err error
	if in.Number < 1 || in.Number > 100000 {
		return in, badRequest("集号需为 1–100000 的整数")
	}
	if in.Title, err = cleanText("标题", in.Title, 200, false); err != nil {
		return in, err
	}
	if in.Cover, err = cleanText("封面", in.Cover, 500, false); err != nil {
		return in, err
	}
	if in.DurationSec < 0 || in.DurationSec > 86400 {
		return in, badRequest("时长需在 0–86400 秒之间")
	}
	switch in.Access {
	case "":
		in.Access = AccessFree
	case AccessFree, AccessLogin, AccessVIP:
	default:
		return in, badRequest("谁能看只能是免费、登录可看或 VIP")
	}
	if in.TrialSec < 0 || in.TrialSec > 86400 {
		return in, badRequest("可试看秒数需在 0–86400 之间")
	}
	if in.DurationSec > 0 && in.TrialSec > in.DurationSec {
		return in, badRequest("可试看秒数不能超过时长")
	}
	return in, nil
}

// ---------- 标签 ----------

// ListTags 返回标签列表的一页（按编号倒序）。
func (s *Service) ListTags(ctx context.Context, f TagFilter) (Page[Tag], error) {
	q, err := cleanQuery(f.Q)
	if err != nil {
		return Page[Tag]{}, err
	}
	if f.Status != "" && f.Status != TagActive && f.Status != TagHidden {
		return Page[Tag]{}, badRequest("状态筛选无效")
	}
	p, limit, offset, err := limitOffset(f.PageRequest)
	if err != nil {
		return Page[Tag]{}, err
	}
	list, total, err := s.repo.ListTags(ctx, TagQuery{Pattern: likePattern(q), Status: f.Status, Merged: f.Merged, Limit: limit, Offset: offset})
	if err != nil {
		return Page[Tag]{}, err
	}
	return Page[Tag]{List: list, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// CreateTag 新建标签；slug 留空时自动生成。
func (s *Service) CreateTag(ctx context.Context, in TagInput) (Tag, error) {
	in, err := cleanTag(in)
	if err != nil {
		return Tag{}, err
	}
	return withAutoSlug(in.Slug, slugPrefixTag, func(slug string) (Tag, error) {
		in.Slug = slug
		return s.repo.CreateTag(ctx, in)
	})
}

// UpdateTag 整体保存标签，返回修改前后的记录；slug 留空时重新生成。
func (s *Service) UpdateTag(ctx context.Context, id int64, in TagInput) (before, after Tag, err error) {
	if in, err = cleanTag(in); err != nil {
		return
	}
	if before, err = s.repo.GetTag(ctx, id); err != nil {
		return
	}
	after, err = withAutoSlug(in.Slug, slugPrefixTag, func(slug string) (Tag, error) {
		in.Slug = slug
		return s.repo.UpdateTag(ctx, id, in)
	})
	return
}

// MergeTag 把标签并入另一个标签（标签不能删除，只能合并），返回挂到目标标签上的剧数。
// 不能并入自己；源标签与目标标签都不能是已被合并的标签。
func (s *Service) MergeTag(ctx context.Context, sourceID, targetID int64) (int64, error) {
	if targetID <= 0 {
		return 0, badRequest("请选择要并入的标签")
	}
	if sourceID == targetID {
		return 0, badRequest("不能并入自己")
	}
	src, err := s.repo.GetTag(ctx, sourceID)
	if err != nil {
		return 0, err
	}
	if src.MergedIntoID != nil {
		return 0, errs.ErrConflict.WithMessage(fmt.Sprintf("该标签已并入 #%d", *src.MergedIntoID))
	}
	dst, err := s.repo.GetTag(ctx, targetID)
	if errors.Is(err, errs.ErrNotFound) {
		return 0, badRequest("标签 #%d 不存在", targetID)
	}
	if err != nil {
		return 0, err
	}
	if dst.MergedIntoID != nil {
		return 0, errs.ErrConflict.WithMessage(fmt.Sprintf("目标标签已并入 #%d，请直接并入 #%d", *dst.MergedIntoID, *dst.MergedIntoID))
	}
	return s.repo.MergeTag(ctx, sourceID, targetID)
}

func cleanTag(in TagInput) (TagInput, error) {
	var err error
	if in.Name, err = cleanText("名称", in.Name, 32, true); err != nil {
		return in, err
	}
	if in.Slug, err = cleanSlug(in.Slug); err != nil {
		return in, err
	}
	switch in.Status {
	case "":
		in.Status = TagActive
	case TagActive, TagHidden:
	default:
		return in, badRequest("状态只能是正常或隐藏")
	}
	return in, nil
}

// ---------- 维度与取值 ----------

// ListTaxonomies 返回全部维度及其取值（按排序值）。
func (s *Service) ListTaxonomies(ctx context.Context) ([]Taxonomy, error) {
	return s.repo.ListTaxonomies(ctx)
}

// CreateTaxonomy 新建维度（排在最后）；slug 留空时自动生成。
func (s *Service) CreateTaxonomy(ctx context.Context, in TaxonomyInput) (Taxonomy, error) {
	in, err := cleanTaxonomy(in)
	if err != nil {
		return Taxonomy{}, err
	}
	return withAutoSlug(in.Slug, slugPrefixTaxonomy, func(slug string) (Taxonomy, error) {
		in.Slug = slug
		return s.repo.CreateTaxonomy(ctx, in)
	})
}

// UpdateTaxonomy 整体保存维度，返回修改前后的记录。已有剧在该维度选了多个取值时不能改为单选。
func (s *Service) UpdateTaxonomy(ctx context.Context, id int64, in TaxonomyInput) (before, after Taxonomy, err error) {
	if in, err = cleanTaxonomy(in); err != nil {
		return
	}
	if before, err = s.repo.GetTaxonomy(ctx, id); err != nil {
		return
	}
	if before.MultiSelect && !in.MultiSelect {
		n, err := s.repo.CountMultiTermDramas(ctx, id)
		if err != nil {
			return before, after, err
		}
		if n > 0 {
			return before, after, errs.ErrConflict.WithMessage(fmt.Sprintf("有 %d 部剧在该维度选了多个取值，请先调整后再改为单选", n))
		}
	}
	after, err = withAutoSlug(in.Slug, slugPrefixTaxonomy, func(slug string) (Taxonomy, error) {
		in.Slug = slug
		return s.repo.UpdateTaxonomy(ctx, id, in)
	})
	return
}

// DeleteTaxonomy 删除维度及其取值（剧的关联一并删除），返回删除前的维度（含取值）。
func (s *Service) DeleteTaxonomy(ctx context.Context, id int64) (Taxonomy, error) {
	return s.repo.DeleteTaxonomy(ctx, id)
}

// ReorderTaxonomies 按传入顺序重排全部维度。ids 必须恰好是现有的全部维度，否则 409（列表已变化）。
func (s *Service) ReorderTaxonomies(ctx context.Context, ids []int64) error {
	if err := checkOrder(ids); err != nil {
		return err
	}
	current, err := s.repo.TaxonomyIDs(ctx)
	if err != nil {
		return err
	}
	if !sameSet(ids, current) {
		return errStaleOrder
	}
	return s.repo.SetTaxonomySort(ctx, ids)
}

// CreateTerm 在维度下新增取值（排在最后）；slug 留空时自动生成。
func (s *Service) CreateTerm(ctx context.Context, taxonomyID int64, in TermInput) (Term, error) {
	in, err := cleanTerm(in)
	if err != nil {
		return Term{}, err
	}
	if _, err := s.repo.GetTaxonomy(ctx, taxonomyID); err != nil {
		return Term{}, err
	}
	return withAutoSlug(in.Slug, slugPrefixTerm, func(slug string) (Term, error) {
		in.Slug = slug
		return s.repo.CreateTerm(ctx, taxonomyID, in)
	})
}

// UpdateTerm 整体保存取值，返回修改前后的记录。
func (s *Service) UpdateTerm(ctx context.Context, taxonomyID, termID int64, in TermInput) (before, after Term, err error) {
	if in, err = cleanTerm(in); err != nil {
		return
	}
	if before, err = s.repo.GetTerm(ctx, taxonomyID, termID); err != nil {
		return
	}
	after, err = withAutoSlug(in.Slug, slugPrefixTerm, func(slug string) (Term, error) {
		in.Slug = slug
		return s.repo.UpdateTerm(ctx, taxonomyID, termID, in)
	})
	return
}

// DeleteTerm 删除取值（剧的关联一并删除），返回删除前的记录。
func (s *Service) DeleteTerm(ctx context.Context, taxonomyID, termID int64) (Term, error) {
	return s.repo.DeleteTerm(ctx, taxonomyID, termID)
}

// ReorderTerms 按传入顺序重排维度下的全部取值；ids 必须恰好是该维度现有的全部取值。
func (s *Service) ReorderTerms(ctx context.Context, taxonomyID int64, ids []int64) error {
	if err := checkOrder(ids); err != nil {
		return err
	}
	if _, err := s.repo.GetTaxonomy(ctx, taxonomyID); err != nil {
		return err
	}
	current, err := s.repo.TermIDs(ctx, taxonomyID)
	if err != nil {
		return err
	}
	if !sameSet(ids, current) {
		return errStaleOrder
	}
	return s.repo.SetTermSort(ctx, taxonomyID, ids)
}

var errStaleOrder = errs.ErrConflict.WithMessage("列表已变化，请刷新后重新排序")

// checkOrder 要求排序列表非空、没有重复且 ID 有效。
func checkOrder(ids []int64) error {
	if len(ids) == 0 {
		return badRequest("排序列表不能为空")
	}
	if len(ids) > 1000 {
		return badRequest("排序列表最多 1000 项")
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return badRequest("排序列表中有无效或重复的 ID")
		}
		seen[id] = true
	}
	return nil
}

func cleanTaxonomy(in TaxonomyInput) (TaxonomyInput, error) {
	var err error
	if in.Name, err = cleanText("名称", in.Name, 32, true); err != nil {
		return in, err
	}
	if in.Slug, err = cleanSlug(in.Slug); err != nil {
		return in, err
	}
	switch in.Status {
	case "":
		in.Status = StatusActive
	case StatusActive, StatusInactive:
	default:
		return in, badRequest("状态只能是启用或停用")
	}
	return in, nil
}

func cleanTerm(in TermInput) (TermInput, error) {
	var err error
	if in.Name, err = cleanText("名称", in.Name, 32, true); err != nil {
		return in, err
	}
	if in.Slug, err = cleanSlug(in.Slug); err != nil {
		return in, err
	}
	switch in.Status {
	case "":
		in.Status = StatusActive
	case StatusActive, StatusInactive:
	default:
		return in, badRequest("状态只能是启用或停用")
	}
	in.Code = strings.TrimSpace(in.Code)
	if in.Code != "" && !codeRe.MatchString(in.Code) {
		return in, badRequest("编号只能包含字母、数字、下划线和连字符，最多 32 个字符")
	}
	return in, nil
}

// ---------- 专题 ----------

// ListTopics 返回专题列表的一页（按编号倒序）。
func (s *Service) ListTopics(ctx context.Context, f TopicFilter) (Page[Topic], error) {
	q, err := cleanQuery(f.Q)
	if err != nil {
		return Page[Topic]{}, err
	}
	if f.Status != "" && f.Status != TopicDraft && f.Status != TopicPublished {
		return Page[Topic]{}, badRequest("状态筛选无效")
	}
	if f.SourceType != "" && f.SourceType != SourceManual && f.SourceType != SourceAuto {
		return Page[Topic]{}, badRequest("剧来源筛选无效")
	}
	p, limit, offset, err := limitOffset(f.PageRequest)
	if err != nil {
		return Page[Topic]{}, err
	}
	list, total, err := s.repo.ListTopics(ctx, TopicQuery{
		Pattern: likePattern(q), Status: f.Status, SourceType: f.SourceType, Limit: limit, Offset: offset,
	})
	if err != nil {
		return Page[Topic]{}, err
	}
	return Page[Topic]{List: list, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// GetTopic 返回专题与手选的剧（按展示顺序）。
func (s *Service) GetTopic(ctx context.Context, id int64) (TopicDetail, error) {
	t, err := s.repo.GetTopic(ctx, id)
	if err != nil {
		return TopicDetail{}, err
	}
	dramas, err := s.repo.ListTopicDramas(ctx, id)
	if err != nil {
		return TopicDetail{}, err
	}
	return TopicDetail{Topic: t, Dramas: dramas}, nil
}

// CreateTopic 新建专题；slug 留空时自动生成。
func (s *Service) CreateTopic(ctx context.Context, in TopicInput) (Topic, error) {
	in, err := s.cleanTopic(ctx, in)
	if err != nil {
		return Topic{}, err
	}
	return withAutoSlug(in.Slug, slugPrefixTopic, func(slug string) (Topic, error) {
		in.Slug = slug
		return s.repo.CreateTopic(ctx, in)
	})
}

// UpdateTopic 整体保存专题，返回修改前后的记录。改为自动专题时手选的剧保留（改回手选后恢复）。
func (s *Service) UpdateTopic(ctx context.Context, id int64, in TopicInput) (before, after Topic, err error) {
	if in, err = s.cleanTopic(ctx, in); err != nil {
		return
	}
	if before, err = s.repo.GetTopic(ctx, id); err != nil {
		return
	}
	after, err = withAutoSlug(in.Slug, slugPrefixTopic, func(slug string) (Topic, error) {
		in.Slug = slug
		return s.repo.UpdateTopic(ctx, id, in)
	})
	return
}

// DeleteTopic 删除专题（不进回收站），返回删除前的记录。
func (s *Service) DeleteTopic(ctx context.Context, id int64) (Topic, error) {
	return s.repo.DeleteTopic(ctx, id)
}

// SetTopicDramas 整体替换手选专题的剧（数组顺序即展示顺序），返回替换前后的剧编号。
func (s *Service) SetTopicDramas(ctx context.Context, id int64, dramaIDs []int64) (before, after []int64, err error) {
	if dramaIDs, err = uniqueIDs("剧集", dramaIDs, 500); err != nil {
		return
	}
	t, err := s.repo.GetTopic(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if t.SourceType != SourceManual {
		return nil, nil, errAutoTopicDramas
	}
	if len(dramaIDs) > 0 {
		live, err := s.repo.LiveDramaIDs(ctx, dramaIDs)
		if err != nil {
			return nil, nil, err
		}
		if m := missing(dramaIDs, live); m != 0 {
			return nil, nil, badRequest("剧 #%d 不存在", m)
		}
	}
	old, err := s.repo.ListTopicDramas(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	before = make([]int64, len(old))
	for i, d := range old {
		before[i] = d.ID
	}
	if err = s.repo.ReplaceTopicDramas(ctx, id, dramaIDs); err != nil {
		return nil, nil, err
	}
	return before, dramaIDs, nil
}

// 自动专题规则的上新窗口。
var topicWindows = map[string]bool{"all": true, "7d": true, "14d": true, "30d": true, "90d": true}

// cleanTopic 校验专题；自动专题还要确认规则引用的取值编号与标签存在。
func (s *Service) cleanTopic(ctx context.Context, in TopicInput) (TopicInput, error) {
	var err error
	if in.Title, err = cleanText("标题", in.Title, 100, true); err != nil {
		return in, err
	}
	if in.Slug, err = cleanSlug(in.Slug); err != nil {
		return in, err
	}
	if in.Summary, err = cleanText("简介", in.Summary, 2000, false); err != nil {
		return in, err
	}
	switch in.Status {
	case "":
		in.Status = TopicDraft
	case TopicDraft, TopicPublished:
	default:
		return in, badRequest("状态只能是草稿或发布")
	}
	switch in.SourceType {
	case "":
		in.SourceType = SourceManual
	case SourceManual, SourceAuto:
	default:
		return in, badRequest("剧来源只能是手选或规则自动")
	}
	if len(in.FAQ) > 20 {
		return in, badRequest("FAQ 最多 20 条")
	}
	faq := make([]FAQ, len(in.FAQ))
	for i, f := range in.FAQ {
		if faq[i].Question, err = cleanText("FAQ 问题", f.Question, 200, true); err != nil {
			return in, err
		}
		if faq[i].Answer, err = cleanText("FAQ 回答", f.Answer, 2000, true); err != nil {
			return in, err
		}
	}
	in.FAQ = faq
	if in.Rule, err = cleanRule(in.Rule); err != nil {
		return in, err
	}
	if in.SourceType == SourceAuto {
		if err := s.checkRule(ctx, in.Rule); err != nil {
			return in, err
		}
	}
	return in, nil
}

// cleanRule 补齐规则的默认值并校验取值范围。
func cleanRule(r TopicRule) (TopicRule, error) {
	codes, err := cleanList("取值编号", r.TermCodes, 10, 32)
	if err != nil {
		return r, err
	}
	for _, c := range codes {
		if !codeRe.MatchString(c) {
			return r, badRequest("取值编号「%s」格式无效", c)
		}
	}
	r.TermCodes = codes
	if r.TagID != nil && *r.TagID <= 0 {
		return r, badRequest("标签 ID 无效")
	}
	r = r.WithDefaults()
	if r.Sort != SortLatest && r.Sort != SortHot && r.Sort != SortRecommend {
		return r, badRequest("规则排序只能是最新、最热或推荐")
	}
	if !topicWindows[r.Window] {
		return r, badRequest("上新时间只能是不限、7 天、14 天、30 天或 90 天")
	}
	if r.Limit < 1 || r.Limit > maxTopicLimit {
		return r, badRequest("规则选剧数量需为 1–%d", maxTopicLimit)
	}
	return r, nil
}

func (s *Service) checkRule(ctx context.Context, r TopicRule) error {
	if len(r.TermCodes) > 0 {
		found, err := s.repo.ExistingTermCodes(ctx, r.TermCodes)
		if err != nil {
			return err
		}
		set := make(map[string]bool, len(found))
		for _, c := range found {
			set[c] = true
		}
		for _, c := range r.TermCodes {
			if !set[c] {
				return badRequest("取值编号「%s」不存在", c)
			}
		}
	}
	if r.TagID != nil {
		return s.checkTags(ctx, []int64{*r.TagID})
	}
	return nil
}
