package platform

import (
	"context"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/platform"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// FeatureService 是功能开关与年龄门（domain/platform.FeatureService）。
type FeatureService interface {
	Get(ctx context.Context) (dom.FeatureConfig, error)
	SetFlags(ctx context.Context, in map[string]bool) (dom.FeatureConfig, error)
	SetAgeGate(ctx context.Context, in dom.AgeGate) (dom.FeatureConfig, error)
}

// GetFeatures 是 GET /api/admin/features：全部开关（含名称与关闭影响说明）与年龄门配置。
func (h *Handlers) GetFeatures(c *gin.Context) {
	out, err := h.Features.Get(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

type flagsRequest struct {
	// Flags 是「开关编码 → 是否打开」。未传的开关按打开保存；传了未登记的编码返回 400。
	Flags map[string]bool `json:"flags"`
}

// SetFeatureFlags 是 PUT /api/admin/features/flags：整体保存功能开关。
func (h *Handlers) SetFeatureFlags(c *gin.Context) {
	var req flagsRequest
	if !admin.Bind(c, &req) {
		return
	}
	before, err := h.Features.Get(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out, err := h.Features.SetFlags(c.Request.Context(), req.Flags)
	if err != nil {
		respond.Error(c, err)
		return
	}
	// 审计里记开关的前后状态：关掉评论、注册这类开关会直接影响线上，要能查到是谁改的
	admin.SetAudit(c, admin.AuditDetail{
		Action: "site_config.features", TargetType: "site_config", TargetID: "features",
		Before: flagMap(before), After: flagMap(out),
	})
	respond.OK(c, out)
}

// SetAgeGate 是 PUT /api/admin/features/age-gate：整体保存年龄确认门。
func (h *Handlers) SetAgeGate(c *gin.Context) {
	var req dom.AgeGate
	if !admin.Bind(c, &req) {
		return
	}
	before, err := h.Features.Get(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out, err := h.Features.SetAgeGate(c.Request.Context(), req)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "site_config.age_gate", TargetType: "site_config", TargetID: "age_gate",
		Before: before.AgeGate, After: out.AgeGate,
	})
	respond.OK(c, out)
}

// flagMap 把开关列表压成「编码 → 是否打开」，审计里只记这个，不记名称与说明。
func flagMap(cfg dom.FeatureConfig) map[string]bool {
	out := make(map[string]bool, len(cfg.Flags))
	for _, f := range cfg.Flags {
		out[string(f.Flag)] = f.On
	}
	return out
}
