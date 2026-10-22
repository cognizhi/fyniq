//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/fx"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// INV-07 for accounts payable (p1-purchases task 1.6; the owner's decision of 23 Oct 2026): the sub-ledger, read from the
// bills, their allocations and the payments' unapplied amounts, as at a day, equals 2010 in each currency and in
// carrying amounts. Worked by hand at 31 October:
//   - BILL-0001, S$1,090.00 on 14 Oct, paid S$400.00 on 20 Oct: S$690.00 open;
//   - BILL-0002, USD 1,090.00 at 1.34 (S$1,460.60) on 14 Oct, paid in full from the wallet at 1.30: nothing open;
//   - BILL-0003, S$100.00 on 15 Oct, paid S$125.00 on 20 Oct, S$25.00 unapplied: nothing open, a S$25.00 credit;
//   - BILL-0004, S$300.00 on 16 Oct, voided on 2 Nov: open at 31 October, gone at 30 November;
//   - a manual journal crediting 2010 with S$50.00 on 25 Oct, which no bill supports (⚠ at the close).
//
// SGD: 690.00 − 25.00 + 300.00 = 965.00 open, and 2010 owes 1,015.00 with the journal's 50.00. A forged allocation that
// settles a bill with no posting behind it leaves 2010 and the sub-ledger apart, which the close refuses (✖).
// req: INV-07
// req: FR-AP-02
// req: FR-CTL-05
func TestTheAPSubLedgerReconcilesWith2010(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Payables Pte Ltd")
	bills, payments := journal.Bills{Store: s}, journal.BillPayments{Store: s}
	sgd, usd := money.MustLookup("SGD"), money.MustLookup("USD")
	var current, wallet storage.Account
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if current, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "SGD"}}); err != nil {
			return err
		}
		if wallet, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1021", Name: "Wise USD", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true, Currency: "USD"}}); err != nil {
			return err
		}
		for day, rate := range map[int]string{14: "1.34", 20: "1.30"} {
			if _, err := fx.Enter(ctx, u, storage.NewFXRate{Kind: storage.FXRateDay, Currency: "USD", On: porttest.Day(10, day), Rate: storage.Decimal(rate),
				Quote: storage.Decimal(rate), QuoteForm: "base-per-unit"}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	post := func(day int, cur money.Currency, net string) storage.Document {
		t.Helper()
		doc, err := bills.Create(ctx, b.Org, bill.Draft{SupplierID: b.Supplier.ID, Date: porttest.Day(10, day), Currency: cur,
			Lines: []bill.Line{{AccountID: b.Rent.ID, Net: moneyOf(t, net, cur.Code), GSTCode: "TX", GST: money.Zero(cur)}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bills.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	pay := func(from storage.Account, cur money.Currency, id uuid.UUID, amount, unapplied string) {
		t.Helper()
		u, _ := money.Parse(unapplied, cur)
		if _, err := payments.Record(ctx, b.Org, billpayment.Draft{SupplierID: b.Supplier.ID, Date: porttest.Day(10, 20), AccountID: from.ID, Currency: cur,
			Bills: []billpayment.Part{{BillID: id, Amount: moneyOf(t, amount, cur.Code)}}, Unapplied: u}); err != nil {
			t.Fatal(err)
		}
	}
	rent := post(14, sgd, "1090.00")
	pay(current, sgd, rent.ID, "400.00", "0")
	cloud := post(14, usd, "1090.00")
	pay(wallet, usd, cloud.ID, "1090.00", "0")
	small := post(15, sgd, "100.00")
	pay(current, sgd, small.ID, "100.00", "25.00")
	gone := post(16, sgd, "300.00")
	if _, err := bills.Void(ctx, b.Org, gone.ID, porttest.Day(11, 2), "Entered twice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Ledger().Append(ctx, storage.NewEntry{PostingDate: porttest.Day(10, 25), PostingIdentity: "manual_journal:" + uuid.NewString() + ":v1:journal@1",
			RuleVersion: "journal@1", Memo: "Accrued legal fees", Source: storage.Source{Type: ledger.SourceManualJournal, ID: uuid.NewString(), Version: 1},
			Lines: []storage.NewLine{
				{AccountID: b.Rent.ID, Currency: "SGD", Amount: "50.00", FXRate: "1", FXRateSource: "base", BaseAmount: "50.00"},
				{AccountID: b.AP.ID, Currency: "SGD", Amount: "-50.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-50.00"},
			}})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	reconcile := func(asAt int, month time.Month) (payables.Reconciliation, payables.Ledger) {
		t.Helper()
		var r payables.Reconciliation
		var l payables.Ledger
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			r, l, err = payables.Reconcile(ctx, u, sgd, b.AP.ID, porttest.Day(month, asAt))
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
	var sgdLine payables.Currency
	for _, c := range r.Currencies {
		if c.Currency == sgd {
			sgdLine = c
		}
		if c.Currency == usd && (!c.Control.IsZero() || !c.SubLedger.IsZero()) {
			t.Errorf("USD at 31 October: %+v", c)
		}
	}
	if sgdLine.Control.String() != "1015.00" || sgdLine.SubLedger.String() != "965.00" || len(sgdLine.Explained) != 1 ||
		sgdLine.Explained[0].SourceType != ledger.SourceManualJournal || sgdLine.Explained[0].Amount.String() != "50.00" {
		t.Errorf("SGD at 31 October: %+v", sgdLine)
	}
	if r.Base.Control.String() != "1015.00" || r.Base.SubLedger.String() != "965.00" {
		t.Errorf("carrying amounts at 31 October: %+v", r.Base)
	}
	if len(l.Suppliers) != 1 || len(l.Suppliers[0].Items) != 3 || l.Suppliers[0].Name != "Atlassian Pty Ltd" {
		t.Fatalf("the open items at 31 October: %+v", l)
	}
	for _, it := range l.Suppliers[0].Items {
		want := map[uuid.UUID]string{rent.ID: "690.00", gone.ID: "300.00"}[it.DocumentID]
		if it.Kind == payables.KindCredit {
			want = "-25.00"
		}
		if it.Open.String() != want {
			t.Errorf("%s %s is open %s, want %s", it.Kind, it.Number, it.Open, want)
		}
	}
	if r, _ := reconcile(30, 11); r.Unexplained() || r.Base.SubLedger.String() != "665.00" {
		t.Errorf("30 November, after the void: %+v", r.Base)
	}

	// The close: ✔ the sub-ledger agrees, and ⚠ the journal's S$50.00 no bill supports.
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
	got := checks()
	if c := got["payables"]; c.Severity != closing.OK {
		t.Errorf("the sub-ledger check: %+v", c)
	}
	if c := got["payables-unsupported"]; c.Severity != closing.Warning || c.Detail != "2010 holds 50.00 SGD from manual journals: support them with the suppliers' statements, or enter the bills" {
		t.Errorf("the unsupported amounts: %+v", c)
	}

	// A forged allocation settles S$100.00 of BILL-0001 with nothing posted: the close refuses it.
	var paymentID uuid.UUID
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		allocs, err := u.Allocations().OfDocument(ctx, rent.ID)
		if err == nil {
			paymentID = allocs[0].SourceID
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := asOrg(ctx, conn(t, "fyniq_migrator"), b.Org, func(tx pgx.Tx) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO fyniq.allocation (source_id, source_version, document_id, on_date, currency, minor_units, amount,
  base_currency, base_minor_units, base_amount, realised_fx, written_by, written_at)
VALUES ($1, 1, $2, '2026-10-21', 'SGD', 2, 100.00, 'SGD', 2, 100.00, 0, 'user:attack-test', now()) RETURNING id`, paymentID, rent.ID).Scan(&id); err != nil {
			return err
		}
		return auditRaw(ctx, tx, "allocation.created", "allocation", id)
	}); err != nil {
		t.Fatal(err)
	}
	if c := checks()["payables"]; c.Severity != closing.Failed ||
		c.Detail != "in SGD, 2010 says 1015.00 is owed and the open bills and credits 865.00; in carrying amounts (SGD), 2010 says 1015.00 and the open bills and credits 865.00: a posting and its document disagree; report it, never adjust it by journal" {
		t.Errorf("after the forged allocation: %+v", c)
	}
}

// FR-AP-03 (p1-purchases task 4.1): a supplier's credit note is entered as a bill is, numbered VC- when it posts, and
// posts the bill's legs reversed: S$200.00 net and S$18.00 of GST from a registered company, Dr 2010 218.00, Cr the
// expense 200.00, Cr 2200 18.00. The AP sub-ledger holds it as a credit with the supplier, and 2010 agrees; voided,
// it is gone; the replay reproduces it.
// req: FR-AP-03
// req: INV-07
func TestVendorCreditsPostAsABillReversed(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Credits Pte Ltd")
	bills := journal.Bills{Store: s}
	sgd := money.MustLookup("SGD")
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.GSTRegistrations().Create(ctx, storage.NewGSTRegistration{RegisteredFrom: porttest.Day(10, 1), GSTNumber: "202612345K",
			ReturnFrequency: "quarterly", QuarterStartMonth: 1, AccountingBasis: "invoice", RecordedAt: porttest.Day(10, 14)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	doc, err := bills.CreateCredit(ctx, b.Org, bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: "CN-12", Date: porttest.Day(10, 20), Currency: sgd,
		Lines: []bill.Line{{AccountID: b.Rent.ID, Net: sgdOf(t, "200.00"), GSTCode: "TX", GST: sgdOf(t, "18.00")}}})
	if err != nil {
		t.Fatal(err)
	}
	if c, err := bills.Check(ctx, b.Org, doc.ID, nil); err != nil || len(c.Problems) > 0 || c.Payable.String() != "-218.00" {
		t.Fatalf("the check: %+v %v", c, err)
	}
	e, err := bills.Post(ctx, b.Org, doc.ID)
	if err != nil || legsOf(t, s, b.Org, e) != "6100 -200 SGD = -200; 2200 -18 SGD = -18; 2010 218 SGD = 218" || e.RuleVersion != ledger.RuleVendorCredit {
		t.Fatalf("the credit: %s %v", legsOf(t, s, b.Org, e), err)
	}
	var r payables.Reconciliation
	var l payables.Ledger
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if doc, err = u.Documents().Get(ctx, doc.ID); err != nil {
			return err
		}
		r, l, err = payables.Reconcile(ctx, u, sgd, b.AP.ID, porttest.Day(10, 31))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if doc.Number != "VC-0001" || r.Unexplained() || len(l.Suppliers) != 1 || l.Suppliers[0].Items[0].Kind != payables.KindCredit ||
		l.Suppliers[0].Items[0].Open.String() != "-218.00" || r.Base.Control.String() != "-218.00" {
		t.Fatalf("the credit in the sub-ledger: %s %+v %+v", doc.Number, r, l)
	}
	if _, err := bills.Void(ctx, b.Org, doc.ID, porttest.Day(10, 25), "Entered twice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if r, l, err = payables.Reconcile(ctx, u, sgd, b.AP.ID, porttest.Day(10, 31)); err != nil {
			return err
		}
		if len(l.Suppliers) != 0 || r.Unexplained() {
			t.Errorf("after the void: %+v %+v", r, l)
		}
		rep, err := journal.Replay(ctx, u)
		if err == nil && len(rep.Mismatches) > 0 {
			t.Errorf("the replay: %+v", rep.Mismatches)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
