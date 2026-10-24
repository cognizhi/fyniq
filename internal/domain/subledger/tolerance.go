package subledger

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/apd/v3"

	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/rounding"
	"github.com/cognizhi/fyniq/internal/domain/settings"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Tolerance is what a settlement difference may clear on one document (FR-BNK-08, Appendix B.1; open question 32,
// decided with the recommended defaults): what is left of an invoice or a bill that a payment or a receipt settles in
// full though it paid a little less. The difference is the carrying amount it clears, in the base currency, after the
// realised FX, which is never limited. It is within tolerance when it is no more than the base tolerance (S$1.00) for a
// document in the base currency, or the greater of the foreign minimum (S$1.00) and the percentage (3%) of the
// document's total at its stored rate for one in another currency; never above the cap per document (S$50.00); and no
// more than what the month's earlier differences leave of the cap per month (S$200.00). Each comparison is exact.
type Tolerance struct {
	Base     money.Money
	Minimum  money.Money
	Percent  string
	Document money.Money
	Month    money.Money
	Used     money.Money // what the month's allocations cleared already, net of reversals
}

// LoadTolerance reads the tolerances in force on a day (the organisation's settings), and what the day's month cleared
// already.
func LoadTolerance(ctx context.Context, u storage.UnitOfWork, base money.Currency, on time.Time) (Tolerance, error) {
	read := func(key string) (string, error) {
		def, ok := settings.Lookup(key)
		if !ok {
			return "", fmt.Errorf("no setting %s", key)
		}
		changes, err := u.Settings().Changes(ctx, def.Key)
		if err != nil {
			return "", err
		}
		return settings.On(def, changes, on).Value, nil
	}
	var out Tolerance
	for _, x := range []struct {
		key string
		to  *money.Money
	}{{"bank.tolerance_base", &out.Base}, {"bank.tolerance_foreign_minimum", &out.Minimum}, {"bank.tolerance_cap_document", &out.Document},
		{"bank.tolerance_cap_month", &out.Month}} {
		v, err := read(x.key)
		if err != nil {
			return Tolerance{}, err
		}
		if *x.to, err = money.ParseStored(v, base); err != nil {
			return Tolerance{}, err
		}
	}
	var err error
	if out.Percent, err = read("bank.tolerance_foreign_percent"); err != nil {
		return Tolerance{}, err
	}
	y, m, _ := on.Date()
	first := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	used, err := u.Allocations().Tolerance(ctx, first, first.AddDate(0, 1, -1))
	if err != nil {
		return Tolerance{}, err
	}
	if out.Used, err = money.ParseStored(string(used), base); err != nil {
		return Tolerance{}, err
	}
	return out, nil
}

// Allows says whether a difference whose carrying amount is diff may clear on a document whose total at its stored rate
// is total, in the base currency, foreign when the document is not in the base currency; and when it may not, why, in
// plain words. A difference allowed should be added to Used before the next one is judged (Take).
func (t Tolerance) Allows(diff, total money.Money, foreign bool) (bool, string, error) {
	above := func(a, b money.Money) (bool, error) {
		c, err := a.Cmp(b)
		return c > 0, err
	}
	if over, err := above(diff, t.Document); err != nil || over {
		return false, fmt.Sprintf("the difference of %s %s is above the cap of %s per document", diff, diff.Currency(), t.Document), err
	}
	if !foreign {
		if over, err := above(diff, t.Base); err != nil || over {
			return false, fmt.Sprintf("the difference of %s %s is above the tolerance of %s", diff, diff.Currency(), t.Base), err
		}
	} else if over, err := above(diff, t.Minimum); err != nil {
		return false, "", err
	} else if over {
		// diff × 100 against total × percent, exactly: the percentage is never rounded first.
		pct, _, err := apd.NewFromString(t.Percent)
		if err != nil {
			return false, "", err
		}
		lhs, rhs := new(apd.Decimal), new(apd.Decimal)
		c := apd.BaseContext.WithPrecision(60)
		if _, err := c.Mul(lhs, diff.Decimal(), apd.New(100, 0)); err != nil {
			return false, "", err
		}
		if _, err := c.Mul(rhs, total.Decimal(), pct); err != nil {
			return false, "", err
		}
		if lhs.Cmp(rhs) > 0 {
			return false, fmt.Sprintf("the difference of %s %s is above both %s and %s%% of the document's %s", diff, diff.Currency(), t.Minimum, t.Percent, total), nil
		}
	}
	after, err := t.Used.Add(diff)
	if err != nil {
		return false, "", err
	}
	if over, err := above(after, t.Month); err != nil || over {
		return false, fmt.Sprintf("with the month's earlier differences of %s it is above the cap of %s per month", t.Used, t.Month), err
	}
	return true, "", nil
}

// Take counts a difference allowed against the month's cap.
func (t *Tolerance) Take(diff money.Money) error {
	var err error
	t.Used, err = t.Used.Add(diff)
	return err
}

// Difference is what settling a document in full with amount leaves as a settlement difference, as the posting rule
// clears it: short, what is open less amount, in the document's currency; diff, its carrying amount in the base currency,
// RND-FX of what is open less RND-FX of amount at the document's stored rate (the rule's split of the control line);
// and total, the document's total at its stored rate, which a foreign tolerance's percentage is of.
func Difference(amount, open, docTotal money.Money, stored money.Rate) (short, diff, total money.Money, err error) {
	if short, err = open.Sub(amount); err != nil {
		return
	}
	all, err := rounding.FX(open, stored)
	if err != nil {
		return
	}
	paid, err := rounding.FX(amount, stored)
	if err != nil {
		return
	}
	if diff, err = all.Sub(paid); err != nil {
		return
	}
	total, err = rounding.FX(docTotal, stored)
	return
}
