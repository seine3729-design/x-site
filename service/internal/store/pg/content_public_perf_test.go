package pg

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"xsite/service/internal/domain/content"
)

// 前台内容读接口在 1 万部剧上的查询计划与耗时（S1-4 验收：P99 ≤ 150ms，空缓存）。
// 建 1 万行数据比较慢，所以默认跳过，需要时执行：
//
//	./dev.sh go test ./internal/store/pg/ -run TestPublicContentPerf -v  （带 XSITE_PERF=1）
const perfDramas = 10000

// perfBudget 是单次请求的 P99 预算（docs/11 S1-4 验收）。
const perfBudget = 150 * time.Millisecond

func TestPublicContentPerf(t *testing.T) {
	if os.Getenv("XSITE_PERF") == "" {
		t.Skip("未设置 XSITE_PERF，跳过 1 万部剧的性能检查")
	}
	f := newPubFixture(t)
	ctx := context.Background()

	// 造 1 万部可见的剧：上架时间、热度、排序值、完结状态都分散开。
	start := time.Now()
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO drama (title, status, publish_at, view_base, hot_score, sort_weight, latest_episode, is_finished)
		SELECT $1 || i, 'published', now() - (i || ' minutes')::interval, i, i, i % 100, i % 30, i % 2 = 0
		FROM generate_series(1, $2) AS i`, f.mark+"压测", perfDramas); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO drama_term (drama_id, term_id) SELECT id, $2 FROM drama WHERE title LIKE $1`,
		f.mark+"压测%", f.termA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO drama_term (drama_id, term_id) SELECT id, $2 FROM drama WHERE title LIKE $1 AND id % 3 = 0`,
		f.mark+"压测%", f.termB.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO drama_tag (drama_id, tag_id) SELECT id, $2 FROM drama WHERE title LIKE $1 AND id % 2 = 0`,
		f.mark+"压测%", f.tagA.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `ANALYZE drama, drama_term, drama_tag`); err != nil {
		t.Fatal(err)
	}
	var sampleID int64
	if err := f.pool.QueryRow(ctx, `SELECT max(id) FROM drama WHERE title LIKE $1`, f.mark+"压测%").Scan(&sampleID); err != nil {
		t.Fatal(err)
	}
	t.Logf("造 %d 部剧耗时 %v", perfDramas, time.Since(start).Round(time.Millisecond))

	topic := must(f.admin.CreateTopic(ctx, content.TopicInput{
		Title: f.mark + "压测专题", Slug: f.mark + "-perf", Status: content.TopicPublished, SourceType: content.SourceAuto,
		ListInIndex: true,
		Rule:        content.TopicRule{TermCodes: []string{f.termB.Code}, Sort: content.SortHot, Window: "30d", Limit: 24},
	}))(t)

	// 排序必须走索引：不能对 1 万行做全表扫描 + 排序。
	t.Run("排序用上索引", func(t *testing.T) {
		for _, tt := range []struct {
			sort  content.DramaSort
			index string
		}{
			{content.SortLatest, "drama_public_latest_idx"},
			{content.SortHot, "drama_status_hot_idx"},
			{content.SortRecommend, "drama_status_recommend_idx"},
		} {
			sql, args, err := publicDramaSQL(content.PublicDramaQuery{Sort: tt.sort, Limit: 24}, false)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := f.pool.Query(ctx, "EXPLAIN (COSTS OFF) "+sql, args...)
			if err != nil {
				t.Fatal(err)
			}
			lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				t.Fatal(err)
			}
			plan := strings.Join(lines, "\n")
			if !strings.Contains(plan, tt.index) || strings.Contains(plan, "Sort Key") {
				t.Errorf("%s 排序应直接按 %s 取前几条、不排序：\n%s", tt.sort, tt.index, plan)
			}
		}
	})

	tax := f.taxonomy.Slug
	cases := []struct {
		name string
		call func() error
	}{
		{"剧详情", func() error { _, err := f.svc.Drama(ctx, sampleID); return err }},
		{"频道第一页", func() error {
			_, err := f.svc.Channel(ctx, f.termA.Slug, content.ChannelRequest{})
			return err
		}},
		{"频道第 100 页", func() error {
			_, err := f.svc.Channel(ctx, f.termA.Slug, content.ChannelRequest{Page: 100, PageSize: 24, Sort: content.SortHot})
			return err
		}},
		{"标签页", func() error {
			_, err := f.svc.Tag(ctx, f.tagA.Slug, content.ListRequest{Sort: content.SortHot})
			return err
		}},
		{"探索（最新）", func() error { _, err := f.svc.Explore(ctx, content.ExploreRequest{}); return err }},
		{"探索（取值 + 标签 + 上新 + 热度）", func() error {
			_, err := f.svc.Explore(ctx, content.ExploreRequest{
				Terms: tax + ":" + f.termA.Slug + "," + tax + ":" + f.termB.Slug,
				Tag:   f.tagA.Slug, Window: "30d", Sort: content.SortHot,
			})
			return err
		}},
		{"探索（随机）", func() error {
			_, err := f.svc.Explore(ctx, content.ExploreRequest{Sort: content.SortRandom})
			return err
		}},
		{"专题列表", func() error { _, err := f.svc.Topics(ctx, content.ListRequest{}); return err }},
		{"专题详情（自动规则）", func() error { _, err := f.svc.Topic(ctx, topic.Slug); return err }},
	}
	for _, c := range cases {
		const n = 50
		durations := make([]time.Duration, 0, n)
		for range n {
			t0 := time.Now()
			if err := c.call(); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			durations = append(durations, time.Since(t0))
		}
		slices.Sort(durations)
		p50, p99 := durations[n/2], durations[n*99/100]
		t.Logf("%-28s P50 %-8v P99 %v", c.name, p50.Round(100*time.Microsecond), p99.Round(100*time.Microsecond))
		if p99 > perfBudget {
			t.Errorf("%s 的 P99 %v 超过预算 %v", c.name, p99, perfBudget)
		}
	}
	fmt.Fprintln(os.Stderr)
}
