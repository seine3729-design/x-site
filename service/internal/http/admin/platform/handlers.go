package platform

import (
	"strconv"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/platform"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type purgeRequest struct {
	Scope   string `json:"scope"`
	DramaID int64  `json:"drama_id"`
}

// CacheOverview 是 GET /api/admin/cache：站点版本、键数量与各前缀占用、可选的清理范围。
func (h *Handlers) CacheOverview(c *gin.Context) {
	out, err := h.Cache.Overview(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

// PurgeCache 是 POST /api/admin/cache/purge：按范围清缓存。
//
// 清「站点设置」或「全部」会让站点版本 +1，所以审计里记下新版本，方便回溯是谁作废了配置。
func (h *Handlers) PurgeCache(c *gin.Context) {
	var req purgeRequest
	if !admin.Bind(c, &req) {
		return
	}
	res, err := h.Cache.Purge(c.Request.Context(), dom.PurgeScope(req.Scope), req.DramaID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	detail := admin.AuditDetail{Action: "cache.purge", TargetType: "cache", TargetID: req.Scope, After: res}
	if req.DramaID > 0 {
		detail.TargetID = req.Scope + ":" + strconv.FormatInt(req.DramaID, 10)
	}
	admin.SetAudit(c, detail)
	respond.OK(c, res)
}
