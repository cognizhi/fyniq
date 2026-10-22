package balance

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Year-end without closing entries (FR-GL-10; PRD-001 rev 0.56, provisional): the revenue and expense accounts start
// each financial year from nothing, and the retained-earnings account carries the profit or loss of every earlier
// year. It is computed whenever a balance is read, never posted, so there is one continuous ledger and nothing to
// carry forward. Without the organisation's financial years, or without its retained-earnings account, nothing folds
// and every account carries on as one balance.

// CodeCrossesYear refuses a general ledger report whose range crosses the start of a financial year.
const CodeCrossesYear = "REPORT-02"

// CrossesYear is the refusal of a range that starts in one financial year and ends in another: a revenue or expense
// account starts each year from nothing, so its lines are read one year at a time.
type CrossesYear struct {
	YearStart time.Time
}

func (e *CrossesYear) Error() string {
	return fmt.Sprintf("%s: the range crosses the start of the financial year on %s", CodeCrossesYear, e.YearStart.Format(time.DateOnly))
}

// yearEnd is what the fold needs: the organisation's years, its revenue and expense accounts, and its
// retained-earnings account. ok is false when either is not set.
type yearEnd struct {
	cal     period.Calendar
	pl      map[uuid.UUID]bool
	revenue map[uuid.UUID]bool
	re      uuid.UUID
	ok      bool
}

func yearEndOf(ctx context.Context, u storage.UnitOfWork) (yearEnd, error) {
	o, err := u.Organisation(ctx)
	if err != nil {
		return yearEnd{}, err
	}
	cal, err := period.CalendarOf(o.Profile)
	if err != nil {
		return yearEnd{}, nil // no financial years set: nothing folds
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return yearEnd{}, err
	}
	y := yearEnd{cal: cal, pl: map[uuid.UUID]bool{}, revenue: map[uuid.UUID]bool{}}
	for _, a := range accounts {
		if a.Type == storage.Revenue || a.Type == storage.Expense {
			y.pl[a.ID] = true
		}
		if a.Type == storage.Revenue {
			y.revenue[a.ID] = true
		}
		if a.Role == storage.RoleRetainedEarnings {
			y.re, y.ok = a.ID, true
		}
	}
	return y, nil
}

// start is the first day of the financial year that holds day; the zero time when nothing folds or day is before the
// books start.
func (y yearEnd) start(day time.Time) time.Time {
	if !y.ok {
		return time.Time{}
	}
	n := y.cal.Number(day)
	if n == 0 {
		return time.Time{}
	}
	return y.cal.Year(n).StartsOn
}

// earlier says whether a period ends before the financial year that starts on ys.
func earlier(p storage.Period, ys time.Time) bool { return !ys.IsZero() && p.EndsOn.Before(ys) }

// Position is the trial balance at the end of a period, year-end applied: every period up to it, with the revenue and
// expense of earlier financial years folded into retained earnings (FR-GL-10). The total does not change, so it nets to
// zero exactly when the plain trial balance does.
func Position(ctx context.Context, u storage.UnitOfWork, base money.Currency, all []storage.Period, through storage.Period) (TrialBalance, error) {
	y, err := yearEndOf(ctx, u)
	if err != nil {
		return TrialBalance{}, err
	}
	ys := y.start(through.EndsOn)
	var past, current []uuid.UUID
	for _, p := range all {
		switch {
		case p.EndsOn.After(through.EndsOn):
		case earlier(p, ys):
			past = append(past, p.ID)
		default:
			current = append(current, p.ID)
		}
	}
	tb, err := Trial(ctx, u, base, current...)
	if err != nil {
		return TrialBalance{}, err
	}
	if len(past) == 0 {
		return tb, nil
	}
	before, err := Trial(ctx, u, base, past...)
	if err != nil {
		return TrialBalance{}, err
	}
	rows := map[uuid.UUID]money.Money{}
	add := func(id uuid.UUID, m money.Money) error {
		cur, ok := rows[id]
		if !ok {
			cur = money.Zero(base)
		}
		sum, err := cur.Add(m)
		rows[id] = sum
		return err
	}
	for _, r := range tb.Rows {
		if err := add(r.AccountID, r.Base); err != nil {
			return TrialBalance{}, err
		}
	}
	for _, r := range before.Rows {
		id := r.AccountID
		if y.pl[id] {
			id = y.re // the profit or loss of an earlier year is in retained earnings now
		}
		if err := add(id, r.Base); err != nil {
			return TrialBalance{}, err
		}
	}
	out := TrialBalance{Total: money.Zero(base)}
	for id, amount := range rows {
		out.Rows = append(out.Rows, Row{AccountID: id, Base: amount})
		if out.Total, err = out.Total.Add(amount); err != nil {
			return TrialBalance{}, err
		}
	}
	sortRows(out.Rows)
	return out, nil
}

// YearResult is a financial year's result and what it does to retained earnings, year-end applied (FR-GL-10; PRD-001
// rev 0.69, provisional). Every amount is in the base currency as in the trial balance, debits positive and credits
// negative, so income and a profit are negative. Nothing is posted for it.
type YearResult struct {
	Income   money.Money // the year's revenue
	Expenses money.Money // the year's expenses
	Result   money.Money // income and expenses together: negative is a profit, positive a loss
	// Retained earnings, while the organisation has an account with the retained-earnings role (Folds); otherwise
	// nothing folds and these are zero.
	Folds          bool
	RetainedID     uuid.UUID
	RetainedStart  money.Money // on the year's first day: every earlier year's result, and what was posted to it before
	RetainedPosted money.Money // posted to the account in the year, such as the opening balances
	RetainedEnd    money.Money // start, posted and the year's result: what the next year opens with
}

// Year is the result of the financial year y from the periods all, as posted so far: the year's own periods, and
// those before it for retained earnings at its start.
func Year(ctx context.Context, u storage.UnitOfWork, base money.Currency, all []storage.Period, y period.Span) (YearResult, error) {
	ye, err := yearEndOf(ctx, u)
	if err != nil {
		return YearResult{}, err
	}
	var in, before []uuid.UUID
	for _, p := range all {
		switch {
		case p.EndsOn.Before(y.StartsOn):
			before = append(before, p.ID)
		case !p.StartsOn.Before(y.StartsOn) && !p.EndsOn.After(y.EndsOn):
			in = append(in, p.ID)
		}
	}
	out := YearResult{Income: money.Zero(base), Expenses: money.Zero(base), Result: money.Zero(base), Folds: ye.ok, RetainedID: ye.re,
		RetainedStart: money.Zero(base), RetainedPosted: money.Zero(base), RetainedEnd: money.Zero(base)}
	year, err := Trial(ctx, u, base, in...)
	if err != nil {
		return YearResult{}, err
	}
	for _, r := range year.Rows {
		switch {
		case ye.revenue[r.AccountID]:
			out.Income, err = out.Income.Add(r.Base)
		case ye.pl[r.AccountID]:
			out.Expenses, err = out.Expenses.Add(r.Base)
		case ye.ok && r.AccountID == ye.re:
			out.RetainedPosted, err = out.RetainedPosted.Add(r.Base)
		}
		if err != nil {
			return YearResult{}, err
		}
	}
	if out.Result, err = out.Income.Add(out.Expenses); err != nil {
		return YearResult{}, err
	}
	if !ye.ok {
		return out, nil
	}
	earlier, err := Trial(ctx, u, base, before...)
	if err != nil {
		return YearResult{}, err
	}
	for _, r := range earlier.Rows {
		if ye.pl[r.AccountID] || r.AccountID == ye.re {
			if out.RetainedStart, err = out.RetainedStart.Add(r.Base); err != nil {
				return YearResult{}, err
			}
		}
	}
	if out.RetainedEnd, err = out.RetainedStart.Add(out.RetainedPosted); err == nil {
		out.RetainedEnd, err = out.RetainedEnd.Add(out.Result)
	}
	return out, err
}
