package storage

import "testing"

func health100(int64) int { return 100 }

func rule(country, province, isp string, effect RuleEffect, boost int32) Rule {
	return Rule{Country: country, Province: province, ISP: isp, Effect: effect, Boost: boost}
}

func line(id int64, scope RegionScope, status LineStatus, priority int32, rules ...Rule) Line {
	return Line{
		ID: id, Kind: LineVideo, AccessMode: AccessCDN, SignType: SignHMACToken, Status: status,
		Priority: priority, Weight: 100, RegionScope: scope, Rules: rules,
	}
}

// 按状态取某线路的评估。
func evalOf(evals []LineEval, id int64) LineEval {
	for _, e := range evals {
		if e.LineID == id {
			return e
		}
	}
	return LineEval{}
}

var gd = Geo{Country: "CN", Province: "广东", ISP: "电信", ASN: "4134"}

func TestEvaluate_PrecisionOrder(t *testing.T) {
	// 同是候选，精度高（省份+运营商具体）应排在全局兜底之前
	lines := []Line{
		line(1, ScopeGlobal, LineActive, 0),                                          // 全局兜底，精度 5
		line(2, ScopeRules, LineActive, 0, rule("CN", "广东", "电信", EffectInclude, 0)), // 精度 50
		line(3, ScopeRules, LineActive, 0, rule("CN", "*", "*", EffectInclude, 0)),   // 精度 20
	}
	evals := Evaluate(lines, gd, health100, 3, nil)
	if r := evalOf(evals, 2).Rank; r != 1 {
		t.Fatalf("省份+运营商精确匹配应排第一，得到 rank=%d", r)
	}
	if evalOf(evals, 2).Detail.Precision != precCPI {
		t.Fatalf("精度应为 %d，得到 %d", precCPI, evalOf(evals, 2).Detail.Precision)
	}
	if !evalOf(evals, 2).Recommended {
		t.Fatal("第一名应标记 recommended")
	}
	if evalOf(evals, 1).Rank != 3 {
		t.Fatalf("全局兜底应排最后，得到 rank=%d", evalOf(evals, 1).Rank)
	}
}

func TestEvaluate_Exclude(t *testing.T) {
	lines := []Line{line(1, ScopeGlobal, LineActive, 0, rule("CN", "*", "移动", EffectExclude, 0))}
	// 移动用户命中 exclude → 排除
	evals := Evaluate(lines, Geo{Country: "CN", Province: "广东", ISP: "移动"}, health100, 3, nil)
	if evalOf(evals, 1).Status != EvalExcluded {
		t.Fatalf("命中 exclude 应被排除，得到 %s", evalOf(evals, 1).Status)
	}
	// 电信用户不命中 exclude → 全局兜底仍可用
	evals = Evaluate(lines, gd, health100, 3, nil)
	if evalOf(evals, 1).Status != EvalCandidate {
		t.Fatalf("未命中 exclude 应作为候选，得到 %s", evalOf(evals, 1).Status)
	}
}

func TestEvaluate_Unmatched(t *testing.T) {
	// region_scope=rules 但规则不命中 → unmatched
	lines := []Line{line(1, ScopeRules, LineActive, 0, rule("HK", "*", "*", EffectInclude, 0))}
	evals := Evaluate(lines, gd, health100, 3, nil)
	if evalOf(evals, 1).Status != EvalUnmatched {
		t.Fatalf("规则线路未命中应为 unmatched，得到 %s", evalOf(evals, 1).Status)
	}
}

func TestEvaluate_WatchPenalty(t *testing.T) {
	lines := []Line{
		line(1, ScopeGlobal, LineActive, 0),
		line(2, ScopeGlobal, LineWatch, 0),
	}
	evals := Evaluate(lines, gd, health100, 3, nil)
	if evalOf(evals, 2).Detail.Watch != watchPenalty {
		t.Fatalf("观察中线路应扣 %d 分", watchPenalty)
	}
	if evalOf(evals, 1).Rank != 1 || evalOf(evals, 2).Rank != 2 {
		t.Fatalf("观察中线路应排在正常线路之后：%+v", evals)
	}
}

func TestEvaluate_UnhealthyFallback(t *testing.T) {
	lines := []Line{line(1, ScopeGlobal, LineActive, 0), line(2, ScopeGlobal, LineActive, 0)}
	// 两条都不健康：应兜底保留健康分最高的一条为候选
	health := func(id int64) int {
		if id == 1 {
			return 10
		}
		return 25
	}
	evals := Evaluate(lines, gd, health, 3, nil)
	if evalOf(evals, 2).Status != EvalCandidate || evalOf(evals, 2).Rank != 1 {
		t.Fatalf("全部不健康时应兜底健康分最高的一条，得到 %+v", evalOf(evals, 2))
	}
	if evalOf(evals, 1).Status != EvalUnhealthy {
		t.Fatalf("较低健康分的应保持 unhealthy，得到 %s", evalOf(evals, 1).Status)
	}
}

func TestEvaluate_CandidateLimit(t *testing.T) {
	lines := []Line{
		line(1, ScopeGlobal, LineActive, 3),
		line(2, ScopeGlobal, LineActive, 2),
		line(3, ScopeGlobal, LineActive, 1),
	}
	evals := Evaluate(lines, gd, health100, 2, nil)
	if evalOf(evals, 1).Rank != 1 || evalOf(evals, 2).Rank != 2 {
		t.Fatalf("优先级高的应入选前二：%+v", evals)
	}
	if evalOf(evals, 3).Status != EvalOverflow {
		t.Fatalf("超出候选数量的应标记 overflow，得到 %s", evalOf(evals, 3).Status)
	}
}

func TestEvaluate_ASNHighestPrecision(t *testing.T) {
	lines := []Line{line(1, ScopeRules, LineActive, 0, Rule{Country: "CN", Province: "*", ISP: "*", ASN: "4134", Effect: EffectInclude})}
	evals := Evaluate(lines, gd, health100, 3, nil)
	if evalOf(evals, 1).Detail.Precision != precASN {
		t.Fatalf("ASN 匹配应为最高精度 %d，得到 %d", precASN, evalOf(evals, 1).Detail.Precision)
	}
}
