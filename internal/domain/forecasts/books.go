package forecasts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/financials"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/planning"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// cashLine is the cash-flow line of the accounts the forecast starts its cash from.
const cashLine = "Cash & cash equivalents"

// Books are the books' figures a forecast starts from (design D3), read through the balance service at the last actual
// month's end, and each report group's average of the last three months, shown beside the expense line that plans it.
type Books struct {
	LastActual  time.Time // the last actual month's first day
	Start       time.Time // the forecast's first month: the month after
	Figures     planning.Books
	Hints       map[string]money.Money // report group → its average over the last three actual months
	SalesGroups map[string]bool        // the report groups whose every account is S&M: their expense line adds to CAC's S&M
	Revision    int64
}

// LastActual is the last month with complete actuals: the month the plan names, else the latest closed period's month
// that ends by today, else the month before today.
func LastActual(periods []storage.Period, today time.Time, named string) (time.Time, error) {
	if named != "" {
		return Month(named)
	}
	var last time.Time
	for _, p := range periods {
		if p.Status == storage.PeriodClosed && !p.EndsOn.After(today) && p.StartsOn.After(last) {
			last = p.StartsOn
		}
	}
	if !last.IsZero() {
		return time.Date(last.Year(), last.Month(), 1, 0, 0, 0, 0, time.UTC), nil
	}
	return time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -1, 0), nil
}

// ReadBooks reads the books' figures for a forecast whose last actual month is last and whose horizon is h months.
func ReadBooks(ctx context.Context, u storage.UnitOfWork, base money.Currency, last time.Time, h int) (Books, error) {
	b := Books{LastActual: last, Start: last.AddDate(0, 1, 0), Hints: map[string]money.Money{}, SalesGroups: map[string]bool{}}
	zero := apd.New(0, 0)
	b.Figures = planning.Books{Cash: zero, Receivables: zero, Payables: zero}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return b, err
	}
	sales := map[string]bool{}
	for _, a := range accounts {
		if a.Group || a.Type != storage.Expense || a.ReportGroup == "" {
			continue
		}
		g := strings.TrimSpace(a.ReportGroup)
		if _, seen := sales[g]; !seen {
			sales[g] = true
		}
		sales[g] = sales[g] && a.Function == planning.FunctionSM
	}
	for g, all := range sales {
		if all {
			b.SalesGroups[g] = true
		}
	}
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return b, err
	}
	at := -1
	for i, p := range periods {
		if !p.StartsOn.After(last) && !p.EndsOn.Before(last) {
			at = i
		}
	}
	dep, err := journal.PlannedDepreciation(ctx, u, b.Start, h, base)
	if err != nil {
		return b, err
	}
	for _, x := range dep {
		b.Figures.Depreciation = append(b.Figures.Depreciation, x.Decimal())
	}
	if b.Revision, err = u.Ledger().Revision(ctx); err != nil {
		return b, err
	}
	if at < 0 { // no books for the last actual month yet: the forecast starts from nothing
		return b, nil
	}
	bs, err := financials.Lay(ctx, u, base, financials.Request{Kind: financials.BalanceSheet, All: periods, First: at, Last: at, Revision: b.Revision})
	if err != nil {
		return b, err
	}
	sum := func(into **apd.Decimal, m money.Money) error {
		x := new(apd.Decimal)
		if _, err := apd.BaseContext.WithPrecision(60).Add(x, *into, m.Decimal()); err != nil {
			return err
		}
		*into = x
		return nil
	}
	for _, block := range bs.Blocks {
		for _, g := range block.Groups {
			for _, l := range g.Lines {
				if len(l.Amounts) == 0 {
					continue
				}
				amount := l.Amounts[len(l.Amounts)-1]
				switch {
				case strings.TrimSpace(l.Account.CashFlowLine) == cashLine:
					err = sum(&b.Figures.Cash, amount)
				case l.Account.Role == "accounts-receivable":
					err = sum(&b.Figures.Receivables, amount)
				case l.Account.Role == "accounts-payable":
					err = sum(&b.Figures.Payables, amount)
				}
				if err != nil {
					return b, err
				}
			}
		}
	}
	first := max(0, at-2)
	pl, err := financials.Lay(ctx, u, base, financials.Request{Kind: financials.ProfitAndLoss, All: periods, First: first, Last: at, Revision: b.Revision})
	if err != nil {
		return b, err
	}
	months := apd.New(int64(at-first+1), 0) // three, fewer only before the books have three months
	for _, block := range pl.Blocks {
		for _, g := range block.Groups {
			if len(g.Totals) == 0 || len(g.Lines) == 0 {
				continue
			}
			avg, err := rounding.PlanQuotient(g.Totals[len(g.Totals)-1].Decimal(), months, base.MinorUnits)
			if err != nil {
				return b, err
			}
			h, err := money.FromRounded(avg, base)
			if err != nil {
				return b, fmt.Errorf("the hint for %s: %w", g.Name, err)
			}
			b.Hints[strings.TrimSpace(g.Name)] = h
		}
	}
	return b, nil
}
