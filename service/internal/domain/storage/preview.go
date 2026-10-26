package storage

import (
	"context"
	"strings"
	"time"
)

// GeoResolver 把客户端 IP 解析成地区（S1-6c）。真实实现是 internal/geoip（ip2region xdb），
// 由 app 装配时按 GEOIP_XDB_PATH 注入；没有配置数据文件时用 NopGeoResolver（总返回未知地区 XX），
// 这时只有全局兜底线路会命中，后台选线预览仍可直接输入「国家 + 省份 + 运营商」测试算法。
type GeoResolver interface {
	Lookup(ip string) Geo
}

type nopGeo struct{}

func (nopGeo) Lookup(string) Geo { return Geo{Country: unknownCountry} }

// NopGeoResolver 是占位解析器：总返回未知地区。没有 GeoIP 数据文件时使用。
var NopGeoResolver GeoResolver = nopGeo{}

// PreviewInput 是后台选线预览的输入：填了 IP 时经 GeoResolver 解析，否则用直接输入的 Geo。
type PreviewInput struct {
	IP   string
	Geo  Geo
	Kind LineKind
}

// PreviewResult 是选线预览结果：解析出的地区、候选数量与每条线路的评估。
type PreviewResult struct {
	Region         string     `json:"region"` // 如 CN-广东-电信
	Country        string     `json:"country"`
	Province       string     `json:"province"`
	ISP            string     `json:"isp"`
	ASN            string     `json:"asn"`
	CandidateCount int        `json:"candidate_count"`
	Lines          []LineEval `json:"lines"`
}

// PreviewService 是后台选线预览（docs/12 第 8.3 节）。健康分与用户端选线同源：
// 服务端探测分 + 客户端上报分按权重融合，因此预览结果与实际下发一致。
type PreviewService struct {
	repo    Repo
	geo     GeoResolver
	health  HealthStore
	quality QualityStore
}

func NewPreviewService(repo Repo, geo GeoResolver, health HealthStore, quality QualityStore) *PreviewService {
	return &PreviewService{repo: repo, geo: geo, health: health, quality: quality}
}

// Preview 解析地区、取该类型全部线路、按选线算法打分，返回地区与每条线路的评估（含落选原因）。
func (s *PreviewService) Preview(ctx context.Context, in PreviewInput) (PreviewResult, error) {
	if in.Kind != LineVideo && in.Kind != LineImage {
		return PreviewResult{}, badRequest("类型只能是 video 或 image")
	}
	geo := in.Geo
	if strings.TrimSpace(in.IP) != "" {
		geo = s.geo.Lookup(strings.TrimSpace(in.IP))
	}
	geo = normalizeGeo(geo)

	lines, err := s.repo.ListLines(ctx, LineQuery{Kind: in.Kind})
	if err != nil {
		return PreviewResult{}, err
	}
	for i := range lines {
		rs, err := s.repo.ListRulesByLine(ctx, lines[i].ID)
		if err != nil {
			return PreviewResult{}, err
		}
		lines[i].Rules = rs
	}

	cc := 3
	if st, err := s.repo.GetSettings(ctx); err == nil && st.CandidateCount > 0 {
		cc = int(st.CandidateCount)
	}
	// 健康分 = 服务端探测分 × 40% + 客户端上报分 × 60%（docs/12 第 4.3 节）；都无数据时按满分，不因无数据过滤线路。
	ids := make([]int64, len(lines))
	for i := range lines {
		ids[i] = lines[i].ID
	}
	bd := ResolveHealth(ctx, s.health, s.quality, ids, geo, time.Now())
	evals := Evaluate(lines, geo, healthFunc(bd), cc, nil)
	// 把健康分来源挂到每条评估上，便于运营在预览页核对分数构成
	for i := range evals {
		if b, ok := bd[evals[i].LineID]; ok {
			b := b
			evals[i].Health = &b
		}
	}
	return PreviewResult{
		Region: formatRegion(geo), Country: geo.Country, Province: geo.Province, ISP: geo.ISP, ASN: geo.ASN,
		CandidateCount: cc, Lines: evals,
	}, nil
}

func normalizeGeo(g Geo) Geo {
	g.Country = strings.ToUpper(strings.TrimSpace(g.Country))
	if g.Country == "" {
		g.Country = unknownCountry
	}
	g.Province = strings.TrimSpace(g.Province)
	g.ISP = strings.TrimSpace(g.ISP)
	g.ASN = strings.TrimSpace(g.ASN)
	return g
}

func formatRegion(g Geo) string {
	parts := []string{g.Country}
	if g.Province != "" && g.Province != "*" {
		parts = append(parts, g.Province)
	}
	if g.ISP != "" && g.ISP != "*" {
		parts = append(parts, g.ISP)
	}
	return strings.Join(parts, "-")
}
