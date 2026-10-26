package pg

import (
	"context"
	"fmt"
	"time"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/store/pg/sqlcgen"

	"github.com/jackc/pgx/v5/pgtype"
)

// 媒体副本（S1-6f）：实现 storage.ReplicaRepo，挂在 StorageRepo 上（与存储源、线路同属存储域）。

func replicaOf(r sqlcgen.MediaReplica) storage.Replica {
	return storage.Replica{
		ObjectID: r.ObjectID, ProviderID: r.ProviderID, Status: storage.ReplicaStatus(r.Status), Error: r.Error,
		ReplicatedAt: utcPtr(r.ReplicatedAt), CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func (r *StorageRepo) SaveReplica(
	ctx context.Context, objectID, providerID int64, status storage.ReplicaStatus, errMsg string,
) (storage.Replica, error) {
	row, err := r.q.UpsertMediaReplica(ctx, sqlcgen.UpsertMediaReplicaParams{
		ObjectID: objectID, ProviderID: providerID, Status: string(status), Error: errMsg,
	})
	if err != nil {
		return storage.Replica{}, dbErr(err, "记录媒体副本")
	}
	return replicaOf(row), nil
}

func (r *StorageRepo) ListReplicas(ctx context.Context, objectID int64) ([]storage.Replica, error) {
	rows, err := r.q.ListMediaReplicas(ctx, objectID)
	if err != nil {
		return nil, fmt.Errorf("读取媒体副本: %w", err)
	}
	out := make([]storage.Replica, len(rows))
	for i, x := range rows {
		out[i] = replicaOf(x)
	}
	return out, nil
}

func (r *StorageRepo) ReadyProviders(ctx context.Context, objectID int64) ([]int64, error) {
	ids, err := r.q.ListReadyProviders(ctx, objectID)
	if err != nil {
		return nil, fmt.Errorf("读取对象 %d 的可用存储源: %w", objectID, err)
	}
	return ids, nil
}

func (r *StorageRepo) CountReplicasByProvider(ctx context.Context, providerID int64) (int64, error) {
	n, err := r.q.CountReplicasByProvider(ctx, providerID)
	if err != nil {
		return 0, fmt.Errorf("统计存储源 %d 的副本: %w", providerID, err)
	}
	return n, nil
}

func (r *StorageRepo) GetObject(ctx context.Context, objectID int64) (storage.MediaObject, error) {
	row, err := r.q.GetMediaObject(ctx, objectID)
	if err != nil {
		return storage.MediaObject{}, dbErr(err, "读取媒体对象")
	}
	return storage.MediaObject{
		ID: row.ID, Kind: storage.ObjectKind(row.Kind), Key: row.Key, SizeBytes: row.SizeBytes,
	}, nil
}

func (r *StorageRepo) DeleteReplica(ctx context.Context, objectID, providerID int64) error {
	if _, err := r.q.DeleteMediaReplica(ctx, sqlcgen.DeleteMediaReplicaParams{
		ObjectID: objectID, ProviderID: providerID,
	}); err != nil {
		return dbErr(err, "删除媒体副本")
	}
	return nil
}

func (r *StorageRepo) ListObjectIDs(ctx context.Context, q storage.ObjectQuery) ([]int64, error) {
	ids, err := r.q.ListMediaObjectIDs(ctx, sqlcgen.ListMediaObjectIDsParams{
		DramaID: q.DramaID, ProviderID: q.ProviderID, CursorID: q.CursorID, PageLimit: int32(q.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("列出媒体对象: %w", err)
	}
	return ids, nil
}

func (r *StorageRepo) ListOrphanObjectIDs(ctx context.Context, cursorID int64, limit int) ([]int64, error) {
	ids, err := r.q.ListOrphanObjectIDs(ctx, sqlcgen.ListOrphanObjectIDsParams{
		CursorID: cursorID, PageLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("列出没有副本的媒体对象: %w", err)
	}
	return ids, nil
}

// ---------- 孤立产物的标记与清理（S1-6f）----------

func (r *StorageRepo) MarkOrphans(ctx context.Context) (int64, error) {
	n, err := r.q.MarkOrphanObjects(ctx)
	if err != nil {
		return 0, fmt.Errorf("标记孤立产物: %w", err)
	}
	return n, nil
}

func (r *StorageRepo) UnmarkOrphans(ctx context.Context) (int64, error) {
	n, err := r.q.UnmarkOrphanObjects(ctx)
	if err != nil {
		return 0, fmt.Errorf("撤销孤立标记: %w", err)
	}
	return n, nil
}

func (r *StorageRepo) ListExpiredOrphans(
	ctx context.Context, before time.Time, cursorID int64, limit int,
) ([]storage.OrphanObject, error) {
	rows, err := r.q.ListExpiredOrphanObjects(ctx, sqlcgen.ListExpiredOrphanObjectsParams{
		Before: pgtype.Timestamptz{Time: before, Valid: true}, CursorID: cursorID, PageLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("列出过期的孤立产物: %w", err)
	}
	out := make([]storage.OrphanObject, len(rows))
	for i, x := range rows {
		out[i] = storage.OrphanObject{
			ID: x.ID, Kind: storage.ObjectKind(x.Kind), Key: x.Key, SizeBytes: x.SizeBytes,
			OrphanedAt: x.OrphanedAt.Time.UTC(),
		}
	}
	return out, nil
}

func (r *StorageRepo) OrphanStats(ctx context.Context, before time.Time) (storage.OrphanStats, error) {
	row, err := r.q.CountOrphanObjects(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return storage.OrphanStats{}, fmt.Errorf("统计孤立产物: %w", err)
	}
	return storage.OrphanStats{Total: row.Total, Expired: row.Expired, Bytes: row.Bytes}, nil
}

func (r *StorageRepo) DeleteObject(ctx context.Context, objectID int64) (bool, error) {
	n, err := r.q.DeleteMediaObject(ctx, objectID)
	if err != nil {
		return false, dbErr(err, "删除媒体对象")
	}
	return n > 0, nil
}
