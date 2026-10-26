package pg

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/moderation"
	"xsite/service/internal/store/pg/sqlcgen"
)

// BlockwordRepo 实现 moderation.Repo（S2-4 屏蔽词）。
type BlockwordRepo struct {
	q *sqlcgen.Queries
}

func NewBlockwordRepo(pool *pgxpool.Pool) *BlockwordRepo {
	return &BlockwordRepo{q: sqlcgen.New(pool)}
}

var _ moderation.Repo = (*BlockwordRepo)(nil)

func (r *BlockwordRepo) List(ctx context.Context, q moderation.BlockwordQuery) ([]moderation.Blockword, int64, error) {
	list, err := r.q.ListBlockwords(ctx, sqlcgen.ListBlockwordsParams{
		Pattern: nullText(q.Pattern), Grp: nullText(q.Group), Action: nullText(q.Action),
		PageOffset: int32(q.Offset), PageLimit: int32(q.Limit),
	})
	if err != nil {
		return nil, 0, dbErr(err, "查询屏蔽词")
	}
	total, err := r.q.CountBlockwords(ctx, sqlcgen.CountBlockwordsParams{
		Pattern: nullText(q.Pattern), Grp: nullText(q.Group), Action: nullText(q.Action),
	})
	if err != nil {
		return nil, 0, dbErr(err, "统计屏蔽词")
	}
	out := make([]moderation.Blockword, len(list))
	for i, b := range list {
		out[i] = blockwordOf(b)
	}
	return out, total, nil
}

func (r *BlockwordRepo) Create(ctx context.Context, word, group, action string) (moderation.Blockword, error) {
	b, err := r.q.CreateBlockword(ctx, sqlcgen.CreateBlockwordParams{Word: word, Grp: group, Action: action})
	if err != nil {
		return moderation.Blockword{}, dbErr(err, "新增屏蔽词")
	}
	return blockwordOf(b), nil
}

func (r *BlockwordRepo) Delete(ctx context.Context, id int64) (moderation.Blockword, error) {
	b, err := r.q.DeleteBlockword(ctx, id)
	if err != nil {
		return moderation.Blockword{}, dbErr(err, "删除屏蔽词")
	}
	return blockwordOf(b), nil
}

func (r *BlockwordRepo) ListAll(ctx context.Context) ([]moderation.WordSpec, error) {
	rows, err := r.q.ListAllBlockwords(ctx)
	if err != nil {
		return nil, dbErr(err, "加载屏蔽词")
	}
	out := make([]moderation.WordSpec, len(rows))
	for i, row := range rows {
		out[i] = moderation.WordSpec{Word: row.Word, Group: row.Group, Action: moderation.Action(row.Action)}
	}
	return out, nil
}

func (r *BlockwordRepo) Groups(ctx context.Context) ([]string, error) {
	g, err := r.q.ListBlockwordGroups(ctx)
	if err != nil {
		return nil, dbErr(err, "查询屏蔽词分组")
	}
	return g, nil
}

func blockwordOf(b sqlcgen.Blockword) moderation.Blockword {
	return moderation.Blockword{
		ID: b.ID, Word: b.Word, Group: b.Group, Action: moderation.Action(b.Action),
		CreatedAt: b.CreatedAt.Time.UTC(),
	}
}
