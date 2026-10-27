// Package finance 是运营后台「收入与成本」（docs/06 第 23 节，A4-1 / S4-3）。
// 运营按月手填收支（人民币分），会员收入由后台从已支付订单按月自动汇总；毛利按人民币口径核算，外币单列。
package finance

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

const (
	// maxAmountFen 单项金额上限（100 亿元），防手滑录入天文数字。
	maxAmountFen = int64(1_000_000_000_000)
	maxNoteLen   = 500
)

// Input 是运营按月手填的收支（人民币分）。
type Input struct {
	AdRevenue       int64
	ReferralRevenue int64
	TrafficCost     int64
	ServerCost      int64
	PaymentFee      int64
	AdNote          string
	ReferralNote    string
	TrafficNote     string
	ServerNote      string
	PaymentNote     string
}

func (in *Input) trim() {
	in.AdNote = strings.TrimSpace(in.AdNote)
	in.ReferralNote = strings.TrimSpace(in.ReferralNote)
	in.TrafficNote = strings.TrimSpace(in.TrafficNote)
	in.ServerNote = strings.TrimSpace(in.ServerNote)
	in.PaymentNote = strings.TrimSpace(in.PaymentNote)
}

func (in Input) validate() error {
	for _, v := range []int64{in.AdRevenue, in.ReferralRevenue, in.TrafficCost, in.ServerCost, in.PaymentFee} {
		if v < 0 || v > maxAmountFen {
			return errs.ErrBadRequest.WithMessage("金额应在 0 到 100 亿元之间")
		}
	}
	for _, s := range []string{in.AdNote, in.ReferralNote, in.TrafficNote, in.ServerNote, in.PaymentNote} {
		if utf8.RuneCountInString(s) > maxNoteLen {
			return errs.ErrBadRequest.WithMessage(fmt.Sprintf("备注最多 %d 个字", maxNoteLen))
		}
	}
	return nil
}

// Entry 是某月的手填收支（含是否保存过与更新时间）。
type Entry struct {
	Input
	Saved     bool
	UpdatedAt *time.Time
}

// ChannelRevenue 是某渠道某币种的会员订单汇总。
type ChannelRevenue struct {
	Channel  string
	Currency string
	Orders   int64
	Gross    int64 // 原始单位：CNY 为分，USDT 为 micro-USDT
	Refund   int64
}

// PlanRevenue 是某套餐某币种的会员订单汇总。
type PlanRevenue struct {
	PlanCode string
	Currency string
	Orders   int64
	Gross    int64
	Refund   int64
}

// CurrencyTotal 是某币种的会员收入总额（净额 = 毛额 − 退款）。
type CurrencyTotal struct {
	Currency string
	Orders   int64
	Gross    int64
	Refund   int64
	Net      int64
}

// Membership 是会员收入自动汇总（按币种总额 + 按渠道 + 按套餐）。
type Membership struct {
	Totals    []CurrencyTotal
	ByChannel []ChannelRevenue
	ByPlan    []PlanRevenue
}

// Report 是收入与成本月报：手填收支 + 会员收入汇总 + 人民币口径毛利。
type Report struct {
	Month      string // YYYY-MM
	Entry      Entry
	Membership Membership
	// GrossProfitFen 人民币口径毛利（分）：广告 + 导流 + 会员 CNY 净收入 − 流量 − 服务器 − 支付手续费。
	// 外币（USDT）会员收入不并入，单列在 Membership。
	GrossProfitFen int64
}
