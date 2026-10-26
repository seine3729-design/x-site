package storage

import (
	"math"
	"math/rand/v2"
	"sort"
)

// 选线算法（docs/12 第 4.3 节，S1-6d）：给定客户端地区与候选线路，按「匹配精度 + 加分 + 优先级 + 健康分 − 观察惩罚」打分排序。
// 本文件是纯逻辑（不依赖 GeoIP / DB / Valkey），供后台选线预览与用户端选源共用；真实地区由 GeoIP 解析（S1-6c），这里只接收解析结果。

// unknownCountry 是地区解析失败或客户端为内网地址时的国家占位（docs/12 第 4.2 节），只匹配 global 线路。
const unknownCountry = "XX"

// Geo 是客户端地区（GeoIP 解析结果或后台预览直接输入）。未知字段留空；Country 用 ISO alpha-2，未知为 "XX"。
type Geo struct {
	Country  string
	Province string
	ISP      string
	ASN      string
}

// 匹配精度分（docs/12 第 3.2 节：asn > country+province+isp > country+isp > country+province > country > global）。
const (
	precASN    = 60
	precCPI    = 50
	precCI     = 40
	precCP     = 30
	precC      = 20
	precStar   = 10 // include 规则全为 *（相当于本线路对所有人生效）
	precGlobal = 5  // region_scope=global 且无 include 规则命中时的兜底精度
)

// 观察中线路的扣分与健康分过滤阈值。
const (
	watchPenalty  = 50
	healthMinKeep = 30
)

// ScoreDetail 是一条线路的得分分解（预览展示用）。
type ScoreDetail struct {
	Precision int `json:"precision"`
	Boost     int `json:"boost"`
	Priority  int `json:"priority"` // priority × 10
	Health    int `json:"health"`   // 健康分 × 0.3（取整）
	Watch     int `json:"watch"`    // 观察中扣分（负贡献，正数表示扣了多少）
	Total     int `json:"total"`
}

// EvalStatus 是一条线路在选线中的评估结果。
type EvalStatus string

const (
	EvalCandidate EvalStatus = "candidate" // 入选候选
	EvalOverflow  EvalStatus = "overflow"  // 命中但超出候选数量
	EvalExcluded  EvalStatus = "excluded"  // 命中 exclude 规则被排除
	EvalUnmatched EvalStatus = "unmatched" // region_scope=rules 但没有规则命中
	EvalUnhealthy EvalStatus = "unhealthy" // 健康分过低被过滤
	EvalDisabled  EvalStatus = "disabled"  // 线路已停用
)

// LineEval 是一条线路的评估：是否入选、排名、得分明细、地区标签。
type LineEval struct {
	LineID      int64       `json:"line_id"`
	Name        string      `json:"name"`
	Status      EvalStatus  `json:"status"`
	Rank        int         `json:"rank"`        // 候选排名（1 起），非候选为 0
	Recommended bool        `json:"recommended"` // 排名第一
	RegionTag   string      `json:"region_tag"`  // 本地区 / 通用
	HealthScore int         `json:"health_score"`
	Detail      ScoreDetail `json:"detail"`
	// Health 是健康分的来源分解（探测分 / 客户端分 / 权重），只在后台预览填充，用户端不返回。
	Health *HealthBreakdown `json:"health,omitempty"`
}

// Evaluate 对一组同类线路（kind 已筛选）按客户端地区打分排序，返回每条线路的评估（含落选原因）。
// health 返回线路的健康分（0–100）；candidateCount 为取前几条（默认 3）。
// rng 为 nil 时同分按线路 ID 稳定排序（后台预览用，便于运营核对）；非 nil 时同分按 weight 加权随机打散
// （用户端选源用，rng 由「设备 ID + 当前 10 分钟片」播种，保证同设备短期内稳定、整体按权重分摊流量）。
func Evaluate(lines []Line, geo Geo, health func(lineID int64) int, candidateCount int, rng *rand.Rand) []LineEval {
	if candidateCount < 1 {
		candidateCount = 3
	}
	evals := make([]LineEval, 0, len(lines))
	scores := map[int64]int{}
	for _, l := range lines {
		e := LineEval{LineID: l.ID, Name: displayName(l), HealthScore: health(l.ID)}
		if l.Status == LineDisabled {
			e.Status = EvalDisabled
			evals = append(evals, e)
			continue
		}
		prec, matched, excluded, boost := matchGeo(l, geo)
		if excluded {
			e.Status = EvalExcluded
			e.Detail.Precision = prec
			evals = append(evals, e)
			continue
		}
		if !matched {
			e.Status = EvalUnmatched
			evals = append(evals, e)
			continue
		}
		hp := health(l.ID)
		d := ScoreDetail{Precision: prec, Boost: boost, Priority: int(l.Priority) * 10, Health: hp * 3 / 10}
		if l.Status == LineWatch {
			d.Watch = watchPenalty
		}
		d.Total = d.Precision + d.Boost + d.Priority + d.Health - d.Watch
		e.Detail = d
		e.RegionTag = regionTag(prec)
		if hp < healthMinKeep {
			e.Status = EvalUnhealthy
		} else {
			e.Status = EvalCandidate
		}
		scores[l.ID] = d.Total
		evals = append(evals, e)
	}

	// 候选：status=candidate；全部不健康时兜底保留健康分最高的一条
	candidates := indicesWithStatus(evals, EvalCandidate)
	if len(candidates) == 0 {
		if idx := bestUnhealthy(evals); idx >= 0 {
			evals[idx].Status = EvalCandidate
			candidates = []int{idx}
		}
	}
	// 同分打散：预览（rng=nil）按线路 ID 稳定；用户端按 weight 加权随机
	// （Efraimidis–Spirakis：key = u^(1/weight)，降序即无放回加权抽样的排列）。
	var tiebreak map[int64]float64
	if rng != nil {
		weightByID := make(map[int64]int32, len(lines))
		for _, l := range lines {
			weightByID[l.ID] = l.Weight
		}
		tiebreak = make(map[int64]float64, len(candidates))
		for _, idx := range candidates {
			w := float64(weightByID[evals[idx].LineID])
			if w < 1 {
				w = 1
			}
			tiebreak[evals[idx].LineID] = math.Pow(rng.Float64(), 1/w)
		}
	}
	sort.SliceStable(candidates, func(a, b int) bool {
		ia, ib := candidates[a], candidates[b]
		if scores[evals[ia].LineID] != scores[evals[ib].LineID] {
			return scores[evals[ia].LineID] > scores[evals[ib].LineID]
		}
		if tiebreak != nil {
			return tiebreak[evals[ia].LineID] > tiebreak[evals[ib].LineID]
		}
		return evals[ia].LineID < evals[ib].LineID
	})
	for rank, idx := range candidates {
		if rank < candidateCount {
			evals[idx].Rank = rank + 1
			evals[idx].Recommended = rank == 0
		} else {
			evals[idx].Status = EvalOverflow
		}
	}
	return evals
}

// matchGeo 计算线路对某地区的匹配：命中 exclude 则排除；否则返回命中的最高精度与最高 boost。
// region_scope=global 且无 include 命中时按兜底精度匹配。
func matchGeo(l Line, geo Geo) (precision int, matched, excluded bool, boost int) {
	bestPrec := -1
	for _, r := range l.Rules {
		if !ruleMatches(r, geo) {
			continue
		}
		if r.Effect == EffectExclude {
			return 0, false, true, 0
		}
		p := precisionOf(r)
		if p > bestPrec {
			bestPrec = p
		}
		if int(r.Boost) > boost {
			boost = int(r.Boost)
		}
	}
	if bestPrec >= 0 {
		return bestPrec, true, false, boost
	}
	if l.RegionScope == ScopeGlobal {
		return precGlobal, true, false, 0
	}
	return 0, false, false, 0
}

// ruleMatches 判断一条规则是否命中地区（* / 空表示不限）。
func ruleMatches(r Rule, geo Geo) bool {
	return (r.Country == "*" || r.Country == geo.Country) &&
		(r.Province == "*" || r.Province == geo.Province) &&
		(r.ISP == "*" || r.ISP == geo.ISP) &&
		(r.ASN == "" || r.ASN == geo.ASN)
}

// precisionOf 按规则的具体程度给精度分。
func precisionOf(r Rule) int {
	if r.ASN != "" {
		return precASN
	}
	c := r.Country != "*"
	p := r.Province != "*"
	i := r.ISP != "*"
	switch {
	case c && p && i:
		return precCPI
	case c && i:
		return precCI
	case c && p:
		return precCP
	case c:
		return precC
	default:
		return precStar
	}
}

func regionTag(prec int) string {
	if prec > precGlobal {
		return "本地区"
	}
	return "通用"
}

func displayName(l Line) string {
	return l.Name // 空名由前端显示为「线路 N」
}

func indicesWithStatus(evals []LineEval, s EvalStatus) []int {
	var out []int
	for i := range evals {
		if evals[i].Status == s {
			out = append(out, i)
		}
	}
	return out
}

// bestUnhealthy 返回健康分最高的「不健康」线路下标（兜底用），没有则 -1。
func bestUnhealthy(evals []LineEval) int {
	best, bestHP := -1, -1
	for i := range evals {
		if evals[i].Status == EvalUnhealthy && evals[i].HealthScore > bestHP {
			best, bestHP = i, evals[i].HealthScore
		}
	}
	return best
}
