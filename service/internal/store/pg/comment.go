package pg

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/moderation"
	"xsite/service/internal/store/pg/sqlcgen"
)

// CommentRepo 实现 moderation.CommentRepo（S2-5）。
type CommentRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewCommentRepo(pool *pgxpool.Pool) *CommentRepo {
	return &CommentRepo{pool: pool, q: sqlcgen.New(pool)}
}

var _ moderation.CommentRepo = (*CommentRepo)(nil)

func (r *CommentRepo) Create(ctx context.Context, in moderation.NewComment) (moderation.Comment, error) {
	p := sqlcgen.CreateCommentParams{
		EpisodeID: in.EpisodeID, Content: in.Content, Status: in.Status,
		ParentID: nullID(in.ParentID),
	}
	if in.Author.IsGuest() {
		p.GuestDeviceID = pgUUID(in.Author.GuestID)
	} else {
		p.UserID = nullID(in.Author.UserID)
	}
	row, err := r.q.CreateComment(ctx, p)
	if err != nil {
		return moderation.Comment{}, dbErr(err, "发表评论")
	}
	return moderation.Comment{
		ID: row.ID, EpisodeID: row.EpisodeID, ParentID: row.ParentID.Int64,
		Content: row.Content, Status: row.Status, LikeCount: row.LikeCount,
		CreatedAt: row.CreatedAt.Time.UTC(),
	}, nil
}

func (r *CommentRepo) List(ctx context.Context, episodeID int64, hot bool, cursor string, limit int) ([]moderation.CommentView, string, error) {
	type rowT struct {
		ID         int64
		ParentID   pgtype.Int8
		Content    string
		ImageCount int32
		LikeCount  int64
		CreatedAt  pgtype.Timestamptz
		UserID     pgtype.Int8
		Nickname   string
		Avatar     string
	}
	var rows []rowT
	if hot {
		curLike, curID := parseHotCursor(cursor)
		got, err := r.q.ListCommentsHot(ctx, sqlcgen.ListCommentsHotParams{
			EpisodeID: episodeID, CursorLike: curLike, CursorID: curID, Lim: int32(limit),
		})
		if err != nil {
			return nil, "", dbErr(err, "读取评论")
		}
		for _, x := range got {
			rows = append(rows, rowT(x))
		}
	} else {
		got, err := r.q.ListCommentsLatest(ctx, sqlcgen.ListCommentsLatestParams{
			EpisodeID: episodeID, CursorID: parseLatestCursor(cursor), Lim: int32(limit),
		})
		if err != nil {
			return nil, "", dbErr(err, "读取评论")
		}
		for _, x := range got {
			rows = append(rows, rowT(x))
		}
	}

	out := make([]moderation.CommentView, len(rows))
	for i, x := range rows {
		out[i] = moderation.CommentView{
			ID: x.ID, ParentID: x.ParentID.Int64, Content: x.Content, ImageCount: int(x.ImageCount),
			LikeCount: x.LikeCount, CreatedAt: x.CreatedAt.Time.UTC(), Author: authorView(x.UserID, x.Nickname, x.Avatar),
		}
	}
	next := ""
	if len(rows) == limit && limit > 0 {
		last := rows[len(rows)-1]
		if hot {
			next = strconv.FormatInt(last.LikeCount, 10) + ":" + strconv.FormatInt(last.ID, 10)
		} else {
			next = strconv.FormatInt(last.ID, 10)
		}
	}
	return out, next, nil
}

func (r *CommentRepo) AuthorOf(ctx context.Context, id int64) (moderation.Author, int64, bool, error) {
	row, err := r.q.CommentAuthor(ctx, id)
	if err != nil {
		return moderation.Author{}, 0, false, dbErr(err, "读取评论")
	}
	a := moderation.Author{}
	if row.UserID.Valid {
		a.UserID = row.UserID.Int64
	}
	if row.GuestDeviceID.Valid {
		a.GuestID = uuidFromPg(row.GuestDeviceID)
	}
	return a, row.EpisodeID, row.Deleted, nil
}

func (r *CommentRepo) SoftDelete(ctx context.Context, id int64) error {
	return dbErr(r.q.SoftDeleteComment(ctx, id), "删除评论")
}

func (r *CommentRepo) Like(ctx context.Context, commentID, userID int64) (bool, error) {
	var added bool
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		n, err := q.InsertCommentLike(ctx, sqlcgen.InsertCommentLikeParams{CommentID: commentID, UserID: userID})
		if err != nil {
			return err
		}
		if n > 0 {
			added = true
			return q.IncrCommentLikeCount(ctx, commentID)
		}
		return nil
	})
	if err != nil {
		return false, dbErr(err, "点赞评论")
	}
	return added, nil
}

// authorView 按 user_id 是否有值构建作者展示：有值为注册用户，否则为游客。
func authorView(userID pgtype.Int8, nickname, avatar string) moderation.AuthorView {
	if userID.Valid {
		name := nickname
		if name == "" {
			name = "用户"
		}
		return moderation.AuthorView{IsGuest: false, UserID: userID.Int64, Nickname: name, Avatar: avatar}
	}
	return moderation.AuthorView{IsGuest: true, Nickname: "游客"}
}

func parseLatestCursor(s string) int64 {
	if s == "" {
		return 0
	}
	id, _ := strconv.ParseInt(s, 10, 64)
	if id < 0 {
		return 0
	}
	return id
}

// parseHotCursor 解析 "likeCount:id"；无 / 非法返回 (-1, 0) 表示首页（不加游标过滤）。
func parseHotCursor(s string) (int64, int64) {
	l, idStr, ok := strings.Cut(s, ":")
	if !ok {
		return -1, 0
	}
	like, err1 := strconv.ParseInt(l, 10, 64)
	id, err2 := strconv.ParseInt(idStr, 10, 64)
	if err1 != nil || err2 != nil || like < 0 || id < 0 {
		return -1, 0
	}
	return like, id
}
