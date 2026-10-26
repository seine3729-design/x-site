package site

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/site"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// permBlockCustomHTML 是新建、修改自定义 HTML 区块所需的权限点（默认只有超级管理员）。
const permBlockCustomHTML = "blocks.custom_html"

// canBlockHTML 表示当前管理员能否新建、修改自定义 HTML 区块。
func canBlockHTML(c *gin.Context) bool {
	id, ok := admin.CurrentAdmin(c)
	return ok && id.Can(permBlockCustomHTML)
}

type blockListQuery struct {
	Page string `form:"page"`
}

// blockSourceDTO 是区块的来源与类型参数（只填该类型用得到的字段）。
type blockSourceDTO struct {
	DramaIDs     []int64  `json:"drama_ids"`
	TopicID      int64    `json:"topic_id"`
	TermCodes    []string `json:"term_codes"`
	TagID        *int64   `json:"tag_id"`
	Sort         string   `json:"sort"`
	Window       string   `json:"window"`
	RankBoard    string   `json:"rank_board"`
	AdSlot       string   `json:"ad_slot"`
	AnnounceKind string   `json:"announce_kind"`
	TopicIDs     []int64  `json:"topic_ids"`
	TagIDs       []int64  `json:"tag_ids"`
}

func (s blockSourceDTO) input() dom.BlockSource {
	return dom.BlockSource{
		DramaIDs: s.DramaIDs, TopicID: s.TopicID, TermCodes: s.TermCodes, TagID: s.TagID,
		Sort: contentSort(s.Sort), Window: s.Window, RankBoard: s.RankBoard, AdSlot: s.AdSlot,
		AnnounceKind: s.AnnounceKind, TopicIDs: s.TopicIDs, TagIDs: s.TagIDs,
	}
}

func blockSourceOf(s dom.BlockSource) blockSourceDTO {
	return blockSourceDTO{
		DramaIDs: ids(s.DramaIDs), TopicID: s.TopicID, TermCodes: strs(s.TermCodes), TagID: s.TagID,
		Sort: string(s.Sort), Window: s.Window, RankBoard: s.RankBoard, AdSlot: s.AdSlot,
		AnnounceKind: s.AnnounceKind, TopicIDs: ids(s.TopicIDs), TagIDs: ids(s.TagIDs),
	}
}

// blockRequest 是新建与整体保存的请求体（未传的可选字段按默认值保存）。
type blockRequest struct {
	Page        string         `json:"page"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Subtitle    string         `json:"subtitle"`
	ItemLimit   int            `json:"item_limit"`
	Status      string         `json:"status"`
	MoreLink    string         `json:"more_link"`
	ColsDesktop int            `json:"cols_desktop"`
	Device      string         `json:"device"`
	SourceType  string         `json:"source_type"`
	Source      blockSourceDTO `json:"source"`
	CustomHTML  string         `json:"custom_html"`
}

func (r blockRequest) input() dom.BlockInput {
	return dom.BlockInput{
		Page: r.Page, Type: dom.BlockType(r.Type), Title: r.Title, Subtitle: r.Subtitle, ItemLimit: r.ItemLimit,
		Status: dom.MenuStatus(r.Status), MoreLink: r.MoreLink, ColsDesktop: r.ColsDesktop, Device: dom.MenuDevice(r.Device),
		SourceType: dom.BlockSourceType(r.SourceType), Source: r.Source.input(), CustomHTML: r.CustomHTML,
	}
}

type blockDTO struct {
	ID          int64          `json:"id"`
	Page        string         `json:"page"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Subtitle    string         `json:"subtitle"`
	ItemLimit   int            `json:"item_limit"`
	Status      string         `json:"status"`
	MoreLink    string         `json:"more_link"`
	ColsDesktop int            `json:"cols_desktop"`
	Device      string         `json:"device"`
	SourceType  string         `json:"source_type"`
	Source      blockSourceDTO `json:"source"`
	CustomHTML  string         `json:"custom_html"`
	Sort        int            `json:"sort"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

func blockOf(b dom.Block) blockDTO {
	return blockDTO{
		ID: b.ID, Page: b.Page, Type: string(b.Type), Title: b.Title, Subtitle: b.Subtitle, ItemLimit: b.ItemLimit,
		Status: string(b.Status), MoreLink: b.MoreLink, ColsDesktop: b.ColsDesktop, Device: string(b.Device),
		SourceType: string(b.SourceType), Source: blockSourceOf(b.Source), CustomHTML: b.CustomHTML, Sort: b.Sort,
		CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
	}
}

// ListBlocks 是 GET /api/admin/blocks?page=：某个页面的全部区块（按顺序）。page 不传按首页。
func (h *Handlers) ListBlocks(c *gin.Context) {
	var q blockListQuery
	if !bindQuery(c, &q) {
		return
	}
	list, err := h.Blocks.List(c.Request.Context(), q.Page)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"list": mapSlice(list, blockOf)})
}

// BlockPages 是 GET /api/admin/blocks/pages：可编排的页面（首页 + 启用的频道页）。
func (h *Handlers) BlockPages(c *gin.Context) {
	pages, err := h.Blocks.Pages(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	type pageDTO struct {
		Value string `json:"value"`
		Label string `json:"label"`
	}
	respond.OK(c, gin.H{"list": mapSlice(pages, func(p dom.BlockPage) pageDTO {
		return pageDTO{Value: p.Value, Label: p.Label}
	})})
}

// CreateBlock 是 POST /api/admin/blocks：新建区块（放在该页面的末尾），前台立即生效。
func (h *Handlers) CreateBlock(c *gin.Context) {
	var req blockRequest
	if !admin.Bind(c, &req) {
		return
	}
	b, err := h.Blocks.Create(c.Request.Context(), req.input(), canBlockHTML(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := blockOf(b)
	admin.SetAudit(c, admin.AuditDetail{Action: "block.create", TargetType: "block", TargetID: itoa(b.ID), After: out})
	respond.OK(c, out)
}

// UpdateBlock 是 PATCH /api/admin/blocks/:id：整体保存；换页面时放到新页面的末尾。
func (h *Handlers) UpdateBlock(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req blockRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Blocks.Update(c.Request.Context(), id, req.input(), canBlockHTML(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := blockOf(after)
	admin.SetAudit(c, admin.AuditDetail{
		Action: "block.update", TargetType: "block", TargetID: itoa(id), Before: blockOf(before), After: out,
	})
	respond.OK(c, out)
}

// DeleteBlock 是 DELETE /api/admin/blocks/:id：删除不可恢复（删除只要 blocks.edit）。
func (h *Handlers) DeleteBlock(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	b, err := h.Blocks.Delete(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "block.delete", TargetType: "block", TargetID: itoa(id), Before: blockOf(b)})
	respond.OK(c, gin.H{"id": id})
}

// blockReorderRequest 是某个页面的新顺序：必须恰好是该页面现有的全部区块。
type blockReorderRequest struct {
	Page string  `json:"page"`
	IDs  []int64 `json:"ids"`
}

// ReorderBlocks 是 POST /api/admin/blocks/reorder：按页面重排。
func (h *Handlers) ReorderBlocks(c *gin.Context) {
	var req blockReorderRequest
	if !admin.Bind(c, &req) {
		return
	}
	if req.IDs == nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请提供 ids（该页面区块的新顺序）"))
		return
	}
	if err := h.Blocks.Reorder(c.Request.Context(), req.Page, req.IDs); err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "block.reorder", TargetType: "block", TargetID: req.Page, After: gin.H{"ids": req.IDs},
	})
	respond.OK(c, gin.H{"page": req.Page, "ids": req.IDs})
}
