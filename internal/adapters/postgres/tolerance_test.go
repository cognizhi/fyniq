//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/receipt"
	"github.com/cognizhi/fyniq/internal/domain/receivables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-BNK-08 and the posting rules "Settlement tolerances" and "Settlement difference on an invoice or bill" (p1-sales task
// 2.2; open question 32's defaults), worked by hand:
//   - USD 1,000.00 at 1.34 (S$1,340.00) received USD 990.00 in full at 1.36: the USD 10.00 left carries S$13.40, within
//     the greater of S$1.00 and 3% of S$1,340.00 (S$40.20): it clears to 7030 and the allocation records S$13.40;
//   - S$1,000.00 received S$998.00 in full: S$2.00 is above the base tolerance of S$1.00, refused; S$999.50 in full clears
//     S$0.50;
//   - four more USD invoices of 1,500.00 at 1.34 (S$2,010.00) each received USD 37.00 short, S$49.58 a time, below the cap
//     of S$50.00 per document: three fit the month's S$200.00 with the S$13.90 already cleared (S$162.64), the fourth
//     would take it to S$212.22 and is refused;
//   - voiding a receipt in full gives its difference back to the month;
//   - the close sees the month's S$198.82 within the cap, and warns once the cap is lowered below it (C.2 #49).
//
// The AR sub-ledger agrees with 1100 throughout, and the replay reproduces every receipt.
// req: FR-BNK-08
// req: ledger/posting-rules Settlement tolerances
// req: ledger/posting-rules Settlement difference on an invoice or bill
func TestSettlementDifferencesWithinTolerance(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ar-specialist"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Tolerances Pte Ltd")
	invoices, receipts := journal.Invoices{Store: s}, journal.CustomerReceipts{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var dbs, wallet storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if dbs, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		if wallet, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1021", Name: "Wise USD", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "USD"}}); err != nil {
			return err
		}
		for day, rate := range map[int]string{14: "1.34", 21: "1.36"} {
			if _, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, day), Rate: storage.Decimal(rate),
				Quote: storage.Decimal(rate), QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	issue := func(customer uuid.UUID, cur, price string) storage.Document {
		t.Helper()
		doc, err := invoices.Create(ctx, b.Org, invoice.Draft{CustomerID: customer, Date: porttest.Day(10, 14), Currency: money.MustLookup(cur),
			Lines: []invoice.Line{invoiceLine(t, b, "1", price, "", "SR", cur)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := invoices.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	inFull := func(customer uuid.UUID, into storage.Account, cur money.Currency, id uuid.UUID, amount string) (journal.RecordedReceipt, error) {
		m, _ := money.Parse(amount, cur)
		return receipts.Record(ctx, b.Org, receipt.Draft{CustomerID: customer, Date: porttest.Day(10, 21), AccountID: into.ID, Currency: cur,
			Invoices: []receipt.Part{{InvoiceID: id, Amount: m, InFull: true}}, Unapplied: money.Zero(cur)})
	}

	globex := issue(b.Globex.ID, "USD", "1000.00")
	r, err := inFull(b.Globex.ID, wallet, usd, globex.ID, "990.00")
	if err != nil || legsOf(t, s, b.Org, r.Entry) != "1100 -1000 USD = -1340; 7030 13.4 SGD = 13.4; 1021 990 USD = 1346.4; 7030 -19.8 SGD = -19.8" ||
		string(r.Allocations[0].Tolerance) != "13.4000" || string(r.Allocations[0].Amount) != "1000.0000" {
		t.Fatalf("USD 990.00 in full: %s %+v %v", legsOf(t, s, b.Org, r.Entry), r.Allocations, err)
	}

	acme := issue(b.Customer.ID, "SGD", "1000.00")
	if _, err := inFull(b.Customer.ID, dbs, sgd, acme.ID, "998.00"); !hasReceiptCode(err, "invoices[1].in_full", receipt.CodeTolerance) {
		t.Errorf("S$2.00 short: %v", err)
	}
	half, err := inFull(b.Customer.ID, dbs, sgd, acme.ID, "999.50")
	if err != nil || string(half.Allocations[0].Tolerance) != "0.5000" {
		t.Fatalf("S$0.50 short: %+v %v", half.Allocations, err)
	}

	// The month's cap: S$13.90 so far; three differences of S$49.58 fit, the fourth does not.
	var last storage.Document
	for i := range 4 {
		last = issue(b.Globex.ID, "USD", "1500.00")
		_, err := inFull(b.Globex.ID, wallet, usd, last.ID, "1463.00")
		if i < 3 && err != nil {
			t.Fatalf("difference %d: %v", i+1, err)
		}
		if i == 3 && !hasReceiptCode(err, "invoices[1].in_full", receipt.CodeTolerance) {
			t.Fatalf("the fourth difference: %v", err)
		}
	}
	// Voiding the USD receipt in full gives its S$13.40 back to the month: S$149.24 used, and the fourth fits.
	if _, err := receipts.Void(ctx, b.Org, r.Receipt.ID, porttest.Day(10, 22), "Bounced"); err != nil {
		t.Fatal(err)
	}
	if _, err := inFull(b.Globex.ID, wallet, usd, last.ID, "1463.00"); err != nil {
		t.Errorf("after the void gave S$13.40 back: %v", err)
	}

	// The close: S$198.82 cleared this month is within the cap (C.2 #49); lowered to S$150.00 since, it warns.
	checks := func() map[string]closing.Check {
		t.Helper()
		cc, err := closing.Service{Store: s}.RunChecks(ctx, b.Org, b.Oct.ID)
		if err != nil {
			t.Fatal(err)
		}
		all, err := closing.Read(cc.Result)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]closing.Check{}
		for _, c := range all {
			out[c.Key] = c
		}
		return out
	}
	if c := checks()["tolerances"]; c.Severity != closing.OK || c.Detail != "198.82 SGD cleared to 7030" {
		t.Errorf("the tolerance check: %+v", c)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "bank.tolerance_cap_month", "200.00", "150.00", porttest.Day(10, 1), 1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if c := checks()["tolerances"]; c.Severity != closing.Warning || c.Detail != "198.82 SGD cleared to 7030 as settlement differences, above the cap of 150.00: review them" {
		t.Errorf("the tolerance check after the cap was lowered: %+v", c)
	}

	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		rec, _, err := receivables.Reconcile(ctx, u, sgd, b.AR.ID, porttest.Day(10, 31))
		if err != nil {
			return err
		}
		if rec.Unexplained() {
			t.Errorf("the AR sub-ledger: %+v", rec)
		}
		rep, err := journal.Replay(ctx, u)
		if err != nil {
			return err
		}
		if len(rep.Mismatches) > 0 {
			t.Errorf("the replay: %+v", rep.Mismatches)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
