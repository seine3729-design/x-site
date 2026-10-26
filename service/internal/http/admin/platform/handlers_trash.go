package platform

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// TrashService 是回收站（domain/platform.TrashService）。
type TrashService interface {
	Overview(ctx context.Context) (dom.TrashOverview, error)
	List(ctx context.Context, f dom.TrashFilter) ([]dom.TrashItem, int64, int, int, error)
	Restore(ctx context.Context, category dom.TrashCategory, id int64) (dom.TrashItem, error)
	Purge(ctx context.Context, category dom.TrashCategory, id int64) (dom.TrashItem, error)
}

type trashListQuery struct {
	Category string `form:"category"`
	Q        string `form:"q"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// TrashOverview 是 GET /api/admin/trash：各分类的条数与保留期。
func (h *Handlers) TrashOverview(c *gin.Context) {
	out, err := h.Trash.Overview(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

// ListTrash 是 GET /api/admin/trash/items：某个分类下的一页，带到期时间与剩余天数。
func (h *Handlers) ListTrash(c *gin.Context) {
	var q trashListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	list, total, page, size, err := h.Trash.List(c.Request.Context(), dom.TrashFilter{
		Category: dom.TrashCategory(q.Category), Q: q.Q, Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"list": list, "total": total, "page": page, "page_size": size})
}

type trashActionRequest struct {
	Category string `json:"category"`
}

// RestoreTrash 是 POST /api/admin/trash/items/:id/restore：还原。
func (h *Handlers) RestoreTrash(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req trashActionRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	item, err := h.Trash.Restore(c.Request.Context(), dom.TrashCategory(req.Category), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "trash.restore", TargetType: req.Category, TargetID: strconv.FormatInt(id, 10),
		After: gin.H{"title": item.Title, "status": item.Status},
	})
	// 只返回还原后仍然有意义的字段：到期时间、剩余天数这些随删除标记一起没了
	respond.OK(c, gin.H{"id": item.ID, "title": item.Title, "slug": item.Slug, "status": item.Status})
}

// PurgeTrash 是 POST /api/admin/trash/items/:id/purge：彻底删除（不可恢复）。
//
// 用 POST 而不是 DELETE：请求体要带分类，而 DELETE 带请求体的支持在各层代理上不一致。
func (h *Handlers) PurgeTrash(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req trashActionRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	item, err := h.Trash.Purge(c.Request.Context(), dom.TrashCategory(req.Category), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	// 物理删除之后库里查不到了，审计里记下标题与附带内容数，便于回溯删了什么
	admin.SetAudit(c, admin.AuditDetail{
		Action: "trash.purge", TargetType: req.Category, TargetID: strconv.FormatInt(id, 10),
		Before: gin.H{"title": item.Title, "slug": item.Slug, "episodes": item.Extra, "deleted_at": item.DeletedAt},
	})
	respond.OK(c, item)
}
