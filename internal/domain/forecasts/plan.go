// Package forecasts keeps the company's driver-based forecasts as documents (PRD-001 FR-PLN-02, FR-PLN-03, FR-PLN-08;
// p1-planning design D5): a forecast is drafted, changed and recorded, never posts, and the latest recorded is the
// company's own, the one Home reads. Its results are worked out on every read by internal/domain/planning from the
// plan, the books' figures at its start and the statutory packs, so a closed month or a new pack moves them.
//
// The plan's types carry the API's documentation as tags: the API takes and gives them as they are.
package forecasts

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/planning"
)

// The forecast's codes.
const (
	CodePlan     = "PLAN-10" // a driver, a plan row or a scenario that does not fit
	CodeRecorded = "PLAN-11" // a recorded forecast changes no more
)

// The limits of a plan, as the database keeps them (FY300).
const (
	MaxName      = 80
	MaxOpex      = 30
	MaxHeadcount = 100
	MaxCapex     = 50
	MaxFunding   = 50
	MaxScenarios = 10
	MaxDelay     = 24
	MaxBuffer    = 36
	MaxLag       = 24
	MaxStartUp   = 3
)

// ForecastPlan is a forecast's drivers, plans and scenarios (design D3, D4): what a person enters. Amounts are decimal
// strings in the base currency, at its minor units; rates are fractions as decimal strings, 0.05 for 5%.
type ForecastPlan struct {
	Name           string             `json:"name" minLength:"1" maxLength:"80" doc:"The forecast's name." example:"FY2027 plan"`
	Horizon        int                `json:"horizon" minimum:"12" maximum:"60" doc:"Months to forecast, 12 to 60." example:"36"`
	LastActual     string             `json:"last_actual,omitempty" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The last month with complete actuals (YYYY-MM): the forecast starts the month after. Left out, the last closed month, else the month before today." example:"2026-10"`
	CashAdjustment string             `json:"cash_adjustment" doc:"Added to the books' cash at the start, such as money already committed; 0 for none." example:"0.00"`
	BufferMonths   *int               `json:"buffer_months,omitempty" minimum:"0" maximum:"36" doc:"Months of average net burn to keep as a buffer for the funding gap; left out, the setting planning.buffer_months (6)." example:"6"`
	Revenue        ForecastRevenue    `json:"revenue" doc:"The revenue drivers."`
	Costs          ForecastCosts      `json:"costs" doc:"The direct-cost percentages and the salary increase."`
	Opex           []ForecastOpex     `json:"opex" maxItems:"30" nullable:"false" doc:"The other operating expenses, each a monthly base that grows, plus a share of revenue."`
	Headcount      []ForecastHire     `json:"headcount" maxItems:"100" nullable:"false" doc:"The team and the planned hires."`
	Capex          []ForecastCapex    `json:"capex" maxItems:"50" nullable:"false" doc:"The purchases of assets."`
	Funding        []ForecastFunding  `json:"funding" maxItems:"50" nullable:"false" doc:"The money planned to be raised or received; none of it is received yet, so runway is also shown without it (FR-PLN-03)."`
	Tax            ForecastTax        `json:"tax" doc:"What the tax estimate needs beyond the sg-cit pack."`
	Scenarios      []ForecastScenario `json:"scenarios,omitempty" minItems:"1" maxItems:"10" doc:"The scenarios; left out, Base, Best and Worst with the workbook's levers. Custom ones carry their own levers and buffer."`
}

// ForecastRevenue are the revenue drivers, monthly unless named otherwise.
type ForecastRevenue struct {
	Customers      string             `json:"customers" doc:"Paying customers at the start; may have one decimal." example:"78"`
	NewCustomers   string             `json:"new_customers" doc:"New customers in the first month; may have one decimal." example:"9"`
	NewGrowth      string             `json:"new_growth" doc:"Monthly growth of new customers, as a fraction." example:"0.04"`
	Churn          string             `json:"churn" doc:"The share of customers at the start of a month who leave in it, as a fraction." example:"0.015"`
	ARPA           string             `json:"arpa" doc:"Revenue per customer per month in year 1." example:"120.00"`
	PriceIncrease  string             `json:"price_increase" doc:"The yearly increase of ARPA, as a fraction." example:"0.05"`
	Services       string             `json:"services" doc:"Services revenue in the first month." example:"30000.00"`
	ServicesGrowth string             `json:"services_growth" doc:"Monthly growth of services, as a fraction." example:"0.01"`
	Other          string             `json:"other" doc:"Other revenue each month." example:"0.00"`
	Collection     ForecastCollection `json:"collection" doc:"When a month's revenue is collected; together 100%, else ⚠."`
}

// ForecastCollection is the collection pattern: the share of a month's revenue collected in it and in the two after.
type ForecastCollection struct {
	ThisMonth  string `json:"this_month" doc:"Collected in the month itself, as a fraction." example:"0.3"`
	NextMonth  string `json:"next_month" doc:"Collected the month after, as a fraction." example:"0.6"`
	MonthAfter string `json:"month_after" doc:"Collected two months after, as a fraction." example:"0.1"`
}

// ForecastCosts are the direct-cost percentages and the yearly salary increase.
type ForecastCosts struct {
	Hosting        string `json:"hosting" doc:"Hosting, as a fraction of subscription revenue." example:"0.15"`
	AI             string `json:"ai" doc:"AI and API costs, as a fraction of total revenue." example:"0.05"`
	Contractors    string `json:"contractors" doc:"Contractors, as a fraction of services revenue." example:"0.05"`
	Fees           string `json:"fees" doc:"Payment fees, as a fraction of total revenue." example:"0.012"`
	SalaryIncrease string `json:"salary_increase" doc:"Each forecast year's salary increase, as a fraction." example:"0.04"`
}

// ForecastOpex is one other operating expense.
type ForecastOpex struct {
	Label   string `json:"label" minLength:"1" maxLength:"80" doc:"What it is." example:"Sales & marketing"`
	Group   string `json:"report_group,omitempty" maxLength:"80" doc:"The chart's income or expense report group it plans, by name: the books' average of the last three months is shown beside it, and a group whose accounts are all S&M adds the line to S&M for CAC." example:"Sales & marketing"`
	Base    string `json:"base" doc:"Its amount in the first month." example:"2500.00"`
	Growth  string `json:"growth" doc:"Its monthly growth, as a fraction." example:"0.02"`
	Percent string `json:"percent" doc:"A share of total revenue added each month, as a fraction." example:"0"`
}

// ForecastHire is one headcount row.
type ForecastHire struct {
	Role     string `json:"role" maxLength:"80" doc:"The person or the role." example:"Senior engineer (hire)"`
	Function string `json:"function" enum:"COGS,R&D,S&M,G&A" doc:"Where the cost goes: COGS (cost of revenue), R&D, S&M or G&A." example:"R&D"`
	Start    string `json:"start" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The first month (YYYY-MM); one before the forecast is the current team, whom the hiring delay does not move." example:"2027-01"`
	End      string `json:"end,omitempty" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The last month, if they leave." example:"2028-02"`
	Salary   string `json:"salary" doc:"The monthly salary at today's rate: it rises by the salary increase each forecast year." example:"8500.00"`
	CPF      bool   `json:"cpf" doc:"true: employer CPF at the rate for 55 and below up to the Ordinary Wage ceiling (sg-cpf), as the workbook; SDL applies to everyone (sg-sdl)." example:"true"`
}

// ForecastCapex is one purchase of an asset.
type ForecastCapex struct {
	Item   string `json:"item,omitempty" maxLength:"80" doc:"What is bought." example:"Laptops for 2027 hires"`
	Amount string `json:"amount" doc:"Its cost, paid in its month." example:"8000.00"`
	Month  string `json:"month" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month it is bought (YYYY-MM); one before the forecast counts as the first month." example:"2027-01"`
	Life   int    `json:"life" minimum:"1" maximum:"600" doc:"Months it is depreciated over, straight line." example:"36"`
}

// ForecastFunding is one funding event.
type ForecastFunding struct {
	Type   string `json:"type" enum:"Equity,SAFE,Loan,Grant" doc:"Equity, a SAFE or a loan raised, which the fundraising levers move and scale; or a grant, which arrives as planned." example:"Loan"`
	Month  string `json:"month" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month the money arrives (YYYY-MM)." example:"2027-02"`
	Amount string `json:"amount" doc:"The amount." example:"100000.00"`
	Rate   string `json:"rate,omitempty" doc:"A loan's yearly interest rate, as a fraction." example:"0.055"`
	Tenor  int    `json:"tenor,omitempty" minimum:"0" maximum:"360" doc:"A loan's months of equal repayments, from the month after it is drawn." example:"36"`
	Notes  string `json:"notes,omitempty" maxLength:"200" doc:"Notes." example:"SME working-capital loan"`
}

// ForecastTax is what the tax estimate needs beyond the sg-cit pack.
type ForecastTax struct {
	StartUpYears         int    `json:"start_up_years" minimum:"0" maximum:"3" doc:"Forecast years under the start-up tax exemption, a qualifying company's first three Years of Assessment; the rest are under the partial exemption." example:"3"`
	LossesBroughtForward string `json:"losses_brought_forward" doc:"Unabsorbed tax losses at the start." example:"0.00"`
	PaymentLag           int    `json:"payment_lag" minimum:"0" maximum:"24" doc:"Months from a forecast year's end to the payment of its tax." example:"9"`
}

// ForecastScenario is a named set of levers.
type ForecastScenario struct {
	Name         string         `json:"name" minLength:"1" maxLength:"40" doc:"Its name." example:"Worst"`
	Levers       ForecastLevers `json:"levers" doc:"How it moves the plan."`
	BufferMonths *int           `json:"buffer_months,omitempty" minimum:"0" maximum:"36" doc:"Its own buffer, in months of burn; left out, the forecast's." example:"3"`
}

// ForecastLevers multiply or delay the plan.
type ForecastLevers struct {
	NewCustomers string `json:"new_customers" doc:"× new customers." example:"0.7"`
	Churn        string `json:"churn" doc:"× churn." example:"1.3"`
	Price        string `json:"price" doc:"× ARPA." example:"0.9"`
	Services     string `json:"services" doc:"× services revenue." example:"0.7"`
	Opex         string `json:"opex" doc:"× each other operating expense." example:"1.1"`
	DirectCosts  string `json:"direct_costs" doc:"× each direct-cost percentage." example:"1.1"`
	FundAmount   string `json:"fund_amount" doc:"× the money raised (not grants)." example:"0.6"`
	HireDelay    int    `json:"hire_delay" minimum:"0" maximum:"24" doc:"Months every planned hire waits." example:"3"`
	FundDelay    int    `json:"fund_delay" minimum:"0" maximum:"24" doc:"Months the money raised waits (not grants)." example:"3"`
}

func lever(s planning.Scenario) ForecastLevers {
	l := s.Levers
	return ForecastLevers{l.NewCustomers.Text('f'), l.Churn.Text('f'), l.Price.Text('f'), l.Services.Text('f'), l.Opex.Text('f'),
		l.DirectCosts.Text('f'), l.FundAmount.Text('f'), l.HireDelay, l.FundDelay}
}

// DefaultScenarios are Base, Best and Worst with the workbook's levers.
func DefaultScenarios() []ForecastScenario {
	var out []ForecastScenario
	for _, s := range []planning.Scenario{planning.Base, planning.Best, planning.Worst} {
		out = append(out, ForecastScenario{Name: s.Name, Levers: lever(s)})
	}
	return out
}

var (
	monthPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)
	plainDecimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,14})(\.[0-9]+)?$`)
	signedAmount = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,14})(\.[0-9]+)?$`)
)

// Month reads YYYY-MM as its first day.
func Month(s string) (time.Time, error) { return time.ParseInLocation("2006-01", s, time.UTC) }

// Check is everything wrong with a plan, every problem at once (ENG-22), for amounts in cur: each with its field, what
// is wrong and how to fix it (ENG-23).
func Check(p ForecastPlan, cur money.Currency) ledger.Problems {
	var probs ledger.Problems
	add := func(field, msg, fix string) {
		probs = append(probs, ledger.Problem{Code: CodePlan, Field: field, Message: msg, Fix: fix})
	}
	// decimal checks a non-negative decimal with at most places decimals and at most top (0: no limit).
	decimal := func(field, s string, places int, top string, what, example string) {
		if !plainDecimal.MatchString(s) {
			add(field, fmt.Sprintf("%q is not a decimal of zero or more", s), "Enter "+what+", such as "+example)
			return
		}
		if i := strings.IndexByte(s, '.'); i >= 0 && len(s)-i-1 > places {
			add(field, fmt.Sprintf("%s has more than %d decimals", s, places), "Enter "+what+" with at most "+fmt.Sprint(places)+" decimals, such as "+example)
			return
		}
		if top != "" {
			x, _, _ := apd.NewFromString(s)
			t, _, _ := apd.NewFromString(top)
			if x.Cmp(t) > 0 {
				add(field, fmt.Sprintf("%s is more than %s", s, top), "Enter "+what+" up to "+top)
			}
		}
	}
	amount := func(field, s string) { decimal(field, s, cur.MinorUnits, "", "an amount", "2500.00") }
	fraction := func(field, s string) { decimal(field, s, 6, "1", "a fraction", "0.05 for 5%") }
	growth := func(field, s string) { decimal(field, s, 6, "9.999999", "a growth rate as a fraction", "0.04 for 4%") }
	month := func(field, s string) {
		if !monthPattern.MatchString(s) {
			add(field, fmt.Sprintf("%q is not a month", s), "Enter the month as YYYY-MM, such as 2027-01")
		}
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(p.Name)); n == 0 || n > MaxName {
		add("name", "the name is blank or longer than 80 characters", "Name the forecast, such as FY2027 plan")
	}
	if p.Horizon < planning.MinHorizon || p.Horizon > planning.MaxHorizon {
		add("horizon", fmt.Sprintf("%d months is outside 12 to 60", p.Horizon), "Forecast between 12 and 60 months")
	}
	if p.LastActual != "" {
		month("last_actual", p.LastActual)
	}
	if !signedAmount.MatchString(p.CashAdjustment) {
		add("cash_adjustment", fmt.Sprintf("%q is not an amount", p.CashAdjustment), "Enter an amount, such as -5000.00, or 0")
	} else if i := strings.IndexByte(p.CashAdjustment, '.'); i >= 0 && len(p.CashAdjustment)-i-1 > cur.MinorUnits {
		add("cash_adjustment", "it has more decimals than the currency", "Enter it in "+cur.Code+" at its minor units")
	}
	buffer := func(field string, b *int) {
		if b != nil && (*b < 0 || *b > MaxBuffer) {
			add(field, fmt.Sprintf("%d months is outside 0 to 36", *b), "Enter the months of burn to keep, 0 to 36")
		}
	}
	buffer("buffer_months", p.BufferMonths)
	r := p.Revenue
	decimal("revenue.customers", r.Customers, 1, "999999999", "a number of customers", "78")
	decimal("revenue.new_customers", r.NewCustomers, 1, "999999999", "a number of customers", "9")
	growth("revenue.new_growth", r.NewGrowth)
	fraction("revenue.churn", r.Churn)
	amount("revenue.arpa", r.ARPA)
	growth("revenue.price_increase", r.PriceIncrease)
	amount("revenue.services", r.Services)
	growth("revenue.services_growth", r.ServicesGrowth)
	amount("revenue.other", r.Other)
	fraction("revenue.collection.this_month", r.Collection.ThisMonth)
	fraction("revenue.collection.next_month", r.Collection.NextMonth)
	fraction("revenue.collection.month_after", r.Collection.MonthAfter)
	c := p.Costs
	fraction("costs.hosting", c.Hosting)
	fraction("costs.ai", c.AI)
	fraction("costs.contractors", c.Contractors)
	fraction("costs.fees", c.Fees)
	growth("costs.salary_increase", c.SalaryIncrease)
	if len(p.Opex) > MaxOpex || len(p.Headcount) > MaxHeadcount || len(p.Capex) > MaxCapex || len(p.Funding) > MaxFunding {
		add("plan", "it has more rows than a forecast keeps", "Keep at most 30 expense lines, 100 people, 50 purchases and 50 funding rows")
	}
	for i, o := range p.Opex {
		at := fmt.Sprintf("opex[%d]", i)
		if n := utf8.RuneCountInString(strings.TrimSpace(o.Label)); n == 0 || n > 80 {
			add(at+".label", "the label is blank or longer than 80 characters", "Say what the expense is, such as Rent")
		}
		if utf8.RuneCountInString(o.Group) > 80 {
			add(at+".report_group", "the report group is longer than 80 characters", "Name a report group of the chart, such as Sales & marketing, or leave it out")
		}
		amount(at+".base", o.Base)
		growth(at+".growth", o.Growth)
		fraction(at+".percent", o.Percent)
	}
	for i, h := range p.Headcount {
		at := fmt.Sprintf("headcount[%d]", i)
		switch h.Function {
		case planning.FunctionCOGS, planning.FunctionRD, planning.FunctionSM, planning.FunctionGA:
		default:
			add(at+".function", fmt.Sprintf("%q is not COGS, R&D, S&M or G&A", h.Function), "Choose where the cost goes")
		}
		month(at+".start", h.Start)
		if h.End != "" {
			month(at+".end", h.End)
			if h.End < h.Start {
				add(at+".end", "they leave before they start", "Enter a last month on or after the first")
			}
		}
		if utf8.RuneCountInString(h.Role) > 80 {
			add(at+".role", "the role is longer than 80 characters", "Shorten it")
		}
		amount(at+".salary", h.Salary)
	}
	for i, x := range p.Capex {
		at := fmt.Sprintf("capex[%d]", i)
		amount(at+".amount", x.Amount)
		month(at+".month", x.Month)
		if x.Life < 1 || x.Life > 600 {
			add(at+".life", fmt.Sprintf("%d months is outside 1 to 600", x.Life), "Enter the months it is depreciated over, such as 36")
		}
	}
	for i, f := range p.Funding {
		at := fmt.Sprintf("funding[%d]", i)
		switch f.Type {
		case planning.FundingEquity, planning.FundingSAFE, planning.FundingGrant:
			if f.Rate != "" || f.Tenor != 0 {
				add(at, "only a loan has a rate and a tenor", "Leave the rate and the tenor out, or make it a loan")
			}
		case planning.FundingLoan:
			fraction(at+".rate", f.Rate)
			if f.Tenor < 1 || f.Tenor > 360 {
				add(at+".tenor", fmt.Sprintf("%d months is outside 1 to 360", f.Tenor), "Enter the months of repayments, such as 36")
			}
		default:
			add(at+".type", fmt.Sprintf("%q is not Equity, SAFE, Loan or Grant", f.Type), "Choose the kind of funding")
		}
		month(at+".month", f.Month)
		amount(at+".amount", f.Amount)
	}
	t := p.Tax
	if t.StartUpYears < 0 || t.StartUpYears > MaxStartUp {
		add("tax.start_up_years", fmt.Sprintf("%d is outside 0 to 3", t.StartUpYears), "Enter the forecast years left under the start-up exemption, at most 3")
	}
	amount("tax.losses_brought_forward", t.LossesBroughtForward)
	if t.PaymentLag < 0 || t.PaymentLag > MaxLag {
		add("tax.payment_lag", fmt.Sprintf("%d months is outside 0 to 24", t.PaymentLag), "Enter the months from a year's end to its tax payment, such as 9")
	}
	if len(p.Scenarios) == 0 || len(p.Scenarios) > MaxScenarios {
		add("scenarios", "a forecast has 1 to 10 scenarios", "Keep Base, and add Best, Worst or your own")
	}
	seen := map[string]bool{}
	for i, s := range p.Scenarios {
		at := fmt.Sprintf("scenarios[%d]", i)
		name := strings.TrimSpace(s.Name)
		if n := utf8.RuneCountInString(name); n == 0 || n > 40 || seen[strings.ToLower(name)] {
			add(at+".name", "the name is blank, longer than 40 characters, or used twice", "Give each scenario its own name")
		}
		seen[strings.ToLower(name)] = true
		l := s.Levers
		for field, v := range map[string]string{"new_customers": l.NewCustomers, "churn": l.Churn, "price": l.Price, "services": l.Services,
			"opex": l.Opex, "direct_costs": l.DirectCosts, "fund_amount": l.FundAmount} {
			decimal(at+".levers."+field, v, 2, "9.99", "a multiplier", "1.1")
		}
		if l.HireDelay < 0 || l.HireDelay > MaxDelay || l.FundDelay < 0 || l.FundDelay > MaxDelay {
			add(at+".levers", "a delay is outside 0 to 24 months", "Enter the delays in months, 0 to 24")
		}
		buffer(at+".buffer_months", s.BufferMonths)
	}
	if len(probs) > 0 {
		return probs
	}
	return nil
}

func dec(s string) *apd.Decimal {
	if s == "" {
		return apd.New(0, 0)
	}
	x, _, err := apd.NewFromString(s)
	if err != nil {
		return apd.New(0, 0) // Check refused it first
	}
	return x
}

// Inputs are the engine's inputs for a checked plan, its start and the books' figures.
func Inputs(p ForecastPlan, cur money.Currency, start time.Time, books planning.Books, bufferMonths int) planning.Inputs {
	buffer := bufferMonths
	if p.BufferMonths != nil {
		buffer = *p.BufferMonths
	}
	m := func(s string) time.Time {
		t, _ := Month(s)
		return t
	}
	in := planning.Inputs{Currency: cur, Start: start, Horizon: p.Horizon, Books: books, CashAdjustment: dec(p.CashAdjustment),
		BufferMonths: apd.New(int64(buffer), 0),
		Revenue: planning.RevenueDrivers{Customers: dec(p.Revenue.Customers), NewCustomers: dec(p.Revenue.NewCustomers),
			NewGrowth: dec(p.Revenue.NewGrowth), Churn: dec(p.Revenue.Churn), ARPA: dec(p.Revenue.ARPA),
			PriceIncrease: dec(p.Revenue.PriceIncrease), Services: dec(p.Revenue.Services), ServicesGrowth: dec(p.Revenue.ServicesGrowth),
			Other: dec(p.Revenue.Other), Collection: [3]*apd.Decimal{dec(p.Revenue.Collection.ThisMonth), dec(p.Revenue.Collection.NextMonth), dec(p.Revenue.Collection.MonthAfter)}},
		Costs: planning.CostDrivers{Hosting: dec(p.Costs.Hosting), AI: dec(p.Costs.AI), Contractors: dec(p.Costs.Contractors),
			Fees: dec(p.Costs.Fees), SalaryIncrease: dec(p.Costs.SalaryIncrease)},
		Tax: planning.TaxInputs{StartUpYears: p.Tax.StartUpYears, LossesBroughtForward: dec(p.Tax.LossesBroughtForward), PaymentLag: p.Tax.PaymentLag},
	}
	for _, o := range p.Opex {
		in.Opex = append(in.Opex, planning.OpexLine{Label: o.Label, Group: o.Group, Base: dec(o.Base), Growth: dec(o.Growth), Percent: dec(o.Percent)})
	}
	for _, h := range p.Headcount {
		x := planning.Hire{Role: h.Role, Function: h.Function, Start: m(h.Start), Salary: dec(h.Salary), CPF: h.CPF}
		if h.End != "" {
			e := m(h.End)
			x.End = &e
		}
		in.Headcount = append(in.Headcount, x)
	}
	for _, x := range p.Capex {
		in.Capex = append(in.Capex, planning.Capex{Amount: dec(x.Amount), Month: m(x.Month), Life: x.Life})
	}
	for _, f := range p.Funding {
		in.Funding = append(in.Funding, planning.Funding{Type: f.Type, Month: m(f.Month), Amount: dec(f.Amount), Rate: dec(f.Rate), Tenor: f.Tenor})
	}
	return in
}

// ScenarioOf is a checked scenario as the engine runs it.
func ScenarioOf(s ForecastScenario) planning.Scenario {
	l := s.Levers
	out := planning.Scenario{Name: strings.TrimSpace(s.Name), Levers: planning.Levers{NewCustomers: dec(l.NewCustomers), Churn: dec(l.Churn),
		Price: dec(l.Price), Services: dec(l.Services), Opex: dec(l.Opex), DirectCosts: dec(l.DirectCosts), FundAmount: dec(l.FundAmount),
		HireDelay: l.HireDelay, FundDelay: l.FundDelay}}
	if s.BufferMonths != nil {
		out.BufferMonths = apd.New(int64(*s.BufferMonths), 0)
	}
	return out
}
