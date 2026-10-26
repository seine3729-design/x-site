package storage

import (
	"context"
	"slices"
	"testing"
	"time"
)

// 选源用的选线（S1-7）：排序与 /api/v1/lines 一致，另外验证手选线路的处理。

// playbackRepo 只实现选线需要的三个查询，并记录请求的线路类型。
type playbackRepo struct {
	Repo
	lines    []Line
	rules    map[int64][]Rule
	settings Settings
	kinds    []LineKind
}

func (r *playbackRepo) ListLines(_ context.Context, q LineQuery) ([]Line, error) {
	r.kinds = append(r.kinds, q.Kind)
	return slices.Clone(r.lines), nil
}

func (r *playbackRepo) ListRulesByLine(_ context.Context, id int64) ([]Rule, error) {
	return r.rules[id], nil
}

func (r *playbackRepo) GetSettings(context.Context) (Settings, error) { return r.settings, nil }

// playbackHealth 给指定线路固定的探测健康分，其余无数据（按满分）。
type playbackHealth struct {
	nopHealthStore
	scores map[int64]int
}

func (h playbackHealth) ListHealth(_ context.Context, ids []int64) (map[int64]LineHealth, error) {
	out := map[int64]LineHealth{}
	for _, id := range ids {
		if s, ok := h.scores[id]; ok {
			out[id] = LineHealth{LineID: id, Score: s, CheckedAt: time.Now()}
		}
	}
	return out, nil
}

type fixedGeo struct{ geo Geo }

func (g fixedGeo) Lookup(string) Geo { return g.geo }

// 优先级 40 > 30 > 20 > 10：线路 1–4 依次排名；线路 5 停用；线路 6 只给美国；线路 7 健康分过低。
func playbackLines() []Line {
	mk := func(id int64, prio int32, status LineStatus, scope RegionScope) Line {
		return Line{ID: id, Kind: LineVideo, Name: "线路", Status: status, RegionScope: scope, Priority: prio, Weight: 100}
	}
	return []Line{
		mk(1, 40, LineActive, ScopeGlobal),
		mk(2, 30, LineActive, ScopeGlobal),
		mk(3, 20, LineActive, ScopeGlobal),
		mk(4, 10, LineActive, ScopeGlobal),
		mk(5, 99, LineDisabled, ScopeGlobal),
		mk(6, 99, LineActive, ScopeRules),
		mk(7, 99, LineActive, ScopeGlobal),
	}
}

func newPlaybackService(n int32) (*LinesService, *playbackRepo) {
	repo := &playbackRepo{
		lines:    playbackLines(),
		rules:    map[int64][]Rule{6: {{LineID: 6, Country: "US", Province: "*", ISP: "*", Effect: EffectInclude}}},
		settings: Settings{CandidateCount: n},
	}
	health := playbackHealth{scores: map[int64]int{7: 10}}
	return NewLinesService(repo, NopGeoResolver, health, NopQualityStore), repo
}

func lineIDs(lines []PlaybackLine) []int64 {
	out := make([]int64, len(lines))
	for i, l := range lines {
		out[i] = l.LineID
	}
	return out
}

func TestForPlayback_PreferredLine(t *testing.T) {
	cn := Geo{Country: "CN", Province: "广东", ISP: "电信"}
	tests := []struct {
		name      string
		preferred int64
		want      []int64
		manual    bool
	}{
		{"没有手选：按排名取前 2 条", 0, []int64{1, 2}, false},
		{"手选排第一的线路：同时是推荐与手选", 1, []int64{1, 2}, true},
		{"手选候选内的线路：放到第一位", 2, []int64{2, 1}, true},
		{"手选超出候选数量的线路：放到第一位并挤掉最后一条", 4, []int64{4, 1}, true},
		{"手选停用的线路：忽略", 5, []int64{1, 2}, false},
		{"手选不服务本地区的线路：忽略", 6, []int64{1, 2}, false},
		{"手选不健康的线路：忽略", 7, []int64{1, 2}, false},
		{"手选不存在的线路：忽略", 999, []int64{1, 2}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newPlaybackService(2)
			sel, err := svc.ForPlayback(context.Background(), PlaybackInput{Geo: cn, Preferred: tt.preferred})
			if err != nil {
				t.Fatal(err)
			}
			if got := lineIDs(sel.Lines); !slices.Equal(got, tt.want) {
				t.Fatalf("线路顺序 %v，期望 %v", got, tt.want)
			}
			if sel.Lines[0].Manual != tt.manual {
				t.Fatalf("第一条的手选标记应为 %v：%+v", tt.manual, sel.Lines[0])
			}
			for _, l := range sel.Lines[1:] {
				if l.Manual {
					t.Fatalf("只有第一条可能是手选：%+v", l)
				}
			}
			// 推荐始终标在算法排第一的线路上，手选置顶后它排在第二
			for _, l := range sel.Lines {
				if l.Recommended != (l.LineID == 1) {
					t.Fatalf("推荐应只标在线路 1：%+v", l)
				}
			}
			if sel.Region != "CN-广东-电信" || sel.Lines[0].RegionTag != "通用" {
				t.Fatalf("地区与标签不对：%q %+v", sel.Region, sel.Lines[0])
			}
			if !slices.Equal(repo.kinds, []LineKind{LineVideo}) {
				t.Fatalf("选源只取视频线路：%v", repo.kinds)
			}
		})
	}
}

func TestForPlayback_ResolvesIPAndKeepsCandidateCount(t *testing.T) {
	repo := &playbackRepo{lines: playbackLines(), rules: map[int64][]Rule{
		6: {{LineID: 6, Country: "US", Province: "*", ISP: "*", Effect: EffectInclude}},
	}}
	svc := NewLinesService(repo, fixedGeo{Geo{Country: "us"}}, NopHealthStore, NopQualityStore)
	// 没有设置候选数量时默认 3 条；填了 IP 时按 GeoIP 结果匹配地区规则
	sel, err := svc.ForPlayback(context.Background(), PlaybackInput{IP: "203.0.113.7", Geo: Geo{Country: "CN"}})
	if err != nil {
		t.Fatal(err)
	}
	if sel.Region != "US" {
		t.Fatalf("应按 IP 解析出的地区：%q", sel.Region)
	}
	if got := lineIDs(sel.Lines); !slices.Equal(got, []int64{6, 7, 1}) {
		t.Fatalf("美国用户应排上只服务美国的线路 6，且默认取 3 条：%v", got)
	}
	if sel.Lines[0].RegionTag != "本地区" {
		t.Fatalf("命中地区规则的线路标「本地区」：%+v", sel.Lines[0])
	}
}

// 全部线路都不健康时算法兜底保留健康分最高的一条；它即使是手选的也不标「手选」（docs/12 第 4.3 节第 6 步要求健康）。
func TestForPlayback_PreferredUnhealthyFallback(t *testing.T) {
	repo := &playbackRepo{lines: playbackLines()[:2], settings: Settings{CandidateCount: 3}}
	health := playbackHealth{scores: map[int64]int{1: 10, 2: 20}}
	svc := NewLinesService(repo, NopGeoResolver, health, NopQualityStore)
	sel, err := svc.ForPlayback(context.Background(), PlaybackInput{Preferred: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := lineIDs(sel.Lines); !slices.Equal(got, []int64{2}) {
		t.Fatalf("应只兜底保留健康分最高的线路 2：%v", got)
	}
	if sel.Lines[0].Manual {
		t.Fatalf("不健康的兜底线路不标手选：%+v", sel.Lines[0])
	}
}

func TestForPlayback_NoLines(t *testing.T) {
	svc := NewLinesService(&playbackRepo{}, NopGeoResolver, NopHealthStore, NopQualityStore)
	sel, err := svc.ForPlayback(context.Background(), PlaybackInput{Preferred: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(sel.Lines) != 0 || sel.Region != "XX" {
		t.Fatalf("没有线路时返回空列表与未知地区：%+v", sel)
	}
}
