package storage

import (
	"github.com/gin-gonic/gin"

	"xsite/service/internal/http/respond"
)

// Health 是 GET /api/admin/storage/health：线路健康看板。返回每条线路的生效健康分与最近一次服务端探测明细
// （probe_enabled 为 false 时表示探测已关闭、分数仅为缺省值）。客户端质量指标待用户端上报接入后补充。只读、不记审计。
func (h *Handlers) Health(c *gin.Context) {
	view, err := h.HealthSvc.Overview(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, view)
}
