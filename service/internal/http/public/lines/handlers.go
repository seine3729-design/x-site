// Package lines 是用户端选线与质量上报接口（docs/12 第 4.4、6.2 节，S1-6d）。
// 注册在 /api/v1 分组上，因此自动走报文加密中间件；handler 只读写明文。
package lines

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

const (
	headerDeviceID = "X-Device-Id"
	headerDebugGeo = "X-Debug-Geo" // 仅 dev：模拟地区，形如 CN/广东/电信

	reportPerMinute = 30              // 每设备每分钟最多 30 条（docs/12 第 6.2 节），超出丢弃
	maxReportItems  = 100             // 单次请求最多处理的上报条数
	reportWindow    = 2 * time.Minute // 分钟桶 key 的存活时间（略长于 1 分钟）
)

// Service 是用户端选线服务（domain/storage.LinesService）。
type Service interface {
	List(ctx context.Context, in dom.SelectInput) (dom.SelectResult, error)
}

// QualityReporter 是客户端质量上报服务（domain/storage.ReportService）。
type QualityReporter interface {
	Report(ctx context.Context, in dom.ReportInput) (int, error)
}

// Limiter 是按 key 的固定窗口限流器（cache.Counter）。
type Limiter interface {
	Add(ctx context.Context, key string, delta int64, window time.Duration) (int64, error)
}

// Handlers 聚合用户端选线与质量上报接口。
type Handlers struct {
	Svc     Service
	Report  QualityReporter
	Limiter Limiter
	Dev     bool
}

// Routes 注册用户端选线接口到 /api/v1 分组。
func (h *Handlers) Routes(v1 gin.IRouter) {
	v1.GET("/lines", h.List)
	v1.POST("/lines/report", h.ReportQuality)
}

type listQuery struct {
	Kind string `form:"kind"`
}

// List 是 GET /api/v1/lines：按客户端地区返回可用线路列表与测速地址，用于线路菜单与测速。
func (h *Handlers) List(c *gin.Context) {
	var q listQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	ip, geo := h.resolveGeo(c)
	res, err := h.Svc.List(c.Request.Context(), dom.SelectInput{
		IP: ip, Geo: geo, Kind: dom.LineKind(q.Kind), DeviceID: deviceID(c),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, res)
}

type reportItem struct {
	LineID   int64  `json:"line_id"`
	Kind     string `json:"kind"`
	Event    string `json:"event"`
	Ms       int32  `json:"ms"`
	Reason   string `json:"reason"`
	ToLineID int64  `json:"to_line_id"`
	Trigger  string `json:"trigger"`
}

type reportBody struct {
	Items []reportItem `json:"items"`
}

// ReportQuality 是 POST /api/v1/lines/report：客户端批量上报线路质量，按设备限流后聚合。
// 超过每设备每分钟配额的部分静默丢弃（不报错），始终返回已接受条数。
func (h *Handlers) ReportQuality(c *gin.Context) {
	var body reportBody
	if err := c.ShouldBindJSON(&body); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	if len(body.Items) > maxReportItems {
		body.Items = body.Items[:maxReportItems]
	}
	accept := h.acceptQuota(c, len(body.Items))
	if accept <= 0 {
		respond.OK(c, gin.H{"accepted": 0})
		return
	}
	items := make([]dom.ReportItem, 0, accept)
	for _, it := range body.Items[:accept] {
		items = append(items, dom.ReportItem{
			LineID: it.LineID, Kind: dom.LineKind(it.Kind), Event: dom.ReportEvent(it.Event),
			Ms: it.Ms, Reason: it.Reason, ToLineID: it.ToLineID, Trigger: it.Trigger,
		})
	}
	ip, geo := h.resolveGeo(c)
	n, err := h.Report.Report(c.Request.Context(), dom.ReportInput{IP: ip, Geo: geo, Items: items})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"accepted": n})
}

// acceptQuota 按「每设备每分钟 30 条」的固定窗口返回本批可接受的条数；限流器故障时放行（采集尽量不丢）。
func (h *Handlers) acceptQuota(c *gin.Context, n int) int {
	if n <= 0 {
		return 0
	}
	device := deviceID(c)
	if device == "" {
		device = c.ClientIP()
	}
	minute := time.Now().Unix() / 60
	key := device + ":" + strconv.FormatInt(minute, 10)
	total, err := h.Limiter.Add(c.Request.Context(), key, int64(n), reportWindow)
	if err != nil {
		return n // 限流器不可用时不阻断采集
	}
	if total <= reportPerMinute {
		return n
	}
	accept := n - int(total-reportPerMinute)
	if accept < 0 {
		accept = 0
	}
	return accept
}

// resolveGeo 返回本次请求的地区来源：dev 环境优先用 X-Debug-Geo 模拟，否则用真实客户端 IP（交 GeoIP 解析）。
func (h *Handlers) resolveGeo(c *gin.Context) (ip string, geo dom.Geo) {
	if h.Dev {
		if dbg := strings.TrimSpace(c.GetHeader(headerDebugGeo)); dbg != "" {
			return "", parseDebugGeo(dbg)
		}
	}
	return c.ClientIP(), dom.Geo{}
}

func deviceID(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader(headerDeviceID))
}

// parseDebugGeo 解析调试地区头 CN/广东/电信（国家/省份/运营商，后两段可省略）。
func parseDebugGeo(s string) dom.Geo {
	parts := strings.Split(s, "/")
	var g dom.Geo
	if len(parts) > 0 {
		g.Country = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		g.Province = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		g.ISP = strings.TrimSpace(parts[2])
	}
	return g
}
