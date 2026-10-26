// Package events 是用户端埋点采集接口（docs/03 第 8.1 节，S4-1）。
// 注册在 /api/v1 分组上，自动走报文加密中间件；handler 只读写明文。
package events

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/analytics"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

const (
	headerDeviceID  = "X-Device-Id"
	eventsPerMinute = 600             // 每设备每分钟最多接受的事件数，超出静默丢弃
	rateWindow      = 2 * time.Minute // 分钟桶存活时间，略长于 1 分钟
)

// Service 是埋点采集服务（domain/analytics.Service）。
type Service interface {
	Collect(ctx context.Context, in dom.CollectInput) (int, error)
}

// Limiter 是按 key 的固定窗口限流器（cache.Counter）。
type Limiter interface {
	Add(ctx context.Context, key string, delta int64, window time.Duration) (int64, error)
}

// Handlers 聚合埋点采集接口。
type Handlers struct {
	Svc     Service
	Limiter Limiter
}

// Routes 注册埋点采集接口到 /api/v1 分组。
func (h *Handlers) Routes(v1 gin.IRouter) {
	v1.POST("/events", h.Collect)
}

type eventItem struct {
	Type      string         `json:"type"`
	DramaID   *int64         `json:"drama_id"`
	EpisodeID *int64         `json:"episode_id"`
	Props     map[string]any `json:"props"`
}

type collectBody struct {
	Client string      `json:"client"`
	Events []eventItem `json:"events"`
}

// Collect 是 POST /api/v1/events：批量上报埋点，服务端校验后入库，尽快返回 204。
// 超过每设备每分钟配额时静默丢弃（仍返回 204，不打扰客户端）。
func (h *Handlers) Collect(c *gin.Context) {
	var body collectBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	if len(body.Events) == 0 {
		c.Status(http.StatusNoContent)
		return
	}
	if len(body.Events) > dom.MaxBatch {
		body.Events = body.Events[:dom.MaxBatch]
	}
	if !h.allow(c, len(body.Events)) {
		c.Status(http.StatusNoContent)
		return
	}
	items := make([]dom.EventItem, len(body.Events))
	for i, e := range body.Events {
		items[i] = dom.EventItem{Type: e.Type, DramaID: e.DramaID, EpisodeID: e.EpisodeID, Props: e.Props}
	}
	if _, err := h.Svc.Collect(c.Request.Context(), dom.CollectInput{
		Client:   body.Client,
		Events:   items,
		DeviceID: deviceID(c),
		// UserID（身份中间件）与 Region / ISP（GeoIP）在后续切片接入
	}); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// allow 按「每设备每分钟」固定窗口限流；限流器故障时放行（采集尽量不丢）。
func (h *Handlers) allow(c *gin.Context, n int) bool {
	device := deviceID(c)
	if device == "" {
		device = c.ClientIP()
	}
	minute := time.Now().Unix() / 60
	key := device + ":" + strconv.FormatInt(minute, 10)
	total, err := h.Limiter.Add(c.Request.Context(), key, int64(n), rateWindow)
	if err != nil {
		return true
	}
	return total <= eventsPerMinute
}

func deviceID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader(headerDeviceID))
}
