package storage

import (
	"context"
	"testing"
	"time"
)

func TestScoreFromProbe(t *testing.T) {
	cases := []struct {
		name string
		res  LineProbeResult
		want int
	}{
		{"不可达记零分", LineProbeResult{OK: false, ElapsedMs: 100}, 0},
		{"快速满分", LineProbeResult{OK: true, ElapsedMs: 100}, 100},
		{"边界 300ms 满分", LineProbeResult{OK: true, ElapsedMs: 300}, 100},
		{"很慢保底 50", LineProbeResult{OK: true, ElapsedMs: 5000}, 50},
		{"边界 3000ms 保底 50", LineProbeResult{OK: true, ElapsedMs: 3000}, 50},
		{"中间按耗时线性", LineProbeResult{OK: true, ElapsedMs: 1650}, 75}, // (1650-300)*50/2700=25，100-25=75
	}
	for _, c := range cases {
		if got := ScoreFromProbe(c.res); got != c.want {
			t.Errorf("%s：ScoreFromProbe=%d，期望 %d", c.name, got, c.want)
		}
	}
}

func TestNewHealthFromProbe(t *testing.T) {
	now := time.Now().UTC()
	res := LineProbeResult{OK: true, ElapsedMs: 100, HTTPStatus: 206, ContentType: "video/mp2t", CORS: true, Message: "可达"}

	// 首次无历史：直接用瞬时分，字段完整映射
	h := NewHealthFromProbe(7, res, LineHealth{}, false, now)
	if h.Score != 100 {
		t.Errorf("首次 Score=%d，期望 100", h.Score)
	}
	if h.LineID != 7 || !h.ProbeOK || h.HTTPStatus != 206 || h.ElapsedMs != 100 || !h.CORS || h.ContentType != "video/mp2t" || !h.CheckedAt.Equal(now) {
		t.Errorf("健康记录字段映射错误：%+v", h)
	}

	// 有历史：EMA = (瞬时 + 历史)/2，平滑抖动
	if got := NewHealthFromProbe(7, res, LineHealth{Score: 40}, true, now).Score; got != 70 {
		t.Errorf("EMA Score=%d，期望 70", got)
	}

	// 单次失败 + 历史高分：下降但不立刻归零（连续失败才趋近 0）
	if got := NewHealthFromProbe(7, LineProbeResult{OK: false}, LineHealth{Score: 100}, true, now).Score; got != 50 {
		t.Errorf("失败 EMA Score=%d，期望 50", got)
	}
}

func TestHealthServiceOverview(t *testing.T) {
	checked := time.Now().UTC()
	lines := []Line{
		{ID: 1, ProviderID: 10, Kind: LineVideo, Name: "线路甲", Status: LineActive, BaseURL: "https://a.example.com"},
		{ID: 2, ProviderID: 10, Kind: LineImage, Name: "线路乙", Status: LineActive, BaseURL: ""},
	}
	store := &stubHealthStore{data: map[int64]LineHealth{
		1: {LineID: 1, Score: 82, ProbeOK: true, HTTPStatus: 206, ElapsedMs: 420, CheckedAt: checked},
	}}
	svc := NewHealthService(stubLineLister{lines}, store, NopQualityStore, true)

	ov, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview 出错：%v", err)
	}
	if !ov.ProbeEnabled {
		t.Error("ProbeEnabled 应透传为 true")
	}
	if len(ov.Lines) != 2 {
		t.Fatalf("应有 2 条线路，得到 %d", len(ov.Lines))
	}
	if v := ov.Lines[0]; !v.HasData || v.Score != 82 || !v.ProbeOK || v.HTTPStatus != 206 || v.CheckedAt == nil {
		t.Errorf("有探测数据的线路视图错误：%+v", v)
	}
	// 无探测数据（含无 base_url 的直连线路）：按默认满分，CheckedAt 为空
	if v := ov.Lines[1]; v.HasData || v.Score != DefaultHealthScore || v.CheckedAt != nil {
		t.Errorf("无数据线路应默认满分且 CheckedAt 为空：%+v", v)
	}
}

type stubLineLister struct{ lines []Line }

func (s stubLineLister) ListLines(context.Context, LineQuery) ([]Line, error) { return s.lines, nil }

type stubHealthStore struct{ data map[int64]LineHealth }

func (s *stubHealthStore) SaveHealth(_ context.Context, h LineHealth, _ time.Duration) error {
	s.data[h.LineID] = h
	return nil
}

func (s *stubHealthStore) GetHealth(_ context.Context, id int64) (LineHealth, bool, error) {
	h, ok := s.data[id]
	return h, ok, nil
}

func (s *stubHealthStore) ListHealth(_ context.Context, ids []int64) (map[int64]LineHealth, error) {
	out := make(map[int64]LineHealth, len(ids))
	for _, id := range ids {
		if h, ok := s.data[id]; ok {
			out[id] = h
		}
	}
	return out, nil
}
