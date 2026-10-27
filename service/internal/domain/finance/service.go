package finance

import (
	"context"
	"regexp"
	"strconv"
	"time"

	"xsite/service/internal/errs"
)

// Repo 读写月度手填收支，并按月汇总已支付订单的会员收入。
type Repo interface {
	// GetMonth 读某月（monthStart 为北京月初）的手填收支；没有记录返回 Entry{Saved:false}。
	GetMonth(ctx context.Context, monthStart time.Time) (Entry, error)
	// SaveMonth upsert 某月手填收支，返回保存后的 Entry。
	SaveMonth(ctx context.Context, monthStart time.Time, in Input) (Entry, error)
	// MembershipRevenue 汇总 [from, to) 内已支付的 vip_plan 订单（按渠道、按套餐、分币种）。
	MembershipRevenue(ctx context.Context, from, to time.Time) (Membership, error)
}

// Service 是收入与成本。
type Service struct {
	repo Repo
	loc  *time.Location
}

// NewService 构造服务。北京时区解析失败（容器缺 tzdata）时退回固定 +8。
func NewService(repo Repo) *Service {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.FixedZone("CST", 8*3600)
	}
	return &Service{repo: repo, loc: loc}
}

var monthRe = regexp.MustCompile(`^\d{4}-\d{2}$`)

// currentMonth 返回当前北京月的 "YYYY-MM"。
func (s *Service) currentMonth() string {
	return time.Now().In(s.loc).Format("2006-01")
}

// parseMonth 把 "YYYY-MM" 解析为北京月初（当月 1 号 00:00 +08）。
func (s *Service) parseMonth(month string) (time.Time, error) {
	if !monthRe.MatchString(month) {
		return time.Time{}, errs.ErrBadRequest.WithMessage("月份格式应为 YYYY-MM")
	}
	y, _ := strconv.Atoi(month[:4])
	m, _ := strconv.Atoi(month[5:7])
	if m < 1 || m > 12 || y < 2000 || y > 2100 {
		return time.Time{}, errs.ErrBadRequest.WithMessage("月份超出范围")
	}
	return time.Date(y, time.Month(m), 1, 0, 0, 0, 0, s.loc), nil
}

// Report 返回某月的收入与成本月报（手填 + 会员汇总 + 毛利）。
func (s *Service) Report(ctx context.Context, month string) (Report, error) {
	if month == "" {
		month = s.currentMonth()
	}
	start, err := s.parseMonth(month)
	if err != nil {
		return Report{}, err
	}
	entry, err := s.repo.GetMonth(ctx, start)
	if err != nil {
		return Report{}, err
	}
	mem, err := s.repo.MembershipRevenue(ctx, start.UTC(), start.AddDate(0, 1, 0).UTC())
	if err != nil {
		return Report{}, err
	}
	mem.Totals = totalsFrom(mem.ByChannel)
	return Report{Month: month, Entry: entry, Membership: mem, GrossProfitFen: grossProfit(entry.Input, mem.Totals)}, nil
}

// Save 校验并 upsert 某月手填收支，返回保存后的月报。
func (s *Service) Save(ctx context.Context, month string, in Input) (Report, error) {
	if month == "" {
		month = s.currentMonth()
	}
	start, err := s.parseMonth(month)
	if err != nil {
		return Report{}, err
	}
	in.trim()
	if err := in.validate(); err != nil {
		return Report{}, err
	}
	if _, err := s.repo.SaveMonth(ctx, start, in); err != nil {
		return Report{}, err
	}
	return s.Report(ctx, month)
}

// totalsFrom 把按渠道明细累加成按币种总额（保持币种首次出现的顺序）。
func totalsFrom(chs []ChannelRevenue) []CurrencyTotal {
	idx := map[string]int{}
	out := []CurrencyTotal{}
	for _, c := range chs {
		i, ok := idx[c.Currency]
		if !ok {
			i = len(out)
			idx[c.Currency] = i
			out = append(out, CurrencyTotal{Currency: c.Currency})
		}
		out[i].Orders += c.Orders
		out[i].Gross += c.Gross
		out[i].Refund += c.Refund
	}
	for i := range out {
		out[i].Net = out[i].Gross - out[i].Refund
	}
	return out
}

// grossProfit 人民币口径毛利（分）：广告 + 导流 + 会员 CNY 净收入 − 流量 − 服务器 − 支付手续费。
func grossProfit(in Input, totals []CurrencyTotal) int64 {
	var cnyNet int64
	for _, t := range totals {
		if t.Currency == "CNY" {
			cnyNet = t.Net
		}
	}
	return in.AdRevenue + in.ReferralRevenue + cnyNet - in.TrafficCost - in.ServerCost - in.PaymentFee
}
