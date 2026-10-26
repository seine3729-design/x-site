package pg

import (
	"context"
	"encoding/base64"

	"github.com/jackc/pgx/v5/pgtype"

	"xsite/service/internal/store/pg/sqlcgen"
)

// 后台二次验证 TOTP（S2-6）。totp_secret_enc 是 text 列，这里用 base64 承载信封加密后的二进制密文，
// 加解密在领域层（admin.MFAService 持 KEK 密钥环），仓储只管存取。

// SetTOTP 保存信封加密后的 TOTP 种子（enc 为二进制密文）。
func (r *AdminRepo) SetTOTP(ctx context.Context, adminID int64, enc []byte) error {
	b64 := base64.StdEncoding.EncodeToString(enc)
	return dbErr(r.q.SetAdminTOTP(ctx, sqlcgen.SetAdminTOTPParams{
		ID: adminID, TotpSecretEnc: pgtype.Text{String: b64, Valid: true},
	}), "保存二次验证密钥")
}

// GetTOTPSecret 读信封加密后的 TOTP 种子（二进制密文）；未绑定时返回 (nil, nil)。
func (r *AdminRepo) GetTOTPSecret(ctx context.Context, adminID int64) ([]byte, error) {
	row, err := r.q.GetAdminTOTPSecret(ctx, adminID)
	if err != nil {
		return nil, dbErr(err, "读取二次验证密钥")
	}
	if !row.Valid || row.String == "" {
		return nil, nil
	}
	enc, err := base64.StdEncoding.DecodeString(row.String)
	if err != nil {
		return nil, dbErr(err, "解析二次验证密钥")
	}
	return enc, nil
}

// ClearTOTP 解绑 TOTP。
func (r *AdminRepo) ClearTOTP(ctx context.Context, adminID int64) error {
	return dbErr(r.q.ClearAdminTOTP(ctx, adminID), "解绑二次验证")
}
