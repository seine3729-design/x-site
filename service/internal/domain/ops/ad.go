package ops

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
	"xsite/service/internal/htmlsan"
)

// AdSlot 是广告位置（docs/06 第 18 节）；各位置的建议尺寸写在后台页面上。
type AdSlot string

const (
	SlotGlobalPopup   AdSlot = "global_popup"   // 全站弹窗
	SlotHomeTop       AdSlot = "home_top"       // 首页顶部横幅
	SlotHomeMid       AdSlot = "home_mid"       // 首页中部横幅
	SlotHomeBottom    AdSlot = "home_bottom"    // 首页底部横幅
	SlotFooterBanner  AdSlot = "footer_banner"  // 页脚横幅
	SlotListFeed      AdSlot = "list_feed"      // 列表信息流
	SlotDetailBanner  AdSlot = "detail_banner"  // 详情页横幅
	SlotPreroll       AdSlot = "preroll"        // 播放前贴片
	SlotPause         AdSlot = "pause"          // 暂停贴片
	SlotImmersiveFeed AdSlot = "immersive_feed" // 沉浸流插入
	SlotTextLink      AdSlot = "text_link"      // 文字链
	SlotAppDownload   AdSlot = "app_download"   // App 下载浮窗
)

// AdSlots 是全部广告位置。
var AdSlots = []AdSlot{
	SlotGlobalPopup, SlotHomeTop, SlotHomeMid, SlotHomeBottom, SlotFooterBanner, SlotListFeed,
	SlotDetailBanner, SlotPreroll, SlotPause, SlotImmersiveFeed, SlotTextLink, SlotAppDownload,
}

// CreativeType 是素材类型。
type CreativeType string

const (
	CreativeImage CreativeType = "image"
	CreativeVideo CreativeType = "video"
	CreativeText  CreativeType = "text"
	CreativeHTML  CreativeType = "html" // 自定义 HTML：要 ads.custom_html 权限，保存时清洗，前台只在沙箱 iframe 中渲染
)

// AdDevice 是投放的端（投给谁）。
type AdDevice string

const (
	AdDeviceAll     AdDevice = "all"
	AdDeviceMobile  AdDevice = "mobile"
	AdDeviceDesktop AdDevice = "desktop"
)

const (
	defaultAdWeight = 10
	maxAdWeight     = 100
	maxFreqCap      = 1000
	maxAdsPerSlot   = 5 // 前台每个位置一次最多取几条
	maxAdNameRunes  = 50
	maxAdTextRunes  = 200
	maxAdAltRunes   = 100
	maxAdHTMLRunes  = 10000 // 提交的 HTML 上限
	maxAdHTMLStored = 20000 // 清洗后（会给链接加 rel / target）的上限，与表约束一致
	maxSlotsParam   = 300   // 前台 slots 参数的长度上限
)

// Ad 是一条广告。EndAt 为空表示一直投放；ImpCount / ClickCount 由 worker 每分钟从 Valkey 落库。
type Ad struct {
	ID           int64
	Name         string
	Slot         AdSlot
	CreativeType CreativeType
	Image        string // 图片素材；视频素材的封面
	VideoURL     string
	Text         string
	HTML         string // 清洗后的 HTML
	Alt          string
	ClickURL     string
	Device       AdDevice
	Locales      []string
	Weight       int
	EndAt        *time.Time
	FreqCap      int // 每设备每天曝光上限，0 表示不限
	Status       Status
	ImpCount     int64
	ClickCount   int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// DisplayStatus 返回 now 时刻的投放状态：投放中、已结束或已停用（广告没有开始时间）。
func (a Ad) DisplayStatus(now time.Time) DisplayStatus {
	switch {
	case a.Status != StatusActive:
		return DisplayInactive
	case a.EndAt != nil && !now.Before(*a.EndAt):
		return DisplayEnded
	}
	return DisplayLive
}

// servesDevice 表示广告是否投给该端；前台没带端时只投「都投」的。
func (a Ad) servesDevice(device AdDevice) bool {
	return a.Device == AdDeviceAll || (device != "" && a.Device == device)
}

func (a Ad) public() PublicAd {
	return PublicAd{
		ID: a.ID, Slot: a.Slot, CreativeType: a.CreativeType, Image: a.Image, VideoURL: a.VideoURL, Text: a.Text,
		HTML: a.HTML, Alt: a.Alt, ClickURL: a.ClickURL,
	}
}

// AdInput 是广告的新建与整体保存。
type AdInput struct {
	Name         string
	Slot         AdSlot
	CreativeType CreativeType
	Image        string
	VideoURL     string
	Text         string
	HTML         string
	Alt          string
	ClickURL     string
	Device       AdDevice
	Locales      []string
	Weight       int // 0 表示取默认值
	EndAt        *time.Time
	FreqCap      int
	Status       Status
}

// AdFilter 是后台列表的筛选：Q 匹配广告名或跳转网址。
type AdFilter struct {
	Q      string
	Slot   AdSlot
	Status Status
	PageRequest
}

// AdQuery 是校验后交给仓储的查询。Pattern 是已转义的 LIKE 模式，空表示不按文本搜。
type AdQuery struct {
	Pattern string
	Slot    AdSlot
	Status  Status
	Limit   int
	Offset  int
}

// PublicAd 是下发给前台的广告（不含广告名、权重、频控、计数等后台字段）。
type PublicAd struct {
	ID           int64        `json:"id"`
	Slot         AdSlot       `json:"slot"`
	CreativeType CreativeType `json:"creative_type"`
	Image        string       `json:"image"`
	VideoURL     string       `json:"video_url"`
	Text         string       `json:"text"`
	HTML         string       `json:"html"`
	Alt          string       `json:"alt"`
	ClickURL     string       `json:"click_url"`
}

// AdEventKind 是前台上报的广告事件。
type AdEventKind string

const (
	AdImpression AdEventKind = "impression"
	AdClick      AdEventKind = "click"
)

// AdCounts 是一条广告待落库的曝光、点击增量。
type AdCounts struct {
	Impressions int64
	Clicks      int64
}

// AdRepo 读写广告。查不到时返回 errs.ErrNotFound。
type AdRepo interface {
	ListAds(ctx context.Context, q AdQuery) ([]Ad, int64, error)
	GetAd(ctx context.Context, id int64) (Ad, error)
	CreateAd(ctx context.Context, in AdInput) (Ad, error)
	UpdateAd(ctx context.Context, id int64, in AdInput) (Ad, error)
	// DeleteAd 删除广告（不进回收站），返回删除前的记录。
	DeleteAd(ctx context.Context, id int64) (Ad, error)
	// ListAdCandidates 返回投放中、未结束的广告（新的在前）。
	ListAdCandidates(ctx context.Context) ([]Ad, error)
	// AddAdCounts 把增量加到曝光、点击累计上；已删除的广告跳过。
	AddAdCounts(ctx context.Context, counts map[int64]AdCounts) error
}

// AdStats 是 Valkey 里的事件去重、待落库计数与频控计数（实现见 store/cache.AdStats）。
type AdStats interface {
	// FirstEvent 报告这是不是去重窗口（1 秒）内该设备对该广告的第一次同类事件。
	FirstEvent(ctx context.Context, device string, adID int64, kind AdEventKind) (bool, error)
	// AddEvent 把事件计入待落库的累计；capDay 非空时（有频控的广告的曝光）同时计入设备在该日（北京日期）的曝光次数。
	AddEvent(ctx context.Context, adID int64, kind AdEventKind, device, capDay string) error
	// DailyImpressions 返回设备在 day 对 ids 中各广告的曝光次数；没有记录的不在结果中。
	DailyImpressions(ctx context.Context, device, day string, ids []int64) (map[int64]int64, error)
	// TakePending 把待落库的累计原子地移到 token 名下并返回；token 名下已有数据（上次落库失败）时直接返回那一份。
	TakePending(ctx context.Context, token string) (map[int64]AdCounts, error)
	// DonePending 删除 token 名下已落库的累计。
	DonePending(ctx context.Context, token string) error
}

// ServeRequest 是前台拉广告的参数（GET /api/v1/ads）。
type ServeRequest struct {
	Slots     string // 逗号分隔的位置
	N         int    // 每个位置最多几条，0 表示 1
	Device    string // mobile / desktop；空表示只投「都投」的广告
	Locale    string
	DeviceKey string // 频控用的设备标识（HTTP 层已做摘要）；空表示不做频控
}

// AdEvent 是一次曝光或点击上报（POST /api/v1/ads/event）。
type AdEvent struct {
	AdID      int64
	Kind      AdEventKind
	DeviceKey string // 去重与频控用的设备标识；空表示不去重
}

// 前台候选广告的缓存：后台增删改后立即删除，TTL 只是兜底；位置、端、语言、频控在每次请求时判断。
const (
	adCacheKey = "ops:ads"
	adCacheTTL = time.Minute
)

// beijing 是频控「按天」的时区（统计口径按北京时间，docs/03 第 11 节）。用固定时区，不依赖运行环境的时区数据。
var beijing = time.FixedZone("CST", 8*60*60)

func beijingDay(t time.Time) string { return t.In(beijing).Format("20060102") }

// ErrCustomHTMLForbidden 是没有 ads.custom_html 权限时新建或修改 HTML 素材的错误。
var ErrCustomHTMLForbidden = errs.ErrForbidden.WithMessage("自定义 HTML 素材需要「自定义 HTML」权限（默认只有超级管理员）")

// 自定义 HTML 素材的白名单与静态页正文、区块的自定义 HTML 共用一套策略（internal/htmlsan，docs/03 第 7.1 节）。

// AdService 是广告的后台管理、前台投放与事件计数。
type AdService struct {
	repo  AdRepo
	stats AdStats
	cache Cache
	now   func() time.Time
	intN  func(n int) int // [0, n) 的随机数（加权随机用），测试中替换
}

// NewAdService 创建广告服务。
func NewAdService(repo AdRepo, stats AdStats, cache Cache) *AdService {
	return &AdService{repo: repo, stats: stats, cache: cache, now: time.Now, intN: rand.IntN}
}

// List 返回后台广告列表的一页（按编号倒序）。
func (s *AdService) List(ctx context.Context, f AdFilter) (Page[Ad], error) {
	q, err := cleanQuery(f.Q)
	if err != nil {
		return Page[Ad]{}, err
	}
	if f.Slot != "" && !slices.Contains(AdSlots, f.Slot) {
		return Page[Ad]{}, badRequest("位置筛选无效")
	}
	if f.Status != "" && f.Status != StatusActive && f.Status != StatusInactive {
		return Page[Ad]{}, badRequest("状态筛选无效")
	}
	p, limit, offset, err := limitOffset(f.PageRequest)
	if err != nil {
		return Page[Ad]{}, err
	}
	list, total, err := s.repo.ListAds(ctx, AdQuery{Pattern: likePattern(q), Slot: f.Slot, Status: f.Status, Limit: limit, Offset: offset})
	if err != nil {
		return Page[Ad]{}, err
	}
	return Page[Ad]{List: list, Total: total, Page: p.Page, PageSize: p.PageSize}, nil
}

// Create 新建广告，前台立即生效。customHTML 表示操作人有 ads.custom_html 权限。
func (s *AdService) Create(ctx context.Context, in AdInput, customHTML bool) (Ad, error) {
	if in.CreativeType == CreativeHTML && !customHTML {
		return Ad{}, ErrCustomHTMLForbidden
	}
	in, err := cleanAd(in)
	if err != nil {
		return Ad{}, err
	}
	a, err := s.repo.CreateAd(ctx, in)
	if err == nil {
		s.invalidate(ctx)
	}
	return a, err
}

// Update 整体保存广告，返回修改前后的记录。改成 HTML 素材、或修改原本是 HTML 素材的广告都要 ads.custom_html 权限。
func (s *AdService) Update(ctx context.Context, id int64, in AdInput, customHTML bool) (before, after Ad, err error) {
	if in.CreativeType == CreativeHTML && !customHTML {
		return Ad{}, Ad{}, ErrCustomHTMLForbidden
	}
	if in, err = cleanAd(in); err != nil {
		return
	}
	if before, err = s.repo.GetAd(ctx, id); err != nil {
		return
	}
	if before.CreativeType == CreativeHTML && !customHTML {
		return Ad{}, Ad{}, ErrCustomHTMLForbidden
	}
	if after, err = s.repo.UpdateAd(ctx, id, in); err == nil {
		s.invalidate(ctx)
	}
	return
}

// Delete 删除广告（不可恢复），返回删除前的记录。
func (s *AdService) Delete(ctx context.Context, id int64) (Ad, error) {
	a, err := s.repo.DeleteAd(ctx, id)
	if err == nil {
		s.invalidate(ctx)
	}
	return a, err
}

// Serve 按位置投放广告：只取投放中、未结束、端与语言匹配、当天曝光未到频控上限的，每个位置按权重不放回地随机取 n 条。
// 结果按请求里位置的顺序排列；没有广告时返回空列表。
func (s *AdService) Serve(ctx context.Context, req ServeRequest) ([]PublicAd, error) {
	slots, err := parseSlots(req.Slots)
	if err != nil {
		return nil, err
	}
	n := req.N
	if n == 0 {
		n = 1
	}
	if n < 1 || n > maxAdsPerSlot {
		return nil, badRequest("n 为每个位置的条数，1–%d", maxAdsPerSlot)
	}
	device := AdDevice(req.Device)
	if device != "" && device != AdDeviceMobile && device != AdDeviceDesktop {
		return nil, badRequest("device 只能是 mobile 或 desktop")
	}
	locale := NormalizeLocale(req.Locale)
	candidates, err := s.candidates(ctx)
	if err != nil {
		return nil, err
	}

	now := s.now()
	bySlot := make(map[AdSlot][]Ad, len(slots))
	var capped []int64
	for _, a := range candidates {
		// 候选在缓存里最多停留一分钟，状态与结束时间每次重新判断
		if a.DisplayStatus(now) != DisplayLive || !slices.Contains(slots, a.Slot) || !a.servesDevice(device) || !targets(a.Locales, locale) {
			continue
		}
		bySlot[a.Slot] = append(bySlot[a.Slot], a)
		if a.FreqCap > 0 {
			capped = append(capped, a.ID)
		}
	}
	seen := s.dailyImpressions(ctx, req.DeviceKey, now, capped)

	out := make([]PublicAd, 0, len(slots))
	for _, slot := range slots {
		pool := slices.DeleteFunc(bySlot[slot], func(a Ad) bool { return a.FreqCap > 0 && seen[a.ID] >= int64(a.FreqCap) })
		for _, a := range s.pick(pool, n) {
			out = append(out, a.public())
		}
	}
	return out, nil
}

// Record 记一次曝光或点击：同一设备、同一广告、同一种事件 1 秒内只计一次；不在投放中的广告忽略。
// 计数先在 Valkey 累加，由 worker 每分钟落库；有频控的广告的曝光同时计入设备当天的次数。
func (s *AdService) Record(ctx context.Context, ev AdEvent) error {
	if ev.Kind != AdImpression && ev.Kind != AdClick {
		return badRequest("kind 只能是 impression 或 click")
	}
	if ev.AdID <= 0 {
		return badRequest("ad_id 无效")
	}
	candidates, err := s.candidates(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(candidates, func(a Ad) bool { return a.ID == ev.AdID })
	if i < 0 {
		return nil
	}
	ad := candidates[i]
	if ev.DeviceKey != "" {
		first, err := s.stats.FirstEvent(ctx, ev.DeviceKey, ad.ID, ev.Kind)
		if err != nil {
			return fmt.Errorf("广告事件去重: %w", err)
		}
		if !first {
			return nil
		}
	}
	capDay := ""
	if ev.Kind == AdImpression && ad.FreqCap > 0 && ev.DeviceKey != "" {
		capDay = beijingDay(s.now())
	}
	if err := s.stats.AddEvent(ctx, ad.ID, ev.Kind, ev.DeviceKey, capDay); err != nil {
		return fmt.Errorf("广告事件计数: %w", err)
	}
	return nil
}

// FlushCounts 把 Valkey 里累计的曝光、点击写入数据库，返回写入了几条广告（worker 每分钟执行）。
// token 是本次任务的标识：写库失败时取出的计数留在 token 名下，任务重试时沿用同一个 token 再写一次。
func (s *AdService) FlushCounts(ctx context.Context, token string) (int, error) {
	counts, err := s.stats.TakePending(ctx, token)
	if err != nil {
		return 0, fmt.Errorf("取出待落库的广告计数: %w", err)
	}
	if len(counts) == 0 {
		return 0, nil
	}
	if err := s.repo.AddAdCounts(ctx, counts); err != nil {
		return 0, fmt.Errorf("广告计数落库: %w", err)
	}
	// 已经写进数据库：清理失败只记日志，不能让任务重试（会重复累加），留下的那一份到期自动删除
	if err := s.stats.DonePending(ctx, token); err != nil {
		slog.WarnContext(ctx, "清理已落库的广告计数失败", "token", token, "err", err)
	}
	return len(counts), nil
}

func (s *AdService) candidates(ctx context.Context) ([]Ad, error) {
	var list []Ad
	err := s.cache.GetOrLoadJSON(ctx, adCacheKey, adCacheTTL, &list, func(ctx context.Context) (any, error) {
		return s.repo.ListAdCandidates(ctx)
	})
	return list, err
}

// dailyImpressions 读取设备当天对有频控的广告的曝光次数。读不到时不做频控（宁可多投一次，也不让页面没有广告）。
func (s *AdService) dailyImpressions(ctx context.Context, device string, now time.Time, ids []int64) map[int64]int64 {
	if device == "" || len(ids) == 0 {
		return nil
	}
	m, err := s.stats.DailyImpressions(ctx, device, beijingDay(now), ids)
	if err != nil {
		slog.WarnContext(ctx, "读取广告频控计数失败，本次不做频控", "err", err)
		return nil
	}
	return m
}

// pick 按权重不放回地随机取 n 条。
func (s *AdService) pick(pool []Ad, n int) []Ad {
	pool = slices.Clone(pool)
	out := make([]Ad, 0, min(n, len(pool)))
	for len(out) < n && len(pool) > 0 {
		total := 0
		for _, a := range pool {
			total += max(a.Weight, 1)
		}
		r := s.intN(total)
		i := 0
		for ; i < len(pool)-1; i++ {
			if r -= max(pool[i].Weight, 1); r < 0 {
				break
			}
		}
		out = append(out, pool[i])
		pool = slices.Delete(pool, i, i+1)
	}
	return out
}

// invalidate 删除前台候选缓存。删除失败只记日志：缓存最多一分钟后自然过期。
func (s *AdService) invalidate(ctx context.Context) {
	if err := s.cache.Delete(ctx, adCacheKey); err != nil {
		slog.WarnContext(ctx, "清除广告缓存失败，最多一分钟后生效", "err", err)
	}
}

// parseSlots 解析逗号分隔的位置：去掉空白与重复，保持请求顺序。
func parseSlots(raw string) ([]AdSlot, error) {
	if len(raw) > maxSlotsParam {
		return nil, badRequest("slots 太长")
	}
	var out []AdSlot
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		slot := AdSlot(p)
		if !slices.Contains(AdSlots, slot) {
			return nil, badRequest("广告位置无效：%q", p)
		}
		if !slices.Contains(out, slot) {
			out = append(out, slot)
		}
	}
	if len(out) == 0 {
		return nil, badRequest("请用 slots 指定广告位置，多个用逗号分隔")
	}
	return out, nil
}

func cleanAd(in AdInput) (AdInput, error) {
	var err error
	if in.Name, err = cleanText("广告名", in.Name, maxAdNameRunes, true); err != nil {
		return in, err
	}
	if !slices.Contains(AdSlots, in.Slot) {
		return in, badRequest("请选择广告位置")
	}
	// 只保留该素材类型用得到的字段，其余清空
	switch in.CreativeType {
	case CreativeImage:
		if in.Image, err = requiredMediaRef("图片素材", in.Image); err != nil {
			return in, err
		}
		if in.Alt, err = cleanText("图片说明", in.Alt, maxAdAltRunes, false); err != nil {
			return in, err
		}
		in.VideoURL, in.Text, in.HTML = "", "", ""
	case CreativeVideo:
		if in.VideoURL, err = requiredMediaRef("视频素材", in.VideoURL); err != nil {
			return in, err
		}
		if in.Image, err = cleanMediaRef("视频封面", in.Image); err != nil {
			return in, err
		}
		if in.Alt, err = cleanText("视频说明", in.Alt, maxAdAltRunes, false); err != nil {
			return in, err
		}
		in.Text, in.HTML = "", ""
	case CreativeText:
		if in.Text, err = cleanText("文字内容", in.Text, maxAdTextRunes, true); err != nil {
			return in, err
		}
		in.Image, in.VideoURL, in.HTML, in.Alt = "", "", "", ""
	case CreativeHTML:
		if in.HTML, err = sanitizeAdHTML(in.HTML); err != nil {
			return in, err
		}
		in.Image, in.VideoURL, in.Text, in.Alt = "", "", "", ""
	default:
		return in, badRequest("请选择素材类型：图片、视频、文字或自定义 HTML")
	}
	if in.ClickURL, err = cleanLink("跳转网址", in.ClickURL); err != nil {
		return in, err
	}
	switch in.Device {
	case "":
		in.Device = AdDeviceAll
	case AdDeviceAll, AdDeviceMobile, AdDeviceDesktop:
	default:
		return in, badRequest("投给谁只能是全部、手机或电脑")
	}
	if in.Locales, err = cleanSet("投放语言", in.Locales, Locales); err != nil {
		return in, err
	}
	switch {
	case in.Weight == 0:
		in.Weight = defaultAdWeight
	case in.Weight < 1 || in.Weight > maxAdWeight:
		return in, badRequest("出现机会（权重）为 1–%d", maxAdWeight)
	}
	if in.FreqCap < 0 || in.FreqCap > maxFreqCap {
		return in, badRequest("每天次数上限为 0–%d（0 表示不限）", maxFreqCap)
	}
	in.EndAt = utc(in.EndAt)
	if in.Status, err = cleanStatus(in.Status); err != nil {
		return in, err
	}
	return in, nil
}

// requiredMediaRef 是必填媒体地址：服务端生成的 img/、video/ key 或兼容的历史地址。
func requiredMediaRef(label, raw string) (string, error) {
	s, err := cleanMediaRef(label, raw)
	if err == nil && s == "" {
		err = badRequest("请填写%s地址", label)
	}
	return s, err
}

// requiredLink 是必填的跳转地址（规则同 cleanLink）。
func requiredLink(label, raw string) (string, error) {
	s, err := cleanLink(label, raw)
	if err == nil && s == "" {
		err = badRequest("请填写%s地址", label)
	}
	return s, err
}

// sanitizeAdHTML 按白名单清洗自定义 HTML；清洗后什么都不剩时拒绝保存。
func sanitizeAdHTML(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", badRequest("请填写自定义 HTML")
	}
	if utf8.RuneCountInString(raw) > maxAdHTMLRunes {
		return "", badRequest("自定义 HTML 不能超过 %d 个字符", maxAdHTMLRunes)
	}
	out := htmlsan.Sanitize(raw)
	if out == "" {
		return "", badRequest("自定义 HTML 清洗后为空：只允许常见排版标签、链接与图片，脚本、样式表、iframe 与事件属性会被去掉")
	}
	if utf8.RuneCountInString(out) > maxAdHTMLStored {
		return "", badRequest("自定义 HTML 清洗后超过 %d 个字符，请精简", maxAdHTMLStored)
	}
	return out, nil
}
