// Package payables is the AP sub-ledger (FR-AP-02, INV-07): what the company owes each supplier as at a day, from the
// documents themselves (the posted bills, their allocations, and the payments' unapplied amounts kept as supplier
// credits), and its reconciliation with the accounts-payable account the balance service gives (ENG-04). Read from the
// documents rather than from 2010's lines, the sub-ledger shows when a posting and its document disagree; what 2010
// holds from other sources (opening balances, manual journals, the month-end revaluation) is named apart.
package payables

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The kinds of open items.
const (
	KindBill   = "bill"
	KindCredit = "credit" // a supplier credit: a vendor credit (FR-AP-03), or a payment's unapplied amount (FR-AR-18 mirrored)
)

// Item is one open document of the sub-ledger as at a day: what is owed on it in its currency (a credit negative) and
// its carrying amount in the base currency.
type Item struct {
	Kind       string
	DocumentID uuid.UUID
	Number     string
	Date       time.Time
	DueDate    time.Time // a bill's: as entered, else from the payment terms (bill.Due); zero for a credit
	Total      money.Money
	Open       money.Money
	Carrying   money.Money
}

// Supplier is a supplier's open items, earliest first.
type Supplier struct {
	ID    uuid.UUID
	Name  string
	Items []Item
}

// Ledger is the AP sub-ledger as at a day: the suppliers with something open, by name.
type Ledger struct {
	AsAt      time.Time
	Suppliers []Supplier
}

// pages walks every document of a type dated on or before a day, a page at a time.
func pages(ctx context.Context, u storage.UnitOfWork, t storage.DocumentType, asAt time.Time, each func(storage.Document) error) error {
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

// voidedBy says whether the entry with identity was reversed on or before asAt.
func voidedBy(ctx context.Context, u storage.UnitOfWork, identity string, asAt time.Time) (bool, error) {
	r, err := u.Ledger().ByIdentity(ctx, ledger.ReversalIdentity(identity))
	if errors.Is(err, ports.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !r.PostingDate.After(asAt), nil
}

// apLines sums an entry's lines on the accounts-payable account: in the document's currency and in the base currency.
func apLines(e storage.Entry, ap uuid.UUID, cur, base money.Currency) (money.Money, money.Money, error) {
	amount, b := money.Zero(cur), money.Zero(base)
	for _, l := range e.Lines {
		if l.AccountID != ap {
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

// settled is what allocations settled through asAt, in the document's currency and in the base currency.
func settled(allocs []storage.Allocation, cur, base money.Currency, asAt time.Time, keep func(storage.Allocation) bool) (money.Money, money.Money, error) {
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

// Open is the AP sub-ledger as at a day: each posted bill dated on or before it and not voided by then, less what was
// allocated to it by then; each posted vendor credit dated on or before it and not voided by then; and each payment dated
// on or before it and not voided by then that left an amount unapplied.
// ap is the accounts-payable account, whose lines carry a document's carrying amount.
func Open(ctx context.Context, u storage.UnitOfWork, base money.Currency, ap uuid.UUID, asAt time.Time) (Ledger, error) {
	out := Ledger{AsAt: asAt}
	bySupplier := map[uuid.UUID]*Supplier{}
	contacts := map[uuid.UUID]storage.Contact{}
	contact := func(id uuid.UUID) (storage.Contact, error) {
		if c, ok := contacts[id]; ok || id == uuid.Nil {
			return c, nil
		}
		c, err := u.Contacts().Get(ctx, id)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return c, err
		}
		contacts[id] = c
		return c, nil
	}
	add := func(supplier uuid.UUID, it Item) {
		s := bySupplier[supplier]
		if s == nil {
			s = &Supplier{ID: supplier}
			bySupplier[supplier] = s
		}
		s.Items = append(s.Items, it)
	}
	err := pages(ctx, u, storage.DocumentBill, asAt, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		identity := ledger.BillIdentity(doc.ID, doc.Version, ledger.RuleBill)
		if gone, err := voidedBy(ctx, u, identity, asAt); err != nil || gone {
			return err
		}
		d, err := bill.Read(doc, base)
		if err != nil {
			return err
		}
		total, err := d.Total()
		if err != nil {
			return err
		}
		e, err := u.Ledger().ByIdentity(ctx, identity)
		if err != nil {
			return err
		}
		_, carried, err := apLines(e, ap, d.Currency, base)
		if err != nil {
			return err
		}
		allocs, err := u.Allocations().OfDocument(ctx, doc.ID)
		if err != nil {
			return err
		}
		paid, paidBase, err := settled(allocs, d.Currency, base, asAt, nil)
		if err != nil {
			return err
		}
		supplier, err := contact(d.SupplierID)
		if err != nil {
			return err
		}
		due, err := bill.Due(ctx, u, d, supplier)
		if err != nil {
			return err
		}
		it := Item{Kind: KindBill, DocumentID: doc.ID, Number: doc.Number, Date: d.Date, DueDate: due, Total: total}
		if it.Open, err = total.Sub(paid); err != nil {
			return err
		}
		if it.Carrying, err = carried.Neg().Sub(paidBase); err != nil {
			return err
		}
		if !it.Open.IsZero() || !it.Carrying.IsZero() {
			add(d.SupplierID, it)
		}
		return nil
	})
	if err != nil {
		return Ledger{}, err
	}
	err = pages(ctx, u, storage.DocumentVendorCredit, asAt, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		identity := ledger.VendorCreditIdentity(doc.ID, doc.Version, ledger.RuleVendorCredit)
		if gone, err := voidedBy(ctx, u, identity, asAt); err != nil || gone {
			return err
		}
		d, err := bill.Read(doc, base)
		if err != nil {
			return err
		}
		total, err := d.Total()
		if err != nil {
			return err
		}
		e, err := u.Ledger().ByIdentity(ctx, identity)
		if err != nil {
			return err
		}
		_, debited, err := apLines(e, ap, d.Currency, base)
		if err != nil {
			return err
		}
		add(d.SupplierID, Item{Kind: KindCredit, DocumentID: doc.ID, Number: doc.Number, Date: d.Date, Total: total, Open: total.Neg(), Carrying: debited.Neg()})
		return nil
	})
	if err != nil {
		return Ledger{}, err
	}
	err = pages(ctx, u, storage.DocumentBillPayment, asAt, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		identity := ledger.BillPaymentIdentity(doc.ID, doc.Version, ledger.RuleBillPayment)
		if gone, err := voidedBy(ctx, u, identity, asAt); err != nil || gone {
			return err
		}
		d, err := billpayment.Read(doc, base)
		if err != nil {
			return err
		}
		if d.Unapplied.IsZero() {
			return nil
		}
		e, err := u.Ledger().ByIdentity(ctx, identity)
		if err != nil {
			return err
		}
		_, debited, err := apLines(e, ap, d.Currency, base)
		if err != nil {
			return err
		}
		allocs, err := u.Allocations().OfSource(ctx, doc.ID)
		if err != nil {
			return err
		}
		_, appliedBase, err := settled(allocs, d.Currency, base, d.Date, func(a storage.Allocation) bool { return a.ReversesID == nil && a.SourceVersion == doc.Version })
		if err != nil {
			return err
		}
		total, err := d.Total()
		if err != nil {
			return err
		}
		it := Item{Kind: KindCredit, DocumentID: doc.ID, Number: doc.Number, Date: d.Date, Total: total, Open: d.Unapplied.Neg()}
		if it.Carrying, err = debited.Sub(appliedBase); err != nil {
			return err
		}
		it.Carrying = it.Carrying.Neg()
		add(d.SupplierID, it)
		return nil
	})
	if err != nil {
		return Ledger{}, err
	}
	for id, s := range bySupplier {
		c, err := contact(id)
		if err != nil {
			return Ledger{}, err
		}
		s.Name = c.LegalName
		slices.SortFunc(s.Items, func(a, b Item) int {
			if c := a.Date.Compare(b.Date); c != 0 {
				return c
			}
			return strings.Compare(a.Number, b.Number)
		})
		out.Suppliers = append(out.Suppliers, *s)
	}
	slices.SortFunc(out.Suppliers, func(a, b Supplier) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// Explained is what a source other than bills and their payments posted to the accounts-payable account, owed positive.
type Explained struct {
	SourceType string
	Amount     money.Money
	Base       money.Money
}

// Currency is the reconciliation of one currency: what the accounts-payable account says is owed, what the
// sub-ledger's open documents add up to, what other sources explain, and what is left unexplained, which is a fault.
type Currency struct {
	Currency    money.Currency
	Control     money.Money
	SubLedger   money.Money
	Explained   []Explained
	Unexplained money.Money
}

// Reconciliation is INV-07 for accounts payable as at a day: each currency's amounts, and the base currency's.
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

// Reconcile is INV-07 for accounts payable as at a day (FR-CTL-05): the sub-ledger's open documents against the
// account's balance from the balance service, per currency and in the base currency. What bills and their payments
// posted is what the sub-ledger accounts for; what other sources posted (opening balances, manual journals, the
// revaluation's base-only lines) is named; anything else is unexplained.
func Reconcile(ctx context.Context, u storage.UnitOfWork, base money.Currency, ap uuid.UUID, asAt time.Time) (Reconciliation, Ledger, error) {
	l, err := Open(ctx, u, base, ap, asAt)
	if err != nil {
		return Reconciliation{}, Ledger{}, err
	}
	totals, err := balance.Control(ctx, u, base, ap, asAt)
	if err != nil {
		return Reconciliation{}, Ledger{}, err
	}
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
	for _, t := range totals {
		c := currency(t.Amount.Currency())
		owed, owedBase := t.Amount.Neg(), t.Base.Neg()
		if c.Control, err = c.Control.Add(owed); err != nil {
			return out, l, err
		}
		if out.Base.Control, err = out.Base.Control.Add(owedBase); err != nil {
			return out, l, err
		}
		if t.SourceType == ledger.SourceBill || t.SourceType == ledger.SourceBillPayment || t.SourceType == ledger.SourceVendorCredit {
			continue
		}
		c.Explained = append(c.Explained, Explained{SourceType: t.SourceType, Amount: owed, Base: owedBase})
		was, ok := explainedBase[t.SourceType]
		if !ok {
			was = money.Zero(base)
		}
		if explainedBase[t.SourceType], err = was.Add(owedBase); err != nil {
			return out, l, err
		}
	}
	for _, s := range l.Suppliers {
		for _, it := range s.Items {
			c := currency(it.Open.Currency())
			if c.SubLedger, err = c.SubLedger.Add(it.Open); err != nil {
				return out, l, err
			}
			if out.Base.SubLedger, err = out.Base.SubLedger.Add(it.Carrying); err != nil {
				return out, l, err
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
			return out, l, err
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
		return out, l, err
	}
	return out, l, nil
}
