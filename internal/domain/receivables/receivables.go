// Package receivables is the AR sub-ledger (FR-AR-03, FR-AR-10, INV-07): what each customer owes the company as at a day,
// from the documents themselves (the issued invoices and what is allocated to them, the credit notes and receipts'
// unapplied amounts less what is applied of them; deposits join with p1-sales task 3.2), and its reconciliation with the accounts-receivable account
// the balance service gives (ENG-04). Read from the documents rather than from 1100's lines, the sub-ledger shows when
// a posting and its document disagree; what 1100 holds from other sources (opening balances, manual journals, the
// month-end revaluation) is named apart.
package receivables

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The kinds of open items.
const (
	KindInvoice = "invoice"
	KindCredit  = subledger.KindCredit // a customer credit: a credit note (FR-AR-04), or a receipt's unapplied amount (FR-AR-18)
)

// Item is one open document of the sub-ledger as at a day: what is owed on it in its currency (a credit negative) and
// its carrying amount in the base currency.
type Item = subledger.Item

// Customer is a customer's open items, earliest first.
type Customer struct {
	ID    uuid.UUID
	Name  string
	Items []Item
}

// Ledger is the AR sub-ledger as at a day: the customers with something open, by name.
type Ledger struct {
	AsAt      time.Time
	Customers []Customer
}

// groups are the sub-ledger's customers as subledger groups.
func (l Ledger) groups() []subledger.Group {
	out := make([]subledger.Group, 0, len(l.Customers))
	for _, c := range l.Customers {
		out = append(out, subledger.Group{ID: c.ID, Name: c.Name, Items: c.Items})
	}
	return out
}

// Open is the AR sub-ledger as at a day: each issued invoice dated on or before it and not voided by then, less what
// was allocated to it by then; each issued credit note dated on or before it and not voided by then, less what was
// applied or refunded of it by then, a credit with the customer; and each receipt dated on or before it and not voided
// by then that left an amount unapplied, a credit too. ar is the accounts-receivable account, whose lines carry a document's carrying
// amount.
func Open(ctx context.Context, u storage.UnitOfWork, base money.Currency, ar uuid.UUID, asAt time.Time) (Ledger, error) {
	out := Ledger{AsAt: asAt}
	byCustomer := map[uuid.UUID]*Customer{}
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
	add := func(customer uuid.UUID, it Item) {
		c := byCustomer[customer]
		if c == nil {
			c = &Customer{ID: customer}
			byCustomer[customer] = c
		}
		c.Items = append(c.Items, it)
	}
	err := subledger.Pages(ctx, u, storage.DocumentInvoice, asAt, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		identity := ledger.InvoiceIdentity(doc.ID, doc.Version, ledger.RuleInvoice)
		if gone, err := subledger.VoidedBy(ctx, u, identity, asAt); err != nil || gone {
			return err
		}
		d, err := invoice.Read(doc, base)
		if err != nil {
			return err
		}
		e, err := u.Ledger().ByIdentity(ctx, identity)
		if err != nil {
			return err
		}
		debited, carried, err := subledger.ControlLines(e, ar, d.Currency, base) // the invoice's total, as its entry debited it
		if err != nil {
			return err
		}
		allocs, err := u.Allocations().OfDocument(ctx, doc.ID)
		if err != nil {
			return err
		}
		received, receivedBase, err := subledger.Settled(allocs, d.Currency, base, asAt, nil)
		if err != nil {
			return err
		}
		customer, err := contact(d.CustomerID)
		if err != nil {
			return err
		}
		due, err := invoice.Due(ctx, u, d, customer)
		if err != nil {
			return err
		}
		it := Item{Kind: KindInvoice, DocumentID: doc.ID, Number: doc.Number, Date: d.Date, DueDate: due, Total: debited}
		if it.Open, err = debited.Sub(received); err != nil {
			return err
		}
		if it.Carrying, err = carried.Sub(receivedBase); err != nil {
			return err
		}
		if it.Open.IsZero() && it.Carrying.IsZero() {
			return nil
		}
		add(d.CustomerID, it)
		return nil
	})
	if err != nil {
		return Ledger{}, err
	}
	err = subledger.Pages(ctx, u, storage.DocumentCreditNote, asAt, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		identity := ledger.CreditNoteIdentity(doc.ID, doc.Version, ledger.RuleCreditNote)
		if gone, err := subledger.VoidedBy(ctx, u, identity, asAt); err != nil || gone {
			return err
		}
		d, err := invoice.Read(doc, base)
		if err != nil {
			return err
		}
		e, err := u.Ledger().ByIdentity(ctx, identity)
		if err != nil {
			return err
		}
		credited, creditedBase, err := subledger.ControlLines(e, ar, d.Currency, base) // its total, as its entry credited it
		if err != nil {
			return err
		}
		allocs, err := u.Allocations().OfDocument(ctx, doc.ID) // what is applied to invoices or refunded of it (FR-AR-18)
		if err != nil {
			return err
		}
		used, usedBase, err := subledger.Settled(allocs, d.Currency, base, asAt, nil)
		if err != nil {
			return err
		}
		it := Item{Kind: KindCredit, DocumentID: doc.ID, Number: doc.Number, Date: d.Date, Total: credited.Neg()}
		if it.Open, err = credited.Add(used); err != nil {
			return err
		}
		if it.Carrying, err = creditedBase.Add(usedBase); err != nil {
			return err
		}
		if !it.Open.IsZero() || !it.Carrying.IsZero() {
			add(d.CustomerID, it)
		}
		return nil
	})
	if err != nil {
		return Ledger{}, err
	}
	err = subledger.Pages(ctx, u, storage.DocumentCustomerReceipt, asAt, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		identity := ledger.CustomerReceiptIdentity(doc.ID, doc.Version, ledger.RuleCustomerReceipt)
		if gone, err := subledger.VoidedBy(ctx, u, identity, asAt); err != nil || gone {
			return err
		}
		d, err := receipt.Read(doc, base)
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
		_, creditedBase, err := subledger.ControlLines(e, ar, d.Currency, base)
		if err != nil {
			return err
		}
		allocs, err := u.Allocations().OfSource(ctx, doc.ID)
		if err != nil {
			return err
		}
		_, appliedBase, err := subledger.Settled(allocs, d.Currency, base, d.Date, func(a storage.Allocation) bool { return a.ReversesID == nil && a.SourceVersion == doc.Version })
		if err != nil {
			return err
		}
		total, err := d.Total()
		if err != nil {
			return err
		}
		later, err := u.Allocations().OfDocument(ctx, doc.ID) // the applications of its unapplied amount to invoices (FR-AR-18)
		if err != nil {
			return err
		}
		used, usedBase, err := subledger.Settled(later, d.Currency, base, asAt, nil)
		if err != nil {
			return err
		}
		// A credit with the customer, negative: what the receipt left unapplied less what is applied of it since; carried at
		// what its credit to 1100 holds beyond the invoices it settled.
		it := Item{Kind: KindCredit, DocumentID: doc.ID, Number: doc.Number, Date: d.Date, Total: total}
		if it.Open, err = used.Sub(d.Unapplied); err != nil {
			return err
		}
		held, err := creditedBase.Neg().Sub(appliedBase)
		if err != nil {
			return err
		}
		if it.Carrying, err = usedBase.Sub(held); err != nil {
			return err
		}
		if !it.Open.IsZero() || !it.Carrying.IsZero() {
			add(d.CustomerID, it)
		}
		return nil
	})
	if err != nil {
		return Ledger{}, err
	}
	for id, c := range byCustomer {
		x, err := contact(id)
		if err != nil {
			return Ledger{}, err
		}
		c.Name = x.LegalName
		slices.SortFunc(c.Items, func(a, b Item) int {
			if c := a.Date.Compare(b.Date); c != 0 {
				return c
			}
			return strings.Compare(a.Number, b.Number)
		})
		out.Customers = append(out.Customers, *c)
	}
	slices.SortFunc(out.Customers, func(a, b Customer) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})
	return out, nil
}

// Documents are the AR sub-ledger's open documents at a day in currencies other than the base, as the month-end
// revaluation revalues them one by one (FR-GL-04): each invoice with what is open of it, as the accounts-receivable
// account holds it (a debit, positive), and its carrying amount at its stored rate. Its signature is fx.SubLedger's.
func Documents(ctx context.Context, u storage.UnitOfWork, base money.Currency, ar uuid.UUID, asAt time.Time) ([]fx.Document, error) {
	l, err := Open(ctx, u, base, ar, asAt)
	if err != nil {
		return nil, err
	}
	var out []fx.Document
	for _, c := range l.Customers {
		for _, it := range c.Items {
			if it.Open.Currency() == base {
				continue
			}
			out = append(out, fx.Document{ID: it.DocumentID, Number: it.Number, Open: it.Open, Carrying: it.Carrying})
		}
	}
	return out, nil
}

// SubLedgers are the sub-ledgers of receivables the month-end revaluation revalues document by document: accounts
// receivable's.
func SubLedgers() fx.SubLedgers {
	return fx.SubLedgers{storage.AccountRole(ledger.RoleAccountsReceivable): Documents}
}

// Explained, Currency and Reconciliation are INV-07's reconciliation of accounts receivable with its sub-ledger (package
// subledger), what is owed to the company positive.
type (
	Explained      = subledger.Explained
	Currency       = subledger.Currency
	Reconciliation = subledger.Reconciliation
)

// own says whether a source type is one of the documents the AR sub-ledger accounts for: invoices, credit notes,
// receipts, write-offs and credits applied to invoices (and, as they arrive, refunds).
func own(sourceType string) bool {
	switch sourceType {
	case ledger.SourceInvoice, ledger.SourceCreditNote, ledger.SourceCustomerReceipt, ledger.SourceWriteOff, ledger.SourceCustomerCreditApplication:
		return true
	}
	return false
}

// Reconcile is INV-07 for accounts receivable as at a day (FR-CTL-05): the sub-ledger's open documents against the
// account's balance from the balance service, per currency and in the base currency. What invoices posted is what the
// sub-ledger accounts for; what other sources posted (opening balances, manual journals, the revaluation's base-only
// lines) is named; anything else is unexplained.
func Reconcile(ctx context.Context, u storage.UnitOfWork, base money.Currency, ar uuid.UUID, asAt time.Time) (Reconciliation, Ledger, error) {
	l, err := Open(ctx, u, base, ar, asAt)
	if err != nil {
		return Reconciliation{}, Ledger{}, err
	}
	totals, err := balance.Control(ctx, u, base, ar, asAt)
	if err != nil {
		return Reconciliation{}, Ledger{}, err
	}
	r, err := subledger.Reconcile(base, asAt, totals, l.groups(), false, own)
	return r, l, err
}
