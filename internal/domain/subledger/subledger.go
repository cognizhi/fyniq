// Package subledger is what the AP and AR sub-ledgers share (INV-07; FR-AP-06, FR-AR-10): an open document's item as at
// a day, the walk over a type's documents, the ageing by days past due, and the reconciliation of the open documents
// with their control account from the balance service (ENG-04). Packages payables and receivables read their own
// documents into items; what follows from the items is the same for both.
package subledger

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// KindCredit is an open item that is a credit with the other party: what it is owed back, or has paid in advance.
const KindCredit = "credit"

// Item is one open document of a sub-ledger as at a day: what is open on it in its currency (a credit negative) and its
// carrying amount in the base currency, signed as the sub-ledger reads it (owed by or to the company, positive).
type Item struct {
	Kind       string
	DocumentID uuid.UUID
	Number     string
	Date       time.Time
	DueDate    time.Time // as entered, else from the payment terms; zero for a credit
	Total      money.Money
	Open       money.Money
	Carrying   money.Money
}

// Group is one party's open items: a supplier's or a customer's.
type Group struct {
	ID    uuid.UUID
	Name  string
	Items []Item
}

// Pages walks every document of a type dated on or before a day, a page at a time.
func Pages(ctx context.Context, u storage.UnitOfWork, t storage.DocumentType, asAt time.Time, each func(storage.Document) error) error {
	f := storage.DocumentFilter{Type: t, To: asAt.Format(time.DateOnly), Limit: 500}
	for {
		docs, err := u.Documents().Page(ctx, f)
		if err != nil {
			return err
		}
		for _, d := range docs {
			if err := each(d); err != nil {
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

// VoidedBy says whether the entry with identity was reversed on or before asAt.
func VoidedBy(ctx context.Context, u storage.UnitOfWork, identity string, asAt time.Time) (bool, error) {
	r, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(identity))
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !r.PostingDate.After(asAt), nil
}

// ControlLines sums an entry's lines on a control account: in the document's currency and in the base currency.
func ControlLines(e storage.Entry, account uuid.UUID, cur, base money.Currency) (money.Money, money.Money, error) {
	amount, b := money.Zero(cur), money.Zero(base)
	for _, l := range e.Lines {
		if l.AccountID != account {
			continue
		}
		if l.Currency == cur.Code {
			x, err := money.ParseStored(string(l.Amount), cur)
			if err != nil {
				return amount, b, err
			}
			if amount, err = amount.Add(x); err != nil {
				return amount, b, err
			}
		}
		y, err := money.ParseStored(string(l.BaseAmount), base)
		if err != nil {
			return amount, b, err
		}
		if b, err = b.Add(y); err != nil {
			return amount, b, err
		}
	}
	return amount, b, nil
}

// Settled is what allocations settled through asAt, in the document's currency and in the base currency; keep, when
// given, chooses the allocations counted.
func Settled(allocs []storage.Allocation, cur, base money.Currency, asAt time.Time, keep func(storage.Allocation) bool) (money.Money, money.Money, error) {
	amount, b := money.Zero(cur), money.Zero(base)
	for _, a := range allocs {
		if a.On.After(asAt) || (keep != nil && !keep(a)) {
			continue
		}
		x, err := money.ParseStored(string(a.Amount), cur)
		if err != nil {
			return amount, b, err
		}
		y, err := money.ParseStored(string(a.BaseAmount), base)
		if err != nil {
			return amount, b, err
		}
		if amount, err = amount.Add(x); err != nil {
			return amount, b, err
		}
		if b, err = b.Add(y); err != nil {
			return amount, b, err
		}
	}
	return amount, b, nil
}

// Explained is what a source other than the sub-ledger's own documents posted to the control account, signed as the
// sub-ledger reads it.
type Explained struct {
	SourceType string
	Amount     money.Money
	Base       money.Money
}

// Currency is the reconciliation of one currency: what the control account says is open, what the sub-ledger's open
// documents add up to, what other sources explain, and what is left unexplained, which is a fault.
type Currency struct {
	Currency    money.Currency
	Control     money.Money
	SubLedger   money.Money
	Explained   []Explained
	Unexplained money.Money
}

// Reconciliation is INV-07 for a control account as at a day: each currency's amounts, and the base currency's.
type Reconciliation struct {
	AsAt       time.Time
	Currencies []Currency
	Base       Currency // in the base currency: the carrying amounts against the account's base balance
}

// Unexplained says whether any amount, in a currency or in the base currency, is left unexplained.
func (r Reconciliation) Unexplained() bool {
	if !r.Base.Unexplained.IsZero() {
		return true
	}
	for _, c := range r.Currencies {
		if !c.Unexplained.IsZero() {
			return true
		}
	}
	return false
}

// Reconcile is INV-07 as at a day (FR-CTL-05): the open documents of groups against the control account's balance by
// source type (totals, from balance.Control), per currency and in the base currency. credit is true for an account whose
// balance is a credit (accounts payable), whose totals are negated to read as owed. What the sub-ledger's own sources
// posted (own) is what its documents account for; what other sources posted is named; anything else is unexplained.
func Reconcile(base money.Currency, asAt time.Time, totals []balance.SourceTotal, groups []Group, credit bool, own func(sourceType string) bool) (Reconciliation, error) {
	out := Reconciliation{AsAt: asAt, Base: Currency{Currency: base, Control: money.Zero(base), SubLedger: money.Zero(base), Unexplained: money.Zero(base)}}
	byCode := map[string]*Currency{}
	currency := func(c money.Currency) *Currency {
		if x := byCode[c.Code]; x != nil {
			return x
		}
		x := &Currency{Currency: c, Control: money.Zero(c), SubLedger: money.Zero(c), Unexplained: money.Zero(c)}
		byCode[c.Code] = x
		return x
	}
	explainedBase := map[string]money.Money{}
	var err error
	for _, t := range totals {
		c := currency(t.Amount.Currency())
		amount, b := t.Amount, t.Base
		if credit {
			amount, b = amount.Neg(), b.Neg()
		}
		if c.Control, err = c.Control.Add(amount); err != nil {
			return out, err
		}
		if out.Base.Control, err = out.Base.Control.Add(b); err != nil {
			return out, err
		}
		if own(t.SourceType) {
			continue
		}
		c.Explained = append(c.Explained, Explained{SourceType: t.SourceType, Amount: amount, Base: b})
		was, ok := explainedBase[t.SourceType]
		if !ok {
			was = money.Zero(base)
		}
		if explainedBase[t.SourceType], err = was.Add(b); err != nil {
			return out, err
		}
	}
	for _, g := range groups {
		for _, it := range g.Items {
			c := currency(it.Open.Currency())
			if c.SubLedger, err = c.SubLedger.Add(it.Open); err != nil {
				return out, err
			}
			if out.Base.SubLedger, err = out.Base.SubLedger.Add(it.Carrying); err != nil {
				return out, err
			}
		}
	}
	codes := make([]string, 0, len(byCode))
	for code := range byCode {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	left := func(c *Currency, explained []Explained, amount func(Explained) money.Money) error {
		x, err := c.Control.Sub(c.SubLedger)
		if err != nil {
			return err
		}
		for _, e := range explained {
			if x, err = x.Sub(amount(e)); err != nil {
				return err
			}
		}
		c.Unexplained = x
		return nil
	}
	for _, code := range codes {
		c := byCode[code]
		if err := left(c, c.Explained, func(e Explained) money.Money { return e.Amount }); err != nil {
			return out, err
		}
		out.Currencies = append(out.Currencies, *c)
	}
	sources := make([]string, 0, len(explainedBase))
	for s := range explainedBase {
		sources = append(sources, s)
	}
	slices.Sort(sources)
	for _, s := range sources {
		out.Base.Explained = append(out.Base.Explained, Explained{SourceType: s, Amount: explainedBase[s], Base: explainedBase[s]})
	}
	if err := left(&out.Base, out.Base.Explained, func(e Explained) money.Money { return e.Base }); err != nil {
		return out, err
	}
	return out, nil
}
