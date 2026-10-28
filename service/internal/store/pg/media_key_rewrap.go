package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/store/pg/sqlcgen"
)

// WrappedMediaKey 是 media_key 的一行：一集某个版本的内容密钥密文。
type WrappedMediaKey struct {
	EpisodeID int64
	KeyVer    int32
	Wrapped   []byte
}

// MediaKeyReplacement 是一次密文替换：Old 是读出时的密文，New 是重新包装后的密文。
type MediaKeyReplacement struct {
	EpisodeID int64
	KeyVer    int32
	Old, New  []byte
}

// MediaKeyRewrapRepo 供运维命令批量重新包装内容密钥（admintool rewrap-media-keys）。
type MediaKeyRewrapRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewMediaKeyRewrapRepo(pool *pgxpool.Pool) *MediaKeyRewrapRepo {
	return &MediaKeyRewrapRepo{pool: pool, q: sqlcgen.New(pool)}
}

// List 按主键顺序列出全部内容密钥密文。
func (r *MediaKeyRewrapRepo) List(ctx context.Context) ([]WrappedMediaKey, error) {
	rows, err := r.q.ListMediaKeysForRewrap(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取内容密钥: %w", err)
	}
	out := make([]WrappedMediaKey, len(rows))
	for i, row := range rows {
		out[i] = WrappedMediaKey{EpisodeID: row.EpisodeID, KeyVer: row.KeyVer, Wrapped: row.WrappedKey}
	}
	return out, nil
}

// Replace 在一个事务里替换密文。任何一行没对上（读出之后被改过或被删了）就整批回滚并报错，
// 不会留下一半新一半旧的状态；重新执行命令即可。
func (r *MediaKeyRewrapRepo) Replace(ctx context.Context, items []MediaKeyReplacement) error {
	return InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		for _, it := range items {
			n, err := q.RewrapMediaKey(ctx, sqlcgen.RewrapMediaKeyParams{
				NewWrappedKey: it.New, EpisodeID: it.EpisodeID, KeyVer: it.KeyVer, OldWrappedKey: it.Old,
			})
			if err != nil {
				return fmt.Errorf("替换第 %d 集第 %d 版内容密钥: %w", it.EpisodeID, it.KeyVer, err)
			}
			if n != 1 {
				return fmt.Errorf("第 %d 集第 %d 版内容密钥在读出后被改动或删除，已整批回滚，请重新执行", it.EpisodeID, it.KeyVer)
			}
		}
		return nil
	})
}
