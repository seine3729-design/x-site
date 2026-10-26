package storage

import (
	"context"
	"testing"
	"time"
)

// stubSelectRepo 只实现选线需要的三个查询，其余方法由 fakeRepo 的存根继承。
type stubSelectRepo struct {
	Repo
	lines    []Line
	rules    map[int64][]Rule
	settings Settings
}

func (r *stubSelectRepo) ListLines(context.Context, LineQuery) ([]Line, error) { return r.lines, nil }
func (r *stubSelectRepo) ListRulesByLine(_ context.Context, id int64) ([]Rule, error) {
	return r.rules[id], nil
}
func (r *stubSelectRepo) GetSettings(context.Context) (Settings, error) { return r.settings, nil }

func newSelectRepo(lines []Line, candidateCount int32) *stubSelectRepo {
	return &stubSelectRepo{
		lines: lines, rules: map[int64][]Rule{}, settings: Settings{CandidateCount: candidateCount},
	}
}

func TestLinesService_List(t *testing.T) {
	lines := []Line{
		{ID: 1, Kind: LineVideo, Name: "线路甲", Status: LineActive, RegionScope: ScopeGlobal, Weight: 100,
			BaseURL: "https://a.example.com", ProbePath: "/_probe/256k.bin", Priority: 10},
		{ID: 2, Kind: LineVideo, Name: "线路乙", Status: LineActive, RegionScope: ScopeGlobal, Weight: 100,
			BaseURL: "", ProbePath: "/_probe/256k.bin"}, // 直连存储：无测速地址
		{ID: 3, Kind: LineVideo, Name: "线路丙", Status: LineDisabled, RegionScope: ScopeGlobal, Weight: 100,
			BaseURL: "https://c.example.com", ProbePath: "/p"},
	}
	svc := NewLinesService(newSelectRepo(lines, 2), NopGeoResolver, NopHealthStore, NopQualityStore)

	res, err := svc.List(context.Background(), SelectInput{Geo: Geo{Country: "CN", Province: "广东", ISP: "电信"}, Kind: LineVideo})
	if err != nil {
		t.Fatalf("List 出错：%v", err)
	}
	if res.Region != "CN-广东-电信" {
		t.Errorf("region=%q，期望 CN-广东-电信", res.Region)
	}
	if res.TTL != lineListTTL {
		t.Errorf("ttl=%d，期望 %d", res.TTL, lineListTTL)
	}
	// 候选数量设为 2：停用的线路不返回，只给入选的两条
	if len(res.Lines) != 2 {
		t.Fatalf("应返回 2 条候选，得到 %d 条：%+v", len(res.Lines), res.Lines)
	}
	// priority 高的排第一并标记推荐
	if res.Lines[0].LineID != 1 || !res.Lines[0].Recommended {
		t.Errorf("优先级高的线路应排第一且推荐：%+v", res.Lines[0])
	}
	if res.Lines[1].Recommended {
		t.Error("只有第一条可以标记推荐")
	}
	if got := res.Lines[0].ProbeURL; got != "https://a.example.com/_probe/256k.bin" {
		t.Errorf("测速地址=%q", got)
	}
	// 无 base_url 的直连线路测速地址为空
	if got := res.Lines[1].ProbeURL; got != "" {
		t.Errorf("直连存储线路不应有测速地址，得到 %q", got)
	}
	// 不含对象地址或签名字段（响应结构里本就没有，这里确认线路名透传）
	if res.Lines[0].Name != "线路甲" {
		t.Errorf("线路名应透传，得到 %q", res.Lines[0].Name)
	}
}

func TestLinesService_List_RejectsBadKind(t *testing.T) {
	svc := NewLinesService(newSelectRepo(nil, 3), NopGeoResolver, NopHealthStore, NopQualityStore)
	if _, err := svc.List(context.Background(), SelectInput{Kind: "audio"}); err == nil {
		t.Error("非法类型应返回 400")
	}
}

// 同设备在 10 分钟片内选线结果稳定，不同设备整体按权重分摊。
func TestWeightedRNG_StablePerDeviceAndTimeSlice(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	// 同设备、同时间片：序列一致
	a, b := weightedRNG("dev-1", now), weightedRNG("dev-1", now.Add(3*time.Minute))
	if a.Float64() != b.Float64() {
		t.Error("同设备在同一个 10 分钟片内应得到相同的随机序列")
	}
	// 同设备、跨时间片：序列变化（10 分钟后重新打散）
	c := weightedRNG("dev-1", now.Add(11*time.Minute))
	if weightedRNG("dev-1", now).Float64() == c.Float64() {
		t.Error("跨 10 分钟片应重新打散")
	}
	// 不同设备：序列不同
	if weightedRNG("dev-1", now).Float64() == weightedRNG("dev-2", now).Float64() {
		t.Error("不同设备应得到不同的随机序列")
	}
	// 无设备标识：退化为确定性排序（nil）
	if weightedRNG("", now) != nil {
		t.Error("设备标识为空时应退化为确定性排序")
	}
}

// 同分候选按 weight 加权随机：高权重线路拿到第一名的次数应显著更多。
func TestEvaluate_WeightedTiebreak(t *testing.T) {
	lines := []Line{
		{ID: 1, Status: LineActive, RegionScope: ScopeGlobal, Weight: 90},
		{ID: 2, Status: LineActive, RegionScope: ScopeGlobal, Weight: 10},
	}
	var firstWins int
	const rounds = 400
	for i := range rounds {
		rng := weightedRNG("dev", time.Unix(int64(i)*600, 0)) // 每轮换一个时间片
		evals := Evaluate(lines, gd, health100, 2, rng)
		if evalOf(evals, 1).Rank == 1 {
			firstWins++
		}
	}
	// 期望约 90%：放宽到 70%–99% 以免偶发抖动导致不稳定
	if firstWins < rounds*70/100 || firstWins > rounds*99/100 {
		t.Errorf("权重 90 的线路应在约 90%% 的轮次排第一，实际 %d/%d", firstWins, rounds)
	}
	// 确定性模式（rng=nil）：始终按线路 ID 稳定排序
	for range 5 {
		if evalOf(Evaluate(lines, gd, health100, 2, nil), 1).Rank != 1 {
			t.Fatal("rng 为 nil 时应按线路 ID 稳定排序")
		}
	}
}
