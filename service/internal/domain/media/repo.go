package media

import (
	"context"
	"errors"
	"time"

	"uuid"

	"xsite/service/internal/errs"
)

var (
	// ErrActive 由仓储在入队时发现该集已有排队或进行中的任务时返回。
	ErrActive = errs.ErrConflict.WithMessage("该集正在转封装，请等当前任务结束后再提交")
	// ErrNoSource 表示没有填写源文件，且该集没有可沿用的上一个任务。
	ErrNoSource = errs.ErrBadRequest.WithMessage("请填写源文件路径")
	// ErrJobGone 表示任务已结束或已被删除（分集被删除时任务随之删除），worker 不再处理。
	ErrJobGone = errors.New("media: 转封装任务已结束或不存在")
)

// StaleAfter 是 running 任务多久没有更新进度视为进程已退出：下一次提交时把它标为失败。
// worker 运行中每分钟至少更新一次进度。
const StaleAfter = 10 * time.Minute

// Repo 读写转封装任务、媒体对象与内容密钥。
type Repo interface {
	// CreateJob 在一个事务内：锁住分集（所属的剧在回收站中返回 errs.ErrNotFound）→ 把超过 StaleAfter 没有进度的
	// running 任务标为失败 → 已有排队或进行中的任务返回 ErrActive → sourceKey 为空时沿用上一个任务的源（没有则 ErrNoSource）
	// → 写任务并入队 River 任务 → 没有可播放视频的分集置为 transcoding。
	CreateJob(ctx context.Context, id uuid.UUID, episodeID int64, sourceKey string, force bool) (Job, error)
	GetJob(ctx context.Context, id uuid.UUID) (Job, error)
	ListJobs(ctx context.Context, q JobQuery) ([]Job, int64, error)

	// StartAttempt 开始一次尝试（running、尝试次数加一）；任务已结束或不存在返回 ErrJobGone。
	StartAttempt(ctx context.Context, id uuid.UUID) (Attempt, error)
	// ReportProgress 更新进度（0–99），同时作为心跳。
	ReportProgress(ctx context.Context, id uuid.UUID, progress int) error
	// NextKeyVer 返回该集下一个内容密钥版本。同一集同时只有一个任务在运行，读到的版本在提交前不会被占用。
	NextKeyVer(ctx context.Context, episodeID int64) (int32, error)
	// Skip 记为跳过（源文件没有变化）。
	Skip(ctx context.Context, id uuid.UUID, sourceHash string) error
	// Retry 记录本次尝试的错误，等待 River 重试。
	Retry(ctx context.Context, id uuid.UUID, sourceHash, msg string) error
	// Fail 记为最终失败；分集还没有可播放的视频时置为 failed。
	Fail(ctx context.Context, id uuid.UUID, sourceHash, msg string) error
	// Complete 在一个事务内写媒体对象与内容密钥、把分集切换到新产物、刷新「更新至 N 集」并记为成功；
	// 任务已不在 running（被标为失败或已删除）时返回 ErrJobGone。
	Complete(ctx context.Context, id uuid.UUID, out Output) error
}
