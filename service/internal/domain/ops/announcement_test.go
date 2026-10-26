package ops

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

var t0 = time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)

func at(d time.Duration) *time.Time { v := t0.Add(d); return &v }

// fakeAnnouncementRepo 只实现测试用到的方法；嵌入的接口为 nil，走到意外的分支会 panic。
type fakeAnnouncementRepo struct {
	AnnouncementRepo
	rows       map[int64]Announcement
	candidates []Announcement
	saved      []AnnouncementInput
	query      AnnouncementQuery
}

func (r *fakeAnnouncementRepo) ListAnnouncements(_ context.Context, q AnnouncementQuery) ([]Announcement, int64, error) {
	r.query = q
	return []Announcement{}, 0, nil
}

func (r *fakeAnnouncementRepo) GetAnnouncement(_ context.Context, id int64) (Announcement, error) {
	a, ok := r.rows[id]
	if !ok {
		return Announcement{}, errs.ErrNotFound
	}
	return a, nil
}

func (r *fakeAnnouncementRepo) CreateAnnouncement(_ context.Context, in AnnouncementInput) (Announcement, error) {
	r.saved = append(r.saved, in)
	return Announcement{ID: 9, Kind: in.Kind, Content: in.Content, Status: in.Status}, nil
}

func (r *fakeAnnouncementRepo) UpdateAnnouncement(_ context.Context, id int64, in AnnouncementInput) (Announcement, error) {
	r.saved = append(r.saved, in)
	return Announcement{ID: id, Kind: in.Kind, Content: in.Content, Status: in.Status}, nil
}

func (r *fakeAnnouncementRepo) DeleteAnnouncement(_ context.Context, id int64) (Announcement, error) {
	return r.GetAnnouncement(context.Background(), id)
}

func (r *fakeAnnouncementRepo) ListAnnouncementCandidates(context.Context) ([]Announcement, error) {
	return r.candidates, nil
}

func newAnnouncementService(r *fakeAnnouncementRepo, c *memCache) *AnnouncementService {
	s := NewAnnouncementService(r, c)
	s.now = func() time.Time { return t0 }
	return s
}

func TestCleanAnnouncement(t *testing.T) {
	cst := time.FixedZone("CST", 8*3600)
	start := time.Date(2026, 10, 24, 9, 0, 0, 0, cst)
	end := start.Add(24 * time.Hour)
	in, err := cleanAnnouncement(AnnouncementInput{
		Kind: AnnouncementPopup, Content: "  国庆会员 5 折  ", ClickURL: " /vip ", Pages: []string{"play", "home", "play"},
		Locales: []string{"en", "zh-CN"}, Image: "/ads/1.webp", StartAt: &start, EndAt: &end,
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.Content != "国庆会员 5 折" || in.ClickURL != "/vip" || in.Frequency != FrequencyAlways || in.Status != StatusActive {
		t.Fatalf("规范化与默认值：%+v", in)
	}
	if !slices.Equal(in.Pages, []string{"home", "play"}) || !slices.Equal(in.Locales, []string{"zh-CN", "en"}) || in.Image != "/ads/1.webp" {
		t.Fatalf("多选项去重并按固定顺序，弹窗保留配图：%+v", in)
	}
	if in.StartAt.Location() != time.UTC || !in.StartAt.Equal(start) {
		t.Fatalf("时间转为 UTC：%v", in.StartAt)
	}
	topbar, _ := cleanAnnouncement(AnnouncementInput{Kind: AnnouncementTopbar, Content: "x", Image: "/ads/1.webp"})
	if topbar.Image != "" {
		t.Fatalf("顶部滚动条没有配图：%q", topbar.Image)
	}

	bad := []struct {
		in  AnnouncementInput
		msg string
	}{
		{AnnouncementInput{Kind: AnnouncementInbox, Content: "x"}, "站内信暂未开放"},
		{AnnouncementInput{Kind: "banner", Content: "x"}, "请选择公告方式"},
		{AnnouncementInput{Kind: AnnouncementTopbar, Content: "   "}, "公告内容不能为空"},
		{AnnouncementInput{Kind: AnnouncementTopbar, Content: strings.Repeat("字", 501)}, "不能超过 500 个字"},
		{AnnouncementInput{Kind: AnnouncementTopbar, Content: "x", ClickURL: "javascript:alert(1)"}, "跳转网址需为"},
		{AnnouncementInput{Kind: AnnouncementPopup, Content: "x", Image: "//evil.example.com/1.png"}, "弹窗配图需为"},
		{AnnouncementInput{Kind: AnnouncementTopbar, Content: "x", Frequency: "hourly"}, "频率只能是"},
		{AnnouncementInput{Kind: AnnouncementTopbar, Content: "x", Pages: []string{"news"}}, "出现页面无效"},
		{AnnouncementInput{Kind: AnnouncementTopbar, Content: "x", Locales: []string{"zh-TW"}}, "投放语言无效"},
		{AnnouncementInput{Kind: AnnouncementTopbar, Content: "x", StartAt: &end, EndAt: &start}, "结束时间要晚于开始时间"},
		{AnnouncementInput{Kind: AnnouncementTopbar, Content: "x", StartAt: &start, EndAt: &start}, "结束时间要晚于开始时间"},
		{AnnouncementInput{Kind: AnnouncementTopbar, Content: "x", Status: "paused"}, "状态只能是"},
	}
	for _, b := range bad {
		_, err := cleanAnnouncement(b.in)
		wantErr(t, err, errs.ErrBadRequest, b.msg)
	}
}

func TestAnnouncementDisplayStatus(t *testing.T) {
	for _, tt := range []struct {
		a    Announcement
		want DisplayStatus
	}{
		{Announcement{Status: StatusInactive}, DisplayInactive},
		{Announcement{Status: StatusActive}, DisplayLive},
		{Announcement{Status: StatusActive, StartAt: at(time.Minute)}, DisplayScheduled},
		{Announcement{Status: StatusActive, StartAt: at(-time.Hour), EndAt: at(0)}, DisplayEnded},
		{Announcement{Status: StatusActive, StartAt: at(0), EndAt: at(time.Second)}, DisplayLive},
		{Announcement{Status: StatusInactive, EndAt: at(-time.Hour)}, DisplayInactive},
	} {
		if got := tt.a.DisplayStatus(t0); got != tt.want {
			t.Errorf("%+v 的显示状态 = %s，期望 %s", tt.a, got, tt.want)
		}
	}
}

func TestAnnouncementLive(t *testing.T) {
	r := &fakeAnnouncementRepo{candidates: []Announcement{
		{ID: 6, Kind: AnnouncementPopup, Content: "未开始", Status: StatusActive, StartAt: at(time.Hour)},
		{ID: 5, Kind: AnnouncementPopup, Content: "英文弹窗", Status: StatusActive, Locales: []string{"en"}, Image: "/a.webp"},
		{ID: 4, Kind: AnnouncementTopbar, Content: "已结束", Status: StatusActive, EndAt: at(-time.Second)},
		{ID: 3, Kind: AnnouncementTopbar, Content: "全部语言", Status: StatusActive, Frequency: FrequencyDaily},
		{ID: 2, Kind: AnnouncementTopbar, Content: "中文", Status: StatusActive, Locales: []string{"zh-CN"}, Pages: []string{"home"}},
	}}
	c := newMemCache()
	s := newAnnouncementService(r, c)
	ctx := context.Background()

	got, err := s.Live(ctx, "zh-CN")
	if err != nil {
		t.Fatal(err)
	}
	if ids := idsOf(got); !slices.Equal(ids, []int64{3, 2}) {
		t.Fatalf("中文只看到显示中、投给中文或全部语言的，新的在前：%v", ids)
	}
	if got[0].Pages == nil || got[0].Frequency != FrequencyDaily || !slices.Equal(got[1].Pages, []string{"home"}) {
		t.Fatalf("出现页面为空时输出 []，频率原样下发：%+v", got)
	}
	if ids := idsOf(must(s.Live(ctx, "en"))); !slices.Equal(ids, []int64{5, 3}) {
		t.Fatalf("英文：%v", ids)
	}
	if ids := idsOf(must(s.Live(ctx, "fr"))); !slices.Equal(ids, []int64{3, 2}) {
		t.Fatalf("不支持的语言按简体中文：%v", ids)
	}
	if c.loads != 1 {
		t.Fatalf("候选公告应只回源一次，得到 %d", c.loads)
	}

	// 时间到了不需要后台修改：缓存里的候选每次按当前时间判断
	s.now = func() time.Time { return t0.Add(2 * time.Hour) }
	if ids := idsOf(must(s.Live(ctx, "zh-CN"))); !slices.Equal(ids, []int64{6, 3, 2}) {
		t.Fatalf("到了开始时间就显示：%v", ids)
	}
}

func TestAnnouncementWritesInvalidateCache(t *testing.T) {
	r := &fakeAnnouncementRepo{rows: map[int64]Announcement{1: {ID: 1, Kind: AnnouncementTopbar, Content: "旧", Status: StatusActive}}}
	c := newMemCache()
	s := newAnnouncementService(r, c)
	ctx := context.Background()
	in := AnnouncementInput{Kind: AnnouncementTopbar, Content: "新"}

	steps := []struct {
		name string
		do   func() error
	}{
		{"新建", func() error { _, err := s.Create(ctx, in); return err }},
		{"保存", func() error { _, _, err := s.Update(ctx, 1, in); return err }},
		{"删除", func() error { _, err := s.Delete(ctx, 1); return err }},
	}
	for _, st := range steps {
		must(s.Live(ctx, "zh-CN"))
		if err := st.do(); err != nil {
			t.Fatalf("%s：%v", st.name, err)
		}
		if _, ok := c.data[announcementCacheKey]; ok {
			t.Fatalf("%s后应清除前台候选缓存", st.name)
		}
	}

	// 清缓存失败不影响保存（缓存一分钟后自然过期）
	c.err = errors.New("valkey down")
	if _, err := s.Create(ctx, in); err != nil {
		t.Fatalf("清缓存失败时保存仍应成功：%v", err)
	}
	// 校验失败与不存在时不写库、不清缓存
	n := len(r.saved)
	_, _, err := s.Update(ctx, 99, in)
	wantErr(t, err, errs.ErrNotFound, "")
	_, err = s.Create(ctx, AnnouncementInput{Kind: AnnouncementInbox, Content: "x"})
	wantErr(t, err, errs.ErrBadRequest, "站内信")
	if len(r.saved) != n {
		t.Fatal("校验失败或记录不存在时不应写库")
	}
}

func TestAnnouncementList(t *testing.T) {
	r := &fakeAnnouncementRepo{}
	s := newAnnouncementService(r, newMemCache())
	ctx := context.Background()
	if _, err := s.List(ctx, AnnouncementFilter{Q: " 5%折 ", Kind: AnnouncementPopup, Status: StatusActive, PageRequest: PageRequest{Page: 2}}); err != nil {
		t.Fatal(err)
	}
	if r.query != (AnnouncementQuery{Pattern: `%5\%折%`, Kind: AnnouncementPopup, Status: StatusActive, Limit: 20, Offset: 20}) {
		t.Fatalf("查询应转义并分页：%+v", r.query)
	}
	for _, f := range []AnnouncementFilter{{Kind: "x"}, {Status: "x"}, {PageRequest: PageRequest{PageSize: 500}}, {Q: strings.Repeat("词", 51)}} {
		if _, err := s.List(ctx, f); errs.From(err).Code != errs.ErrBadRequest.Code {
			t.Errorf("%+v 应 400，得到 %v", f, err)
		}
	}
}

func idsOf(list []PublicAnnouncement) []int64 {
	out := make([]int64, len(list))
	for i, a := range list {
		out[i] = a.ID
	}
	return out
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
