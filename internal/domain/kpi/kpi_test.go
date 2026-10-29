package kpi

import (
	"strings"
	"testing"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

var sgd = money.MustLookup("SGD")

func m(s string) money.Money {
	x, err := money.Parse(s, sgd)
	if err != nil {
		panic(err)
	}
	return x
}

func ptr[T any](x T) *T { return &x }

// october is a month worked by hand: revenue 10,000.00 of which 9,000.00 subscriptions; gross profit 8,000.00;
// EBITDA 2,000.00; net profit 1,500.00; S&M 3,000.00, R&D 2,000.00, G&A 1,000.00; cost of sales and operating
// expenses 8,000.00; net burn 4,000.00; at the month end cash 120,000.00, receivables 15,000.00, payables 6,000.00,
// current assets 140,000.00 and liabilities 20,000.00, total liabilities 30,000.00, equity 110,000.00; 10 paying
// customers, 2 new, 1 lost, 8 people.
func october() Month {
	return Month{Revenue: m("10000.00"), GrossProfit: m("8000.00"), EBITDA: m("2000.00"), NetProfit: m("1500.00"), Subscription: ptr(m("9000.00")),
		SM: m("3000.00"), RD: m("2000.00"), GA: m("1000.00"), Costs: m("8000.00"), NetBurn: m("4000.00"), Cash: m("120000.00"),
		Receivables: m("15000.00"), Payables: m("6000.00"), CurrentAssets: m("140000.00"), CurrentLiabilities: m("20000.00"),
		TotalLiabilities: m("30000.00"), Equity: m("110000.00"), Counts: &Counts{Paying: 10, New: 2, Lost: 1, Headcount: ptr(int64(8)), HasCustomers: true}}
}

func shown(vs []Value) map[string]string {
	out := map[string]string{}
	for _, v := range vs {
		switch {
		case v.Figure != "":
			out[v.Key] = v.Figure
		case v.Words != "":
			out[v.Key] = v.Words
		default:
			out[v.Key] = "— " + v.Undefined
		}
	}
	return out
}

// Every KPI of the set, worked by hand from october with August and September before it (net burn 2,000.00 and
// 3,000.00; revenue 8,000.00 and 9,000.00; costs 6,000.00 and 7,000.00; September's subscriptions 8,000.00):
// runway 120,000 ÷ 3,000 = 40.0 months; growth 1,000 ÷ 9,000 = 11.1%; churn 1 ÷ (10 − 2 + 1) = 11.1%; LTV 900 × 0.8 ×
// 9 = 6,480.00; LTV:CAC 6,480 ÷ 1,500 = 4.3; payback 1,500 ÷ 720 = 2.1 months; burn multiple 4,000 ÷ 12,000 = 0.3;
// quick ratio 135,000 ÷ 20,000 = 6.75, half away to 6.8; DSO 15,000 ÷ (9,000 ÷ 30) = 50.0 days; DPO 6,000 ÷ (7,000 ÷
// 30) = 25.7 days.
// req: FR-KPI-02
// req: FR-KPI-03
// req: RND-KPI
func TestTheKPIsWorkedByHand(t *testing.T) {
	aug, sep, oct := october(), october(), october()
	aug.NetBurn, aug.Revenue, aug.Costs = m("2000.00"), m("8000.00"), m("6000.00")
	sep.NetBurn, sep.Revenue, sep.Costs, sep.Subscription = m("3000.00"), m("9000.00"), m("7000.00"), ptr(m("8000.00"))
	got := shown(Compute([]Month{aug, sep, oct}, &sep, sgd))
	want := map[string]string{
		"booked_mrr": "9000.00", "contracted_mrr": "— no subscription data", "arr": "108000.00", "revenue_growth": "11.1",
		"gross_margin": "80.0", "ebitda_margin": "20.0", "net_margin": "15.0", "spend_sm": "3000.00", "spend_rd": "2000.00",
		"spend_ga": "1000.00", "net_burn": "4000.00", "runway": "40.0", "arpa": "900.00", "churn": "11.1", "cac": "1500.00",
		"ltv": "6480.00", "ltv_cac": "4.3", "cac_payback": "2.1", "burn_multiple": "0.3", "current_ratio": "7.0", "quick_ratio": "6.8",
		"working_capital": "120000.00", "debt_equity": "0.3", "dso": "50.0", "dpo": "25.7", "headcount": "8", "revenue_per_employee": "15000.00",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
	if len(got) != len(Definitions) || len(want) != len(Definitions) {
		t.Errorf("%d values, %d expected, %d definitions", len(got), len(want), len(Definitions))
	}
	for _, v := range Compute([]Month{aug, sep, oct}, &sep, sgd) {
		if v.Entered != (v.Key == "arpa" || v.Key == "churn" || v.Key == "cac" || v.Key == "ltv" || v.Key == "ltv_cac" || v.Key == "cac_payback" ||
			v.Key == "headcount" || v.Key == "revenue_per_employee") {
			t.Errorf("%s entered = %v", v.Key, v.Entered)
		}
	}
}

// G4-047: without customer counts, churn, ARPA, LTV and CAC show "—" with "no subscription data", never 0, and the
// ledger KPIs still show (rev 0.161: with counts entered they show, as above). Without a headcount, headcount and
// revenue per employee have none either.
// req: FR-KPI-03
// req: G4-047
func TestWithoutCountsTheCustomerKPIsHaveNone(t *testing.T) {
	oct := october()
	oct.Counts = nil
	got := shown(Compute([]Month{oct}, nil, sgd))
	for _, k := range []string{"churn", "arpa", "ltv", "cac", "ltv_cac", "cac_payback"} {
		if !strings.HasPrefix(got[k], "— no subscription data") {
			t.Errorf("%s = %q", k, got[k])
		}
	}
	for _, k := range []string{"headcount", "revenue_per_employee"} {
		if got[k] != "— "+NoHeadcount {
			t.Errorf("%s = %q", k, got[k])
		}
	}
	if got["gross_margin"] != "80.0" || got["runway"] != "30.0" || got["revenue_growth"] != "— "+NoMonthBefore || got["burn_multiple"] != "— "+NoMonthBefore {
		t.Errorf("the ledger KPIs: %v", got)
	}
}

// G4-048: equity at or below zero in a month that generates cash: debt/equity is blank, runway says "Not burning", and
// the burn multiple is blank. A month without revenue has no margins; one without current liabilities no ratios.
// req: FR-KPI-03
// req: G4-048
func TestTheWorkbooksUndefinedStatesCarryOver(t *testing.T) {
	oct := october()
	oct.Equity, oct.NetBurn = m("-5000.00"), m("-2500.00")
	got := shown(Compute([]Month{oct}, nil, sgd))
	if got["debt_equity"] != "— "+NoEquity || got["runway"] != "Not burning" || got["burn_multiple"] != "— "+NotBurning || got["net_burn"] != "-2500.00" {
		t.Errorf("G4-048: %v", got)
	}
	idle := october()
	idle.Revenue, idle.GrossProfit, idle.EBITDA, idle.NetProfit, idle.CurrentLiabilities, idle.Costs = money.Zero(sgd), money.Zero(sgd), money.Zero(sgd),
		money.Zero(sgd), money.Zero(sgd), money.Zero(sgd)
	got = shown(Compute([]Month{idle}, nil, sgd))
	for k, w := range map[string]string{"gross_margin": NoRevenue, "net_margin": NoRevenue, "current_ratio": NoLiabilities, "quick_ratio": NoLiabilities,
		"dso": NoAverage, "dpo": NoAverage, "ltv": NoRevenue} {
		if got[k] != "— "+w {
			t.Errorf("%s = %q, want — %s", k, got[k], w)
		}
	}
	if got["working_capital"] != "140000.00" {
		t.Errorf("working capital = %q", got["working_capital"])
	}
	// No subscription account in the chart: no MRR, ARR, ARPA or burn multiple, and a churn of 0 leaves the LTV unbounded.
	plain := october()
	plain.Subscription = nil
	plain.Counts.Lost = 0
	got = shown(Compute([]Month{plain}, nil, sgd))
	for k, w := range map[string]string{"booked_mrr": NoSubscription, "arr": NoSubscription, "arpa": NoSubscription, "churn": "0.0"} {
		if got[k] != w && got[k] != "— "+w {
			t.Errorf("%s = %q, want %s", k, got[k], w)
		}
	}
	withMRR := october()
	withMRR.Counts.Lost = 0
	if got := shown(Compute([]Month{withMRR}, nil, sgd)); got["ltv"] != "— "+NoChurn {
		t.Errorf("ltv with no churn = %q", got["ltv"])
	}
}

// Every definition is complete: a key once, a version naming it, a unit, a source, a formula and a date basis; and
// every KPI that can be undefined says when.
// req: FR-KPI-03
func TestEveryDefinitionStatesItself(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Definitions {
		if seen[d.Key] || !strings.HasPrefix(d.Version, d.Key+"@") || d.Unit == "" || d.Source == "" || d.Formula == "" || d.DateBasis == "" || d.Name == "" {
			t.Errorf("an incomplete definition: %+v", d)
		}
		seen[d.Key] = true
	}
}
