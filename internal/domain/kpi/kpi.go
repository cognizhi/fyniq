// Package kpi holds the workbook's KPI set as versioned definitions (PRD-001 FR-KPI-02, FR-KPI-03; p1-dashboard-kpis
// tasks 1.2, design D1): each with its source, formula, date basis, unit and undefined states, computed for a month
// from exact figures the balance service gives through the statements, and from the month's entered customer counts
// and headcount (rev 0.161). Every value is rounded once by RND-KPI; a KPI that cannot be worked out has no value,
// only its reason, never 0. The definitions follow the workbook's own (zh-sheet-accounting PRD §11.5).
package kpi

import (
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
)

// Unit is what a KPI's value measures, which decides its decimals (RND-KPI).
type Unit string

// The units.
const (
	Amount  Unit = "amount"  // the base currency's minor units
	Percent Unit = "percent" // 1 decimal
	Ratio   Unit = "ratio"   // a ratio or a multiple, 1 decimal
	Months  Unit = "months"  // 1 decimal
	Days    Unit = "days"    // 1 decimal
	Count   Unit = "count"   // whole
)

func (u Unit) places(base money.Currency) int {
	switch u {
	case Amount:
		return base.MinorUnits
	case Count:
		return 0
	}
	return 1
}

// The sources a KPI reads (FR-KPI-03).
const (
	Ledger  = "ledger"  // the balance service, through the statements
	Entered = "entered" // the month's customer counts or headcount, entered by a person (rev 0.161)
	Both    = "ledger and entered"
	Pending = "subscription data" // FR-AR-15 and FR-KPI-04, in P2
)

// Definition is one KPI as code: what it is, how it is worked out, from what, and when it cannot be.
type Definition struct {
	Key       string
	Name      string
	Version   string
	Unit      Unit
	Source    string
	Formula   string // in words, as the tile shows it on hover
	DateBasis string
	Undefined string // when it has no value, in words
}

// The reasons a KPI has no value.
const (
	NoRevenue       = "no revenue in the month"
	NoMonthBefore   = "the books have no month before"
	NoRevenueBefore = "no revenue the month before"
	NoSubscription  = "no subscription revenue account in the chart"
	NoSubscriptions = "no subscription data"
	NoCounts        = "no subscription data: enter the month's customer counts"
	NoPaying        = "no paying customers"
	NoStart         = "no customers at the start of the month"
	NoNew           = "no new customers"
	NoChurn         = "no customers lost, so a lifetime is not bounded"
	NoMargin        = "no gross profit per customer"
	NotBurning      = "not burning cash"
	NoGrowth        = "ARR did not grow"
	NoLiabilities   = "no current liabilities"
	NoEquity        = "equity is not above zero"
	NoAverage       = "nothing to average over the last three months"
	NoHeadcount     = "no headcount entered for the month"
	NoPeople        = "a headcount of zero"
)

// Definitions are the KPI set in the order the KPI page shows it (FR-KPI-02). A change of formula is a new version.
var Definitions = []Definition{
	{"cash", "Cash", "cash@1", Amount, Ledger, "Cash in the money accounts at the month end", "the month end", ""},
	{"revenue", "Revenue", "revenue@1", Amount, Ledger, "Revenue recognised in the month", "the accounting month", ""},
	{"net_profit_ytd", "Net profit, year to date", "net_profit_ytd@1", Amount, Ledger, "Net profit from the financial year's first day to the month end", "the financial year to the month end", ""},
	{"owed_to_us", "Owed to us", "owed_to_us@1", Amount, Ledger, "Receivables at the month end", "the month end", ""},
	{"owed_by_us", "We owe", "owed_by_us@1", Amount, Ledger, "Payables and accruals at the month end", "the month end", ""},
	{"booked_mrr", "Booked MRR", "booked_mrr@1", Amount, Ledger, "Subscription revenue recognised in the month: revenue accounts whose report group is a subscription stream", "the accounting month", NoSubscription},
	{"contracted_mrr", "Contracted MRR", "contracted_mrr@1", Amount, Pending, "The monthly value of active subscriptions at month end", "the month end", NoSubscriptions},
	{"arr", "ARR (booked)", "arr@1", Amount, Ledger, "Booked MRR × 12", "the accounting month", NoSubscription},
	{"revenue_growth", "Revenue growth", "revenue_growth@1", Percent, Ledger, "(Revenue − revenue the month before) ÷ revenue the month before", "the accounting month and the one before", NoRevenueBefore},
	{"gross_margin", "Gross margin", "gross_margin@1", Percent, Ledger, "Gross profit ÷ revenue", "the accounting month", NoRevenue},
	{"ebitda_margin", "EBITDA margin", "ebitda_margin@1", Percent, Ledger, "EBITDA ÷ revenue; EBITDA is operating profit before depreciation and amortisation, without other income", "the accounting month", NoRevenue},
	{"net_margin", "Net margin", "net_margin@1", Percent, Ledger, "Net profit ÷ revenue", "the accounting month", NoRevenue},
	{"spend_sm", "Spend on sales & marketing", "spend_sm@1", Amount, Ledger, "The month's expenses of accounts whose function is S&M", "the accounting month", ""},
	{"spend_rd", "Spend on research & development", "spend_rd@1", Amount, Ledger, "The month's expenses of accounts whose function is R&D", "the accounting month", ""},
	{"spend_ga", "Spend on general & administrative", "spend_ga@1", Amount, Ledger, "The month's expenses of accounts whose function is G&A", "the accounting month", ""},
	{"net_burn", "Net burn", "net_burn@1", Amount, Ledger, "−(operating + investing cash flow); below zero in a month that generates cash", "the accounting month", ""},
	{"runway", "Runway", "runway@1", Months, Ledger, "Cash at month end ÷ the average net burn of the last three months of the financial year; \"Not burning\" when that average is zero or less", "the month end and the last three months", NoAverage},
	{"arpa", "ARPA", "arpa@1", Amount, Both, "Booked MRR ÷ paying customers", "the accounting month", NoCounts},
	{"churn", "Customer churn", "churn@1", Percent, Entered, "Customers lost ÷ customers at the start of the month (paying − new + lost)", "the accounting month", NoCounts},
	{"cac", "CAC", "cac@1", Amount, Both, "Spend on sales & marketing ÷ new customers", "the accounting month", NoCounts},
	{"ltv", "LTV", "ltv@1", Amount, Both, "ARPA × gross margin ÷ churn", "the accounting month", NoChurn},
	{"ltv_cac", "LTV : CAC", "ltv_cac@1", Ratio, Both, "LTV ÷ CAC (3 or more is healthy)", "the accounting month", NoChurn},
	{"cac_payback", "CAC payback", "cac_payback@1", Months, Both, "CAC ÷ (ARPA × gross margin)", "the accounting month", NoMargin},
	{"burn_multiple", "Burn multiple", "burn_multiple@1", Ratio, Ledger, "Net burn ÷ net new ARR (booked MRR's change × 12); blank when not burning", "the accounting month and the one before", NotBurning},
	{"current_ratio", "Current ratio", "current_ratio@1", Ratio, Ledger, "Current assets ÷ current liabilities", "the month end", NoLiabilities},
	{"quick_ratio", "Quick ratio", "quick_ratio@1", Ratio, Ledger, "(Cash + receivables) ÷ current liabilities", "the month end", NoLiabilities},
	{"working_capital", "Working capital", "working_capital@1", Amount, Ledger, "Current assets − current liabilities", "the month end", ""},
	{"debt_equity", "Debt / equity", "debt_equity@1", Ratio, Ledger, "Total liabilities ÷ equity; blank when equity is zero or less", "the month end", NoEquity},
	{"dso", "DSO", "dso@1", Days, Ledger, "Receivables ÷ (the last three months' average revenue ÷ 30)", "the month end and the last three months", NoAverage},
	{"dpo", "DPO", "dpo@1", Days, Ledger, "Payables and accruals ÷ (the last three months' average cost of sales and operating expenses ÷ 30)", "the month end and the last three months", NoAverage},
	{"headcount", "Headcount", "headcount@1", Count, Entered, "People employed in the month, as entered until the payroll link", "the accounting month", NoHeadcount},
	{"revenue_per_employee", "Revenue per employee", "revenue_per_employee@1", Amount, Both, "Revenue × 12 ÷ headcount", "the accounting month", NoHeadcount},
}

// Counts are a month's customer counts and headcount as a person entered them (rev 0.161).
type Counts struct {
	Paying, New, Lost int64
	Headcount         *int64 // nil: not entered
	EnteredBy         string
	EnteredAt         time.Time
	HasCustomers      bool // the customer counts were entered, not only the headcount
}

// Month is one month's exact figures, in the base currency, signed as the statements show them: revenue and profit
// positive, expenses positive, assets, liabilities and equity positive.
type Month struct {
	Start                       time.Time
	Revenue, GrossProfit        money.Money
	EBITDA, NetProfit           money.Money
	NetProfitYTD                money.Money  // from the financial year's first day to the month end
	Subscription                *money.Money // nil: the chart has no subscription revenue account
	SM, RD, GA                  money.Money
	Costs                       money.Money // cost of sales and operating expenses
	NetBurn                     money.Money // −(operating + investing cash flow)
	Cash, Receivables, Payables money.Money // at the month end
	CurrentAssets               money.Money
	CurrentLiabilities          money.Money
	TotalLiabilities, Equity    money.Money
	Counts                      *Counts
}

// Value is a KPI's value for a month: its figure rounded by RND-KPI, or words that stand for it ("Not burning"), or why
// it has none.
type Value struct {
	Definition
	Figure    string // "" when it has words or no value
	Words     string
	Undefined string
	Entered   bool // it used the month's entered counts
}

var work = apd.BaseContext.WithPrecision(40)

func dec(m money.Money) *apd.Decimal { return m.Decimal() }

func quo(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Quo(out, a, b); err != nil {
		panic(err) // b is checked non-zero before
	}
	return out
}

func mul(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Mul(out, a, b); err != nil {
		panic(err)
	}
	return out
}

func sub(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Sub(out, a, b); err != nil {
		panic(err)
	}
	return out
}

func add(a, b *apd.Decimal) *apd.Decimal {
	out := new(apd.Decimal)
	if _, err := work.Add(out, a, b); err != nil {
		panic(err)
	}
	return out
}

func n(i int64) *apd.Decimal { return apd.New(i, 0) }

// average is the exact mean of the figures.
func average(xs []*apd.Decimal) *apd.Decimal {
	total := new(apd.Decimal)
	for _, x := range xs {
		total = add(total, x)
	}
	return quo(total, n(int64(len(xs))))
}

// Compute works out every KPI for the last of months: at most the last three months of one financial year, earliest
// first, each with its figures (design D1: the three-month averages use the months of the financial year that exist).
// before is the month before the last, from whichever year, nil when the books have none.
func Compute(months []Month, before *Month, base money.Currency) []Value {
	m := months[len(months)-1]
	hundred := n(100)
	out := make([]Value, 0, len(Definitions))
	for _, d := range Definitions {
		v := Value{Definition: d}
		set := func(x *apd.Decimal) { v.Figure = rounding.KPI(x, d.Unit.places(base)) }
		none := func(why string) { v.Undefined = why }
		positive := func(x money.Money) bool { return x.Sign() > 0 }
		counts := m.Counts != nil && m.Counts.HasCustomers
		rev := dec(m.Revenue)
		margin := func(of money.Money) {
			if m.Revenue.IsZero() {
				none(NoRevenue)
				return
			}
			set(mul(quo(dec(of), rev), hundred))
		}
		// arpa, churn and the gross margin as fractions, for the unit economics; ok false with why when one has no value.
		arpa := func() (*apd.Decimal, string) {
			switch {
			case m.Subscription == nil:
				return nil, NoSubscription
			case !counts:
				return nil, NoCounts
			case m.Counts.Paying <= 0:
				return nil, NoPaying
			}
			return quo(dec(*m.Subscription), n(m.Counts.Paying)), ""
		}
		churn := func() (*apd.Decimal, string) {
			if !counts {
				return nil, NoCounts
			}
			start := m.Counts.Paying - m.Counts.New + m.Counts.Lost
			if start <= 0 {
				return nil, NoStart
			}
			return quo(n(m.Counts.Lost), n(start)), ""
		}
		gm := func() (*apd.Decimal, string) {
			if m.Revenue.IsZero() {
				return nil, NoRevenue
			}
			return quo(dec(m.GrossProfit), rev), ""
		}
		cac := func() (*apd.Decimal, string) {
			switch {
			case !counts:
				return nil, NoCounts
			case m.Counts.New <= 0:
				return nil, NoNew
			}
			return quo(dec(m.SM), n(m.Counts.New)), ""
		}
		ltv := func() (*apd.Decimal, string) {
			a, why := arpa()
			if why != "" {
				return nil, why
			}
			g, why := gm()
			if why != "" {
				return nil, why
			}
			c, why := churn()
			if why != "" {
				return nil, why
			}
			if c.IsZero() {
				return nil, NoChurn
			}
			return quo(mul(a, g), c), ""
		}
		switch d.Key {
		case "cash":
			set(dec(m.Cash))
		case "revenue":
			set(rev)
		case "net_profit_ytd":
			set(dec(m.NetProfitYTD))
		case "owed_to_us":
			set(dec(m.Receivables))
		case "owed_by_us":
			set(dec(m.Payables))
		case "booked_mrr":
			if m.Subscription == nil {
				none(NoSubscription)
			} else {
				set(dec(*m.Subscription))
			}
		case "contracted_mrr":
			none(NoSubscriptions)
		case "arr":
			if m.Subscription == nil {
				none(NoSubscription)
			} else {
				set(mul(dec(*m.Subscription), n(12)))
			}
		case "revenue_growth":
			switch {
			case before == nil:
				none(NoMonthBefore)
			case before.Revenue.IsZero():
				none(NoRevenueBefore)
			default:
				b := dec(before.Revenue)
				b.Abs(b)
				set(mul(quo(sub(rev, dec(before.Revenue)), b), hundred))
			}
		case "gross_margin":
			margin(m.GrossProfit)
		case "ebitda_margin":
			margin(m.EBITDA)
		case "net_margin":
			margin(m.NetProfit)
		case "spend_sm":
			set(dec(m.SM))
		case "spend_rd":
			set(dec(m.RD))
		case "spend_ga":
			set(dec(m.GA))
		case "net_burn":
			set(dec(m.NetBurn))
		case "runway":
			var burns []*apd.Decimal
			for _, x := range months {
				burns = append(burns, dec(x.NetBurn))
			}
			avg := average(burns)
			if avg.Sign() <= 0 {
				v.Words = "Not burning"
			} else {
				set(quo(dec(m.Cash), avg))
			}
		case "arpa":
			if a, why := arpa(); why != "" {
				none(why)
			} else {
				set(a)
			}
		case "churn":
			if c, why := churn(); why != "" {
				none(why)
			} else {
				set(mul(c, hundred))
			}
		case "cac":
			if c, why := cac(); why != "" {
				none(why)
			} else {
				set(c)
			}
		case "ltv":
			if l, why := ltv(); why != "" {
				none(why)
			} else {
				set(l)
			}
		case "ltv_cac":
			l, why := ltv()
			c, why2 := cac()
			switch {
			case why != "":
				none(why)
			case why2 != "":
				none(why2)
			case c.IsZero():
				none(NoNew)
			default:
				set(quo(l, c))
			}
		case "cac_payback":
			c, why := cac()
			a, why2 := arpa()
			g, why3 := gm()
			switch {
			case why != "":
				none(why)
			case why2 != "":
				none(why2)
			case why3 != "":
				none(why3)
			default:
				per := mul(a, g)
				if per.Sign() <= 0 {
					none(NoMargin)
				} else {
					set(quo(c, per))
				}
			}
		case "burn_multiple":
			switch {
			case !positive(m.NetBurn):
				none(NotBurning)
			case m.Subscription == nil:
				none(NoSubscription)
			case before == nil || before.Subscription == nil:
				none(NoMonthBefore)
			default:
				arr := mul(sub(dec(*m.Subscription), dec(*before.Subscription)), n(12))
				if arr.Sign() <= 0 {
					none(NoGrowth)
				} else {
					set(quo(dec(m.NetBurn), arr))
				}
			}
		case "current_ratio":
			if m.CurrentLiabilities.Sign() <= 0 {
				none(NoLiabilities)
			} else {
				set(quo(dec(m.CurrentAssets), dec(m.CurrentLiabilities)))
			}
		case "quick_ratio":
			if m.CurrentLiabilities.Sign() <= 0 {
				none(NoLiabilities)
			} else {
				set(quo(add(dec(m.Cash), dec(m.Receivables)), dec(m.CurrentLiabilities)))
			}
		case "working_capital":
			set(sub(dec(m.CurrentAssets), dec(m.CurrentLiabilities)))
		case "debt_equity":
			if !positive(m.Equity) {
				none(NoEquity)
			} else {
				set(quo(dec(m.TotalLiabilities), dec(m.Equity)))
			}
		case "dso", "dpo":
			var xs []*apd.Decimal
			for _, x := range months {
				if d.Key == "dso" {
					xs = append(xs, dec(x.Revenue))
				} else {
					xs = append(xs, dec(x.Costs))
				}
			}
			daily := quo(average(xs), n(30))
			if daily.Sign() <= 0 {
				none(NoAverage)
			} else if d.Key == "dso" {
				set(quo(dec(m.Receivables), daily))
			} else {
				set(quo(dec(m.Payables), daily))
			}
		case "headcount":
			if m.Counts == nil || m.Counts.Headcount == nil {
				none(NoHeadcount)
			} else {
				set(n(*m.Counts.Headcount))
			}
		case "revenue_per_employee":
			switch {
			case m.Counts == nil || m.Counts.Headcount == nil:
				none(NoHeadcount)
			case *m.Counts.Headcount <= 0:
				none(NoPeople)
			default:
				set(quo(mul(rev, n(12)), n(*m.Counts.Headcount)))
			}
		}
		v.Entered = m.Counts != nil && v.Undefined == "" && strings.Contains(d.Source, Entered)
		out = append(out, v)
	}
	return out
}
