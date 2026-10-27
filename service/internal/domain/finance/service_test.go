package finance

import (
	"context"
	"strings"
	"testing"
	"time"
)

type fakeRepo struct {
	entry      Entry
	mem        Membership
	savedIn    Input
	savedMonth time.Time
}

func (f *fakeRepo) GetMonth(_ context.Context, _ time.Time) (Entry, error) { return f.entry, nil }

func (f *fakeRepo) SaveMonth(_ context.Context, m time.Time, in Input) (Entry, error) {
	f.savedIn, f.savedMonth = in, m
	f.entry = Entry{Input: in, Saved: true}
	return f.entry, nil
}

func (f *fakeRepo) MembershipRevenue(_ context.Context, _, _ time.Time) (Membership, error) {
	return f.mem, nil
}

func TestParseMonth(t *testing.T) {
	svc := NewService(&fakeRepo{})
	got, err := svc.parseMonth("2026-10")
	if err != nil {
		t.Fatal(err)
	}
	if got.Year() != 2026 || got.Month() != time.October || got.Day() != 1 {
		t.Fatalf("月初解析不对: %v", got)
	}
	for _, bad := range []string{"", "2026-13", "2026/10", "1999-05", "2101-01", "abc"} {
		if _, err := svc.parseMonth(bad); err == nil {
			t.Fatalf("%q 应报错", bad)
		}
	}
}

func TestReportGrossProfitAndTotals(t *testing.T) {
	repo := &fakeRepo{
		entry: Entry{
			Input: Input{AdRevenue: 100_000, ReferralRevenue: 50_000, TrafficCost: 30_000, ServerCost: 20_000, PaymentFee: 10_000},
			Saved: true,
		},
		mem: Membership{ByChannel: []ChannelRevenue{
			{Channel: "alipay", Currency: "CNY", Orders: 2, Gross: 20_000, Refund: 5_000},
			{Channel: "wechat", Currency: "CNY", Orders: 1, Gross: 10_000, Refund: 0},
			{Channel: "usdt", Currency: "USDT", Orders: 1, Gross: 5_000_000, Refund: 0},
		}},
	}
	rep, err := NewService(repo).Report(context.Background(), "2026-10")
	if err != nil {
		t.Fatal(err)
	}
	// 按币种总额：CNY orders=3 gross=30000 refund=5000 net=25000；USDT net=5000000。
	var cny, usdt *CurrencyTotal
	for i := range rep.Membership.Totals {
		switch rep.Membership.Totals[i].Currency {
		case "CNY":
			cny = &rep.Membership.Totals[i]
		case "USDT":
			usdt = &rep.Membership.Totals[i]
		}
	}
	if cny == nil || cny.Orders != 3 || cny.Gross != 30_000 || cny.Refund != 5_000 || cny.Net != 25_000 {
		t.Fatalf("CNY 总额不对: %+v", cny)
	}
	if usdt == nil || usdt.Net != 5_000_000 {
		t.Fatalf("USDT 总额不对: %+v", usdt)
	}
	// 毛利（CNY 口径）= 100000 + 50000 + 25000(会员CNY净) − 30000 − 20000 − 10000 = 115000；USDT 不并入。
	if rep.GrossProfitFen != 115_000 {
		t.Fatalf("毛利应 115000, 得 %d", rep.GrossProfitFen)
	}
}

func TestSaveValidateAndTrim(t *testing.T) {
	svc := NewService(&fakeRepo{})
	ctx := context.Background()

	if _, err := svc.Save(ctx, "2026-10", Input{AdRevenue: -1}); err == nil {
		t.Fatal("负金额应报错")
	}
	if _, err := svc.Save(ctx, "2026-10", Input{ServerCost: maxAmountFen + 1}); err == nil {
		t.Fatal("超上限金额应报错")
	}
	if _, err := svc.Save(ctx, "2026-10", Input{AdNote: strings.Repeat("字", maxNoteLen+1)}); err == nil {
		t.Fatal("备注超长应报错")
	}

	repo := &fakeRepo{}
	rep, err := NewService(repo).Save(ctx, "2026-10", Input{AdRevenue: 12_300, AdNote: "  渠道结算  "})
	if err != nil {
		t.Fatal(err)
	}
	if repo.savedIn.AdNote != "渠道结算" {
		t.Fatalf("备注未去首尾空白: %q", repo.savedIn.AdNote)
	}
	if !rep.Entry.Saved || rep.Entry.AdRevenue != 12_300 {
		t.Fatalf("保存后月报不对: %+v", rep.Entry)
	}
}

func TestReportDefaultsToCurrentMonth(t *testing.T) {
	svc := NewService(&fakeRepo{})
	rep, err := svc.Report(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Month != svc.currentMonth() || !monthRe.MatchString(rep.Month) {
		t.Fatalf("空月份应回退当前北京月, 得 %q", rep.Month)
	}
}
