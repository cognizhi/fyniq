//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/billpayment"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/domain/payables"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AP-13 (rev 0.93, the owner's decisions of 23 Oct 2026): a posted bill's payment date is planned beside it, posting
// nothing and never changing (FY175), the latest plan in force; a draft, a date before the bill's and a paid bill are
// refused. The bills to pay as at a day are the overdue ones and those due or planned within the reminder days,
// earliest first. A supplier's discount terms give its bills their discount, worked out with RND-DISCOUNT.
// req: FR-AP-13
// req: RND-DISCOUNT
func TestBillPaymentsArePlannedAndReminded(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "ap-specialist"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Plans Pte Ltd")
	bills := journal.Bills{Store: s}
	sgd := money.MustLookup("SGD")
	var current storage.Account
	var discounted storage.Contact
	ten := 10
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if current, err = u.Accounts().Create(ctx, storage.NewAccount{Code: "1010", Name: "DBS current", Type: storage.Asset,
			AccountAttributes: storage.AccountAttributes{MoneyAccount: true}}); err != nil {
			return err
		}
		discounted, err = u.Contacts().Create(ctx, storage.ContactDetails{LegalName: "Printers Pte Ltd", Vendor: true, DiscountPercent: "2", DiscountDays: &ten})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	post := func(c storage.Contact, ref string, date, due time.Time, net string) storage.Document {
		t.Helper()
		doc, err := bills.Create(ctx, b.Org, bill.Draft{SupplierID: c.ID, SupplierRef: ref, Date: date, DueDate: due, Currency: sgd,
			Lines: []bill.Line{{AccountID: b.Rent.ID, Net: sgdOf(t, net), GSTCode: "NR", GST: money.Zero(sgd)}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bills.Post(ctx, b.Org, doc.ID); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	day := porttest.Day
	post(b.Supplier, "A-1", day(10, 1), day(10, 10), "100.00")
	post(b.Supplier, "A-2", day(10, 1), day(10, 22), "200.00")
	later := post(discounted, "P-1", day(10, 14), day(11, 30), "1090.00")
	paid := post(b.Supplier, "A-3", day(10, 2), day(10, 21), "50.00")
	if _, err := (journal.BillPayments{Store: s}).Record(ctx, b.Org, billpayment.Draft{SupplierID: b.Supplier.ID, Date: day(10, 15), AccountID: current.ID,
		Currency: sgd, Bills: []billpayment.Part{{BillID: paid.ID, Amount: sgdOf(t, "50.00")}}, Unapplied: money.Zero(sgd)}); err != nil {
		t.Fatal(err)
	}

	// The discount its supplier offers: 2% of S$1,090.00 if paid by 24 October.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		d, err := bill.Read(later, sgd)
		if err != nil {
			return err
		}
		disc, err := bill.DiscountOf(discounted, d)
		if err != nil || disc == nil || disc.Amount.String() != "21.80" || !disc.Until.Equal(day(10, 24)) {
			t.Errorf("the discount: %+v %v", disc, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	on := func(d time.Time) *time.Time { return &d }
	if _, err := bills.PlanPayment(ctx, b.Org, later.ID, on(day(10, 23))); err != nil {
		t.Fatal(err)
	}
	draft, err := bills.Create(ctx, b.Org, bill.Draft{SupplierID: b.Supplier.ID, Date: day(10, 1), Currency: sgd,
		Lines: []bill.Line{{AccountID: b.Rent.ID, Net: sgdOf(t, "1.00"), GSTCode: "NR", GST: money.Zero(sgd)}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		id   storage.Document
		on   *time.Time
		code string
	}{
		"a draft":                {draft, on(day(10, 23)), bill.CodeNotPosted},
		"before the bill's date": {later, on(day(10, 13)), bill.CodePlan},
		"a paid bill":            {paid, on(day(10, 23)), bill.CodePlan},
	} {
		if _, err := bills.PlanPayment(ctx, b.Org, c.id.ID, c.on); !hasCode(err, c.code) {
			t.Errorf("%s: %v; want %s", name, err, c.code)
		}
	}

	reminders := func(today time.Time, days int) string {
		t.Helper()
		var out []string
		if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			rs, err := payables.Reminders(ctx, u, sgd, b.AP.ID, today, days)
			for _, r := range rs {
				out = append(out, r.Item.Number+" "+r.Why)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return fmtList(out)
	}
	// On 20 October with 3 days: A-1 is overdue, P-1 is planned for the 23rd, A-2 falls due on the 22nd; A-3 is paid.
	if got, want := reminders(day(10, 20), 3), "BILL-0001 overdue, BILL-0002 due, BILL-0003 planned"; got != want {
		t.Errorf("on 20 October: %s; want %s", got, want)
	}
	// Unscheduled, P-1 falls due only on 30 November.
	if _, err := bills.PlanPayment(ctx, b.Org, later.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got, want := reminders(day(10, 20), 3), "BILL-0001 overdue, BILL-0002 due"; got != want {
		t.Errorf("unscheduled: %s; want %s", got, want)
	}

	for _, c := range []struct{ role, stmt, state string }{
		{"fyniq_api", "UPDATE fyniq.bill_payment_plan SET planned_on = '2026-12-01'", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.bill_payment_plan", "42501"},
		{"fyniq_migrator", "UPDATE fyniq.bill_payment_plan SET planned_on = '2026-12-01'", "FY175"},
		{"fyniq_migrator", "DELETE FROM fyniq.bill_payment_plan", "FY175"},
		{"fyniq_worker", "INSERT INTO fyniq.bill_payment_plan (bill_id, recorded_by) VALUES ('" + later.ID.String() + "', 'user:x')", "42501"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}

func fmtList(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}
