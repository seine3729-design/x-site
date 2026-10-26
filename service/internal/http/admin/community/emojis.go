package community

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type emojiPackDTO struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Sort      int        `json:"sort"`
	CreatedAt time.Time  `json:"created_at"`
	Emojis    []emojiDTO `json:"emojis"`
}

type emojiDTO struct {
	ID        int64     `json:"id"`
	PackID    int64     `json:"pack_id"`
	Code      string    `json:"code"`
	Image     string    `json:"image"`
	Sort      int       `json:"sort"`
	CreatedAt time.Time `json:"created_at"`
}

type emojiPackRequest struct {
	Name string `json:"name"`
	Sort int    `json:"sort"`
}

type emojiItemRequest struct {
	Code  string `json:"code"`
	Image string `json:"image"`
	Sort  int    `json:"sort"`
}

func packDTOf(p dom.EmojiPack) emojiPackDTO {
	// emojis 默认空数组而非 null：AdminEmojiPack schema 里 emojis 是必填数组（新建 / 修改 / 删除回执都遵守）。
	return emojiPackDTO{ID: p.ID, Name: p.Name, Sort: p.Sort, CreatedAt: p.CreatedAt, Emojis: []emojiDTO{}}
}

func emojiDTOf(e dom.Emoji) emojiDTO {
	return emojiDTO{ID: e.ID, PackID: e.PackID, Code: e.Code, Image: e.Image, Sort: e.Sort, CreatedAt: e.CreatedAt}
}

// ListEmojis 是 GET /api/admin/emojis：全部表情包（含各自的表情，按 sort）。
func (h *Handlers) ListEmojis(c *gin.Context) {
	list, err := h.Emojis.List(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	packs := make([]emojiPackDTO, len(list))
	for i, pe := range list {
		dto := packDTOf(pe.Pack)
		dto.Emojis = mapSlice(pe.Emojis, emojiDTOf)
		packs[i] = dto
	}
	respond.OK(c, gin.H{"packs": packs})
}

// CreatePack 是 POST /api/admin/emojis/packs：新建表情包。
func (h *Handlers) CreatePack(c *gin.Context) {
	var req emojiPackRequest
	if !admin.Bind(c, &req) {
		return
	}
	p, err := h.Emojis.CreatePack(c.Request.Context(), req.Name, req.Sort)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := packDTOf(p)
	admin.SetAudit(c, admin.AuditDetail{Action: "emoji_pack.create", TargetType: "emoji_pack", TargetID: itoa(p.ID), After: out})
	respond.OK(c, out)
}

// UpdatePack 是 PATCH /api/admin/emojis/packs/:id：改表情包名与排序。
func (h *Handlers) UpdatePack(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	var req emojiPackRequest
	if !admin.Bind(c, &req) {
		return
	}
	p, err := h.Emojis.UpdatePack(c.Request.Context(), id, req.Name, req.Sort)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := packDTOf(p)
	admin.SetAudit(c, admin.AuditDetail{Action: "emoji_pack.update", TargetType: "emoji_pack", TargetID: itoa(id), After: out})
	respond.OK(c, out)
}

// DeletePack 是 DELETE /api/admin/emojis/packs/:id：删表情包（级联删其表情）。
func (h *Handlers) DeletePack(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	p, err := h.Emojis.DeletePack(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "emoji_pack.delete", TargetType: "emoji_pack", TargetID: itoa(id), Before: packDTOf(p)})
	respond.OK(c, nil)
}

// CreateEmojiInPack 是 POST /api/admin/emojis/packs/:id/emojis：往某个表情包里加表情。
func (h *Handlers) CreateEmojiInPack(c *gin.Context) {
	packID, ok := admin.PathID(c)
	if !ok {
		return
	}
	var req emojiItemRequest
	if !admin.Bind(c, &req) {
		return
	}
	e, err := h.Emojis.CreateEmoji(c.Request.Context(), packID, req.Code, req.Image, req.Sort)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := emojiDTOf(e)
	admin.SetAudit(c, admin.AuditDetail{Action: "emoji.create", TargetType: "emoji", TargetID: itoa(e.ID), After: out})
	respond.OK(c, out)
}

// UpdateEmoji 是 PATCH /api/admin/emojis/items/:id：改表情的码 / 图 / 排序。
func (h *Handlers) UpdateEmoji(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	var req emojiItemRequest
	if !admin.Bind(c, &req) {
		return
	}
	e, err := h.Emojis.UpdateEmoji(c.Request.Context(), id, req.Code, req.Image, req.Sort)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := emojiDTOf(e)
	admin.SetAudit(c, admin.AuditDetail{Action: "emoji.update", TargetType: "emoji", TargetID: itoa(id), After: out})
	respond.OK(c, out)
}

// DeleteEmoji 是 DELETE /api/admin/emojis/items/:id：删表情。
func (h *Handlers) DeleteEmoji(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	e, err := h.Emojis.DeleteEmoji(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "emoji.delete", TargetType: "emoji", TargetID: itoa(id), Before: emojiDTOf(e)})
	respond.OK(c, nil)
}
