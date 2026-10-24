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
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The kinds of open items.
const (
	KindBill   = "bill"
	KindCredit = subledger.KindCredit // a supplier credit: a vendor credit (FR-AP-03), or a payment's unapplied amount (FR-AR-18 mirrored)
)

// Item is one open document of the sub-ledger as at a day: what is owed on it in its currency (a credit negative) and
// its carrying amount in the base currency.
type Item = subledger.Item

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
	err := subledger.Pages(ctx, u, storage.DocumentBill, asAt, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		identity := ledger.BillIdentity(doc.ID, doc.Version, ledger.RuleBill)
		if gone, err := subledger.VoidedBy(ctx, u, identity, asAt); err != nil || gone {
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
		credited, carried, err := subledger.ControlLines(e, ap, d.Currency, base) // its total, less any tax withheld (FR-AP-19)
		if err != nil {
			return err
		}
		allocs, err := u.Allocations().OfDocument(ctx, doc.ID)
		if err != nil {
			return err
		}
		paid, paidBase, err := subledger.Settled(allocs, d.Currency, base, asAt, nil)
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
		if it.Open, err = credited.Neg().Sub(paid); err != nil {
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
	err = subledger.Pages(ctx, u, storage.DocumentVendorCredit, asAt, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		identity := ledger.VendorCreditIdentity(doc.ID, doc.Version, ledger.RuleVendorCredit)
		if gone, err := subledger.VoidedBy(ctx, u, identity, asAt); err != nil || gone {
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
		_, debited, err := subledger.ControlLines(e, ap, d.Currency, base)
		if err != nil {
			return err
		}
		allocs, err := u.Allocations().OfDocument(ctx, doc.ID) // its applications to bills (FR-AP-03)
		if err != nil {
			return err
		}
		applied, appliedBase, err := subledger.Settled(allocs, d.Currency, base, asAt, nil)
		if err != nil {
			return err
		}
		it := Item{Kind: KindCredit, DocumentID: doc.ID, Number: doc.Number, Date: d.Date, Total: total}
		if it.Open, err = applied.Sub(total); err != nil {
			return err
		}
		if it.Carrying, err = appliedBase.Sub(debited); err != nil {
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
	err = subledger.Pages(ctx, u, storage.DocumentBillPayment, asAt, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		identity := ledger.BillPaymentIdentity(doc.ID, doc.Version, ledger.RuleBillPayment)
		if gone, err := subledger.VoidedBy(ctx, u, identity, asAt); err != nil || gone {
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
		_, debited, err := subledger.ControlLines(e, ap, d.Currency, base)
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
		later, err := u.Allocations().OfDocument(ctx, doc.ID) // the applications of its unapplied amount to bills (FR-AR-18)
		if err != nil {
			return err
		}
		used, usedBase, err := subledger.Settled(later, d.Currency, base, asAt, nil)
		if err != nil {
			return err
		}
		it := Item{Kind: KindCredit, DocumentID: doc.ID, Number: doc.Number, Date: d.Date, Total: total}
		if it.Open, err = used.Sub(d.Unapplied); err != nil {
			return err
		}
		if it.Carrying, err = debited.Sub(appliedBase); err != nil {
			return err
		}
		if it.Carrying, err = usedBase.Sub(it.Carrying); err != nil {
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

// Documents are the AP sub-ledger's open documents at a day in currencies other than the base, as the month-end
// revaluation revalues them one by one (FR-GL-04; rev 0.92): each bill and supplier credit with what is open of it,
// as the accounts-payable account holds it (a credit to it negative, as owed is), and its carrying amount at its stored
// rate. Its signature is fx.SubLedger's.
func Documents(ctx context.Context, u storage.UnitOfWork, base money.Currency, ap uuid.UUID, asAt time.Time) ([]fx.Document, error) {
	l, err := Open(ctx, u, base, ap, asAt)
	if err != nil {
		return nil, err
	}
	var out []fx.Document
	for _, s := range l.Suppliers {
		for _, it := range s.Items {
			if it.Open.Currency() == base {
				continue
			}
			// The account is a liability: what is owed is a credit, as the balance service gives it.
			out = append(out, fx.Document{ID: it.DocumentID, Number: it.Number, Open: it.Open.Neg(), Carrying: it.Carrying.Neg()})
		}
	}
	return out, nil
}

// SubLedgers are the sub-ledgers the month-end revaluation revalues document by document: accounts payable's.
func SubLedgers() fx.SubLedgers {
	return fx.SubLedgers{storage.AccountRole(ledger.RoleAccountsPayable): Documents}
}

// Reminder is a bill to pay soon (FR-AP-13; rev 0.93): its supplier, the open item, its planned payment date if any,
// and why it is listed.
type Reminder struct {
	Supplier  Supplier
	Item      Item
	PlannedOn *time.Time
	Why       string // overdue, due or planned
}

// The reasons a bill is to pay.
const (
	WhyOverdue = "overdue"
	WhyDue     = "due"     // it falls due within the days
	WhyPlanned = "planned" // its planned payment date is within the days
)

// Reminders are the bills to pay as at today (FR-AP-13): every posted bill with something open that is overdue, falls
// due within days, or is planned to be paid within them, earliest first; a planned date that has passed counts as due.
func Reminders(ctx context.Context, u storage.UnitOfWork, base money.Currency, ap uuid.UUID, today time.Time, days int) ([]Reminder, error) {
	l, err := Open(ctx, u, base, ap, today)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for _, s := range l.Suppliers {
		for _, it := range s.Items {
			if it.Kind == KindBill {
				ids = append(ids, it.DocumentID)
			}
		}
	}
	plans, err := u.BillPlans().Of(ctx, ids)
	if err != nil {
		return nil, err
	}
	until := today.AddDate(0, 0, days)
	var out []Reminder
	for _, s := range l.Suppliers {
		for _, it := range s.Items {
			if it.Kind != KindBill || it.Open.Sign() <= 0 {
				continue
			}
			r := Reminder{Supplier: Supplier{ID: s.ID, Name: s.Name}, Item: it, PlannedOn: plans[it.DocumentID].PlannedOn}
			switch {
			case !it.DueDate.IsZero() && it.DueDate.Before(today):
				r.Why = WhyOverdue
			case r.PlannedOn != nil && !r.PlannedOn.After(until):
				r.Why = WhyPlanned
			case !it.DueDate.IsZero() && !it.DueDate.After(until):
				r.Why = WhyDue
			default:
				continue
			}
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b Reminder) int { return a.when().Compare(b.when()) })
	return out, nil
}

// when is the reminder's date: the planned date when it has one, else the due date.
func (r Reminder) when() time.Time {
	if r.PlannedOn != nil && r.Why != WhyOverdue {
		return *r.PlannedOn
	}
	return r.Item.DueDate
}

// Explained, Currency and Reconciliation are INV-07's reconciliation of accounts payable with its sub-ledger (package
// subledger), what is owed positive.
type (
	Explained      = subledger.Explained
	Currency       = subledger.Currency
	Reconciliation = subledger.Reconciliation
)

// groups are the sub-ledger's suppliers as subledger groups.
func (l Ledger) groups() []subledger.Group {
	out := make([]subledger.Group, 0, len(l.Suppliers))
	for _, s := range l.Suppliers {
		out = append(out, subledger.Group{ID: s.ID, Name: s.Name, Items: s.Items})
	}
	return out
}

// own says whether a source type is one of the documents the AP sub-ledger accounts for: bills and their payments,
// credits, applications and refunds.
func own(sourceType string) bool {
	switch sourceType {
	case ledger.SourceBill, ledger.SourceBillPayment, ledger.SourceVendorCredit, ledger.SourceCreditApplication, ledger.SourceSupplierRefund:
		return true
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
	r, err := subledger.Reconcile(base, asAt, totals, l.groups(), true, own)
	return r, l, err
}
