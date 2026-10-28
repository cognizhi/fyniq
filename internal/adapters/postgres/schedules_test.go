//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/closing"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/ledger"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// deferralsAt are the schedules and holding accounts as at a day.
func deferralsAt(t *testing.T, s storage.Store, org uuid.UUID, asAt time.Time) journal.Deferrals {
	t.Helper()
	var d journal.Deferrals
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		d, err = journal.DeferralsAt(ctx, u, asAt)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return d
}

// closeChecks are October's close checks by key.
func closeChecks(t *testing.T, ctx context.Context, s storage.Store, org, period uuid.UUID) map[string]closing.Check {
	t.Helper()
	cc, err := closing.Service{Store: s}.RunChecks(ctx, org, period)
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

// INV-07 for 1210 Prepayments (FR-GL-03; p1-general-ledger-close task 6.4). Worked by hand at 31 October:
//   - the Jira bill of 14 October puts 1,308.00 on 1210, spread over September to December at 327.00 a month;
//     September's and October's parts are released at once on its date: 654.00 recognised, 654.00 remaining;
//   - a bill of 15 October puts a domain renewal of 600.00 on 1210 itself, which no schedule releases (⚠);
//   - a manual journal debits 1210 with 50.00 on 25 October (⚠).
//
// 1210 holds 1,308.00 − 654.00 + 600.00 + 50.00 = 1,304.00: the schedules 654.00, the bill's own 600.00 and the
// journal's 50.00, so it ties. At 30 November, with the job not run, November's part waits and 1210 still ties. A
// release no schedule has, posted with the bill's ID, leaves 1210 apart from its documents, which the close refuses (✖).
// req: INV-07
// req: FR-GL-03
// req: ENG-05
func TestPrepaymentSchedulesReconcileWith1210(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newSpreadBooks(t, ctx, s, "Schedules Pte Ltd")
	bills := journal.Bills{Store: s, Clock: clock.NewFrozen(time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC))}
	jira, err := bills.Create(ctx, b.Org, spreadDraft(t, b, "AU-3001", "1200.00", "108.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, jira.ID); err != nil {
		t.Fatal(err)
	}
	sgd := money.MustLookup("SGD")
	domain, err := bills.Create(ctx, b.Org, bill.Draft{SupplierID: b.Supplier.ID, SupplierRef: "AU-3002", Date: porttest.Day(10, 15), Currency: sgd,
		Lines: []bill.Line{{AccountID: b.Prepaid.ID, Description: "Domain, two years ahead", Net: sgdOf(t, "600.00"), GSTCode: "TX", GST: money.Zero(sgd)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bills.Post(ctx, b.Org, domain.ID); err != nil {
		t.Fatal(err)
	}
	append_ := func(e storage.NewEntry) {
		t.Helper()
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Ledger().Append(ctx, e)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	append_(storage.NewEntry{PostingDate: porttest.Day(10, 25), PostingIdentity: "manual_journal:" + uuid.NewString() + ":v1:journal@1",
		RuleVersion: "journal@1", Memo: "Insurance paid ahead", Source: storage.Source{Type: ledger.SourceManualJournal, ID: uuid.NewString(), Version: 1},
		Lines: []storage.NewLine{
			{AccountID: b.Prepaid.ID, Currency: "SGD", Amount: "50.00", FXRate: "1", FXRateSource: "base", BaseAmount: "50.00"},
			{AccountID: b.Bank.ID, Currency: "SGD", Amount: "-50.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-50.00"},
		}})

	d := deferralsAt(t, s, b.Org, porttest.Day(10, 31))
	if len(d.Schedules) != 1 || len(d.Holdings) != 1 {
		t.Fatalf("31 October: %+v", d)
	}
	sc := d.Schedules[0]
	if sc.Document.ID != jira.ID || sc.Line != 1 || sc.Holding != ledger.RolePrepayments || sc.Total.String() != "1308.00" ||
		sc.Recognised.String() != "654.00" || sc.Remaining.String() != "654.00" || len(sc.Months) != 4 {
		t.Fatalf("the Jira schedule at 31 October: %+v", sc)
	}
	for i, want := range []string{journal.ReleasePosted, journal.ReleasePosted, journal.ReleaseProjected, journal.ReleaseProjected} {
		if m := sc.Months[i]; m.Status != want || m.Amount.String() != "327.00" {
			t.Errorf("month %d at 31 October: %+v, want %s", i+1, m, want)
		}
	}
	h := d.Holdings[0]
	r := h.Reconciliation.Base
	if h.Account.ID != b.Prepaid.ID || h.Open != 1 || h.Reconciliation.Unexplained() || r.Control.String() != "1304.00" || r.SubLedger.String() != "1254.00" ||
		len(r.Explained) != 1 || r.Explained[0].SourceType != ledger.SourceManualJournal || r.Explained[0].Amount.String() != "50.00" {
		t.Fatalf("1210 at 31 October: %+v", h)
	}
	if len(h.Direct) != 1 || h.Direct[0].DocumentID != domain.ID || h.Direct[0].Carrying.String() != "600.00" {
		t.Fatalf("the cost on 1210 itself: %+v", h.Direct)
	}
	var posted storage.Document
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		posted, err = u.Documents().Get(ctx, domain.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got := closeChecks(t, ctx, s, b.Org, b.Oct.ID)
	if c := got["prepayments"]; c.Severity != closing.OK || c.Detail != "1 open schedule(s)" {
		t.Errorf("the prepayments check: %+v", c)
	}
	if c := got["prepayments-unsupported"]; c.Severity != closing.Warning ||
		c.Detail != "1210 holds 50.00 SGD from manual journals: release them by journal as the months pass, or have the accountant check them" {
		t.Errorf("the journal's amount: %+v", c)
	}
	if c := got["prepayments-unscheduled"]; c.Severity != closing.Warning || c.Title != "Prepayments holds costs entered on it directly" ||
		c.Detail != posted.Number+" (600.00): no schedule releases them; spread the cost over its months instead, or release them by journal" {
		t.Errorf("the bill's own amount: %+v", c)
	}

	// 30 November, the job not run: November's part waits, and 1210 still ties.
	d = deferralsAt(t, s, b.Org, porttest.Day(11, 30))
	if m := d.Schedules[0].Months[2]; m.Status != journal.ReleaseWaiting || d.Holdings[0].Reconciliation.Unexplained() {
		t.Fatalf("30 November: %+v %+v", m, d.Holdings[0])
	}

	// A release of May 2027, which the bill's schedule does not have, posted on 26 October with the bill's ID.
	may := time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC)
	append_(storage.NewEntry{PostingDate: porttest.Day(10, 26), PostingIdentity: ledger.BillReleaseIdentity(jira.ID, 1, 1, may, ledger.RuleBillRelease),
		RuleVersion: ledger.RuleBillRelease, Memo: "Forged", Source: storage.Source{Type: ledger.SourceBillRelease, ID: jira.ID.String(), Version: 1},
		Lines: []storage.NewLine{
			{AccountID: b.Rent.ID, Currency: "SGD", Amount: "100.00", FXRate: "1", FXRateSource: "base", BaseAmount: "100.00"},
			{AccountID: b.Prepaid.ID, Currency: "SGD", Amount: "-100.00", FXRate: "1", FXRateSource: "base", BaseAmount: "-100.00"},
		}})
	if c := closeChecks(t, ctx, s, b.Org, b.Oct.ID)["prepayments"]; c.Severity != closing.Failed ||
		c.Detail != "1210 says 1204.00 SGD and the open schedules 1254.00: a posting and its document disagree; report it, never adjust it by journal" {
		t.Errorf("after the forged release: %+v", c)
	}
}

// INV-07 for 2400 Deferred revenue (FR-GL-03): an invoice of 14 October spreads 1,200.00 over September to December,
// 300.00 a month, September's and October's released when it is issued. At 31 October 2400 holds 600.00, which its
// schedule holds, and the close says so.
// req: INV-07
// req: FR-GL-03
func TestDeferredRevenueSchedulesReconcileWith2400(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "accountant"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Deferrals Pte Ltd")
	invoices := journal.Invoices{Store: s, Clock: clock.NewFrozen(time.Date(2026, 10, 20, 4, 0, 0, 0, time.UTC))}
	doc, err := invoices.Create(ctx, b.Org, spreadInvoice(t, b, "PO-77", "1200.00"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invoices.Post(ctx, b.Org, doc.ID); err != nil {
		t.Fatal(err)
	}
	d := deferralsAt(t, s, b.Org, porttest.Day(10, 31))
	if len(d.Schedules) != 1 || len(d.Holdings) != 1 {
		t.Fatalf("31 October: %+v", d)
	}
	sc, h := d.Schedules[0], d.Holdings[0]
	if sc.Holding != ledger.RoleDeferredRevenue || sc.Total.String() != "1200.00" || sc.Recognised.String() != "600.00" || sc.Remaining.String() != "600.00" {
		t.Errorf("the schedule: %+v", sc)
	}
	if h.Account.ID != b.Deferred.ID || h.Reconciliation.Unexplained() || h.Reconciliation.Base.Control.String() != "600.00" || len(h.Direct) != 0 {
		t.Errorf("2400: %+v", h)
	}
	if c := closeChecks(t, ctx, s, b.Org, b.Oct.ID)["deferred-revenue"]; c.Severity != closing.OK || c.Title != "The deferred revenue schedules agree with deferred revenue" {
		t.Errorf("the deferred revenue check: %+v", c)
	}
}
