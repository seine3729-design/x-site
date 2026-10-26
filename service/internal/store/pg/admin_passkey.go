package pg

import (
	"context"
	"time"

	"xsite/service/internal/domain/admin"
	"xsite/service/internal/store/pg/sqlcgen"
)

// 后台二次验证 Passkey（S2-6）。凭据按 docs/02 的列落库；登录断言时由领域层重建 go-webauthn 凭据校验。

// Insert 写入一把新注册的 Passkey，返回自增 id 与创建时间。
func (r *AdminRepo) Insert(ctx context.Context, adminID int64, c admin.PasskeyCredential) (int64, time.Time, error) {
	row, err := r.q.InsertAdminPasskey(ctx, sqlcgen.InsertAdminPasskeyParams{
		AdminID:      adminID,
		CredentialID: c.CredentialID,
		PublicKey:    c.PublicKey,
		SignCount:    int64(c.SignCount),
		Transports:   c.Transports,
		Name:         c.Name,
	})
	if err != nil {
		return 0, time.Time{}, dbErr(err, "注册 Passkey")
	}
	return row.ID, row.CreatedAt.Time, nil
}

// List 返回某管理员的全部 Passkey（按 id 升序）。
func (r *AdminRepo) List(ctx context.Context, adminID int64) ([]admin.PasskeyCredential, error) {
	rows, err := r.q.ListAdminPasskeys(ctx, adminID)
	if err != nil {
		return nil, dbErr(err, "读取 Passkey")
	}
	out := make([]admin.PasskeyCredential, len(rows))
	for i, row := range rows {
		out[i] = admin.PasskeyCredential{
			ID: row.ID, CredentialID: row.CredentialID, PublicKey: row.PublicKey,
			SignCount: uint32(row.SignCount), Transports: row.Transports, Name: row.Name,
			CreatedAt: row.CreatedAt.Time, LastUsedAt: optTime(row.LastUsedAt),
		}
	}
	return out, nil
}

// UpdateOnLogin 断言校验通过后写回计数与最近使用时间。
func (r *AdminRepo) UpdateOnLogin(ctx context.Context, credentialID []byte, signCount uint32) error {
	return dbErr(r.q.UpdateAdminPasskeyOnLogin(ctx, sqlcgen.UpdateAdminPasskeyOnLoginParams{
		SignCount: int64(signCount), CredentialID: credentialID,
	}), "更新 Passkey")
}

// Delete 删除本人的一把 Passkey（限定 admin_id），返回是否删除。
func (r *AdminRepo) Delete(ctx context.Context, adminID, id int64) (bool, error) {
	n, err := r.q.DeleteAdminPasskey(ctx, sqlcgen.DeleteAdminPasskeyParams{ID: id, AdminID: adminID})
	if err != nil {
		return false, dbErr(err, "删除 Passkey")
	}
	return n > 0, nil
}

// Count 返回某管理员已注册的 Passkey 数量。
func (r *AdminRepo) Count(ctx context.Context, adminID int64) (int, error) {
	n, err := r.q.CountAdminPasskeys(ctx, adminID)
	if err != nil {
		return 0, dbErr(err, "统计 Passkey")
	}
	return int(n), nil
}

// DeleteByAdmin 清空某管理员的全部 Passkey（重置二次验证）。
func (r *AdminRepo) DeleteByAdmin(ctx context.Context, adminID int64) error {
	return dbErr(r.q.DeleteAdminPasskeysByAdmin(ctx, adminID), "清空 Passkey")
}

// ClearMFA 清除某管理员的全部二次验证因子（TOTP + Passkey），用于重置二次验证（admin.ManageRepo）。
func (r *AdminRepo) ClearMFA(ctx context.Context, adminID int64) error {
	if err := r.ClearTOTP(ctx, adminID); err != nil {
		return err
	}
	return r.DeleteByAdmin(ctx, adminID)
}
