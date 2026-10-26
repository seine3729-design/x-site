package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/store/pg/sqlcgen"
)

// StorageRepo 实现 storage.Repo。
type StorageRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewStorageRepo(pool *pgxpool.Pool) *StorageRepo {
	return &StorageRepo{q: sqlcgen.New(pool), pool: pool}
}

func storageRowOf(r sqlcgen.StorageProvider) storage.Row {
	return storage.Row{
		ID: r.ID, Name: r.Name, Type: storage.Type(r.Type), Subtype: storage.Subtype(r.Subtype),
		Purpose: storage.Purpose(r.Purpose), Role: storage.Role(r.Role), Endpoint: r.Endpoint, Region: r.Region,
		Bucket: r.Bucket, AccountID: r.AccountID, PathStyle: r.PathStyle, PathPrefix: r.PathPrefix,
		CredentialEnc: r.CredentialEnc, ReadCredentialEnc: r.ReadCredentialEnc,
		CredTail: r.CredTail, ReadCredTail: r.ReadCredTail, MaxPresignTTL: r.MaxPresignTtl,
		Status: storage.Status(r.Status), CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func createParamsOf(in storage.Row) sqlcgen.CreateStorageProviderParams {
	return sqlcgen.CreateStorageProviderParams{
		Name: in.Name, Type: string(in.Type), Subtype: string(in.Subtype), Purpose: string(in.Purpose),
		Role: string(in.Role), Endpoint: in.Endpoint, Region: in.Region, Bucket: in.Bucket, AccountID: in.AccountID,
		PathStyle: in.PathStyle, PathPrefix: in.PathPrefix, CredentialEnc: in.CredentialEnc,
		ReadCredentialEnc: in.ReadCredentialEnc, CredTail: in.CredTail, ReadCredTail: in.ReadCredTail,
		MaxPresignTtl: in.MaxPresignTTL, Status: string(in.Status),
	}
}

func (r *StorageRepo) List(ctx context.Context, q storage.Query) ([]storage.Row, int64, error) {
	pattern, typ, status := nullText(q.Pattern), nullText(string(q.Type)), nullText(string(q.Status))
	rows, err := r.q.ListStorageProviders(ctx, sqlcgen.ListStorageProvidersParams{
		Pattern: pattern, Type: typ, Status: status, PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取存储源列表: %w", err)
	}
	total, err := r.q.CountStorageProviders(ctx, sqlcgen.CountStorageProvidersParams{Pattern: pattern, Type: typ, Status: status})
	if err != nil {
		return nil, 0, fmt.Errorf("统计存储源: %w", err)
	}
	out := make([]storage.Row, len(rows))
	for i, x := range rows {
		out[i] = storageRowOf(x)
	}
	return out, total, nil
}

func (r *StorageRepo) Get(ctx context.Context, id int64) (storage.Row, error) {
	row, err := r.q.GetStorageProvider(ctx, id)
	if err != nil {
		return storage.Row{}, dbErr(err, "读取存储源")
	}
	return storageRowOf(row), nil
}

func (r *StorageRepo) Create(ctx context.Context, in storage.Row) (storage.Row, error) {
	row, err := r.q.CreateStorageProvider(ctx, createParamsOf(in))
	if err != nil {
		return storage.Row{}, dbErr(err, "创建存储源")
	}
	return storageRowOf(row), nil
}

func (r *StorageRepo) Update(ctx context.Context, id int64, in storage.Row) (storage.Row, error) {
	p := createParamsOf(in)
	row, err := r.q.UpdateStorageProvider(ctx, sqlcgen.UpdateStorageProviderParams{
		Name: p.Name, Type: p.Type, Subtype: p.Subtype, Purpose: p.Purpose, Role: p.Role, Endpoint: p.Endpoint,
		Region: p.Region, Bucket: p.Bucket, AccountID: p.AccountID, PathStyle: p.PathStyle, PathPrefix: p.PathPrefix,
		CredentialEnc: p.CredentialEnc, ReadCredentialEnc: p.ReadCredentialEnc, CredTail: p.CredTail,
		ReadCredTail: p.ReadCredTail, MaxPresignTtl: p.MaxPresignTtl, Status: p.Status, ID: id,
	})
	if err != nil {
		return storage.Row{}, dbErr(err, "修改存储源")
	}
	return storageRowOf(row), nil
}

func (r *StorageRepo) Delete(ctx context.Context, id int64) (storage.Row, error) {
	row, err := r.q.DeleteStorageProvider(ctx, id)
	if err != nil {
		return storage.Row{}, dbErr(err, "删除存储源")
	}
	return storageRowOf(row), nil
}
