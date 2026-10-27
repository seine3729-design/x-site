package finance

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/finance"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// 金额单位一律人民币分（fen），与订单一致；前端负责元 ↔ 分换算。会员收入 gross/refund 为订单原始单位
// （CNY 为分、USDT 为 micro-USDT），由 currency 标明，前端按币种格式化。

type saveRequest struct {
	AdRevenue       int64  `json:"ad_revenue"`
	ReferralRevenue int64  `json:"referral_revenue"`
	TrafficCost     int64  `json:"traffic_cost"`
	ServerCost      int64  `json:"server_cost"`
	PaymentFee      int64  `json:"payment_fee"`
	AdNote          string `json:"ad_note"`
	ReferralNote    string `json:"referral_note"`
	TrafficNote     string `json:"traffic_note"`
	ServerNote      string `json:"server_note"`
	PaymentNote     string `json:"payment_note"`
}

func (r saveRequest) input() dom.Input {
	return dom.Input{
		AdRevenue: r.AdRevenue, ReferralRevenue: r.ReferralRevenue, TrafficCost: r.TrafficCost,
		ServerCost: r.ServerCost, PaymentFee: r.PaymentFee,
		AdNote: r.AdNote, ReferralNote: r.ReferralNote, TrafficNote: r.TrafficNote,
		ServerNote: r.ServerNote, PaymentNote: r.PaymentNote,
	}
}

type entryDTO struct {
	AdRevenue       int64      `json:"ad_revenue"`
	ReferralRevenue int64      `json:"referral_revenue"`
	TrafficCost     int64      `json:"traffic_cost"`
	ServerCost      int64      `json:"server_cost"`
	PaymentFee      int64      `json:"payment_fee"`
	AdNote          string     `json:"ad_note"`
	ReferralNote    string     `json:"referral_note"`
	TrafficNote     string     `json:"traffic_note"`
	ServerNote      string     `json:"server_note"`
	PaymentNote     string     `json:"payment_note"`
	Saved           bool       `json:"saved"`
	UpdatedAt       *time.Time `json:"updated_at"`
}

type currencyTotalDTO struct {
	Currency string `json:"currency"`
	Orders   int64  `json:"orders"`
	Gross    int64  `json:"gross"`
	Refund   int64  `json:"refund"`
	Net      int64  `json:"net"`
}

type channelRevenueDTO struct {
	Channel  string `json:"channel"`
	Currency string `json:"currency"`
	Orders   int64  `json:"orders"`
	Gross    int64  `json:"gross"`
	Refund   int64  `json:"refund"`
}

type planRevenueDTO struct {
	PlanCode string `json:"plan_code"`
	Currency string `json:"currency"`
	Orders   int64  `json:"orders"`
	Gross    int64  `json:"gross"`
	Refund   int64  `json:"refund"`
}

type membershipDTO struct {
	Totals    []currencyTotalDTO  `json:"totals"`
	ByChannel []channelRevenueDTO `json:"by_channel"`
	ByPlan    []planRevenueDTO    `json:"by_plan"`
}

type reportDTO struct {
	Month          string        `json:"month"`
	Entry          entryDTO      `json:"entry"`
	Membership     membershipDTO `json:"membership"`
	GrossProfitFen int64         `json:"gross_profit_fen"`
}

func toReportDTO(r dom.Report) reportDTO {
	return reportDTO{
		Month: r.Month,
		Entry: entryDTO{
			AdRevenue: r.Entry.AdRevenue, ReferralRevenue: r.Entry.ReferralRevenue, TrafficCost: r.Entry.TrafficCost,
			ServerCost: r.Entry.ServerCost, PaymentFee: r.Entry.PaymentFee,
			AdNote: r.Entry.AdNote, ReferralNote: r.Entry.ReferralNote, TrafficNote: r.Entry.TrafficNote,
			ServerNote: r.Entry.ServerNote, PaymentNote: r.Entry.PaymentNote,
			Saved: r.Entry.Saved, UpdatedAt: r.Entry.UpdatedAt,
		},
		Membership: membershipDTO{
			Totals:    totalsDTO(r.Membership.Totals),
			ByChannel: channelsDTO(r.Membership.ByChannel),
			ByPlan:    plansDTO(r.Membership.ByPlan),
		},
		GrossProfitFen: r.GrossProfitFen,
	}
}

func totalsDTO(in []dom.CurrencyTotal) []currencyTotalDTO {
	out := make([]currencyTotalDTO, 0, len(in))
	for _, t := range in {
		out = append(out, currencyTotalDTO{Currency: t.Currency, Orders: t.Orders, Gross: t.Gross, Refund: t.Refund, Net: t.Net})
	}
	return out
}

func channelsDTO(in []dom.ChannelRevenue) []channelRevenueDTO {
	out := make([]channelRevenueDTO, 0, len(in))
	for _, c := range in {
		out = append(out, channelRevenueDTO{Channel: c.Channel, Currency: c.Currency, Orders: c.Orders, Gross: c.Gross, Refund: c.Refund})
	}
	return out
}

func plansDTO(in []dom.PlanRevenue) []planRevenueDTO {
	out := make([]planRevenueDTO, 0, len(in))
	for _, p := range in {
		out = append(out, planRevenueDTO{PlanCode: p.PlanCode, Currency: p.Currency, Orders: p.Orders, Gross: p.Gross, Refund: p.Refund})
	}
	return out
}

// Get 是 GET /api/admin/finance?month=：某月收入与成本（month 为空取当前北京月）。
func (h *Handlers) Get(c *gin.Context) {
	rep, err := h.Svc.Report(c.Request.Context(), c.Query("month"))
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, toReportDTO(rep))
}

// Save 是 PUT /api/admin/finance?month=：整体保存某月手填收支。
func (h *Handlers) Save(c *gin.Context) {
	var req saveRequest
	if !admin.Bind(c, &req) {
		return
	}
	rep, err := h.Svc.Save(c.Request.Context(), c.Query("month"), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "finance.save", TargetType: "finance", TargetID: rep.Month, After: rep.Entry})
	respond.OK(c, toReportDTO(rep))
}
