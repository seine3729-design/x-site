package analytics

import (
	"strconv"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/analytics"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// Overview 是 GET /api/admin/analytics/overview?days=：数据看板概览。
// days 为空或非法时领域层回退默认（7 天），上限 90 天；窗口一律到昨天。
func (h *Handlers) Overview(c *gin.Context) {
	ov, err := h.Svc.Overview(c.Request.Context(), atoiOr(c.Query("days"), 0))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, ov)
}

// Dramas 是 GET /api/admin/analytics/dramas?days=&sort=&page=&page_size=：剧集分析列表。
// sort ∈ {viewers, play_starts, watch_seconds}，其它值归默认（viewers）。
func (h *Handlers) Dramas(c *gin.Context) {
	res, err := h.Svc.Dramas(c.Request.Context(), dom.DramasInput{
		Days:     atoiOr(c.Query("days"), 0),
		Sort:     c.Query("sort"),
		Page:     atoiOr(c.Query("page"), 0),
		PageSize: atoiOr(c.Query("page_size"), 0),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, res)
}

// DramaDetail 是 GET /api/admin/analytics/dramas/:id?days=：单剧分析（窗口汇总 + 每日序列 + 集间留存曲线）。
// 剧不存在返回 404。
func (h *Handlers) DramaDetail(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	detail, found, err := h.Svc.DramaDetail(c.Request.Context(), id, atoiOr(c.Query("days"), 0))
	if err != nil {
		respond.Error(c, err)
		return
	}
	if !found {
		respond.Error(c, errs.ErrNotFound)
		return
	}
	respond.OK(c, detail)
}

// atoiOr 解析十进制整数，失败返回 def（领域层统一做范围校验与回退）。
func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// Export 是 GET /api/admin/analytics/export?days=：把看板数据打包成给 AI 的中文简报文本（前端复制给 Agent）。
func (h *Handlers) Export(c *gin.Context) {
	pkg, err := h.Svc.Export(c.Request.Context(), atoiOr(c.Query("days"), 0))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pkg)
}
