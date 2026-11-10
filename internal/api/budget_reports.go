package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/budget"
	"github.com/cognizhi/fyniq/internal/domain/forecasts"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ Budget vs Actual (FR-RPT-03) and the planning summary (FR-KPI-01)

// VsFigures are an actual against its budget.
type VsFigures struct {
	Actual   BudgetAmount `json:"actual" doc:"The books' figure, signed as the P&L shows it."`
	Budget   BudgetAmount `json:"budget" doc:"The budget's figure for the same months."`
	Variance BudgetAmount `json:"variance" doc:"Actual less budget."`
	Percent  string       `json:"variance_percent,omitempty" doc:"The variance as a percentage of the budget's size (RND-PERCENT); absent when the budget is zero." example:"-12.5"`
}

// VsRowView is a row of Budget vs Actual.
type VsRowView struct {
	AccountID  string             `json:"account_id" format:"uuid" doc:"The account." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c4b20"`
	Code       string             `json:"code" doc:"Its code." example:"6110"`
	Name       string             `json:"name" doc:"Its name." example:"Advertising"`
	Dimensions []DimensionValueID `json:"dimension_value_ids,omitempty" doc:"by=dimension: the row's dimension values; none for the lines with no value."`
	Income     bool               `json:"income" doc:"true for an income account, false for an expense account." example:"false"`
	Favourable bool               `json:"favourable" doc:"Income at or above its budget, or costs at or below theirs." example:"true"`
	VsFigures
}

// BudgetVsActual is the report.
type BudgetVsActual struct {
	Currency   string      `json:"currency" doc:"The base currency." example:"SGD"`
	YearStart  string      `json:"financial_year" format:"date" doc:"The financial year's first day." example:"2027-01-01"`
	From       string      `json:"from" doc:"The first month (YYYY-MM)." example:"2027-01"`
	To         string      `json:"to" doc:"The last month (YYYY-MM)." example:"2027-03"`
	By         string      `json:"by" enum:"account,dimension" doc:"account: each account, its dimension sets added up; dimension: each account and set of dimension values." example:"account"`
	BudgetID   string      `json:"budget_id,omitempty" format:"uuid" doc:"The year's budget, its latest recorded version; absent while the year has none, when every budget figure is 0." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9d01"`
	BudgetName string      `json:"budget_name,omitempty" doc:"Its name." example:"FY2027 budget"`
	Rows       []VsRowView `json:"rows" nullable:"false" doc:"Each account, or account and set, with an actual or a budget, by code."`
	Income     VsFigures   `json:"income" doc:"The income rows' totals."`
	Costs      VsFigures   `json:"costs" doc:"The expense rows' totals."`
	Net        VsFigures   `json:"net" doc:"Income less costs."`
	Revision   int64       `json:"ledger_revision" doc:"The ledger revision the actuals were read at (ENG-46)." example:"42"`
}

// PlanningSummary is what Home shows of the budget and the forecast for a month.
type PlanningSummary struct {
	Month    string                `json:"month" doc:"The month asked for (YYYY-MM)." example:"2027-03"`
	Currency string                `json:"currency" doc:"The base currency." example:"SGD"`
	Budget   *PlanningBudgetTile   `json:"budget,omitempty" doc:"Revenue and costs against budget for the financial year to the month's end; absent while the year has no approved budget, which budget_missing says."`
	Missing  string                `json:"budget_missing,omitempty" doc:"Why there is no budget figure." example:"No approved budget for the year from 1 Jan 2027"`
	Forecast *PlanningForecastTile `json:"forecast,omitempty" doc:"The cash-out month of the latest recorded forecast; absent while none is recorded, which forecast_missing says."`
	NoPlan   string                `json:"forecast_missing,omitempty" doc:"Why there is no forecast figure." example:"No forecast is recorded yet"`
}

// PlanningBudgetTile is the year to date against the budget.
type PlanningBudgetTile struct {
	BudgetID string    `json:"budget_id" format:"uuid" doc:"The year's budget." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9d01"`
	Name     string    `json:"name" doc:"Its name." example:"FY2027 budget"`
	From     string    `json:"from" doc:"The financial year's first month (YYYY-MM)." example:"2027-01"`
	Revenue  VsFigures `json:"revenue" doc:"Income against its budget, the year to date."`
	Costs    VsFigures `json:"costs" doc:"Costs against their budget, the year to date."`
}

// PlanningForecastTile is the latest recorded forecast's first scenario: its cash-out month without the funding
// planned, as Home, the tiles and the alerts read it (FR-PLN-03), and with it.
type PlanningForecastTile struct {
	ForecastID     string `json:"forecast_id" format:"uuid" doc:"The forecast." example:"01928f6e-7a3b-7c41-9d2e-5f8a1b3c9c01"`
	Name           string `json:"name" doc:"Its name." example:"FY2027 plan"`
	Scenario       string `json:"scenario" doc:"The scenario read: its first." example:"Base"`
	CashOut        string `json:"cash_out_month,omitempty" doc:"Without the funding planned: the first month cash closes below zero (YYYY-MM); absent when not within the horizon." example:"2027-07"`
	Runway         int    `json:"runway_months" doc:"Without the funding planned: the months before it, or the horizon." example:"7"`
	Beyond         bool   `json:"runway_beyond" doc:"true: no cash-out month within the horizon." example:"false"`
	CashOutFunded  string `json:"cash_out_month_with_funding,omitempty" doc:"The same with the funding planned." example:"2028-02"`
	BeyondFunded   bool   `json:"runway_beyond_with_funding" doc:"With the funding planned: no cash-out month within the horizon." example:"true"`
	WithoutFunding string `json:"note" doc:"What the figure leaves out." example:"Without the funding planned: none of it is received yet"`
}

type (
	vsInput struct {
		From string `query:"from" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The first month (YYYY-MM); left out, the financial year's first month." example:"2027-01"`
		To   string `query:"to" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The last month (YYYY-MM); left out, the last month that has ended." example:"2027-03"`
		By   string `query:"by" enum:"account,dimension" doc:"account (the default) or dimension." example:"account"`
	}
	vsOutput     struct{ Body BudgetVsActual }
	summaryInput struct {
		Month string `query:"month" pattern:"^[0-9]{4}-(0[1-9]|1[0-2])$" doc:"The month (YYYY-MM); left out, the last month that has ended." example:"2027-03"`
	}
	summaryOutput struct{ Body PlanningSummary }
)

func vsOf(f budget.Figures) VsFigures {
	return VsFigures{Actual: BudgetAmount(f.Actual.String()), Budget: BudgetAmount(f.Budget.String()), Variance: BudgetAmount(f.Variance.String()), Percent: f.Percent}
}

// monthOr reads a YYYY-MM, or the last month that has ended by today.
func monthOr(s string, today time.Time) time.Time {
	if s != "" {
		if m, err := time.ParseInLocation("2006-01", s, time.UTC); err == nil {
			return m
		}
	}
	return time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0)
}

func registerBudgetReports(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}

	register(a, authz.GetBudgetVsActual, huma.Operation{
		OperationID: "getBudgetVsActual", Method: http.MethodGet, Path: "/v1/reports/budget-vs-actual", Tags: []string{"Reports"},
		Summary: "Get Budget vs Actual",
		Description: "The year's budget against the books for months of one financial year (FR-RPT-03, design D6): each income and expense " +
			"account, or account and set of dimension values, with its actual from the balance service, its budget from the year's " +
			"latest recorded version, the variance and its percentage of the budget (RND-PERCENT), and whether it is favourable. " +
			"Amounts are signed as the P&L shows them. Read-only; no side effects.",
	}, append([]string{CodeInvalid, budget.CodeBudget}, locks...), func(ctx context.Context, in *vsInput) (*vsOutput, error) {
		p, _ := authn.From(ctx)
		var out vsOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			cal, err := period.CalendarOf(o.Profile)
			if err != nil {
				return err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			to := monthOr(in.To, today)
			from := cal.Year(max(cal.Number(to), 1)).StartsOn
			if in.From != "" {
				from = monthOr(in.From, today)
			}
			by := in.By
			if by == "" {
				by = budget.ByAccount
			}
			r, err := budget.Report(ctx, u, base, cal, from, to, by)
			if err != nil {
				return err
			}
			rev, err := u.Ledger().Revision(ctx)
			if err != nil {
				return err
			}
			out.Body = BudgetVsActual{Currency: base.Code, YearStart: r.Year.StartsOn.Format(time.DateOnly), From: r.From.Format("2006-01"),
				To: r.To.Format("2006-01"), By: by, BudgetName: r.Name, Rows: []VsRowView{}, Income: vsOf(r.Income), Costs: vsOf(r.Costs),
				Net: vsOf(r.Net), Revision: rev}
			if r.Budget != nil {
				out.Body.BudgetID = r.Budget.ID.String()
			}
			for _, row := range r.Rows {
				v := VsRowView{AccountID: row.Account.ID.String(), Code: row.Account.Code, Name: row.Account.Name, Income: row.Income,
					Favourable: row.Favourable, VsFigures: vsOf(row.Figures)}
				for _, id := range row.Values {
					v.Dimensions = append(v.Dimensions, DimensionValueID(id.String()))
				}
				out.Body.Rows = append(out.Body.Rows, v)
			}
			return nil
		})
		return &out, err
	})

	register(a, authz.GetPlanningSummary, huma.Operation{
		OperationID: "getPlanningSummary", Method: http.MethodGet, Path: "/v1/planning/summary", Tags: []string{"Planning"},
		Summary: "Get the planning summary for Home",
		Description: "What Home shows of the plans for a month (FR-KPI-01): revenue and costs against the year's budget to the month's end, " +
			"and the cash-out month and runway of the latest recorded forecast's first scenario without the funding planned, since " +
			"none of it is received yet (FR-PLN-03), with the figures with it beside. Each is absent, with why, while there is no " +
			"budget or forecast. Read-only; no side effects.",
	}, append([]string{CodeInvalid}, append(locks, ForecastCodes...)...), func(ctx context.Context, in *summaryInput) (*summaryOutput, error) {
		p, _ := authn.From(ctx)
		var out summaryOutput
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			o, err := u.Organisation(ctx)
			if err != nil {
				return err
			}
			base, err := money.Lookup(o.BaseCurrency)
			if err != nil {
				return err
			}
			today, err := orgToday(ctx, a, u)
			if err != nil {
				return err
			}
			month := monthOr(in.Month, today)
			out.Body = PlanningSummary{Month: month.Format("2006-01"), Currency: base.Code}
			if cal, err := period.CalendarOf(o.Profile); err != nil {
				out.Body.Missing = "The financial year is not set"
			} else {
				y := cal.Year(max(cal.Number(month), 1))
				r, err := budget.Report(ctx, u, base, cal, y.StartsOn, month, budget.ByAccount)
				switch {
				case err != nil:
					return err
				case r.Budget == nil:
					out.Body.Missing = "No approved budget for the year from " + y.StartsOn.Format("2 Jan 2006")
				default:
					out.Body.Budget = &PlanningBudgetTile{BudgetID: r.Budget.ID.String(), Name: r.Name, From: y.StartsOn.Format("2006-01"),
						Revenue: vsOf(r.Income), Costs: vsOf(r.Costs)}
				}
			}
			recorded, err := forecasts.Recorded(ctx, u)
			if err != nil {
				return err
			}
			if len(recorded) == 0 {
				out.Body.NoPlan = "No forecast is recorded yet"
				return nil
			}
			plan, err := forecasts.Read(recorded[0])
			if err != nil {
				return err
			}
			if len(plan.Scenarios) == 0 {
				out.Body.NoPlan = "The recorded forecast has no scenario"
				return nil
			}
			res, err := forecasts.Work(ctx, u, plan, base, today, plan.Scenarios[0].Name)
			if err != nil {
				return err
			}
			with, without := res.Runs[0].Result.Key, res.Runs[1].Result.Key
			t := &PlanningForecastTile{ForecastID: recorded[0].ID.String(), Name: plan.Name, Scenario: res.Runs[0].Scenario, Runway: without.RunwayMonths,
				Beyond: without.RunwayBeyond, BeyondFunded: with.RunwayBeyond, WithoutFunding: "Without the funding planned: none of it is received yet"}
			if without.CashOutMonth != nil {
				t.CashOut = without.CashOutMonth.Format("2006-01")
			}
			if with.CashOutMonth != nil {
				t.CashOutFunded = with.CashOutMonth.Format("2006-01")
			}
			out.Body.Forecast = t
			return nil
		})
		return &out, err
	})
}
