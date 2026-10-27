package analytics

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// 本文件是看板与剧集分析的「读」侧（S4-3，docs/03 第 8 节、docs/02 第 6.3 节）。
// 只读 S4-2 的预聚合表（metric_daily / metric_drama_daily），不扫 event_raw。
// 口径：日表按北京时间日界定稿，看板窗口一律「到昨天」为止——今天是 partial，计入会让环比失真。

// 看板读查询的边界。
const (
	defaultDays     = 7
	maxDays         = 90
	defaultPageSize = 20
	maxPageSize     = 100
)

// sortColumns 是剧集分析允许的排序维度白名单（键为对外取值，值为 metric_drama_daily 聚合列）。
// 排序列不能参数化，由 store 层按本白名单映射成固定列名，杜绝注入。
var sortColumns = map[string]string{
	"viewers":       "viewers",
	"play_starts":   "play_starts",
	"watch_seconds": "watch_seconds",
}

const defaultSort = "viewers"

// Metrics 是全站计数类指标的一组值（看板窗口汇总、环比对比段、每日序列共用）。
type Metrics struct {
	UV            int64 `json:"uv"`             // 去重设备数
	PlayAttempts  int64 `json:"play_attempts"`  // 播放尝试
	PlayStarts    int64 `json:"play_starts"`    // 起播成功
	PlayErrors    int64 `json:"play_errors"`    // 播放失败
	WatchSeconds  int64 `json:"watch_seconds"`  // 有效观看秒数之和
	Registrations int64 `json:"registrations"`  // 注册成功
	RegWallViews  int64 `json:"reg_wall_views"` // 注册墙曝光
	AdImpressions int64 `json:"ad_impressions"` // 广告曝光
	AdClicks      int64 `json:"ad_clicks"`      // 广告点击
}

func (m *Metrics) add(o Metrics) {
	m.UV += o.UV
	m.PlayAttempts += o.PlayAttempts
	m.PlayStarts += o.PlayStarts
	m.PlayErrors += o.PlayErrors
	m.WatchSeconds += o.WatchSeconds
	m.Registrations += o.Registrations
	m.RegWallViews += o.RegWallViews
	m.AdImpressions += o.AdImpressions
	m.AdClicks += o.AdClicks
}

// DailyPoint 是某一北京日的全站指标，既是 store 读出的一行，也是看板每日序列的一项。
type DailyPoint struct {
	Day string `json:"day"` // YYYY-MM-DD（北京日）
	Metrics
}

// Overview 是数据看板概览：窗口汇总 + 上一等长窗口对比段（环比由前端按 totals/prev 计算）+ 每日序列（零填充）。
type Overview struct {
	From   string       `json:"from"` // 窗口起始北京日（含）
	To     string       `json:"to"`   // 窗口结束北京日（含，为昨天）
	Days   int          `json:"days"`
	Totals Metrics      `json:"totals"`
	Prev   Metrics      `json:"prev"` // 上一等长窗口的汇总，供环比
	Daily  []DailyPoint `json:"daily"`
}

// DramaRow 是 store 聚合出的某剧窗口数据（未判定新剧）。
type DramaRow struct {
	ID            int64
	Title         string
	Status        string
	LatestEpisode int32
	CreatedAt     time.Time
	Viewers       int64
	PlayStarts    int64
	WatchSeconds  int64
}

// DramaStat 是剧集分析列表的一行。
type DramaStat struct {
	ID            int64     `json:"id"`
	Title         string    `json:"title"`
	Status        string    `json:"status"`
	LatestEpisode int32     `json:"latest_episode"`
	CreatedAt     time.Time `json:"created_at"`
	Viewers       int64     `json:"viewers"`
	PlayStarts    int64     `json:"play_starts"`
	WatchSeconds  int64     `json:"watch_seconds"`
	IsNew         bool      `json:"is_new"` // 创建于本窗口内（北京日）
}

// DramasInput 是剧集分析列表的入参。
type DramasInput struct {
	Days     int
	Sort     string // viewers / play_starts / watch_seconds，其它值归默认
	Page     int
	PageSize int
}

// DramasResult 是剧集分析列表的结果（页码分页）。
type DramasResult struct {
	From     string      `json:"from"`
	To       string      `json:"to"`
	Days     int         `json:"days"`
	Sort     string      `json:"sort"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
	List     []DramaStat `json:"list"`
}

// QueryRepo 读预聚合表（store 实现）。日期参数为北京日的 00:00（store 只取其 Y/M/D）。
type QueryRepo interface {
	// ListMetricDaily 读 [from, to] 内每个北京日的全站日汇总（升序，可能缺日）。
	ListMetricDaily(ctx context.Context, from, to time.Time) ([]DailyPoint, error)
	// CountDramaDaily 返回 [from, to] 内有数据的剧的总数（分页用）。
	CountDramaDaily(ctx context.Context, from, to time.Time) (int64, error)
	// ListDramaDaily 按 sortColumn 降序分页返回 [from, to] 内按剧聚合的窗口数据。
	ListDramaDaily(ctx context.Context, from, to time.Time, sortColumn string, limit, offset int) ([]DramaRow, error)
	// DramaBasic 读一部剧的基本信息（不存在返回 found=false）。
	DramaBasic(ctx context.Context, id int64) (DramaBasicRow, bool, error)
	// ListDramaDailyByID 读某剧 [from, to] 的每日观看规模（升序，可能缺日）。
	ListDramaDailyByID(ctx context.Context, id int64, from, to time.Time) ([]DramaDayMetric, error)
	// ListEpisodeRetention 读某剧全部分集在 [from, to] 的聚合（按集号升序，窗口内无数据的集 viewers=0）。
	ListEpisodeRetention(ctx context.Context, id int64, from, to time.Time) ([]EpisodeRetention, error)
}

// QueryService 是看板与剧集分析的读服务（API 进程用）。
type QueryService struct {
	repo QueryRepo
	loc  *time.Location
	now  func() time.Time
}

// NewQueryService 构造读服务。北京时区解析失败（容器缺 tzdata）时退回固定 +8。
func NewQueryService(repo QueryRepo) *QueryService {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return &QueryService{repo: repo, loc: loc, now: time.Now}
}

// window 计算「到昨天」的当前窗口与上一等长窗口（均为北京日 00:00，闭区间）。
func (s *QueryService) window(days int) (curFrom, curTo, prevFrom, prevTo time.Time) {
	days = clampDays(days)
	now := s.now().In(s.loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	curTo = today.AddDate(0, 0, -1) // 昨天
	curFrom = curTo.AddDate(0, 0, -(days - 1))
	prevTo = curFrom.AddDate(0, 0, -1)
	prevFrom = prevTo.AddDate(0, 0, -(days - 1))
	return
}

// Overview 汇总数据看板概览（最近 days 天，到昨天）。
func (s *QueryService) Overview(ctx context.Context, days int) (Overview, error) {
	days = clampDays(days)
	curFrom, curTo, prevFrom, _ := s.window(days)

	// 一次取「上一窗口起 ~ 当前窗口止」的全部日行，再按日拆分，省一次往返。
	rows, err := s.repo.ListMetricDaily(ctx, prevFrom, curTo)
	if err != nil {
		return Overview{}, err
	}
	byDay := make(map[string]Metrics, len(rows))
	for _, r := range rows {
		byDay[r.Day] = r.Metrics
	}

	ov := Overview{
		From:  curFrom.Format(dateFmt),
		To:    curTo.Format(dateFmt),
		Days:  days,
		Daily: make([]DailyPoint, 0, days),
	}
	// 当前窗口：逐日零填充 + 汇总。
	for d := curFrom; !d.After(curTo); d = d.AddDate(0, 0, 1) {
		key := d.Format(dateFmt)
		m := byDay[key] // 缺日为零值
		ov.Daily = append(ov.Daily, DailyPoint{Day: key, Metrics: m})
		ov.Totals.add(m)
	}
	// 上一窗口：只汇总（prevFrom ~ prevTo = curFrom 的前 days 天）。
	for d := prevFrom; d.Before(curFrom); d = d.AddDate(0, 0, 1) {
		ov.Prev.add(byDay[d.Format(dateFmt)])
	}
	return ov, nil
}

// Dramas 返回剧集分析列表（窗口聚合 + 新剧标记 + 排序分页）。
func (s *QueryService) Dramas(ctx context.Context, in DramasInput) (DramasResult, error) {
	days := clampDays(in.Days)
	sort := in.Sort
	col, ok := sortColumns[sort]
	if !ok {
		sort, col = defaultSort, sortColumns[defaultSort]
	}
	page, size := clampPage(in.Page, in.PageSize)
	curFrom, curTo, _, _ := s.window(days)

	total, err := s.repo.CountDramaDaily(ctx, curFrom, curTo)
	if err != nil {
		return DramasResult{}, err
	}
	rows, err := s.repo.ListDramaDaily(ctx, curFrom, curTo, col, size, (page-1)*size)
	if err != nil {
		return DramasResult{}, err
	}
	list := make([]DramaStat, 0, len(rows))
	for _, r := range rows {
		list = append(list, DramaStat{
			ID: r.ID, Title: r.Title, Status: r.Status, LatestEpisode: r.LatestEpisode,
			CreatedAt: r.CreatedAt, Viewers: r.Viewers, PlayStarts: r.PlayStarts, WatchSeconds: r.WatchSeconds,
			IsNew: !r.CreatedAt.Before(curFrom), // 创建于窗口起始北京日 00:00 之后
		})
	}
	return DramasResult{
		From: curFrom.Format(dateFmt), To: curTo.Format(dateFmt), Days: days, Sort: sort,
		Total: total, Page: page, PageSize: size, List: list,
	}, nil
}

const dateFmt = "2006-01-02"

func clampDays(days int) int {
	if days < 1 {
		return defaultDays
	}
	if days > maxDays {
		return maxDays
	}
	return days
}

func clampPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > maxPageSize {
		size = defaultPageSize
	}
	return page, size
}

// ---------- 单剧分析（S4-3）----------

// DramaMetrics 是单剧的一组观看规模指标（窗口汇总与每日序列共用）。
type DramaMetrics struct {
	Viewers      int64 `json:"viewers"`
	PlayStarts   int64 `json:"play_starts"`
	WatchSeconds int64 `json:"watch_seconds"`
}

func (m *DramaMetrics) add(o DramaMetrics) {
	m.Viewers += o.Viewers
	m.PlayStarts += o.PlayStarts
	m.WatchSeconds += o.WatchSeconds
}

// DramaDayMetric 是单剧某北京日的指标（每日序列的一项）。
type DramaDayMetric struct {
	Day string `json:"day"`
	DramaMetrics
}

// EpisodeRetention 是集间留存曲线的一项（按集号升序）。
// retention_pct 相对第 1 集：第 1 集记 100，第 n 集 = 窗口内该集 viewers / 第 1 集 viewers × 100（保留 1 位小数）。
// 口径：metric_episode_daily.viewers 是「按天按集去重设备数」，窗口内跨天累加为观看规模近似、非严格去重留存；
// 严格去重留存与流失拐点（drop_peak_sec / drop_episode）待埋点 SDK / 专门留存表（W4-1）。
type EpisodeRetention struct {
	EpisodeID    int64   `json:"episode_id"`
	Number       int32   `json:"number"`
	Title        string  `json:"title"`
	Viewers      int64   `json:"viewers"`
	PlayStarts   int64   `json:"play_starts"`
	WatchSeconds int64   `json:"watch_seconds"`
	RetentionPct float64 `json:"retention_pct"`
}

// DramaBasicRow 是 store 读出的剧基本信息。
type DramaBasicRow struct {
	ID            int64
	Title         string
	Status        string
	LatestEpisode int32
	CreatedAt     time.Time
}

// DramaDetail 是单剧分析：基本信息 + 窗口汇总 + 上一等长窗口（环比）+ 每日序列 + 集间留存曲线。
// 追剧 / 评论 / 举报数、来源、端占比、QoS、与全站均值对比待后续接入（依赖埋点 SDK 或跨域表）。
type DramaDetail struct {
	ID            int64              `json:"id"`
	Title         string             `json:"title"`
	Status        string             `json:"status"`
	LatestEpisode int32              `json:"latest_episode"`
	CreatedAt     time.Time          `json:"created_at"`
	OnlineDays    int                `json:"online_days"` // 从创建日到今天的天数（北京日，含当天，至少 1）
	From          string             `json:"from"`
	To            string             `json:"to"`
	Days          int                `json:"days"`
	Totals        DramaMetrics       `json:"totals"`
	Prev          DramaMetrics       `json:"prev"`
	Daily         []DramaDayMetric   `json:"daily"`
	Episodes      []EpisodeRetention `json:"episodes"`
}

// DramaDetail 组装单剧分析（最近 days 天，到昨天）。剧不存在返回 found=false。
func (s *QueryService) DramaDetail(ctx context.Context, id int64, days int) (DramaDetail, bool, error) {
	days = clampDays(days)
	curFrom, curTo, prevFrom, _ := s.window(days)

	basic, found, err := s.repo.DramaBasic(ctx, id)
	if err != nil {
		return DramaDetail{}, false, err
	}
	if !found {
		return DramaDetail{}, false, nil
	}

	// 每日序列：一把取 [prevFrom, curTo]，再拆当前 / 上一窗口（与 Overview 同法）。
	daily, err := s.repo.ListDramaDailyByID(ctx, id, prevFrom, curTo)
	if err != nil {
		return DramaDetail{}, false, err
	}
	byDay := make(map[string]DramaMetrics, len(daily))
	for _, d := range daily {
		byDay[d.Day] = d.DramaMetrics
	}

	now := s.now().In(s.loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	detail := DramaDetail{
		ID: basic.ID, Title: basic.Title, Status: basic.Status, LatestEpisode: basic.LatestEpisode,
		CreatedAt: basic.CreatedAt, OnlineDays: onlineDays(basic.CreatedAt, today, s.loc),
		From: curFrom.Format(dateFmt), To: curTo.Format(dateFmt), Days: days,
		Daily: make([]DramaDayMetric, 0, days),
	}
	for d := curFrom; !d.After(curTo); d = d.AddDate(0, 0, 1) {
		key := d.Format(dateFmt)
		m := byDay[key]
		detail.Daily = append(detail.Daily, DramaDayMetric{Day: key, DramaMetrics: m})
		detail.Totals.add(m)
	}
	for d := prevFrom; d.Before(curFrom); d = d.AddDate(0, 0, 1) {
		detail.Prev.add(byDay[d.Format(dateFmt)])
	}

	// 集间留存曲线：窗口内按集聚合，以第 1 集 viewers 为基数算百分比。
	eps, err := s.repo.ListEpisodeRetention(ctx, id, curFrom, curTo)
	if err != nil {
		return DramaDetail{}, false, err
	}
	if len(eps) > 0 && eps[0].Viewers > 0 {
		base := float64(eps[0].Viewers)
		for i := range eps {
			eps[i].RetentionPct = math.Round(float64(eps[i].Viewers)/base*1000) / 10
		}
	}
	detail.Episodes = eps
	return detail, true, nil
}

// onlineDays 返回从 created（转北京日）到 today（北京日 00:00）的天数，含当天，至少 1。
func onlineDays(created, today time.Time, loc *time.Location) int {
	c := created.In(loc)
	start := time.Date(c.Year(), c.Month(), c.Day(), 0, 0, 0, 0, loc)
	d := int(today.Sub(start).Hours()/24) + 1
	if d < 1 {
		return 1
	}
	return d
}

// ---------- 导出给 Agent 的数据包（S4-3，docs/06 第 1 节「复制数据给 Agent」）----------

// exportTopN 导出时附带的热门剧条数。
const exportTopN = 10

// ExportPackage 是打包给 AI 的数据简报：一段中文 Markdown（提示词 + 数据 + 口径），运营复制后粘贴给大模型。
type ExportPackage struct {
	Days        int       `json:"days"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	GeneratedAt time.Time `json:"generated_at"`
	Content     string    `json:"content"`
}

// Export 组装某窗口的数据简报（复用看板概览与剧集 Top N）。
func (s *QueryService) Export(ctx context.Context, days int) (ExportPackage, error) {
	days = clampDays(days)
	ov, err := s.Overview(ctx, days)
	if err != nil {
		return ExportPackage{}, err
	}
	dr, err := s.Dramas(ctx, DramasInput{Days: days, Sort: defaultSort, Page: 1, PageSize: exportTopN})
	if err != nil {
		return ExportPackage{}, err
	}
	return ExportPackage{Days: days, From: ov.From, To: ov.To, GeneratedAt: s.now(), Content: buildExport(ov, dr)}, nil
}

// buildExport 把概览与剧集数据拼成给 AI 的中文 Markdown（提示词在前、口径在后）。
func buildExport(ov Overview, dr DramasResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 运营数据简报（%s ~ %s，北京时间到昨天）\n\n", ov.From, ov.To)
	b.WriteString("你是资深运营数据分析师。请只依据下面的数据，用中文写一份给管理层的简报：先给 3–5 条关键结论，" +
		"再指出风险与改进建议，最后用一句话总结。不要编造数据，拿不准的地方说明「数据不足」。口径见末尾。\n\n")

	b.WriteString("## 全站概览（本期 vs 上一等长窗口）\n\n")
	b.WriteString("| 指标 | 本期 | 上期 | 环比 |\n| --- | ---: | ---: | ---: |\n")
	for _, r := range []struct {
		name      string
		cur, prev int64
	}{
		{"独立访客 UV", ov.Totals.UV, ov.Prev.UV},
		{"播放尝试", ov.Totals.PlayAttempts, ov.Prev.PlayAttempts},
		{"起播次数", ov.Totals.PlayStarts, ov.Prev.PlayStarts},
		{"播放失败", ov.Totals.PlayErrors, ov.Prev.PlayErrors},
		{"注册数", ov.Totals.Registrations, ov.Prev.Registrations},
		{"注册墙曝光", ov.Totals.RegWallViews, ov.Prev.RegWallViews},
		{"广告曝光", ov.Totals.AdImpressions, ov.Prev.AdImpressions},
		{"广告点击", ov.Totals.AdClicks, ov.Prev.AdClicks},
	} {
		fmt.Fprintf(&b, "| %s | %d | %d | %s |\n", r.name, r.cur, r.prev, deltaStr(r.cur, r.prev))
	}
	fmt.Fprintf(&b, "| 有效观看时长 | %s | %s | %s |\n\n", durHours(ov.Totals.WatchSeconds), durHours(ov.Prev.WatchSeconds), deltaStr(ov.Totals.WatchSeconds, ov.Prev.WatchSeconds))
	fmt.Fprintf(&b, "派生：起播成功率 %s（起播 / 尝试）、注册墙转化 %s（注册 / 曝光）、广告点击率 %s（点击 / 曝光）。\n\n",
		rateStr(ov.Totals.PlayStarts, ov.Totals.PlayAttempts), rateStr(ov.Totals.Registrations, ov.Totals.RegWallViews), rateStr(ov.Totals.AdClicks, ov.Totals.AdImpressions))

	b.WriteString("## 每日趋势\n\n| 日期 | UV | 起播 | 注册 | 有效观看 |\n| --- | ---: | ---: | ---: | ---: |\n")
	for _, d := range ov.Daily {
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %s |\n", d.Day, d.UV, d.PlayStarts, d.Registrations, durHours(d.WatchSeconds))
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## 热门剧集 Top %d（按观看人次）\n\n", len(dr.List))
	if len(dr.List) == 0 {
		b.WriteString("（窗口内暂无观看数据）\n\n")
	} else {
		b.WriteString("| 剧名 | 观看人次 | 起播 | 有效观看 | 最新集 | 新剧 |\n| --- | ---: | ---: | ---: | ---: | :---: |\n")
		for _, d := range dr.List {
			isNew := ""
			if d.IsNew {
				isNew = "是"
			}
			fmt.Fprintf(&b, "| %s | %d | %d | %s | %d | %s |\n", mdCell(d.Title), d.Viewers, d.PlayStarts, durHours(d.WatchSeconds), d.LatestEpisode, isNew)
		}
		b.WriteString("\n")
	}

	b.WriteString("## 口径\n\n")
	b.WriteString("- 窗口为到昨天为止的整天（北京时间），环比对比紧邻的上一等长窗口；今天未定稿不计入。\n")
	b.WriteString("- 有效观看时长为 watch 事件上报秒数之和；UV 为去重设备数。\n")
	b.WriteString("- 当前仅含服务端可观测指标；留存、首帧 / 卡顿 QoS、来源渠道、会员收益与福利等尚未纳入，待前台埋点 SDK 接入后补齐。\n")
	return b.String()
}

// deltaStr 环比文案：上期为 0 时按「新增」/「—」处理。
func deltaStr(cur, prev int64) string {
	if prev == 0 {
		if cur == 0 {
			return "—"
		}
		return "新增"
	}
	return fmt.Sprintf("%+.1f%%", float64(cur-prev)/float64(prev)*100)
}

// rateStr 比率百分比；分母 ≤ 0 返回「—」。
func rateStr(num, den int64) string {
	if den <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", float64(num)/float64(den)*100)
}

// durHours 秒 → 「x.x 小时」。
func durHours(sec int64) string {
	if sec <= 0 {
		return "0"
	}
	return fmt.Sprintf("%.1f 小时", float64(sec)/3600)
}

// mdCell 转义表格单元格里的竖线，避免剧名里的 | 破坏 Markdown 表格。
func mdCell(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
