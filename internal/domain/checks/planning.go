package checks

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/budget"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The Appendix D row whose view opens the planning checks' items.
const viewBudgets = "Budgets, forecasts"

// The checks of planning (p1-planning).
var planningChecks = []Definition{
	{Key: "planning.budget", Ref: "C.1 #35", Area: "Planning", Worst: Warning, Title: "Every year with postings has a budget",
		Explain: "A financial year whose books have postings has an approved budget, so Budget vs Actual, the close's budget " +
			"variances and Home's budget tiles have something to compare with. Draft one in Planning › Budgets and have it approved.",
		Rules: "FR-PLN-01", Owner: "controller", View: viewBudgets, Triggers: []string{"document", "journal_entry", "period"}, Run: budgetPerYear},
}

// budgetPerYear lists each financial year with posted entries and no recorded budget (C.1 #35, FR-PLN-01).
func budgetPerYear(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	cal, err := period.CalendarOf(e.Org.Profile)
	if err != nil {
		return Missing("the books' start and the first financial year's end", "Settings › Organisation"), nil
	}
	years := map[int][]uuid.UUID{}
	var order []int
	for _, p := range e.Periods {
		n := cal.Number(p.StartsOn)
		if _, seen := years[n]; !seen {
			order = append(order, n)
		}
		years[n] = append(years[n], p.ID)
	}
	var items []Item
	for _, n := range order {
		tb, err := balance.Trial(ctx, u, e.Base, years[n]...)
		if err != nil {
			return Result{}, err
		}
		posted := false
		for _, r := range tb.Rows {
			posted = posted || !r.Base.IsZero()
		}
		if !posted {
			continue
		}
		y := cal.Year(n)
		_, _, ok, err := budget.OfYear(ctx, u, y.StartsOn)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			items = append(items, Item{Kind: "period", ID: y.StartsOn.Format("2006-01-02"),
				Label:  fmt.Sprintf("The financial year %s to %s", y.StartsOn.Format("2 Jan 2006"), y.EndsOn.Format("2 Jan 2006")),
				Detail: "It has postings and no approved budget"})
		}
	}
	if len(items) == 0 {
		return Fine("every financial year with postings has an approved budget"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d financial year(s) with postings and no approved budget", len(items)),
		"Draft the year's budget in Planning › Budgets and have it approved"), nil
}
