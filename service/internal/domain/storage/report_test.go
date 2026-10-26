package storage

import (
	"context"
	"testing"
)

type bucketKey struct {
	LineID       int64
	Country, ISP string
}

type stubQualityStore struct {
	buckets map[bucketKey]QualityDelta
	calls   int
}

func newStubQualityStore() *stubQualityStore {
	return &stubQualityStore{buckets: map[bucketKey]QualityDelta{}}
}

func (s *stubQualityStore) AddQuality(_ context.Context, lineID int64, country, isp string, _ int64, d QualityDelta) error {
	s.calls++
	k := bucketKey{LineID: lineID, Country: country, ISP: isp}
	cur := s.buckets[k]
	cur.Samples += d.Samples
	cur.FirstFrameSum += d.FirstFrameSum
	cur.FirstFrameN += d.FirstFrameN
	cur.Errors += d.Errors
	cur.Stalls += d.Stalls
	s.buckets[k] = cur
	return nil
}

func (s *stubQualityStore) SumQuality(_ context.Context, lineIDs []int64, country, isp string, _ []int64) (map[int64]QualityDelta, error) {
	out := map[int64]QualityDelta{}
	for _, id := range lineIDs {
		if d, ok := s.buckets[bucketKey{LineID: id, Country: country, ISP: isp}]; ok {
			out[id] = d
		}
	}
	return out, nil
}

func TestReportService_AggregatesByLineAndRegion(t *testing.T) {
	store := newStubQualityStore()
	svc := NewReportService(NopGeoResolver, store)

	n, err := svc.Report(context.Background(), ReportInput{
		Geo: Geo{Country: "CN", Province: "广东", ISP: "电信"},
		Items: []ReportItem{
			{LineID: 12, Kind: LineVideo, Event: EventFirstFrame, Ms: 820},
			{LineID: 12, Kind: LineVideo, Event: EventFirstFrame, Ms: 600},
			{LineID: 12, Kind: LineVideo, Event: EventStall},
			{LineID: 7, Kind: LineVideo, Event: EventError, Reason: "timeout"},
			{LineID: 12, Kind: LineVideo, Event: EventSwitch, ToLineID: 7, Trigger: "auto_stall"},
		},
	})
	if err != nil {
		t.Fatalf("Report 出错：%v", err)
	}
	if n != 5 {
		t.Errorf("接受条数=%d，期望 5", n)
	}
	// 同批同线路合并成一次写入，减少 Valkey 往返：两条线路 → 两次调用
	if store.calls != 2 {
		t.Errorf("Valkey 写入次数=%d，期望 2（按线路合并）", store.calls)
	}
	got := store.buckets[bucketKey{LineID: 12, Country: "CN", ISP: "电信"}]
	want := QualityDelta{Samples: 4, FirstFrameSum: 1420, FirstFrameN: 2, Stalls: 1}
	if got != want {
		t.Errorf("线路 12 聚合结果=%+v，期望 %+v", got, want)
	}
	if got := store.buckets[bucketKey{LineID: 7, Country: "CN", ISP: "电信"}]; got.Errors != 1 || got.Samples != 1 {
		t.Errorf("线路 7 应记 1 次失败，得到 %+v", got)
	}
}

func TestReportService_SkipsInvalidItems(t *testing.T) {
	store := newStubQualityStore()
	svc := NewReportService(NopGeoResolver, store)

	n, err := svc.Report(context.Background(), ReportInput{
		Geo: Geo{Country: "CN"},
		Items: []ReportItem{
			{LineID: 0, Event: EventFirstFrame, Ms: 100}, // 线路编号非法
			{LineID: 5, Event: "unknown_event"},          // 事件未知
			{LineID: 5, Event: EventFirstFrame, Ms: 0},   // 耗时缺失：计样本不计首帧
			{LineID: 5, Event: EventProbe, Ms: 120},      // 合法
		},
	})
	if err != nil {
		t.Fatalf("Report 出错：%v", err)
	}
	if n != 2 {
		t.Errorf("接受条数=%d，期望 2（跳过非法项）", n)
	}
	got := store.buckets[bucketKey{LineID: 5, Country: "CN", ISP: ""}]
	if got.Samples != 2 || got.FirstFrameN != 0 || got.FirstFrameSum != 0 {
		t.Errorf("耗时缺失的首帧不应计入均值：%+v", got)
	}
}

func TestReportService_UnknownRegionFallsBackToXX(t *testing.T) {
	store := newStubQualityStore()
	svc := NewReportService(NopGeoResolver, store)

	// 填了 IP：经 NopGeoResolver 解析为未知地区 XX（GeoIP 数据未接入）
	if _, err := svc.Report(context.Background(), ReportInput{
		IP:    "1.2.3.4",
		Items: []ReportItem{{LineID: 9, Event: EventFirstFrame, Ms: 500}},
	}); err != nil {
		t.Fatalf("Report 出错：%v", err)
	}
	if _, ok := store.buckets[bucketKey{LineID: 9, Country: "XX", ISP: ""}]; !ok {
		t.Errorf("地区未知时应归入 XX 桶，实际桶：%+v", store.buckets)
	}
}

func TestReportService_EmptyBatch(t *testing.T) {
	store := newStubQualityStore()
	svc := NewReportService(NopGeoResolver, store)
	n, err := svc.Report(context.Background(), ReportInput{Geo: Geo{Country: "CN"}})
	if err != nil || n != 0 {
		t.Errorf("空批次应返回 0 且无错误，得到 n=%d err=%v", n, err)
	}
	if store.calls != 0 {
		t.Errorf("空批次不应写 Valkey，实际写了 %d 次", store.calls)
	}
}
