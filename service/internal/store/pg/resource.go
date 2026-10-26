package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/resource"
	"xsite/service/internal/store/pg/sqlcgen"
)

// ResourceRepo 实现 resource.Repo。
type ResourceRepo struct {
	q *sqlcgen.Queries
}

func NewResourceRepo(pool *pgxpool.Pool) *ResourceRepo {
	return &ResourceRepo{q: sqlcgen.New(pool)}
}

func ftpServerOf(r sqlcgen.FtpServer) resource.FTPServer {
	return resource.FTPServer{
		ID: r.ID, Name: r.Name, Host: r.Host, Port: r.Port, TLS: resource.TLSMode(r.Tls), Passive: r.Passive,
		CredRef: r.CredRef, Status: resource.Status(r.Status), CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func (r *ResourceRepo) ListFTPServers(ctx context.Context, q resource.Query) ([]resource.FTPServer, int64, error) {
	pattern, status := nullText(q.Pattern), nullText(string(q.Status))
	rows, err := r.q.ListFTPServers(ctx, sqlcgen.ListFTPServersParams{
		Pattern: pattern, Status: status, PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取资源机列表: %w", err)
	}
	total, err := r.q.CountFTPServers(ctx, sqlcgen.CountFTPServersParams{Pattern: pattern, Status: status})
	if err != nil {
		return nil, 0, fmt.Errorf("统计资源机: %w", err)
	}
	out := make([]resource.FTPServer, len(rows))
	for i, x := range rows {
		out[i] = ftpServerOf(x)
	}
	return out, total, nil
}

func (r *ResourceRepo) GetFTPServer(ctx context.Context, id int64) (resource.FTPServer, error) {
	row, err := r.q.GetFTPServer(ctx, id)
	if err != nil {
		return resource.FTPServer{}, dbErr(err, "读取资源机")
	}
	return ftpServerOf(row), nil
}

func (r *ResourceRepo) CreateFTPServer(ctx context.Context, in resource.FTPServerInput) (resource.FTPServer, error) {
	row, err := r.q.CreateFTPServer(ctx, sqlcgen.CreateFTPServerParams{
		Name: in.Name, Host: in.Host, Port: in.Port, Tls: string(in.TLS), Passive: in.Passive, CredRef: in.CredRef, Status: string(in.Status),
	})
	if err != nil {
		return resource.FTPServer{}, dbErr(err, "创建资源机")
	}
	return ftpServerOf(row), nil
}

func (r *ResourceRepo) UpdateFTPServer(ctx context.Context, id int64, in resource.FTPServerInput) (resource.FTPServer, error) {
	row, err := r.q.UpdateFTPServer(ctx, sqlcgen.UpdateFTPServerParams{
		ID: id, Name: in.Name, Host: in.Host, Port: in.Port, Tls: string(in.TLS), Passive: in.Passive, CredRef: in.CredRef, Status: string(in.Status),
	})
	if err != nil {
		return resource.FTPServer{}, dbErr(err, "修改资源机")
	}
	return ftpServerOf(row), nil
}

func (r *ResourceRepo) DeleteFTPServer(ctx context.Context, id int64) (resource.FTPServer, error) {
	row, err := r.q.DeleteFTPServer(ctx, id)
	if err != nil {
		return resource.FTPServer{}, dbErr(err, "删除资源机")
	}
	return ftpServerOf(row), nil
}
