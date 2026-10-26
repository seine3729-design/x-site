package content

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/resourcewarm"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// ResourceWarmService 是资源预热（domain/resourcewarm.Service）。
type ResourceWarmService interface {
	Start(ctx context.Context, in dom.Input, adminID int64) (dom.Job, error)
	Stop(ctx context.Context, id int64) (dom.Job, error)
	Current(ctx context.Context) (dom.Progress, error)
	History(ctx context.Context) ([]dom.Job, error)
}

type warmStartRequest struct {
	// DramaID 为 0 表示全部剧。
	DramaID            int64  `json:"drama_id"`
	Mode               string `json:"mode"`
	SegmentsPerQuality int    `json:"segments_per_quality"`
	Concurrency        int    `json:"concurrency"`
	IncludeUnpublished bool   `json:"include_unpublished"`
}

// StartResourceWarm 是 POST /api/admin/resource-warm/start：开一轮预热。
//
// 同一时间仅一轮（docs/02 第 4.4 节）：已有在跑的一轮时返回 409。
func (h *Handlers) StartResourceWarm(c *gin.Context) {
	var req warmStartRequest
	if !admin.Bind(c, &req) {
		return
	}
	actor, _ := admin.CurrentAdmin(c)
	job, err := h.ResourceWarm.Start(c.Request.Context(), dom.Input{
		DramaID: req.DramaID, Mode: dom.Mode(req.Mode), SegmentsPerQuality: req.SegmentsPerQuality,
		Concurrency: req.Concurrency, IncludeUnpublished: req.IncludeUnpublished,
	}, actor.AdminID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "resource_warm.start", TargetType: "resource_warm",
		TargetID: strconv.FormatInt(job.ID, 10),
		After: gin.H{
			"drama_id": job.DramaID, "mode": job.Mode, "segments_per_quality": job.SegmentsPerQuality,
			"concurrency": job.Concurrency, "include_unpublished": job.IncludeUnpublished,
			"episodes_total": job.EpisodesTotal,
		},
	})
	respond.OK(c, job)
}

// StopResourceWarm 是 POST /api/admin/resource-warm/stop：停止在跑的那一轮。
//
// 只设信号，worker 处理完当前这一集就收尾 —— 已经热好的算数（docs/06 第 10 节）。
func (h *Handlers) StopResourceWarm(c *gin.Context) {
	var req struct {
		ID int64 `json:"id"`
	}
	if !admin.Bind(c, &req) {
		return
	}
	job, err := h.ResourceWarm.Stop(c.Request.Context(), req.ID)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "resource_warm.stop", TargetType: "resource_warm",
		TargetID: strconv.FormatInt(job.ID, 10),
		After:    gin.H{"episodes_done": job.EpisodesDone, "warmed": job.Warmed, "failed": job.Failed},
	})
	respond.OK(c, job)
}

// warmStreamInterval 是 SSE 推送间隔。
//
// 1 秒：预热是分钟级的任务，再快运营也看不出区别，而每帧都要查一次库。
const warmStreamInterval = time.Second

// warmStreamMax 是单条 SSE 连接的存活上限。
//
// 预热可能跑几小时，但不能让一条连接挂那么久（代理、负载均衡都会在中间掐掉，
// 还会攒下泄漏的 goroutine）。到点主动关闭，前端的 EventSource 会自动重连。
const warmStreamMax = 10 * time.Minute

// ResourceWarmProgress 是 GET /api/admin/resource-warm/progress：进度。
//
// 默认返回 JSON；带 ?stream=1 时返回 SSE（docs/07「支持 SSE」）。
// 两种都给：SSE 省掉轮询的请求数，但轮询在代理不透传 event-stream 的环境里更可靠，
// 前端可以降级。
func (h *Handlers) ResourceWarmProgress(c *gin.Context) {
	if c.Query("stream") == "1" {
		h.streamWarmProgress(c)
		return
	}
	out, err := h.ResourceWarm.Current(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, out)
}

// streamWarmProgress 用 SSE 推进度，任务结束后推最后一帧再关闭。
func (h *Handlers) streamWarmProgress(c *gin.Context) {
	ctx := c.Request.Context()
	// 先取一次：拿不到就按普通错误返回，别开着一条什么都不推的连接
	first, err := h.ResourceWarm.Current(ctx)
	if err != nil {
		respond.Error(c, err)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-store")
	c.Header("Connection", "keep-alive")
	// 关掉 Nginx 的响应缓冲，否则进度会攒着一起发
	c.Header("X-Accel-Buffering", "no")
	c.Status(200)

	flusher, ok := c.Writer.(interface{ Flush() })
	if !ok {
		respond.Error(c, fmt.Errorf("响应流不支持推送"))
		return
	}

	send := func(p dom.Progress) bool {
		b, err := json.Marshal(p)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(c.Writer, "event: progress\ndata: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send(first) {
		return
	}
	// 任务已经结束了：推完这一帧就收，没必要让前端挂着
	if !first.Running {
		return
	}

	tick := time.NewTicker(warmStreamInterval)
	defer tick.Stop()
	deadline := time.After(warmStreamMax)
	for {
		select {
		case <-ctx.Done():
			// 运营关了页面
			return
		case <-deadline:
			return
		case <-tick.C:
		}
		p, err := h.ResourceWarm.Current(ctx)
		if err != nil {
			// 读失败就结束这条流，前端重连后会拿到普通错误
			return
		}
		if !send(p) {
			return
		}
		if !p.Running {
			return
		}
	}
}

// ListResourceWarmHistory 是 GET /api/admin/resource-warm/history：最近几轮的记录。
func (h *Handlers) ListResourceWarmHistory(c *gin.Context) {
	list, err := h.ResourceWarm.History(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"list": list})
}
