package storage

import (
	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/storage"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type settingsReq struct {
	CandidateCount         int32 `json:"candidate_count"`
	DefaultTTL             int32 `json:"default_ttl"`
	SmartRouting           bool  `json:"smart_routing"`
	PrimaryVideoProviderID int64 `json:"primary_video_provider_id"`
	PrimaryImageProviderID int64 `json:"primary_image_provider_id"`
	ReplicateOnUpload      bool  `json:"replicate_on_upload"`
}

func (r settingsReq) input() dom.SettingsInput {
	return dom.SettingsInput{
		CandidateCount: r.CandidateCount, DefaultTTL: r.DefaultTTL, SmartRouting: r.SmartRouting,
		PrimaryVideoProviderID: r.PrimaryVideoProviderID, PrimaryImageProviderID: r.PrimaryImageProviderID,
		ReplicateOnUpload: r.ReplicateOnUpload,
	}
}

// GetSettings 是 GET /api/admin/storage/settings：读取存储全局设置（单行），
// 并附带交付自检 delivery：主存储没挂线路时前台会取不到新上传的资源，这里提前报出来。
func (h *Handlers) GetSettings(c *gin.Context) {
	s, err := h.SettingsSvc.Overview(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, s)
}

// UpdateSettings 是 PUT /api/admin/storage/settings：整体保存存储全局设置。
func (h *Handlers) UpdateSettings(c *gin.Context) {
	var req settingsReq
	if !admin.Bind(c, &req) {
		return
	}
	before, _ := h.SettingsSvc.Get(c.Request.Context())
	after, err := h.SettingsSvc.Update(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "storage_settings.update", TargetType: "storage_settings", TargetID: "1", Before: before, After: after})
	respond.OK(c, after)
}
