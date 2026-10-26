package storage

import (
	"context"
	"encoding/binary"
	"hash/fnv"
	"math/rand/v2"
	"sort"
	"strings"
	"time"
)

// 用户端选线（docs/12 第 4.3、4.4 节，S1-6d）：GET /api/v1/lines。复用 Evaluate 的打分，
// 对同分候选按 weight 加权随机打散（种子 = 设备 ID + 当前 10 分钟片），返回该地区可用线路列表与测速地址。

// lineListTTL 是「地区 → 线路列表」的客户端缓存时长（秒，docs/12 第 4.3 节末）。
const lineListTTL = 30

// SelectInput 是用户端选线输入。IP 非空时经 GeoResolver 解析，否则用 Geo（dev 的 X-Debug-Geo 直接给定地区）。
type SelectInput struct {
	IP       string
	Geo      Geo
	Kind     LineKind
	DeviceID string // 加权随机种子来源；为空时退化为确定性排序
}

// SelectedLine 是返回给客户端的一条线路（不含对象地址 / 签名）。
type SelectedLine struct {
	LineID      int64  `json:"line_id"`
	Name        string `json:"name"`
	RegionTag   string `json:"region_tag"`
	Recommended bool   `json:"recommended"`
	ProbeURL    string `json:"probe_url"` // 测速地址（base_url + probe_path）；直连存储线路为空
}

// SelectResult 是 GET /api/v1/lines 的响应。
type SelectResult struct {
	Region string         `json:"region"` // 如 CN-广东-电信
	Lines  []SelectedLine `json:"lines"`
	TTL    int            `json:"ttl"`
}

// LinesService 是用户端选线服务。
type LinesService struct {
	repo    Repo
	geo     GeoResolver
	health  HealthStore
	quality QualityStore
}

func NewLinesService(repo Repo, geo GeoResolver, health HealthStore, quality QualityStore) *LinesService {
	return &LinesService{repo: repo, geo: geo, health: health, quality: quality}
}

// List 解析地区、取该类型全部线路、按选线算法打分（同分按设备加权随机），返回入选候选线路列表。
func (s *LinesService) List(ctx context.Context, in SelectInput) (SelectResult, error) {
	if in.Kind != LineVideo && in.Kind != LineImage {
		return SelectResult{}, badRequest("类型只能是 video 或 image")
	}
	geo := in.Geo
	if strings.TrimSpace(in.IP) != "" {
		geo = s.geo.Lookup(strings.TrimSpace(in.IP))
	}
	geo = normalizeGeo(geo)

	lines, err := s.repo.ListLines(ctx, LineQuery{Kind: in.Kind})
	if err != nil {
		return SelectResult{}, err
	}
	for i := range lines {
		rs, err := s.repo.ListRulesByLine(ctx, lines[i].ID)
		if err != nil {
			return SelectResult{}, err
		}
		lines[i].Rules = rs
	}

	cc := 3
	if st, err := s.repo.GetSettings(ctx); err == nil && st.CandidateCount > 0 {
		cc = int(st.CandidateCount)
	}
	// 健康分 = 服务端探测分 × 40% + 客户端上报分 × 60%，客户端部分按本地区最近 15 分钟的上报聚合
	byID := make(map[int64]Line, len(lines))
	ids := make([]int64, len(lines))
	for i, l := range lines {
		byID[l.ID] = l
		ids[i] = l.ID
	}
	now := time.Now()
	bd := ResolveHealth(ctx, s.health, s.quality, ids, geo, now)
	evals := Evaluate(lines, geo, healthFunc(bd), cc, weightedRNG(in.DeviceID, now))
	// 只取入选候选（rank>0），按 rank 升序输出
	cand := make([]LineEval, 0, cc)
	for _, e := range evals {
		if e.Rank > 0 {
			cand = append(cand, e)
		}
	}
	sort.Slice(cand, func(i, j int) bool { return cand[i].Rank < cand[j].Rank })
	out := make([]SelectedLine, 0, len(cand))
	for _, e := range cand {
		out = append(out, SelectedLine{
			LineID: e.LineID, Name: e.Name, RegionTag: e.RegionTag, Recommended: e.Recommended,
			ProbeURL: probeURL(byID[e.LineID]),
		})
	}
	return SelectResult{Region: formatRegion(geo), Lines: out, TTL: lineListTTL}, nil
}

// weightedRNG 由「设备 ID + 当前 10 分钟片」播种，使同设备短期内选线稳定；设备为空时返回 nil（确定性排序）。
func weightedRNG(deviceID string, now time.Time) *rand.Rand {
	if deviceID == "" {
		return nil
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(deviceID))
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(now.Unix()/600))
	_, _ = h.Write(b[:])
	seed := h.Sum64()
	return rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
}

// probeURL 拼线路的测速地址（base_url + probe_path）；无 base_url（直连存储）的线路返回空。
func probeURL(l Line) string {
	if l.BaseURL == "" {
		return ""
	}
	return strings.TrimRight(l.BaseURL, "/") + l.ProbePath
}
