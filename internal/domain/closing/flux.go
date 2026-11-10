package closing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/budget"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/period"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Flux analysis in the close (PRD-001 FR-GL-14; the founders' decisions of rev 0.63, built in rev 0.65, provisional):
// each income or expense account's total for the period against its total for the previous period, both from the
// balance service (ENG-04) in the base currency, debit positive, as the trial balance and the general ledger report
// show them. A variance needs an explanation when it is more than both thresholds, two organisation settings changed
// under FM-1; an explanation records the two totals it explained and goes stale when either changes. Balance-sheet
// accounts rely on the support the close requires (INV-07). Once the month's financial year has a recorded budget, each
// account's total is also compared with the month's budget for it, at the same thresholds (p1-planning design D6), and
// explained apart: an explanation says which comparison it is of.

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

// Variance is one income or expense account's movement against the previous month, or against the month's budget.
type Variance struct {
	Account  storage.Account
	Kind     string      // storage.VarianceMonth or storage.VarianceBudget
	Current  money.Money // the account's total for the period: debit positive, credit negative
	Previous money.Money // its total for the previous period; for a budget variance, the month's budget for it, signed alike
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

// Flux is a period's comparison with the previous month and with its budget.
type Flux struct {
	Period     storage.Period
	Previous   *storage.Period // the period that ends the day before it starts; nil when there is none
	Currency   money.Currency
	Thresholds Thresholds
	Variances  []Variance // each income or expense account with a total in either period, by code
	// Budgeted says the month's financial year has a recorded budget; BudgetVariances are then each income or expense
	// account with a total or a budget for the month, by code.
	Budgeted        bool
	BudgetVariances []Variance
}

// Unexplained counts the variances, of either kind, that need an explanation and have no current one.
func (f Flux) Unexplained() int {
	return f.UnexplainedOf(storage.VarianceMonth) + f.UnexplainedOf(storage.VarianceBudget)
}

// UnexplainedOf counts the variances of a kind that need an explanation and have no current one.
func (f Flux) UnexplainedOf(kind string) int {
	n := 0
	for _, v := range f.Of(kind) {
		if v.Unexplained() {
			n++
		}
	}
	return n
}

// Of are the variances of a kind.
func (f Flux) Of(kind string) []Variance {
	if kind == storage.VarianceBudget {
		return f.BudgetVariances
	}
	return f.Variances
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
	f := Flux{Period: p, Currency: cur, Thresholds: t, Variances: []Variance{}, BudgetVariances: []Variance{}}
	for i := range all {
		if all[i].EndsOn.Equal(p.StartsOn.AddDate(0, 0, -1)) {
			f.Previous = &all[i]
		}
	}
	totals := func(tb balance.TrialBalance) map[uuid.UUID]money.Money {
		out := make(map[uuid.UUID]money.Money, len(tb.Rows))
		for _, r := range tb.Rows {
			out[r.AccountID] = r.Base
		}
		return out
	}
	now, err := balance.Trial(ctx, u, cur, p.ID)
	if err != nil {
		return Flux{}, err
	}
	nowBy, beforeBy := totals(now), map[uuid.UUID]money.Money{}
	if f.Previous != nil {
		before, err := balance.Trial(ctx, u, cur, f.Previous.ID)
		if err != nil {
			return Flux{}, err
		}
		beforeBy = totals(before)
	}
	// The month's budget (design D6), signed as the ledger signs it; none while its financial year has no recorded
	// budget, or the years are not set.
	var budgetBy map[uuid.UUID]money.Money
	o, err := u.Organisation(ctx)
	if err != nil {
		return Flux{}, err
	}
	if cal, err := period.CalendarOf(o.Profile); err == nil {
		if budgetBy, f.Budgeted, err = budget.ForMonths(ctx, u, cur, cal, p.StartsOn, p.StartsOn, nil); err != nil {
			return Flux{}, err
		}
	}
	if f.Previous == nil && !f.Budgeted {
		return f, nil
	}
	explanations, err := u.Closes().VarianceExplanations(ctx, p.ID)
	if err != nil {
		return Flux{}, err
	}
	latest := make(map[string]*storage.VarianceExplanation, len(explanations))
	for i := range explanations {
		latest[kindOf(explanations[i])+":"+explanations[i].AccountID.String()] = &explanations[i]
	}
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return Flux{}, err
	}
	figure := func(by map[uuid.UUID]money.Money, id uuid.UUID) money.Money {
		if m, ok := by[id]; ok {
			return m
		}
		return money.Zero(cur)
	}
	for _, a := range accounts {
		if a.Type != storage.Revenue && a.Type != storage.Expense {
			continue
		}
		current := figure(nowBy, a.ID)
		if f.Previous != nil {
			v, ok, err := varianceOf(a, storage.VarianceMonth, current, figure(beforeBy, a.ID), latest, t, cur)
			if err != nil {
				return Flux{}, err
			}
			if ok {
				f.Variances = append(f.Variances, v)
			}
		}
		if f.Budgeted {
			v, ok, err := varianceOf(a, storage.VarianceBudget, current, figure(budgetBy, a.ID), latest, t, cur)
			if err != nil {
				return Flux{}, err
			}
			if ok {
				f.BudgetVariances = append(f.BudgetVariances, v)
			}
		}
	}
	byCode := func(vs []Variance) {
		sort.Slice(vs, func(i, j int) bool { return vs[i].Account.Code < vs[j].Account.Code })
	}
	byCode(f.Variances)
	byCode(f.BudgetVariances)
	return f, nil
}

// kindOf is an explanation's kind; one written before the kinds is of the previous month.
func kindOf(e storage.VarianceExplanation) string {
	if e.Kind == "" {
		return storage.VarianceMonth
	}
	return e.Kind
}

// varianceOf is an account's variance of a kind: its total against the other figure, with its latest explanation of
// that kind; ok is false when both are zero, which is no variance.
func varianceOf(a storage.Account, kind string, current, other money.Money, latest map[string]*storage.VarianceExplanation, t Thresholds,
	cur money.Currency) (v Variance, ok bool, err error) {
	if current.IsZero() && other.IsZero() {
		return v, false, nil
	}
	v = Variance{Account: a, Kind: kind, Current: current, Previous: other, Explanation: latest[kind+":"+a.ID.String()]}
	if v.Change, err = v.Current.Sub(v.Previous); err != nil {
		return v, false, err
	}
	if !v.Previous.IsZero() {
		if v.Percent, err = rounding.Percent(v.Change, v.Previous); err != nil {
			return v, false, err
		}
	}
	if v.Needs, err = NeedsExplanation(v.Current, v.Previous, t); err != nil {
		return v, false, err
	}
	if e := v.Explanation; e != nil {
		was, err := money.ParseStored(string(e.Current), cur)
		if err != nil {
			return v, false, err
		}
		wasBefore, err := money.ParseStored(string(e.Previous), cur)
		if err != nil {
			return v, false, err
		}
		v.Explained = was.Equal(v.Current) && wasBefore.Equal(v.Previous)
	}
	return v, true, nil
}

// Explain records an explanation of an account's variance of a kind for an open period, in u (FR-GL-14): current and
// previous are the two figures the writer saw (for a budget variance, previous is the month's budget), which must still
// be the account's, so an explanation is never filed against figures its writer did not read. The thresholds in force
// on day decide nothing here: any variance may be explained.
func Explain(ctx context.Context, u storage.UnitOfWork, periodID, accountID uuid.UUID, kind string, current, previous money.Money, words string, day time.Time) (storage.VarianceExplanation, Variance, error) {
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
	if kind == "" {
		kind = storage.VarianceMonth
	}
	vs := f.Of(kind)
	var v *Variance
	for i := range vs {
		if vs[i].Account.ID == accountID {
			v = &vs[i]
		}
	}
	if v == nil {
		why := "it is not an income or expense account with a total in this period or the previous one"
		switch {
		case kind == storage.VarianceBudget && !f.Budgeted:
			why = "the month's financial year has no recorded budget"
		case kind == storage.VarianceBudget:
			why = "it is not an income or expense account with a total or a budget in this period"
		case f.Previous == nil:
			why = "the period has no previous period to compare with"
		}
		return storage.VarianceExplanation{}, Variance{}, &Error{CodeNoVariance, "the account has no variance to explain: " + why,
			"Explain one of the variances the period lists"}
	}
	if !v.Current.Equal(current) || !v.Previous.Equal(previous) {
		return storage.VarianceExplanation{}, Variance{}, &Error{CodeMoved, fmt.Sprintf("the account's figures are now %s and %s %s, not the %s and %s explained",
			v.Current, v.Previous, cur.Code, current, previous), "Read the variances again and explain the figures as they are now"}
	}
	e, err := u.Closes().ExplainVariance(ctx, storage.NewVarianceExplanation{PeriodID: p.ID, AccountID: accountID, Kind: kind,
		Current: storage.Decimal(v.Current.String()), Previous: storage.Decimal(v.Previous.String()), Explanation: words})
	if err != nil {
		return storage.VarianceExplanation{}, Variance{}, err
	}
	v.Explanation, v.Explained = &e, true
	return e, *v, nil
}

// fluxCheck is the close's check of the variances (Appendix C.2 #57): ✖ while a variance above the thresholds, against
// the previous month or the month's budget, has no current explanation.
func fluxCheck(f Flux) Check {
	month, budgeted := f.UnexplainedOf(storage.VarianceMonth), f.UnexplainedOf(storage.VarianceBudget)
	switch {
	case f.Previous == nil && !f.Budgeted:
		return Check{"variances", OK, "No earlier month to compare", ""}
	case month+budgeted > 0:
		var parts []string
		if month > 0 {
			parts = append(parts, fmt.Sprintf("%d income or expense account(s) moved by more than %s %s and %s%% against %s",
				month, f.Thresholds.Amount, f.Currency.Code, f.Thresholds.Percent.Text('f'), f.Previous.StartsOn.Format("January 2006")))
		}
		if budgeted > 0 {
			parts = append(parts, fmt.Sprintf("%d differ from %s's budget by more than %s %s and %s%%", budgeted,
				f.Period.StartsOn.Format("January 2006"), f.Thresholds.Amount, f.Currency.Code, f.Thresholds.Percent.Text('f')))
		}
		return Check{"variances", Failed, "Variances need an explanation", strings.Join(parts, ", and ") +
			" with no explanation of their totals as they are now: explain them, then run the checks again"}
	default:
		return Check{"variances", OK, "Every variance above the thresholds is explained", ""}
	}
}
