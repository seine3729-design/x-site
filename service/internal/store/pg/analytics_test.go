package pg

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/domain/analytics"
)

// 埋点采集（S4-1）走真实 PostgreSQL：CopyFrom 批量写入 event_raw 分区表，
// 覆盖分区路由（按 occurred_at 落到当天分区）、可空外键（user/drama/episode）与 jsonb props。
func TestAnalyticsRepo_InsertEvents(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	dev := fmt.Sprintf("it-ev-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM event_raw WHERE device_id = $1`, dev)
	})

	repo := NewAnalyticsRepo(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	drama := int64(7)
	user := int64(42)
	n, err := repo.InsertEvents(ctx, []analytics.Event{
		{OccurredAt: now, Type: "play_start", DeviceID: dev, UserID: &user, DramaID: &drama, Client: "web", Region: "CN", Props: []byte(`{}`)},
		{OccurredAt: now, Type: "watch", DeviceID: dev, Client: "app", Props: []byte(`{"seconds":30}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("应写入 2 条，得 %d", n)
	}

	type row struct {
		drama, user, ep *int64
		client, props   string
	}
	got := map[string]row{}
	rows, err := pool.Query(ctx,
		`SELECT event_type, drama_id, user_id, episode_id, client, props::text FROM event_raw WHERE device_id = $1`, dev)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var et string
		var r row
		if err := rows.Scan(&et, &r.drama, &r.user, &r.ep, &r.client, &r.props); err != nil {
			t.Fatal(err)
		}
		got[et] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("读回 %d 条，期望 2", len(got))
	}

	ps := got["play_start"]
	if ps.drama == nil || *ps.drama != 7 || ps.user == nil || *ps.user != 42 || ps.ep != nil || ps.client != "web" {
		t.Errorf("play_start 字段不符：%+v", ps)
	}
	w := got["watch"]
	if w.drama != nil || w.user != nil || w.client != "app" || !strings.Contains(w.props, "seconds") {
		t.Errorf("watch 字段不符：%+v", w)
	}
}

// 汇总（S4-2）走真实 PostgreSQL：造 event_raw → 经 domain Rollup（北京时区日界）→ 校验 metric_* 聚合。
// 用唯一 drama_id / episode_id 隔离，避免与本地其它数据相互干扰。
func TestAnalyticsRepo_Rollup(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAnalyticsRepo(pool)

	uniq := time.Now().UnixNano()
	dev1 := fmt.Sprintf("it-rl-%d-a", uniq)
	dev2 := fmt.Sprintf("it-rl-%d-b", uniq)
	drama := uniq%1_000_000 + 9_000_000
	ep := drama + 1
	now := time.Now().UTC()
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM event_raw WHERE device_id = ANY($1)`, []string{dev1, dev2})
		_, _ = pool.Exec(bg, `DELETE FROM metric_drama_daily WHERE drama_id = $1`, drama)
		_, _ = pool.Exec(bg, `DELETE FROM metric_episode_daily WHERE episode_id = $1`, ep)
	})

	mk := func(dev, typ, props string) analytics.Event {
		return analytics.Event{OccurredAt: now, Type: typ, DeviceID: dev, DramaID: &drama, EpisodeID: &ep, Client: "web", Props: []byte(props)}
	}
	if _, err := repo.InsertEvents(ctx, []analytics.Event{
		mk(dev1, "play_start", `{}`),
		mk(dev2, "play_start", `{}`),
		mk(dev1, "watch", `{"seconds":30}`),
		mk(dev1, "watch", `{"seconds":"bad"}`), // 非数字 seconds：被 jsonb_typeof 守卫排除
	}); err != nil {
		t.Fatal(err)
	}

	if err := analytics.NewRollup(repo).Daily(ctx, 1); err != nil {
		t.Fatal(err)
	}

	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	day := time.Now().In(loc).Format("2006-01-02")

	var viewers, starts, ws int64
	if err := pool.QueryRow(ctx,
		`SELECT viewers, play_starts, watch_seconds FROM metric_drama_daily WHERE day = $1 AND drama_id = $2`,
		day, drama).Scan(&viewers, &starts, &ws); err != nil {
		t.Fatalf("查 metric_drama_daily：%v", err)
	}
	if viewers != 2 || starts != 2 || ws != 30 {
		t.Errorf("剧日汇总应为 viewers=2 starts=2 watch=30，得 %d/%d/%d", viewers, starts, ws)
	}

	if err := pool.QueryRow(ctx,
		`SELECT viewers, play_starts, watch_seconds FROM metric_episode_daily WHERE day = $1 AND episode_id = $2`,
		day, ep).Scan(&viewers, &starts, &ws); err != nil {
		t.Fatalf("查 metric_episode_daily：%v", err)
	}
	if viewers != 2 || starts != 2 || ws != 30 {
		t.Errorf("分集日汇总应为 viewers=2 starts=2 watch=30，得 %d/%d/%d", viewers, starts, ws)
	}
}

// 分区维护（S4-2）：EnsurePartitions 为远未来日滚动创建分区，并能写入该日事件（验证边界格式）。
func TestAnalyticsRepo_EnsurePartition(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAnalyticsRepo(pool)

	future := time.Now().UTC().AddDate(0, 0, 120) // 迁移未建的远未来日
	name := "event_raw_" + future.Format("20060102")
	dev := fmt.Sprintf("it-pt-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM event_raw WHERE device_id = $1`, dev)
		_, _ = pool.Exec(bg, `DROP TABLE IF EXISTS `+name)
	})

	if err := repo.EnsurePartitions(ctx, future, future); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_class WHERE relname = $1)`, name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatalf("未创建分区 %s", name)
	}

	at := time.Date(future.Year(), future.Month(), future.Day(), 12, 0, 0, 0, time.UTC)
	if _, err := repo.InsertEvents(ctx, []analytics.Event{
		{OccurredAt: at, Type: "visitor", DeviceID: dev, Props: []byte("{}")},
	}); err != nil {
		t.Fatalf("写入未来分区失败（边界格式可能不对）：%v", err)
	}
}

// 看板读查询（S4-3）走真实 PostgreSQL：
//   - ListMetricDaily：逐列核对映射没串位、区间过滤与升序；
//   - CountDramaDaily / ListDramaDaily：JOIN drama、按剧 SUM、动态排序列、LIMIT/OFFSET 分页。
//
// 用远古日（1990-03）与唯一剧 ID 隔离，剧指标取极大值确保稳居共享窗口前列，断言对并行写入鲁棒。
func TestAnalyticsRepo_ReadQueries(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAnalyticsRepo(pool)

	// ---------- metric_daily：列映射 + 区间 + 升序 ----------
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM metric_daily WHERE day IN ('1990-03-01','1990-03-03')`)
	})
	// 各列取互不相同的值，任何字段串位都会在逐列断言时暴露；ON CONFLICT 幂等，便于并行复跑。
	if _, err := pool.Exec(ctx, `
		INSERT INTO metric_daily (day, uv, play_attempts, play_starts, play_errors, watch_seconds,
		                          registrations, reg_wall_views, ad_impressions, ad_clicks)
		VALUES ('1990-03-01', 11,12,13,14,15,16,17,18,19)
		ON CONFLICT (day) DO UPDATE SET uv=EXCLUDED.uv, play_attempts=EXCLUDED.play_attempts,
		  play_starts=EXCLUDED.play_starts, play_errors=EXCLUDED.play_errors, watch_seconds=EXCLUDED.watch_seconds,
		  registrations=EXCLUDED.registrations, reg_wall_views=EXCLUDED.reg_wall_views,
		  ad_impressions=EXCLUDED.ad_impressions, ad_clicks=EXCLUDED.ad_clicks`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO metric_daily (day, uv) VALUES ('1990-03-03', 99) ON CONFLICT (day) DO UPDATE SET uv=EXCLUDED.uv`); err != nil {
		t.Fatal(err)
	}

	daily, err := repo.ListMetricDaily(ctx,
		time.Date(1990, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(1990, 3, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	byDay := map[string]analytics.Metrics{}
	for _, d := range daily {
		byDay[d.Day] = d.Metrics
	}
	m, ok := byDay["1990-03-01"]
	if !ok {
		t.Fatalf("缺 1990-03-01 行，读回 %+v", daily)
	}
	if m.UV != 11 || m.PlayAttempts != 12 || m.PlayStarts != 13 || m.PlayErrors != 14 || m.WatchSeconds != 15 ||
		m.Registrations != 16 || m.RegWallViews != 17 || m.AdImpressions != 18 || m.AdClicks != 19 {
		t.Fatalf("metric_daily 列映射错位: %+v", m)
	}
	for i := 1; i < len(daily); i++ { // 升序
		if daily[i-1].Day > daily[i].Day {
			t.Fatalf("ListMetricDaily 未按日升序: %v", daily)
		}
	}

	// ---------- metric_drama_daily：JOIN + SUM + 动态排序 + 分页 ----------
	uniq := time.Now().UnixNano()
	high := insertID(t, pool, `INSERT INTO drama (title, status, latest_episode) VALUES ($1,'published',12) RETURNING id`, fmt.Sprintf("分析高-%d", uniq))
	low := insertID(t, pool, `INSERT INTO drama (title) VALUES ($1) RETURNING id`, fmt.Sprintf("分析低-%d", uniq))
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM metric_drama_daily WHERE drama_id = ANY($1)`, []int64{high, low})
		_, _ = pool.Exec(bg, `DELETE FROM drama WHERE id = ANY($1)`, []int64{high, low})
	})
	rows := []struct {
		id                  int64
		day                 string
		viewers, starts, ws int64
	}{
		{high, "1990-03-01", 9_000_000_010, 5, 9_000_000_100},
		{high, "1990-03-02", 20, 7, 50}, // 跨两天验证 SUM
		{low, "1990-03-01", 9_000_000_005, 1, 9_000_000_900},
	}
	for _, r := range rows {
		if _, err := pool.Exec(ctx,
			`INSERT INTO metric_drama_daily (day, drama_id, viewers, play_starts, watch_seconds) VALUES ($1::date,$2,$3,$4,$5)`,
			r.day, r.id, r.viewers, r.starts, r.ws); err != nil {
			t.Fatal(err)
		}
	}
	dfrom := time.Date(1990, 3, 1, 0, 0, 0, 0, time.UTC)
	dto := time.Date(1990, 3, 2, 0, 0, 0, 0, time.UTC)

	if cnt, err := repo.CountDramaDaily(ctx, dfrom, dto); err != nil {
		t.Fatal(err)
	} else if cnt < 2 {
		t.Fatalf("CountDramaDaily 应至少含造的 2 部, 得 %d", cnt)
	}

	// 按 viewers 降序：high(9e9+30) 在 low(9e9+5) 之前；核对 SUM 与 JOIN 字段。
	list, err := repo.ListDramaDaily(ctx, dfrom, dto, "viewers", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	iHigh, iLow := indexOf(list, high), indexOf(list, low)
	if iHigh < 0 || iLow < 0 {
		t.Fatalf("列表未包含造的两部剧: high@%d low@%d", iHigh, iLow)
	}
	if iHigh > iLow {
		t.Fatalf("viewers 降序应 high 在前: high@%d low@%d", iHigh, iLow)
	}
	hh := list[iHigh]
	if hh.Viewers != 9_000_000_030 || hh.PlayStarts != 12 || hh.WatchSeconds != 9_000_000_150 {
		t.Fatalf("按剧 SUM 聚合不对: %+v", hh)
	}
	if hh.Status != "published" || hh.LatestEpisode != 12 || hh.Title == "" || hh.CreatedAt.IsZero() {
		t.Fatalf("JOIN drama 字段不对: %+v", hh)
	}

	// 切到 watch_seconds 降序：low(9e9+900) 反超 high(9e9+150)，验证动态排序列生效。
	list2, err := repo.ListDramaDaily(ctx, dfrom, dto, "watch_seconds", 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	if indexOf(list2, low) > indexOf(list2, high) {
		t.Fatalf("watch_seconds 降序应 low 在 high 之前")
	}

	// 分页：size=1 两页首行应不同，验证 LIMIT/OFFSET 生效。
	p1, err := repo.ListDramaDaily(ctx, dfrom, dto, "viewers", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := repo.ListDramaDaily(ctx, dfrom, dto, "viewers", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(p1) != 1 || len(p2) != 1 {
		t.Fatalf("每页应 1 行, 得 %d / %d", len(p1), len(p2))
	}
	if p1[0].ID == p2[0].ID {
		t.Fatalf("OFFSET 未推进分页, 两页首行相同 id=%d", p1[0].ID)
	}
}

func indexOf(list []analytics.DramaRow, id int64) int {
	for i, d := range list {
		if d.ID == id {
			return i
		}
	}
	return -1
}

// 单剧分析读查询（S4-3）走真实库：造 1 部剧 + 3 集 + metric_drama_daily / metric_episode_daily，
// 验证 DramaBasic、ListDramaDailyByID（按日升序）、ListEpisodeRetention（JOIN episode、按集号、窗口内无数据的集 viewers=0）。
// 唯一剧 / 集 ID + 远古日隔离，ListEpisodeRetention 按 drama_id 过滤，断言对并行写入鲁棒。
func TestAnalyticsRepo_DramaDetailQueries(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewAnalyticsRepo(pool)

	uniq := time.Now().UnixNano()
	drama := insertID(t, pool, `INSERT INTO drama (title, status, latest_episode) VALUES ($1,'published',3) RETURNING id`, fmt.Sprintf("单剧分析-%d", uniq))
	ep1 := insertID(t, pool, `INSERT INTO episode (drama_id, number, title) VALUES ($1,1,'第1集') RETURNING id`, drama)
	ep2 := insertID(t, pool, `INSERT INTO episode (drama_id, number, title) VALUES ($1,2,'第2集') RETURNING id`, drama)
	ep3 := insertID(t, pool, `INSERT INTO episode (drama_id, number, title) VALUES ($1,3,'第3集') RETURNING id`, drama)
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM metric_drama_daily WHERE drama_id = $1`, drama)
		_, _ = pool.Exec(bg, `DELETE FROM metric_episode_daily WHERE episode_id = ANY($1)`, []int64{ep1, ep2, ep3})
		_, _ = pool.Exec(bg, `DELETE FROM drama WHERE id = $1`, drama) // 分集随 ON DELETE CASCADE
	})

	for _, r := range []struct {
		day     string
		v, s, w int64
	}{{"1990-04-01", 10, 10, 100}, {"1990-04-02", 20, 20, 200}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO metric_drama_daily (day, drama_id, viewers, play_starts, watch_seconds) VALUES ($1::date,$2,$3,$4,$5)`,
			r.day, drama, r.v, r.s, r.w); err != nil {
			t.Fatal(err)
		}
	}
	// ep1=100、ep2=60，ep3 不写（验证 LEFT JOIN 补 viewers=0）。
	if _, err := pool.Exec(ctx,
		`INSERT INTO metric_episode_daily (day, episode_id, drama_id, viewers, play_starts, watch_seconds)
		 VALUES ('1990-04-01',$1,$3,100,100,1000), ('1990-04-01',$2,$3,60,60,600)`,
		ep1, ep2, drama); err != nil {
		t.Fatal(err)
	}

	from := time.Date(1990, 4, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(1990, 4, 2, 0, 0, 0, 0, time.UTC)

	basic, found, err := repo.DramaBasic(ctx, drama)
	if err != nil || !found {
		t.Fatalf("DramaBasic 应找到: found=%v err=%v", found, err)
	}
	if basic.Status != "published" || basic.LatestEpisode != 3 || basic.Title == "" || basic.CreatedAt.IsZero() {
		t.Fatalf("剧基本信息不对: %+v", basic)
	}
	if _, f2, _ := repo.DramaBasic(ctx, -1); f2 {
		t.Fatalf("不存在的剧应 found=false")
	}

	daily, err := repo.ListDramaDailyByID(ctx, drama, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(daily) != 2 || daily[0].Day != "1990-04-01" || daily[0].Viewers != 10 || daily[1].Viewers != 20 {
		t.Fatalf("单剧每日序列不对: %+v", daily)
	}

	eps, err := repo.ListEpisodeRetention(ctx, drama, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 3 {
		t.Fatalf("应返回全部 3 集: %d", len(eps))
	}
	if eps[0].Number != 1 || eps[0].Viewers != 100 || eps[1].Viewers != 60 {
		t.Fatalf("集留存聚合不对: %+v", eps)
	}
	if eps[2].Number != 3 || eps[2].Title != "第3集" || eps[2].Viewers != 0 {
		t.Fatalf("窗口内无数据的集也应出现且 viewers=0: %+v", eps[2])
	}
}
