package pg

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/moderation"
	"xsite/service/internal/store/pg/sqlcgen"
)

// EmojiRepo 实现 moderation.EmojiRepo（S2-5）。
type EmojiRepo struct {
	q *sqlcgen.Queries
}

func NewEmojiRepo(pool *pgxpool.Pool) *EmojiRepo {
	return &EmojiRepo{q: sqlcgen.New(pool)}
}

var _ moderation.EmojiRepo = (*EmojiRepo)(nil)

func (r *EmojiRepo) ListPacks(ctx context.Context) ([]moderation.EmojiPack, []moderation.Emoji, error) {
	prows, err := r.q.ListEmojiPacks(ctx)
	if err != nil {
		return nil, nil, dbErr(err, "读取表情包")
	}
	erows, err := r.q.ListEmojis(ctx)
	if err != nil {
		return nil, nil, dbErr(err, "读取表情")
	}
	packs := make([]moderation.EmojiPack, len(prows))
	for i, p := range prows {
		packs[i] = moderation.EmojiPack{ID: p.ID, Name: p.Name, Sort: int(p.Sort), CreatedAt: p.CreatedAt.Time.UTC()}
	}
	emojis := make([]moderation.Emoji, len(erows))
	for i, e := range erows {
		emojis[i] = emojiOf(e)
	}
	return packs, emojis, nil
}

func (r *EmojiRepo) CreatePack(ctx context.Context, name string, sort int) (moderation.EmojiPack, error) {
	p, err := r.q.CreateEmojiPack(ctx, sqlcgen.CreateEmojiPackParams{Name: name, Sort: int32(sort)})
	if err != nil {
		return moderation.EmojiPack{}, dbErr(err, "新建表情包")
	}
	return packOf(p), nil
}

func (r *EmojiRepo) UpdatePack(ctx context.Context, id int64, name string, sort int) (moderation.EmojiPack, error) {
	p, err := r.q.UpdateEmojiPack(ctx, sqlcgen.UpdateEmojiPackParams{ID: id, Name: name, Sort: int32(sort)})
	if err != nil {
		return moderation.EmojiPack{}, dbErr(err, "修改表情包")
	}
	return packOf(p), nil
}

func (r *EmojiRepo) DeletePack(ctx context.Context, id int64) (moderation.EmojiPack, error) {
	p, err := r.q.DeleteEmojiPack(ctx, id)
	if err != nil {
		return moderation.EmojiPack{}, dbErr(err, "删除表情包")
	}
	return packOf(p), nil
}

func (r *EmojiRepo) CreateEmoji(ctx context.Context, packID int64, code, image string, sort int) (moderation.Emoji, error) {
	e, err := r.q.CreateEmoji(ctx, sqlcgen.CreateEmojiParams{PackID: packID, Code: code, Image: image, Sort: int32(sort)})
	if err != nil {
		return moderation.Emoji{}, dbErr(err, "新建表情")
	}
	return emojiOf(e), nil
}

func (r *EmojiRepo) UpdateEmoji(ctx context.Context, id int64, code, image string, sort int) (moderation.Emoji, error) {
	e, err := r.q.UpdateEmoji(ctx, sqlcgen.UpdateEmojiParams{ID: id, Code: code, Image: image, Sort: int32(sort)})
	if err != nil {
		return moderation.Emoji{}, dbErr(err, "修改表情")
	}
	return emojiOf(e), nil
}

func (r *EmojiRepo) DeleteEmoji(ctx context.Context, id int64) (moderation.Emoji, error) {
	e, err := r.q.DeleteEmoji(ctx, id)
	if err != nil {
		return moderation.Emoji{}, dbErr(err, "删除表情")
	}
	return emojiOf(e), nil
}

func packOf(p sqlcgen.EmojiPack) moderation.EmojiPack {
	return moderation.EmojiPack{ID: p.ID, Name: p.Name, Sort: int(p.Sort), CreatedAt: p.CreatedAt.Time.UTC()}
}

func emojiOf(e sqlcgen.Emoji) moderation.Emoji {
	return moderation.Emoji{ID: e.ID, PackID: e.PackID, Code: e.Code, Image: e.Image, Sort: int(e.Sort), CreatedAt: e.CreatedAt.Time.UTC()}
}
