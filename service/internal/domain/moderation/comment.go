package moderation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strconv"
	"time"

	"xsite/service/internal/errs"
)

const (
	maxCommentRunes = 500
	commentDedupTTL = 10 // 秒：同作者相同内容的去重窗口（docs/03 第 5.1 节）
)

// CommentStatus 取值。
const (
	StatusPending = "pending"
	StatusVisible = "visible"
	StatusHidden  = "hidden"
)

// Comment 是一条评论（领域模型 / 发表后回执）。
type Comment struct {
	ID        int64     `json:"id"`
	EpisodeID int64     `json:"episode_id"`
	ParentID  int64     `json:"parent_id,omitempty"`
	Content   string    `json:"content"` // 可能已按 mask 替换
	Status    string    `json:"status"`  // visible 立即可见；pending 审核中
	LikeCount int64     `json:"like_count"`
	CreatedAt time.Time `json:"created_at"`
}

// AuthorView 是评论作者的展示信息（游客显示「游客」，不暴露用户名）。
type AuthorView struct {
	IsGuest  bool   `json:"is_guest"`
	UserID   int64  `json:"user_id,omitempty"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
}

// CommentView 是评论列表的一项。
type CommentView struct {
	ID         int64      `json:"id"`
	ParentID   int64      `json:"parent_id,omitempty"`
	Content    string     `json:"content"`
	ImageCount int        `json:"image_count"`
	LikeCount  int64      `json:"like_count"`
	CreatedAt  time.Time  `json:"created_at"`
	Author     AuthorView `json:"author"`
}

// CommentPage 是一页评论（游标分页）。
type CommentPage struct {
	List       []CommentView `json:"list"`
	NextCursor string        `json:"next_cursor"`
}

// NewComment 是要创建的评论（仓储层用）。
type NewComment struct {
	EpisodeID int64
	Author    Author
	ParentID  int64
	Content   string
	Status    string
}

// CommentRepo 读写评论表（实现见 internal/store/pg）。
type CommentRepo interface {
	Create(ctx context.Context, in NewComment) (Comment, error)
	List(ctx context.Context, episodeID int64, hot bool, cursor string, limit int) (items []CommentView, nextCursor string, err error)
	// AuthorOf 返回评论作者、所属分集与是否已删，用于删除鉴权与通知收件人解析。
	AuthorOf(ctx context.Context, id int64) (author Author, episodeID int64, deleted bool, err error)
	SoftDelete(ctx context.Context, id int64) error
	// Like 点赞（幂等插入 + 累加计数），返回是否本次新增。
	Like(ctx context.Context, commentID, userID int64) (added bool, err error)
}

// Notifier 异步投递社区通知（评论被回复 / 被点赞 → 写 S2-3 的 notification）。实现见 internal/jobs（River 入队）。
// 只投递给注册用户；入队失败不影响评论 / 点赞本身（best-effort）。
type Notifier interface {
	Notify(ctx context.Context, recipientUserID int64, typ string, commentID, episodeID int64) error
}

// 通知类型（与 notification.type 枚举一致）。
const (
	NotifyReply = "reply"
	NotifyLike  = "like"
)

const commentListMax = 50

// CommentService 处理评论的发表、列表、删除、点赞（docs/03 第 5.1 节）。
type CommentService struct {
	repo     CommentRepo
	eval     Evaluator
	features Features
	policy   Policy
	users    Users
	limiter  Limiter
	notifier Notifier // 可空：nil 时不投递通知
}

func NewCommentService(repo CommentRepo, eval Evaluator, features Features, policy Policy, users Users, limiter Limiter, notifier Notifier) *CommentService {
	return &CommentService{repo: repo, eval: eval, features: features, policy: policy, users: users, limiter: limiter, notifier: notifier}
}

// CreateCommentInput 是发表评论的入参。
type CreateCommentInput struct {
	Author    Author
	EpisodeID int64
	ParentID  int64
	Content   string
}

// Create 发表评论：过功能开关 / 禁言 / 清洗 / 防刷 / 屏蔽词 / 先审后显，返回落库后的评论（含状态）。
func (s *CommentService) Create(ctx context.Context, in CreateCommentInput) (Comment, error) {
	if err := s.gate(ctx, in.Author, FlagComments); err != nil {
		return Comment{}, err
	}
	content := cleanContent(in.Content)
	if content == "" {
		return Comment{}, errs.ErrBadRequest.WithMessage("评论内容不能为空")
	}
	if runeLen(content) > maxCommentRunes {
		return Comment{}, errs.ErrBadRequest.WithMessage("评论最多 500 个字")
	}
	if ok, err := s.limiter.FirstPost(ctx, dedupKey("comment", in.Author, content)); err != nil {
		return Comment{}, err
	} else if !ok {
		return Comment{}, errDupContent
	}
	d := s.eval.Evaluate(content)
	if d.Blocked {
		return Comment{}, errBlocked
	}
	pre, err := s.policy.PreModeration(ctx)
	if err != nil {
		return Comment{}, err
	}
	status := StatusVisible
	if d.Review || pre {
		status = StatusPending
	}
	c, err := s.repo.Create(ctx, NewComment{
		EpisodeID: in.EpisodeID, Author: in.Author, ParentID: in.ParentID,
		Content: d.Text, Status: status,
	})
	if err == nil && c.Status == StatusVisible && in.ParentID > 0 {
		s.notifyReply(ctx, in, c)
	}
	return c, err
}

// notifyReply 在回复可见时通知父评论的注册用户作者（跳过游客作者与自己回复自己）。best-effort。
func (s *CommentService) notifyReply(ctx context.Context, in CreateCommentInput, reply Comment) {
	if s.notifier == nil {
		return
	}
	parent, _, deleted, err := s.repo.AuthorOf(ctx, in.ParentID)
	if err != nil || deleted || parent.UserID <= 0 || parent.UserID == in.Author.UserID {
		return
	}
	if err := s.notifier.Notify(ctx, parent.UserID, NotifyReply, reply.ID, in.EpisodeID); err != nil {
		slog.WarnContext(ctx, "投递评论回复通知失败", "parent_id", in.ParentID, "err", err)
	}
}

// List 返回某集的评论（游标分页）。只含显示中、未删的评论。
func (s *CommentService) List(ctx context.Context, episodeID int64, sort, cursor string, limit int) (CommentPage, error) {
	if episodeID <= 0 {
		return CommentPage{}, errs.ErrBadRequest.WithMessage("分集 ID 无效")
	}
	if limit <= 0 || limit > commentListMax {
		limit = commentListMax
	}
	hot := sort == "hot"
	items, next, err := s.repo.List(ctx, episodeID, hot, cursor, limit)
	if err != nil {
		return CommentPage{}, err
	}
	if items == nil {
		items = []CommentView{}
	}
	return CommentPage{List: items, NextCursor: next}, nil
}

// Delete 删除本人评论（软删）。不是本人（作者不匹配）返回 404，避免暴露他人评论的存在。
func (s *CommentService) Delete(ctx context.Context, author Author, id int64) error {
	owner, _, deleted, err := s.repo.AuthorOf(ctx, id)
	if err != nil {
		return err
	}
	if deleted || !sameAuthor(owner, author) {
		return errs.ErrNotFound.WithMessage("评论不存在")
	}
	return s.repo.SoftDelete(ctx, id)
}

// Like 点赞评论（仅注册用户，幂等）。本次新增点赞时通知被赞评论的注册用户作者。
func (s *CommentService) Like(ctx context.Context, userID, commentID int64) error {
	if userID <= 0 {
		return errs.ErrNeedLogin
	}
	added, err := s.repo.Like(ctx, commentID, userID)
	if err != nil {
		return err
	}
	if added {
		s.notifyLike(ctx, userID, commentID)
	}
	return nil
}

// notifyLike 通知被赞评论的注册用户作者（跳过游客作者与自赞）。best-effort。
func (s *CommentService) notifyLike(ctx context.Context, likerUserID, commentID int64) {
	if s.notifier == nil {
		return
	}
	author, episodeID, deleted, err := s.repo.AuthorOf(ctx, commentID)
	if err != nil || deleted || author.UserID <= 0 || author.UserID == likerUserID {
		return
	}
	if err := s.notifier.Notify(ctx, author.UserID, NotifyLike, commentID, episodeID); err != nil {
		slog.WarnContext(ctx, "投递评论点赞通知失败", "comment_id", commentID, "err", err)
	}
}

// gate 做发表前的公共校验：功能开关、游客互动开关、禁言（委托给公共 authorCanPost）。
func (s *CommentService) gate(ctx context.Context, author Author, feature string) error {
	return authorCanPost(ctx, s.features, s.users, author, feature)
}

func sameAuthor(a, b Author) bool {
	if a.UserID > 0 || b.UserID > 0 {
		return a.UserID == b.UserID
	}
	return a.GuestID == b.GuestID
}

// dedupKey 生成去重键：作用域 + 作者 + 内容哈希。
func dedupKey(scope string, a Author, content string) string {
	sum := sha256.Sum256([]byte(content))
	who := "u:" + strconv.FormatInt(a.UserID, 10)
	if a.IsGuest() {
		who = "g:" + a.GuestID.String()
	}
	return scope + ":" + who + ":" + hex.EncodeToString(sum[:12])
}
