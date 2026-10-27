package analytics

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeQueryRepo struct {
	daily     []DailyPoint
	dramaRows []DramaRow
	count     int64

	basic       DramaBasicRow
	basicFound  bool
	dramaDaily  []DramaDayMetric
	episodes    []EpisodeRetention
	gotDetailID int64

	gotFrom, gotTo      time.Time
	gotSort             string
	gotLimit, gotOffset int
}

func (f *fakeQueryRepo) ListMetricDaily(_ context.Context, from, to time.Time) ([]DailyPoint, error) {
	f.gotFrom, f.gotTo = from, to
	return f.daily, nil
}

func (f *fakeQueryRepo) CountDramaDaily(_ context.Context, _, _ time.Time) (int64, error) {
	return f.count, nil
}

func (f *fakeQueryRepo) ListDramaDaily(_ context.Context, from, to time.Time, sortColumn string, limit, offset int) ([]DramaRow, error) {
	f.gotFrom, f.gotTo = from, to
	f.gotSort, f.gotLimit, f.gotOffset = sortColumn, limit, offset
	return f.dramaRows, nil
}

func dp(day string, uv, playStarts int64) DailyPoint {
	return DailyPoint{Day: day, Metrics: Metrics{UV: uv, PlayStarts: playStarts}}
}

func TestOverviewWindowAndZeroFill(t *testing.T) {
	repo := &fakeQueryRepo{daily: []DailyPoint{
		dp("2026-10-20", 10, 1), dp("2026-10-21", 20, 2), dp("2026-10-22", 30, 3),
		dp("2026-10-23", 40, 4) /* 10-24 缺，验证零填充 */, dp("2026-10-25", 60, 6),
	}}
	svc := NewQueryService(repo)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, svc.loc) // 今天（北京）
	svc.now = func() time.Time { return now }

	ov, err := svc.Overview(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	// 窗口到昨天（10-25），当前窗口 = 10-23 ~ 10-25。
	if ov.From != "2026-10-23" || ov.To != "2026-10-25" || ov.Days != 3 {
		t.Fatalf("窗口不对: from=%s to=%s days=%d", ov.From, ov.To, ov.Days)
	}
	if len(ov.Daily) != 3 {
		t.Fatalf("每日序列应 3 天, 实际 %d", len(ov.Daily))
	}
	if ov.Daily[1].Day != "2026-10-24" || ov.Daily[1].UV != 0 {
		t.Fatalf("缺日未零填充: %+v", ov.Daily[1])
	}
	if ov.Totals.UV != 100 { // 40 + 0 + 60
		t.Fatalf("当前窗口 UV 汇总应 100, 实际 %d", ov.Totals.UV)
	}
	if ov.Prev.UV != 60 { // 10 + 20 + 30
		t.Fatalf("上一窗口 UV 汇总应 60, 实际 %d", ov.Prev.UV)
	}
	// 一次往返取 [prevFrom, curTo] = 10-20 ~ 10-25。
	if ov.Daily[0].UV != 40 || ov.Daily[2].UV != 60 {
		t.Fatalf("当前窗口序列值不对: %+v", ov.Daily)
	}
	if repo.gotFrom.Format(dateFmt) != "2026-10-20" || repo.gotTo.Format(dateFmt) != "2026-10-25" {
		t.Fatalf("repo 查询区间不对: %s ~ %s", repo.gotFrom.Format(dateFmt), repo.gotTo.Format(dateFmt))
	}
}

func TestDramasSortPagingAndNewFlag(t *testing.T) {
	repo := &fakeQueryRepo{count: 42}
	svc := NewQueryService(repo)
	fixed := time.Date(2026, 10, 26, 10, 0, 0, 0, svc.loc)
	svc.now = func() time.Time { return fixed }
	repo.dramaRows = []DramaRow{
		{ID: 1, Title: "新剧", CreatedAt: fixed, Viewers: 5},                    // 窗口内创建
		{ID: 2, Title: "老剧", CreatedAt: fixed.AddDate(0, 0, -60), Viewers: 3}, // 远早于窗口
	}

	res, err := svc.Dramas(context.Background(), DramasInput{Days: 7, Sort: "watch_seconds", Page: 2, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Sort != "watch_seconds" || repo.gotSort != "watch_seconds" {
		t.Fatalf("排序未透传: res=%s repo=%s", res.Sort, repo.gotSort)
	}
	if repo.gotLimit != 10 || repo.gotOffset != 10 {
		t.Fatalf("分页换算不对: limit=%d offset=%d", repo.gotLimit, repo.gotOffset)
	}
	if res.Total != 42 || res.Page != 2 || res.PageSize != 10 {
		t.Fatalf("结果分页不对: %+v", res)
	}
	cf, ct, _, _ := svc.window(7)
	if !repo.gotFrom.Equal(cf) || !repo.gotTo.Equal(ct) {
		t.Fatalf("剧集查询应取当前窗口: %v~%v 期望 %v~%v", repo.gotFrom, repo.gotTo, cf, ct)
	}
	if len(res.List) != 2 {
		t.Fatalf("列表长度应 2, 实际 %d", len(res.List))
	}
	if !res.List[0].IsNew {
		t.Fatalf("剧1 创建于窗口内应标为新剧")
	}
	if res.List[1].IsNew {
		t.Fatalf("剧2 创建于窗口前不应标为新剧")
	}
}

func TestDramasSortFallbackAndClamp(t *testing.T) {
	repo := &fakeQueryRepo{}
	svc := NewQueryService(repo)
	svc.now = func() time.Time { return time.Date(2026, 10, 26, 10, 0, 0, 0, svc.loc) }

	res, err := svc.Dramas(context.Background(), DramasInput{Days: 0, Sort: "bogus", Page: 0, PageSize: 0})
	if err != nil {
		t.Fatal(err)
	}
	if res.Sort != defaultSort || repo.gotSort != sortColumns[defaultSort] {
		t.Fatalf("非法排序未回退默认: res=%s repo=%s", res.Sort, repo.gotSort)
	}
	if res.Page != 1 || res.PageSize != defaultPageSize || repo.gotOffset != 0 {
		t.Fatalf("分页未回退默认: page=%d size=%d offset=%d", res.Page, res.PageSize, repo.gotOffset)
	}
	if res.Days != defaultDays {
		t.Fatalf("days 应回退默认 %d, 实际 %d", defaultDays, res.Days)
	}

	res2, err := svc.Dramas(context.Background(), DramasInput{Days: 999, Sort: "viewers", Page: 1, PageSize: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if res2.PageSize != defaultPageSize {
		t.Fatalf("超限 pageSize 应回退 %d, 实际 %d", defaultPageSize, res2.PageSize)
	}
	if res2.Days != maxDays {
		t.Fatalf("超限 days 应回退 %d, 实际 %d", maxDays, res2.Days)
	}
}

func (f *fakeQueryRepo) DramaBasic(_ context.Context, id int64) (DramaBasicRow, bool, error) {
	f.gotDetailID = id
	return f.basic, f.basicFound, nil
}

func (f *fakeQueryRepo) ListDramaDailyByID(_ context.Context, _ int64, from, to time.Time) ([]DramaDayMetric, error) {
	f.gotFrom, f.gotTo = from, to
	return f.dramaDaily, nil
}

func (f *fakeQueryRepo) ListEpisodeRetention(_ context.Context, _ int64, _, _ time.Time) ([]EpisodeRetention, error) {
	return f.episodes, nil
}

func TestDramaDetailWindowRetentionAndNotFound(t *testing.T) {
	repo := &fakeQueryRepo{}
	svc := NewQueryService(repo)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, svc.loc) // 今天（北京）
	svc.now = func() time.Time { return now }

	// 剧不存在 → found=false。
	if _, found, err := svc.DramaDetail(context.Background(), 404, 7); err != nil || found {
		t.Fatalf("不存在的剧应 found=false err=nil; 得 found=%v err=%v", found, err)
	}

	// 剧存在：days=3 → 当前窗口 10-23~10-25，上一窗口 10-20~10-22。
	repo.basicFound = true
	repo.basic = DramaBasicRow{ID: 7, Title: "测试剧", Status: "published", LatestEpisode: 3,
		CreatedAt: time.Date(2026, 10, 20, 8, 0, 0, 0, svc.loc)} // 创建于 10-20
	repo.dramaDaily = []DramaDayMetric{
		{Day: "2026-10-20", DramaMetrics: DramaMetrics{Viewers: 5, PlayStarts: 5, WatchSeconds: 50}},
		{Day: "2026-10-23", DramaMetrics: DramaMetrics{Viewers: 10, PlayStarts: 10, WatchSeconds: 100}},
		{Day: "2026-10-25", DramaMetrics: DramaMetrics{Viewers: 20, PlayStarts: 20, WatchSeconds: 200}},
	}
	repo.episodes = []EpisodeRetention{
		{EpisodeID: 1, Number: 1, Title: "第1集", Viewers: 100},
		{EpisodeID: 2, Number: 2, Title: "第2集", Viewers: 60},
		{EpisodeID: 3, Number: 3, Title: "第3集", Viewers: 30},
	}

	d, found, err := svc.DramaDetail(context.Background(), 7, 3)
	if err != nil || !found {
		t.Fatalf("剧存在应 found=true: found=%v err=%v", found, err)
	}
	if d.From != "2026-10-23" || d.To != "2026-10-25" || d.Days != 3 {
		t.Fatalf("窗口不对: %s~%s days=%d", d.From, d.To, d.Days)
	}
	if len(d.Daily) != 3 || d.Daily[1].Day != "2026-10-24" || d.Daily[1].Viewers != 0 {
		t.Fatalf("缺日未零填充: %+v", d.Daily)
	}
	if d.Totals.Viewers != 30 { // 10 + 0 + 20
		t.Fatalf("当前窗口 viewers 应 30: %d", d.Totals.Viewers)
	}
	if d.Prev.Viewers != 5 { // 仅 10-20=5
		t.Fatalf("上一窗口 viewers 应 5: %d", d.Prev.Viewers)
	}
	// 留存曲线：第1集 100、第2集 60、第3集 30。
	if len(d.Episodes) != 3 || d.Episodes[0].RetentionPct != 100 || d.Episodes[1].RetentionPct != 60 || d.Episodes[2].RetentionPct != 30 {
		t.Fatalf("留存百分比不对: %+v", d.Episodes)
	}
	// 上架天数：10-20 到 10-26 含当天 = 7。
	if d.OnlineDays != 7 {
		t.Fatalf("上架天数应 7: %d", d.OnlineDays)
	}
	if repo.gotDetailID != 7 {
		t.Fatalf("repo 收到的剧 id 应 7: %d", repo.gotDetailID)
	}
}

func TestExportContainsPromptDataAndCaliber(t *testing.T) {
	repo := &fakeQueryRepo{
		daily: []DailyPoint{dp("2026-10-23", 40, 4), dp("2026-10-25", 60, 6)},
		count: 1,
	}
	svc := NewQueryService(repo)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, svc.loc)
	svc.now = func() time.Time { return now }
	repo.dramaRows = []DramaRow{
		{ID: 7, Title: "霸道总裁|特殊", Viewers: 100, PlayStarts: 50, WatchSeconds: 7200, LatestEpisode: 12, CreatedAt: now},
	}

	pkg, err := svc.Export(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Days != 3 || pkg.From != "2026-10-23" || pkg.To != "2026-10-25" || pkg.GeneratedAt.IsZero() {
		t.Fatalf("导出窗口 / 时间不对: %+v", pkg)
	}
	c := pkg.Content
	for _, want := range []string{
		"你是资深运营数据分析师",                    // 提示词
		"运营数据简报（2026-10-23 ~ 2026-10-25", // 标题 + 窗口
		"独立访客 UV",                        // 概览指标
		"## 每日趋势",
		"## 热门剧集 Top 1",
		"霸道总裁\\|特殊", // 剧名里的 | 被转义
		"2.0 小时",    // 7200 秒
		"## 口径",
		"北京时间",
	} {
		if !strings.Contains(c, want) {
			t.Fatalf("导出内容缺少 %q\n--- 实际 ---\n%s", want, c)
		}
	}
}
