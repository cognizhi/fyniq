package closing

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Flux analysis in the close (PRD-001 FR-GL-14; the founders' decisions of rev 0.63, built in rev 0.65, provisional):
// each income or expense account's total for the period against its total for the previous period, both from the
// balance service (ENG-04) in the base currency, debit positive, as the trial balance and the general ledger report
// show them. A variance needs an explanation when it is more than both thresholds, two organisation settings changed
// under FM-1; an explanation records the two totals it explained and goes stale when either changes. Balance-sheet
// accounts rely on the support the close requires (INV-07); budget variances arrive with budgets (p1-planning).

// The settings of the thresholds.
const (
	VarianceAmountKey  = "close.variance_amount"
	VariancePercentKey = "close.variance_percent"
)

// The stable error codes of an explanation (ENG-23).
const (
	CodeNoVariance = "CLOSE-09" // the account has no variance in the period to explain
	CodeMoved      = "CLOSE-10" // the account's totals are not the ones the explanation was written for
)

// Thresholds are the variance thresholds in force: a variance above both needs an explanation.
type Thresholds struct {
	Amount  money.Money // in the base currency
	Percent apd.Decimal // of the previous period's total, such as 10 for 10%
}

// exact is wide enough that multiplying a stored amount by a percentage is exact.
var exact = apd.BaseContext.WithPrecision(60)

// NeedsExplanation says whether a variance from previous to current needs an explanation: its size is more than the
// amount and, when previous is not zero, more than the percentage of previous's size. When previous is zero the amount
// alone decides. It compares exact amounts (size × 100 against percent × |previous|), never a rounded percentage.
func NeedsExplanation(current, previous money.Money, t Thresholds) (bool, error) {
	change, err := current.Sub(previous)
	if err != nil {
		return false, err
	}
	size := change.Decimal()
	size.Abs(size)
	limit := t.Amount.Decimal()
	if size.Cmp(limit) <= 0 {
		return false, nil
	}
	if previous.IsZero() {
		return true, nil
	}
	before := previous.Decimal()
	before.Abs(before)
	var scaled, allowed apd.Decimal
	if _, err := exact.Mul(&scaled, size, apd.New(100, 0)); err != nil {
		return false, err
	}
	if _, err := exact.Mul(&allowed, &t.Percent, before); err != nil {
		return false, err
	}
	return scaled.Cmp(&allowed) > 0, nil
}

// Variance is one income or expense account's month-on-month movement.
type Variance struct {
	Account  storage.Account
	Current  money.Money // the account's total for the period: debit positive, credit negative
	Previous money.Money // its total for the previous period
	Change   money.Money // Current − Previous
	// Percent is Change as a percentage of the previous total's size (RND-PERCENT), with the change's sign; empty when
	// the previous total is zero.
	Percent     string
	Needs       bool                         // above both thresholds: it needs an explanation
	Explanation *storage.VarianceExplanation // the account's latest explanation for the period, if any
	Explained   bool                         // the latest explanation is of these two totals
}

// Unexplained says whether the variance needs an explanation and has no current one.
func (v Variance) Unexplained() bool { return v.Needs && !v.Explained }

// Flux is a period's month-on-month comparison.
type Flux struct {
	Period     storage.Period
	Previous   *storage.Period // the period that ends the day before it starts; nil when there is none
	Currency   money.Currency
	Thresholds Thresholds
	Variances  []Variance // each income or expense account with a total in either period, by code
}

// Unexplained counts the variances that need an explanation and have no current one.
func (f Flux) Unexplained() int {
	n := 0
	for _, v := range f.Variances {
		if v.Unexplained() {
			n++
		}
	}
	return n
}

// thresholds reads the thresholds in force on day.
func thresholds(ctx context.Context, u storage.UnitOfWork, base money.Currency, day time.Time) (Thresholds, error) {
	value := func(key string) (string, error) {
		d, ok := settings.Lookup(key)
		if !ok {
			return "", fmt.Errorf("closing: no setting %s", key)
		}
		changes, err := u.Settings().Changes(ctx, key)
		if err != nil {
			return "", err
		}
		return settings.On(d, changes, day).Value, nil
	}
	var t Thresholds
	amount, err := value(VarianceAmountKey)
	if err != nil {
		return t, err
	}
	if t.Amount, err = money.Parse(amount, base); err != nil {
		return t, err
	}
	percent, err := value(VariancePercentKey)
	if err != nil {
		return t, err
	}
	if _, _, err := t.Percent.SetString(percent); err != nil {
		return t, fmt.Errorf("closing: %s is %q: %w", VariancePercentKey, percent, err)
	}
	return t, nil
}

// Compare compares a period with the previous one, with the thresholds in force on day and each account's latest
// explanation.
func Compare(ctx context.Context, u storage.UnitOfWork, periodID uuid.UUID, day time.Time) (Flux, error) {
	p, err := u.Periods().Get(ctx, periodID)
	if err != nil {
		return Flux{}, err
	}
	all, err := u.Periods().List(ctx)
	if err != nil {
		return Flux{}, err
	}
	cur, _, err := base(ctx, u)
	if err != nil {
		return Flux{}, err
	}
	return compare(ctx, u, p, all, cur, day)
}

func compare(ctx context.Context, u storage.UnitOfWork, p storage.Period, all []storage.Period, cur money.Currency, day time.Time) (Flux, error) {
	t, err := thresholds(ctx, u, cur, day)
	if err != nil {
		return Flux{}, err
	}
	f := Flux{Period: p, Currency: cur, Thresholds: t, Variances: []Variance{}}
	for i := range all {
		if all[i].EndsOn.Equal(p.StartsOn.AddDate(0, 0, -1)) {
			f.Previous = &all[i]
		}
	}
	if f.Previous == nil {
		return f, nil
	}
	now, err := balance.Trial(ctx, u, cur, p.ID)
	if err != nil {
		return Flux{}, err
	}
	before, err := balance.Trial(ctx, u, cur, f.Previous.ID)
	if err != nil {
		return Flux{}, err
	}
	totals := func(tb balance.TrialBalance) map[uuid.UUID]money.Money {
		out := make(map[uuid.UUID]money.Money, len(tb.Rows))
		for _, r := range tb.Rows {
			out[r.AccountID] = r.Base
		}
		return out
	}
	nowBy, beforeBy := totals(now), totals(before)
	explanations, err := u.Closes().VarianceExplanations(ctx, p.ID)
	if err != nil {
		return Flux{}, err
	}
	latest := make(map[uuid.UUID]*storage.VarianceExplanation, len(explanations))
	for i := range explanations {
		latest[explanations[i].AccountID] = &explanations[i]
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return Flux{}, err
	}
	for _, a := range accounts {
		if a.Type != storage.Revenue && a.Type != storage.Expense {
			continue
		}
		v := Variance{Account: a, Current: money.Zero(cur), Previous: money.Zero(cur), Explanation: latest[a.ID]}
		if m, ok := nowBy[a.ID]; ok {
			v.Current = m
		}
		if m, ok := beforeBy[a.ID]; ok {
			v.Previous = m
		}
		if v.Current.IsZero() && v.Previous.IsZero() {
			continue
		}
		if v.Change, err = v.Current.Sub(v.Previous); err != nil {
			return Flux{}, err
		}
		if !v.Previous.IsZero() {
			if v.Percent, err = rounding.Percent(v.Change, v.Previous); err != nil {
				return Flux{}, err
			}
		}
		if v.Needs, err = NeedsExplanation(v.Current, v.Previous, t); err != nil {
			return Flux{}, err
		}
		if e := v.Explanation; e != nil {
			was, err := money.ParseStored(string(e.Current), cur)
			if err != nil {
				return Flux{}, err
			}
			wasBefore, err := money.ParseStored(string(e.Previous), cur)
			if err != nil {
				return Flux{}, err
			}
			v.Explained = was.Equal(v.Current) && wasBefore.Equal(v.Previous)
		}
		f.Variances = append(f.Variances, v)
	}
	sort.Slice(f.Variances, func(i, j int) bool { return f.Variances[i].Account.Code < f.Variances[j].Account.Code })
	return f, nil
}

// Explain records an explanation of an account's variance for an open period, in u (FR-GL-14): current and previous
// are the two totals the writer saw, which must still be the account's, so an explanation is never filed against
// figures its writer did not read. The thresholds in force on day decide nothing here: any variance may be explained.
func Explain(ctx context.Context, u storage.UnitOfWork, periodID, accountID uuid.UUID, current, previous money.Money, words string, day time.Time) (storage.VarianceExplanation, Variance, error) {
	p, err := u.Periods().Get(ctx, periodID)
	if err != nil {
		return storage.VarianceExplanation{}, Variance{}, err
	}
	if p.Status != storage.PeriodOpen {
		return storage.VarianceExplanation{}, Variance{}, &Error{CodeNotOpen, fmt.Sprintf("the period %s to %s is %s", p.StartsOn.Format(time.DateOnly),
			p.EndsOn.Format(time.DateOnly), p.Status), "Explain variances while the period is open: ask to reopen it first"}
	}
	all, err := u.Periods().List(ctx)
	if err != nil {
		return storage.VarianceExplanation{}, Variance{}, err
	}
	cur, _, err := base(ctx, u)
	if err != nil {
		return storage.VarianceExplanation{}, Variance{}, err
	}
	f, err := compare(ctx, u, p, all, cur, day)
	if err != nil {
		return storage.VarianceExplanation{}, Variance{}, err
	}
	var v *Variance
	for i := range f.Variances {
		if f.Variances[i].Account.ID == accountID {
			v = &f.Variances[i]
		}
	}
	if v == nil {
		why := "it is not an income or expense account with a total in this period or the previous one"
		if f.Previous == nil {
			why = "the period has no previous period to compare with"
		}
		return storage.VarianceExplanation{}, Variance{}, &Error{CodeNoVariance, "the account has no variance to explain: " + why,
			"Explain one of the variances the period lists"}
	}
	if !v.Current.Equal(current) || !v.Previous.Equal(previous) {
		return storage.VarianceExplanation{}, Variance{}, &Error{CodeMoved, fmt.Sprintf("the account's totals are now %s and %s %s, not the %s and %s explained",
			v.Current, v.Previous, cur.Code, current, previous), "Read the variances again and explain the totals as they are now"}
	}
	e, err := u.Closes().ExplainVariance(ctx, storage.NewVarianceExplanation{PeriodID: p.ID, AccountID: accountID,
		Current: storage.Decimal(v.Current.String()), Previous: storage.Decimal(v.Previous.String()), Explanation: words})
	if err != nil {
		return storage.VarianceExplanation{}, Variance{}, err
	}
	v.Explanation, v.Explained = &e, true
	return e, *v, nil
}

// fluxCheck is the close's check of the variances (Appendix C.2 #57): ✖ while a variance above the thresholds has no
// current explanation.
func fluxCheck(f Flux) Check {
	switch n := f.Unexplained(); {
	case f.Previous == nil:
		return Check{"variances", OK, "No earlier month to compare", ""}
	case n > 0:
		return Check{"variances", Failed, "Variances need an explanation", fmt.Sprintf(
			"%d income or expense account(s) moved by more than %s %s and %s%% against %s with no explanation of their totals as they are now: explain them, then run the checks again",
			n, f.Thresholds.Amount, f.Currency.Code, f.Thresholds.Percent.Text('f'), f.Previous.StartsOn.Format("January 2006"))}
	default:
		return Check{"variances", OK, "Every variance above the thresholds is explained", ""}
	}
}
