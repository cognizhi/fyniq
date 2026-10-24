package deposit

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/subledger"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// KindDeposit is the kind of a deposit held in the deposits sub-ledger.
const KindDeposit = "deposit"

// Ledger is the deposits sub-ledger as at a day (FR-AR-09): what 2410 holds of each customer's deposits, by name.
type Ledger struct {
	AsAt      time.Time
	Customers []subledger.Group
}

// Held is the deposits sub-ledger as at a day: each posted deposit dated on or before it and not voided by then, with
// what 2410 holds of it in the base currency, at the rate it came in: its base amount less its GST, less the parts the
// invoices that deduct it and the refunds of it took by then (RND-DEPOSIT). account is the customer deposits account.
func Held(ctx context.Context, u storage.UnitOfWork, base money.Currency, account uuid.UUID, asAt time.Time) (Ledger, error) {
	out := Ledger{AsAt: asAt}
	byCustomer := map[uuid.UUID]*subledger.Group{}
	err := subledger.Pages(ctx, u, storage.DocumentCustomerDeposit, asAt, func(doc storage.Document) error {
		if doc.Status == storage.DocumentDraft {
			return nil
		}
		identity := ledger.CustomerDepositIdentity(doc.ID, doc.Version, ledger.RuleCustomerDeposit)
		if gone, err := subledger.VoidedBy(ctx, u, identity, asAt); err != nil || gone {
			return err
		}
		d, err := Read(doc, base)
		if err != nil {
			return err
		}
		e, err := u.Ledger().ByIdentity(ctx, identity)
		if err != nil {
			return err
		}
		_, credited, err := subledger.ControlLines(e, account, base, base) // what it put on 2410, a credit
		if err != nil {
			return err
		}
		held := credited.Neg()
		allocs, err := u.Allocations().OfDocument(ctx, doc.ID)
		if err != nil {
			return err
		}
		for _, a := range allocs {
			if a.On.After(asAt) {
				continue
			}
			b, err := money.ParseStored(string(a.BaseAmount), base)
			if err != nil {
				return err
			}
			t := money.Zero(base)
			if a.Tax != "" {
				if t, err = money.ParseStored(string(a.Tax), base); err != nil {
					return err
				}
			}
			part, _ := b.Sub(t) // what the use took off 2410
			if held, err = held.Sub(part); err != nil {
				return err
			}
		}
		if held.IsZero() {
			return nil
		}
		g := byCustomer[d.CustomerID]
		if g == nil {
			g = &subledger.Group{ID: d.CustomerID}
			byCustomer[d.CustomerID] = g
		}
		g.Items = append(g.Items, subledger.Item{Kind: KindDeposit, DocumentID: doc.ID, Number: doc.Number, Date: d.Date, Total: d.Amount, Open: held, Carrying: held})
		return nil
	})
	if err != nil {
		return Ledger{}, err
	}
	for id, g := range byCustomer {
		c, err := u.Contacts().Get(ctx, id)
		if err != nil && !errors.Is(err, ports.ErrNotFound) {
			return Ledger{}, err
		}
		g.Name = c.LegalName
		slices.SortFunc(g.Items, func(a, b subledger.Item) int {
			if c := a.Date.Compare(b.Date); c != 0 {
				return c
			}
			return strings.Compare(a.Number, b.Number)
		})
		out.Customers = append(out.Customers, *g)
	}
	slices.SortFunc(out.Customers, func(a, b subledger.Group) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.ID.String(), b.ID.String())
	})
	return out, nil
}

// own says whether a source type posts to 2410 for a deposit the sub-ledger accounts for: the deposit itself, the
// invoice that deducts it, and its refund.
func own(sourceType string) bool {
	switch sourceType {
	case ledger.SourceCustomerDeposit, ledger.SourceInvoice, ledger.SourceCustomerRefund:
		return true
	}
	return false
}

// Reconcile is the deposits sub-ledger against the customer deposits account's balance from the balance service as at a
// day (FR-AR-09, as INV-07 does for 1100): what the deposits hold against what 2410 holds, a liability positive. What
// deposits, invoices and refunds posted is what the sub-ledger accounts for; what other sources posted (opening balances,
// manual journals) is named; anything else is unexplained.
func Reconcile(ctx context.Context, u storage.UnitOfWork, base money.Currency, account uuid.UUID, asAt time.Time) (subledger.Reconciliation, Ledger, error) {
	l, err := Held(ctx, u, base, account, asAt)
	if err != nil {
		return subledger.Reconciliation{}, Ledger{}, err
	}
	totals, err := balance.Control(ctx, u, base, account, asAt)
	if err != nil {
		return subledger.Reconciliation{}, Ledger{}, err
	}
	r, err := subledger.Reconcile(base, asAt, totals, l.Customers, true, own)
	return r, l, err
}
