// Package media 是运营后台的转封装接口（S1-5，docs/specs/S1-5.md）：提交分集的转封装任务、查询任务进度。
// 分集属于剧集页面：读要 dramas.view、提交要 dramas.edit。会话、CSRF、审计由 admin.Routes 返回的分组提供。
package media

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/media"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// Service 是转封装服务（domain/media.Service）。
type Service interface {
	Enqueue(ctx context.Context, episodeID int64, in dom.EnqueueInput) (dom.Job, error)
	GetJob(ctx context.Context, id string) (dom.Job, error)
	ListJobs(ctx context.Context, f dom.JobFilter) (dom.JobPage, error)
}

// Handlers 聚合转封装与后台试播接口。
type Handlers struct {
	Svc Service
	// PreviewSvc 与 PreviewHLS 为 nil 时试播接口返回 503（装配失败不影响其余后台功能）。
	PreviewSvc PreviewService
	PreviewHLS PreviewHLS
}

// Routes 注册到 admin.Routes 返回的分组上。
func (h *Handlers) Routes(authed gin.IRouter) {
	episodes := authed.Group("/episodes", admin.RequirePage("dramas"))
	episodes.POST("/:id/transcode", h.Enqueue)
	episodes.POST("/:id/preview", h.Preview)
	jobs := authed.Group("/transcode-jobs", admin.RequirePage("dramas"))
	jobs.GET("", h.ListJobs)
	jobs.GET("/:id", h.GetJob)
	// 后台试播的清单与密钥：授权来自后台会话 + dramas 权限点，再叠一层预览令牌（限定分集与线路）。
	preview := authed.Group("/preview", admin.RequirePage("dramas"))
	preview.GET("/:token/:file", h.PreviewPlaylist)
	preview.GET("/key/:episode_id/:key_ver", h.PreviewKey)
}

type renditionDTO struct {
	Name        string `json:"name"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Bitrate     int64  `json:"bitrate"`
	PeakBitrate int64  `json:"peak_bitrate"`
	Codecs      string `json:"codecs"`
}

type jobDTO struct {
	ID            string         `json:"id"`
	EpisodeID     int64          `json:"episode_id"`
	DramaID       int64          `json:"drama_id"`
	EpisodeNumber int32          `json:"episode_number"`
	SourceKey     string         `json:"source_key"`
	Force         bool           `json:"force"`
	Status        string         `json:"status"`
	Progress      int32          `json:"progress"`
	Attempts      int32          `json:"attempts"`
	Error         string         `json:"error"`
	SourceHash    string         `json:"source_hash"`
	KeyVer        int32          `json:"key_ver"`
	Renditions    []renditionDTO `json:"renditions"`
	Poster        string         `json:"poster"`
	DurationSec   int32          `json:"duration_sec"`
	CreatedAt     time.Time      `json:"created_at"`
	StartedAt     *time.Time     `json:"started_at"`
	FinishedAt    *time.Time     `json:"finished_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

func jobOf(j dom.Job) jobDTO {
	rs := make([]renditionDTO, len(j.Renditions))
	for i, r := range j.Renditions {
		rs[i] = renditionDTO(r)
	}
	return jobDTO{
		ID: j.ID.String(), EpisodeID: j.EpisodeID, DramaID: j.DramaID, EpisodeNumber: j.EpisodeNumber, SourceKey: j.SourceKey,
		Force: j.Force, Status: string(j.Status), Progress: j.Progress, Attempts: j.Attempts, Error: j.Error,
		SourceHash: j.SourceHash, KeyVer: j.KeyVer, Renditions: rs, Poster: j.Poster, DurationSec: j.DurationSec,
		CreatedAt: j.CreatedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt, UpdatedAt: j.UpdatedAt,
	}
}

type enqueueRequest struct {
	SourceKey string `json:"source_key"`
	Force     bool   `json:"force"`
}

// Enqueue 是 POST /api/admin/episodes/:id/transcode：提交转封装任务；source_key 留空时沿用该集上一个任务的源文件。
func (h *Handlers) Enqueue(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req enqueueRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	j, err := h.Svc.Enqueue(c.Request.Context(), id, dom.EnqueueInput{SourceKey: req.SourceKey, Force: req.Force})
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := jobOf(j)
	admin.SetAudit(c, admin.AuditDetail{Action: "episode.transcode", TargetType: "episode", TargetID: strconv.FormatInt(id, 10),
		After: gin.H{"job_id": out.ID, "source_key": out.SourceKey, "force": out.Force}})
	respond.OK(c, out)
}

type listQuery struct {
	EpisodeID int64  `form:"episode_id"`
	DramaID   int64  `form:"drama_id"`
	Status    string `form:"status"`
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
}

type pageDTO struct {
	List     []jobDTO `json:"list"`
	Total    int64    `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
}

// ListJobs 是 GET /api/admin/transcode-jobs：按创建时间倒序，页码分页。
func (h *Handlers) ListJobs(c *gin.Context) {
	var q listQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	page, err := h.Svc.ListJobs(c.Request.Context(), dom.JobFilter{
		EpisodeID: q.EpisodeID, DramaID: q.DramaID, Status: dom.JobStatus(q.Status), Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	list := make([]jobDTO, len(page.List))
	for i, j := range page.List {
		list[i] = jobOf(j)
	}
	respond.OK(c, pageDTO{List: list, Total: page.Total, Page: page.Page, PageSize: page.PageSize})
}

// GetJob 是 GET /api/admin/transcode-jobs/:id：单个任务（轮询进度）。
func (h *Handlers) GetJob(c *gin.Context) {
	j, err := h.Svc.GetJob(c.Request.Context(), c.Param("id"))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, jobOf(j))
}
