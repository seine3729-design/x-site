package jobs

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// 社区通知（S2-5，docs/03 第 5.1/6 节）：评论被回复 / 被点赞后，异步写一条站内信给注册用户作者。
// 收件人与「是否该通知」（跳过游客、自赞 / 自回复）在入队前由领域层判定；worker 只负责落库。

// CommunityNotifyArgs 是一条待投递的社区通知。
type CommunityNotifyArgs struct {
	UserID    int64  `json:"user_id"`    // 收件人（注册用户）
	Type      string `json:"type"`       // reply / like
	CommentID int64  `json:"comment_id"` // 关联评论（回复时是新回复、点赞时是被赞评论）
	EpisodeID int64  `json:"episode_id"` // 便于前台跳转到对应分集
}

func (CommunityNotifyArgs) Kind() string { return "community_notify" }

// CommunityNotifier 把通知写入 notification 表（由 store/pg.MeRepo 实现）。
type CommunityNotifier interface {
	InsertNotification(ctx context.Context, userID int64, typ string, payload []byte) error
}

// CommunityNotifyWorker 消费社区通知任务，写入站内信。
type CommunityNotifyWorker struct {
	river.WorkerDefaults[CommunityNotifyArgs]
	Notifier CommunityNotifier
}

func (w *CommunityNotifyWorker) Work(ctx context.Context, job *river.Job[CommunityNotifyArgs]) error {
	payload, err := json.Marshal(map[string]int64{
		"comment_id": job.Args.CommentID,
		"episode_id": job.Args.EpisodeID,
	})
	if err != nil {
		return err
	}
	return w.Notifier.InsertNotification(ctx, job.Args.UserID, job.Args.Type, payload)
}

// ConfigureCommunity 注册社区通知 worker（入 push 队列）。notifier 为 nil 时不注册。
func ConfigureCommunity(cfg *river.Config, notifier CommunityNotifier) {
	if notifier == nil {
		return
	}
	river.AddWorker(cfg.Workers, &CommunityNotifyWorker{Notifier: notifier})
}

// CommunityNotifyEnqueuer 在 API 进程入队社区通知，实现 moderation.Notifier。
type CommunityNotifyEnqueuer struct {
	River *river.Client[pgx.Tx]
}

func (e *CommunityNotifyEnqueuer) Notify(ctx context.Context, recipientUserID int64, typ string, commentID, episodeID int64) error {
	_, err := e.River.Insert(ctx, CommunityNotifyArgs{
		UserID: recipientUserID, Type: typ, CommentID: commentID, EpisodeID: episodeID,
	}, &river.InsertOpts{Queue: QueuePush})
	return err
}
