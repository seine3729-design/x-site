package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

// fakeAdRepo 是内存版广告仓储。
type fakeAdRepo struct {
	mu         sync.Mutex
	ads        map[int64]Ad
	candidates []Ad
	nextID     int64
	created    []AdInput
	updated    []AdInput
	counts     []map[int64]AdCounts
	countErr   error
}

func newFakeAdRepo(ads ...Ad) *fakeAdRepo {
	r := &fakeAdRepo{ads: map[int64]Ad{}, nextID: 100}
	for _, a := range ads {
		r.ads[a.ID] = a
	}
	return r
}

func (r *fakeAdRepo) ListAds(context.Context, AdQuery) ([]Ad, int64, error) { return nil, 0, nil }

func (r *fakeAdRepo) GetAd(_ context.Context, id int64) (Ad, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.ads[id]
	if !ok {
		return Ad{}, errs.ErrNotFound
	}
	return a, nil
}

func (r *fakeAdRepo) CreateAd(_ context.Context, in AdInput) (Ad, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created = append(r.created, in)
	r.nextID++
	a := adFromInput(r.nextID, in)
	r.ads[a.ID] = a
	return a, nil
}

func (r *fakeAdRepo) UpdateAd(_ context.Context, id int64, in AdInput) (Ad, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ads[id]; !ok {
		return Ad{}, errs.ErrNotFound
	}
	r.updated = append(r.updated, in)
	a := adFromInput(id, in)
	r.ads[id] = a
	return a, nil
}

func (r *fakeAdRepo) DeleteAd(_ context.Context, id int64) (Ad, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.ads[id]
	if !ok {
		return Ad{}, errs.ErrNotFound
	}
	delete(r.ads, id)
	return a, nil
}

func (r *fakeAdRepo) ListAdCandidates(context.Context) ([]Ad, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.candidates), nil
}

func (r *fakeAdRepo) AddAdCounts(_ context.Context, counts map[int64]AdCounts) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.countErr != nil {
		return r.countErr
	}
	r.counts = append(r.counts, counts)
	return nil
}

func adFromInput(id int64, in AdInput) Ad {
	return Ad{
		ID: id, Name: in.Name, Slot: in.Slot, CreativeType: in.CreativeType, Image: in.Image, VideoURL: in.VideoURL, Text: in.Text,
		HTML: in.HTML, Alt: in.Alt, ClickURL: in.ClickURL, Device: in.Device, Locales: in.Locales, Weight: in.Weight,
		EndAt: in.EndAt, FreqCap: in.FreqCap, Status: in.Status,
	}
}

// fakeAdStats 是内存版 Valkey 计数。
type fakeAdStats struct {
	mu       sync.Mutex
	seen     map[string]bool // 去重窗口内出现过的「设备:广告:事件」
	events   []string        // AddEvent 的调用：「广告:事件:设备:日期」
	daily    map[string]int64
	dailyErr error
	pending  map[int64]AdCounts
	taken    map[string]map[int64]AdCounts
	done     []string
}

func newFakeAdStats() *fakeAdStats {
	return &fakeAdStats{seen: map[string]bool{}, daily: map[string]int64{}, taken: map[string]map[int64]AdCounts{}}
}

func (f *fakeAdStats) FirstEvent(_ context.Context, device string, adID int64, kind AdEventKind) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := fmt.Sprintf("%s:%d:%s", device, adID, kind)
	if f.seen[k] {
		return false, nil
	}
	f.seen[k] = true
	return true, nil
}

func (f *fakeAdStats) AddEvent(_ context.Context, adID int64, kind AdEventKind, device, capDay string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, fmt.Sprintf("%d:%s:%s:%s", adID, kind, device, capDay))
	if capDay != "" {
		f.daily[fmt.Sprintf("%s:%s:%d", device, capDay, adID)]++
	}
	return nil
}

func (f *fakeAdStats) DailyImpressions(_ context.Context, device, day string, ids []int64) (map[int64]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dailyErr != nil {
		return nil, f.dailyErr
	}
	out := map[int64]int64{}
	for _, id := range ids {
		if n := f.daily[fmt.Sprintf("%s:%s:%d", device, day, id)]; n > 0 {
			out[id] = n
		}
	}
	return out, nil
}

func (f *fakeAdStats) TakePending(_ context.Context, token string) (map[int64]AdCounts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m, ok := f.taken[token]; ok {
		return m, nil
	}
	m := f.pending
	f.pending = nil
	if len(m) > 0 {
		f.taken[token] = m
	}
	return m, nil
}

func (f *fakeAdStats) DonePending(_ context.Context, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.taken, token)
	f.done = append(f.done, token)
	return nil
}

// seqIntN 依次返回给定的值（对 n 取模），让加权随机可预测。
func seqIntN(vals ...int) func(int) int {
	var mu sync.Mutex
	i := 0
	return func(n int) int {
		mu.Lock()
		defer mu.Unlock()
		v := vals[i%len(vals)]
		i++
		return v % n
	}
}

func newTestAdService(repo *fakeAdRepo, stats *fakeAdStats) (*AdService, *memCache) {
	c := newMemCache()
	s := NewAdService(repo, stats, c)
	s.now = func() time.Time { return time.Date(2026, 10, 24, 17, 0, 0, 0, time.UTC) } // 北京时间 25 日 01:00
	return s, c
}

func validAd() AdInput {
	return AdInput{Name: "首页顶部会员", Slot: SlotHomeTop, CreativeType: CreativeImage, Image: "/ads/top.webp", Alt: "会员 5 折", ClickURL: "/vip"}
}

func TestCleanAd(t *testing.T) {
	end := time.Date(2026, 11, 1, 8, 0, 0, 0, time.FixedZone("CST", 8*3600))
	in := validAd()
	in.Name, in.Text, in.HTML, in.VideoURL, in.EndAt = "  首页顶部会员  ", "多余的文字", "<b>多余</b>", "/v.mp4", &end
	got, err := cleanAd(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "首页顶部会员" || got.Device != AdDeviceAll || got.Weight != defaultAdWeight || got.FreqCap != 0 || got.Status != StatusActive ||
		got.Locales == nil || len(got.Locales) != 0 {
		t.Fatalf("默认值：%+v", got)
	}
	if got.Text != "" || got.HTML != "" || got.VideoURL != "" || got.Image != "/ads/top.webp" || got.Alt != "会员 5 折" {
		t.Fatalf("只保留该素材类型用得到的字段：%+v", got)
	}
	if got.EndAt.Location() != time.UTC || !got.EndAt.Equal(end) {
		t.Fatalf("结束时间转为 UTC：%v", got.EndAt)
	}

	video := validAd()
	video.CreativeType, video.VideoURL, video.Image = CreativeVideo, "https://cdn.example.com/a.mp4", "/ads/poster.webp"
	if got, err := cleanAd(video); err != nil || got.VideoURL == "" || got.Image != "/ads/poster.webp" {
		t.Fatalf("视频素材可以带封面：%+v %v", got, err)
	}
	text := validAd()
	text.CreativeType, text.Text = CreativeText, " 新剧上线 "
	if got, err := cleanAd(text); err != nil || got.Text != "新剧上线" || got.Image != "" || got.Alt != "" {
		t.Fatalf("文字素材：%+v %v", got, err)
	}

	for name, tt := range map[string]struct {
		mutate func(*AdInput)
		msg    string
	}{
		"没有广告名":     {func(a *AdInput) { a.Name = " " }, "广告名不能为空"},
		"广告名太长":     {func(a *AdInput) { a.Name = strings.Repeat("广", 51) }, "广告名不能超过 50 个字"},
		"位置无效":      {func(a *AdInput) { a.Slot = "sidebar" }, "请选择广告位置"},
		"素材类型无效":    {func(a *AdInput) { a.CreativeType = "flash" }, "请选择素材类型"},
		"图片没填":      {func(a *AdInput) { a.Image = "" }, "请填写图片素材"},
		"图片地址非法":    {func(a *AdInput) { a.Image = "javascript:alert(1)" }, "图片素材需为"},
		"视频没填":      {func(a *AdInput) { a.CreativeType = CreativeVideo }, "请填写视频素材"},
		"文字没填":      {func(a *AdInput) { a.CreativeType = CreativeText }, "文字内容不能为空"},
		"文字太长":      {func(a *AdInput) { a.CreativeType, a.Text = CreativeText, strings.Repeat("字", 201) }, "文字内容不能超过 200 个字"},
		"HTML 没填":   {func(a *AdInput) { a.CreativeType = CreativeHTML }, "请填写自定义 HTML"},
		"HTML 太长":   {func(a *AdInput) { a.CreativeType, a.HTML = CreativeHTML, strings.Repeat("a", maxAdHTMLRunes+1) }, "不能超过"},
		"HTML 只有脚本": {func(a *AdInput) { a.CreativeType, a.HTML = CreativeHTML, "<script>alert(1)</script>" }, "清洗后为空"},
		"跳转网址非法":    {func(a *AdInput) { a.ClickURL = "//evil.example.com" }, "跳转网址需为"},
		"端无效":       {func(a *AdInput) { a.Device = "tv" }, "投给谁"},
		"语言无效":      {func(a *AdInput) { a.Locales = []string{"fr"} }, "投放语言无效"},
		"权重太大":      {func(a *AdInput) { a.Weight = 101 }, "出现机会"},
		"权重为负":      {func(a *AdInput) { a.Weight = -1 }, "出现机会"},
		"频控为负":      {func(a *AdInput) { a.FreqCap = -1 }, "每天次数上限"},
		"频控太大":      {func(a *AdInput) { a.FreqCap = 1001 }, "每天次数上限"},
		"状态无效":      {func(a *AdInput) { a.Status = "paused" }, "状态只能是"},
	} {
		in := validAd()
		tt.mutate(&in)
		_, err := cleanAd(in)
		if err == nil {
			t.Errorf("%s：应拒绝", name)
			continue
		}
		if e := errs.From(err); e.Code != errs.ErrBadRequest.Code || !strings.Contains(e.Message, tt.msg) {
			t.Errorf("%s：期望 400 且包含「%s」，得到 %s「%s」", name, tt.msg, e.Code, e.Message)
		}
	}
}

func TestSanitizeAdHTML(t *testing.T) {
	in := validAd()
	in.CreativeType = CreativeHTML
	in.HTML = `<div style="color:red;position:fixed" onclick="steal()"><a href="javascript:alert(1)">坏链接</a>` +
		`<a href="https://example.com/vip">会员</a><img src="https://cdn.example.com/a.png" onerror="alert(1)" alt="图">` +
		`<script>alert(document.cookie)</script><iframe src="https://evil.example.com"></iframe><style>body{display:none}</style>` +
		`<svg onload="alert(1)"></svg><form action="/x"><input name="p"></form></div>`
	got, err := cleanAd(in)
	if err != nil {
		t.Fatal(err)
	}
	h := got.HTML
	for _, bad := range []string{"<script", "onclick", "onerror", "onload", "javascript:", "<iframe", "<style", "position", "<form", "<input", "steal"} {
		if strings.Contains(strings.ToLower(h), bad) {
			t.Errorf("清洗后不应包含 %q：%s", bad, h)
		}
	}
	for _, want := range []string{`href="https://example.com/vip"`, `target="_blank"`, "noopener", `src="https://cdn.example.com/a.png"`, "color: red", "会员"} {
		if !strings.Contains(h, want) {
			t.Errorf("清洗后应保留 %q：%s", want, h)
		}
	}
	if got.Image != "" || got.Alt != "" || got.Text != "" {
		t.Fatalf("HTML 素材不保留其他素材字段：%+v", got)
	}
}

func TestAdCustomHTMLPermission(t *testing.T) {
	ctx := context.Background()
	html := Ad{ID: 1, Name: "HTML", Slot: SlotHomeMid, CreativeType: CreativeHTML, HTML: "<b>x</b>", Device: AdDeviceAll, Weight: 10, Status: StatusActive}
	img := Ad{ID: 2, Name: "图片", Slot: SlotHomeMid, CreativeType: CreativeImage, Image: "/a.webp", Device: AdDeviceAll, Weight: 10, Status: StatusActive}
	repo := newFakeAdRepo(html, img)
	s, _ := newTestAdService(repo, newFakeAdStats())

	in := validAd()
	in.CreativeType, in.HTML = CreativeHTML, "<p>自定义</p>"
	_, err := s.Create(ctx, in, false)
	wantErr(t, err, errs.ErrForbidden, "自定义 HTML")
	if len(repo.created) != 0 {
		t.Fatal("没有权限时不应写库")
	}
	if _, err := s.Create(ctx, in, true); err != nil {
		t.Fatalf("有权限可以新建 HTML 素材：%v", err)
	}

	// 改成 HTML、修改已有的 HTML 素材（即使改成别的类型）都要权限；普通素材不受影响
	_, _, err = s.Update(ctx, img.ID, in, false)
	wantErr(t, err, errs.ErrForbidden, "自定义 HTML")
	_, _, err = s.Update(ctx, html.ID, validAd(), false)
	wantErr(t, err, errs.ErrForbidden, "自定义 HTML")
	if _, _, err := s.Update(ctx, img.ID, validAd(), false); err != nil {
		t.Fatalf("普通素材不需要 HTML 权限：%v", err)
	}
	if before, after, err := s.Update(ctx, html.ID, validAd(), true); err != nil || before.CreativeType != CreativeHTML || after.CreativeType != CreativeImage {
		t.Fatalf("有权限可以修改 HTML 素材：%+v %+v %v", before, after, err)
	}
	// 删除只要「修改」权限（HTTP 层判断），领域层不拦
	if _, err := s.Delete(ctx, html.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Update(ctx, 999, validAd(), true); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不存在的广告 404：%v", err)
	}
}

func TestAdWritesInvalidateCache(t *testing.T) {
	ctx := context.Background()
	repo := newFakeAdRepo()
	s, c := newTestAdService(repo, newFakeAdStats())
	if _, err := s.Serve(ctx, ServeRequest{Slots: "home_top", DeviceKey: "d1"}); err != nil {
		t.Fatal(err)
	}
	if c.loads != 1 {
		t.Fatalf("第一次读应回源：%d", c.loads)
	}
	a, err := s.Create(ctx, validAd(), false)
	if err != nil {
		t.Fatal(err)
	}
	repo.candidates = []Ad{a}
	got, _ := s.Serve(ctx, ServeRequest{Slots: "home_top", DeviceKey: "d1"})
	if c.loads != 2 || len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("新建后清缓存，前台立即能拉到：loads=%d %+v", c.loads, got)
	}
	if _, _, err := s.Update(ctx, a.ID, validAd(), false); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Serve(ctx, ServeRequest{Slots: "home_top", DeviceKey: "d1"})
	if _, err := s.Delete(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = s.Serve(ctx, ServeRequest{Slots: "home_top", DeviceKey: "d1"})
	if c.loads != 4 {
		t.Fatalf("保存、删除后都应清缓存：%d", c.loads)
	}
}

func liveAd(id int64, slot AdSlot, weight int) Ad {
	return Ad{ID: id, Name: fmt.Sprintf("广告%d", id), Slot: slot, CreativeType: CreativeImage, Image: fmt.Sprintf("/ads/%d.webp", id),
		Device: AdDeviceAll, Weight: weight, Status: StatusActive}
}

func TestServeTargeting(t *testing.T) {
	ctx := context.Background()
	past := time.Date(2026, 10, 24, 16, 0, 0, 0, time.UTC) // 已过（缓存里还没刷新掉的）
	future := past.Add(48 * time.Hour)
	mobile, desktop := liveAd(2, SlotHomeTop, 10), liveAd(3, SlotHomeTop, 10)
	mobile.Device, desktop.Device = AdDeviceMobile, AdDeviceDesktop
	ended, off, en, later := liveAd(4, SlotHomeTop, 10), liveAd(5, SlotHomeTop, 10), liveAd(6, SlotHomeTop, 10), liveAd(7, SlotHomeTop, 10)
	ended.EndAt, off.Status, en.Locales, later.EndAt = &past, StatusInactive, []string{"en"}, &future
	repo := newFakeAdRepo()
	repo.candidates = []Ad{liveAd(1, SlotHomeTop, 10), mobile, desktop, ended, off, en, later, liveAd(8, SlotFooterBanner, 10)}
	s, _ := newTestAdService(repo, newFakeAdStats())

	ids := func(req ServeRequest) []int64 {
		t.Helper()
		req.N = maxAdsPerSlot
		list, err := s.Serve(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		out := []int64{}
		for _, a := range list {
			out = append(out, a.ID)
		}
		slices.Sort(out)
		return out
	}
	if got := ids(ServeRequest{Slots: "home_top", Device: "mobile", DeviceKey: "d"}); !slices.Equal(got, []int64{1, 2, 7}) {
		t.Fatalf("手机：都投 + 只投手机的；已结束、停用、只投英语的不下发：%v", got)
	}
	if got := ids(ServeRequest{Slots: "home_top", Device: "desktop", Locale: "en", DeviceKey: "d"}); !slices.Equal(got, []int64{1, 3, 6, 7}) {
		t.Fatalf("电脑 + 英语：%v", got)
	}
	if got := ids(ServeRequest{Slots: "home_top", Locale: "fr", DeviceKey: "d"}); !slices.Equal(got, []int64{1, 7}) {
		t.Fatalf("不带端只投「都投」的；不支持的语言按简体中文：%v", got)
	}
	list, err := s.Serve(ctx, ServeRequest{Slots: "footer_banner, home_top,footer_banner", N: 1, DeviceKey: "d"})
	if err != nil || len(list) != 2 || list[0].Slot != SlotFooterBanner || list[1].Slot != SlotHomeTop {
		t.Fatalf("多个位置按请求顺序、位置去重、每个位置 n 条：%+v %v", list, err)
	}
	if list[0].Image != "/ads/8.webp" || list[0].CreativeType != CreativeImage {
		t.Fatalf("下发素材字段：%+v", list[0])
	}
	if got, err := s.Serve(ctx, ServeRequest{Slots: "pause", DeviceKey: "d"}); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("没有广告时返回空列表：%#v %v", got, err)
	}

	for name, req := range map[string]ServeRequest{
		"没有位置":  {Slots: " , "},
		"位置无效":  {Slots: "home_top,sidebar"},
		"位置串太长": {Slots: strings.Repeat("home_top,", 40)},
		"n 太大":  {Slots: "home_top", N: 6},
		"n 为负":  {Slots: "home_top", N: -1},
		"端无效":   {Slots: "home_top", Device: "tv"},
	} {
		if _, err := s.Serve(ctx, req); errs.From(err).Code != errs.ErrBadRequest.Code {
			t.Errorf("%s：应 400，得到 %v", name, err)
		}
	}
}

func TestServeWeightedPick(t *testing.T) {
	ctx := context.Background()
	repo := newFakeAdRepo()
	repo.candidates = []Ad{liveAd(1, SlotHomeMid, 1), liveAd(2, SlotHomeMid, 3), liveAd(3, SlotHomeMid, 6)} // 总权重 10
	s, _ := newTestAdService(repo, newFakeAdStats())

	// 落点 0 → 1 号；1–3 → 2 号；4–9 → 3 号
	for r, want := range map[int]int64{0: 1, 1: 2, 3: 2, 4: 3, 9: 3} {
		s.intN = seqIntN(r)
		got, err := s.Serve(ctx, ServeRequest{Slots: "home_mid", DeviceKey: "d"})
		if err != nil || len(got) != 1 || got[0].ID != want {
			t.Errorf("落点 %d 应选 %d 号：%+v %v", r, want, got, err)
		}
	}
	// 取多条时不重复：先选中 3 号（落点 9），剩下 1、2 号总权重 4，落点 0 → 1 号
	s.intN = seqIntN(9, 0)
	got, _ := s.Serve(ctx, ServeRequest{Slots: "home_mid", N: 2, DeviceKey: "d"})
	if len(got) != 2 || got[0].ID != 3 || got[1].ID != 1 {
		t.Fatalf("不放回加权抽样：%+v", got)
	}
	// n 超过候选数时全部返回
	s.intN = seqIntN(0)
	if got, _ := s.Serve(ctx, ServeRequest{Slots: "home_mid", N: 5, DeviceKey: "d"}); len(got) != 3 {
		t.Fatalf("候选不足 n 条时全部返回：%d", len(got))
	}

	// 真随机：权重 1 : 3 : 6 的出现比例大致相符
	s.intN = NewAdService(repo, nil, nil).intN
	hits := map[int64]int{}
	for range 3000 {
		got, _ := s.Serve(ctx, ServeRequest{Slots: "home_mid", DeviceKey: "d"})
		hits[got[0].ID]++
	}
	if hits[1] < 150 || hits[1] > 450 || hits[3] < 1500 || hits[3] > 2100 {
		t.Fatalf("加权随机比例偏离过大：%v", hits)
	}
}

func TestFrequencyCap(t *testing.T) {
	ctx := context.Background()
	capped, free := liveAd(1, SlotGlobalPopup, 10), liveAd(2, SlotGlobalPopup, 10)
	capped.FreqCap = 2
	repo := newFakeAdRepo()
	repo.candidates = []Ad{capped, free}
	stats := newFakeAdStats()
	s, _ := newTestAdService(repo, stats)
	serve := func(device string) []int64 {
		t.Helper()
		list, err := s.Serve(ctx, ServeRequest{Slots: "global_popup", N: 5, DeviceKey: device})
		if err != nil {
			t.Fatal(err)
		}
		out := []int64{}
		for _, a := range list {
			out = append(out, a.ID)
		}
		slices.Sort(out)
		return out
	}
	impression := func(device string, id int64) {
		t.Helper()
		stats.seen = map[string]bool{} // 模拟去重窗口（1 秒）已过
		if err := s.Record(ctx, AdEvent{AdID: id, Kind: AdImpression, DeviceKey: device}); err != nil {
			t.Fatal(err)
		}
	}

	impression("phone", 1)
	if got := serve("phone"); !slices.Equal(got, []int64{1, 2}) {
		t.Fatalf("曝光 1 次未到上限：%v", got)
	}
	impression("phone", 1)
	if got := serve("phone"); !slices.Equal(got, []int64{2}) {
		t.Fatalf("同一设备当天曝光达到上限后不再下发该广告：%v", got)
	}
	if got := serve("laptop"); !slices.Equal(got, []int64{1, 2}) {
		t.Fatalf("换设备仍下发：%v", got)
	}
	// 按北京时间计天：UTC 24 日 17:00 是北京时间 25 日
	if want := "1:impression:phone:20261025"; !slices.Contains(stats.events, want) {
		t.Fatalf("有频控的广告曝光按北京日期计入设备当天次数：%v", stats.events)
	}
	// 第二天重新计数
	s.now = func() time.Time { return time.Date(2026, 10, 25, 16, 30, 0, 0, time.UTC) } // 北京时间 26 日 00:30
	if got := serve("phone"); !slices.Equal(got, []int64{1, 2}) {
		t.Fatalf("北京时间第二天重新下发：%v", got)
	}
	// 计数读不到时照常下发（宁可多投一次，不让页面没有广告）
	stats.dailyErr = errors.New("valkey down")
	s.now = func() time.Time { return time.Date(2026, 10, 24, 17, 0, 0, 0, time.UTC) }
	if got := serve("phone"); !slices.Equal(got, []int64{1, 2}) {
		t.Fatalf("频控计数不可用时不做频控：%v", got)
	}
}

func TestRecordAdEvent(t *testing.T) {
	ctx := context.Background()
	capped := liveAd(1, SlotHomeTop, 10)
	capped.FreqCap = 3
	repo := newFakeAdRepo()
	repo.candidates = []Ad{capped, liveAd(2, SlotHomeTop, 10)}
	stats := newFakeAdStats()
	s, _ := newTestAdService(repo, stats)

	for _, ev := range []AdEvent{
		{AdID: 1, Kind: AdImpression, DeviceKey: "d1"},
		{AdID: 1, Kind: AdImpression, DeviceKey: "d1"}, // 1 秒内重复：不计
		{AdID: 1, Kind: AdClick, DeviceKey: "d1"},      // 不同事件分别计
		{AdID: 1, Kind: AdImpression, DeviceKey: "d2"}, // 不同设备分别计
		{AdID: 2, Kind: AdImpression, DeviceKey: "d1"}, // 没有频控：不记设备当天次数
		{AdID: 404, Kind: AdClick, DeviceKey: "d1"},    // 不在投放中的广告：忽略
	} {
		if err := s.Record(ctx, ev); err != nil {
			t.Fatalf("%+v：%v", ev, err)
		}
	}
	want := []string{"1:impression:d1:20261025", "1:click:d1:", "1:impression:d2:20261025", "2:impression:d1:"}
	if !slices.Equal(stats.events, want) {
		t.Fatalf("计数调用：%v", stats.events)
	}
	for name, ev := range map[string]AdEvent{
		"事件类型无效": {AdID: 1, Kind: "view", DeviceKey: "d1"},
		"广告编号无效": {AdID: 0, Kind: AdClick, DeviceKey: "d1"},
	} {
		if err := s.Record(ctx, ev); errs.From(err).Code != errs.ErrBadRequest.Code {
			t.Errorf("%s：应 400，得到 %v", name, err)
		}
	}
}

func TestFlushAdCounts(t *testing.T) {
	ctx := context.Background()
	repo := newFakeAdRepo()
	stats := newFakeAdStats()
	s, _ := newTestAdService(repo, stats)

	if n, err := s.FlushCounts(ctx, "job-1"); err != nil || n != 0 || len(repo.counts) != 0 {
		t.Fatalf("没有待落库的计数时不写库：%d %v", n, err)
	}
	stats.pending = map[int64]AdCounts{1: {Impressions: 5, Clicks: 1}, 2: {Impressions: 2}}
	repo.countErr = errors.New("db down")
	if _, err := s.FlushCounts(ctx, "job-2"); err == nil {
		t.Fatal("写库失败应返回错误，由任务重试")
	}
	if len(stats.done) != 0 {
		t.Fatal("写库失败时保留取出的计数")
	}
	// 重试沿用同一个任务标识，拿回上次取出的那一份
	repo.countErr = nil
	stats.pending = map[int64]AdCounts{3: {Clicks: 9}} // 新到的计数留给下一轮
	n, err := s.FlushCounts(ctx, "job-2")
	if err != nil || n != 2 || len(repo.counts) != 1 || repo.counts[0][1] != (AdCounts{Impressions: 5, Clicks: 1}) || !slices.Equal(stats.done, []string{"job-2"}) {
		t.Fatalf("重试时写入上次取出的计数：%d %v %v %v", n, err, repo.counts, stats.done)
	}
	if n, _ := s.FlushCounts(ctx, "job-3"); n != 1 || repo.counts[1][3] != (AdCounts{Clicks: 9}) {
		t.Fatalf("下一轮写入新到的计数：%d %v", n, repo.counts)
	}
}

func TestAdDisplayStatus(t *testing.T) {
	now := time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	for _, tt := range []struct {
		ad   Ad
		want DisplayStatus
	}{
		{Ad{Status: StatusActive}, DisplayLive},
		{Ad{Status: StatusActive, EndAt: &future}, DisplayLive},
		{Ad{Status: StatusActive, EndAt: &past}, DisplayEnded},
		{Ad{Status: StatusInactive, EndAt: &future}, DisplayInactive},
	} {
		if got := tt.ad.DisplayStatus(now); got != tt.want {
			t.Errorf("%+v：期望 %s，得到 %s", tt.ad, tt.want, got)
		}
	}
}
