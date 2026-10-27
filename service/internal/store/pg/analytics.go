package pg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/analytics"
	"xsite/service/internal/store/pg/sqlcgen"
)

// AnalyticsRepo 实现埋点采集与看板汇总（S4-1 / S4-2）。
// 采集走 sqlc 的 CopyFrom；汇总（复杂聚合 + ON CONFLICT）与分区维护（动态 DDL）sqlc 不支持，手写参数化 SQL。
type AnalyticsRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewAnalyticsRepo(pool *pgxpool.Pool) *AnalyticsRepo {
	return &AnalyticsRepo{q: sqlcgen.New(pool), pool: pool}
}

// ---------- 采集（S4-1）----------

func (r *AnalyticsRepo) InsertEvents(ctx context.Context, events []analytics.Event) (int, error) {
	params := make([]sqlcgen.InsertEventsParams, len(events))
	for i, e := range events {
		params[i] = sqlcgen.InsertEventsParams{
			OccurredAt: pgtype.Timestamptz{Time: e.OccurredAt, Valid: true},
			EventType:  e.Type,
			DeviceID:   e.DeviceID,
			UserID:     eventNullID(e.UserID),
			DramaID:    eventNullID(e.DramaID),
			EpisodeID:  eventNullID(e.EpisodeID),
			Client:     e.Client,
			Region:     e.Region,
			Isp:        e.ISP,
			Props:      e.Props,
		}
	}
	n, err := r.q.InsertEvents(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("写入埋点事件: %w", err)
	}
	return int(n), nil
}

func eventNullID(p *int64) pgtype.Int8 {
	if p == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *p, Valid: true}
}

// ---------- 汇总（S4-2）----------

// watchSecondsExpr 是有效观看秒数之和：只累计 props.seconds 为 JSON 数字的 watch 事件，向下取整后求和。
// 用 jsonb_typeof 守卫避免 null / 非数字值在 ::numeric 时报错。
const watchSecondsExpr = `COALESCE(sum(floor((props->>'seconds')::numeric)) FILTER (WHERE event_type = 'watch' AND jsonb_typeof(props->'seconds') = 'number'), 0)::bigint`

var (
	metricDailySQL = `
INSERT INTO metric_daily (day, uv, play_attempts, play_starts, play_errors, watch_seconds,
                          registrations, reg_wall_views, ad_impressions, ad_clicks, finalized_at)
SELECT $1::date,
    count(DISTINCT device_id) FILTER (WHERE device_id <> ''),
    count(*) FILTER (WHERE event_type = 'play_attempt'),
    count(*) FILTER (WHERE event_type = 'play_start'),
    count(*) FILTER (WHERE event_type = 'play_error'),
    ` + watchSecondsExpr + `,
    count(*) FILTER (WHERE event_type = 'register'),
    count(*) FILTER (WHERE event_type = 'register_wall'),
    count(*) FILTER (WHERE event_type = 'ad_impression'),
    count(*) FILTER (WHERE event_type = 'ad_click'),
    now()
FROM event_raw WHERE occurred_at >= $2 AND occurred_at < $3
ON CONFLICT (day) DO UPDATE SET
    uv = EXCLUDED.uv, play_attempts = EXCLUDED.play_attempts, play_starts = EXCLUDED.play_starts,
    play_errors = EXCLUDED.play_errors, watch_seconds = EXCLUDED.watch_seconds,
    registrations = EXCLUDED.registrations, reg_wall_views = EXCLUDED.reg_wall_views,
    ad_impressions = EXCLUDED.ad_impressions, ad_clicks = EXCLUDED.ad_clicks, finalized_at = now()`

	metricDramaSQL = `
INSERT INTO metric_drama_daily (day, drama_id, viewers, play_starts, watch_seconds)
SELECT $1::date, drama_id,
    count(DISTINCT device_id) FILTER (WHERE device_id <> ''),
    count(*) FILTER (WHERE event_type = 'play_start'),
    ` + watchSecondsExpr + `
FROM event_raw WHERE occurred_at >= $2 AND occurred_at < $3 AND drama_id IS NOT NULL
GROUP BY drama_id
ON CONFLICT (day, drama_id) DO UPDATE SET
    viewers = EXCLUDED.viewers, play_starts = EXCLUDED.play_starts, watch_seconds = EXCLUDED.watch_seconds`

	metricEpisodeSQL = `
INSERT INTO metric_episode_daily (day, episode_id, drama_id, viewers, play_starts, watch_seconds)
SELECT $1::date, episode_id, COALESCE(max(drama_id), 0),
    count(DISTINCT device_id) FILTER (WHERE device_id <> ''),
    count(*) FILTER (WHERE event_type = 'play_start'),
    ` + watchSecondsExpr + `
FROM event_raw WHERE occurred_at >= $2 AND occurred_at < $3 AND episode_id IS NOT NULL
GROUP BY episode_id
ON CONFLICT (day, episode_id) DO UPDATE SET
    drama_id = EXCLUDED.drama_id, viewers = EXCLUDED.viewers,
    play_starts = EXCLUDED.play_starts, watch_seconds = EXCLUDED.watch_seconds`

	metricHourlySQL = `
INSERT INTO metric_hourly (hour, uv, play_starts, watch_seconds)
SELECT $1::timestamptz,
    count(DISTINCT device_id) FILTER (WHERE device_id <> ''),
    count(*) FILTER (WHERE event_type = 'play_start'),
    ` + watchSecondsExpr + `
FROM event_raw WHERE occurred_at >= $1::timestamptz AND occurred_at < $1::timestamptz + interval '1 hour'
ON CONFLICT (hour) DO UPDATE SET
    uv = EXCLUDED.uv, play_starts = EXCLUDED.play_starts, watch_seconds = EXCLUDED.watch_seconds`
)

// RollupDay 定稿某一天（day 为北京日期字符串 YYYY-MM-DD，from/to 为该日的 UTC 时间范围）。
// 三张日表在一个事务里一起重算，ON CONFLICT 覆盖，重跑幂等。
func (r *AnalyticsRepo) RollupDay(ctx context.Context, day string, from, to time.Time) error {
	return InTx(ctx, r.pool, func(tx pgx.Tx) error {
		for _, sql := range []string{metricDailySQL, metricDramaSQL, metricEpisodeSQL} {
			if _, err := tx.Exec(ctx, sql, day, from, to); err != nil {
				return fmt.Errorf("汇总 %s: %w", day, err)
			}
		}
		return nil
	})
}

// RollupHour 重算某个 UTC 小时的实时预聚合。
func (r *AnalyticsRepo) RollupHour(ctx context.Context, hour time.Time) error {
	if _, err := r.pool.Exec(ctx, metricHourlySQL, hour); err != nil {
		return fmt.Errorf("汇总小时 %s: %w", hour.Format(time.RFC3339), err)
	}
	return nil
}

// ---------- 分区维护（S4-2）----------

// EnsurePartitions 为 [from, to] 的每个 UTC 日建一个 event_raw 日分区（已存在则跳过）。
func (r *AnalyticsRepo) EnsurePartitions(ctx context.Context, from, to time.Time) error {
	for d := dayUTC(from); !d.After(dayUTC(to)); d = d.AddDate(0, 0, 1) {
		name := "event_raw_" + d.Format("20060102")
		start := d.Format("2006-01-02") + " 00:00:00+00"
		end := d.AddDate(0, 0, 1).Format("2006-01-02") + " 00:00:00+00"
		// name/start/end 均由程序按固定格式生成（非用户输入）；分区表名无法参数化，只能拼接。
		sql := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s PARTITION OF event_raw FOR VALUES FROM ('%s') TO ('%s')`, name, start, end)
		if _, err := r.pool.Exec(ctx, sql); err != nil {
			return fmt.Errorf("建事件分区 %s: %w", name, err)
		}
	}
	return nil
}

// DropPartitionsBefore 删除 cutoff（UTC 日）之前的 event_raw 分区，返回删除数量。
func (r *AnalyticsRepo) DropPartitionsBefore(ctx context.Context, cutoff time.Time) (int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid = i.inhrelid WHERE i.inhparent = 'event_raw'::regclass`)
	if err != nil {
		return 0, fmt.Errorf("列出事件分区: %w", err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return 0, err
		}
		names = append(names, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	cut := dayUTC(cutoff).Format("20060102")
	dropped := 0
	for _, n := range names {
		suffix := strings.TrimPrefix(n, "event_raw_")
		if len(suffix) != 8 || !isDigits(suffix) || suffix >= cut {
			continue
		}
		if _, err := r.pool.Exec(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, n)); err != nil {
			return dropped, fmt.Errorf("删除事件分区 %s: %w", n, err)
		}
		dropped++
	}
	return dropped, nil
}

func dayUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func isDigits(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s != ""
}

// ---------- 看板读查询（S4-3）----------

// pgDay 把北京日 00:00 的 time.Time 转成 pgtype.Date。pgx 的 Date 二进制编码取 Time 的年月日
// （按其自身时区）重建为 UTC 零点，故这里直接用北京零点即可，日界不会错位。
func pgDay(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: true} }

// ListMetricDaily 读 [from, to]（北京日闭区间）内每个有记录的全站日汇总，升序。
func (r *AnalyticsRepo) ListMetricDaily(ctx context.Context, from, to time.Time) ([]analytics.DailyPoint, error) {
	rows, err := r.q.ListMetricDaily(ctx, sqlcgen.ListMetricDailyParams{FromDay: pgDay(from), ToDay: pgDay(to)})
	if err != nil {
		return nil, fmt.Errorf("读取全站日汇总: %w", err)
	}
	out := make([]analytics.DailyPoint, len(rows))
	for i, m := range rows {
		out[i] = analytics.DailyPoint{
			Day: m.Day.Time.Format("2006-01-02"),
			Metrics: analytics.Metrics{
				UV: m.Uv, PlayAttempts: m.PlayAttempts, PlayStarts: m.PlayStarts, PlayErrors: m.PlayErrors,
				WatchSeconds: m.WatchSeconds, Registrations: m.Registrations, RegWallViews: m.RegWallViews,
				AdImpressions: m.AdImpressions, AdClicks: m.AdClicks,
			},
		}
	}
	return out, nil
}

// CountDramaDaily 返回 [from, to]（北京日闭区间）内有数据的剧总数（分页总量）。
func (r *AnalyticsRepo) CountDramaDaily(ctx context.Context, from, to time.Time) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx,
		`SELECT count(DISTINCT drama_id) FROM metric_drama_daily WHERE day >= $1::date AND day <= $2::date`,
		pgDay(from), pgDay(to)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("统计剧集分析行数: %w", err)
	}
	return n, nil
}

// dramaSortColumns 是剧集分析允许的排序列白名单（领域层已校验，这里再防一道，拼进 SQL 的列名只会取白名单值）。
var dramaSortColumns = map[string]string{
	"viewers":       "viewers",
	"play_starts":   "play_starts",
	"watch_seconds": "watch_seconds",
}

// ListDramaDaily 按剧聚合 [from, to]（北京日闭区间）的窗口数据，按 sortColumn 降序分页。
func (r *AnalyticsRepo) ListDramaDaily(ctx context.Context, from, to time.Time, sortColumn string, limit, offset int) ([]analytics.DramaRow, error) {
	col, ok := dramaSortColumns[sortColumn]
	if !ok {
		col = "viewers"
	}
	// col 只取自固定白名单（非用户输入）；其余值全部参数化。ORDER BY 列无法参数化，只能拼接。
	sql := fmt.Sprintf(`
SELECT d.id, d.title, d.status, d.latest_episode, d.created_at,
       COALESCE(sum(m.viewers), 0)::bigint       AS viewers,
       COALESCE(sum(m.play_starts), 0)::bigint   AS play_starts,
       COALESCE(sum(m.watch_seconds), 0)::bigint AS watch_seconds
FROM metric_drama_daily m
JOIN drama d ON d.id = m.drama_id
WHERE m.day >= $1::date AND m.day <= $2::date
GROUP BY d.id
ORDER BY %s DESC, d.id DESC
LIMIT $3::int OFFSET $4::int`, col)
	rows, err := r.pool.Query(ctx, sql, pgDay(from), pgDay(to), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("读取剧集分析列表: %w", err)
	}
	defer rows.Close()
	out := []analytics.DramaRow{}
	for rows.Next() {
		var d analytics.DramaRow
		var createdAt pgtype.Timestamptz
		if err := rows.Scan(&d.ID, &d.Title, &d.Status, &d.LatestEpisode, &createdAt,
			&d.Viewers, &d.PlayStarts, &d.WatchSeconds); err != nil {
			return nil, err
		}
		d.CreatedAt = createdAt.Time
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------- 单剧分析（S4-3）----------

// DramaBasic 读一部剧的基本信息（含回收站中的剧：单剧分析允许回看已删除的剧）。不存在返回 found=false。
func (r *AnalyticsRepo) DramaBasic(ctx context.Context, id int64) (analytics.DramaBasicRow, bool, error) {
	var d analytics.DramaBasicRow
	var createdAt pgtype.Timestamptz
	err := r.pool.QueryRow(ctx,
		`SELECT id, title, status, latest_episode, created_at FROM drama WHERE id = $1`, id).
		Scan(&d.ID, &d.Title, &d.Status, &d.LatestEpisode, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return analytics.DramaBasicRow{}, false, nil
	}
	if err != nil {
		return analytics.DramaBasicRow{}, false, fmt.Errorf("读取剧基本信息: %w", err)
	}
	d.CreatedAt = createdAt.Time
	return d, true, nil
}

// ListDramaDailyByID 读某剧 [from, to]（北京日闭区间）的每日观看规模，升序。
func (r *AnalyticsRepo) ListDramaDailyByID(ctx context.Context, id int64, from, to time.Time) ([]analytics.DramaDayMetric, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT day, viewers, play_starts, watch_seconds FROM metric_drama_daily
		 WHERE drama_id = $1 AND day >= $2::date AND day <= $3::date ORDER BY day`,
		id, pgDay(from), pgDay(to))
	if err != nil {
		return nil, fmt.Errorf("读取单剧每日汇总: %w", err)
	}
	defer rows.Close()
	out := []analytics.DramaDayMetric{}
	for rows.Next() {
		var day pgtype.Date
		var m analytics.DramaDayMetric
		if err := rows.Scan(&day, &m.Viewers, &m.PlayStarts, &m.WatchSeconds); err != nil {
			return nil, err
		}
		m.Day = day.Time.Format("2006-01-02")
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListEpisodeRetention 读某剧全部分集在 [from, to]（北京日闭区间）内的聚合，按集号升序。
// LEFT JOIN：窗口内没有观看数据的集也要出现在留存曲线里（viewers=0），曲线才完整；
// 日期范围放在 ON 子句，否则 WHERE 会把无数据的集整行滤掉。
func (r *AnalyticsRepo) ListEpisodeRetention(ctx context.Context, id int64, from, to time.Time) ([]analytics.EpisodeRetention, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT e.id, e.number, e.title,
		        COALESCE(sum(m.viewers), 0)::bigint       AS viewers,
		        COALESCE(sum(m.play_starts), 0)::bigint   AS play_starts,
		        COALESCE(sum(m.watch_seconds), 0)::bigint AS watch_seconds
		 FROM episode e
		 LEFT JOIN metric_episode_daily m
		   ON m.episode_id = e.id AND m.day >= $2::date AND m.day <= $3::date
		 WHERE e.drama_id = $1
		 GROUP BY e.id, e.number, e.title
		 ORDER BY e.number`,
		id, pgDay(from), pgDay(to))
	if err != nil {
		return nil, fmt.Errorf("读取单剧集留存: %w", err)
	}
	defer rows.Close()
	out := []analytics.EpisodeRetention{}
	for rows.Next() {
		var e analytics.EpisodeRetention
		if err := rows.Scan(&e.EpisodeID, &e.Number, &e.Title, &e.Viewers, &e.PlayStarts, &e.WatchSeconds); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
