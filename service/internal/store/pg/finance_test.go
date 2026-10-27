package pg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"xsite/service/internal/domain/finance"
)

// 月度手填收支走真实库：未保存返回 Saved=false，upsert 插入后读回逐列核对、再次保存验证覆盖。
// 用 nonce 派生的偏僻年份隔离 finance_monthly（全局主键 month），并行与复跑都不互相覆盖。
func TestFinanceRepo_MonthlyUpsert(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewFinanceRepo(pool)

	yr := 1900 + int(time.Now().UnixNano()%100)
	month := time.Date(yr, 5, 1, 0, 0, 0, 0, time.UTC)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM finance_monthly WHERE month = $1`, pgDay(month))
	})

	if e0, err := repo.GetMonth(ctx, month); err != nil {
		t.Fatal(err)
	} else if e0.Saved {
		t.Fatalf("未保存的月应 Saved=false")
	}

	in := finance.Input{
		AdRevenue: 100000, ReferralRevenue: 50000, TrafficCost: 30000, ServerCost: 20000, PaymentFee: 10000,
		AdNote: "广告", ReferralNote: "导流", ServerNote: "服务器", PaymentNote: "手续费",
	}
	if _, err := repo.SaveMonth(ctx, month, in); err != nil {
		t.Fatal(err)
	}
	e1, err := repo.GetMonth(ctx, month)
	if err != nil {
		t.Fatal(err)
	}
	if !e1.Saved || e1.AdRevenue != 100000 || e1.ReferralRevenue != 50000 || e1.TrafficCost != 30000 ||
		e1.ServerCost != 20000 || e1.PaymentFee != 10000 || e1.AdNote != "广告" || e1.PaymentNote != "手续费" || e1.UpdatedAt == nil {
		t.Fatalf("保存后读回不符: %+v", e1)
	}

	in.AdRevenue = 999
	in.AdNote = "改了"
	if _, err := repo.SaveMonth(ctx, month, in); err != nil {
		t.Fatal(err)
	}
	e2, err := repo.GetMonth(ctx, month)
	if err != nil {
		t.Fatal(err)
	}
	if e2.AdRevenue != 999 || e2.AdNote != "改了" || e2.ServerCost != 20000 {
		t.Fatalf("upsert 未正确更新: %+v", e2)
	}
}

// 会员收入汇总走真实库：造 1 用户 + 多笔 vip_plan 订单，验证按渠道 / 按套餐 / 分币种聚合，
// 未支付（paid_at IS NULL）被排除，已退款仍计入（按原支付月，refund 记退款）。
// 订单 paid_at 落在 nonce 派生的偏僻年份 6 月，窗口只含本测试数据，断言对并行鲁棒。
func TestFinanceRepo_MembershipRevenue(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewFinanceRepo(pool)

	nonce := time.Now().UnixNano()
	yr := 1900 + int(nonce%100)
	var uid, monthPlan, yearPlan int64
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (username, password_hash) VALUES ($1,'x') RETURNING id`,
		fmt.Sprintf("tf%d", nonce)).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM vip_plan WHERE code='month'`).Scan(&monthPlan); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM vip_plan WHERE code='year'`).Scan(&yearPlan); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM pay_order WHERE user_id = $1`, uid)
		_, _ = pool.Exec(bg, `DELETE FROM "user" WHERE id = $1`, uid)
	})

	i := 0
	mk := func(channel, currency string, plan, amount, refund int64, status string, paidAt any) {
		i++
		if _, err := pool.Exec(ctx,
			`INSERT INTO pay_order (order_no, user_id, product_type, product_id, product_snapshot, channel, client,
			                        currency, amount, status, paid_at, refund_amount, idempotency_key)
			 VALUES ($1,$2,'vip_plan',$3,'{}'::jsonb,$4,'web_pc',$5,$6,$7,$8,$9,$10)`,
			fmt.Sprintf("fin-ord-%d-%04d", nonce, i), uid, plan, channel, currency, amount, status, paidAt, refund,
			fmt.Sprintf("idem-%d-%d", nonce, i)); err != nil {
			t.Fatal(err)
		}
	}
	day := func(d int) time.Time { return time.Date(yr, 6, d, 8, 0, 0, 0, time.UTC) }
	mk("alipay", "CNY", monthPlan, 10000, 0, "paid", day(10))
	mk("wechat", "CNY", monthPlan, 10000, 2000, "paid", day(11))
	mk("alipay", "USDT", yearPlan, 5000000, 0, "paid", day(12))
	mk("alipay", "CNY", monthPlan, 10000, 10000, "refunded", day(13)) // 退款仍计入原支付月
	mk("alipay", "CNY", monthPlan, 10000, 0, "pending", nil)          // 未支付，应排除

	from := time.Date(yr, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(yr, 7, 1, 0, 0, 0, 0, time.UTC)
	mem, err := repo.MembershipRevenue(ctx, from, to)
	if err != nil {
		t.Fatal(err)
	}

	ch := map[string]finance.ChannelRevenue{}
	for _, c := range mem.ByChannel {
		ch[c.Channel+"/"+c.Currency] = c
	}
	if c := ch["alipay/CNY"]; c.Orders != 2 || c.Gross != 20000 || c.Refund != 10000 {
		t.Fatalf("alipay/CNY 应 orders=2 gross=20000 refund=10000, 得 %+v", c)
	}
	if c := ch["wechat/CNY"]; c.Orders != 1 || c.Gross != 10000 || c.Refund != 2000 {
		t.Fatalf("wechat/CNY 应 orders=1 gross=10000 refund=2000, 得 %+v", c)
	}
	if c := ch["alipay/USDT"]; c.Orders != 1 || c.Gross != 5000000 {
		t.Fatalf("alipay/USDT 应 orders=1 gross=5000000, 得 %+v", c)
	}

	pl := map[string]finance.PlanRevenue{}
	for _, p := range mem.ByPlan {
		pl[p.PlanCode+"/"+p.Currency] = p
	}
	if p := pl["month/CNY"]; p.Orders != 3 || p.Gross != 30000 || p.Refund != 12000 {
		t.Fatalf("month/CNY 应 orders=3 gross=30000 refund=12000, 得 %+v", p)
	}
	if p := pl["year/USDT"]; p.Orders != 1 || p.Gross != 5000000 {
		t.Fatalf("year/USDT 应 orders=1 gross=5000000, 得 %+v", p)
	}
}
