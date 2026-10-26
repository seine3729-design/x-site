package pg

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dom "xsite/service/internal/domain/feedback"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// FeedbackRepo 是反馈社区的仓储实现（迁移 00012，db/queries/feedback.sql）。
type FeedbackRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewFeedbackRepo(pool *pgxpool.Pool) *FeedbackRepo {
	return &FeedbackRepo{q: sqlcgen.New(pool), pool: pool}
}

// fbErr 把数据库错误映射为业务错误；唯一键按约束名给出具体提示。
func fbErr(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			switch pgErr.ConstraintName {
			case "fb_user_handle_norm_key":
				return errs.ErrConflict.WithMessage("该用户名已被使用，请换一个")
			case "fb_project_slug_key":
				return errs.ErrConflict.WithMessage("项目标识已被使用")
			case "fb_project_key_prefix_key":
				return errs.ErrConflict.WithMessage("反馈编号前缀已被使用")
			case "fb_feedback_project_id_number_key":
				return errs.ErrConflict.WithMessage("反馈编号冲突，请重试")
			}
			return errs.ErrConflict
		case "23503": // foreign_key_violation
			return errs.ErrBadRequest.WithMessage("关联的数据不存在")
		case "23514": // check_violation
			return errs.ErrBadRequest.WithMessage("提交的内容不符合约束，请检查后重试")
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

// ---------- 类型转换 ----------

// fbTime / fbTimePtr 把数据库时间转为领域时间；反向转换复用本包已有的 tsOf。
func fbTime(t pgtype.Timestamptz) time.Time { return t.Time }

func fbTimePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func feedbackKey(prefix string, number int64) string {
	if prefix == "" {
		return strconv.FormatInt(number, 10)
	}
	return prefix + "-" + strconv.FormatInt(number, 10)
}

// fbUserOf 把库里的行映射成领域模型。
//
// 刻意不映射 u.RecoveryCodeHash：恢复码已删除（adr/0011），那一列恒为空串，
// 只是暂时还留在表结构里。领域模型里不再有对应字段。
func fbUserOf(u sqlcgen.FbUser) dom.User {
	return dom.User{
		ID: u.ID, Handle: u.Handle, Nickname: u.Nickname, Bio: u.Bio, ShowLevel: u.ShowLevel,
		TesterNo: u.TesterNo, Status: dom.UserStatus(u.Status), Points: u.Points, SessionVersion: u.SessionVersion,
		PasswordHash: u.PasswordHash, CreatedAt: fbTime(u.CreatedAt),
	}
}

func fbProjectOf(p sqlcgen.FbProject) dom.Project {
	return dom.Project{
		ID: p.ID, Slug: p.Slug, KeyPrefix: p.KeyPrefix, Name: p.Name, Summary: p.Summary,
		Description: p.Description, Cover: p.Cover, TryURL: p.TryUrl,
		Status: dom.ProjectStatus(p.Status), Visibility: dom.Visibility(p.Visibility),
		JoinMode: dom.JoinMode(p.JoinMode), Sort: p.Sort,
		CreatedAt: fbTime(p.CreatedAt), UpdatedAt: fbTime(p.UpdatedAt),
	}
}

func fbInviteOf(i sqlcgen.FbInvite, projectName string) dom.Invite {
	return dom.Invite{
		ID: i.ID, CodeHint: i.CodeHint, ProjectID: i.ProjectID.Int64, ProjectName: projectName,
		Role: dom.MemberRole(i.Role), MaxUses: i.MaxUses, UsedCount: i.UsedCount, Note: i.Note,
		ExpiresAt: fbTimePtr(i.ExpiresAt), RevokedAt: fbTimePtr(i.RevokedAt), CreatedAt: fbTime(i.CreatedAt),
	}
}

func fbPostOf(p sqlcgen.FbPost, projectName, projectSlug string) dom.Post {
	return dom.Post{
		ID: p.ID, ProjectID: p.ProjectID, ProjectName: projectName, ProjectSlug: projectSlug,
		Kind: dom.PostKind(p.Kind), Title: p.Title, Body: p.Body, Status: p.Status,
		Visibility: dom.Visibility(p.Visibility), Pinned: p.Pinned, PublishedAt: fbTimePtr(p.PublishedAt),
		CreatedAt: fbTime(p.CreatedAt), UpdatedAt: fbTime(p.UpdatedAt),
	}
}

func fbFeedbackOf(f sqlcgen.FbFeedback, prefix, slug, projectName, handle, nickname string, voted bool) dom.Feedback {
	return dom.Feedback{
		ID: f.ID, ProjectID: f.ProjectID, ProjectSlug: slug, ProjectName: projectName,
		Number: f.Number, Key: feedbackKey(prefix, f.Number),
		AuthorID: f.AuthorID.Int64, AuthorHandle: handle, AuthorNickname: nickname,
		Kind: dom.Kind(f.Kind), Title: f.Title, Body: f.Body, Env: f.Env,
		Visibility: dom.Visibility(f.Visibility), Status: dom.Status(f.Status), Priority: dom.Priority(f.Priority),
		DuplicateOf: f.DuplicateOf.Int64, VoteCount: f.VoteCount, Voted: voted, Version: f.Version,
		Source: dom.Source(f.Source), APIKeyID: f.ApiKeyID.Int64, PageURL: f.PageUrl, Contact: f.Contact,
		LastActivityAt: fbTime(f.LastActivityAt), CreatedAt: fbTime(f.CreatedAt),
	}
}

// ---------- 用户与注册 ----------

// Register 在一个事务里核销邀请码并建用户。check 返回错误时整体回滚。
func (r *FeedbackRepo) Register(ctx context.Context, in dom.NewUser, check func(dom.Invite) error) (dom.User, error) {
	var out dom.User
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		inv, err := q.LockFbInviteByHash(ctx, in.InviteHash)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// 与「已失效」用同一提示，避免把邀请码当探测接口
				return errs.ErrBadRequest.WithMessage("邀请码无效或已失效")
			}
			return fbErr(err, "读取邀请码")
		}
		if err := check(fbInviteOf(inv, "")); err != nil {
			return err
		}
		user, err := q.CreateFbUser(ctx, sqlcgen.CreateFbUserParams{
			Handle: in.Handle, PasswordHash: in.PasswordHash, Nickname: in.Nickname,
		})
		if err != nil {
			return fbErr(err, "创建社区用户")
		}
		if err := q.CreateFbInviteRedemption(ctx, sqlcgen.CreateFbInviteRedemptionParams{
			InviteID: inv.ID, UserID: user.ID,
		}); err != nil {
			return fbErr(err, "记录邀请核销")
		}
		if _, err := q.ConsumeFbInvite(ctx, inv.ID); err != nil {
			return fbErr(err, "更新邀请使用次数")
		}
		if inv.ProjectID.Valid {
			if _, err := q.UpsertFbMember(ctx, sqlcgen.UpsertFbMemberParams{
				ProjectID: inv.ProjectID.Int64, UserID: user.ID, Role: inv.Role, Status: "active",
			}); err != nil {
				return fbErr(err, "加入项目")
			}
		}
		out = fbUserOf(user)
		return nil
	})
	if err != nil {
		return dom.User{}, err
	}
	return out, nil
}

func (r *FeedbackRepo) UserByHandle(ctx context.Context, handle string) (dom.User, error) {
	u, err := r.q.GetFbUserByHandle(ctx, handle)
	if err != nil {
		return dom.User{}, fbErr(err, "读取社区用户")
	}
	return fbUserOf(u), nil
}

func (r *FeedbackRepo) User(ctx context.Context, id int64) (dom.User, error) {
	u, err := r.q.GetFbUser(ctx, id)
	if err != nil {
		return dom.User{}, fbErr(err, "读取社区用户")
	}
	return fbUserOf(u), nil
}

func (r *FeedbackRepo) UpdateSecret(ctx context.Context, id int64, passwordHash string) (dom.User, error) {
	u, err := r.q.UpdateFbUserSecret(ctx, sqlcgen.UpdateFbUserSecretParams{
		PasswordHash: passwordHash, ID: id,
	})
	if err != nil {
		return dom.User{}, fbErr(err, "更新密码")
	}
	return fbUserOf(u), nil
}

func (r *FeedbackRepo) UpdateProfile(ctx context.Context, id int64, in dom.ProfileInput) (dom.User, error) {
	u, err := r.q.UpdateFbUserProfile(ctx, sqlcgen.UpdateFbUserProfileParams{
		Nickname: in.Nickname, Bio: in.Bio, ShowLevel: in.ShowLevel, ID: id,
	})
	if err != nil {
		return dom.User{}, fbErr(err, "更新资料")
	}
	return fbUserOf(u), nil
}

func (r *FeedbackRepo) UpdateUserStatus(ctx context.Context, id int64, status dom.UserStatus) (dom.User, error) {
	u, err := r.q.UpdateFbUserStatus(ctx, sqlcgen.UpdateFbUserStatusParams{Status: string(status), ID: id})
	if err != nil {
		return dom.User{}, fbErr(err, "更新账号状态")
	}
	return fbUserOf(u), nil
}

func (r *FeedbackRepo) ListUsers(ctx context.Context, f dom.UserFilter) ([]dom.User, int64, error) {
	pattern := nullText(f.Q)
	status := nullText(string(f.Status))
	rows, err := r.q.ListFbUsers(ctx, sqlcgen.ListFbUsersParams{
		Pattern: pattern, Status: status,
		PageLimit: int32(f.PageSize), PageOffset: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取社区用户列表")
	}
	total, err := r.q.CountFbUsers(ctx, sqlcgen.CountFbUsersParams{Pattern: pattern, Status: status})
	if err != nil {
		return nil, 0, fbErr(err, "统计社区用户")
	}
	out := make([]dom.User, 0, len(rows))
	for _, u := range rows {
		out = append(out, fbUserOf(u))
	}
	return out, total, nil
}

func (r *FeedbackRepo) Leaderboard(ctx context.Context, limit int) ([]dom.LeaderboardEntry, error) {
	rows, err := r.q.ListFbLeaderboard(ctx, int32(limit))
	if err != nil {
		return nil, fbErr(err, "读取贡献榜")
	}
	out := make([]dom.LeaderboardEntry, 0, len(rows))
	for _, x := range rows {
		out = append(out, dom.LeaderboardEntry{UserID: x.ID, Handle: x.Handle, Nickname: x.Nickname, Points: x.Points})
	}
	return out, nil
}

// ---------- 项目 ----------

func projectParams(in dom.ProjectInput) sqlcgen.CreateFbProjectParams {
	return sqlcgen.CreateFbProjectParams{
		Slug: in.Slug, KeyPrefix: in.KeyPrefix, Name: in.Name, Summary: in.Summary,
		Description: in.Description, Cover: in.Cover, TryUrl: in.TryURL,
		Status: string(in.Status), Visibility: string(in.Visibility), JoinMode: string(in.JoinMode), Sort: in.Sort,
	}
}

func (r *FeedbackRepo) CreateProject(ctx context.Context, in dom.ProjectInput) (dom.Project, error) {
	p, err := r.q.CreateFbProject(ctx, projectParams(in))
	if err != nil {
		return dom.Project{}, fbErr(err, "创建项目")
	}
	return fbProjectOf(p), nil
}

func (r *FeedbackRepo) UpdateProject(ctx context.Context, id int64, in dom.ProjectInput) (dom.Project, error) {
	c := projectParams(in)
	p, err := r.q.UpdateFbProject(ctx, sqlcgen.UpdateFbProjectParams{
		Slug: c.Slug, KeyPrefix: c.KeyPrefix, Name: c.Name, Summary: c.Summary, Description: c.Description,
		Cover: c.Cover, TryUrl: c.TryUrl, Status: c.Status, Visibility: c.Visibility, JoinMode: c.JoinMode,
		Sort: c.Sort, ID: id,
	})
	if err != nil {
		return dom.Project{}, fbErr(err, "保存项目")
	}
	return fbProjectOf(p), nil
}

func (r *FeedbackRepo) DeleteProject(ctx context.Context, id int64) (dom.Project, error) {
	p, err := r.q.DeleteFbProject(ctx, id)
	if err != nil {
		return dom.Project{}, fbErr(err, "删除项目")
	}
	return fbProjectOf(p), nil
}

func (r *FeedbackRepo) Project(ctx context.Context, id int64) (dom.Project, error) {
	p, err := r.q.GetFbProject(ctx, id)
	if err != nil {
		return dom.Project{}, fbErr(err, "读取项目")
	}
	return fbProjectOf(p), nil
}

func (r *FeedbackRepo) ProjectBySlug(ctx context.Context, slug string) (dom.Project, error) {
	p, err := r.q.GetFbProjectBySlug(ctx, slug)
	if err != nil {
		return dom.Project{}, fbErr(err, "读取项目")
	}
	return fbProjectOf(p), nil
}

func (r *FeedbackRepo) ListProjects(ctx context.Context, f dom.ProjectFilter) ([]dom.Project, int64, error) {
	pattern := nullText(f.Q)
	status := nullText(string(f.Status))
	rows, err := r.q.ListFbProjects(ctx, sqlcgen.ListFbProjectsParams{
		Pattern: pattern, Status: status,
		PageLimit: int32(f.PageSize), PageOffset: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取项目列表")
	}
	total, err := r.q.CountFbProjects(ctx, sqlcgen.CountFbProjectsParams{Pattern: pattern, Status: status})
	if err != nil {
		return nil, 0, fbErr(err, "统计项目")
	}
	out := make([]dom.Project, 0, len(rows))
	for _, p := range rows {
		out = append(out, fbProjectOf(p))
	}
	return out, total, nil
}

func (r *FeedbackRepo) VisibleProjects(ctx context.Context, v dom.Viewer, page, pageSize int) ([]dom.ProjectView, int64, error) {
	rows, err := r.q.ListVisibleFbProjects(ctx, sqlcgen.ListVisibleFbProjectsParams{
		ViewerID:  nullID(v.UserID),
		PageLimit: int32(pageSize), PageOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取项目目录")
	}
	total, err := r.q.CountVisibleFbProjects(ctx, nullID(v.UserID))
	if err != nil {
		return nil, 0, fbErr(err, "统计项目")
	}
	out := make([]dom.ProjectView, 0, len(rows))
	for _, x := range rows {
		out = append(out, dom.ProjectView{
			Project: fbProjectOf(sqlcgen.FbProject{
				ID: x.ID, Slug: x.Slug, KeyPrefix: x.KeyPrefix, Name: x.Name, Summary: x.Summary,
				Description: x.Description, Cover: x.Cover, TryUrl: x.TryUrl, Status: x.Status,
				Visibility: x.Visibility, JoinMode: x.JoinMode, NextNumber: x.NextNumber, Sort: x.Sort,
				CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
			}),
			Joined: x.Joined, MemberCount: x.MemberCount, FeedbackCount: x.FeedbackCount,
		})
	}
	return out, total, nil
}

func (r *FeedbackRepo) VisibleProject(ctx context.Context, v dom.Viewer, slug string) (dom.ProjectView, error) {
	x, err := r.q.GetVisibleFbProject(ctx, sqlcgen.GetVisibleFbProjectParams{ViewerID: nullID(v.UserID), Slug: slug})
	if err != nil {
		return dom.ProjectView{}, fbErr(err, "读取项目")
	}
	return dom.ProjectView{
		Project: fbProjectOf(sqlcgen.FbProject{
			ID: x.ID, Slug: x.Slug, KeyPrefix: x.KeyPrefix, Name: x.Name, Summary: x.Summary,
			Description: x.Description, Cover: x.Cover, TryUrl: x.TryUrl, Status: x.Status,
			Visibility: x.Visibility, JoinMode: x.JoinMode, NextNumber: x.NextNumber, Sort: x.Sort,
			CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}),
		Joined: x.Joined, MemberRole: dom.MemberRole(x.MemberRole), MemberStatus: x.MemberStatus,
		MemberCount: x.MemberCount, FeedbackCount: x.FeedbackCount,
	}, nil
}

// ---------- 成员 ----------

func (r *FeedbackRepo) UpsertMember(ctx context.Context, projectID, userID int64, role dom.MemberRole, status string) (dom.Member, error) {
	m, err := r.q.UpsertFbMember(ctx, sqlcgen.UpsertFbMemberParams{
		ProjectID: projectID, UserID: userID, Role: string(role), Status: status,
	})
	if err != nil {
		return dom.Member{}, fbErr(err, "保存项目成员")
	}
	return dom.Member{
		ProjectID: m.ProjectID, UserID: m.UserID, Role: dom.MemberRole(m.Role),
		Status: m.Status, JoinedAt: fbTime(m.JoinedAt),
	}, nil
}

func (r *FeedbackRepo) Member(ctx context.Context, projectID, userID int64) (dom.Member, error) {
	m, err := r.q.GetFbMember(ctx, sqlcgen.GetFbMemberParams{ProjectID: projectID, UserID: userID})
	if err != nil {
		return dom.Member{}, fbErr(err, "读取项目成员")
	}
	return dom.Member{
		ProjectID: m.ProjectID, UserID: m.UserID, Role: dom.MemberRole(m.Role),
		Status: m.Status, JoinedAt: fbTime(m.JoinedAt),
	}, nil
}

func (r *FeedbackRepo) ListMembers(ctx context.Context, projectID int64, page, pageSize int) ([]dom.Member, int64, error) {
	rows, err := r.q.ListFbMembers(ctx, sqlcgen.ListFbMembersParams{
		ProjectID: projectID, PageLimit: int32(pageSize), PageOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取项目成员列表")
	}
	total, err := r.q.CountFbMembers(ctx, projectID)
	if err != nil {
		return nil, 0, fbErr(err, "统计项目成员")
	}
	out := make([]dom.Member, 0, len(rows))
	for _, m := range rows {
		out = append(out, dom.Member{
			ProjectID: m.ProjectID, UserID: m.UserID, Handle: m.Handle, Nickname: m.Nickname,
			UserStatus: dom.UserStatus(m.UserStatus), Role: dom.MemberRole(m.Role),
			Status: m.Status, JoinedAt: fbTime(m.JoinedAt),
		})
	}
	return out, total, nil
}

// ---------- 邀请码 ----------

func (r *FeedbackRepo) CreateInvite(ctx context.Context, codeHash, hint string, in dom.InviteInput) (dom.Invite, error) {
	i, err := r.q.CreateFbInvite(ctx, sqlcgen.CreateFbInviteParams{
		CodeHash: codeHash, CodeHint: hint, ProjectID: nullID(in.ProjectID), Role: string(in.Role),
		MaxUses: in.MaxUses, Note: in.Note, ExpiresAt: tsOf(in.ExpiresAt), CreatedBy: in.CreatedBy,
	})
	if err != nil {
		return dom.Invite{}, fbErr(err, "创建邀请码")
	}
	return fbInviteOf(i, ""), nil
}

func (r *FeedbackRepo) Invite(ctx context.Context, id int64) (dom.Invite, error) {
	i, err := r.q.GetFbInvite(ctx, id)
	if err != nil {
		return dom.Invite{}, fbErr(err, "读取邀请码")
	}
	return fbInviteOf(i, ""), nil
}

func (r *FeedbackRepo) ListInvites(ctx context.Context, projectID int64, page, pageSize int) ([]dom.Invite, int64, error) {
	pid := nullID(projectID)
	rows, err := r.q.ListFbInvites(ctx, sqlcgen.ListFbInvitesParams{
		ProjectID: pid, PageLimit: int32(pageSize), PageOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取邀请码列表")
	}
	total, err := r.q.CountFbInvites(ctx, pid)
	if err != nil {
		return nil, 0, fbErr(err, "统计邀请码")
	}
	out := make([]dom.Invite, 0, len(rows))
	for _, x := range rows {
		out = append(out, fbInviteOf(sqlcgen.FbInvite{
			ID: x.ID, CodeHash: x.CodeHash, CodeHint: x.CodeHint, ProjectID: x.ProjectID, Role: x.Role,
			MaxUses: x.MaxUses, UsedCount: x.UsedCount, Note: x.Note, ExpiresAt: x.ExpiresAt,
			RevokedAt: x.RevokedAt, CreatedBy: x.CreatedBy, CreatedAt: x.CreatedAt,
		}, x.ProjectName.String))
	}
	return out, total, nil
}

func (r *FeedbackRepo) RevokeInvite(ctx context.Context, id int64) (dom.Invite, error) {
	i, err := r.q.RevokeFbInvite(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dom.Invite{}, errs.ErrConflict.WithMessage("该邀请码不存在或已作废")
		}
		return dom.Invite{}, fbErr(err, "作废邀请码")
	}
	return fbInviteOf(i, ""), nil
}

// ---------- 动态 ----------

func (r *FeedbackRepo) CreatePost(ctx context.Context, in dom.PostInput, publishedAt *time.Time) (dom.Post, error) {
	p, err := r.q.CreateFbPost(ctx, sqlcgen.CreateFbPostParams{
		ProjectID: in.ProjectID, Kind: string(in.Kind), Title: in.Title, Body: in.Body,
		Status: in.Status, Visibility: string(in.Visibility), Pinned: in.Pinned, PublishedAt: tsOf(publishedAt),
	})
	if err != nil {
		return dom.Post{}, fbErr(err, "创建动态")
	}
	return fbPostOf(p, "", ""), nil
}

func (r *FeedbackRepo) UpdatePost(ctx context.Context, id int64, in dom.PostInput, publishedAt *time.Time) (dom.Post, error) {
	p, err := r.q.UpdateFbPost(ctx, sqlcgen.UpdateFbPostParams{
		Kind: string(in.Kind), Title: in.Title, Body: in.Body, Status: in.Status,
		Visibility: string(in.Visibility), Pinned: in.Pinned, PublishedAt: tsOf(publishedAt), ID: id,
	})
	if err != nil {
		return dom.Post{}, fbErr(err, "保存动态")
	}
	return fbPostOf(p, "", ""), nil
}

func (r *FeedbackRepo) DeletePost(ctx context.Context, id int64) (dom.Post, error) {
	p, err := r.q.DeleteFbPost(ctx, id)
	if err != nil {
		return dom.Post{}, fbErr(err, "删除动态")
	}
	return fbPostOf(p, "", ""), nil
}

func (r *FeedbackRepo) Post(ctx context.Context, id int64) (dom.Post, error) {
	p, err := r.q.GetFbPost(ctx, id)
	if err != nil {
		return dom.Post{}, fbErr(err, "读取动态")
	}
	return fbPostOf(p, "", ""), nil
}

func (r *FeedbackRepo) ListPosts(ctx context.Context, f dom.PostFilter) ([]dom.Post, int64, error) {
	pid := nullID(f.ProjectID)
	status := nullText(f.Status)
	rows, err := r.q.ListFbPosts(ctx, sqlcgen.ListFbPostsParams{
		ProjectID: pid, Status: status,
		PageLimit: int32(f.PageSize), PageOffset: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取动态列表")
	}
	total, err := r.q.CountFbPosts(ctx, sqlcgen.CountFbPostsParams{ProjectID: pid, Status: status})
	if err != nil {
		return nil, 0, fbErr(err, "统计动态")
	}
	out := make([]dom.Post, 0, len(rows))
	for _, x := range rows {
		out = append(out, fbPostOf(sqlcgen.FbPost{
			ID: x.ID, ProjectID: x.ProjectID, Kind: x.Kind, Title: x.Title, Body: x.Body, Status: x.Status,
			Visibility: x.Visibility, Pinned: x.Pinned, PublishedAt: x.PublishedAt,
			CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}, x.ProjectName, x.ProjectSlug))
	}
	return out, total, nil
}

func (r *FeedbackRepo) VisiblePosts(ctx context.Context, v dom.Viewer, projectID int64, page, pageSize int) ([]dom.Post, int64, error) {
	rows, err := r.q.ListVisibleFbPosts(ctx, sqlcgen.ListVisibleFbPostsParams{
		ViewerID: nullID(v.UserID), ProjectID: nullID(projectID),
		PageLimit: int32(pageSize), PageOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取动态流")
	}
	total, err := r.q.CountVisibleFbPosts(ctx, sqlcgen.CountVisibleFbPostsParams{
		ViewerID: nullID(v.UserID), ProjectID: nullID(projectID),
	})
	if err != nil {
		return nil, 0, fbErr(err, "统计动态")
	}
	out := make([]dom.Post, 0, len(rows))
	for _, x := range rows {
		out = append(out, fbPostOf(sqlcgen.FbPost{
			ID: x.ID, ProjectID: x.ProjectID, Kind: x.Kind, Title: x.Title, Body: x.Body, Status: x.Status,
			Visibility: x.Visibility, Pinned: x.Pinned, PublishedAt: x.PublishedAt,
			CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}, x.ProjectName, x.ProjectSlug))
	}
	return out, total, nil
}

func (r *FeedbackRepo) VisiblePost(ctx context.Context, v dom.Viewer, id int64) (dom.Post, error) {
	x, err := r.q.GetVisibleFbPost(ctx, sqlcgen.GetVisibleFbPostParams{ViewerID: nullID(v.UserID), ID: id})
	if err != nil {
		return dom.Post{}, fbErr(err, "读取动态")
	}
	return fbPostOf(sqlcgen.FbPost{
		ID: x.ID, ProjectID: x.ProjectID, Kind: x.Kind, Title: x.Title, Body: x.Body, Status: x.Status,
		Visibility: x.Visibility, Pinned: x.Pinned, PublishedAt: x.PublishedAt,
		CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
	}, x.ProjectName, x.ProjectSlug), nil
}

// ---------- 反馈 ----------

// CreateFeedback 在一个事务里分配编号、写反馈并写首条状态历史。
func (r *FeedbackRepo) CreateFeedback(ctx context.Context, projectID, authorID int64, in dom.SubmitInput) (dom.Feedback, error) {
	var out dom.Feedback
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		next, err := q.NextFbFeedbackNumber(ctx, projectID)
		if err != nil {
			return fbErr(err, "分配反馈编号")
		}
		f, err := q.CreateFbFeedback(ctx, sqlcgen.CreateFbFeedbackParams{
			ProjectID: projectID, Number: next.Number, AuthorID: nullID(authorID),
			Kind: string(in.Kind), Title: in.Title, Body: in.Body, Env: in.Env,
			Visibility: string(in.Visibility), PageUrl: in.PageURL,
		})
		if err != nil {
			return fbErr(err, "创建反馈")
		}
		if _, err := q.CreateFbFeedbackEvent(ctx, sqlcgen.CreateFbFeedbackEventParams{
			FeedbackID: f.ID, FromStatus: "", ToStatus: string(dom.StatusSubmitted),
			Note: "已收到，等待官方确认",
		}); err != nil {
			return fbErr(err, "写入状态历史")
		}
		out = fbFeedbackOf(f, next.KeyPrefix, in.ProjectSlug, "", "", "", false)
		return nil
	})
	if err != nil {
		return dom.Feedback{}, err
	}
	return out, nil
}

func (r *FeedbackRepo) Feedback(ctx context.Context, id int64) (dom.Feedback, error) {
	x, err := r.q.GetFbFeedback(ctx, id)
	if err != nil {
		return dom.Feedback{}, fbErr(err, "读取反馈")
	}
	return fbFeedbackOf(sqlcgen.FbFeedback{
		ID: x.ID, ProjectID: x.ProjectID, Number: x.Number, AuthorID: x.AuthorID, Kind: x.Kind,
		Title: x.Title, Body: x.Body, Env: x.Env, Visibility: x.Visibility, Status: x.Status,
		Priority: x.Priority, DuplicateOf: x.DuplicateOf, VoteCount: x.VoteCount, Version: x.Version,
		Source: x.Source, ApiKeyID: x.ApiKeyID, Contact: x.Contact, PageUrl: x.PageUrl,
		LastActivityAt: x.LastActivityAt, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
	}, x.KeyPrefix, x.ProjectSlug, x.ProjectName, x.AuthorHandle, x.AuthorNickname, false), nil
}

func (r *FeedbackRepo) FeedbackByKey(ctx context.Context, v dom.Viewer, slug string, number int64) (dom.Feedback, error) {
	x, err := r.q.GetFbFeedbackByKey(ctx, sqlcgen.GetFbFeedbackByKeyParams{
		ViewerID: nullID(v.UserID), Slug: slug, Number: number,
	})
	if err != nil {
		return dom.Feedback{}, fbErr(err, "读取反馈")
	}
	return fbFeedbackOf(sqlcgen.FbFeedback{
		ID: x.ID, ProjectID: x.ProjectID, Number: x.Number, AuthorID: x.AuthorID, Kind: x.Kind,
		Title: x.Title, Body: x.Body, Env: x.Env, Visibility: x.Visibility, Status: x.Status,
		Priority: x.Priority, DuplicateOf: x.DuplicateOf, VoteCount: x.VoteCount, Version: x.Version,
		Source: x.Source, ApiKeyID: x.ApiKeyID, Contact: x.Contact, PageUrl: x.PageUrl,
		LastActivityAt: x.LastActivityAt, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
	}, x.KeyPrefix, x.ProjectSlug, x.ProjectName, x.AuthorHandle, x.AuthorNickname, x.Voted), nil
}

func (r *FeedbackRepo) VisibleFeedback(ctx context.Context, v dom.Viewer, f dom.FeedbackFilter) ([]dom.Feedback, int64, error) {
	viewer := nullID(v.UserID)
	pid := nullID(f.ProjectID)
	status := nullText(string(f.Status))
	kind := nullText(string(f.Kind))
	author := nullID(f.AuthorID)
	pattern := nullText(f.Q)
	rows, err := r.q.ListVisibleFbFeedback(ctx, sqlcgen.ListVisibleFbFeedbackParams{
		ViewerID: viewer, ProjectID: pid, Status: status, Kind: kind, AuthorID: author,
		Pattern: pattern, SortBy: f.Sort,
		PageLimit: int32(f.PageSize), PageOffset: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取反馈列表")
	}
	total, err := r.q.CountVisibleFbFeedback(ctx, sqlcgen.CountVisibleFbFeedbackParams{
		ViewerID: viewer, ProjectID: pid, Status: status, Kind: kind, AuthorID: author, Pattern: pattern,
	})
	if err != nil {
		return nil, 0, fbErr(err, "统计反馈")
	}
	out := make([]dom.Feedback, 0, len(rows))
	for _, x := range rows {
		out = append(out, fbFeedbackOf(sqlcgen.FbFeedback{
			ID: x.ID, ProjectID: x.ProjectID, Number: x.Number, AuthorID: x.AuthorID, Kind: x.Kind,
			Title: x.Title, Body: x.Body, Env: x.Env, Visibility: x.Visibility, Status: x.Status,
			Priority: x.Priority, DuplicateOf: x.DuplicateOf, VoteCount: x.VoteCount, Version: x.Version,
			Source: x.Source, ApiKeyID: x.ApiKeyID, Contact: x.Contact, PageUrl: x.PageUrl,
			LastActivityAt: x.LastActivityAt, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}, x.KeyPrefix, x.ProjectSlug, x.ProjectName, x.AuthorHandle, x.AuthorNickname, x.Voted))
	}
	return out, total, nil
}

func (r *FeedbackRepo) AdminFeedback(ctx context.Context, f dom.FeedbackFilter) ([]dom.Feedback, int64, error) {
	pid := nullID(f.ProjectID)
	status := nullText(string(f.Status))
	kind := nullText(string(f.Kind))
	priority := nullText(string(f.Priority))
	visibility := nullText(string(f.Visibility))
	source := nullText(string(f.Source))
	pattern := nullText(f.Q)
	rows, err := r.q.ListAdminFbFeedback(ctx, sqlcgen.ListAdminFbFeedbackParams{
		ProjectID: pid, Status: status, Kind: kind, Priority: priority, Visibility: visibility,
		Source: source, Pattern: pattern, SortBy: f.Sort,
		PageLimit: int32(f.PageSize), PageOffset: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取反馈列表")
	}
	total, err := r.q.CountAdminFbFeedback(ctx, sqlcgen.CountAdminFbFeedbackParams{
		ProjectID: pid, Status: status, Kind: kind, Priority: priority, Visibility: visibility,
		Source: source, Pattern: pattern,
	})
	if err != nil {
		return nil, 0, fbErr(err, "统计反馈")
	}
	out := make([]dom.Feedback, 0, len(rows))
	for _, x := range rows {
		out = append(out, fbFeedbackOf(sqlcgen.FbFeedback{
			ID: x.ID, ProjectID: x.ProjectID, Number: x.Number, AuthorID: x.AuthorID, Kind: x.Kind,
			Title: x.Title, Body: x.Body, Env: x.Env, Visibility: x.Visibility, Status: x.Status,
			Priority: x.Priority, DuplicateOf: x.DuplicateOf, VoteCount: x.VoteCount, Version: x.Version,
			Source: x.Source, ApiKeyID: x.ApiKeyID, Contact: x.Contact, PageUrl: x.PageUrl,
			LastActivityAt: x.LastActivityAt, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}, x.KeyPrefix, x.ProjectSlug, x.ProjectName, x.AuthorHandle, x.AuthorNickname, false))
	}
	return out, total, nil
}

func (r *FeedbackRepo) StatusCounts(ctx context.Context, projectID int64) ([]dom.StatusCount, error) {
	rows, err := r.q.CountFbFeedbackByStatus(ctx, nullID(projectID))
	if err != nil {
		return nil, fbErr(err, "统计反馈状态")
	}
	out := make([]dom.StatusCount, 0, len(rows))
	for _, x := range rows {
		out = append(out, dom.StatusCount{Status: dom.Status(x.Status), Total: x.Total})
	}
	return out, nil
}

// Transition 在一个事务里带乐观锁更新反馈、写状态历史，并在需要时记一次贡献。
func (r *FeedbackRepo) Transition(ctx context.Context, id int64, in dom.TriageInput, award *dom.Award) (dom.Feedback, error) {
	var out dom.Feedback
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		cur, err := q.GetFbFeedback(ctx, id)
		if err != nil {
			return fbErr(err, "读取反馈")
		}
		f, err := q.TransitionFbFeedback(ctx, sqlcgen.TransitionFbFeedbackParams{
			Status: string(in.Status), Priority: string(in.Priority), Visibility: string(in.Visibility),
			DuplicateOf: nullID(in.DuplicateOf), ID: id, ExpectedVersion: in.ExpectedVersion,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errs.ErrConflict.WithMessage("这条反馈已被其他人更新，请刷新后再保存")
			}
			return fbErr(err, "保存反馈状态")
		}
		if _, err := q.CreateFbFeedbackEvent(ctx, sqlcgen.CreateFbFeedbackEventParams{
			FeedbackID: id, FromStatus: cur.Status, ToStatus: string(in.Status),
			Note: in.Note, InternalNote: in.InternalNote, ActorAdmin: nullID(in.ActorAdminID),
		}); err != nil {
			return fbErr(err, "写入状态历史")
		}
		if award != nil {
			// source_key 冲突表示这次贡献已记过，不重复加分
			if _, err := q.CreateFbContribution(ctx, sqlcgen.CreateFbContributionParams{
				UserID: award.UserID, ProjectID: nullID(award.ProjectID), FeedbackID: nullID(id),
				Kind: award.Kind, Points: award.Points, Reason: award.Reason, SourceKey: award.SourceKey,
			}); err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					return fbErr(err, "记录贡献")
				}
			} else if _, err := q.BumpFbUserPoints(ctx, sqlcgen.BumpFbUserPointsParams{
				Delta: award.Points, ID: award.UserID,
			}); err != nil {
				return fbErr(err, "更新贡献值")
			}
		}
		out = fbFeedbackOf(f, "", "", "", "", "", false)
		return nil
	})
	if err != nil {
		return dom.Feedback{}, err
	}
	// 回读带项目与作者信息的完整视图，供审计与响应使用
	full, err := r.Feedback(ctx, out.ID)
	if err != nil {
		return out, nil
	}
	return full, nil
}

func (r *FeedbackRepo) Events(ctx context.Context, feedbackID int64) ([]dom.Event, error) {
	rows, err := r.q.ListFbFeedbackEvents(ctx, feedbackID)
	if err != nil {
		return nil, fbErr(err, "读取状态历史")
	}
	out := make([]dom.Event, 0, len(rows))
	for _, e := range rows {
		out = append(out, dom.Event{
			ID: e.ID, FromStatus: e.FromStatus, ToStatus: dom.Status(e.ToStatus),
			Note: e.Note, InternalNote: e.InternalNote, ActorName: e.ActorName, CreatedAt: fbTime(e.CreatedAt),
		})
	}
	return out, nil
}

// ---------- 投票 ----------

// SetVote 设置或取消投票，并按明细重算票数（重复点击不会算错）。
func (r *FeedbackRepo) SetVote(ctx context.Context, feedbackID, userID int64, active bool) (dom.Feedback, error) {
	var out dom.Feedback
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if active {
			if err := q.CreateFbVote(ctx, sqlcgen.CreateFbVoteParams{FeedbackID: feedbackID, UserID: userID}); err != nil {
				return fbErr(err, "投票")
			}
		} else if err := q.DeleteFbVote(ctx, sqlcgen.DeleteFbVoteParams{FeedbackID: feedbackID, UserID: userID}); err != nil {
			return fbErr(err, "取消投票")
		}
		f, err := q.SyncFbFeedbackVotes(ctx, feedbackID)
		if err != nil {
			return fbErr(err, "更新票数")
		}
		out = fbFeedbackOf(f, "", "", "", "", "", active)
		return nil
	})
	if err != nil {
		return dom.Feedback{}, err
	}
	return out, nil
}
