// Package analytics 是埋点采集与看板统计（docs/03 第 8、11 节，docs/02 第 6.3 节，S4-1 起）。
// 本文件是采集侧：前台 / APP 经 POST /api/v1/events 批量上报原始事件，服务端校验后写入 event_raw 分区表。
// 汇总（metric_*）与看板 / 剧集分析接口在后续切片（S4-2 / S4-3）加入。
package analytics

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

// 采集约束。
const (
	// MaxBatch 是单次上报的事件条数上限（docs/03 第 8.1 节：最多 50 条/次）。
	MaxBatch = 50
	// MaxPropsBytes 是单个事件 props 序列化后的大小上限，超过则丢弃该条。
	MaxPropsBytes = 4096
	maxDeviceLen  = 128
	maxRegionLen  = 16
	maxISPLen     = 32
)

// 事件类型白名单（docs/03 第 8.1 节）。未在表内的类型按脏数据静默丢弃，不扩散到汇总。
// 用集合而非 CHECK 约束：埋点类型会随前台迭代增删，改一处白名单比改迁移轻。
var allowedTypes = map[string]bool{
	"visitor":       true, // 访客到访
	"page_view":     true, // 页面浏览
	"play_attempt":  true, // 播放尝试（props: first_frame_ms/result/line_id/cdn）
	"play_start":    true, // 起播成功（出画面）
	"play_error":    true, // 播放失败（props: reason 三分类）
	"play_stall":    true, // 卡顿（props: stall_ms）
	"line_switch":   true, // 换线
	"watch":         true, // 观看秒数（props: seconds，≥10s 才算有效，由汇总判定）
	"ad_impression": true, // 广告曝光
	"ad_click":      true, // 广告点击
	"register_wall": true, // 注册墙曝光 / 点击（props: step）
	"register":      true, // 注册成功
	"domain_switch": true, // 域名切换（props: from/to/reason，docs/09）
	"push_received": true, // APP 推送到达
	"push_clicked":  true, // APP 推送点击
}

func allowedType(t string) bool { return allowedTypes[t] }

// 客户端白名单；其它值归一为空。
var allowedClients = map[string]bool{"web": true, "app": true}

// EventItem 是上报的一条事件（客户端提供的部分）。
type EventItem struct {
	Type      string
	DramaID   *int64
	EpisodeID *int64
	Props     map[string]any
}

// CollectInput 是一次采集请求：客户端事件 + 服务端补充的上下文。
type CollectInput struct {
	Client string
	Events []EventItem
	// 以下由服务端从请求补充，客户端不可伪造。
	DeviceID string
	UserID   *int64
	Region   string
	ISP      string
}

// Event 是准备写库的一条事件（已归一、props 已序列化）。
type Event struct {
	OccurredAt time.Time
	Type       string
	DeviceID   string
	UserID     *int64
	DramaID    *int64
	EpisodeID  *int64
	Client     string
	Region     string
	ISP        string
	Props      []byte
}

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}

// truncate 把字符串按 rune 截断到 n 个字符，防止超长字段撑爆列。
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// marshalProps 序列化事件属性；为空或超限返回 nil（调用方决定是否丢弃）。
func marshalProps(p map[string]any) ([]byte, bool) {
	if len(p) == 0 {
		return []byte("{}"), true
	}
	b, err := json.Marshal(p)
	if err != nil || len(b) > MaxPropsBytes {
		return nil, false
	}
	return b, true
}
