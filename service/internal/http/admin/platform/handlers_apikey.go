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

// APIKeyService 是开放平台 API Key 管理（domain/platform.APIKeyService）。
type APIKeyService interface {
	List(ctx context.Context, f dom.APIKeyFilter) ([]dom.APIKey, int64, int, int, error)
	Create(ctx context.Context, in dom.APIKeyInput, createdBy int64) (dom.NewAPIKey, error)
	Update(ctx context.Context, id int64, in dom.APIKeyInput) (dom.APIKey, error)
	SetStatus(ctx context.Context, id int64, status dom.KeyStatus) (dom.APIKey, error)
	Get(ctx context.Context, id int64) (dom.APIKey, error)
}

type apiKeyListQuery struct {
	Q        string `form:"q"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// ListAPIKeys 是 GET /api/admin/apikeys：搜用途、使用方或密钥开头几位；按状态筛选。
// 响应里只有 hint，没有完整密钥 —— 库里也只存哈希。
func (h *Handlers) ListAPIKeys(c *gin.Context) {
	var q apiKeyListQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	list, total, page, size, err := h.APIKeys.List(c.Request.Context(), dom.APIKeyFilter{
		Q: q.Q, Status: dom.KeyStatus(q.Status), Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"list": list, "total": total, "page": page, "page_size": size, "scopes": dom.APIScopes()})
}

type apiKeyRequest struct {
	Name   string   `json:"name"`
	Owner  string   `json:"owner"`
	Scopes []string `json:"scopes"`
}

func (r apiKeyRequest) input() dom.APIKeyInput {
	scopes := make([]dom.APIScope, len(r.Scopes))
	for i, s := range r.Scopes {
		scopes[i] = dom.APIScope(s)
	}
	return dom.APIKeyInput{Name: r.Name, Owner: r.Owner, Scopes: scopes}
}

// CreateAPIKey 是 POST /api/admin/apikeys：生成一把密钥。
//
// 高敏操作（apikeys.edit，迁移 00003 标了 sensitive）。响应里的 secret 是**唯一一次**能拿到完整密钥的机会：
// 库里只存哈希，后台列表、审计、日志都取不回来。
func (h *Handlers) CreateAPIKey(c *gin.Context) {
	var req apiKeyRequest
	if !admin.Bind(c, &req) {
		return
	}
	actor, _ := admin.CurrentAdmin(c)
	out, err := h.APIKeys.Create(c.Request.Context(), req.input(), actor.AdminID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	// 审计只记元数据与开头几位。完整密钥不进审计：审计是给人看的，密钥一旦落在那里就等于多了一处泄露面。
	admin.SetAudit(c, admin.AuditDetail{
		Action: "api_key.create", TargetType: "api_key", TargetID: strconv.FormatInt(out.ID, 10),
		After: gin.H{"name": out.Name, "owner": out.Owner, "hint": out.Hint, "scopes": out.Scopes},
	})
	respond.OK(c, out)
}

// UpdateAPIKey 是 PATCH /api/admin/apikeys/:id：改用途与使用方（密钥与权限范围不可改）。
func (h *Handlers) UpdateAPIKey(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req apiKeyRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, err := h.APIKeys.Get(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	out, err := h.APIKeys.Update(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "api_key.update", TargetType: "api_key", TargetID: strconv.FormatInt(id, 10),
		Before: gin.H{"name": before.Name, "owner": before.Owner},
		After:  gin.H{"name": out.Name, "owner": out.Owner},
	})
	respond.OK(c, out)
}

// DisableAPIKey 是 POST /api/admin/apikeys/:id/disable：停用（即时失效）。
func (h *Handlers) DisableAPIKey(c *gin.Context) {
	h.setAPIKeyStatus(c, dom.KeyInactive, "api_key.disable")
}

// EnableAPIKey 是 POST /api/admin/apikeys/:id/enable：重新启用。
func (h *Handlers) EnableAPIKey(c *gin.Context) {
	h.setAPIKeyStatus(c, dom.KeyActive, "api_key.enable")
}

func (h *Handlers) setAPIKeyStatus(c *gin.Context, status dom.KeyStatus, action string) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	out, err := h.APIKeys.SetStatus(c.Request.Context(), id, status)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: action, TargetType: "api_key", TargetID: strconv.FormatInt(id, 10),
		After: gin.H{"name": out.Name, "hint": out.Hint, "status": out.Status},
	})
	respond.OK(c, out)
}
