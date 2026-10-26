package content

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/resourcecheck"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// ResourceCheckService 是资源体检（domain/resourcecheck.Service）。
type ResourceCheckService interface {
	List(ctx context.Context, f dom.Filter) (dom.Overview, error)
	Run(ctx context.Context, req dom.RunRequest) (dom.RunResult, error)
	Clear(ctx context.Context, dramaID int64) (int64, error)
}

type resourceCheckQuery struct {
	DramaID int64  `form:"drama_id"`
	BadOnly bool   `form:"bad_only"`
	Q       string `form:"q"`
	Limit   int    `form:"limit"`
}

// ListResourceChecks 是 GET /api/admin/resource-checks：体检结果。
//
// 不分页（docs/03 第 9.4 节「最多展示 200 条」）：运营的用法是筛「只看打不开」逐条去修，
// 超出上限时响应里的 total 会告诉他还有多少没显示。
func (h *Handlers) ListResourceChecks(c *gin.Context) {
	var q resourceCheckQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	out, err := h.ResourceChecks.List(c.Request.Context(), dom.Filter{
		DramaID: q.DramaID, BadOnly: q.BadOnly, Q: q.Q, Limit: q.Limit,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

type runCheckRequest struct {
	// DramaID 为 0 表示全部剧。
	DramaID int64 `json:"drama_id"`
	// IncludeUnpublished 只在全部剧时有意义。
	IncludeUnpublished bool `json:"include_unpublished"`
}

// RunResourceCheck 是 POST /api/admin/resource-checks/run：发起体检。
//
// 立即返回、不等结果：全站体检是分钟级的事，由 worker 按剧分片跑，页面轮询列表看进展。
func (h *Handlers) RunResourceCheck(c *gin.Context) {
	var req runCheckRequest
	if !admin.Bind(c, &req) {
		return
	}
	out, err := h.ResourceChecks.Run(c.Request.Context(), dom.RunRequest{
		DramaID: req.DramaID, IncludeUnpublished: req.IncludeUnpublished,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "resource_check.run", TargetType: "resource_check",
		TargetID: strconv.FormatInt(req.DramaID, 10),
		After: gin.H{
			"drama_id": req.DramaID, "include_unpublished": req.IncludeUnpublished,
			"dramas": out.Dramas, "queued": out.Queued,
		},
	})
	respond.OK(c, out)
}

type clearCheckRequest struct {
	// DramaID 为 0 表示清全部。
	DramaID int64 `json:"drama_id"`
}

// ClearResourceChecks 是 POST /api/admin/resource-checks/clear：清空结果。
//
// **只清体检表，不动源文件**（docs/06 第 9 节）。清掉的只是「上次探测的结论」，
// 重新体检就会再长出来。
func (h *Handlers) ClearResourceChecks(c *gin.Context) {
	var req clearCheckRequest
	if !admin.Bind(c, &req) {
		return
	}
	n, err := h.ResourceChecks.Clear(c.Request.Context(), req.DramaID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "resource_check.clear", TargetType: "resource_check",
		TargetID: strconv.FormatInt(req.DramaID, 10),
		After:    gin.H{"drama_id": req.DramaID, "deleted": n},
	})
	respond.OK(c, gin.H{"deleted": n})
}
