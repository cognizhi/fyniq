//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// INV-07 for accounts receivable and FR-AR-10 (p1-sales task 1.6): the sub-ledger, read from the issued invoices and
// their allocations as at a day, equals 1100 in each currency and in carrying amounts. Worked by hand at 31 October:
//   - INV-0001 to Acme, S$1,000.00 on 14 Oct, due on its 14 days' terms on 28 Oct;
//   - INV-0002 to Globex, USD 1,000.00 at the 14 October rate 1.34 (S$1,340.00), due on the organisation's 30 days on
//     13 Nov;
//   - INV-0003 to Acme, S$300.00 on 16 Oct, voided on 2 Nov: open at 31 October, gone at 30 November;
//   - a manual journal debiting 1100 with S$50.00 on 25 Oct, which no invoice supports (⚠ at the close).
//
// SGD: 1,000.00 + 300.00 = 1,300.00 open and 1100 says 1,350.00 with the journal's 50.00; USD 1,000.00 both. At 30
// November INV-0001 is 33 days past due (31–60) and INV-0002 17 (1–30). At the 31 October rate 1.36 the USD 1,000.00
// is worth S$1,360.00, an unrealised gain of S$20.00, and the month-end revaluation revalues INV-0002 on its own.
// req: INV-07
// req: FR-AR-10
// req: FR-CTL-05
// req: FR-GL-04
func TestTheARSubLedgerReconcilesWith1100(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Receivables Pte Ltd")
	invoices := journal.Invoices{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		if _, err := u.Accounts().Update(ctx, b.AR.ID, b.AR.Version, storage.AccountChange{Name: b.AR.Name, Active: true,
			AccountAttributes: storage.AccountAttributes{Revalue: true}}); err != nil {
			return err
		}
		for day, rate := range map[int]string{14: "1.34", 31: "1.36"} {
			if _, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, day), Rate: storage.Decimal(rate),
				Quote: storage.Decimal(rate), QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	issue := func(customer uuid.UUID, day int, cur, price string) storage.Document {
		t.Helper()
		doc, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: customer, Date: porttest.Day(10, day), Currency: money.MustLookup(cur),
			Lines: []invoice.Line{invoiceLine(t, b, "1", price, "", "SR", cur)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoices.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	acme := issue(b.Customer.ID, 14, "SGD", "1000.00")
	globex := issue(b.Globex.ID, 14, "USD", "1000.00")
	gone := issue(b.Customer.ID, 16, "SGD", "300.00")
	if _, err := invoices.Void(ctx, b.Org, gone.ID, porttest.Day(11, 2), "Raised twice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, 25), PostingIdentity: "manual_journal:" + uuid.NewString() + ":v1:journal@1",
			RuleVersion: "journal@1", Memo: "Accrued fee", Source: storage.Source{Type: ledger.SourceManualJournal, ID: uuid.NewString(), Version: 1},
			Lines: []storage.NewLine{
				{AccountID: b.AR.ID, Currency: "SGD", Amount: "50.00", FXRate: "1", FXRateSource: "base", BaseAmount: "50.00"},
				{AccountID: b.Sales.ID, Currency: "SGD", Amount: "-50.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-50.00"},
			}})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	reconcile := func(asAt int, month time.Month) (receivables.Reconciliation, receivables.Ledger) {
		t.Helper()
		var r receivables.Reconciliation
		var l receivables.Ledger
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, l, err = receivables.Reconcile(ctx, u, sgd, b.AR.ID, porttest.Day(month, asAt))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return r, l
	}
	r, l := reconcile(31, 10)
	if r.Unexplained() {
		t.Fatalf("31 October: %+v", r)
	}
	for _, c := range r.Currencies {
		switch c.Currency {
		case sgd:
			if c.Control.String() != "1350.00" || c.SubLedger.String() != "1300.00" || len(c.Explained) != 1 ||
				c.Explained[0].SourceType != ledger.SourceManualJournal || c.Explained[0].Amount.String() != "50.00" {
				t.Errorf("SGD at 31 October: %+v", c)
			}
		case usd:
			if c.Control.String() != "1000.00" || c.SubLedger.String() != "1000.00" {
				t.Errorf("USD at 31 October: %+v", c)
			}
		}
	}
	if r.Base.Control.String() != "2690.00" || r.Base.SubLedger.String() != "2640.00" {
		t.Errorf("carrying amounts at 31 October: %+v", r.Base)
	}
	if len(l.Customers) != 2 || l.Customers[0].Name != "Acme Analytics Pte Ltd" || len(l.Customers[0].Items) != 2 || l.Customers[1].Name != "Globex Inc" ||
		l.Customers[1].Items[0].DocumentID != globex.ID || l.Customers[1].Items[0].Carrying.String() != "1340.00" ||
		!l.Customers[1].Items[0].DueDate.Equal(porttest.Day(11, 13)) || !l.Customers[0].Items[0].DueDate.Equal(porttest.Day(10, 28)) {
		t.Fatalf("the open items at 31 October: %+v", l)
	}
	r, l = reconcile(30, 11)
	if r.Unexplained() || r.Base.SubLedger.String() != "2340.00" {
		t.Errorf("30 November, after the void: %+v", r.Base)
	}

	// The ageing at 30 November, and the exposure at 31 October.
	a, err := receivables.Age(l, sgd)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range a.Totals {
		want := map[string]string{"SGD": "0.00 0.00 1000.00 0.00 0.00 1000.00", "USD": "0.00 1000.00 0.00 0.00 0.00 1000.00"}[row.Currency.Code]
		if got := row.Buckets[0].String() + " " + row.Buckets[1].String() + " " + row.Buckets[2].String() + " " + row.Buckets[3].String() + " " +
			row.Buckets[4].String() + " " + row.Total.String(); got != want {
			t.Errorf("the %s ageing at 30 November: %s, want %s", row.Currency, got, want)
		}
	}
	_, l = reconcile(31, 10)
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		a, err := receivables.Age(l, sgd)
		if err != nil {
			return err
		}
		xs, err := receivables.Exposures(ctx, u, sgd, a)
		if err != nil {
			return err
		}
		if len(xs) != 1 || xs[0].Currency != usd || xs[0].Rate.String() != "1.36" || xs[0].AtRate.String() != "1360.00" || xs[0].Unrealised.String() != "20.00" {
			t.Errorf("the exposure at 31 October: %+v", xs)
		}
		// The month-end revaluation revalues the open invoice on its own (FR-GL-04).
		plan, err := fx.Prepare(ctx, u, b.Oct.ID, journal.SubLedgers())
		if err != nil {
			return err
		}
		var found bool
		for _, it := range plan.Items {
			if it.Document != nil && *it.Document == globex.ID {
				found = it.Change.String() == "20.00"
			}
		}
		if !found {
			t.Errorf("the revaluation does not revalue INV-0002 alone by 20.00: %+v", plan.Items)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The close: ✔ the sub-ledger agrees, and ⚠ the journal's S$50.00 no invoice supports.
	cc, err := closing.Service{Store: s}.RunChecks(ctx, b.Org, b.Oct.ID)
	if err != nil {
		t.Fatal(err)
	}
	all, err := closing.Read(cc.Result)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]closing.Check{}
	for _, c := range all {
		got[c.Key] = c
	}
	if c := got["receivables"]; c.Severity != closing.OK || c.Detail != "3 open invoice(s) and customer credit(s)" {
		t.Errorf("the sub-ledger check: %+v", c)
	}
	if c := got["receivables-unsupported"]; c.Severity != closing.Warning ||
		c.Detail != "1100 holds 50.00 SGD from manual journals: support them with the customers' statements, or enter the invoices" {
		t.Errorf("the unsupported amounts: %+v", c)
	}
	_ = acme
}
