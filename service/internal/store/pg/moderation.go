package pg

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/moderation"
	"xsite/service/internal/store/pg/sqlcgen"
)

// CommentPolicyRepo 读写评论先审后显开关（site_config.comment_policy）。实现 moderation.PolicyRepo。
type CommentPolicyRepo struct {
	q *sqlcgen.Queries
}

func NewCommentPolicyRepo(pool *pgxpool.Pool) *CommentPolicyRepo {
	return &CommentPolicyRepo{q: sqlcgen.New(pool)}
}

var _ moderation.PolicyRepo = (*CommentPolicyRepo)(nil)

func (r *CommentPolicyRepo) GetCommentPolicy(ctx context.Context) (moderation.CommentPolicy, error) {
	raw, err := r.q.GetCommentPolicy(ctx)
	if err != nil {
		return moderation.CommentPolicy{}, dbErr(err, "读取评论策略")
	}
	return decodePolicy(raw)
}

func (r *CommentPolicyRepo) SetCommentPolicy(ctx context.Context, p moderation.CommentPolicy) (moderation.CommentPolicy, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return moderation.CommentPolicy{}, err
	}
	raw, err := r.q.SetCommentPolicy(ctx, b)
	if err != nil {
		return moderation.CommentPolicy{}, dbErr(err, "保存评论策略")
	}
	return decodePolicy(raw)
}

func decodePolicy(raw []byte) (moderation.CommentPolicy, error) {
	var p moderation.CommentPolicy
	if len(raw) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return moderation.CommentPolicy{}, err
	}
	return p, nil
}

// ModerationRepo 实现 moderation.ModerationRepo（S2-5，§24）。
type ModerationRepo struct {
	q *sqlcgen.Queries
}

func NewModerationRepo(pool *pgxpool.Pool) *ModerationRepo {
	return &ModerationRepo{q: sqlcgen.New(pool)}
}

var _ moderation.ModerationRepo = (*ModerationRepo)(nil)

func (r *ModerationRepo) ListComments(ctx context.Context, q moderation.ModerationQuery) ([]moderation.ModerationItem, error) {
	rows, err := r.q.ListModerationComments(ctx, sqlcgen.ListModerationCommentsParams{
		Status: q.Status, Reported: q.Reported, Pattern: q.Pattern, Lim: int32(q.Limit), Off: int32(q.Offset),
	})
	if err != nil {
		return nil, dbErr(err, "读取评论审核列表")
	}
	out := make([]moderation.ModerationItem, len(rows))
	for i, x := range rows {
		out[i] = moderation.ModerationItem{
			ID: x.ID, Kind: moderation.KindComment, EpisodeID: x.EpisodeID, Content: x.Content,
			Status: x.Status, Author: authorView(x.UserID, x.Nickname, x.Avatar), ReportCount: x.ReportCount,
			CreatedAt: x.CreatedAt.Time.UTC(), ParentID: x.ParentID.Int64, LikeCount: x.LikeCount,
		}
	}
	return out, nil
}

func (r *ModerationRepo) CountComments(ctx context.Context, q moderation.ModerationQuery) (int64, error) {
	n, err := r.q.CountModerationComments(ctx, sqlcgen.CountModerationCommentsParams{
		Status: q.Status, Reported: q.Reported, Pattern: q.Pattern,
	})
	if err != nil {
		return 0, dbErr(err, "统计评论审核列表")
	}
	return n, nil
}

func (r *ModerationRepo) ListDanmaku(ctx context.Context, q moderation.ModerationQuery) ([]moderation.ModerationItem, error) {
	rows, err := r.q.ListModerationDanmaku(ctx, sqlcgen.ListModerationDanmakuParams{
		Status: q.Status, Reported: q.Reported, Pattern: q.Pattern, Lim: int32(q.Limit), Off: int32(q.Offset),
	})
	if err != nil {
		return nil, dbErr(err, "读取弹幕审核列表")
	}
	out := make([]moderation.ModerationItem, len(rows))
	for i, x := range rows {
		out[i] = moderation.ModerationItem{
			ID: x.ID, Kind: moderation.KindDanmaku, EpisodeID: x.EpisodeID, Content: x.Content,
			Status: x.Status, Author: authorView(x.UserID, x.Nickname, x.Avatar), ReportCount: x.ReportCount,
			CreatedAt: x.CreatedAt.Time.UTC(), TimeMS: int(x.TimeMs), Color: x.Color,
		}
	}
	return out, nil
}

func (r *ModerationRepo) CountDanmaku(ctx context.Context, q moderation.ModerationQuery) (int64, error) {
	n, err := r.q.CountModerationDanmaku(ctx, sqlcgen.CountModerationDanmakuParams{
		Status: q.Status, Reported: q.Reported, Pattern: q.Pattern,
	})
	if err != nil {
		return 0, dbErr(err, "统计弹幕审核列表")
	}
	return n, nil
}

func (r *ModerationRepo) SetCommentsStatus(ctx context.Context, ids []int64, status string) (int64, error) {
	n, err := r.q.SetCommentsStatus(ctx, sqlcgen.SetCommentsStatusParams{Status: status, Ids: ids})
	return n, dbErr(err, "更新评论状态")
}

func (r *ModerationRepo) SoftDeleteComments(ctx context.Context, ids []int64) (int64, error) {
	n, err := r.q.SoftDeleteComments(ctx, ids)
	return n, dbErr(err, "删除评论")
}

func (r *ModerationRepo) SetDanmakuStatus(ctx context.Context, ids []int64, status string) (int64, error) {
	n, err := r.q.SetDanmakuStatus(ctx, sqlcgen.SetDanmakuStatusParams{Status: status, Ids: ids})
	return n, dbErr(err, "更新弹幕状态")
}

func (r *ModerationRepo) DeleteDanmaku(ctx context.Context, ids []int64) (int64, error) {
	n, err := r.q.DeleteDanmaku(ctx, ids)
	return n, dbErr(err, "删除弹幕")
}
