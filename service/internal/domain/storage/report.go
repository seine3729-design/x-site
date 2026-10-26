package storage

import (
	"context"
	"strings"
	"time"
)

// 客户端质量上报（docs/12 第 6.2 节，S1-6d）：POST /api/v1/lines/report。
// 按「线路 × 国家 × 运营商」聚合到 Valkey 的分钟桶（15 分钟滑动窗口），供健康分计算（客户端上报占 60%）。
// 本阶段只做采集聚合；把这些数据融合进健康分（探测 40% + 上报 60%、按地区细分）是后续增量。

// ReportEvent 是上报的事件类型。
type ReportEvent string

const (
	EventProbe      ReportEvent = "probe"       // 测速
	EventFirstFrame ReportEvent = "first_frame" // 首帧
	EventStall      ReportEvent = "stall"       // 卡顿
	EventError      ReportEvent = "error"       // 失败（reason: timeout/http_4xx/http_5xx/cors/decode）
	EventSwitch     ReportEvent = "switch"      // 切换线路
)

// ReportItem 是一条质量上报。
type ReportItem struct {
	LineID   int64
	Kind     LineKind
	Event    ReportEvent
	Ms       int32  // first_frame / probe 的耗时
	Reason   string // error 的原因
	ToLineID int64  // switch 的目标线路
	Trigger  string // switch 的触发原因
}

// QualityDelta 是对一个分钟桶的聚合增量，也用于读回窗口内的累计量。
type QualityDelta struct {
	Samples       int64 `json:"samples"`
	FirstFrameSum int64 `json:"first_frame_sum"`
	FirstFrameN   int64 `json:"first_frame_n"`
	Errors        int64 `json:"errors"`
	Stalls        int64 `json:"stalls"`
}

// IsZero 表示没有任何可聚合的量。
func (d QualityDelta) IsZero() bool {
	return d.Samples == 0 && d.FirstFrameSum == 0 && d.FirstFrameN == 0 && d.Errors == 0 && d.Stalls == 0
}

// QualityStore 读写质量上报的聚合（Valkey 实现在 store/cache）。
// 写入时除地区桶外还会写一份地区无关的汇总桶（country 与 isp 均为 QualityAllRegions），
// 供健康看板展示与地区样本不足时回退。
type QualityStore interface {
	AddQuality(ctx context.Context, lineID int64, country, isp string, minute int64, d QualityDelta) error
	// SumQuality 累加若干线路在指定地区、指定分钟桶上的质量数据；没有数据的线路不出现在结果里。
	SumQuality(ctx context.Context, lineIDs []int64, country, isp string, minutes []int64) (map[int64]QualityDelta, error)
}

// QualityAllRegions 是汇总桶的地区占位（不是通配查询，而是一个额外写入的「全部地区」桶）。
const QualityAllRegions = "*"

// nopQualityStore 是空实现：写无副作用、读总是无数据。未装配 Valkey 时用它，健康分只按服务端探测计算。
type nopQualityStore struct{}

func (nopQualityStore) AddQuality(context.Context, int64, string, string, int64, QualityDelta) error {
	return nil
}
func (nopQualityStore) SumQuality(context.Context, []int64, string, string, []int64) (map[int64]QualityDelta, error) {
	return map[int64]QualityDelta{}, nil
}

// NopQualityStore 是质量上报的空存储，总返回无数据。
var NopQualityStore QualityStore = nopQualityStore{}

// ReportInput 是一次上报。IP 非空时经 GeoResolver 解析地区，否则用 Geo。
type ReportInput struct {
	IP    string
	Geo   Geo
	Items []ReportItem
}

// ReportService 处理客户端质量上报。
type ReportService struct {
	geo   GeoResolver
	store QualityStore
}

func NewReportService(geo GeoResolver, store QualityStore) *ReportService {
	return &ReportService{geo: geo, store: store}
}

// Report 解析地区、按线路聚合本批上报，写入对应分钟桶。返回已接受的条数（忽略非法项）。
func (s *ReportService) Report(ctx context.Context, in ReportInput) (int, error) {
	geo := in.Geo
	if strings.TrimSpace(in.IP) != "" {
		geo = s.geo.Lookup(strings.TrimSpace(in.IP))
	}
	geo = normalizeGeo(geo)
	minute := time.Now().Unix() / 60

	// 同批里同线路合并，减少 Valkey 往返
	byLine := map[int64]*QualityDelta{}
	order := make([]int64, 0)
	accepted := 0
	for _, it := range in.Items {
		if it.LineID <= 0 || !validEvent(it.Event) {
			continue
		}
		d := byLine[it.LineID]
		if d == nil {
			d = &QualityDelta{}
			byLine[it.LineID] = d
			order = append(order, it.LineID)
		}
		d.Samples++
		switch it.Event {
		case EventFirstFrame:
			if it.Ms > 0 {
				d.FirstFrameSum += int64(it.Ms)
				d.FirstFrameN++
			}
		case EventError:
			d.Errors++
		case EventStall:
			d.Stalls++
		}
		accepted++
	}
	for _, lineID := range order {
		if err := s.store.AddQuality(ctx, lineID, geo.Country, geo.ISP, minute, *byLine[lineID]); err != nil {
			return accepted, err
		}
	}
	return accepted, nil
}

func validEvent(e ReportEvent) bool {
	switch e {
	case EventProbe, EventFirstFrame, EventStall, EventError, EventSwitch:
		return true
	}
	return false
}
