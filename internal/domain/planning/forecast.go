package planning

import (
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/pack"
)

// Month is one forecast month's lines (design D3). Amounts are rounded under RND-PLAN (driver lines once from their
// exact value, totals as sums of the rounded lines); customers and ARPA are exact. A nil ratio is blank: it cannot be
// worked out (no revenue, no new customers, no growth in ARR).
type Month struct {
	N    int       // 1 is the forecast's first month
	Date time.Time // its first day
	Year int       // forecast year, 1 for months 1 to 12

	CustomersOpen, NewCustomers, LostCustomers, CustomersClose, ARPA *apd.Decimal

	Subscription, Services, Other, Revenue                                *apd.Decimal
	Hosting, AI, Contractors, Fees, PayrollCOGS, DirectCosts, GrossProfit *apd.Decimal
	PayrollRD, PayrollSM, PayrollGA, Payroll                              *apd.Decimal
	Headcount                                                             int
	Opex                                                                  []*apd.Decimal // one per other-expense line, in order
	OpexTotal, EBITDA, Depreciation, Grants, Interest                     *apd.Decimal
	ProfitBeforeTax, Tax, NetProfit                                       *apd.Decimal

	Collections, Suppliers, PayrollPaid, GrantsReceived, InterestPaid, TaxPaid, Operating *apd.Decimal
	CapexPaid, Equity, LoansDrawn, LoanRepayments, NetCashFlow, CashOpen, CashClose       *apd.Decimal

	NetBurn, ARR, SalesMarketing *apd.Decimal
	GrossMargin                  *Fraction // gross profit ÷ revenue
	CAC, LTV                     *Fraction // S&M ÷ new customers; ARPA × gross margin ÷ churn
	LTVToCAC, BurnMultiple       *Fraction
}

// TaxYear is one forecast year's tax estimate (design D3).
type TaxYear struct {
	Year, YA                                                           int
	Scheme                                                             string // pack.SchemeStartUp or pack.SchemePartial
	ProfitBeforeTax, LossesBroughtForward, Chargeable, Exempt, Taxable *apd.Decimal
	GrossTax, Rebate, Tax, LossesCarriedForward                        *apd.Decimal
}

// YearSummary sums or ends a forecast year; a nil figure is blank (the year is beyond the horizon, or ends past it).
type YearSummary struct {
	Year                                             int
	Revenue, GrossProfit, EBITDA, NetProfit, NetBurn *apd.Decimal
	Cash, Customers, ARR                             *apd.Decimal
	Headcount                                        *int
}

// Key are a scenario's key results (FR-PLN-03).
type Key struct {
	CashOutMonth  *time.Time // the first month cash closes below zero; nil: not within the horizon
	RunwayMonths  int        // the months before it; the horizon when there is none ("> H")
	RunwayBeyond  bool       // no cash-out month within the horizon
	MinimumCash   *apd.Decimal
	MinimumMonth  time.Time
	BreakEven     *time.Time // the first month with revenue whose EBITDA is not negative
	AverageBurn   *apd.Decimal
	FundingGap    *apd.Decimal
	RevenueNext12 *apd.Decimal
	ARREnd        *apd.Decimal
	CustomersEnd  *apd.Decimal // exact
}

// Result is one scenario's forecast.
type Result struct {
	Scenario      string
	Start         time.Time
	Horizon       int
	OpeningCash   *apd.Decimal
	Buffer        *apd.Decimal // the months of burn the funding gap kept: the scenario's, else the forecast's
	Months        []Month
	Taxes         []TaxYear
	Years         []YearSummary
	Key           Key
	CollectionsOK bool // the collection pattern totals 100%
}

// The codes of a forecast that cannot be worked out.
const (
	CodeHorizon   = "PLAN-01" // a horizon outside 12 to 60 months
	CodeStatutory = "PLAN-02" // a pack figure the forecast needs is missing
	CodeInput     = "PLAN-03" // an input the model cannot use
)

// Error is a forecast that cannot be worked out, with its code.
type Error struct {
	Code, Field, Message, Fix string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s %s: %s (%s)", e.Code, e.Field, e.Message, e.Fix)
}

// monthNo is the forecast month of d: 1 for the start's month, 0 for the month before.
func monthNo(d, start time.Time) int {
	return (d.Year()-start.Year())*12 + int(d.Month()) - int(start.Month()) + 1
}

func first(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC) }

type person struct {
	function    string
	first, last int
	salary      *apd.Decimal
	cpf         bool
}

type event struct {
	kind   string
	month  int
	amount *apd.Decimal
	rate   *apd.Decimal
	tenor  int
}

// Run works one scenario of a forecast (design D3, D4).
func Run(in Inputs, sc Scenario, st Statutory) (Result, error) {
	if in.Horizon < MinHorizon || in.Horizon > MaxHorizon {
		return Result{}, &Error{CodeHorizon, "horizon", fmt.Sprintf("%d months is outside 12 to 60", in.Horizon),
			"Forecast between 12 and 60 months"}
	}
	c := &calc{cur: in.Currency}
	H, lv := in.Horizon, sc.Levers
	start := first(in.Start)
	buffer := in.BufferMonths
	if sc.BufferMonths != nil {
		buffer = sc.BufferMonths
	}
	out := Result{Scenario: sc.Name, Start: start, Horizon: H}
	r := in.Revenue
	sum := c.add(r.Collection[0], r.Collection[1], r.Collection[2])
	out.CollectionsOK = sum.Cmp(d(1)) == 0

	var people []person
	for _, h := range in.Headcount {
		s := monthNo(first(h.Start), start)
		f := s
		if s > 1 {
			f = s + lv.HireDelay // a planned hire moves by the hiring delay; the current team does not
		}
		l := 1 << 30
		if h.End != nil {
			l = monthNo(first(*h.End), start)
		}
		people = append(people, person{h.Function, f, l, h.Salary, h.CPF})
	}
	var events []event
	for _, f := range in.Funding {
		m := max(1, monthNo(first(f.Month), start)) // an event before the start counts as month 1
		amount := f.Amount
		if f.Type != FundingGrant {
			m += lv.FundDelay
			amount = c.mul(f.Amount, lv.FundAmount)
		}
		tenor := max(1, f.Tenor)
		events = append(events, event{f.Type, m, amount, f.Rate, tenor})
	}
	type buy struct {
		month, life int
		amount      *apd.Decimal
	}
	var buys []buy
	for _, x := range in.Capex {
		buys = append(buys, buy{max(1, monthNo(first(x.Month), start)), x.Life, x.Amount})
	}

	// staff is a function's payroll for month n of forecast year y, exact: salary raised each year, employer CPF on
	// the raised salary up to the ceiling, and SDL, from the packs in force that month.
	staff := func(fn string, n, y int, date time.Time) *apd.Decimal {
		total := apd.New(0, 0)
		raise := c.pow(c.grow(in.Costs.SalaryIncrease), y-1)
		for _, p := range people {
			if p.function != fn || p.salary.Sign() == 0 || n < p.first || n > p.last {
				continue
			}
			salary := c.mul(p.salary, raise)
			cost := salary
			if p.cpf {
				rate, ceiling, err := st.EmployerCPF(date)
				if err != nil {
					c.fail(&Error{CodeStatutory, "headcount", fmt.Sprintf("no CPF rate for %s: %v", date.Format("Jan 2006"), err),
						"Activate the sg-cpf pack for the forecast's months"})
					continue
				}
				cost = c.add(cost, c.mul(minOf(salary, ceiling), rate))
			}
			levy, err := st.SDL(date, salary)
			if err != nil {
				c.fail(&Error{CodeStatutory, "headcount", fmt.Sprintf("no SDL for %s: %v", date.Format("Jan 2006"), err),
					"Activate the sg-sdl pack for the forecast's months"})
				continue
			}
			total = c.add(total, cost, levy)
		}
		return total
	}

	zero := func() *apd.Decimal { return apd.New(0, 0) }
	churn := c.mul(r.Churn, lv.Churn)
	months := make([]Month, H)
	at := func(n int) *Month { return &months[n-1] }
	revenueOf := func(n int) *apd.Decimal {
		if n < 1 {
			return zero()
		}
		return at(n).Revenue
	}
	salesLine := -1
	for i, o := range in.Opex {
		if o.Group == GroupSalesMarketing {
			salesLine = i
			break
		}
	}

	for n := 1; n <= H; n++ {
		m := at(n)
		m.N, m.Date, m.Year = n, start.AddDate(0, n-1, 0), (n-1)/12+1
		y := m.Year
		if n == 1 {
			m.CustomersOpen = r.Customers
		} else {
			m.CustomersOpen = at(n - 1).CustomersClose
		}
		m.NewCustomers = c.mul(r.NewCustomers, c.pow(c.grow(r.NewGrowth), n-1), lv.NewCustomers)
		m.LostCustomers = c.mul(m.CustomersOpen, churn)
		m.CustomersClose = c.sub(c.add(m.CustomersOpen, m.NewCustomers), m.LostCustomers)
		m.ARPA = c.mul(r.ARPA, c.pow(c.grow(r.PriceIncrease), y-1), lv.Price)

		m.Subscription = c.round(c.mul(m.CustomersClose, m.ARPA))
		m.Services = c.round(c.mul(r.Services, c.pow(c.grow(r.ServicesGrowth), n-1), lv.Services))
		m.Other = c.round(r.Other)
		m.Revenue = c.add(m.Subscription, m.Services, m.Other)

		m.Hosting = c.round(c.mul(m.Subscription, in.Costs.Hosting, lv.DirectCosts))
		m.AI = c.round(c.mul(m.Revenue, in.Costs.AI, lv.DirectCosts))
		m.Contractors = c.round(c.mul(m.Services, in.Costs.Contractors, lv.DirectCosts))
		m.Fees = c.round(c.mul(m.Revenue, in.Costs.Fees, lv.DirectCosts))
		m.PayrollCOGS = c.round(staff(FunctionCOGS, n, y, m.Date))
		m.DirectCosts = c.add(m.Hosting, m.AI, m.Contractors, m.Fees, m.PayrollCOGS)
		m.GrossProfit = c.sub(m.Revenue, m.DirectCosts)

		m.PayrollRD = c.round(staff(FunctionRD, n, y, m.Date))
		m.PayrollSM = c.round(staff(FunctionSM, n, y, m.Date))
		m.PayrollGA = c.round(staff(FunctionGA, n, y, m.Date))
		m.Payroll = c.add(m.PayrollRD, m.PayrollSM, m.PayrollGA)
		for _, p := range people {
			if n >= p.first && n <= p.last {
				m.Headcount++
			}
		}
		m.Opex = make([]*apd.Decimal, len(in.Opex))
		for i, o := range in.Opex {
			line := c.add(c.mul(o.Base, c.pow(c.grow(o.Growth), n-1)), c.mul(o.Percent, m.Revenue))
			m.Opex[i] = c.round(c.mul(line, lv.Opex))
		}
		m.OpexTotal = c.add(m.Opex...)
		m.EBITDA = c.sub(c.sub(m.GrossProfit, m.Payroll), m.OpexTotal)

		dep := whole(zero())
		if n-1 < len(in.Books.Depreciation) && in.Books.Depreciation[n-1] != nil {
			dep = whole(in.Books.Depreciation[n-1])
		}
		for _, b := range buys {
			if b.life >= 1 && n >= b.month && n <= b.month+b.life-1 {
				dep = c.addFrac(dep, c.over(b.amount, d(int64(b.life))))
			}
		}
		m.Depreciation = c.roundFrac(dep)
		grants := zero()
		interest := whole(zero())
		for _, e := range events {
			if e.kind == FundingGrant && e.month == n {
				grants = c.add(grants, e.amount)
			}
			if e.kind == FundingLoan && e.month < n && n <= e.month+e.tenor {
				// the balance at the start of the month × rate ÷ 12: amount × (tenor − repaid months) ÷ tenor
				left := d(int64(e.tenor - (n - e.month - 1)))
				interest = c.addFrac(interest, c.over(c.mul(e.amount, left, e.rate), d(int64(12*e.tenor))))
			}
		}
		m.Grants = c.round(grants)
		m.Interest = c.roundFrac(interest)
		m.ProfitBeforeTax = c.sub(c.add(c.sub(m.EBITDA, m.Depreciation), m.Grants), m.Interest)
	}

	// Tax by forecast year: chargeable after losses, the start-up or the partial exemption, the rate and the YA's
	// rebate from sg-cit, for the YA after the calendar year in which the forecast year ends.
	years := (H + 11) / 12
	taxOf := make([]*apd.Decimal, years+1)
	losses := in.Tax.LossesBroughtForward
	if losses == nil {
		losses = zero()
	}
	for y := 1; y <= years; y++ {
		pbt := zero()
		for n := 12*(y-1) + 1; n <= min(12*y, H); n++ {
			pbt = c.add(pbt, at(n).ProfitBeforeTax)
		}
		end := start.AddDate(0, 12*y-1, 0)
		t := TaxYear{Year: y, YA: end.Year() + 1, Scheme: pack.SchemePartial, ProfitBeforeTax: pbt, LossesBroughtForward: losses}
		if y <= in.Tax.StartUpYears {
			t.Scheme = pack.SchemeStartUp
		}
		t.Chargeable = max0(c.sub(pbt, losses))
		var err error
		if t.Exempt, err = st.Exemption(t.Scheme, t.YA, t.Chargeable); err != nil {
			c.fail(&Error{CodeStatutory, "tax", fmt.Sprintf("no %s exemption for YA %d: %v", t.Scheme, t.YA, err), "Activate the sg-cit pack"})
			t.Exempt = zero()
		}
		t.Taxable = c.sub(t.Chargeable, t.Exempt)
		rate, err := st.TaxRate(t.YA)
		if err != nil {
			c.fail(&Error{CodeStatutory, "tax", fmt.Sprintf("no tax rate for YA %d: %v", t.YA, err), "Activate the sg-cit pack"})
			rate = zero()
		}
		t.GrossTax = c.mul(t.Taxable, rate)
		if t.Rebate, err = st.Rebate(t.YA, t.GrossTax); err != nil {
			c.fail(&Error{CodeStatutory, "tax", fmt.Sprintf("the rebate for YA %d: %v", t.YA, err), "Activate the sg-cit pack"})
			t.Rebate = zero()
		}
		t.Tax = c.round(max0(c.sub(t.GrossTax, t.Rebate)))
		profit := max0(pbt)
		t.LossesCarriedForward = c.add(max0(c.sub(losses, profit)), max0(c.neg(pbt)))
		losses = t.LossesCarriedForward
		taxOf[y] = t.Tax
		out.Taxes = append(out.Taxes, t)
	}

	openAP := in.Books.Payables
	openAR := in.Books.Receivables
	cash := c.add(in.Books.Cash, nz(in.CashAdjustment))
	out.OpeningCash = cash
	for n := 1; n <= H; n++ {
		m := at(n)
		m.Tax = zero()
		if n%12 == 0 || n == H {
			m.Tax = taxOf[m.Year]
		}
		m.NetProfit = c.sub(m.ProfitBeforeTax, m.Tax)

		coll := c.add(c.mul(r.Collection[0], revenueOf(n)), c.mul(r.Collection[1], revenueOf(n-1)), c.mul(r.Collection[2], revenueOf(n-2)))
		switch n {
		case 1:
			coll = c.add(coll, c.mul(dec("0.6"), openAR))
		case 2:
			coll = c.add(coll, c.mul(dec("0.4"), openAR))
		}
		m.Collections = c.round(coll)
		m.Suppliers = c.neg(c.add(m.Hosting, m.AI, m.Contractors, m.Fees, m.OpexTotal))
		if n == 1 {
			m.Suppliers = c.sub(m.Suppliers, openAP)
		}
		m.PayrollPaid = c.neg(c.add(m.Payroll, m.PayrollCOGS))
		m.GrantsReceived = m.Grants
		m.InterestPaid = c.neg(m.Interest)
		m.TaxPaid = zero()
		if lag := in.Tax.PaymentLag; n > lag && (n-lag)%12 == 0 && (n-lag)/12 <= years {
			m.TaxPaid = c.neg(taxOf[(n-lag)/12])
		}
		m.Operating = c.add(m.Collections, m.Suppliers, m.PayrollPaid, m.GrantsReceived, m.InterestPaid, m.TaxPaid)

		capex, equity, drawn := zero(), zero(), zero()
		repay := whole(zero())
		for _, b := range buys {
			if b.month == n {
				capex = c.add(capex, b.amount)
			}
		}
		for _, e := range events {
			switch {
			case (e.kind == FundingEquity || e.kind == FundingSAFE) && e.month == n:
				equity = c.add(equity, e.amount)
			case e.kind == FundingLoan && e.month == n:
				drawn = c.add(drawn, e.amount)
			}
			if e.kind == FundingLoan && e.month < n && n <= e.month+e.tenor {
				repay = c.addFrac(repay, c.over(e.amount, d(int64(e.tenor))))
			}
		}
		m.CapexPaid = c.neg(c.round(capex))
		m.Equity = c.round(equity)
		m.LoansDrawn = c.round(drawn)
		m.LoanRepayments = c.neg(c.roundFrac(repay))
		m.NetCashFlow = c.add(m.Operating, m.CapexPaid, m.Equity, m.LoansDrawn, m.LoanRepayments)
		m.CashOpen = cash
		m.CashClose = c.add(cash, m.NetCashFlow)
		cash = m.CashClose

		m.NetBurn = max0(c.neg(c.add(m.Operating, m.CapexPaid)))
		m.ARR = c.mul(m.Subscription, d(12))
		m.SalesMarketing = m.PayrollSM
		if salesLine >= 0 {
			m.SalesMarketing = c.add(m.PayrollSM, m.Opex[salesLine])
		}
		if m.Revenue.Sign() != 0 {
			gm := c.over(m.GrossProfit, m.Revenue)
			m.GrossMargin = &gm
			if churn.Sign() != 0 {
				ltv := c.over(c.mul(m.ARPA, m.GrossProfit), c.mul(m.Revenue, churn))
				m.LTV = &ltv
			}
		}
		if m.NewCustomers.Sign() != 0 {
			cac := c.over(m.SalesMarketing, m.NewCustomers)
			m.CAC = &cac
			if m.LTV != nil && cac.sign() != 0 {
				x := c.over(c.mul(m.LTV.num, cac.den), c.mul(m.LTV.den, cac.num))
				m.LTVToCAC = &x
			}
		}
		if n >= 2 {
			if delta := c.sub(m.ARR, at(n-1).ARR); delta.Sign() > 0 {
				x := c.over(m.NetBurn, delta)
				m.BurnMultiple = &x
			}
		}
	}
	out.Months = months

	// Key results.
	k := &out.Key
	k.RunwayMonths, k.RunwayBeyond = H, true
	for i := range months {
		m := &months[i]
		if k.CashOutMonth == nil && m.CashClose.Sign() < 0 {
			t := m.Date
			k.CashOutMonth, k.RunwayMonths, k.RunwayBeyond = &t, m.N-1, false
		}
		if k.BreakEven == nil && m.Revenue.Sign() > 0 && m.EBITDA.Sign() >= 0 {
			t := m.Date
			k.BreakEven = &t
		}
		if k.MinimumCash == nil || m.CashClose.Cmp(k.MinimumCash) < 0 {
			k.MinimumCash, k.MinimumMonth = m.CashClose, m.Date
		}
	}
	burn12, rev12 := zero(), zero()
	for n := 1; n <= 12; n++ {
		burn12 = c.add(burn12, at(n).NetBurn)
		rev12 = c.add(rev12, at(n).Revenue)
	}
	k.AverageBurn = c.roundFrac(c.over(burn12, d(12)))
	if buffer == nil {
		buffer = zero()
	}
	out.Buffer = buffer
	// buffer × (Σ burn ÷ 12) − minimum cash, at least 0, exactly, then rounded once
	gap := c.over(c.sub(c.mul(buffer, burn12), c.mul(k.MinimumCash, d(12))), d(12))
	if gap.sign() < 0 {
		gap = whole(zero())
	}
	k.FundingGap = c.roundFrac(gap)
	k.RevenueNext12 = rev12
	k.ARREnd = at(H).ARR
	k.CustomersEnd = at(H).CustomersClose

	for y := 1; y <= 5; y++ {
		s := YearSummary{Year: y}
		if 12*(y-1)+1 <= H {
			s.Revenue, s.GrossProfit, s.EBITDA, s.NetProfit, s.NetBurn = zero(), zero(), zero(), zero(), zero()
			for n := 12*(y-1) + 1; n <= min(12*y, H); n++ {
				m := at(n)
				s.Revenue = c.add(s.Revenue, m.Revenue)
				s.GrossProfit = c.add(s.GrossProfit, m.GrossProfit)
				s.EBITDA = c.add(s.EBITDA, m.EBITDA)
				s.NetProfit = c.add(s.NetProfit, m.NetProfit)
				s.NetBurn = c.add(s.NetBurn, m.NetBurn)
			}
		}
		if 12*y <= H {
			m := at(12 * y)
			s.Cash, s.Customers, s.ARR = m.CashClose, m.CustomersClose, m.ARR
			hc := m.Headcount
			s.Headcount = &hc
		}
		out.Years = append(out.Years, s)
	}
	if c.err != nil {
		var e *Error
		if errors.As(c.err, &e) || errors.Is(c.err, ErrDigits) {
			return Result{}, c.err
		}
		return Result{}, &Error{CodeInput, "forecast", c.err.Error(), "Check the drivers"}
	}
	return out, nil
}

// WithoutFunding is the forecast with its funding plan left out: every funding row is in the future, so not yet
// received (FR-PLN-03, design D4).
func WithoutFunding(in Inputs) Inputs {
	in.Funding = nil
	return in
}

func nz(x *apd.Decimal) *apd.Decimal {
	if x == nil {
		return apd.New(0, 0)
	}
	return x
}
