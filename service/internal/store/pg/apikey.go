package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// APIKeyRepo 实现 platform.APIKeyRepo（开放平台 API Key，docs/06 第 32 节）。
type APIKeyRepo struct {
	q *sqlcgen.Queries
}

func NewAPIKeyRepo(pool *pgxpool.Pool) *APIKeyRepo {
	return &APIKeyRepo{q: sqlcgen.New(pool)}
}

func (r *APIKeyRepo) ListAPIKeys(ctx context.Context, q platform.APIKeyQuery) ([]platform.APIKey, int64, error) {
	rows, err := r.q.ListAPIKeys(ctx, sqlcgen.ListAPIKeysParams{
		Pattern: q.Pattern, Status: string(q.Status), Lim: int32(q.Limit), Off: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取 API Key: %w", err)
	}
	total, err := r.q.CountAPIKeys(ctx, sqlcgen.CountAPIKeysParams{Pattern: q.Pattern, Status: string(q.Status)})
	if err != nil {
		return nil, 0, fmt.Errorf("统计 API Key: %w", err)
	}
	out := make([]platform.APIKey, len(rows))
	for i, x := range rows {
		out[i] = platform.APIKey{
			ID: x.ID, Name: x.Name, Owner: x.Owner, Hint: x.KeyHint, Scopes: scopesOf(x.Scopes),
			Status: platform.KeyStatus(x.Status), UsedCount: x.UsedCount, LastUsedAt: timePtr(x.LastUsedAt),
			CreatedBy: x.CreatedBy, CreatedAt: x.CreatedAt.Time, UpdatedAt: x.UpdatedAt.Time,
		}
	}
	return out, total, nil
}

func (r *APIKeyRepo) CreateAPIKey(
	ctx context.Context, in platform.APIKeyInput, keyHash, hint string, createdBy int64,
) (platform.APIKey, error) {
	row, err := r.q.CreateAPIKey(ctx, sqlcgen.CreateAPIKeyParams{
		Name: in.Name, Owner: in.Owner, KeyHash: keyHash, KeyHint: hint,
		Scopes: scopeStrings(in.Scopes), CreatedBy: createdBy,
	})
	if err != nil {
		return platform.APIKey{}, fmt.Errorf("生成 API Key: %w", err)
	}
	return platform.APIKey{
		ID: row.ID, Name: row.Name, Owner: row.Owner, Hint: row.KeyHint, Scopes: scopesOf(row.Scopes),
		Status: platform.KeyStatus(row.Status), UsedCount: row.UsedCount, LastUsedAt: timePtr(row.LastUsedAt),
		CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

func (r *APIKeyRepo) GetAPIKey(ctx context.Context, id int64) (platform.APIKey, error) {
	row, err := r.q.GetAPIKey(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.APIKey{}, errs.ErrNotFound
	}
	if err != nil {
		return platform.APIKey{}, fmt.Errorf("读取 API Key: %w", err)
	}
	return platform.APIKey{
		ID: row.ID, Name: row.Name, Owner: row.Owner, Hint: row.KeyHint, Scopes: scopesOf(row.Scopes),
		Status: platform.KeyStatus(row.Status), UsedCount: row.UsedCount, LastUsedAt: timePtr(row.LastUsedAt),
		CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

func (r *APIKeyRepo) UpdateAPIKey(ctx context.Context, id int64, name, owner string) (platform.APIKey, error) {
	row, err := r.q.UpdateAPIKey(ctx, sqlcgen.UpdateAPIKeyParams{ID: id, Name: name, Owner: owner})
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.APIKey{}, errs.ErrNotFound
	}
	if err != nil {
		return platform.APIKey{}, fmt.Errorf("修改 API Key: %w", err)
	}
	return platform.APIKey{
		ID: row.ID, Name: row.Name, Owner: row.Owner, Hint: row.KeyHint, Scopes: scopesOf(row.Scopes),
		Status: platform.KeyStatus(row.Status), UsedCount: row.UsedCount, LastUsedAt: timePtr(row.LastUsedAt),
		CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

func (r *APIKeyRepo) SetAPIKeyStatus(ctx context.Context, id int64, status platform.KeyStatus) (platform.APIKey, error) {
	row, err := r.q.SetAPIKeyStatus(ctx, sqlcgen.SetAPIKeyStatusParams{ID: id, Status: string(status)})
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.APIKey{}, errs.ErrNotFound
	}
	if err != nil {
		return platform.APIKey{}, fmt.Errorf("修改 API Key 状态: %w", err)
	}
	return platform.APIKey{
		ID: row.ID, Name: row.Name, Owner: row.Owner, Hint: row.KeyHint, Scopes: scopesOf(row.Scopes),
		Status: platform.KeyStatus(row.Status), UsedCount: row.UsedCount, LastUsedAt: timePtr(row.LastUsedAt),
		CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

func (r *APIKeyRepo) AuthenticateAPIKey(ctx context.Context, keyHash string) (int64, string, []platform.APIScope, error) {
	row, err := r.q.AuthenticateAPIKey(ctx, keyHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil, errs.ErrNotFound
	}
	if err != nil {
		return 0, "", nil, fmt.Errorf("校验 API Key: %w", err)
	}
	return row.ID, row.Name, scopesOf(row.Scopes), nil
}

func (r *APIKeyRepo) TouchAPIKeyUsage(ctx context.Context, id int64) error {
	if err := r.q.TouchAPIKeyUsage(ctx, id); err != nil {
		return fmt.Errorf("记录 API Key 调用: %w", err)
	}
	return nil
}

func scopesOf(list []string) []platform.APIScope {
	out := make([]platform.APIScope, len(list))
	for i, s := range list {
		out[i] = platform.APIScope(s)
	}
	return out
}

func scopeStrings(list []platform.APIScope) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = string(s)
	}
	return out
}
