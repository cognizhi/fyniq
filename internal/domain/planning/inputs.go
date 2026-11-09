// Package planning is the driver-based forecast and the feasibility calculator (PRD-001 §8.14, FR-PLN-02, FR-PLN-03,
// FR-PLN-06, FR-PLN-08; p1-planning design D2 to D4, D8). The model is the workbook's own, as its reference model
// states it (zh-sheet-accounting tests/ref_planning.py), worked in exact decimals under RND-PLAN, with the statutory
// figures from the packs. Every function is pure: the inputs, the books' starting figures and the packs in, the
// figures out; nothing here posts, reads the clock or touches storage.
package planning

import (
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
)

// The payroll functions of a headcount row.
const (
	FunctionCOGS = "COGS" // cost of revenue
	FunctionRD   = "R&D"
	FunctionSM   = "S&M"
	FunctionGA   = "G&A"
)

// The kinds of funding.
const (
	FundingEquity = "Equity"
	FundingSAFE   = "SAFE"
	FundingLoan   = "Loan"
	FundingGrant  = "Grant" // not raised: the fundraising levers leave it as planned
)

// The horizon's limits, in months (FR-PLN-02).
const (
	MinHorizon = 12
	MaxHorizon = 60
)

// Inputs are one forecast's drivers and plans, with the books' figures at its start (design D3).
type Inputs struct {
	Currency money.Currency
	Start    time.Time // the first day of the forecast's first month
	Horizon  int       // months, 12 to 60

	Books          Books
	CashAdjustment *apd.Decimal // added to the books' cash at the start
	BufferMonths   *apd.Decimal // months of average net burn to keep (planning.buffer_months, or the scenario's)

	Revenue   RevenueDrivers
	Costs     CostDrivers
	Opex      []OpexLine
	Headcount []Hire
	Capex     []Capex
	Funding   []Funding
	Tax       TaxInputs
}

// Books are the books' figures at the last actual month's end, through the balance service.
type Books struct {
	Cash        *apd.Decimal // the cash and cash equivalents accounts
	Receivables *apd.Decimal // owed to the company: collected 60% in month 1 and 40% in month 2
	Payables    *apd.Decimal // owed by the company, as a positive amount: paid in month 1
	// Depreciation is each forecast month's planned depreciation of the assets already held (month 1 first), from
	// the fixed-asset register's schedules; a month past the slice has none.
	Depreciation []*apd.Decimal
}

// RevenueDrivers are the revenue drivers, monthly unless named otherwise; rates are fractions.
type RevenueDrivers struct {
	Customers      *apd.Decimal    // customers at the start
	NewCustomers   *apd.Decimal    // new customers in month 1
	NewGrowth      *apd.Decimal    // monthly growth of new customers
	Churn          *apd.Decimal    // monthly churn of the customers at the start of each month
	ARPA           *apd.Decimal    // revenue per customer per month in year 1
	PriceIncrease  *apd.Decimal    // yearly increase of ARPA
	Services       *apd.Decimal    // services revenue in month 1
	ServicesGrowth *apd.Decimal    // monthly growth of services
	Other          *apd.Decimal    // other revenue each month
	Collection     [3]*apd.Decimal // the share of a month's revenue collected in that month, the next and the one after
}

// CostDrivers are the direct-cost percentages, as fractions, and the yearly salary increase.
type CostDrivers struct {
	Hosting        *apd.Decimal // of subscription revenue
	AI             *apd.Decimal // AI and API costs, of total revenue
	Contractors    *apd.Decimal // of services revenue
	Fees           *apd.Decimal // payment fees, of total revenue
	SalaryIncrease *apd.Decimal // each forecast year
}

// OpexLine is one other operating expense: base × (1 + growth)ⁿ⁻¹ + percent × total revenue each month.
type OpexLine struct {
	Label   string
	Group   string       // the chart's report group it plans, if any: its hint, and S&M when it is sales_marketing
	Base    *apd.Decimal // in month 1
	Growth  *apd.Decimal // monthly
	Percent *apd.Decimal // of total revenue
}

// GroupSalesMarketing is the report group whose other-expense line adds to S&M for CAC.
const GroupSalesMarketing = "sales_marketing"

// Hire is one headcount row: a person or a role, from a month to a month.
type Hire struct {
	Role     string
	Function string     // COGS, R&D, S&M or G&A
	Start    time.Time  // the month they start; one before the forecast is the current team
	End      *time.Time // the last month, if they leave
	Salary   *apd.Decimal
	CPF      bool // employer CPF and the ceiling apply
}

// Capex is one purchase: its amount in its month, depreciated straight over its life in months.
type Capex struct {
	Amount *apd.Decimal
	Month  time.Time
	Life   int
}

// Funding is one funding event: equity, a SAFE or a loan raised, or a grant received.
type Funding struct {
	Type   string
	Month  time.Time
	Amount *apd.Decimal
	Rate   *apd.Decimal // a loan's yearly interest rate
	Tenor  int          // a loan's months of equal principal repayments
}

// TaxInputs are what the tax estimate needs beyond the packs.
type TaxInputs struct {
	StartUpYears         int          // forecast years under the start-up exemption, the rest under the partial (3 by default)
	LossesBroughtForward *apd.Decimal // unabsorbed losses at the start
	PaymentLag           int          // months from a forecast year's end to the payment of its tax (9 by default)
}

// Levers multiply or delay the plan for a scenario (design D4).
type Levers struct {
	NewCustomers, Churn, Price, Services, Opex, DirectCosts, FundAmount *apd.Decimal
	HireDelay, FundDelay                                                int
}

// Scenario is a named set of levers, with its own buffer months if it sets them.
type Scenario struct {
	Name         string
	Levers       Levers
	BufferMonths *apd.Decimal
}

func dec(s string) *apd.Decimal {
	x, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return x
}

// Base, Best and Worst, with the workbook's lever values.
var (
	Base  = Scenario{Name: "Base", Levers: Levers{dec("1"), dec("1"), dec("1"), dec("1"), dec("1"), dec("1"), dec("1"), 0, 0}}
	Best  = Scenario{Name: "Best", Levers: Levers{dec("1.3"), dec("0.8"), dec("1.1"), dec("1.2"), dec("0.95"), dec("0.95"), dec("1"), 0, 0}}
	Worst = Scenario{Name: "Worst", Levers: Levers{dec("0.7"), dec("1.3"), dec("0.9"), dec("0.7"), dec("1.1"), dec("1.1"), dec("0.6"), 3, 3}}
)

// Statutory gives the figures the forecast reads from the packs: for a month, or for a Year of Assessment.
type Statutory interface {
	// EmployerCPF is the employer's rate for 55 and below on wages above S$750, and the Ordinary Wage ceiling, for
	// wages earned in month: the workbook's one rate for every employee with CPF (design D3, Risks).
	EmployerCPF(month time.Time) (rate, ceiling *apd.Decimal, err error)
	// SDL is the levy on one employee's wages for month.
	SDL(month time.Time, wages *apd.Decimal) (*apd.Decimal, error)
	// TaxRate is the rate on normal chargeable income for the YA.
	TaxRate(ya int) (*apd.Decimal, error)
	// Exemption is the amount the scheme ("sute" or "pte") exempts of chargeable in the YA.
	Exemption(scheme string, ya int, chargeable *apd.Decimal) (*apd.Decimal, error)
	// Rebate is the YA's rebate on tax, zero when the YA has none.
	Rebate(ya int, tax *apd.Decimal) (*apd.Decimal, error)
}
