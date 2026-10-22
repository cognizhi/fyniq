//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/bill"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AP-09, FR-AUT-01, FR-AUT-02 (the owner's decision of 22 Oct 2026, built in rev 0.84; p1-purchases task 7.1): the
// scheduler makes the draft bill of each occurrence of a recurring bill, from its current version, saved by its own
// principal, and nothing more. The worker's role holds no INSERT on documents: its one way to make a bill is
// create_scheduled_bill_draft, which makes a version-1 draft bill of an active recurring bill's next occurrence, once;
// it cannot post it, change the recurring bill, or rewrite a version. The draft keeps naming the recurring bill's
// version when a person completes it, and posts as any bill does.
// req: FR-AP-09
// req: FR-AUT-01
// req: FR-AUT-02
// req: SEC-03
// asvs: V13.2.2
func TestRecurringBillsMakeDraftsTheSchedulerCannotPost(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "recurring-author"}})
	s := store(t, "fyniq_api")
	b := newPurchasesBooks(t, ctx, s, "Recurring bills Pte Ltd")
	api := journal.Bills{Store: s}
	content := bill.Draft{SupplierID: b.Supplier.ID, Currency: money.MustLookup("SGD"), Notes: "Unit 05-12",
		Lines: []bill.Line{{AccountID: b.Rent.ID, Description: "Office rent", Net: sgdOf(t, "3000.00"), GSTCode: "TX", GST: sgdOf(t, "0.00")}}}
	r, err := api.CreateRecurring(ctx, b.Org, journal.RecurringBillInput{Name: "Office rent",
		Schedule: storage.Schedule{EveryMonths: 1, DayOfMonth: 31, StartsOn: porttest.Day(10, 1)}, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if r.NextOn == nil || !r.NextOn.Equal(porttest.Day(10, 31)) || r.Version != 1 || r.ContentVersion != 1 || r.EditedBy != "user:recurring-author" {
		t.Fatalf("a new recurring bill: %+v", r)
	}

	// Its statuses: ready, as the bill rules see its content on its next occurrence.
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		st, err := journal.RecurringBillStatuses(ctx, u, []storage.RecurringBill{r}, porttest.Day(10, 20))
		if err != nil {
			return err
		}
		if len(st[r.ID]) != 1 || st[r.ID][0].Code != journal.StatusRecurringReady {
			t.Errorf("a ready recurring bill: %+v", st[r.ID])
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The scheduler catches up to its day: 31 October and 30 November.
	worker := journal.Bills{Store: store(t, "fyniq_worker")}
	run, err := worker.MakeDue(context.Background(), b.Org, porttest.Day(11, 30))
	if err != nil || run.Made != 2 {
		t.Fatalf("the first run: %+v %v", run, err)
	}
	if again, err := worker.MakeDue(context.Background(), b.Org, porttest.Day(11, 30)); err != nil || again.Made != 0 {
		t.Fatalf("a second run the same day: %+v %v", again, err)
	}
	var draft storage.Document
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if r, err = u.RecurringBills().Get(ctx, r.ID); err != nil {
			return err
		}
		draft, err = u.Documents().Get(ctx, *r.LastBillID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !r.LastOn.Equal(porttest.Day(11, 30)) || !r.NextOn.Equal(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("after the run: last %v, next %v", r.LastOn, r.NextOn)
	}
	d, err := bill.Read(draft, money.MustLookup("SGD"))
	if err != nil {
		t.Fatal(err)
	}
	if draft.Type != storage.DocumentBill || draft.Status != storage.DocumentDraft || draft.Version != 1 || draft.Number != "" ||
		draft.SavedBy != "scheduler:bills.recurring" || !d.Date.Equal(porttest.Day(11, 30)) || d.SupplierID != b.Supplier.ID || d.Notes != "Unit 05-12" ||
		len(d.Lines) != 1 || d.SupplierRef != "" || d.RecurringBill == nil || d.RecurringBill.ID != r.ID || d.RecurringBill.Version != 1 {
		t.Errorf("the November draft: %+v %+v", draft, d)
	}
	// Its audit events name the scheduler.
	if err := store(t, "fyniq_readonly").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		if err != nil {
			return err
		}
		var trail strings.Builder
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		for _, want := range []string{`"actor":{"type":"scheduler","id":"bills.recurring"}`, `"action":"recurring_bill.ran"`,
			`"action":"recurring_bill.created"`} {
			if !strings.Contains(trail.String(), want) {
				t.Errorf("the audit trail lacks %s", want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A person adds the supplier's number: the draft keeps naming the recurring bill's version, and posts as any bill.
	d.SupplierRef = "RENT-2611"
	d.RecurringBill = nil
	bills := journal.Bills{Store: s}
	if _, err := bills.Revise(ctx, b.Org, draft.ID, d); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		draft, err = u.Documents().Get(ctx, draft.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if ref := bill.RecurringOf(draft); draft.Version != 2 || ref == nil || ref.ID != r.ID || ref.Version != 1 {
		t.Errorf("the completed draft: %+v %+v", draft, ref)
	}
	if e, err := bills.Post(ctx, b.Org, draft.ID); err != nil || legsOf(t, s, b.Org, e) != "6100 3000 SGD = 3000; 2010 -3000 SGD = -3000" {
		t.Fatalf("posting the completed draft: %s %v", legsOf(t, s, b.Org, e), err)
	}

	// A paused one makes nothing; resumed on 15 February, it skips what it missed.
	if r, err = api.PauseRecurring(ctx, b.Org, r.ID, r.Version); err != nil {
		t.Fatal(err)
	}
	if run, err := worker.MakeDue(context.Background(), b.Org, time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)); err != nil || run.Made != 0 {
		t.Fatalf("a run while paused: %+v %v", run, err)
	}
	if r, err = api.ResumeRecurring(ctx, b.Org, r.ID, r.Version, time.Date(2027, 2, 15, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if r.Status != storage.RecurringActive || !r.NextOn.Equal(time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("resumed on 15 February: %+v", r)
	}
	// A changed content is a new version; a content incomplete is a status (✖), not a refusal.
	content.Lines[0].GSTCode = "SR"
	if r, err = api.UpdateRecurring(ctx, b.Org, r.ID, r.Version, journal.RecurringBillInput{Name: r.Name, Schedule: r.Schedule, Content: content}); err != nil ||
		r.ContentVersion != 2 {
		t.Fatalf("a new content: %+v %v", r, err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		st, err := journal.RecurringBillStatuses(ctx, u, []storage.RecurringBill{r}, time.Date(2027, 2, 15, 0, 0, 0, 0, time.UTC))
		if err != nil {
			return err
		}
		if len(st[r.ID]) != 1 || st[r.ID][0].Code != bill.CodeGSTCode || st[r.ID][0].Severity != journal.SeverityError {
			t.Errorf("an incomplete recurring bill: %+v", st[r.ID])
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var october storage.Document // the first draft, still a draft
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		drafts, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentBill, Status: storage.DocumentDraft, Limit: 10})
		for _, x := range drafts {
			if x.Date == "2026-10-31" {
				october = x
			}
		}
		return err
	}); err != nil || october.ID == draft.ID || october.SavedBy != "scheduler:bills.recurring" {
		t.Fatalf("the October draft: %+v %v", october, err)
	}
	id, doc := r.ID.String(), october.ID.String()
	for _, c := range []struct{ role, stmt, state string }{
		// The worker makes bills only through create_scheduled_bill_draft.
		{"fyniq_worker", "INSERT INTO fyniq.document (type) VALUES ('bill')", "42501"},
		// Only for an active recurring bill's next occurrence, once.
		{"fyniq_worker", "SELECT fyniq.create_scheduled_bill_draft('" + id + "', '2026-11-30')", "FY057"},
		{"fyniq_worker", "SELECT fyniq.create_scheduled_bill_draft('" + id + "', '2027-03-31')", "FY057"},
		// It cannot post a draft: it cannot say who posted it.
		{"fyniq_worker", "UPDATE fyniq.document SET status = 'posted' WHERE id = '" + doc + "'", "FY053"},
		// Nor change a recurring bill beyond its next occurrence.
		{"fyniq_worker", "UPDATE fyniq.recurring_bill SET name = 'x' WHERE id = '" + id + "'", "42501"},
		{"fyniq_worker", "UPDATE fyniq.recurring_bill SET last_on = NULL WHERE id = '" + id + "'", "42501"},
		// Nobody else makes scheduled bills.
		{"fyniq_api", "SELECT fyniq.create_scheduled_bill_draft('" + id + "', '2027-02-28')", "42501"},
		// A version never changes or goes.
		{"fyniq_api", "UPDATE fyniq.recurring_bill_version SET content = '{}' WHERE recurring_bill_id = '" + id + "'", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.recurring_bill_version WHERE recurring_bill_id = '" + id + "'", "42501"},
		{"fyniq_migrator", "UPDATE fyniq.recurring_bill_version SET content = '{}' WHERE recurring_bill_id = '" + id + "'", "FY058"},
		{"fyniq_migrator", "DELETE FROM fyniq.recurring_bill_version WHERE recurring_bill_id = '" + id + "'", "FY058"},
		// A version's content is a bill's without its date, due date, supplier's number or source.
		{"fyniq_api", "INSERT INTO fyniq.recurring_bill_version (recurring_bill_id, version, content, edited_by) VALUES ('" + id +
			"', 9, '{\"date\":\"2026-10-01\"}', 'user:x')", "23514"},
		{"fyniq_api", "INSERT INTO fyniq.recurring_bill_version (recurring_bill_id, version, content, edited_by) VALUES ('" + id +
			"', 9, '{\"supplier_ref\":\"RENT-1\"}', 'user:x')", "23514"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}
