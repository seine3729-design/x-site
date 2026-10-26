package storage

import (
	"context"
	"testing"
	"time"
)

func TestQualityScore(t *testing.T) {
	cases := []struct {
		name   string
		d      QualityDelta
		want   int
		wantOK bool
	}{
		{"样本不足不计分", QualityDelta{Samples: 4, FirstFrameN: 4, FirstFrameSum: 400}, 0, false},
		// 无失败 + 首帧 500ms（≤1000 满分）：100*0.6 + 100*0.4 = 100
		{"全好满分", QualityDelta{Samples: 10, FirstFrameN: 10, FirstFrameSum: 5000}, 100, true},
		// 无失败 + 无首帧样本：只用失败率分
		{"只有失败率", QualityDelta{Samples: 10}, 100, true},
		// 失败率 10%（阈值 20%）→ 失败分 50；首帧 500ms → 100。50*0.6 + 100*0.4 = 70
		{"一成失败", QualityDelta{Samples: 10, Errors: 1, FirstFrameN: 10, FirstFrameSum: 5000}, 70, true},
		// 失败率 20% 触顶 → 失败分 0；首帧满分。0*0.6 + 100*0.4 = 40
		{"失败率触顶", QualityDelta{Samples: 10, Errors: 2, FirstFrameN: 10, FirstFrameSum: 5000}, 40, true},
		// 失败率 50% → 失败分 0
		{"半数失败", QualityDelta{Samples: 10, Errors: 5}, 0, true},
		// 无失败 + 首帧 4000ms（≥上限）→ 首帧分 40。100*0.6 + 40*0.4 = 76
		{"首帧很慢", QualityDelta{Samples: 10, FirstFrameN: 10, FirstFrameSum: 40000}, 76, true},
		// 无失败 + 首帧 2500ms（中点）→ 首帧分 70。100*0.6 + 70*0.4 = 88
		{"首帧居中", QualityDelta{Samples: 10, FirstFrameN: 10, FirstFrameSum: 25000}, 88, true},
	}
	for _, c := range cases {
		got, ok := QualityScore(c.d)
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s：QualityScore=%d,%v，期望 %d,%v", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

func TestBlend(t *testing.T) {
	cases := []struct {
		name      string
		probe     int
		hasProbe  bool
		client    int
		hasClient bool
		want      int
	}{
		// 探测 40% + 客户端 60%
		{"两者都有", 100, true, 50, true, 70},
		{"两者都有-探测差", 0, true, 100, true, 60},
		{"只有探测", 80, true, 0, false, 80},
		{"只有客户端", 0, false, 30, true, 30},
		{"都没有按满分", 0, false, 0, false, DefaultHealthScore},
	}
	for _, c := range cases {
		if got := blend(c.probe, c.hasProbe, c.client, c.hasClient); got != c.want {
			t.Errorf("%s：blend=%d，期望 %d", c.name, got, c.want)
		}
	}
}

func TestQualityWindow(t *testing.T) {
	now := time.Unix(600*1000, 0) // 整分钟
	w := QualityWindow(now)
	if len(w) != QualityWindowMinutes {
		t.Fatalf("窗口长度=%d，期望 %d", len(w), QualityWindowMinutes)
	}
	cur := now.Unix() / 60
	if w[len(w)-1] != cur {
		t.Errorf("窗口应以当前分钟结尾，得到 %d，期望 %d", w[len(w)-1], cur)
	}
	if w[0] != cur-int64(QualityWindowMinutes-1) {
		t.Errorf("窗口起点=%d，期望 %d", w[0], cur-int64(QualityWindowMinutes-1))
	}
}

// 可按地区返回不同数据的质量存根，用于验证地区优先与回退。
type regionQualityStore struct {
	data map[bucketKey]QualityDelta
	// 记录每次 SumQuality 的地区，验证是否发生了回退查询
	queried []string
}

func (s *regionQualityStore) AddQuality(context.Context, int64, string, string, int64, QualityDelta) error {
	return nil
}

func (s *regionQualityStore) SumQuality(_ context.Context, lineIDs []int64, country, isp string, _ []int64) (map[int64]QualityDelta, error) {
	s.queried = append(s.queried, country+"/"+isp)
	out := map[int64]QualityDelta{}
	for _, id := range lineIDs {
		if d, ok := s.data[bucketKey{LineID: id, Country: country, ISP: isp}]; ok {
			out[id] = d
		}
	}
	return out, nil
}

func TestResolveHealth_PrefersRegionalData(t *testing.T) {
	probe := &stubHealthStore{data: map[int64]LineHealth{
		1: {LineID: 1, Score: 100},
	}}
	q := &regionQualityStore{data: map[bucketKey]QualityDelta{
		// 本地区：10 条样本、5 次失败 → 失败率 50% → 客户端分 0
		{LineID: 1, Country: "CN", ISP: "电信"}: {Samples: 10, Errors: 5},
		// 汇总桶：全好 → 100（不该被用到，因为本地区样本已足够）
		{LineID: 1, Country: "*", ISP: "*"}: {Samples: 100},
	}}

	bd := ResolveHealth(context.Background(), probe, q, []int64{1}, Geo{Country: "CN", ISP: "电信"}, time.Now())
	b := bd[1]
	if !b.ByRegion {
		t.Error("本地区样本充足时应标记 ByRegion")
	}
	if b.ClientScore != 0 || !b.HasClient {
		t.Errorf("应采用本地区的客户端分 0，得到 %d（has=%v）", b.ClientScore, b.HasClient)
	}
	// 探测 100 × 40% + 客户端 0 × 60% = 40
	if b.Final != 40 {
		t.Errorf("融合分=%d，期望 40", b.Final)
	}
}

func TestResolveHealth_FallsBackToGlobalWhenRegionThin(t *testing.T) {
	probe := &stubHealthStore{data: map[int64]LineHealth{1: {LineID: 1, Score: 100}}}
	q := &regionQualityStore{data: map[bucketKey]QualityDelta{
		// 本地区样本不足（2 < 5）
		{LineID: 1, Country: "CN", ISP: "电信"}: {Samples: 2},
		// 汇总桶：10 条、2 次失败 → 失败率 20% 触顶 → 客户端分 0
		{LineID: 1, Country: "*", ISP: "*"}: {Samples: 10, Errors: 2},
	}}

	bd := ResolveHealth(context.Background(), probe, q, []int64{1}, Geo{Country: "CN", ISP: "电信"}, time.Now())
	b := bd[1]
	if b.ByRegion {
		t.Error("回退到汇总桶时不应标记 ByRegion")
	}
	if !b.HasClient || b.ClientScore != 0 {
		t.Errorf("应回退到汇总桶的客户端分 0，得到 %d（has=%v）", b.ClientScore, b.HasClient)
	}
	if b.Final != 40 {
		t.Errorf("融合分=%d，期望 40", b.Final)
	}
	// 确认确实查了两次：先本地区、再汇总
	if len(q.queried) != 2 || q.queried[0] != "CN/电信" || q.queried[1] != "*/*" {
		t.Errorf("查询顺序应为本地区后汇总，实际 %v", q.queried)
	}
}

func TestResolveHealth_UnknownRegionSkipsRegionalQuery(t *testing.T) {
	probe := &stubHealthStore{data: map[int64]LineHealth{1: {LineID: 1, Score: 60}}}
	q := &regionQualityStore{data: map[bucketKey]QualityDelta{
		{LineID: 1, Country: "*", ISP: "*"}: {Samples: 10},
	}}

	// 地区未知（XX）：直接查汇总桶，省一次往返
	bd := ResolveHealth(context.Background(), probe, q, []int64{1}, Geo{Country: unknownCountry}, time.Now())
	if len(q.queried) != 1 || q.queried[0] != "*/*" {
		t.Errorf("地区未知时应只查汇总桶，实际 %v", q.queried)
	}
	// 探测 60 × 40% + 客户端 100 × 60% = 84
	if got := bd[1].Final; got != 84 {
		t.Errorf("融合分=%d，期望 84", got)
	}
}

func TestResolveHealth_NoDataFallsBackToDefault(t *testing.T) {
	bd := ResolveHealth(context.Background(), NopHealthStore, NopQualityStore, []int64{1, 2}, gd, time.Now())
	for _, id := range []int64{1, 2} {
		b := bd[id]
		if b.HasProbe || b.HasClient {
			t.Errorf("线路 %d 不应有任何数据：%+v", id, b)
		}
		if b.Final != DefaultHealthScore {
			t.Errorf("线路 %d 无数据时应为缺省满分，得到 %d", id, b.Final)
		}
	}
}

func TestResolveHealth_ThinSamplesStillExposedForDashboard(t *testing.T) {
	probe := &stubHealthStore{data: map[int64]LineHealth{1: {LineID: 1, Score: 70}}}
	q := &regionQualityStore{data: map[bucketKey]QualityDelta{
		{LineID: 1, Country: "*", ISP: "*"}: {Samples: 3, Errors: 1, FirstFrameN: 3, FirstFrameSum: 3000},
	}}

	bd := ResolveHealth(context.Background(), probe, q, []int64{1}, Geo{Country: unknownCountry}, time.Now())
	b := bd[1]
	// 样本不足：不参与打分，但原始数据仍要带出来给看板展示
	if b.HasClient {
		t.Error("样本不足不应参与打分")
	}
	if b.Quality.Samples != 3 {
		t.Errorf("样本不足时仍应返回原始聚合供看板展示，得到 %+v", b.Quality)
	}
	if b.Final != 70 {
		t.Errorf("样本不足时应只用探测分 70，得到 %d", b.Final)
	}
}

// 融合后的健康分必须真正影响选线：客户端上报很差的线路应被挤出候选。
func TestResolveHealth_DrivesLineSelection(t *testing.T) {
	lines := []Line{
		{ID: 1, Kind: LineVideo, Name: "上报良好", Status: LineActive, RegionScope: ScopeGlobal, Weight: 100,
			BaseURL: "https://good.example.com", ProbePath: "/p"},
		{ID: 2, Kind: LineVideo, Name: "上报很差", Status: LineActive, RegionScope: ScopeGlobal, Weight: 100,
			BaseURL: "https://bad.example.com", ProbePath: "/p"},
	}
	// 两条线路的探测分都是满分，差别只在客户端上报
	probe := &stubHealthStore{data: map[int64]LineHealth{
		1: {LineID: 1, Score: 100}, 2: {LineID: 2, Score: 100},
	}}
	q := &regionQualityStore{data: map[bucketKey]QualityDelta{
		{LineID: 1, Country: "*", ISP: "*"}: {Samples: 20},             // 无失败 → 客户端 100 → 融合 100
		{LineID: 2, Country: "*", ISP: "*"}: {Samples: 20, Errors: 20}, // 全失败 → 客户端 0 → 融合 40
	}}
	repo := newSelectRepo(lines, 2)
	svc := NewLinesService(repo, NopGeoResolver, probe, q)

	res, err := svc.List(context.Background(), SelectInput{Kind: LineVideo})
	if err != nil {
		t.Fatalf("List 出错：%v", err)
	}
	if len(res.Lines) != 2 {
		t.Fatalf("两条线路都健康（40 > 30 阈值），应都入选，得到 %d 条", len(res.Lines))
	}
	// 融合分 100 的线路得分更高，应排第一
	if res.Lines[0].LineID != 1 || !res.Lines[0].Recommended {
		t.Errorf("客户端上报更好的线路应排第一，实际顺序 %v", []int64{res.Lines[0].LineID, res.Lines[1].LineID})
	}
}
