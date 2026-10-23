//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-GL-04 for accounts payable (rev 0.92; p1-purchases task 10.2, p1-multi-currency-fx task 3.6's AP half), worked by
// hand: each open bill in a foreign currency is revalued against its own stored rate, one line each on 2010 naming it,
// and what 2010 holds beyond its bills is an item of its own. USD 1,000.37 at 1.3456 (S$1,346.10) and USD 200.01 at
// 1.3501 (S$270.03), at the 31 October rate 1.3333, are S$1,333.79 and S$266.67: gains of 12.31 and 3.36, where the
// account as a whole would have gained 15.66; a manual USD 100.00 at 1.35 on 2010 gains 1.67; 7040 takes 17.34. A bill
// back-dated into October afterwards makes the month stale, and the next run posts only its change, USD 50.00 at 1.34
// (S$67.00) to S$66.67: 0.33. The AP sub-ledger still reconciles, and the replay reproduces every run.
// req: FR-GL-04
// req: INV-07
// req: G4-039
func TestBillsAreRevaluedOneByOne(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := porttest.NewBooks(t, s, "AP revaluation Pte Ltd")
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var ap storage.Account
	var supplier storage.Contact
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if ap, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "2010", Name: "Accounts payable", Type: storage.Liability,
			Role: storage.AccountRole(ledger.RoleAccountsPayable), AccountAttributes: storage.AccountAttributes{Revalue: true}}); err != nil {
			return err
		}
		for _, a := range []storage.NewAccount{
			{Code: "2200", Name: "GST control", Type: storage.Liability, Role: storage.AccountRole(ledger.RoleGSTControl)},
			{Code: "7030", Name: "FX & rounding differences", Type: storage.Expense, Role: storage.AccountRole(ledger.RoleFXRounding)},
			{Code: "7040", Name: "Unrealised FX gain or loss", Type: storage.Revenue, Role: storage.RoleUnrealisedFX},
		} {
			if _, err = u.Accounts().Create(ctx, a); err != nil {
				return err
			}
		}
		for _, r := range []struct {
			day  int
			rate string
		}{{14, "1.3456"}, {20, "1.3501"}, {31, "1.3333"}} {
			if _, err = fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, r.day), Rate: storage.Decimal(r.rate), Quote: storage.Decimal(r.rate),
				QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		supplier, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Vercel Inc", Vendor: true, Currency: "USD"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	bills := journal.Bills{Store: s}
	post := func(ref string, day int, net string, rate *money.Rate) storage.Document {
		t.Helper()
		doc, err := bills.Create(ctx, b.Org, bill.Draft{SupplierID: supplier.ID, SupplierRef: ref, Date: porttest.Day(10, day), Currency: usd, Rate: rate,
			Lines: []bill.Line{{AccountID: b.Rent.ID, Net: moneyOf(t, net, "USD"), GSTCode: "NR", GST: money.Zero(usd)}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bills.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	post("INV-1", 14, "1000.37", nil)
	post("INV-2", 20, "200.01", nil)
	// A manual USD line on 2010, which no bill accounts for: written straight to the ledger, so it has no document to replay.
	raw := uuid.NewString()
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		id := raw
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, 22), PostingIdentity: id, RuleVersion: "test@1",
			Source: storage.Source{Type: "manual_journal", ID: id, Version: 1}, Lines: []storage.NewLine{
				{AccountID: b.Rent.ID, Currency: "SGD", Amount: "135.00", FXRate: "1", FXRateSource: "base", BaseAmount: "135.00"},
				{AccountID: ap.ID, Currency: "USD", Amount: "-100.00", FXRate: "1.35", FXRateSource: "document", BaseAmount: "-135.00"},
			}})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	run := func() fx.Run {
		t.Helper()
		var r fx.Run
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, err = journal.PostRevaluation(ctx, u, b.Oct.ID, porttest.Day(11, 1))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if r.Entry == nil {
			t.Fatal("the run posted nothing")
		}
		return r
	}
	descriptions := func(e *storage.Entry) string {
		var out []string
		for _, l := range e.Lines {
			out = append(out, l.Description)
		}
		return strings.Join(out, " | ")
	}
	first := run()
	if got := legsOf(t, s, b.Org, *first.Entry); got != "2010 0 USD = 12.31; 2010 0 USD = 3.36; 2010 0 USD = 1.67; 7040 -17.34 SGD = -17.34" {
		t.Errorf("October's run: %s", got)
	}
	if d := descriptions(first.Entry); !strings.HasPrefix(d, "BILL-0001: -1000.37 USD revalued at") || !strings.Contains(d, "BILL-0002: -200.01 USD") ||
		!strings.Contains(d, "| -100.00 USD revalued at") {
		t.Errorf("the lines name their bills: %s", d)
	}

	// A bill back-dated into October: the next run posts only its change.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := money.ParseRate("1.34", usd, sgd, "document")
		if err != nil {
			return err
		}
		post("INV-3", 25, "50.00", &r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	second := run()
	if got := legsOf(t, s, b.Org, *second.Entry); got != "2010 0 USD = 0.33; 7040 -0.33 SGD = -0.33" {
		t.Errorf("the stale run: %s", got)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rec, _, err := payables.Reconcile(ctx, u, sgd, ap.ID, porttest.Day(10, 31))
		if err != nil {
			return err
		}
		if rec.Unexplained() {
			t.Errorf("the AP sub-ledger does not reconcile: %+v", rec)
		}
		r, err := journal.Replay(ctx, u)
		if err != nil {
			return err
		}
		for _, m := range r.Mismatches {
			if m.PostingIdentity != raw {
				t.Errorf("the replay: %+v", m)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
