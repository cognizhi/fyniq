package api

import (
	"context"
	"slices"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/budget"
	"github.com/cognizhi/fyniq/internal/domain/forecasts"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/planning"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------- a P&L's budget and forecast comparatives (FR-RPT-02, design D6)

// budgetColumn is a P&L's budget column for the months from..to: each account's budget, signed as the ledger signs it,
// of the lines that carry each of only's values; nil when a month's financial year has no recorded budget, or the
// years are not set, so the statement leaves the column out.
func budgetColumn(ctx context.Context, u storage.UnitOfWork, profile storage.OrganisationProfile, base money.Currency, from, to time.Time,
	only []uuid.UUID) (map[uuid.UUID]money.Money, error) {
	cal, err := period.CalendarOf(profile)
	if err != nil {
		return nil, nil // no financial years, so no budget
	}
	var carries func([]uuid.UUID) bool
	if len(only) > 0 {
		carries = func(values []uuid.UUID) bool {
			for _, v := range only {
				if !slices.Contains(values, v) {
					return false
				}
			}
			return true
		}
	}
	figs, ok, err := budget.ForMonths(ctx, u, base, cal, from, to, carries)
	if err != nil || !ok {
		return nil, err
	}
	return figs, nil
}

// forecastSections are the P&L blocks a forecast has a figure for, in the P&L's order, and how each month gives it.
var forecastSections = []struct {
	key string
	of  func(m planning.Month, c *apd.Context, cur money.Currency) (*apd.Decimal, error)
}{
	{"revenue", func(m planning.Month, _ *apd.Context, cur money.Currency) (*apd.Decimal, error) {
		return rounding.Plan(m.Revenue, cur), nil
	}},
	{"cost_of_sales", func(m planning.Month, _ *apd.Context, cur money.Currency) (*apd.Decimal, error) {
		return rounding.Plan(m.DirectCosts, cur), nil
	}},
	{"gross_profit", func(m planning.Month, _ *apd.Context, cur money.Currency) (*apd.Decimal, error) {
		return rounding.Plan(m.GrossProfit, cur), nil
	}},
	// From gross profit to EBITDA, as each shows, so gross profit less these is the EBITDA the forecast shows.
	{"operating_expenses", func(m planning.Month, c *apd.Context, cur money.Currency) (*apd.Decimal, error) {
		var d apd.Decimal
		_, err := c.Sub(&d, rounding.Plan(m.GrossProfit, cur), rounding.Plan(m.EBITDA, cur))
		return &d, err
	}},
	{"profit_before_tax", func(m planning.Month, _ *apd.Context, cur money.Currency) (*apd.Decimal, error) {
		return rounding.Plan(m.ProfitBeforeTax, cur), nil
	}},
	{"net_profit", func(m planning.Month, _ *apd.Context, cur money.Currency) (*apd.Decimal, error) {
		return rounding.Plan(m.NetProfit, cur), nil
	}},
}

// statementForecast is a P&L's forecast comparative for the months from..to (first days): the latest recorded
// forecast's first scenario with its funding plan, worked out on today, each section the total of its months as the
// forecast shows them (RND-PLAN); nil when no forecast is recorded, it has no scenario, or a month is not one of its.
func (a *API) statementForecast(ctx context.Context, u storage.UnitOfWork, base money.Currency, from, to time.Time) (*StatementForecast, error) {
	recorded, err := forecasts.Recorded(ctx, u)
	if err != nil || len(recorded) == 0 {
		return nil, err
	}
	plan, err := forecasts.Read(recorded[0])
	if err != nil {
		return nil, err
	}
	if len(plan.Scenarios) == 0 {
		return nil, nil
	}
	today, err := orgToday(ctx, a, u)
	if err != nil {
		return nil, err
	}
	res, err := forecasts.Work(ctx, u, plan, base, today, plan.Scenarios[0].Name)
	if err != nil {
		return nil, err
	}
	run := res.Runs[0] // with its funding plan, as the P&L it plans
	c := apd.BaseContext.WithPrecision(60)
	sums := make([]*apd.Decimal, len(forecastSections))
	for i := range sums {
		sums[i] = apd.New(0, 0)
	}
	covered := 0
	for _, m := range run.Result.Months {
		start := time.Date(m.Date.Year(), m.Date.Month(), 1, 0, 0, 0, 0, time.UTC)
		if start.Before(from) || start.After(to) {
			continue
		}
		covered++
		for i, s := range forecastSections {
			x, err := s.of(m, c, base)
			if err != nil {
				return nil, err
			}
			if _, err := c.Add(sums[i], sums[i], x); err != nil {
				return nil, err
			}
		}
	}
	if want := (to.Year()-from.Year())*12 + int(to.Month()) - int(from.Month()) + 1; covered != want {
		return nil, nil
	}
	out := &StatementForecast{ForecastID: recorded[0].ID.String(), Name: plan.Name, Scenario: run.Scenario, Sections: []StatementForecastSection{}}
	for i, s := range forecastSections {
		m, err := money.FromRounded(sums[i], base)
		if err != nil {
			return nil, err
		}
		out.Sections = append(out.Sections, StatementForecastSection{Key: s.key, Amount: moneyOut(m)})
	}
	return out, nil
}
