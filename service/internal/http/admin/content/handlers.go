package content

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/content"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// bindQuery 解析查询参数；类型不对时已写出 400。
func bindQuery(c *gin.Context, dst any) bool {
	if err := c.ShouldBindQuery(dst); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return false
	}
	return true
}

// boolOr 返回 *b，未传时返回默认值。
func boolOr(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}

// ---------- 剧集 ----------

type dramaListQuery struct {
	Q        string `form:"q"`
	Status   string `form:"status"`
	TagID    int64  `form:"tag_id"`
	TermID   int64  `form:"term_id"`
	Sort     string `form:"sort"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// dramaFields 是新建与保存共用的剧资料字段。
type dramaFields struct {
	Title          string     `json:"title"`
	Aliases        []string   `json:"aliases"`
	Slug           string     `json:"slug"`
	Summary        string     `json:"summary"`
	CoverPortrait  string     `json:"cover_portrait"`
	CoverLandscape string     `json:"cover_landscape"`
	Badge          string     `json:"badge"`
	Score          *float64   `json:"score"`
	Year           *int32     `json:"year"`
	PublishAt      *time.Time `json:"publish_at"`
	IsFinished     bool       `json:"is_finished"`
	SortWeight     int32      `json:"sort_weight"`
	ViewBase       int64      `json:"view_base"`
	SEOTitle       string     `json:"seo_title"`
	SEODesc        string     `json:"seo_desc"`
}

func (f dramaFields) input(status dom.DramaStatus) dom.DramaInput {
	return dom.DramaInput{
		Title: f.Title, Aliases: f.Aliases, Slug: f.Slug, Summary: f.Summary, CoverPortrait: f.CoverPortrait,
		CoverLandscape: f.CoverLandscape, Badge: f.Badge, Score: f.Score, Year: f.Year, Status: status,
		PublishAt: f.PublishAt, IsFinished: f.IsFinished, SortWeight: f.SortWeight, ViewBase: f.ViewBase,
		SEOTitle: f.SEOTitle, SEODesc: f.SEODesc,
	}
}

type dramaUpdateRequest struct {
	dramaFields
	Status string `json:"status"`
}

type tagIDsRequest struct {
	TagIDs []int64 `json:"tag_ids" binding:"required"`
}

type termIDsRequest struct {
	TermIDs []int64 `json:"term_ids" binding:"required"`
}

type bulkRequest struct {
	IDs     []int64 `json:"ids" binding:"required"`
	Action  string  `json:"action" binding:"required"`
	TagIDs  []int64 `json:"tag_ids"`
	TopicID int64   `json:"topic_id"`
	TermIDs []int64 `json:"term_ids"`
}

// ListDramas 是 GET /api/admin/dramas：搜索、筛选、排序与页码分页（不含回收站中的剧）。
func (h *Handlers) ListDramas(c *gin.Context) {
	var q dramaListQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Svc.ListDramas(c.Request.Context(), dom.DramaFilter{
		Q: q.Q, Status: dom.DramaStatus(q.Status), TagID: q.TagID, TermID: q.TermID, Sort: dom.DramaSort(q.Sort),
		PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page, dramaOf(h.now())))
}

// GetDrama 是 GET /api/admin/dramas/:id：剧资料与标签、维度取值。
func (h *Handlers) GetDrama(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	d, err := h.Svc.GetDrama(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, dramaDetailDTO{
		dramaDTO: dramaOf(h.now())(d.Drama),
		Tags:     mapSlice(d.Tags, tagOf),
		Terms:    mapSlice(d.Terms, dramaTermOf),
	})
}

// CreateDrama 是 POST /api/admin/dramas：新建草稿。
func (h *Handlers) CreateDrama(c *gin.Context) {
	var req dramaFields
	if !admin.Bind(c, &req) {
		return
	}
	d, err := h.Svc.CreateDrama(c.Request.Context(), req.input(dom.DramaDraft))
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := dramaOf(h.now())(d)
	admin.SetAudit(c, admin.AuditDetail{Action: "drama.create", TargetType: "drama", TargetID: itoa(d.ID), After: out})
	respond.OK(c, out)
}

// UpdateDrama 是 PATCH /api/admin/dramas/:id：整体保存剧资料。
func (h *Handlers) UpdateDrama(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req dramaUpdateRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.UpdateDrama(c.Request.Context(), id, req.input(dom.DramaStatus(req.Status)))
	if err != nil {
		respond.Error(c, err)
		return
	}
	toDTO := dramaOf(h.now())
	out := toDTO(after)
	admin.SetAudit(c, admin.AuditDetail{Action: "drama.update", TargetType: "drama", TargetID: itoa(id), Before: toDTO(before), After: out})
	respond.OK(c, out)
}

// DeleteDrama 是 DELETE /api/admin/dramas/:id：放进回收站。
func (h *Handlers) DeleteDrama(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Svc.DeleteDrama(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "drama.delete", TargetType: "drama", TargetID: itoa(id), Before: dramaOf(h.now())(before)})
	respond.OK(c, nil)
}

func tagIDsOf(list []dom.Tag) []int64 {
	return mapSlice(list, func(t dom.Tag) int64 { return t.ID })
}

// SetDramaTags 是 PUT /api/admin/dramas/:id/tags：整体替换标签。
func (h *Handlers) SetDramaTags(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req tagIDsRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.SetDramaTags(c.Request.Context(), id, req.TagIDs)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "drama.set_tags", TargetType: "drama", TargetID: itoa(id),
		Before: gin.H{"tag_ids": tagIDsOf(before)}, After: gin.H{"tag_ids": tagIDsOf(after)}})
	respond.OK(c, gin.H{"tags": mapSlice(after, tagOf)})
}

func termIDsOf(list []dom.TermRef) []int64 {
	return mapSlice(list, func(t dom.TermRef) int64 { return t.ID })
}

// SetDramaTerms 是 PUT /api/admin/dramas/:id/terms：整体替换维度取值（单选维度最多一个）。
func (h *Handlers) SetDramaTerms(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req termIDsRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.SetDramaTerms(c.Request.Context(), id, req.TermIDs)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "drama.set_terms", TargetType: "drama", TargetID: itoa(id),
		Before: gin.H{"term_ids": termIDsOf(before)}, After: gin.H{"term_ids": termIDsOf(after)}})
	respond.OK(c, gin.H{"terms": mapSlice(after, dramaTermOf)})
}

// BulkDramas 是 POST /api/admin/dramas/bulk：批量上架、下架、转草稿、追加标签、加入手选专题、设维度取值。
func (h *Handlers) BulkDramas(c *gin.Context) {
	var req bulkRequest
	if !admin.Bind(c, &req) {
		return
	}
	n, err := h.Svc.BulkDramas(c.Request.Context(), dom.BulkInput{
		IDs: req.IDs, Action: dom.BulkAction(req.Action), TagIDs: req.TagIDs, TopicID: req.TopicID, TermIDs: req.TermIDs,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "drama.bulk", TargetType: "drama",
		After: gin.H{"action": req.Action, "ids": req.IDs, "tag_ids": ids(req.TagIDs), "topic_id": req.TopicID,
			"term_ids": ids(req.TermIDs), "affected": n}})
	respond.OK(c, gin.H{"affected": n})
}

// ---------- 分集 ----------

type episodeRequest struct {
	Number      int32  `json:"number"`
	Title       string `json:"title"`
	Cover       string `json:"cover"`
	DurationSec int32  `json:"duration_sec"`
	Access      string `json:"access"`
	TrialSec    int32  `json:"trial_sec"`
}

func (r episodeRequest) input() dom.EpisodeInput {
	return dom.EpisodeInput{
		Number: r.Number, Title: r.Title, Cover: r.Cover, DurationSec: r.DurationSec, Access: dom.Access(r.Access), TrialSec: r.TrialSec,
	}
}

// ListEpisodes 是 GET /api/admin/dramas/:id/episodes：一部剧的全部分集（按集号）。
func (h *Handlers) ListEpisodes(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	list, err := h.Svc.ListEpisodes(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, listOf(list, episodeOf))
}

// CreateEpisode 是 POST /api/admin/dramas/:id/episodes。
func (h *Handlers) CreateEpisode(c *gin.Context) {
	dramaID, ok := admin.PathID(c)
	var req episodeRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	ep, err := h.Svc.CreateEpisode(c.Request.Context(), dramaID, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := episodeOf(ep)
	admin.SetAudit(c, admin.AuditDetail{Action: "episode.create", TargetType: "episode", TargetID: itoa(ep.ID), After: out})
	respond.OK(c, out)
}

// UpdateEpisode 是 PATCH /api/admin/episodes/:id：整体保存分集。
func (h *Handlers) UpdateEpisode(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req episodeRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.UpdateEpisode(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := episodeOf(after)
	admin.SetAudit(c, admin.AuditDetail{Action: "episode.update", TargetType: "episode", TargetID: itoa(id), Before: episodeOf(before), After: out})
	respond.OK(c, out)
}

// DeleteEpisode 是 DELETE /api/admin/episodes/:id（直接删除，不进回收站）。
func (h *Handlers) DeleteEpisode(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Svc.DeleteEpisode(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "episode.delete", TargetType: "episode", TargetID: itoa(id), Before: episodeOf(before)})
	respond.OK(c, nil)
}

// ---------- 标签 ----------

// listQuery 是标签、专题列表共用的搜索、状态筛选与页码参数。
type listQuery struct {
	Q        string `form:"q"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

type tagListQuery struct {
	listQuery
	Merged string `form:"merged"`
}

type tagRequest struct {
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	IsHidden bool   `json:"is_hidden"`
	Status   string `json:"status"`
}

func (r tagRequest) input() dom.TagInput {
	return dom.TagInput{Name: r.Name, Slug: r.Slug, IsHidden: r.IsHidden, Status: dom.TagStatus(r.Status)}
}

type mergeRequest struct {
	TargetID int64 `json:"target_id" binding:"required"`
}

// ListTags 是 GET /api/admin/tags。merged=true 只看已合并的，merged=false 只看未合并的。
func (h *Handlers) ListTags(c *gin.Context) {
	var q tagListQuery
	if !bindQuery(c, &q) {
		return
	}
	var merged *bool
	switch q.Merged {
	case "":
	case "true", "false":
		v := q.Merged == "true"
		merged = &v
	default:
		respond.Error(c, errs.ErrBadRequest.WithMessage("merged 只能是 true 或 false"))
		return
	}
	page, err := h.Svc.ListTags(c.Request.Context(), dom.TagFilter{
		Q: q.Q, Status: dom.TagStatus(q.Status), Merged: merged, PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page, tagOf))
}

// CreateTag 是 POST /api/admin/tags。
func (h *Handlers) CreateTag(c *gin.Context) {
	var req tagRequest
	if !admin.Bind(c, &req) {
		return
	}
	t, err := h.Svc.CreateTag(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := tagOf(t)
	admin.SetAudit(c, admin.AuditDetail{Action: "tag.create", TargetType: "tag", TargetID: itoa(t.ID), After: out})
	respond.OK(c, out)
}

// UpdateTag 是 PATCH /api/admin/tags/:id：整体保存标签。
func (h *Handlers) UpdateTag(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req tagRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.UpdateTag(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := tagOf(after)
	admin.SetAudit(c, admin.AuditDetail{Action: "tag.update", TargetType: "tag", TargetID: itoa(id), Before: tagOf(before), After: out})
	respond.OK(c, out)
}

// MergeTag 是 POST /api/admin/tags/:id/merge：把该标签并入 target_id。
func (h *Handlers) MergeTag(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req mergeRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	moved, err := h.Svc.MergeTag(c.Request.Context(), id, req.TargetID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "tag.merge", TargetType: "tag", TargetID: itoa(id),
		Before: gin.H{"merged_into_id": nil}, After: gin.H{"merged_into_id": req.TargetID, "moved_dramas": moved}})
	respond.OK(c, gin.H{"merged_into_id": req.TargetID, "moved_dramas": moved})
}

// ---------- 维度与取值 ----------

type taxonomyRequest struct {
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	MultiSelect   bool   `json:"multi_select"`
	ShowInExplore *bool  `json:"show_in_explore"`
	Status        string `json:"status"`
}

func (r taxonomyRequest) input() dom.TaxonomyInput {
	return dom.TaxonomyInput{
		Name: r.Name, Slug: r.Slug, MultiSelect: r.MultiSelect, ShowInExplore: boolOr(r.ShowInExplore, true), Status: dom.ActiveStatus(r.Status),
	}
}

type termRequest struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Status    string `json:"status"`
	IsChannel bool   `json:"is_channel"`
	Code      string `json:"code"`
}

func (r termRequest) input() dom.TermInput {
	return dom.TermInput{Name: r.Name, Slug: r.Slug, Status: dom.ActiveStatus(r.Status), IsChannel: r.IsChannel, Code: r.Code}
}

type reorderRequest struct {
	IDs []int64 `json:"ids" binding:"required"`
}

// ListTaxonomies 是 GET /api/admin/taxonomies：全部维度及其取值（数量少，不分页）。
func (h *Handlers) ListTaxonomies(c *gin.Context) {
	list, err := h.Svc.ListTaxonomies(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, listOf(list, taxonomyOf))
}

// CreateTaxonomy 是 POST /api/admin/taxonomies（排在最后）。
func (h *Handlers) CreateTaxonomy(c *gin.Context) {
	var req taxonomyRequest
	if !admin.Bind(c, &req) {
		return
	}
	t, err := h.Svc.CreateTaxonomy(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := taxonomyOf(t)
	admin.SetAudit(c, admin.AuditDetail{Action: "taxonomy.create", TargetType: "taxonomy", TargetID: itoa(t.ID), After: out})
	respond.OK(c, out)
}

// UpdateTaxonomy 是 PATCH /api/admin/taxonomies/:id：整体保存维度。
func (h *Handlers) UpdateTaxonomy(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req taxonomyRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.UpdateTaxonomy(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := taxonomyOf(after)
	admin.SetAudit(c, admin.AuditDetail{Action: "taxonomy.update", TargetType: "taxonomy", TargetID: itoa(id), Before: taxonomyOf(before), After: out})
	respond.OK(c, out)
}

// DeleteTaxonomy 是 DELETE /api/admin/taxonomies/:id：连同取值与剧的关联一起删除。
func (h *Handlers) DeleteTaxonomy(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Svc.DeleteTaxonomy(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "taxonomy.delete", TargetType: "taxonomy", TargetID: itoa(id), Before: taxonomyOf(before)})
	respond.OK(c, nil)
}

// ReorderTaxonomies 是 POST /api/admin/taxonomies/reorder：按传入顺序重排全部维度。
func (h *Handlers) ReorderTaxonomies(c *gin.Context) {
	var req reorderRequest
	if !admin.Bind(c, &req) {
		return
	}
	if err := h.Svc.ReorderTaxonomies(c.Request.Context(), req.IDs); err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "taxonomy.reorder", TargetType: "taxonomy", After: gin.H{"ids": req.IDs}})
	respond.OK(c, nil)
}

// termPath 解析 /taxonomies/:id/terms/:term_id；失败时已写出 404。
func termPath(c *gin.Context) (taxonomyID, termID int64, ok bool) {
	if taxonomyID, ok = admin.PathID(c); !ok {
		return
	}
	termID, ok = admin.ParamID(c, "term_id")
	return
}

// CreateTerm 是 POST /api/admin/taxonomies/:id/terms（排在该维度最后）。
func (h *Handlers) CreateTerm(c *gin.Context) {
	taxonomyID, ok := admin.PathID(c)
	var req termRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	t, err := h.Svc.CreateTerm(c.Request.Context(), taxonomyID, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := termOf(t)
	admin.SetAudit(c, admin.AuditDetail{Action: "taxonomy_term.create", TargetType: "taxonomy_term", TargetID: itoa(t.ID), After: out})
	respond.OK(c, out)
}

// UpdateTerm 是 PATCH /api/admin/taxonomies/:id/terms/:term_id：整体保存取值。
func (h *Handlers) UpdateTerm(c *gin.Context) {
	taxonomyID, termID, ok := termPath(c)
	var req termRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.UpdateTerm(c.Request.Context(), taxonomyID, termID, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := termOf(after)
	admin.SetAudit(c, admin.AuditDetail{Action: "taxonomy_term.update", TargetType: "taxonomy_term", TargetID: itoa(termID), Before: termOf(before), After: out})
	respond.OK(c, out)
}

// DeleteTerm 是 DELETE /api/admin/taxonomies/:id/terms/:term_id：剧的关联一并删除。
func (h *Handlers) DeleteTerm(c *gin.Context) {
	taxonomyID, termID, ok := termPath(c)
	if !ok {
		return
	}
	before, err := h.Svc.DeleteTerm(c.Request.Context(), taxonomyID, termID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "taxonomy_term.delete", TargetType: "taxonomy_term", TargetID: itoa(termID), Before: termOf(before)})
	respond.OK(c, nil)
}

// ReorderTerms 是 POST /api/admin/taxonomies/:id/terms/reorder：按传入顺序重排该维度的全部取值。
func (h *Handlers) ReorderTerms(c *gin.Context) {
	taxonomyID, ok := admin.PathID(c)
	var req reorderRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	if err := h.Svc.ReorderTerms(c.Request.Context(), taxonomyID, req.IDs); err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "taxonomy_term.reorder", TargetType: "taxonomy", TargetID: itoa(taxonomyID), After: gin.H{"ids": req.IDs}})
	respond.OK(c, nil)
}

// ---------- 专题 ----------

type topicListQuery struct {
	listQuery
	SourceType string `form:"source_type"`
}

type topicRequest struct {
	Title       string   `json:"title"`
	Slug        string   `json:"slug"`
	Summary     string   `json:"summary"`
	Status      string   `json:"status"`
	SourceType  string   `json:"source_type"`
	Rule        *ruleDTO `json:"rule"`
	FAQ         []faqDTO `json:"faq"`
	ListInIndex *bool    `json:"list_in_index"`
}

func (r topicRequest) input() dom.TopicInput {
	var rule dom.TopicRule
	if r.Rule != nil {
		rule = dom.TopicRule{TermCodes: r.Rule.TermCodes, TagID: r.Rule.TagID, Sort: dom.DramaSort(r.Rule.Sort), Window: r.Rule.Window, Limit: r.Rule.Limit}
	}
	return dom.TopicInput{
		Title: r.Title, Slug: r.Slug, Summary: r.Summary, Status: dom.TopicStatus(r.Status), SourceType: dom.SourceType(r.SourceType),
		Rule: rule, FAQ: mapSlice(r.FAQ, func(f faqDTO) dom.FAQ { return dom.FAQ(f) }), ListInIndex: boolOr(r.ListInIndex, true),
	}
}

type topicDramasRequest struct {
	DramaIDs []int64 `json:"drama_ids" binding:"required"`
}

// ListTopics 是 GET /api/admin/topics。
func (h *Handlers) ListTopics(c *gin.Context) {
	var q topicListQuery
	if !bindQuery(c, &q) {
		return
	}
	page, err := h.Svc.ListTopics(c.Request.Context(), dom.TopicFilter{
		Q: q.Q, Status: dom.TopicStatus(q.Status), SourceType: dom.SourceType(q.SourceType),
		PageRequest: dom.PageRequest{Page: q.Page, PageSize: q.PageSize},
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageOf(page, topicOf))
}

// GetTopic 是 GET /api/admin/topics/:id：专题与手选的剧（按展示顺序）。
func (h *Handlers) GetTopic(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	t, err := h.Svc.GetTopic(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, topicDetailDTO{topicDTO: topicOf(t.Topic), Dramas: mapSlice(t.Dramas, topicDramaOf(h.now()))})
}

// CreateTopic 是 POST /api/admin/topics。
func (h *Handlers) CreateTopic(c *gin.Context) {
	var req topicRequest
	if !admin.Bind(c, &req) {
		return
	}
	t, err := h.Svc.CreateTopic(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := topicOf(t)
	admin.SetAudit(c, admin.AuditDetail{Action: "topic.create", TargetType: "topic", TargetID: itoa(t.ID), After: out})
	respond.OK(c, out)
}

// UpdateTopic 是 PATCH /api/admin/topics/:id：整体保存专题。
func (h *Handlers) UpdateTopic(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req topicRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.UpdateTopic(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := topicOf(after)
	admin.SetAudit(c, admin.AuditDetail{Action: "topic.update", TargetType: "topic", TargetID: itoa(id), Before: topicOf(before), After: out})
	respond.OK(c, out)
}

// DeleteTopic 是 DELETE /api/admin/topics/:id（不进回收站）。
func (h *Handlers) DeleteTopic(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Svc.DeleteTopic(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "topic.delete", TargetType: "topic", TargetID: itoa(id), Before: topicOf(before)})
	respond.OK(c, nil)
}

// SetTopicDramas 是 PUT /api/admin/topics/:id/dramas：整体替换手选专题的剧（数组顺序即展示顺序）。
func (h *Handlers) SetTopicDramas(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req topicDramasRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.SetTopicDramas(c.Request.Context(), id, req.DramaIDs)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "topic.set_dramas", TargetType: "topic", TargetID: itoa(id),
		Before: gin.H{"drama_ids": ids(before)}, After: gin.H{"drama_ids": ids(after)}})
	respond.OK(c, gin.H{"drama_ids": ids(after)})
}
