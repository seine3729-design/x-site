package pg

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/user"
	"xsite/service/internal/store/pg/sqlcgen"
)

// UserAdminRepo 实现 user.AdminRepo（后台用户管理，S2-7 / A2-2）。
type UserAdminRepo struct {
	q *sqlcgen.Queries
}

func NewUserAdminRepo(pool *pgxpool.Pool) *UserAdminRepo {
	return &UserAdminRepo{q: sqlcgen.New(pool)}
}

func (r *UserAdminRepo) ListUsers(ctx context.Context, q user.AdminQuery) ([]user.AdminUser, int64, error) {
	rows, err := r.q.ListAdminUsers(ctx, sqlcgen.ListAdminUsersParams{
		Status: q.Status, Pattern: q.Pattern, Lim: int32(q.Limit), Off: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, dbErr(err, "查询用户列表")
	}
	total, err := r.q.CountAdminUsers(ctx, sqlcgen.CountAdminUsersParams{Status: q.Status, Pattern: q.Pattern})
	if err != nil {
		return nil, 0, dbErr(err, "统计用户数")
	}
	list := make([]user.AdminUser, len(rows))
	for i, row := range rows {
		list[i] = mapAdminUser(row.ID, row.Username, row.Nickname, row.Avatar, row.Bio, row.Status,
			row.Locale, row.VerifiedAt, row.CreatedAt)
	}
	return list, total, nil
}

func (r *UserAdminRepo) UserDetail(ctx context.Context, id int64) (user.AdminUserDetail, error) {
	row, err := r.q.AdminUserByID(ctx, id)
	if err != nil {
		return user.AdminUserDetail{}, dbErr(err, "查询用户")
	}
	stats, err := r.q.AdminUserStats(ctx, id)
	if err != nil {
		return user.AdminUserDetail{}, dbErr(err, "统计用户足迹")
	}
	against, err := r.q.AdminUserReportAgainst(ctx, id)
	if err != nil {
		return user.AdminUserDetail{}, dbErr(err, "统计被举报数")
	}
	return user.AdminUserDetail{
		AdminUser: mapAdminUser(row.ID, row.Username, row.Nickname, row.Avatar, row.Bio, row.Status,
			row.Locale, row.VerifiedAt, row.CreatedAt),
		NicknameStatus: row.NicknameStatus,
		BioStatus:      row.BioStatus,
		AvatarStatus:   row.AvatarStatus,
		HistoryPaused:  row.HistoryPaused,
		Stats: user.AdminUserStats{
			CommentCount:  stats.CommentCount,
			DanmakuCount:  stats.DanmakuCount,
			ReportSent:    stats.ReportSent,
			ReportAgainst: against,
			FollowCount:   stats.FollowCount,
			HistoryCount:  stats.HistoryCount,
			SessionCount:  stats.SessionCount,
		},
	}, nil
}

func (r *UserAdminRepo) SetUserStatus(ctx context.Context, id int64, status user.Status) (user.AdminUser, error) {
	row, err := r.q.SetAdminUserStatus(ctx, sqlcgen.SetAdminUserStatusParams{ID: id, Status: string(status)})
	if err != nil {
		// 用户不存在或已在回收站：UPDATE 没命中行，pgx 返回 ErrNoRows，dbErr 映射为 ErrNotFound
		return user.AdminUser{}, dbErr(err, "修改账号状态")
	}
	return mapAdminUser(row.ID, row.Username, row.Nickname, row.Avatar, row.Bio, row.Status,
		row.Locale, row.VerifiedAt, row.CreatedAt), nil
}

func (r *UserAdminRepo) ListGuests(ctx context.Context, q user.AdminQuery) ([]user.Guest, int64, error) {
	rows, err := r.q.ListAdminGuests(ctx, sqlcgen.ListAdminGuestsParams{
		Pattern: q.Pattern, Lim: int32(q.Limit), Off: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, dbErr(err, "查询游客列表")
	}
	total, err := r.q.CountAdminGuests(ctx, q.Pattern)
	if err != nil {
		return nil, 0, dbErr(err, "统计游客数")
	}
	list := make([]user.Guest, len(rows))
	for i, row := range rows {
		list[i] = user.Guest{
			ID:             uuidFromPg(row.ID).String(),
			FirstSeen:      row.FirstSeen.Time,
			SourceChannel:  row.SourceChannel,
			Platform:       row.Platform,
			AttestLevel:    row.AttestLevel,
			MergedUserID:   optInt8Ptr(row.MergedUserID),
			MergedUsername: row.MergedUsername,
		}
	}
	return list, total, nil
}

// mapAdminUser 把列表 / 详情 / 改状态三处结构相同的行拼成领域模型。
func mapAdminUser(id int64, username, nickname, avatar, bio, status string,
	locale pgtype.Text, verifiedAt, createdAt pgtype.Timestamptz) user.AdminUser {
	u := user.AdminUser{
		ID: id, Username: username, Nickname: nickname, Avatar: avatar, Bio: bio,
		Status: user.Status(status), VerifiedAt: timePtr(verifiedAt), CreatedAt: createdAt.Time,
	}
	if locale.Valid {
		u.Locale = locale.String
	}
	return u
}
