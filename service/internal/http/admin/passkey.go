package admin

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/gin-gonic/gin"

	adm "xsite/service/internal/domain/admin"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// PasskeyService 是后台二次验证 Passkey 服务（domain/admin.PasskeyService）。
type PasskeyService interface {
	BeginRegistration(ctx context.Context, adminID int64, username string) (json.RawMessage, error)
	FinishRegistration(ctx context.Context, adminID int64, username, name string, response []byte) error
	ListPasskeys(ctx context.Context, adminID int64) ([]adm.PasskeyInfo, error)
	DeletePasskey(ctx context.Context, adminID, id int64) error
	// BeginAssertion 为某管理员生成断言选项并把挑战暂存到 challengeKey（登录 / reauth 共用）。
	BeginAssertion(ctx context.Context, adminID int64, challengeKey string) (json.RawMessage, error)
}

type passkeyFinishRequest struct {
	Name     string          `json:"name" binding:"max=64"`
	Response json.RawMessage `json:"response" binding:"required"`
}

type passkeyLoginBeginRequest struct {
	MFATicket string `json:"mfa_ticket" binding:"required,max=64"`
}

// BeginPasskeyRegister 是 POST /api/admin/mfa/passkey/register/begin：生成注册选项（挑战暂存，待 finish）。
func (h *Handlers) BeginPasskeyRegister(c *gin.Context) {
	id, _ := CurrentAdmin(c)
	options, err := h.Passkey.BeginRegistration(c.Request.Context(), id.AdminID, id.Username)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, options)
}

// FinishPasskeyRegister 是 POST /api/admin/mfa/passkey/register/finish：校验注册响应并保存 Passkey。
func (h *Handlers) FinishPasskeyRegister(c *gin.Context) {
	id, _ := CurrentAdmin(c)
	var req passkeyFinishRequest
	if !Bind(c, &req) {
		return
	}
	if err := h.Passkey.FinishRegistration(c.Request.Context(), id.AdminID, id.Username, req.Name, req.Response); err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin.mfa_passkey_register", TargetType: "admin", TargetID: strconv.FormatInt(id.AdminID, 10)})
	respond.OK(c, nil)
}

// ListPasskeys 是 GET /api/admin/mfa/passkeys：列出当前管理员的 Passkey。
func (h *Handlers) ListPasskeys(c *gin.Context) {
	id, _ := CurrentAdmin(c)
	list, err := h.Passkey.ListPasskeys(c.Request.Context(), id.AdminID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, list)
}

// DeletePasskey 是 DELETE /api/admin/mfa/passkeys/:id：删除当前管理员的一把 Passkey。
func (h *Handlers) DeletePasskey(c *gin.Context) {
	id, _ := CurrentAdmin(c)
	pid, ok := PathID(c)
	if !ok {
		return
	}
	if err := h.Passkey.DeletePasskey(c.Request.Context(), id.AdminID, pid); err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin.mfa_passkey_delete", TargetType: "admin", TargetID: strconv.FormatInt(id.AdminID, 10)})
	respond.OK(c, nil)
}

// BeginLoginPasskey 是 POST /api/admin/login/passkey/begin：登录第二步用 Passkey 时先取断言选项（公开，凭票据）。
func (h *Handlers) BeginLoginPasskey(c *gin.Context) {
	var req passkeyLoginBeginRequest
	if !Bind(c, &req) {
		return
	}
	adminID, err := h.Auth.MFATicketAdmin(c.Request.Context(), req.MFATicket)
	if err != nil {
		respond.Error(c, err)
		return
	}
	options, err := h.Passkey.BeginAssertion(c.Request.Context(), adminID, adm.PasskeyLoginKey(req.MFATicket))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, options)
}

// BeginReauthPasskey 是 POST /api/admin/reauth/passkey/begin：高敏重验用 Passkey 时先取断言选项（已登录）。
func (h *Handlers) BeginReauthPasskey(c *gin.Context) {
	id, _ := CurrentAdmin(c)
	if !id.MFAEnabled {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请先在账号安全绑定二次验证"))
		return
	}
	options, err := h.Passkey.BeginAssertion(c.Request.Context(), id.AdminID, adm.PasskeyReauthKey(id.SessionKey))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, options)
}
