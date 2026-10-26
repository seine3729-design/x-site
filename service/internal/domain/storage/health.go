package storage

import (
	"context"
	"time"
)

// 线路健康分（docs/12 第 4.3 节，S1-6d）：由服务端探测（40%）与客户端质量上报（60%）加权得出，
// 按「线路 × 国家 × 运营商」计算。探测分由 worker 周期写入 Valkey（线路级，不分地区）；
// 客户端分在读取时按最近 15 分钟的分钟桶实时聚合（地区级），两者在 ResolveHealth 中融合，不额外存一份融合结果。

const (
	// DefaultHealthScore 是既无探测数据也无上报数据时的健康分：按满分处理，不因无数据把线路判为不健康而过滤掉。
	DefaultHealthScore = 100

	// 探测瞬时分：可达且快为满分，越慢越低，最低到 healthSlowScore；不可达为 0。
	healthFastMs    = 300  // ≤ 300ms 记满分
	healthSlowMs    = 3000 // ≥ 3000ms 记 healthSlowScore
	healthSlowScore = 50   // 可达但很慢时的分数

	// 探测与客户端上报在健康分中的权重（docs/12 第 4.3 节：探测 40%、上报 60%）。
	probeWeight  = 40
	clientWeight = 60

	// QualityWindowMinutes 是客户端上报参与健康分的滑动窗口（分钟）。
	QualityWindowMinutes = 15

	// minQualitySamples 是客户端分生效所需的最少样本数：样本太少不足以代表线路质量，这时只用探测分。
	minQualitySamples = 5

	// 客户端失败率分：无失败为满分，失败率达到 failRateBadPct 记 0 分，中间线性。
	failRateBadPct = 20

	// 客户端首帧分：≤ ffGoodMs 记满分，≥ ffBadMs 记 ffBadScore，中间线性（预算见 docs/08 第 1 节：首帧 P50 ≤ 1s）。
	ffGoodMs   = 1000
	ffBadMs    = 4000
	ffBadScore = 40

	// 客户端分内部：失败率与首帧的权重（失败比慢更严重）。
	failRateWeight   = 60
	firstFrameWeight = 40
)

// LineHealth 是一条线路的健康记录（存 Valkey、也是看板返回的明细）。Score 是生效健康分（0–100），
// 其余字段是最近一次服务端探测的结果，供运营在健康看板上核对。
type LineHealth struct {
	LineID      int64     `json:"line_id"`
	Score       int       `json:"score"`
	ProbeOK     bool      `json:"probe_ok"`
	HTTPStatus  int       `json:"http_status"`
	ElapsedMs   int64     `json:"elapsed_ms"`
	ContentType string    `json:"content_type"`
	CORS        bool      `json:"cors"`
	Message     string    `json:"message"`
	CheckedAt   time.Time `json:"checked_at"`
}

// HealthStore 读写线路健康分（Valkey 实现在 store/cache）。GetHealth 的第二个返回值表示是否有数据。
type HealthStore interface {
	SaveHealth(ctx context.Context, h LineHealth, ttl time.Duration) error
	GetHealth(ctx context.Context, lineID int64) (LineHealth, bool, error)
	ListHealth(ctx context.Context, lineIDs []int64) (map[int64]LineHealth, error)
}

// LineLister 是健康探测与看板需要的线路查询（Repo 的子集）。
type LineLister interface {
	ListLines(ctx context.Context, q LineQuery) ([]Line, error)
}

// nopHealthStore 是空实现：读总是无数据、写无副作用。未装配 Valkey（如部分测试）时用它，选线按默认满分。
type nopHealthStore struct{}

func (nopHealthStore) SaveHealth(context.Context, LineHealth, time.Duration) error { return nil }
func (nopHealthStore) GetHealth(context.Context, int64) (LineHealth, bool, error) {
	return LineHealth{}, false, nil
}
func (nopHealthStore) ListHealth(context.Context, []int64) (map[int64]LineHealth, error) {
	return map[int64]LineHealth{}, nil
}

// NopHealthStore 是健康分的空存储，总返回无数据。
var NopHealthStore HealthStore = nopHealthStore{}

// ScoreFromProbe 把一次探测结果换算成瞬时健康分（0–100）：不可达为 0，可达按耗时在 100–50 之间线性取值。
func ScoreFromProbe(r LineProbeResult) int {
	if !r.OK {
		return 0
	}
	switch {
	case r.ElapsedMs <= healthFastMs:
		return 100
	case r.ElapsedMs >= healthSlowMs:
		return healthSlowScore
	default:
		drop := (r.ElapsedMs - healthFastMs) * int64(100-healthSlowScore) / (healthSlowMs - healthFastMs)
		return 100 - int(drop)
	}
}

// NewHealthFromProbe 由一次探测结果与上一轮健康记录计算新的健康记录。分数用指数移动平均（α=0.5）平滑：
// 偶发失败只小幅下降、连续失败才趋近 0，避免单次抖动把线路挤出候选；首次无历史时直接用瞬时分。
func NewHealthFromProbe(lineID int64, r LineProbeResult, prev LineHealth, hasPrev bool, now time.Time) LineHealth {
	score := ScoreFromProbe(r)
	if hasPrev {
		score = (score + prev.Score) / 2
	}
	return LineHealth{
		LineID: lineID, Score: score, ProbeOK: r.OK, HTTPStatus: r.HTTPStatus,
		ElapsedMs: r.ElapsedMs, ContentType: r.ContentType, CORS: r.CORS, Message: r.Message, CheckedAt: now,
	}
}

// ---------- 客户端上报分与融合 ----------

// QualityScore 把一个窗口内的客户端上报换算成客户端分（0–100）。
// 样本数不足 minQualitySamples 时返回 ok=false（不参与健康分，避免个别设备的异常左右全局）。
// 分数 = 失败率分 × 60% + 首帧分 × 40%；没有首帧样本时只用失败率分。
func QualityScore(d QualityDelta) (int, bool) {
	if d.Samples < minQualitySamples {
		return 0, false
	}
	// 失败率分：0% → 100，≥ failRateBadPct → 0
	failPct := d.Errors * 100 / d.Samples
	failScore := 0
	if failPct < failRateBadPct {
		failScore = 100 - int(failPct)*100/failRateBadPct
	}
	if d.FirstFrameN == 0 {
		return failScore, true
	}
	avgMs := d.FirstFrameSum / d.FirstFrameN
	return (failScore*failRateWeight + firstFrameScore(avgMs)*firstFrameWeight) / 100, true
}

// firstFrameScore 把平均首帧耗时换算成分数：≤ ffGoodMs 满分，≥ ffBadMs 记 ffBadScore，中间线性。
func firstFrameScore(avgMs int64) int {
	switch {
	case avgMs <= ffGoodMs:
		return 100
	case avgMs >= ffBadMs:
		return ffBadScore
	default:
		drop := (avgMs - ffGoodMs) * int64(100-ffBadScore) / (ffBadMs - ffGoodMs)
		return 100 - int(drop)
	}
}

// HealthBreakdown 是一条线路健康分的来源分解，既供选线取 Final，也供健康看板展示明细。
type HealthBreakdown struct {
	Final       int          `json:"final"`        // 生效健康分（0–100）
	ProbeScore  int          `json:"probe_score"`  // 服务端探测分
	HasProbe    bool         `json:"has_probe"`    // 是否有探测数据
	ClientScore int          `json:"client_score"` // 客户端上报分
	HasClient   bool         `json:"has_client"`   // 客户端样本是否足够、已参与融合
	Quality     QualityDelta `json:"quality"`      // 窗口内的上报聚合（样本不足时也返回，供看板展示）
	ByRegion    bool         `json:"by_region"`    // 客户端数据取自本地区（false 表示回退到全部地区的汇总）
}

// blend 按 docs/12 第 4.3 节的权重融合探测分与客户端分：两者都有时 40%/60%，只有一边时用那一边，
// 都没有时按缺省满分（不因无数据把线路判为不健康）。
func blend(probe int, hasProbe bool, client int, hasClient bool) int {
	switch {
	case hasProbe && hasClient:
		return (probe*probeWeight + client*clientWeight) / 100
	case hasProbe:
		return probe
	case hasClient:
		return client
	default:
		return DefaultHealthScore
	}
}

// QualityWindow 返回覆盖最近 QualityWindowMinutes 分钟的分钟桶编号（含当前分钟）。
func QualityWindow(now time.Time) []int64 {
	cur := now.Unix() / 60
	out := make([]int64, 0, QualityWindowMinutes)
	for i := int64(QualityWindowMinutes - 1); i >= 0; i-- {
		out = append(out, cur-i)
	}
	return out
}

// ResolveHealth 批量解析一组线路在给定地区的健康分：读探测分（线路级）+ 聚合客户端上报（地区级），
// 按权重融合。本地区样本不足时回退到全部地区的汇总桶，仍不足则只用探测分。
// 读取失败不阻断选线：对应部分按「无数据」处理（健康分退化为另一边或缺省满分）。
func ResolveHealth(
	ctx context.Context, health HealthStore, quality QualityStore, lineIDs []int64, geo Geo, now time.Time,
) map[int64]HealthBreakdown {
	out := make(map[int64]HealthBreakdown, len(lineIDs))
	if len(lineIDs) == 0 {
		return out
	}
	probes, err := health.ListHealth(ctx, lineIDs)
	if err != nil {
		probes = map[int64]LineHealth{}
	}
	minutes := QualityWindow(now)

	// 先按本地区读；地区未知（XX / 空运营商）时直接用汇总桶，省一次往返
	byRegion := geo.Country != "" && geo.Country != unknownCountry && geo.ISP != ""
	regional := map[int64]QualityDelta{}
	if byRegion {
		if regional, err = quality.SumQuality(ctx, lineIDs, geo.Country, geo.ISP, minutes); err != nil {
			regional = map[int64]QualityDelta{}
		}
	}
	// 本地区样本不足的线路回退到汇总桶
	var fallbackIDs []int64
	for _, id := range lineIDs {
		if _, ok := QualityScore(regional[id]); !ok {
			fallbackIDs = append(fallbackIDs, id)
		}
	}
	global := map[int64]QualityDelta{}
	if len(fallbackIDs) > 0 {
		if global, err = quality.SumQuality(ctx, fallbackIDs, QualityAllRegions, QualityAllRegions, minutes); err != nil {
			global = map[int64]QualityDelta{}
		}
	}

	for _, id := range lineIDs {
		var b HealthBreakdown
		if h, ok := probes[id]; ok {
			b.ProbeScore, b.HasProbe = h.Score, true
		}
		// 优先用本地区数据，样本不足再看汇总
		if score, ok := QualityScore(regional[id]); ok {
			b.ClientScore, b.HasClient, b.Quality, b.ByRegion = score, true, regional[id], true
		} else if score, ok := QualityScore(global[id]); ok {
			b.ClientScore, b.HasClient, b.Quality = score, true, global[id]
		} else if d, exists := regional[id]; exists {
			b.Quality = d // 样本不足，只展示不计分
		} else {
			b.Quality = global[id]
		}
		b.Final = blend(b.ProbeScore, b.HasProbe, b.ClientScore, b.HasClient)
		out[id] = b
	}
	return out
}

// healthFunc 把解析结果包成 Evaluate 需要的取分函数；缺失的线路按缺省满分。
func healthFunc(bd map[int64]HealthBreakdown) func(int64) int {
	return func(lineID int64) int {
		if b, ok := bd[lineID]; ok {
			return b.Final
		}
		return DefaultHealthScore
	}
}

// ---------- 健康看板（docs/12 第 8.3 节）----------

// ClientQualityView 是看板上一条线路最近 15 分钟的客户端上报指标（全部地区汇总）。
type ClientQualityView struct {
	Samples      int64 `json:"samples"`        // 上报条数
	FirstFrameMs int64 `json:"first_frame_ms"` // 平均首帧耗时；无首帧样本时为 0
	FailRatePct  int   `json:"fail_rate_pct"`  // 失败率（百分比，取整）
	Stalls       int64 `json:"stalls"`         // 卡顿次数
	Score        int   `json:"score"`          // 客户端分
	Counted      bool  `json:"counted"`        // 样本是否足够、已参与健康分
}

// HealthLineView 是看板上一条线路的健康视图。Score 是生效健康分
// （探测分 × 40% + 客户端分 × 60%，缺一边时用另一边，都没有时为 DefaultHealthScore）。
type HealthLineView struct {
	LineID     int64      `json:"line_id"`
	Name       string     `json:"name"`
	ProviderID int64      `json:"provider_id"`
	Kind       LineKind   `json:"kind"`
	Status     LineStatus `json:"status"`
	BaseURL    string     `json:"base_url"`
	Score      int        `json:"score"`
	HasData    bool       `json:"has_data"` // 是否有任一来源的数据（探测或客户端上报）
	// 服务端探测（最近一次）
	ProbeScore  int        `json:"probe_score"`
	HasProbe    bool       `json:"has_probe"`
	ProbeOK     bool       `json:"probe_ok"`
	HTTPStatus  int        `json:"http_status"`
	ElapsedMs   int64      `json:"elapsed_ms"`
	ContentType string     `json:"content_type"`
	CORS        bool       `json:"cors"`
	Message     string     `json:"message"`
	CheckedAt   *time.Time `json:"checked_at"`
	// 客户端上报（最近 15 分钟，全部地区汇总）
	Client ClientQualityView `json:"client"`
}

// HealthOverview 是健康看板总览。ProbeEnabled 为 false 时看板提示探测已关闭。
type HealthOverview struct {
	ProbeEnabled bool             `json:"probe_enabled"`
	WindowMin    int              `json:"window_min"`    // 客户端上报的统计窗口（分钟）
	ProbeWeight  int              `json:"probe_weight"`  // 探测在健康分中的权重（百分比）
	ClientWeight int              `json:"client_weight"` // 客户端上报的权重（百分比）
	Lines        []HealthLineView `json:"lines"`
}

// HealthService 组装线路健康看板（docs/12 第 8.3 节）。
type HealthService struct {
	lines        LineLister
	store        HealthStore
	quality      QualityStore
	probeEnabled bool
}

func NewHealthService(lines LineLister, store HealthStore, quality QualityStore, probeEnabled bool) *HealthService {
	return &HealthService{lines: lines, store: store, quality: quality, probeEnabled: probeEnabled}
}

// Overview 返回全部线路的健康视图（按仓储的默认排序：存储源、优先级倒序）。
// 客户端指标取全部地区的汇总桶（看板是总览；按地区下钻见 docs/12 第 8.3 节的热力图，待前端接入）。
func (s *HealthService) Overview(ctx context.Context) (HealthOverview, error) {
	lines, err := s.lines.ListLines(ctx, LineQuery{})
	if err != nil {
		return HealthOverview{}, err
	}
	ids := make([]int64, len(lines))
	for i := range lines {
		ids[i] = lines[i].ID
	}
	healths, err := s.store.ListHealth(ctx, ids)
	if err != nil {
		return HealthOverview{}, err
	}
	// 看板看全局：地区留空让 ResolveHealth 直接走汇总桶
	bd := ResolveHealth(ctx, s.store, s.quality, ids, Geo{Country: unknownCountry}, time.Now())

	views := make([]HealthLineView, len(lines))
	for i, l := range lines {
		b := bd[l.ID]
		v := HealthLineView{
			LineID: l.ID, Name: displayName(l), ProviderID: l.ProviderID, Kind: l.Kind,
			Status: l.Status, BaseURL: l.BaseURL,
			Score: b.Final, HasData: b.HasProbe || b.HasClient,
			ProbeScore: b.ProbeScore, HasProbe: b.HasProbe,
			Client: clientViewOf(b),
		}
		if h, ok := healths[l.ID]; ok {
			t := h.CheckedAt
			v.ProbeOK = h.ProbeOK
			v.HTTPStatus = h.HTTPStatus
			v.ElapsedMs = h.ElapsedMs
			v.ContentType = h.ContentType
			v.CORS = h.CORS
			v.Message = h.Message
			v.CheckedAt = &t
		}
		views[i] = v
	}
	return HealthOverview{
		ProbeEnabled: s.probeEnabled, WindowMin: QualityWindowMinutes,
		ProbeWeight: probeWeight, ClientWeight: clientWeight, Lines: views,
	}, nil
}

func clientViewOf(b HealthBreakdown) ClientQualityView {
	v := ClientQualityView{
		Samples: b.Quality.Samples, Stalls: b.Quality.Stalls,
		Score: b.ClientScore, Counted: b.HasClient,
	}
	if b.Quality.Samples > 0 {
		v.FailRatePct = int(b.Quality.Errors * 100 / b.Quality.Samples)
	}
	if b.Quality.FirstFrameN > 0 {
		v.FirstFrameMs = b.Quality.FirstFrameSum / b.Quality.FirstFrameN
	}
	return v
}
