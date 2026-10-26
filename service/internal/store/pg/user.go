package pg

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/user"
	"xsite/service/internal/store/pg/sqlcgen"
)

// UserRepo 实现 user.Repo（S2-1 注册用户）。
type UserRepo struct {
	q *sqlcgen.Queries
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{q: sqlcgen.New(pool)}
}

func (r *UserRepo) Create(ctx context.Context, u user.NewUser) (user.User, error) {
	row, err := r.q.CreateUser(ctx, sqlcgen.CreateUserParams{
		Username: u.Username, Nickname: u.Nickname, PasswordHash: u.PasswordHash,
	})
	if err != nil {
		// 用户名唯一键冲突经 dbErr 映射为 ErrConflict
		return user.User{}, dbErr(err, "创建用户")
	}
	return mapUser(row.ID, row.Username, row.Nickname, row.Avatar, row.Bio, row.Status, row.Locale, "", row.CreatedAt), nil
}

func (r *UserRepo) ByUsername(ctx context.Context, username string) (user.User, error) {
	row, err := r.q.UserByUsername(ctx, username)
	if err != nil {
		return user.User{}, dbErr(err, "查询用户")
	}
	return mapUser(row.ID, row.Username, row.Nickname, row.Avatar, row.Bio, row.Status, row.Locale, row.PasswordHash, row.CreatedAt), nil
}

func (r *UserRepo) ByID(ctx context.Context, id int64) (user.User, error) {
	row, err := r.q.UserByID(ctx, id)
	if err != nil {
		return user.User{}, dbErr(err, "查询用户")
	}
	return mapUser(row.ID, row.Username, row.Nickname, row.Avatar, row.Bio, row.Status, row.Locale, row.PasswordHash, row.CreatedAt), nil
}

func (r *UserRepo) UpdatePassword(ctx context.Context, id int64, hash string) error {
	return dbErr(r.q.UpdateUserPassword(ctx, sqlcgen.UpdateUserPasswordParams{ID: id, PasswordHash: hash}), "更新密码")
}

// Muted 报告该注册用户是否被禁言（satisfies moderation.Users，S2-5）。
func (r *UserRepo) Muted(ctx context.Context, id int64) (bool, error) {
	muted, err := r.q.UserMuted(ctx, id)
	if err != nil {
		return false, dbErr(err, "查询用户状态")
	}
	return muted, nil
}

func (r *UserRepo) GetProfile(ctx context.Context, id int64) (user.Profile, error) {
	row, err := r.q.GetUserProfile(ctx, id)
	if err != nil {
		return user.Profile{}, dbErr(err, "查询资料")
	}
	return user.Profile{
		Nickname: row.Nickname, Avatar: row.Avatar, Bio: row.Bio,
		NicknameStatus: row.NicknameStatus, BioStatus: row.BioStatus, AvatarStatus: row.AvatarStatus,
		Locale: row.Locale.String,
	}, nil
}

func (r *UserRepo) UpdateProfile(ctx context.Context, id int64, f user.ProfileFields) (user.Profile, error) {
	row, err := r.q.UpdateUserProfile(ctx, sqlcgen.UpdateUserProfileParams{
		ID: id, Nickname: textPtr(f.Nickname), Avatar: textPtr(f.Avatar), Bio: textPtr(f.Bio),
	})
	if err != nil {
		return user.Profile{}, dbErr(err, "更新资料")
	}
	return user.Profile{
		Nickname: row.Nickname, Avatar: row.Avatar, Bio: row.Bio,
		NicknameStatus: row.NicknameStatus, BioStatus: row.BioStatus, AvatarStatus: row.AvatarStatus,
		Locale: row.Locale.String,
	}, nil
}

// textPtr 把可选字符串映射为 pgtype.Text：nil 表示「不更新」（NULL，配合 COALESCE 保留原值），
// 非 nil（含空串）表示更新为该值。与把空串当 NULL 的 nullText 语义不同。
func textPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// mapUser 把查询结果拼成领域模型；locale 可空。
func mapUser(id int64, username, nickname, avatar, bio, status string, locale pgtype.Text, pwHash string, createdAt pgtype.Timestamptz) user.User {
	u := user.User{
		ID: id, Username: username, Nickname: nickname, Avatar: avatar, Bio: bio,
		Status: user.Status(status), PasswordHash: pwHash, CreatedAt: createdAt.Time,
	}
	if locale.Valid {
		u.Locale = locale.String
	}
	return u
}
