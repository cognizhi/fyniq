package budget

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Budget vs Actual (FR-RPT-03; p1-planning design D6): the year's budget against the books, by account or by account
// and dimension set, for a run of months inside one financial year. Amounts are signed as the P&L shows them; income
// above its budget, or costs below theirs, are favourable.

// The ways a report groups its rows.
const (
	ByAccount   = "account"   // each account, every dimension set added up
	ByDimension = "dimension" // each account and set of dimension values, matched exactly
)

// Figures are an actual, its budget, the variance (actual − budget) and the variance as a percentage of the budget's
// size (RND-PERCENT; "" when the budget is zero).
type Figures struct {
	Actual, Budget, Variance money.Money
	Percent                  string
}

// VsRow is one row of the report.
type VsRow struct {
	Account    storage.Account
	Values     []uuid.UUID // the dimension set, by=dimension; nil otherwise or for the lines with none
	Income     bool
	Favourable bool
	Figures
}

// VsActual is the report for the months from the first of from to the end of to.
type VsActual struct {
	Year     period.Year
	From, To time.Time
	By       string
	Budget   *storage.Document // the year's budget, nil while it has none
	Name     string            // its name
	Rows     []VsRow
	Income   Figures
	Costs    Figures
	Net      Figures // income less costs
}

func figuresOf(actual, budget money.Money) (Figures, error) {
	f := Figures{Actual: actual, Budget: budget}
	var err error
	if f.Variance, err = actual.Sub(budget); err != nil {
		return f, err
	}
	if !budget.IsZero() {
		f.Percent, err = rounding.Percent(f.Variance, budget)
	}
	return f, err
}

// Report reads Budget vs Actual for the months from..to, which lie in one financial year, as the books stand.
func Report(ctx context.Context, u storage.UnitOfWork, base money.Currency, cal period.Calendar, from, to time.Time, by string) (VsActual, error) {
	from = time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	to = time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)
	out := VsActual{From: from, To: to, By: by}
	if by != ByAccount && by != ByDimension {
		return out, &Error{CodeBudget, fmt.Sprintf("%q is neither account nor dimension", by), "Group by account or by dimension"}
	}
	n := cal.Number(from.AddDate(0, 1, -1)) // a month's last day: the books may start within it
	if n < 1 || to.Before(from) || cal.Number(to.AddDate(0, 1, -1)) != n {
		return out, &Error{CodeBudget, "the months are not in one financial year, in order", "Ask for months of one financial year, the first before the last"}
	}
	out.Year = cal.Year(n)
	doc, b, ok, err := OfYear(ctx, u, out.Year.StartsOn)
	if err != nil {
		return out, err
	}
	if ok {
		out.Budget, out.Name = &doc, b.Name
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return out, err
	}
	byID := map[uuid.UUID]storage.Account{}
	for _, a := range accounts {
		byID[a.ID] = a
	}
	key := func(account uuid.UUID, values []uuid.UUID) string {
		if by == ByAccount {
			return account.String()
		}
		vs := slices.Clone(values)
		slices.SortFunc(vs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		parts := []string{account.String()}
		for _, v := range vs {
			parts = append(parts, v.String())
		}
		return strings.Join(parts, "|")
	}
	type acc struct {
		account        uuid.UUID
		values         []uuid.UUID
		actual, budget *apd.Decimal
	}
	rows := map[string]*acc{}
	at := func(account uuid.UUID, values []uuid.UUID) *acc {
		k := key(account, values)
		if r, ok := rows[k]; ok {
			return r
		}
		r := &acc{account: account, actual: apd.New(0, 0), budget: apd.New(0, 0)}
		if by == ByDimension {
			r.values = values
		}
		rows[k] = r
		return r
	}
	c := apd.BaseContext.WithPrecision(60)
	// The actuals: each month's lines of the income and expense accounts, by account and set, as the P&L signs them.
	periods, err := u.Periods().List(ctx)
	if err != nil {
		return out, err
	}
	for _, p := range periods {
		if p.StartsOn.Before(from) || p.StartsOn.After(to) {
			continue
		}
		sets, err := balance.BySet(ctx, u, base, p.ID)
		if err != nil {
			return out, err
		}
		for _, s := range sets {
			a, ok := byID[s.AccountID]
			if !ok || (a.Type != storage.Revenue && a.Type != storage.Expense) {
				continue
			}
			x := s.Base.Decimal()
			if a.Type == storage.Revenue {
				x.Neg(x)
			}
			r := at(s.AccountID, s.Values)
			c.Add(r.actual, r.actual, x)
		}
	}
	// The budget: each line's months from..to.
	if ok {
		first := 0
		for i, m := range out.Year.Months {
			if firstOf(m.StartsOn).Equal(from) {
				first = i
			}
		}
		last := first + (to.Year()-from.Year())*12 + int(to.Month()) - int(from.Month())
		for _, l := range b.Lines {
			id, err := uuid.Parse(l.AccountID)
			if err != nil {
				return out, err
			}
			var values []uuid.UUID
			for _, v := range l.Dimensions {
				vid, err := uuid.Parse(v)
				if err != nil {
					return out, err
				}
				values = append(values, vid)
			}
			r := at(id, values)
			for k := first; k <= last && k < len(l.Amounts); k++ {
				x, _, err := apd.NewFromString(l.Amounts[k])
				if err != nil {
					return out, err
				}
				c.Add(r.budget, r.budget, x)
			}
		}
	}
	zero := money.Zero(base)
	totals := map[bool][2]money.Money{true: {zero, zero}, false: {zero, zero}}
	for _, r := range rows {
		a := byID[r.account]
		actual, err := money.FromRounded(r.actual, base)
		if err != nil {
			return out, err
		}
		budget, err := money.FromRounded(r.budget, base)
		if err != nil {
			return out, err
		}
		if actual.IsZero() && budget.IsZero() {
			continue
		}
		f, err := figuresOf(actual, budget)
		if err != nil {
			return out, err
		}
		income := a.Type == storage.Revenue
		row := VsRow{Account: a, Values: r.values, Income: income, Figures: f}
		row.Favourable = (income && f.Variance.Sign() >= 0) || (!income && f.Variance.Sign() <= 0)
		out.Rows = append(out.Rows, row)
		t := totals[income]
		if t[0], err = t[0].Add(actual); err != nil {
			return out, err
		}
		if t[1], err = t[1].Add(budget); err != nil {
			return out, err
		}
		totals[income] = t
	}
	slices.SortFunc(out.Rows, func(x, y VsRow) int {
		if d := strings.Compare(x.Account.Code, y.Account.Code); d != 0 {
			return d
		}
		return strings.Compare(fmt.Sprint(x.Values), fmt.Sprint(y.Values))
	})
	if out.Income, err = figuresOf(totals[true][0], totals[true][1]); err != nil {
		return out, err
	}
	if out.Costs, err = figuresOf(totals[false][0], totals[false][1]); err != nil {
		return out, err
	}
	netActual, err := totals[true][0].Sub(totals[false][0])
	if err != nil {
		return out, err
	}
	netBudget, err := totals[true][1].Sub(totals[false][1])
	if err != nil {
		return out, err
	}
	out.Net, err = figuresOf(netActual, netBudget)
	return out, err
}
