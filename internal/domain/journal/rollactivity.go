package journal

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/contactsummary"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// CustomerActivity is what one customer was billed, what it paid and what was recognised from it over a run, next to its deferred revenue (PRD-001 FR-RPT-13,
// rev 0.594), all in the base currency. Billed is what its invoices and credit notes dated inside the run moved on the receivables account (a credit note
// negative, GST included: what the customer owes more), Collected what its receipts dated inside the run took off it. Recognised is the revenue released from
// its deferral schedules inside the run (RollRow.Released) plus what its invoices and credit notes credited straight to a revenue account.
//
// The receivable's movement is read apart, from the sub-ledger at both ends of the run, and Billed - Collected + Other = Movement exactly, where Other is
// the movement of its other documents (a refund, a credit applied, a write-off, a deposit). Explained says that Other is what those documents moved; when it
// is not (a remeasurement, an edit of an earlier document), Unexplained holds the rest and the report says so rather than hiding it.
type CustomerActivity struct {
	Party       uuid.UUID
	Billed      money.Money
	Collected   money.Money
	Recognised  money.Money // the whole of it, direct and released
	Direct      money.Money // the part credited straight to revenue
	Movement    money.Money // the receivable's carrying amount at the run's end less what it was the day before
	Other       money.Money // Movement - Billed + Collected
	Unexplained money.Money // Other less what the customer's other documents moved; zero when it is explained
}

// activityTypes are the customer documents that move the receivable apart from invoices, credit notes and receipts.
var activityOther = []storage.DocumentType{storage.DocumentCustomerRefund, storage.DocumentWriteOff, storage.DocumentCustomerDeposit, storage.DocumentCustomerCreditApplication, storage.DocumentOpeningBalance}

// CustomerActivities works out, per customer, what was billed, collected and recognised over a run (FR-RPT-13); released is what each customer's
// deferred revenue released inside the run (the roll-forward's own column), so that Recognised is the whole.
func CustomerActivities(ctx context.Context, u storage.UnitOfWork, base money.Currency, from, to time.Time, released map[uuid.UUID]money.Money) ([]CustomerActivity, error) {
	accounts, err := u.Accounts().List(ctx)
	if err != nil {
		return nil, err
	}
	ar, ap := map[uuid.UUID]bool{}, map[uuid.UUID]bool{}
	revenue := map[uuid.UUID]bool{}
	var arIDs []uuid.UUID
	for _, a := range accounts {
		switch {
		case string(a.Role) == ledger.RoleAccountsReceivable:
			ar[a.ID] = true
			arIDs = append(arIDs, a.ID)
		case string(a.Role) == ledger.RoleAccountsPayable:
			ap[a.ID] = true
		case a.Type == storage.Revenue:
			revenue[a.ID] = true
		}
	}
	zero := money.Zero(base)
	by := map[uuid.UUID]*CustomerActivity{}
	row := func(p uuid.UUID) *CustomerActivity {
		if by[p] == nil {
			by[p] = &CustomerActivity{Party: p, Billed: zero, Collected: zero, Recognised: zero, Direct: zero, Movement: zero, Other: zero, Unexplained: zero}
		}
		return by[p]
	}
	add := func(into *money.Money, x money.Money) error {
		var err error
		*into, err = into.Add(x)
		return err
	}
	// The customer's documents inside the run, by type.
	walk := func(t storage.DocumentType, each func(r *CustomerActivity, doc storage.Document, value money.Money) error) error {
		f := storage.DocumentFilter{Type: t, From: from.Format(time.DateOnly), To: to.Format(time.DateOnly), Limit: 500}
		for {
			docs, err := u.Documents().Page(ctx, f)
			if err != nil {
				return err
			}
			for _, d := range docs {
				if d.Status == storage.DocumentDraft || d.Status == storage.DocumentReversed {
					continue
				}
				party := partyOf(d)
				if party == uuid.Nil {
					continue
				}
				_, value, ok, err := contactsummary.Moved(ctx, u, d.ID, ar, ap, base)
				if err != nil {
					return err
				}
				if !ok {
					continue
				}
				if err := each(row(party), d, value); err != nil {
					return err
				}
			}
			if len(docs) < f.Limit {
				return nil
			}
			last := docs[len(docs)-1]
			f.AfterDate, f.AfterID = last.Date, &last.ID
		}
	}
	for _, t := range []storage.DocumentType{storage.DocumentInvoice, storage.DocumentCreditNote} {
		err := walk(t, func(r *CustomerActivity, d storage.Document, value money.Money) error {
			if err := add(&r.Billed, value); err != nil {
				return err
			}
			straight, err := creditedTo(ctx, u, d.ID, ar, ap, revenue, base)
			if err != nil {
				return err
			}
			return add(&r.Direct, straight)
		})
		if err != nil {
			return nil, err
		}
	}
	err = walk(storage.DocumentCustomerReceipt, func(r *CustomerActivity, _ storage.Document, value money.Money) error {
		return add(&r.Collected, value.Neg())
	})
	if err != nil {
		return nil, err
	}
	otherMoved := map[uuid.UUID]money.Money{}
	for _, t := range activityOther {
		err := walk(t, func(r *CustomerActivity, _ storage.Document, value money.Money) error {
			m, ok := otherMoved[r.Party]
			if !ok {
				m = zero
			}
			var err error
			otherMoved[r.Party], err = m.Add(value)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	// The receivable's movement, from the sub-ledger at both ends.
	carrying := func(asAt time.Time) (map[uuid.UUID]money.Money, error) {
		out := map[uuid.UUID]money.Money{}
		for _, id := range arIDs {
			l, err := receivables.Open(ctx, u, base, id, asAt)
			if err != nil {
				return nil, err
			}
			for _, c := range l.Customers {
				m, ok := out[c.ID]
				if !ok {
					m = zero
				}
				for _, it := range c.Items {
					if m, err = m.Add(it.Carrying); err != nil {
						return nil, err
					}
				}
				out[c.ID] = m
			}
		}
		return out, nil
	}
	opening, err := carrying(from.AddDate(0, 0, -1))
	if err != nil {
		return nil, err
	}
	closing, err := carrying(to)
	if err != nil {
		return nil, err
	}
	for p := range opening {
		row(p)
	}
	for p := range closing {
		row(p)
	}
	for p := range released {
		row(p)
	}
	out := make([]CustomerActivity, 0, len(by))
	for p, r := range by {
		o, c := zero, zero
		if m, ok := opening[p]; ok {
			o = m
		}
		if m, ok := closing[p]; ok {
			c = m
		}
		if r.Movement, err = c.Sub(o); err != nil {
			return nil, err
		}
		billedLessCollected, err := r.Billed.Sub(r.Collected)
		if err != nil {
			return nil, err
		}
		if r.Other, err = r.Movement.Sub(billedLessCollected); err != nil {
			return nil, err
		}
		explained := zero
		if m, ok := otherMoved[p]; ok {
			explained = m
		}
		if r.Unexplained, err = r.Other.Sub(explained); err != nil {
			return nil, err
		}
		if rel, ok := released[p]; ok {
			r.Recognised, err = r.Direct.Add(rel)
		} else {
			r.Recognised = r.Direct
		}
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, nil
}

// creditedTo is what a document's current posting credited to the revenue accounts, in the base currency (a debit, as a credit note's, is negative).
func creditedTo(ctx context.Context, u storage.UnitOfWork, doc uuid.UUID, ar, ap, revenue map[uuid.UUID]bool, base money.Currency) (money.Money, error) {
	zero := money.Zero(base)
	e, ok, err := contactsummary.Current(ctx, u, doc, ar, ap)
	if err != nil || !ok {
		return zero, err
	}
	sum := zero
	for _, l := range e.Lines {
		if !revenue[l.AccountID] {
			continue
		}
		v, err := money.ParseStored(string(l.BaseAmount), base)
		if err != nil {
			return zero, err
		}
		if sum, err = sum.Sub(v); err != nil { // a credit is stored negative
			return zero, err
		}
	}
	return sum, nil
}
