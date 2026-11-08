//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/domain/balance"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/domain/retention"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The readers after the ledger's purge (AUD-10, INV-07, INV-08, ENG-46). Invoices issued in 2010 (one in USD, one voided
// the same year) and one in 2011 reconcile with receivables as at 30 June 2011 exactly as before the purge: each open
// item's carry-forward under its own invoice, the void found by its tombstone, each document's posting read through
// ByIdentity as its carry-forward. The balance service refuses a day before the boundary and a revision before the
// purge (BALANCE-PURGED), and the nightly verification of the balances passes.
// req: AUD-10
// req: INV-07
// req: INV-08
// req: ENG-46
func TestTheReadersAfterTheLedgerPurge(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Purged Receivables Pte Ltd")
	sgd := money.MustLookup("SGD")
	years := map[int]storage.Period{}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		o, err := u.Organisation(ctx)
		if err != nil {
			return err
		}
		start, end := ymd(2010, 1, 1), ymd(2010, 12, 31)
		o.Profile.BooksStart, o.Profile.FirstYearEnd, o.Profile.FYStartMonth = &start, &end, 1
		if _, err := u.UpdateOrganisation(ctx, o.Version, o.Profile); err != nil {
			return err
		}
		for _, a := range []storage.NewAccount{{Code: "2999", Name: "Suspense", Type: storage.Liability, Role: "suspense"},
			{Code: "3100", Name: "Retained earnings", Type: storage.Equity, Role: "retained-earnings"}} {
			if _, err := u.Accounts().Create(ctx, a); err != nil {
				return err
			}
		}
		for _, y := range []int{2010, 2011} {
			if years[y], err = u.Periods().Create(ctx, storage.NewPeriod{StartsOn: ymd(y, 1, 1), EndsOn: ymd(y, 12, 31)}); err != nil {
				return err
			}
		}
		_, err = fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: ymd(2010, 3, 1), Rate: "1.34", Quote: "1.34",
			QuoteForm: "base-per-unit"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	invoices := journal.Invoices{Store: s}
	issue := func(customer storage.Contact, on time.Time, cur, price string) storage.Document {
		t.Helper()
		doc, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: customer.ID, Date: on, Currency: money.MustLookup(cur),
			Lines: []invoice.Line{invoiceLine(t, b, "1", price, "", "SR", cur)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoices.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	acme := issue(b.Customer, ymd(2010, 3, 1), "SGD", "1000.00")
	issue(b.Globex, ymd(2010, 3, 1), "USD", "1000.00")
	gone := issue(b.Customer, ymd(2010, 4, 1), "SGD", "300.00")
	if _, err := invoices.Void(ctx, b.Org, gone.ID, ymd(2010, 5, 1), "Raised twice"); err != nil {
		t.Fatal(err)
	}
	issue(b.Customer, ymd(2011, 2, 1), "SGD", "50.00")
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Periods().Close(ctx, years[2010].ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	asAt := ymd(2011, 6, 30)
	reconcile := func() string {
		t.Helper()
		var r receivables.Reconciliation
		var l receivables.Ledger
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, l, err = receivables.Reconcile(ctx, u, sgd, b.AR.ID, asAt)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if r.Unexplained() {
			t.Errorf("receivables do not reconcile: %+v", r)
		}
		raw, _ := json.Marshal(map[string]any{"reconciliation": r, "ledger": l})
		return string(raw)
	}
	before := reconcile()
	switchPurgeOn(t, b.Org)
	blobs, signer := anchoring(t)
	got, err := retention.PurgeLedger(context.Background(), store(t, "fyniq_worker"), blobs, signer, ymd(2026, 11, 9), b.Org)
	if err != nil || !got.Boundary.Equal(ymd(2011, 1, 1)) {
		t.Fatalf("the purge: %+v %v", got, err)
	}
	if after := reconcile(); after != before {
		t.Errorf("receivables changed:\n before %s\n after  %s", before, after)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		e, err := u.Ledger().ByIdentity(ctx, ledger.InvoiceIdentity(acme.ID, acme.Version, ledger.RuleInvoice))
		if err != nil || e.Carries == nil || e.RuleVersion != retention.RuleVersion || e.Source.Type != "invoice" || e.Source.ID != acme.ID.String() {
			t.Errorf("an open invoice's posting: %+v %v", e, err)
		}
		purged, err := u.Ledger().PurgedOfSource(ctx, gone.ID)
		if err != nil || len(purged) != 2 || purged[1].ReversesEntryID == nil {
			t.Errorf("a voided invoice's purged postings: %+v %v", purged, err)
		}
		var p *balance.Purged
		if _, err := balance.GeneralLedger(ctx, u, sgd, b.AR.ID, ymd(2010, 12, 1), ymd(2010, 12, 31)); !errors.As(err, &p) || !p.Boundary.Equal(ymd(2011, 1, 1)) {
			t.Errorf("a ledger before the boundary: %v", err)
		}
		if _, err := balance.TrialAt(ctx, u, sgd, balance.At(3), years[2011].ID); !errors.As(err, &p) || p.Revision != 3 {
			t.Errorf("a revision before the purge: %v", err)
		}
		if _, err := balance.Trial(ctx, u, sgd, years[2010].ID); !errors.As(err, &p) {
			t.Errorf("a purged period's trial balance: %v", err)
		}
		if _, err := balance.Trial(ctx, u, sgd, years[2011].ID); err != nil {
			t.Errorf("the year after the boundary: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	vs, err := balance.VerifyAll(context.Background(), store(t, "fyniq_worker"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if v.Organisation == b.Org && v.Differences != 0 {
			t.Errorf("the nightly verification of the balances: %+v", v)
		}
	}
}
