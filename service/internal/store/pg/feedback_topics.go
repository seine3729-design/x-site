package pg

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dom "xsite/service/internal/domain/feedback"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// FeedbackTopicRepo 是议题区的仓储实现（迁移 00016）。
type FeedbackTopicRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewFeedbackTopicRepo(pool *pgxpool.Pool) *FeedbackTopicRepo {
	return &FeedbackTopicRepo{q: sqlcgen.New(pool), pool: pool}
}

// ---------- 类型转换 ----------

func fbTopicOf(t sqlcgen.FbTopic, projectName, projectSlug, projectCover string) dom.Topic {
	return dom.Topic{
		ID: t.ID, ProjectID: t.ProjectID, ProjectName: projectName, ProjectSlug: projectSlug,
		ProjectCover: projectCover, Title: t.Title, Body: t.Body, AnswerHint: t.AnswerHint,
		Status: dom.TopicStatus(t.Status), Visibility: dom.Visibility(t.Visibility), Pinned: t.Pinned,
		AnswerCount: t.AnswerCount, EndsAt: fbTimePtr(t.EndsAt), PublishedAt: fbTimePtr(t.PublishedAt),
		CreatedAt: fbTime(t.CreatedAt), UpdatedAt: fbTime(t.UpdatedAt),
	}
}

func fbAnswerOf(a sqlcgen.FbTopicAnswer, testerNo int64, handle, nickname string, helpfulByMe bool) dom.Answer {
	return dom.Answer{
		ID: a.ID, TopicID: a.TopicID, ParentID: a.ParentID.Int64, TesterNo: testerNo,
		Handle: handle, Nickname: nickname, Body: a.Body, Featured: a.Featured,
		HelpfulCount: a.HelpfulCount, HelpfulByMe: helpfulByMe, Status: a.Status,
		CreatedAt: fbTime(a.CreatedAt),
	}
}

func fbAPIKeyOf(k sqlcgen.FbApiKey, projectName, projectSlug string) dom.APIKey {
	origins := k.AllowedOrigins
	if origins == nil {
		origins = []string{}
	}
	return dom.APIKey{
		ID: k.ID, ProjectID: k.ProjectID, ProjectName: projectName, ProjectSlug: projectSlug,
		Name: k.Name, KeyHint: k.KeyHint, DefaultVisibility: dom.Visibility(k.DefaultVisibility),
		AllowedOrigins: origins, DailyLimit: k.DailyLimit, Status: k.Status, UsedCount: k.UsedCount,
		LastUsedAt: fbTimePtr(k.LastUsedAt), RevokedAt: fbTimePtr(k.RevokedAt), CreatedAt: fbTime(k.CreatedAt),
	}
}

// ---------- 议题 ----------

func topicParams(in dom.TopicInput, publishedAt *time.Time) sqlcgen.CreateFbTopicParams {
	return sqlcgen.CreateFbTopicParams{
		ProjectID: in.ProjectID, Title: in.Title, Body: in.Body, AnswerHint: in.AnswerHint,
		Status: string(in.Status), Visibility: string(in.Visibility), Pinned: in.Pinned,
		EndsAt: tsOf(in.EndsAt), PublishedAt: tsOf(publishedAt), CreatedBy: nullID(in.CreatedBy),
	}
}

func (r *FeedbackTopicRepo) CreateTopic(ctx context.Context, in dom.TopicInput, publishedAt *time.Time) (dom.Topic, error) {
	t, err := r.q.CreateFbTopic(ctx, topicParams(in, publishedAt))
	if err != nil {
		return dom.Topic{}, fbErr(err, "创建议题")
	}
	return fbTopicOf(t, "", "", ""), nil
}

func (r *FeedbackTopicRepo) UpdateTopic(ctx context.Context, id int64, in dom.TopicInput, publishedAt *time.Time) (dom.Topic, error) {
	c := topicParams(in, publishedAt)
	t, err := r.q.UpdateFbTopic(ctx, sqlcgen.UpdateFbTopicParams{
		Title: c.Title, Body: c.Body, AnswerHint: c.AnswerHint, Status: c.Status,
		Visibility: c.Visibility, Pinned: c.Pinned, EndsAt: c.EndsAt, PublishedAt: c.PublishedAt, ID: id,
	})
	if err != nil {
		return dom.Topic{}, fbErr(err, "保存议题")
	}
	return fbTopicOf(t, "", "", ""), nil
}

func (r *FeedbackTopicRepo) DeleteTopic(ctx context.Context, id int64) (dom.Topic, error) {
	t, err := r.q.DeleteFbTopic(ctx, id)
	if err != nil {
		return dom.Topic{}, fbErr(err, "删除议题")
	}
	return fbTopicOf(t, "", "", ""), nil
}

func (r *FeedbackTopicRepo) Topic(ctx context.Context, id int64) (dom.Topic, error) {
	x, err := r.q.GetFbTopic(ctx, id)
	if err != nil {
		return dom.Topic{}, fbErr(err, "读取议题")
	}
	return fbTopicOf(sqlcgen.FbTopic{
		ID: x.ID, ProjectID: x.ProjectID, Title: x.Title, Body: x.Body, AnswerHint: x.AnswerHint,
		Status: x.Status, Visibility: x.Visibility, Pinned: x.Pinned, AnswerCount: x.AnswerCount,
		EndsAt: x.EndsAt, PublishedAt: x.PublishedAt, CreatedBy: x.CreatedBy,
		CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
	}, x.ProjectName, x.ProjectSlug, ""), nil
}

func (r *FeedbackTopicRepo) ListTopics(ctx context.Context, f dom.TopicFilter) ([]dom.Topic, int64, error) {
	pid := nullID(f.ProjectID)
	status := nullText(string(f.Status))
	pattern := nullText(f.Q)
	rows, err := r.q.ListFbTopics(ctx, sqlcgen.ListFbTopicsParams{
		ProjectID: pid, Status: status, Pattern: pattern,
		PageLimit: int32(f.PageSize), PageOffset: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取议题列表")
	}
	total, err := r.q.CountFbTopics(ctx, sqlcgen.CountFbTopicsParams{ProjectID: pid, Status: status, Pattern: pattern})
	if err != nil {
		return nil, 0, fbErr(err, "统计议题")
	}
	out := make([]dom.Topic, 0, len(rows))
	for _, x := range rows {
		out = append(out, fbTopicOf(sqlcgen.FbTopic{
			ID: x.ID, ProjectID: x.ProjectID, Title: x.Title, Body: x.Body, AnswerHint: x.AnswerHint,
			Status: x.Status, Visibility: x.Visibility, Pinned: x.Pinned, AnswerCount: x.AnswerCount,
			EndsAt: x.EndsAt, PublishedAt: x.PublishedAt, CreatedBy: x.CreatedBy,
			CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}, x.ProjectName, x.ProjectSlug, ""))
	}
	return out, total, nil
}

func (r *FeedbackTopicRepo) VisibleTopics(ctx context.Context, v dom.Viewer, f dom.TopicFilter) ([]dom.Topic, int64, error) {
	viewer := nullID(v.UserID)
	pid := nullID(f.ProjectID)
	status := nullText(string(f.Status))
	rows, err := r.q.ListVisibleFbTopics(ctx, sqlcgen.ListVisibleFbTopicsParams{
		ViewerID: viewer, ProjectID: pid, Status: status,
		PageLimit: int32(f.PageSize), PageOffset: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取议题列表")
	}
	total, err := r.q.CountVisibleFbTopics(ctx, sqlcgen.CountVisibleFbTopicsParams{
		ViewerID: viewer, ProjectID: pid, Status: status,
	})
	if err != nil {
		return nil, 0, fbErr(err, "统计议题")
	}
	out := make([]dom.Topic, 0, len(rows))
	for _, x := range rows {
		out = append(out, fbTopicOf(sqlcgen.FbTopic{
			ID: x.ID, ProjectID: x.ProjectID, Title: x.Title, Body: x.Body, AnswerHint: x.AnswerHint,
			Status: x.Status, Visibility: x.Visibility, Pinned: x.Pinned, AnswerCount: x.AnswerCount,
			EndsAt: x.EndsAt, PublishedAt: x.PublishedAt, CreatedBy: x.CreatedBy,
			CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}, x.ProjectName, x.ProjectSlug, x.ProjectCover))
	}
	return out, total, nil
}

func (r *FeedbackTopicRepo) VisibleTopic(ctx context.Context, v dom.Viewer, id int64) (dom.Topic, bool, error) {
	x, err := r.q.GetVisibleFbTopic(ctx, sqlcgen.GetVisibleFbTopicParams{ViewerID: nullID(v.UserID), ID: id})
	if err != nil {
		return dom.Topic{}, false, fbErr(err, "读取议题")
	}
	t := fbTopicOf(sqlcgen.FbTopic{
		ID: x.ID, ProjectID: x.ProjectID, Title: x.Title, Body: x.Body, AnswerHint: x.AnswerHint,
		Status: x.Status, Visibility: x.Visibility, Pinned: x.Pinned, AnswerCount: x.AnswerCount,
		EndsAt: x.EndsAt, PublishedAt: x.PublishedAt, CreatedBy: x.CreatedBy,
		CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
	}, x.ProjectName, x.ProjectSlug, x.ProjectCover)
	return t, x.IsMember, nil
}

// ---------- 回答 ----------

// answerBadge 读取回答作者的展示署名（内测编号与昵称）。
// 写入类查询用 RETURNING *，关联不到 fb_user，所以这里补一次主键查询：
// 否则发布回答、点「有道理」或设精选之后返回的对象没有署名，前端据此刷新列表会让署名凭空消失。
// 署名只用于展示，查不到就留空，不让它影响写入结果。
func answerBadge(ctx context.Context, q *sqlcgen.Queries, userID pgtype.Int8) (int64, string, string) {
	if !userID.Valid {
		return 0, "", ""
	}
	b, err := q.GetFbUserBadge(ctx, userID.Int64)
	if err != nil {
		return 0, "", ""
	}
	return b.TesterNo, b.Handle, b.Nickname
}

// CreateAnswer 在一个事务里写回答并重算议题回答数。
func (r *FeedbackTopicRepo) CreateAnswer(ctx context.Context, topicID, userID, parentID int64, body string) (dom.Answer, error) {
	var out dom.Answer
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		a, err := q.CreateFbTopicAnswer(ctx, sqlcgen.CreateFbTopicAnswerParams{
			TopicID: topicID, ParentID: nullID(parentID), UserID: nullID(userID), Body: body,
		})
		if err != nil {
			return fbErr(err, "发布回答")
		}
		if _, err := q.SyncFbTopicAnswerCount(ctx, topicID); err != nil {
			return fbErr(err, "更新回答数")
		}
		no, handle, nickname := answerBadge(ctx, q, a.UserID)
		out = fbAnswerOf(a, no, handle, nickname, false)
		// 前台不下发内部状态
		out.Status = ""
		return nil
	})
	if err != nil {
		return dom.Answer{}, err
	}
	return out, nil
}

func (r *FeedbackTopicRepo) Answer(ctx context.Context, id int64) (dom.Answer, error) {
	a, err := r.q.GetFbTopicAnswer(ctx, id)
	if err != nil {
		return dom.Answer{}, fbErr(err, "读取回答")
	}
	return fbAnswerOf(a, 0, "", "", false), nil
}

func (r *FeedbackTopicRepo) VisibleAnswers(ctx context.Context, v dom.Viewer, topicID int64, page, pageSize int) ([]dom.Answer, int64, error) {
	rows, err := r.q.ListVisibleFbAnswers(ctx, sqlcgen.ListVisibleFbAnswersParams{
		ViewerID: nullID(v.UserID), TopicID: topicID,
		PageLimit: int32(pageSize), PageOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取回答列表")
	}
	total, err := r.q.CountVisibleFbAnswers(ctx, topicID)
	if err != nil {
		return nil, 0, fbErr(err, "统计回答")
	}
	out := make([]dom.Answer, 0, len(rows))
	for _, x := range rows {
		a := fbAnswerOf(sqlcgen.FbTopicAnswer{
			ID: x.ID, TopicID: x.TopicID, ParentID: x.ParentID, UserID: x.UserID, Body: x.Body,
			Featured: x.Featured, HelpfulCount: x.HelpfulCount, Status: x.Status,
			CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}, x.TesterNo.Int64, x.Handle, x.Nickname, x.HelpfulByMe)
		// 前台不下发内部状态
		a.Status = ""
		out = append(out, a)
	}
	return out, total, nil
}

func (r *FeedbackTopicRepo) AdminAnswers(ctx context.Context, topicID int64, page, pageSize int) ([]dom.Answer, int64, error) {
	rows, err := r.q.ListAdminFbAnswers(ctx, sqlcgen.ListAdminFbAnswersParams{
		TopicID: topicID, PageLimit: int32(pageSize), PageOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取回答列表")
	}
	total, err := r.q.CountAdminFbAnswers(ctx, topicID)
	if err != nil {
		return nil, 0, fbErr(err, "统计回答")
	}
	out := make([]dom.Answer, 0, len(rows))
	for _, x := range rows {
		out = append(out, fbAnswerOf(sqlcgen.FbTopicAnswer{
			ID: x.ID, TopicID: x.TopicID, ParentID: x.ParentID, UserID: x.UserID, Body: x.Body,
			Featured: x.Featured, HelpfulCount: x.HelpfulCount, Status: x.Status,
			CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}, x.TesterNo.Int64, x.Handle, x.Nickname, false))
	}
	return out, total, nil
}

func (r *FeedbackTopicRepo) SetAnswerFeatured(ctx context.Context, id int64, featured bool) (dom.Answer, error) {
	a, err := r.q.SetFbAnswerFeatured(ctx, sqlcgen.SetFbAnswerFeaturedParams{Featured: featured, ID: id})
	if err != nil {
		return dom.Answer{}, fbErr(err, "标记精选")
	}
	no, handle, nickname := answerBadge(ctx, r.q, a.UserID)
	return fbAnswerOf(a, no, handle, nickname, false), nil
}

// SetAnswerStatus 在一个事务里改可见状态并重算议题回答数。
func (r *FeedbackTopicRepo) SetAnswerStatus(ctx context.Context, id int64, status string) (dom.Answer, error) {
	var out dom.Answer
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		a, err := q.SetFbAnswerStatus(ctx, sqlcgen.SetFbAnswerStatusParams{Status: status, ID: id})
		if err != nil {
			return fbErr(err, "保存回答状态")
		}
		if _, err := q.SyncFbTopicAnswerCount(ctx, a.TopicID); err != nil {
			return fbErr(err, "更新回答数")
		}
		no, handle, nickname := answerBadge(ctx, q, a.UserID)
		out = fbAnswerOf(a, no, handle, nickname, false)
		return nil
	})
	if err != nil {
		return dom.Answer{}, err
	}
	return out, nil
}

// SetHelpful 设置或取消「有道理」，并按明细重算计数。
func (r *FeedbackTopicRepo) SetHelpful(ctx context.Context, answerID, userID int64, active bool) (dom.Answer, error) {
	var out dom.Answer
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if active {
			if err := q.CreateFbAnswerHelpful(ctx, sqlcgen.CreateFbAnswerHelpfulParams{AnswerID: answerID, UserID: userID}); err != nil {
				return fbErr(err, "点有道理")
			}
		} else if err := q.DeleteFbAnswerHelpful(ctx, sqlcgen.DeleteFbAnswerHelpfulParams{AnswerID: answerID, UserID: userID}); err != nil {
			return fbErr(err, "取消有道理")
		}
		a, err := q.SyncFbAnswerHelpful(ctx, answerID)
		if err != nil {
			return fbErr(err, "更新有道理数")
		}
		no, handle, nickname := answerBadge(ctx, q, a.UserID)
		out = fbAnswerOf(a, no, handle, nickname, active)
		out.Status = ""
		return nil
	})
	if err != nil {
		return dom.Answer{}, err
	}
	return out, nil
}

// ---------- 开放提交接口的 API Key ----------

// FeedbackAPIKeyRepo 是开放提交接口的仓储实现。
type FeedbackAPIKeyRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewFeedbackAPIKeyRepo(pool *pgxpool.Pool) *FeedbackAPIKeyRepo {
	return &FeedbackAPIKeyRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *FeedbackAPIKeyRepo) CreateAPIKey(ctx context.Context, keyHash, hint string, in dom.APIKeyInput) (dom.APIKey, error) {
	k, err := r.q.CreateFbApiKey(ctx, sqlcgen.CreateFbApiKeyParams{
		ProjectID: in.ProjectID, Name: in.Name, KeyHash: keyHash, KeyHint: hint,
		DefaultVisibility: string(in.DefaultVisibility), AllowedOrigins: in.AllowedOrigins,
		DailyLimit: in.DailyLimit, Status: in.Status, CreatedBy: in.CreatedBy,
	})
	if err != nil {
		return dom.APIKey{}, fbErr(err, "创建 API Key")
	}
	return fbAPIKeyOf(k, "", ""), nil
}

func (r *FeedbackAPIKeyRepo) UpdateAPIKey(ctx context.Context, id int64, in dom.APIKeyInput) (dom.APIKey, error) {
	k, err := r.q.UpdateFbApiKey(ctx, sqlcgen.UpdateFbApiKeyParams{
		Name: in.Name, DefaultVisibility: string(in.DefaultVisibility), AllowedOrigins: in.AllowedOrigins,
		DailyLimit: in.DailyLimit, Status: in.Status, ID: id,
	})
	if err != nil {
		return dom.APIKey{}, fbErr(err, "保存 API Key")
	}
	return fbAPIKeyOf(k, "", ""), nil
}

func (r *FeedbackAPIKeyRepo) RevokeAPIKey(ctx context.Context, id int64) (dom.APIKey, error) {
	k, err := r.q.RevokeFbApiKey(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dom.APIKey{}, errs.ErrConflict.WithMessage("该 Key 不存在或已作废")
		}
		return dom.APIKey{}, fbErr(err, "作废 API Key")
	}
	return fbAPIKeyOf(k, "", ""), nil
}

func (r *FeedbackAPIKeyRepo) APIKey(ctx context.Context, id int64) (dom.APIKey, error) {
	x, err := r.q.GetFbApiKey(ctx, id)
	if err != nil {
		return dom.APIKey{}, fbErr(err, "读取 API Key")
	}
	return fbAPIKeyOf(sqlcgen.FbApiKey{
		ID: x.ID, ProjectID: x.ProjectID, Name: x.Name, KeyHash: x.KeyHash, KeyHint: x.KeyHint,
		DefaultVisibility: x.DefaultVisibility, AllowedOrigins: x.AllowedOrigins, DailyLimit: x.DailyLimit,
		Status: x.Status, UsedCount: x.UsedCount, LastUsedAt: x.LastUsedAt, RevokedAt: x.RevokedAt,
		CreatedBy: x.CreatedBy, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
	}, x.ProjectName, x.ProjectSlug), nil
}

func (r *FeedbackAPIKeyRepo) ListAPIKeys(ctx context.Context, projectID int64, page, pageSize int) ([]dom.APIKey, int64, error) {
	pid := nullID(projectID)
	rows, err := r.q.ListFbApiKeys(ctx, sqlcgen.ListFbApiKeysParams{
		ProjectID: pid, PageLimit: int32(pageSize), PageOffset: int32((page - 1) * pageSize),
	})
	if err != nil {
		return nil, 0, fbErr(err, "读取 API Key 列表")
	}
	total, err := r.q.CountFbApiKeys(ctx, pid)
	if err != nil {
		return nil, 0, fbErr(err, "统计 API Key")
	}
	out := make([]dom.APIKey, 0, len(rows))
	for _, x := range rows {
		out = append(out, fbAPIKeyOf(sqlcgen.FbApiKey{
			ID: x.ID, ProjectID: x.ProjectID, Name: x.Name, KeyHash: x.KeyHash, KeyHint: x.KeyHint,
			DefaultVisibility: x.DefaultVisibility, AllowedOrigins: x.AllowedOrigins, DailyLimit: x.DailyLimit,
			Status: x.Status, UsedCount: x.UsedCount, LastUsedAt: x.LastUsedAt, RevokedAt: x.RevokedAt,
			CreatedBy: x.CreatedBy, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
		}, x.ProjectName, x.ProjectSlug))
	}
	return out, total, nil
}

func (r *FeedbackAPIKeyRepo) APIKeyByHash(ctx context.Context, keyHash string) (dom.APIKey, dom.ProjectStatus, error) {
	x, err := r.q.GetFbApiKeyByHash(ctx, keyHash)
	if err != nil {
		return dom.APIKey{}, "", fbErr(err, "读取 API Key")
	}
	key := fbAPIKeyOf(sqlcgen.FbApiKey{
		ID: x.ID, ProjectID: x.ProjectID, Name: x.Name, KeyHash: x.KeyHash, KeyHint: x.KeyHint,
		DefaultVisibility: x.DefaultVisibility, AllowedOrigins: x.AllowedOrigins, DailyLimit: x.DailyLimit,
		Status: x.Status, UsedCount: x.UsedCount, LastUsedAt: x.LastUsedAt, RevokedAt: x.RevokedAt,
		CreatedBy: x.CreatedBy, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt,
	}, x.ProjectName, x.ProjectSlug)
	return key, dom.ProjectStatus(x.ProjectStatus), nil
}

// CreateFeedbackFromAPI 在一个事务里分配编号、写反馈与首条状态历史，并累计 Key 的使用次数。
func (r *FeedbackAPIKeyRepo) CreateFeedbackFromAPI(ctx context.Context, key dom.APIKey, in dom.IntakeInput) (dom.Feedback, error) {
	var out dom.Feedback
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		next, err := q.NextFbFeedbackNumber(ctx, key.ProjectID)
		if err != nil {
			return fbErr(err, "分配反馈编号")
		}
		f, err := q.CreateFbFeedbackFromAPI(ctx, sqlcgen.CreateFbFeedbackFromAPIParams{
			ProjectID: key.ProjectID, Number: next.Number, Kind: string(in.Kind), Title: in.Title,
			Body: in.Body, Env: in.Env, Visibility: string(key.DefaultVisibility),
			ApiKeyID: nullID(key.ID), Contact: in.Contact, PageUrl: in.PageURL,
		})
		if err != nil {
			return fbErr(err, "创建反馈")
		}
		if _, err := q.CreateFbFeedbackEvent(ctx, sqlcgen.CreateFbFeedbackEventParams{
			FeedbackID: f.ID, FromStatus: "", ToStatus: string(dom.StatusSubmitted),
			Note: "已收到，等待官方确认", InternalNote: "来自开放接口：" + key.Name,
		}); err != nil {
			return fbErr(err, "写入状态历史")
		}
		if err := q.TouchFbApiKey(ctx, key.ID); err != nil {
			return fbErr(err, "更新 Key 使用次数")
		}
		out = fbFeedbackOf(f, next.KeyPrefix, key.ProjectSlug, key.ProjectName, "", "", false)
		return nil
	})
	if err != nil {
		return dom.Feedback{}, err
	}
	return out, nil
}
