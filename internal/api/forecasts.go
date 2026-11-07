package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/forecasts"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/planning"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ forecasts (FR-PLN-02, FR-PLN-03, FR-PLN-08)
//
// A forecast is the workbook's driver-based model (the owner's decision of 8 Nov 2026, PRD-001 rev 0.214): drafted,
// changed and recorded as a document that posts nothing, its results worked out on every read from the plan, the
// books' figures at its start and the statutory packs (p1-planning design D3 to D5). The latest recorded is the
// company's own, the one Home reads. runForecast works a plan out without saving it.

// ForecastCodes are the codes a forecast answers besides the common ones.
var ForecastCodes = []string{forecasts.CodePlan, forecasts.CodeRecorded, planning.CodeHorizon, planning.CodeStatutory, planning.CodeInput,
	planning.CodeDigits, storage.CodeForecast}

// ForecastView is a forecast, drafted or recorded.
type ForecastView struct {
	ID         string                 `json:"id" format:"uuid" doc:"The forecast's ID." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9c01"`
	Status     string                 `json:"status" enum:"draft,recorded,superseded" doc:"draft: a sandbox to change and run; recorded: the latest recorded, the company's own and Home's; superseded: a later one is recorded." example:"recorded"`
	Version    int                    `json:"version" doc:"The version of its content; with its status it makes the ETag." example:"1"`
	Plan       forecasts.ForecastPlan `json:"plan" doc:"Its drivers, plans and scenarios."`
	RecordedBy *Person                `json:"recorded_by,omitempty" doc:"Who recorded it."`
	RecordedAt *time.Time             `json:"recorded_at,omitempty" doc:"When it was recorded." example:"2026-11-20T09:30:00Z"`
}

// ForecastPage is a page of forecasts: the latest recorded first, then the others recorded, then the drafts.
type ForecastPage struct {
	Items      []ForecastView `json:"items" nullable:"false"`
	NextCursor string         `json:"next_cursor,omitempty" doc:"Send it as cursor for the next page; absent on the last page." example:"eyJhZnRlciI6IjIwMjYifQ"`
}

// ForecastHint is the books' average of a report group over the last three actual months.
type ForecastHint struct {
	ReportGroup string `json:"report_group" doc:"The chart's report group." example:"Sales & marketing"`
	Average     string `json:"average" doc:"Its average a month over the last three actual months, from the balance service." example:"2410.55"`
}

// ForecastBooks are the books' figures a forecast starts from (design D3).
type ForecastBooks struct {
	LastActual  string         `json:"last_actual" doc:"The last month with complete actuals (YYYY-MM)." example:"2026-10"`
	Start       string         `json:"start" doc:"The forecast's first month (YYYY-MM)." example:"2026-11"`
	Cash        string         `json:"cash" doc:"The cash and cash equivalents at the last actual month's end." example:"791132.86"`
	Receivables string         `json:"receivables" doc:"Owed to the company then, on the receivables control account: collected 60% in month 1 and 40% in month 2, as the workbook." example:"40745.00"`
	Payables    string         `json:"payables" doc:"Owed by the company then, on the payables control account: paid in month 1." example:"5677.00"`
	Hints       []ForecastHint `json:"hints" nullable:"false" doc:"Each report group's average over the last three actual months, for the expense lines."`
	Revision    int64          `json:"ledger_revision" doc:"The ledger revision they were read at (ENG-46)." example:"42"`
}

// ForecastMonth is one forecast month's lines (RND-PLAN): driver lines rounded once, totals adding the rounded lines.
type ForecastMonth struct {
	Month           string              `json:"month" doc:"The month (YYYY-MM)." example:"2026-11"`
	Year            int                 `json:"year" doc:"The forecast year, 1 for months 1 to 12." example:"1"`
	CustomersOpen   string              `json:"customers_open" doc:"Customers at the start of the month, to 1 decimal (they stay fractional, as the workbook)." example:"78.0"`
	NewCustomers    string              `json:"new_customers" doc:"New customers, to 1 decimal." example:"9.0"`
	LostCustomers   string              `json:"lost_customers" doc:"Customers lost, to 1 decimal." example:"1.2"`
	Customers       string              `json:"customers" doc:"Customers at the end of the month, to 1 decimal." example:"85.8"`
	ARPA            string              `json:"arpa" doc:"Revenue per customer." example:"120.00"`
	Subscription    string              `json:"subscription_revenue" doc:"Customers × ARPA." example:"10296.00"`
	Services        string              `json:"services_revenue" doc:"Services revenue." example:"30000.00"`
	Other           string              `json:"other_revenue" doc:"Other revenue." example:"0.00"`
	Revenue         string              `json:"revenue" doc:"Total revenue." example:"40296.00"`
	Hosting         string              `json:"hosting" doc:"Hosting: its share of subscription revenue." example:"1544.40"`
	AI              string              `json:"ai" doc:"AI and API costs: their share of revenue." example:"2014.80"`
	Contractors     string              `json:"contractors" doc:"Contractors: their share of services revenue." example:"1500.00"`
	Fees            string              `json:"fees" doc:"Payment fees: their share of revenue." example:"483.55"`
	PayrollCOGS     string              `json:"payroll_cogs" doc:"The payroll of the COGS function, with employer CPF and SDL." example:"0.00"`
	DirectCosts     string              `json:"direct_costs" doc:"Total direct costs." example:"5542.75"`
	GrossProfit     string              `json:"gross_profit" doc:"Revenue less direct costs." example:"34753.25"`
	GrossMargin     string              `json:"gross_margin,omitempty" doc:"Gross profit as a percentage of revenue (RND-PERCENT); absent without revenue." example:"86.2"`
	PayrollRD       string              `json:"payroll_rd" doc:"The R&D payroll." example:"20871.75"`
	PayrollSM       string              `json:"payroll_sm" doc:"The S&M payroll." example:"5627.25"`
	PayrollGA       string              `json:"payroll_ga" doc:"The G&A payroll." example:"7031.25"`
	Payroll         string              `json:"payroll" doc:"The payroll of R&D, S&M and G&A." example:"33530.25"`
	Headcount       int                 `json:"headcount" doc:"People employed in the month." example:"5"`
	Opex            []ExpenseLineAmount `json:"other_expenses" nullable:"false" doc:"Each other expense line, in the plan's order."`
	OpexTotal       string              `json:"other_expenses_total" doc:"Their total." example:"7350.00"`
	EBITDA          string              `json:"ebitda" doc:"Gross profit less payroll and other expenses." example:"-6127.00"`
	Depreciation    string              `json:"depreciation" doc:"The register's assets and the planned capex, straight line." example:"616.67"`
	Grants          string              `json:"grants" doc:"Grants received." example:"0.00"`
	Interest        string              `json:"interest" doc:"Interest on the loans' balance at the start of the month." example:"0.00"`
	ProfitBeforeTax string              `json:"profit_before_tax" doc:"EBITDA less depreciation and interest, plus grants." example:"-6743.67"`
	Tax             string              `json:"tax" doc:"Corporate tax, booked in a forecast year's last month." example:"0.00"`
	NetProfit       string              `json:"net_profit" doc:"Profit before tax less tax." example:"-6743.67"`
	Collections     string              `json:"collections" doc:"Cash from customers, by the collection pattern." example:"36573.80"`
	Suppliers       string              `json:"suppliers" doc:"Cash to suppliers (negative)." example:"-12892.75"`
	PayrollPaid     string              `json:"payroll_paid" doc:"Cash to staff (negative)." example:"-33530.25"`
	GrantsReceived  string              `json:"grants_received" doc:"Cash from grants." example:"0.00"`
	InterestPaid    string              `json:"interest_paid" doc:"Interest paid (negative)." example:"0.00"`
	TaxPaid         string              `json:"tax_paid" doc:"Tax paid, the payment lag after its year (negative)." example:"0.00"`
	Operating       string              `json:"operating_cash_flow" doc:"The operating cash flow." example:"-9849.20"`
	Capex           string              `json:"capex" doc:"Cash for assets bought (negative)." example:"0.00"`
	Equity          string              `json:"equity_raised" doc:"Cash from equity and SAFEs." example:"0.00"`
	Loans           string              `json:"loans_drawn" doc:"Cash from loans drawn." example:"0.00"`
	Repayments      string              `json:"loan_repayments" doc:"Loan principal repaid (negative)." example:"0.00"`
	NetCashFlow     string              `json:"net_cash_flow" doc:"The month's net cash flow." example:"-9849.20"`
	CashOpen        string              `json:"cash_open" doc:"Cash at the start of the month." example:"791132.86"`
	Cash            string              `json:"cash" doc:"Cash at the end of the month." example:"781283.66"`
	NetBurn         string              `json:"net_burn" doc:"Cash used by operations and capex, 0 when they bring cash in." example:"9849.20"`
	ARR             string              `json:"arr" doc:"12 × subscription revenue." example:"123552.00"`
	SalesMarketing  string              `json:"sales_marketing" doc:"S&M payroll and the expense line of a report group whose accounts are all S&M." example:"8127.25"`
	CAC             string              `json:"cac,omitempty" doc:"S&M ÷ new customers; absent without new customers." example:"903.03"`
	LTV             string              `json:"ltv,omitempty" doc:"ARPA × gross margin ÷ churn; absent without revenue or churn." example:"6898.50"`
	LTVToCAC        string              `json:"ltv_to_cac,omitempty" doc:"LTV ÷ CAC, to 2 decimals; absent when either is." example:"7.64"`
	BurnMultiple    string              `json:"burn_multiple,omitempty" doc:"Net burn ÷ the month's increase in ARR, to 2 decimals; absent without an increase." example:"1.35"`
}

// ForecastTaxYear is one forecast year's tax estimate.
type ForecastTaxYear struct {
	Year                 int    `json:"year" doc:"The forecast year." example:"1"`
	YA                   int    `json:"ya" doc:"The Year of Assessment: the year after the calendar year the forecast year ends in." example:"2028"`
	Scheme               string `json:"scheme" enum:"sute,pte" doc:"sute: the start-up tax exemption; pte: the partial tax exemption." example:"sute"`
	ProfitBeforeTax      string `json:"profit_before_tax" doc:"The year's profit before tax." example:"120000.00"`
	LossesBroughtForward string `json:"losses_brought_forward" doc:"Losses set off first." example:"0.00"`
	Chargeable           string `json:"chargeable_income" doc:"Profit less losses, at least 0." example:"120000.00"`
	Exempt               string `json:"exempt" doc:"The exempt amount of the scheme, to the cent." example:"85000.00"`
	GrossTax             string `json:"gross_tax" doc:"The rate of sg-cit on the rest, to the cent." example:"5950.00"`
	Rebate               string `json:"rebate" doc:"The YA's rebate from sg-cit, to the cent; 0 when the YA has none." example:"0.00"`
	Tax                  string `json:"tax" doc:"The year's tax, rounded once (RND-PLAN)." example:"5950.00"`
	LossesCarriedForward string `json:"losses_carried_forward" doc:"Losses left for the next year." example:"0.00"`
}

// ForecastYear sums or ends a forecast year; a figure past the horizon is absent.
type ForecastYear struct {
	Year        int    `json:"year" doc:"The forecast year." example:"1"`
	Revenue     string `json:"revenue,omitempty" doc:"Its revenue." example:"575399.18"`
	GrossProfit string `json:"gross_profit,omitempty" doc:"Its gross profit." example:"491233.20"`
	EBITDA      string `json:"ebitda,omitempty" doc:"Its EBITDA." example:"-98213.41"`
	NetProfit   string `json:"net_profit,omitempty" doc:"Its net profit." example:"-105612.08"`
	NetBurn     string `json:"net_burn,omitempty" doc:"Its net burn." example:"218252.20"`
	Cash        string `json:"cash,omitempty" doc:"Cash at its end; absent when it ends past the horizon." example:"2756660.90"`
	Customers   string `json:"customers,omitempty" doc:"Customers at its end, to 1 decimal." example:"168.4"`
	ARR         string `json:"arr,omitempty" doc:"ARR at its end." example:"254649.60"`
	Headcount   *int   `json:"headcount,omitempty" doc:"Headcount at its end." example:"9"`
}

// ForecastKey are a run's key results (FR-PLN-03).
type ForecastKey struct {
	CashOutMonth  string `json:"cash_out_month,omitempty" doc:"The first month cash closes below zero (YYYY-MM); absent: not within the horizon." example:"2028-03"`
	RunwayMonths  int    `json:"runway_months" doc:"The months before the cash-out month; the horizon when there is none, with runway_beyond." example:"16"`
	RunwayBeyond  bool   `json:"runway_beyond" doc:"true: no cash-out month within the horizon, so runway is more than runway_months." example:"false"`
	MinimumCash   string `json:"minimum_cash" doc:"The lowest month-end cash." example:"-91351.81"`
	MinimumMonth  string `json:"minimum_month" doc:"Its month (YYYY-MM)." example:"2026-11"`
	BreakEven     string `json:"break_even,omitempty" doc:"The first month with revenue whose EBITDA is not negative (YYYY-MM); absent: not within the forecast." example:"2029-05"`
	AverageBurn   string `json:"average_burn" doc:"The average net burn of the first 12 months." example:"18187.69"`
	FundingGap    string `json:"funding_gap" doc:"The buffer × the average burn, less the minimum cash, at least 0." example:"0.00"`
	BufferMonths  int    `json:"buffer_months" doc:"The buffer the gap used: the scenario's, the forecast's or planning.buffer_months." example:"6"`
	RevenueNext12 string `json:"revenue_next_12" doc:"The revenue of the next 12 months." example:"575399.18"`
	ARREnd        string `json:"arr_end" doc:"ARR at the horizon." example:"987252.12"`
	CustomersEnd  string `json:"customers_end" doc:"Customers at the horizon, to 1 decimal." example:"621.9"`
}

// ForecastRun is one scenario, with or without its funding plan.
type ForecastRun struct {
	Scenario      string            `json:"scenario" doc:"The scenario." example:"Base"`
	WithFunding   bool              `json:"with_funding" doc:"true: with the funding plan; false: without it, as Home, the tiles and the alerts read runway, since none of it is received yet (FR-PLN-03)." example:"true"`
	CollectionsOK bool              `json:"collections_ok" doc:"false: the collection pattern does not total 100%, so revenue is not all collected (⚠)." example:"true"`
	Key           ForecastKey       `json:"key_results" doc:"The key results."`
	Years         []ForecastYear    `json:"years" nullable:"false" doc:"The yearly summary, five forecast years."`
	Taxes         []ForecastTaxYear `json:"tax_years" nullable:"false" doc:"The tax estimate by forecast year."`
	Months        []ForecastMonth   `json:"months,omitempty" doc:"Every month from 1 to the horizon; left out of a comparison."`
}

// ForecastResults are a forecast worked out today.
type ForecastResults struct {
	ForecastID string          `json:"forecast_id,omitempty" format:"uuid" doc:"The forecast; absent for a plan run without saving it." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9c01"`
	Currency   string          `json:"currency" doc:"The base currency every amount is in." example:"SGD"`
	On         string          `json:"on" format:"date" doc:"The day it was worked out: today." example:"2026-11-20"`
	Books      ForecastBooks   `json:"books" doc:"The books' figures at the start."`
	Packs      []PackVersionID `json:"packs" nullable:"false" doc:"The statutory pack versions it read."`
	Runs       []ForecastRun   `json:"runs" nullable:"false" doc:"Each scenario asked for, with and then without its funding plan."`
	Sandbox    string          `json:"sandbox" doc:"What a forecast never does." example:"A forecast never posts: nothing here changes the books (FR-PLN-08)."`
}

// ExpenseLineAmount is one expense line's amount in a month.
type ExpenseLineAmount string

// Schema implements huma.SchemaProvider.
func (ExpenseLineAmount) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "An other expense line's amount in the month, in the plan's order.", Examples: []any{"2500.00"}}
}

// PackVersionID is a statutory pack version, <pack>@<year>.<n>.
type PackVersionID string

// Schema implements huma.SchemaProvider.
func (PackVersionID) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: "string", Description: "A statutory pack version, <pack>@<year>.<n> (ENG-30).", Examples: []any{"sg-cpf@2026.1"}}
}

const sandboxNote = "A forecast never posts: nothing here changes the books (FR-PLN-08)."

type (
	ForecastPath struct {
		ForecastID string `path:"forecast_id" format:"uuid" doc:"The forecast." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9c01"`
	}
	forecastListInput  struct{ PageQuery }
	forecastListOutput struct{ Body ForecastPage }
	forecastDraftInput struct {
		KeyHeader
		PreferHeader
		Body forecasts.ForecastPlan
	}
	forecastReviseInput struct {
		ForecastPath
		KeyHeader
		IfMatchHeader
		PreferHeader
		Body forecasts.ForecastPlan
	}
	forecastRecordInput struct {
		ForecastPath
		KeyHeader
		IfMatchHeader
		PreferHeader
	}
	forecastReadOutput struct {
		ETag string `header:"ETag" doc:"Its version and status, for If-Match."`
		Body ForecastView
	}
	forecastWriteOutput struct {
		Status   int
		ETag     string `header:"ETag" doc:"Its version and status, for If-Match."`
		Location string `header:"Location" doc:"Its URL."`
		Replayed string `header:"Idempotent-Replayed" doc:"true when this is the saved answer to an earlier request with the same Idempotency-Key."`
		Applied  string `header:"Preference-Applied" doc:"return=preview when this is a preview: nothing was saved (FR-INT-14)."`
		Body     ForecastView
	}
	forecastResultsInput struct {
		ForecastPath
		Scenario string `query:"scenario" maxLength:"40" doc:"One scenario by name; left out, every scenario." example:"Base"`
	}
	forecastRunInput struct {
		Scenario string `query:"scenario" maxLength:"40" doc:"One scenario by name; left out, every scenario." example:"Base"`
		Body     forecasts.ForecastPlan
	}
	forecastResultsOutput struct{ Body ForecastResults }
)

func t2(x *apd.Decimal, cur money.Currency) string {
	if x == nil {
		return ""
	}
	return rounding.Plan(x, cur).Text('f')
}

func ratioText(f *planning.Fraction, places int) (string, error) {
	if f == nil {
		return "", nil
	}
	x, err := f.Round(places)
	if err != nil {
		return "", err
	}
	return x.Text('f'), nil
}

func ym(t time.Time) string { return t.Format("2006-01") }

// forecastResultsOf is a worked-out forecast as the API shows it; withMonths false leaves the months out.
func forecastResultsOf(r forecasts.Results, cur money.Currency, on time.Time, withMonths bool) (ForecastResults, error) {
	out := ForecastResults{Currency: cur.Code, On: on.Format(time.DateOnly), Packs: []PackVersionID{}, Runs: []ForecastRun{}, Sandbox: sandboxNote}
	for _, id := range r.Packs {
		out.Packs = append(out.Packs, PackVersionID(id))
	}
	b := r.Books
	out.Books = ForecastBooks{LastActual: ym(b.LastActual), Start: ym(b.Start), Cash: t2(b.Figures.Cash, cur),
		Receivables: t2(b.Figures.Receivables, cur), Payables: t2(b.Figures.Payables, cur), Hints: []ForecastHint{}, Revision: b.Revision}
	for g, h := range b.Hints {
		out.Books.Hints = append(out.Books.Hints, ForecastHint{ReportGroup: g, Average: h.String()})
	}
	sortHints(out.Books.Hints)
	amount := func(x *apd.Decimal) string { return t2(x, cur) }
	for _, run := range r.Runs {
		res := run.Result
		k := res.Key
		fr := ForecastRun{Scenario: run.Scenario, WithFunding: run.WithFunding, CollectionsOK: res.CollectionsOK,
			Years: []ForecastYear{}, Taxes: []ForecastTaxYear{}}
		fr.Key = ForecastKey{RunwayMonths: k.RunwayMonths, RunwayBeyond: k.RunwayBeyond, MinimumCash: amount(k.MinimumCash),
			MinimumMonth: ym(k.MinimumMonth), AverageBurn: amount(k.AverageBurn), FundingGap: amount(k.FundingGap),
			RevenueNext12: amount(k.RevenueNext12), ARREnd: amount(k.ARREnd), CustomersEnd: rounding.PlanCount(k.CustomersEnd)}
		if res.Buffer != nil {
			fr.Key.BufferMonths = int(res.Buffer.Coeff.Int64())
		}
		if k.CashOutMonth != nil {
			fr.Key.CashOutMonth = ym(*k.CashOutMonth)
		}
		if k.BreakEven != nil {
			fr.Key.BreakEven = ym(*k.BreakEven)
		}
		for _, y := range res.Years {
			fy := ForecastYear{Year: y.Year, Revenue: amount(y.Revenue), GrossProfit: amount(y.GrossProfit), EBITDA: amount(y.EBITDA),
				NetProfit: amount(y.NetProfit), NetBurn: amount(y.NetBurn), Cash: amount(y.Cash), ARR: amount(y.ARR), Headcount: y.Headcount}
			if y.Customers != nil {
				fy.Customers = rounding.PlanCount(y.Customers)
			}
			fr.Years = append(fr.Years, fy)
		}
		for _, x := range res.Taxes {
			fr.Taxes = append(fr.Taxes, ForecastTaxYear{Year: x.Year, YA: x.YA, Scheme: x.Scheme, ProfitBeforeTax: amount(x.ProfitBeforeTax),
				LossesBroughtForward: amount(x.LossesBroughtForward), Chargeable: amount(x.Chargeable), Exempt: amount(x.Exempt),
				GrossTax: amount(x.GrossTax), Rebate: amount(x.Rebate), Tax: amount(x.Tax), LossesCarriedForward: amount(x.LossesCarriedForward)})
		}
		if withMonths {
			for _, m := range res.Months {
				fm := ForecastMonth{Month: ym(m.Date), Year: m.Year, CustomersOpen: rounding.PlanCount(m.CustomersOpen),
					NewCustomers: rounding.PlanCount(m.NewCustomers), LostCustomers: rounding.PlanCount(m.LostCustomers),
					Customers: rounding.PlanCount(m.CustomersClose), ARPA: amount(m.ARPA), Subscription: amount(m.Subscription),
					Services: amount(m.Services), Other: amount(m.Other), Revenue: amount(m.Revenue), Hosting: amount(m.Hosting), AI: amount(m.AI),
					Contractors: amount(m.Contractors), Fees: amount(m.Fees), PayrollCOGS: amount(m.PayrollCOGS), DirectCosts: amount(m.DirectCosts),
					GrossProfit: amount(m.GrossProfit), PayrollRD: amount(m.PayrollRD), PayrollSM: amount(m.PayrollSM), PayrollGA: amount(m.PayrollGA),
					Payroll: amount(m.Payroll), Headcount: m.Headcount, Opex: []ExpenseLineAmount{}, OpexTotal: amount(m.OpexTotal), EBITDA: amount(m.EBITDA),
					Depreciation: amount(m.Depreciation), Grants: amount(m.Grants), Interest: amount(m.Interest),
					ProfitBeforeTax: amount(m.ProfitBeforeTax), Tax: amount(m.Tax), NetProfit: amount(m.NetProfit), Collections: amount(m.Collections),
					Suppliers: amount(m.Suppliers), PayrollPaid: amount(m.PayrollPaid), GrantsReceived: amount(m.GrantsReceived),
					InterestPaid: amount(m.InterestPaid), TaxPaid: amount(m.TaxPaid), Operating: amount(m.Operating), Capex: amount(m.CapexPaid),
					Equity: amount(m.Equity), Loans: amount(m.LoansDrawn), Repayments: amount(m.LoanRepayments), NetCashFlow: amount(m.NetCashFlow),
					CashOpen: amount(m.CashOpen), Cash: amount(m.CashClose), NetBurn: amount(m.NetBurn), ARR: amount(m.ARR),
					SalesMarketing: amount(m.SalesMarketing)}
				for _, o := range m.Opex {
					fm.Opex = append(fm.Opex, ExpenseLineAmount(amount(o)))
				}
				var err error
				if m.GrossMargin != nil {
					pct, err := m.GrossMargin.Percent()
					if err != nil {
						return out, err
					}
					fm.GrossMargin = pct.Text('f')
				}
				if fm.CAC, err = ratioText(m.CAC, cur.MinorUnits); err != nil {
					return out, err
				}
				if fm.LTV, err = ratioText(m.LTV, cur.MinorUnits); err != nil {
					return out, err
				}
				if fm.LTVToCAC, err = ratioText(m.LTVToCAC, 2); err != nil {
					return out, err
				}
				if fm.BurnMultiple, err = ratioText(m.BurnMultiple, 2); err != nil {
					return out, err
				}
				fr.Months = append(fr.Months, fm)
			}
		}
		out.Runs = append(out.Runs, fr)
	}
	return out, nil
}

func sortHints(h []ForecastHint) {
	for i := 1; i < len(h); i++ {
		for j := i; j > 0 && h[j].ReportGroup < h[j-1].ReportGroup; j-- {
			h[j], h[j-1] = h[j-1], h[j]
		}
	}
}

// planForecastOf is a forecast document as the API shows it.
func (a *API) planForecastOf(ctx context.Context, u storage.UnitOfWork, doc storage.Document, latest uuid.UUID) (ForecastView, error) {
	p, err := forecasts.Read(doc)
	if err != nil {
		return ForecastView{}, err
	}
	out := ForecastView{ID: doc.ID.String(), Status: "draft", Version: doc.Version, Plan: p}
	if doc.Status == storage.DocumentPosted {
		out.Status = "superseded"
		if doc.ID == latest {
			out.Status = "recorded"
		}
		out.RecordedBy, out.RecordedAt = personOf(doc.PostedBy), doc.PostedAt
		if err := a.name(ctx, u, out.RecordedBy); err != nil {
			return out, err
		}
	}
	return out, nil
}

func latestOf(docs []storage.Document) uuid.UUID {
	if len(docs) == 0 {
		return uuid.Nil
	}
	return docs[0].ID
}

// shownPlanForecast is a forecast by ID: not found for any other document.
func (a *API) shownPlanForecast(ctx context.Context, u storage.UnitOfWork, id uuid.UUID) (ForecastView, error) {
	doc, err := u.Documents().Get(ctx, id)
	if err != nil {
		return ForecastView{}, err
	}
	if doc.Type != storage.DocumentForecast {
		return ForecastView{}, ports.ErrNotFound
	}
	recorded, err := forecasts.Recorded(ctx, u)
	if err != nil {
		return ForecastView{}, err
	}
	return a.planForecastOf(ctx, u, doc, latestOf(recorded))
}

func forecastStatus(v ForecastView) string {
	if v.Status == "draft" {
		return "draft"
	}
	return "posted"
}

func forecastWriteAnswer(w written[ForecastView]) *forecastWriteOutput {
	if w.Previewed {
		return &forecastWriteOutput{Status: w.Status, Applied: applied(w), Body: w.Body}
	}
	return &forecastWriteOutput{Status: w.Status, ETag: etag(w.Body.Version, forecastStatus(w.Body)), Location: "/v1/forecasts/" + w.Body.ID,
		Replayed: replayedHeader(w.Replayed), Body: w.Body}
}

// planIn is a plan as the request sent it, with its scenarios defaulting to Base, Best and Worst.
func planIn(p forecasts.ForecastPlan) forecasts.ForecastPlan {
	if len(p.Scenarios) == 0 {
		p.Scenarios = forecasts.DefaultScenarios()
	}
	return p
}

func registerForecasts(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	writes := append([]string{CodeIdempotencyReused, CodeIdempotencyBusy, CodeIdempotencyOld, CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, locks...)

	register(a, authz.ListForecasts, huma.Operation{
		OperationID: "listForecasts", Method: http.MethodGet, Path: "/v1/forecasts", Tags: []string{"Planning"},
		Summary:     "List the forecasts",
		Description: "Every forecast: the latest recorded first, the company's own, then the others recorded, then the drafts, a page at a time (FR-PLN-02). Read-only; no side effects.",
	}, append([]string{CodeCursor}, locks...), func(ctx context.Context, in *forecastListInput) (*forecastListOutput, error) {
		p, _ := authn.From(ctx)
		out := forecastListOutput{Body: ForecastPage{Items: []ForecastView{}}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			recorded, err := forecasts.Recorded(ctx, u)
			if err != nil {
				return err
			}
			latest := latestOf(recorded)
			var all []ForecastView
			drafts, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentForecast, Status: storage.DocumentDraft, Limit: 200})
			if err != nil {
				return err
			}
			for _, doc := range append(recorded, drafts...) {
				v, err := a.planForecastOf(ctx, u, doc, latest)
				if err != nil {
					return err
				}
				all = append(all, v)
			}
			out.Body.Items, out.Body.NextCursor, err = pageOf(all, func(x ForecastView) string { return x.ID }, in.PageQuery)
			if out.Body.Items == nil {
				out.Body.Items = []ForecastView{}
			}
			return err
		})
		return &out, err
	})

	register(a, authz.CreateForecast, huma.Operation{
		OperationID: "createForecast", Method: http.MethodPost, Path: "/v1/forecasts", Tags: []string{"Planning"},
		DefaultStatus: http.StatusCreated, Summary: "Draft a forecast",
		Description: "Drafts a driver-based forecast (FR-PLN-02, design D5): the workbook's model, worked in exact decimals (RND-PLAN). " +
			"Without scenarios it gets Base, Best and Worst with the workbook's levers. A draft is a sandbox: change it, read its results, " +
			"and record it once it is the company's plan. It never posts (FR-PLN-08). Side effects: the draft, audit events. Every " +
			"problem comes back together. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{}, writes...), ForecastCodes...), func(ctx context.Context, in *forecastDraftInput) (*forecastWriteOutput, error) {
		w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in.Body, http.StatusCreated, func(ctx context.Context, u storage.UnitOfWork) (ForecastView, error) {
			base, err := assetBase(ctx, u)
			if err != nil {
				return ForecastView{}, err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return ForecastView{}, err
			}
			doc, err := forecasts.DraftIn(ctx, u, planIn(in.Body), base, today)
			if err != nil {
				return ForecastView{}, equityProblems(err)
			}
			return a.shownPlanForecast(ctx, u, doc.ID)
		})
		if err != nil {
			return nil, err
		}
		return forecastWriteAnswer(w), nil
	})

	register(a, authz.GetForecast, huma.Operation{
		OperationID: "getForecast", Method: http.MethodGet, Path: "/v1/forecasts/{forecast_id}", Tags: []string{"Planning"},
		Summary:     "Get a forecast",
		Description: "A forecast's plan, drafted or recorded, with its status and who recorded it (FR-PLN-02). Its figures are getForecastResults'. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *ForecastPath) (*forecastReadOutput, error) {
		id, err := parseID("forecast_id", in.ForecastID)
		if err != nil {
			return nil, err
		}
		p, _ := authn.From(ctx)
		var out forecastReadOutput
		err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			v, err := a.shownPlanForecast(ctx, u, id)
			out.Body, out.ETag = v, etag(v.Version, forecastStatus(v))
			return err
		})
		return &out, err
	})

	register(a, authz.ReplaceForecast, huma.Operation{
		OperationID: "replaceForecast", Method: http.MethodPut, Path: "/v1/forecasts/{forecast_id}", Tags: []string{"Planning"},
		Summary: "Change a drafted forecast",
		Description: "Saves a new version of a forecast not recorded yet (FR-PLN-02); a recorded one changes no more (PLAN-11): draft a new one " +
			"instead. Side effects: the new version, audit events. Needs If-Match with its ETag. Idempotent with Idempotency-Key; " +
			"Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), ForecastCodes...),
		func(ctx context.Context, in *forecastReviseInput) (*forecastWriteOutput, error) {
			id, err := parseID("forecast_id", in.ForecastID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ForecastView, error) {
				doc, err := forecasts.Drafted(ctx, u, id)
				if err != nil {
					return ForecastView{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return ForecastView{}, err
				}
				base, err := assetBase(ctx, u)
				if err != nil {
					return ForecastView{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return ForecastView{}, err
				}
				if _, err := forecasts.ReviseIn(ctx, u, id, planIn(in.Body), base, today); err != nil {
					return ForecastView{}, equityProblems(err)
				}
				return a.shownPlanForecast(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return forecastWriteAnswer(w), nil
		})

	register(a, authz.RecordForecast, huma.Operation{
		OperationID: "recordForecast", Method: http.MethodPost, Path: "/v1/forecasts/{forecast_id}/record", Tags: []string{"Planning"},
		Summary: "Record a forecast",
		Description: "Records a drafted forecast as the company's plan (FR-PLN-02, design D5): from then on the latest recorded is Home's, " +
			"never changed. It posts nothing (FR-PLN-08) and needs no approval. Side effects: the forecast recorded, audit events. Needs " +
			"If-Match with its ETag. Idempotent with Idempotency-Key; Prefer: return=preview saves nothing.",
	}, append(append([]string{CodeNotFound, CodeStale, CodeIfMatchRequired}, writes...), ForecastCodes...),
		func(ctx context.Context, in *forecastRecordInput) (*forecastWriteOutput, error) {
			id, err := parseID("forecast_id", in.ForecastID)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(in.IfMatch) == "" {
				return nil, checkIfMatch(in.IfMatch, "")
			}
			w, err := write(ctx, a, in.IdempotencyKey, in.Prefer, in, http.StatusOK, func(ctx context.Context, u storage.UnitOfWork) (ForecastView, error) {
				doc, err := forecasts.Drafted(ctx, u, id)
				if err != nil {
					return ForecastView{}, err
				}
				if err := checkIfMatch(in.IfMatch, recordETag(doc)); err != nil {
					return ForecastView{}, err
				}
				base, err := assetBase(ctx, u)
				if err != nil {
					return ForecastView{}, err
				}
				today, err := orgToday(ctx, a, u)
				if err != nil {
					return ForecastView{}, err
				}
				if _, err := forecasts.RecordIn(ctx, u, id, base, today); err != nil {
					return ForecastView{}, equityProblems(err)
				}
				return a.shownPlanForecast(ctx, u, id)
			})
			if err != nil {
				return nil, err
			}
			return forecastWriteAnswer(w), nil
		})

	results := func(ctx context.Context, u storage.UnitOfWork, p forecasts.ForecastPlan, only string, months bool) (ForecastResults, error) {
		base, err := assetBase(ctx, u)
		if err != nil {
			return ForecastResults{}, err
		}
		today, err := orgToday(ctx, a, u)
		if err != nil {
			return ForecastResults{}, err
		}
		r, err := forecasts.Work(ctx, u, p, base, today, only)
		if err != nil {
			return ForecastResults{}, err
		}
		return forecastResultsOf(r, base, today, months)
	}

	register(a, authz.GetForecastResults, huma.Operation{
		OperationID: "getForecastResults", Method: http.MethodGet, Path: "/v1/forecasts/{forecast_id}/results", Tags: []string{"Planning"},
		Summary: "Work a forecast out",
		Description: "A forecast's figures today (FR-PLN-02, FR-PLN-03): from the books at its last actual month and the sg-cit, sg-cpf and " +
			"sg-sdl packs, each scenario with and then without its funding plan, month by month, with the yearly summary, the tax " +
			"estimate and the key results. Every figure is RND-PLAN's: a driver line rounded once, a total the sum of the lines shown. " +
			"Worked out on every read, never stored: a closed month or a new pack moves it. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, append(locks, ForecastCodes...)...), func(ctx context.Context, in *forecastResultsInput) (*forecastResultsOutput, error) {
		id, err := parseID("forecast_id", in.ForecastID)
		if err != nil {
			return nil, err
		}
		pr, _ := authn.From(ctx)
		var out forecastResultsOutput
		err = a.deps.Store.Do(ctx, pr.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			if doc.Type != storage.DocumentForecast {
				return ports.ErrNotFound
			}
			p, err := forecasts.Read(doc)
			if err != nil {
				return err
			}
			out.Body, err = results(ctx, u, p, in.Scenario, true)
			out.Body.ForecastID = id.String()
			return err
		})
		return &out, err
	})

	register(a, authz.CompareForecastScenarios, huma.Operation{
		OperationID: "compareForecastScenarios", Method: http.MethodGet, Path: "/v1/forecasts/{forecast_id}/comparison", Tags: []string{"Planning"},
		Summary: "Compare a forecast's scenarios",
		Description: "Every scenario of a forecast side by side (FR-PLN-03): each with and without its funding plan, its key results, yearly " +
			"summary and tax estimate, without the months. Read-only; no side effects.",
	}, append([]string{CodeNotFound}, append(locks, ForecastCodes...)...), func(ctx context.Context, in *ForecastPath) (*forecastResultsOutput, error) {
		id, err := parseID("forecast_id", in.ForecastID)
		if err != nil {
			return nil, err
		}
		pr, _ := authn.From(ctx)
		var out forecastResultsOutput
		err = a.deps.Store.Do(ctx, pr.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			doc, err := u.Documents().Get(ctx, id)
			if err != nil {
				return err
			}
			if doc.Type != storage.DocumentForecast {
				return ports.ErrNotFound
			}
			p, err := forecasts.Read(doc)
			if err != nil {
				return err
			}
			out.Body, err = results(ctx, u, p, "", false)
			out.Body.ForecastID = id.String()
			return err
		})
		return &out, err
	})

	// A run saves nothing, so a repeat is harmless and return=preview would answer the same.
	register(a, authz.RunForecast, exempt(exempt(huma.Operation{
		OperationID: "runForecast", Method: http.MethodPost, Path: "/v1/forecasts:run", Tags: []string{"Planning"},
		Summary: "Work a plan out without saving it",
		Description: "Works a plan out as getForecastResults does, from the books and the packs, without saving anything (FR-PLN-02, " +
			"FR-PLN-08): a sandbox for a what-if. Every problem of the plan comes back together. Saves nothing; no side effects.",
	}, "Idempotency-Key", "A forecast run saves nothing, so a repeat is harmless and answers the same (FR-PLN-08)."),
		"Prefer", "A forecast run saves nothing: return=preview would answer the same as the request itself (FR-INT-14)."), append([]string{CodeMalformed, CodeTooLarge, CodeMediaType, CodeInvalid}, append(locks, ForecastCodes...)...),
		func(ctx context.Context, in *forecastRunInput) (*forecastResultsOutput, error) {
			pr, _ := authn.From(ctx)
			var out forecastResultsOutput
			err := a.deps.Store.Do(ctx, pr.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
				base, err := assetBase(ctx, u)
				if err != nil {
					return err
				}
				p := planIn(in.Body)
				if probs := forecasts.Check(p, base); probs != nil {
					return equityProblems(probs)
				}
				out.Body, err = results(ctx, u, p, in.Scenario, true)
				return err
			})
			return &out, err
		})
}
