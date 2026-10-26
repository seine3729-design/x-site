package pg

import (
	"context"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/store/pg/sqlcgen"
)

func settingsOf(r sqlcgen.StorageSetting) storage.Settings {
	return storage.Settings{
		CandidateCount: r.CandidateCount, DefaultTTL: r.DefaultTtl, SmartRouting: r.SmartRouting,
		PrimaryVideoProviderID: r.PrimaryVideoProviderID.Int64, PrimaryImageProviderID: r.PrimaryImageProviderID.Int64,
		ReplicateOnUpload: r.ReplicateOnUpload, UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func (r *StorageRepo) GetSettings(ctx context.Context) (storage.Settings, error) {
	row, err := r.q.GetStorageSettings(ctx)
	if err != nil {
		return storage.Settings{}, dbErr(err, "读取存储设置")
	}
	return settingsOf(row), nil
}

func (r *StorageRepo) UpdateSettings(ctx context.Context, in storage.SettingsInput) (storage.Settings, error) {
	row, err := r.q.UpdateStorageSettings(ctx, sqlcgen.UpdateStorageSettingsParams{
		CandidateCount: in.CandidateCount, DefaultTtl: in.DefaultTTL, SmartRouting: in.SmartRouting,
		PrimaryVideoProviderID: nullID(in.PrimaryVideoProviderID), PrimaryImageProviderID: nullID(in.PrimaryImageProviderID),
		ReplicateOnUpload: in.ReplicateOnUpload,
	})
	if err != nil {
		return storage.Settings{}, dbErr(err, "保存存储设置")
	}
	return settingsOf(row), nil
}
