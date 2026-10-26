package admin

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	adm "xsite/service/internal/domain/admin"
	"xsite/service/internal/http/respond"
)

// MFAService 是后台二次验证服务（domain/admin.MFAService）。
type MFAService interface {
	BeginTOTP(ctx context.Context, adminID int64, username string) (adm.TOTPSetup, error)
	ConfirmTOTP(ctx context.Context, adminID int64, code string) error
}

type totpSetupDTO struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauth_url"`
}

type totpVerifyRequest struct {
	Code string `json:"code"`
}

// BeginTOTPSetup 是 POST /api/admin/mfa/totp/setup：为当前管理员生成待绑定的 TOTP 种子（返回二维码信息，尚未启用）。
func (h *Handlers) BeginTOTPSetup(c *gin.Context) {
	id, _ := CurrentAdmin(c)
	setup, err := h.MFA.BeginTOTP(c.Request.Context(), id.AdminID, id.Username)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, totpSetupDTO{Secret: setup.Secret, OtpauthURL: setup.OtpauthURL})
}

// VerifyTOTPSetup 是 POST /api/admin/mfa/totp/verify：用一次性验证码确认绑定，通过后启用 TOTP。
func (h *Handlers) VerifyTOTPSetup(c *gin.Context) {
	id, _ := CurrentAdmin(c)
	var req totpVerifyRequest
	if !Bind(c, &req) {
		return
	}
	if err := h.MFA.ConfirmTOTP(c.Request.Context(), id.AdminID, req.Code); err != nil {
		respond.Error(c, err)
		return
	}
	SetAudit(c, AuditDetail{Action: "admin.mfa_totp_enable", TargetType: "admin", TargetID: strconv.FormatInt(id.AdminID, 10)})
	respond.OK(c, nil)
}
