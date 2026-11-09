package planning_test

import (
	"errors"
	"testing"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/pack"
	"github.com/cognizhi/fyniq/internal/domain/planning"
	"github.com/cognizhi/fyniq/packs"
)

var sgd = money.MustLookup("SGD")

func dec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	x, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func month(t *testing.T, s string) time.Time {
	t.Helper()
	m, err := time.ParseInLocation("2006-01", s, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// shipped are the statutory figures of the shipped packs.
func shipped(t *testing.T) planning.Packs {
	t.Helper()
	load := func(dir string) pack.Version {
		v, err := pack.Load(packs.FS, dir)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	return planning.Packs{CITPack: load("sg-cit/2026.1"), CPFPack: load("sg-cpf/2026.1"), SDLPack: load("sg-sdl/2026.1")}
}

func same(t *testing.T, what string, got *apd.Decimal, want string) {
	t.Helper()
	if got == nil || got.Cmp(dec(t, want)) != 0 {
		t.Errorf("%s: got %v, want %s", what, got, want)
	}
}

func rounded(t *testing.T, what string, f *planning.Fraction, places int, want string) {
	t.Helper()
	if f == nil {
		t.Errorf("%s: blank, want %s", what, want)
		return
	}
	got, err := f.Round(places)
	if err != nil || got.Text('f') != want {
		t.Errorf("%s: got %v, %v; want %s", what, got, err, want)
	}
}

// small is a company of 100 customers with services, one current engineer, a support person, a sales hire in March,
// a laptop in February, a loan, a grant and an equity round: each line can be worked by hand.
func small(t *testing.T) planning.Inputs {
	end := month(t, "2027-12")
	return planning.Inputs{
		Currency: sgd, Start: month(t, "2027-01"), Horizon: 12,
		Books: planning.Books{Cash: dec(t, "50000.00"), Receivables: dec(t, "10000.00"), Payables: dec(t, "4000.00"),
			Depreciation: []*apd.Decimal{dec(t, "100.00"), dec(t, "100.00"), dec(t, "100.00")}},
		BufferMonths: dec(t, "6"),
		Revenue: planning.RevenueDrivers{Customers: dec(t, "100"), NewCustomers: dec(t, "10"), NewGrowth: dec(t, "0.10"),
			Churn: dec(t, "0.02"), ARPA: dec(t, "50.00"), PriceIncrease: dec(t, "0.05"), Services: dec(t, "1000.00"),
			ServicesGrowth: dec(t, "0.01"), Other: dec(t, "200.00"),
			Collection: [3]*apd.Decimal{dec(t, "0.3"), dec(t, "0.6"), dec(t, "0.1")}},
		Costs: planning.CostDrivers{Hosting: dec(t, "0.12"), AI: dec(t, "0.08"), Contractors: dec(t, "0.25"), Fees: dec(t, "0.02"),
			SalaryIncrease: dec(t, "0.04")},
		Opex: []planning.OpexLine{{Label: "Sales & marketing", Group: planning.GroupSalesMarketing, Base: dec(t, "500.00"),
			Growth: dec(t, "0.02"), Percent: dec(t, "0.01")}},
		Headcount: []planning.Hire{
			{Role: "CTO", Function: planning.FunctionRD, Start: month(t, "2026-12"), Salary: dec(t, "7600.00"), CPF: true},
			{Role: "Support", Function: planning.FunctionCOGS, Start: month(t, "2027-01"), Salary: dec(t, "3000.00")},
			{Role: "Sales", Function: planning.FunctionSM, Start: month(t, "2027-03"), End: &end, Salary: dec(t, "9000.00"), CPF: true},
		},
		Capex: []planning.Capex{{Amount: dec(t, "3000.00"), Month: month(t, "2027-02"), Life: 36}},
		Funding: []planning.Funding{
			{Type: planning.FundingLoan, Month: month(t, "2027-01"), Amount: dec(t, "12000.00"), Rate: dec(t, "0.06"), Tenor: 12},
			{Type: planning.FundingGrant, Month: month(t, "2027-06"), Amount: dec(t, "5000.00")},
			{Type: planning.FundingEquity, Month: month(t, "2027-04"), Amount: dec(t, "100000.00")},
		},
		Tax: planning.TaxInputs{StartUpYears: 3, LossesBroughtForward: dec(t, "0"), PaymentLag: 9},
	}
}

// req: FR-PLN-02
// req: FR-PLN-03
// req: RND-PLAN
// Each line of the first months, worked by hand from the drivers and the 2026 packs: a driver line rounded once from
// the lines it is a percentage of as shown, totals adding the rounded lines, customers exact.
func TestTheForecastMonthByMonth(t *testing.T) {
	r, err := planning.Run(small(t), planning.Base, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	m1, m2, m3 := r.Months[0], r.Months[1], r.Months[2]
	for _, c := range []struct {
		what string
		got  *apd.Decimal
		want string
	}{
		{"m1 customers", m1.CustomersClose, "108"}, // 100 + 10 − 2
		{"m1 subscription", m1.Subscription, "5400.00"},
		{"m1 revenue", m1.Revenue, "6600.00"}, // + 1,000.00 services + 200.00 other
		{"m1 hosting", m1.Hosting, "648.00"},  // 12% of subscription
		{"m1 AI", m1.AI, "528.00"},            // 8% of revenue
		{"m1 contractors", m1.Contractors, "250.00"},
		{"m1 fees", m1.Fees, "132.00"},
		{"m1 COGS payroll", m1.PayrollCOGS, "3007.50"}, // 3,000.00 and SDL 7.50, no CPF
		{"m1 gross profit", m1.GrossProfit, "2034.50"},
		{"m1 R&D", m1.PayrollRD, "8903.25"}, // 7,600.00 + 17% CPF 1,292.00 + SDL at its 11.25 maximum
		{"m1 S&M", m1.PayrollSM, "0"},       // the sales hire starts in March
		{"m1 other expenses", m1.OpexTotal, "566.00"},
		{"m1 EBITDA", m1.EBITDA, "-7434.75"},
		{"m1 depreciation", m1.Depreciation, "100.00"},
		{"m1 interest", m1.Interest, "0"}, // the loan is drawn in month 1: interest from month 2
		{"m1 profit before tax", m1.ProfitBeforeTax, "-7534.75"},
		{"m1 collections", m1.Collections, "7980.00"}, // 30% of 6,600.00 + 60% of the 10,000.00 owed
		{"m1 suppliers", m1.Suppliers, "-6124.00"},    // the direct costs, the expense line and the 4,000.00 owed
		{"m1 payroll paid", m1.PayrollPaid, "-11910.75"},
		{"m1 operating", m1.Operating, "-10054.75"},
		{"m1 loan drawn", m1.LoansDrawn, "12000.00"},
		{"m1 cash", m1.CashClose, "51945.25"},
		{"m1 net burn", m1.NetBurn, "10054.75"},
		{"m1 ARR", m1.ARR, "64800.00"},
		{"m1 S&M for CAC", m1.SalesMarketing, "566.00"},

		{"m2 customers", m2.CustomersClose, "116.84"},
		{"m2 revenue", m2.Revenue, "7052.00"},
		{"m2 hosting", m2.Hosting, "701.04"},
		{"m2 AI", m2.AI, "564.16"},
		{"m2 contractors", m2.Contractors, "252.50"},
		{"m2 fees", m2.Fees, "141.04"},
		{"m2 gross profit", m2.GrossProfit, "2385.76"},
		{"m2 other expenses", m2.OpexTotal, "580.52"},
		{"m2 depreciation", m2.Depreciation, "183.33"}, // 100.00 + 3,000.00 ÷ 36, rounded once
		{"m2 interest", m2.Interest, "60.00"},          // 12,000.00 × 6% ÷ 12
		{"m2 profit before tax", m2.ProfitBeforeTax, "-7341.34"},
		{"m2 collections", m2.Collections, "10075.60"}, // 30% of 7,052.00 + 60% of 6,600.00 + 40% of 10,000.00
		{"m2 operating", m2.Operating, "-4134.41"},
		{"m2 capex", m2.CapexPaid, "-3000.00"},
		{"m2 repayment", m2.LoanRepayments, "-1000.00"},
		{"m2 cash", m2.CashClose, "43810.84"},
		{"m2 net burn", m2.NetBurn, "7134.41"},

		{"m3 customers", m3.CustomersClose, "126.6032"}, // exact
		{"m3 subscription", m3.Subscription, "6330.16"},
		{"m3 services", m3.Services, "1020.10"},
		{"m3 S&M", m3.PayrollSM, "10371.25"},  // 9,000.00 + 17% of the 8,000.00 ceiling + 11.25
		{"m3 interest", m3.Interest, "55.00"}, // on 11,000.00 left
	} {
		same(t, c.what, c.got, c.want)
	}
	if m1.Headcount != 2 || m3.Headcount != 3 {
		t.Errorf("headcount: got %d and %d, want 2 and 3", m1.Headcount, m3.Headcount)
	}
	pct, err := m1.GrossMargin.Percent()
	if err != nil || pct.Text('f') != "30.8" {
		t.Errorf("m1 gross margin: got %v, %v; want 30.8", pct, err)
	}
	rounded(t, "m1 CAC", m1.CAC, 2, "56.60")
	rounded(t, "m1 LTV", m1.LTV, 2, "770.64") // 50.00 × 2,034.50 ÷ 6,600.00 ÷ 2%
	rounded(t, "m1 LTV to CAC", m1.LTVToCAC, 2, "13.62")
	rounded(t, "m2 burn multiple", m2.BurnMultiple, 2, "1.35") // 7,134.41 ÷ (5,842.00 − 5,400.00) × 12
	if m1.BurnMultiple != nil {
		t.Error("month 1 has a burn multiple; with no month before it is blank")
	}
	if !r.CollectionsOK {
		t.Error("30%, 60% and 10% total 100%")
	}
	same(t, "opening cash", r.OpeningCash, "50000.00")
	// The equity round arrives in April, the grant in June.
	same(t, "m4 equity", r.Months[3].Equity, "100000.00")
	same(t, "m6 grants", r.Months[5].Grants, "5000.00")
	// What is shown adds up: cash is the opening cash plus every month's net cash flow.
	sum := new(apd.Decimal).Set(r.OpeningCash)
	for _, m := range r.Months {
		if _, err := apd.BaseContext.Add(sum, sum, m.NetCashFlow); err != nil {
			t.Fatal(err)
		}
	}
	same(t, "the last month's cash", r.Months[11].CashClose, sum.Text('f'))
}

// req: FR-PLN-02
// req: FR-PLN-03
// Every lever moves the plan as the workbook's do: Worst has fewer customers, more churn, a lower price, three months'
// hiring and fundraising delay and 60% of the money raised; the grant is not fundraising.
func TestTheLevers(t *testing.T) {
	in := small(t)
	base, err := planning.Run(in, planning.Base, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	worst, err := planning.Run(in, planning.Worst, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	same(t, "worst m1 new customers", worst.Months[0].NewCustomers, "7")     // 10 × 0.7
	same(t, "worst m1 lost customers", worst.Months[0].LostCustomers, "2.6") // 100 × 2% × 1.3
	same(t, "worst m1 ARPA", worst.Months[0].ARPA, "45")                     // 50 × 0.9
	same(t, "worst m3 S&M", worst.Months[2].PayrollSM, "0")                  // the March hire moves to June
	same(t, "worst m6 S&M", worst.Months[5].PayrollSM, "10371.25")
	same(t, "worst m1 R&D", worst.Months[0].PayrollRD, base.Months[0].PayrollRD.Text('f')) // the current team does not move
	same(t, "worst m4 equity", worst.Months[3].Equity, "0")
	same(t, "worst m7 equity", worst.Months[6].Equity, "60000.00") // April + 3 months, 60% of 100,000.00
	same(t, "worst m4 loan drawn", worst.Months[3].LoansDrawn, "7200.00")
	same(t, "worst m6 grants", worst.Months[5].Grants, "5000.00")
	without, err := planning.Run(planning.WithoutFunding(in), planning.Base, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	same(t, "without funding m4 equity", without.Months[3].Equity, "0")
	same(t, "without funding m6 grants", without.Months[5].Grants, "0")
	if without.Key.MinimumCash.Cmp(base.Key.MinimumCash) >= 0 {
		t.Errorf("without its funding the minimum cash %s is not below %s", without.Key.MinimumCash, base.Key.MinimumCash)
	}
	custom := planning.Scenario{Name: "Slow hiring", Levers: planning.Base.Levers, BufferMonths: dec(t, "3")}
	custom.Levers.HireDelay = 1
	cr, err := planning.Run(in, custom, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	same(t, "custom m3 S&M", cr.Months[2].PayrollSM, "0")
	same(t, "custom m4 S&M", cr.Months[3].PayrollSM, "10371.25")
}

// profitable earns 50,000.00 a month of other revenue and spends nothing.
func profitable(t *testing.T, start string) planning.Inputs {
	return planning.Inputs{
		Currency: sgd, Start: month(t, start), Horizon: 24,
		Books:        planning.Books{Cash: dec(t, "0"), Receivables: dec(t, "0"), Payables: dec(t, "0")},
		BufferMonths: dec(t, "6"),
		Revenue: planning.RevenueDrivers{Customers: dec(t, "0"), NewCustomers: dec(t, "0"), NewGrowth: dec(t, "0"),
			Churn: dec(t, "0"), ARPA: dec(t, "0"), PriceIncrease: dec(t, "0"), Services: dec(t, "0"), ServicesGrowth: dec(t, "0"),
			Other: dec(t, "50000.00"), Collection: [3]*apd.Decimal{dec(t, "1"), dec(t, "0"), dec(t, "0")}},
		Costs: planning.CostDrivers{Hosting: dec(t, "0"), AI: dec(t, "0"), Contractors: dec(t, "0"), Fees: dec(t, "0"), SalaryIncrease: dec(t, "0")},
		Tax:   planning.TaxInputs{StartUpYears: 1, LossesBroughtForward: dec(t, "0"), PaymentLag: 9},
	}
}

// req: FR-PLN-03
// req: RND-PLAN
// A year's tax: the start-up exemption, then the partial one; losses brought forward first; the YA's rebate from the
// pack; booked in the year's last month and paid nine months later.
func TestTheTaxEstimate(t *testing.T) {
	r, err := planning.Run(profitable(t, "2027-01"), planning.Base, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	y1, y2 := r.Taxes[0], r.Taxes[1]
	if y1.YA != 2028 || y1.Scheme != pack.SchemeStartUp || y2.Scheme != pack.SchemePartial {
		t.Errorf("year 1 is YA %d under %s, year 2 under %s; want YA 2028 under sute, then pte", y1.YA, y1.Scheme, y2.Scheme)
	}
	same(t, "year 1 profit", y1.ProfitBeforeTax, "600000.00")
	same(t, "year 1 exempt", y1.Exempt, "125000.00")
	same(t, "year 1 tax", y1.Tax, "80750.00") // 17% of 475,000.00; YA 2028 has no rebate
	same(t, "year 2 exempt", y2.Exempt, "102500.00")
	same(t, "year 2 tax", y2.Tax, "84575.00") // 17% of 497,500.00
	same(t, "month 12 tax", r.Months[11].Tax, "80750.00")
	same(t, "month 12 net profit", r.Months[11].NetProfit, "-30750.00")
	same(t, "month 11 tax", r.Months[10].Tax, "0")
	same(t, "month 21 tax paid", r.Months[20].TaxPaid, "-80750.00") // 9 months after December
	same(t, "month 24 tax", r.Months[23].Tax, "84575.00")           // paid in month 33, past the horizon

	in := profitable(t, "2027-01")
	in.Tax.LossesBroughtForward = dec(t, "100000.00")
	r, err = planning.Run(in, planning.Base, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	same(t, "after losses: chargeable", r.Taxes[0].Chargeable, "500000.00")
	same(t, "after losses: tax", r.Taxes[0].Tax, "63750.00") // 17% of 375,000.00
	same(t, "after losses: carried forward", r.Taxes[0].LossesCarriedForward, "0")

	// A year ending in 2025 is YA 2026: 50% of the tax, at most 40,000.00.
	r, err = planning.Run(profitable(t, "2025-01"), planning.Base, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	same(t, "YA 2026 rebate", r.Taxes[0].Rebate, "40000.00")
	same(t, "YA 2026 tax", r.Taxes[0].Tax, "40750.00")

	// A loss year carries its loss forward.
	in = profitable(t, "2027-01")
	in.Revenue.Other = dec(t, "0")
	in.Opex = []planning.OpexLine{{Label: "Rent", Base: dec(t, "1000.00"), Growth: dec(t, "0"), Percent: dec(t, "0")}}
	r, err = planning.Run(in, planning.Base, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	same(t, "loss year tax", r.Taxes[0].Tax, "0")
	same(t, "loss carried forward", r.Taxes[0].LossesCarriedForward, "12000.00")
	same(t, "loss year 2 brought forward", r.Taxes[1].LossesBroughtForward, "12000.00")
}

// req: FR-PLN-03
// The key results: the cash-out month and runway, the minimum and its month, break-even, the average burn of the
// first 12 months and the funding gap for the buffer.
func TestTheKeyResults(t *testing.T) {
	in := profitable(t, "2027-01")
	in.Revenue.Other = dec(t, "0")
	in.Books.Cash = dec(t, "30000.00")
	in.Opex = []planning.OpexLine{{Label: "Rent", Base: dec(t, "10000.00"), Growth: dec(t, "0"), Percent: dec(t, "0")}}
	r, err := planning.Run(in, planning.Base, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	k := r.Key
	if k.CashOutMonth == nil || !k.CashOutMonth.Equal(month(t, "2027-04")) || k.RunwayMonths != 3 || k.RunwayBeyond {
		t.Errorf("cash out %v, runway %d (%v); want April 2027 after 3 months", k.CashOutMonth, k.RunwayMonths, k.RunwayBeyond)
	}
	same(t, "minimum cash", k.MinimumCash, "-210000.00") // 30,000.00 − 24 × 10,000.00
	if !k.MinimumMonth.Equal(month(t, "2028-12")) || k.BreakEven != nil {
		t.Errorf("minimum in %v, break-even %v; want December 2028 and none", k.MinimumMonth, k.BreakEven)
	}
	same(t, "average burn", k.AverageBurn, "10000.00")
	same(t, "funding gap", k.FundingGap, "270000.00") // 6 × 10,000.00 + 210,000.00
	in.BufferMonths = dec(t, "0")
	r, _ = planning.Run(in, planning.Base, shipped(t))
	same(t, "funding gap without a buffer", r.Key.FundingGap, "210000.00")

	r, err = planning.Run(profitable(t, "2027-01"), planning.Base, shipped(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.Key.CashOutMonth != nil || !r.Key.RunwayBeyond || r.Key.RunwayMonths != 24 {
		t.Errorf("a profitable company: cash out %v, runway > %d (%v); want none, beyond 24", r.Key.CashOutMonth, r.Key.RunwayMonths, r.Key.RunwayBeyond)
	}
	if r.Key.BreakEven == nil || !r.Key.BreakEven.Equal(month(t, "2027-01")) {
		t.Errorf("break-even %v; want January 2027", r.Key.BreakEven)
	}
	same(t, "no burn, no gap", r.Key.FundingGap, "0")
	same(t, "revenue next 12", r.Key.RevenueNext12, "600000.00")
	if y := r.Years[0]; y.Revenue == nil || y.Cash == nil || r.Years[2].Revenue != nil {
		t.Errorf("year 1 %v, year 3 %v; want year 1 summed and year 3 beyond the horizon", y, r.Years[2])
	}
}

// req: FR-PLN-02
// A collection pattern that does not total 100% is flagged; a horizon outside 12 to 60 months, or a month the packs
// do not hold, is refused with its code.
func TestTheForecastsRefusals(t *testing.T) {
	in := small(t)
	in.Revenue.Collection[2] = dec(t, "0.2")
	r, err := planning.Run(in, planning.Base, shipped(t))
	if err != nil || r.CollectionsOK {
		t.Errorf("collections of 110%%: got ok=%v, %v; want flagged", r.CollectionsOK, err)
	}
	for _, h := range []int{11, 61} {
		in := small(t)
		in.Horizon = h
		var e *planning.Error
		if _, err := planning.Run(in, planning.Base, shipped(t)); !errors.As(err, &e) || e.Code != planning.CodeHorizon {
			t.Errorf("a horizon of %d: got %v; want %s", h, err, planning.CodeHorizon)
		}
	}
	in = small(t)
	in.Start = month(t, "2025-01") // the CPF pack holds no rate before 2026, and the CTO is already employed
	in.Headcount[0].Start = month(t, "2024-12")
	var e *planning.Error
	if _, err := planning.Run(in, planning.Base, shipped(t)); !errors.As(err, &e) || e.Code != planning.CodeStatutory {
		t.Errorf("a forecast of 2025's payroll: got %v; want %s", err, planning.CodeStatutory)
	}
}
