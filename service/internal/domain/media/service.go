package media

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"uuid"

	"xsite/service/internal/errs"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxSourceKeyLen = 1024 // S3 对象键上限
)

var (
	errBadSourceKey = errs.ErrBadRequest.WithMessage("源文件路径不合法：应为对象存储中的相对路径，不能以 / 开头，不能含 .. 、反斜杠或控制字符")
	errBadStatus    = errs.ErrBadRequest.WithMessage("status 只能是 queued、running、succeeded、skipped、failed")
	errBadPage      = errs.ErrBadRequest.WithMessage(fmt.Sprintf("page 从 1 开始，page_size 为 1–%d", maxPageSize))
)

// Service 是后台使用的转封装服务：提交任务与查询进度。
type Service struct {
	repo Repo
}

func NewService(repo Repo) *Service { return &Service{repo: repo} }

// Enqueue 为一集提交转封装任务。
func (s *Service) Enqueue(ctx context.Context, episodeID int64, in EnqueueInput) (Job, error) {
	if in.SourceKey != "" {
		if err := ValidateSourceKey(in.SourceKey); err != nil {
			return Job{}, err
		}
	}
	return s.repo.CreateJob(ctx, uuid.NewV7(), episodeID, in.SourceKey, in.Force)
}

// GetJob 读取一个任务；编号格式不对视为不存在。
func (s *Service) GetJob(ctx context.Context, id string) (Job, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return Job{}, errs.ErrNotFound
	}
	return s.repo.GetJob(ctx, u)
}

// ListJobs 按创建时间倒序列出任务（页码分页）。
func (s *Service) ListJobs(ctx context.Context, f JobFilter) (JobPage, error) {
	switch f.Status {
	case "", JobQueued, JobRunning, JobSucceeded, JobSkipped, JobFailed:
	default:
		return JobPage{}, errBadStatus
	}
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = defaultPageSize
	}
	if f.Page < 1 || f.PageSize < 1 || f.PageSize > maxPageSize {
		return JobPage{}, errBadPage
	}
	list, total, err := s.repo.ListJobs(ctx, JobQuery{
		EpisodeID: f.EpisodeID, DramaID: f.DramaID, Status: f.Status, Limit: f.PageSize, Offset: (f.Page - 1) * f.PageSize,
	})
	if err != nil {
		return JobPage{}, err
	}
	return JobPage{List: list, Total: total, Page: f.Page, PageSize: f.PageSize}, nil
}

// ValidateSourceKey 校验源文件路径：对象存储中的相对路径（1–1024 字节、合法 UTF-8），不以 / 开头，
// 没有 . / .. 路径段、空路径段、反斜杠与控制字符。
func ValidateSourceKey(key string) error {
	if key == "" || len(key) > maxSourceKeyLen || !utf8.ValidString(key) ||
		strings.HasPrefix(key, "/") || strings.HasSuffix(key, "/") || strings.Contains(key, `\`) {
		return errBadSourceKey
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return errBadSourceKey
		}
	}
	for seg := range strings.SplitSeq(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return errBadSourceKey
		}
	}
	return nil
}
