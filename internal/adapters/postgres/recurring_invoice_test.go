//go:build local

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-AR-07, FR-AUT-01, FR-AUT-02 (the owner's decision of 24 Oct 2026, built in rev 0.135; p1-sales task 6.1, part): the
// scheduler makes the draft invoice of each occurrence of a recurring invoice, from its current version, saved by its
// own principal, and nothing more. A fortnightly one from Monday 5 Oct makes the drafts of 5 and 19 Oct by 25 Oct, each
// dated on its occurrence and naming the version it came from; a second run makes none. The worker holds no INSERT on
// documents: its one way to make an invoice is create_scheduled_invoice_draft, for an active recurring invoice's next
// occurrence, once; it cannot issue one or change the recurring invoice, and a version never changes. A person
// completes a draft, which keeps naming the version, and issues it as any invoice. Paused, it makes none; resumed, it
// skips what it missed. A line naming a quote is refused.
// req: FR-AR-07
// req: FR-AUT-01
// req: FR-AUT-02
// req: SEC-03
// asvs: V13.2.2
func TestRecurringInvoicesMakeDraftsTheSchedulerCannotIssue(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "recurring-author"}})
	s := store(t, "fyniq_api")
	b := newSalesBooks(t, ctx, s, "Recurring invoices Pte Ltd")
	api := journal.Invoices{Store: s}
	sgd := money.MustLookup("SGD")
	content := invoice.Draft{CustomerID: b.Customer.ID, Reference: "PO-4411", Currency: sgd, Notes: "Fortnightly support",
		Lines: []invoice.Line{invoiceLine(t, b, "1", "500.00", "", "SR", "SGD")}}
	r, err := api.CreateRecurring(ctx, b.Org, journal.RecurringInvoiceInput{Name: "Support, Acme",
		Schedule: storage.Schedule{EveryWeeks: 2, StartsOn: porttest.Day(10, 5)}, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	if r.NextOn == nil || !r.NextOn.Equal(porttest.Day(10, 5)) || r.Version != 1 || r.ContentVersion != 1 || r.EditedBy != "user:recurring-author" {
		t.Fatalf("a new recurring invoice: %+v", r)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		st, err := journal.RecurringInvoiceStatuses(ctx, u, []storage.RecurringInvoice{r}, porttest.Day(10, 1))
		if err != nil {
			return err
		}
		if len(st[r.ID]) != 1 || st[r.ID][0].Code != journal.StatusRecurringReady {
			t.Errorf("a ready recurring invoice: %+v", st[r.ID])
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	quoted := content
	quoted.Lines = []invoice.Line{invoiceLine(t, b, "1", "500.00", "", "SR", "SGD")}
	quoted.Lines[0].Quote = &invoice.QuoteRef{QuoteID: b.Customer.ID, Line: 1}
	if _, err := api.CreateRecurring(ctx, b.Org, journal.RecurringInvoiceInput{Name: "Quoted",
		Schedule: storage.Schedule{EveryMonths: 1, DayOfMonth: 1, StartsOn: porttest.Day(10, 1)}, Content: quoted}); !hasInvoiceCode(err, invoice.CodeLine) {
		t.Errorf("a line naming a quote: %v", err)
	}

	// The scheduler catches up to 25 October: 5 and 19 October.
	worker := journal.Invoices{Store: store(t, "fyniq_worker")}
	run, err := worker.MakeDue(context.Background(), b.Org, porttest.Day(10, 25))
	if err != nil || run.Made != 2 {
		t.Fatalf("the first run: %+v %v", run, err)
	}
	if again, err := worker.MakeDue(context.Background(), b.Org, porttest.Day(10, 25)); err != nil || again.Made != 0 {
		t.Fatalf("a second run the same day: %+v %v", again, err)
	}
	var draft storage.Document
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if r, err = u.RecurringInvoices().Get(ctx, r.ID); err != nil {
			return err
		}
		draft, err = u.Documents().Get(ctx, *r.LastInvoiceID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !r.LastOn.Equal(porttest.Day(10, 19)) || !r.NextOn.Equal(porttest.Day(11, 2)) {
		t.Errorf("after the run: last %v, next %v", r.LastOn, r.NextOn)
	}
	d, err := invoice.Read(draft, sgd)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Type != storage.DocumentInvoice || draft.Status != storage.DocumentDraft || draft.Version != 1 || draft.Number != "" ||
		draft.SavedBy != "scheduler:invoices.recurring" || !d.Date.Equal(porttest.Day(10, 19)) || d.CustomerID != b.Customer.ID || d.Reference != "PO-4411" ||
		len(d.Lines) != 1 || d.RecurringInvoice == nil || d.RecurringInvoice.ID != r.ID || d.RecurringInvoice.Version != 1 {
		t.Errorf("the 19 October draft: %+v %+v", draft, d)
	}
	if err := store(t, "fyniq_readonly").Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		events, err := u.Audit().Events(ctx, 0, 10000)
		if err != nil {
			return err
		}
		var trail strings.Builder
		for _, ev := range events {
			trail.WriteString(ev.Body + "\n")
		}
		for _, want := range []string{`"actor":{"type":"scheduler","id":"invoices.recurring"}`, `"action":"recurring_invoice.ran"`,
			`"action":"recurring_invoice.created"`} {
			if !strings.Contains(trail.String(), want) {
				t.Errorf("the audit trail lacks %s", want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A person changes the notes: the draft keeps naming the recurring invoice's version, and issues as any invoice.
	d.Notes = "Fortnightly support, 5 to 18 October"
	d.RecurringInvoice = nil
	invoices := journal.Invoices{Store: s}
	if _, err := invoices.Revise(ctx, b.Org, draft.ID, d); err != nil {
		t.Fatal(err)
	}
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		draft, err = u.Documents().Get(ctx, draft.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if ref := invoice.RecurringOf(draft); draft.Version != 2 || ref == nil || ref.ID != r.ID || ref.Version != 1 {
		t.Errorf("the completed draft: %+v %+v", draft, ref)
	}
	if _, err := invoices.Post(ctx, b.Org, draft.ID); err != nil {
		t.Fatalf("issuing the completed draft: %v", err)
	}

	// A paused one makes nothing; resumed on Wednesday 18 November, it skips what it missed.
	if r, err = api.PauseRecurring(ctx, b.Org, r.ID, r.Version); err != nil {
		t.Fatal(err)
	}
	if run, err := worker.MakeDue(context.Background(), b.Org, porttest.Day(11, 17)); err != nil || run.Made != 0 {
		t.Fatalf("a run while paused: %+v %v", run, err)
	}
	if r, err = api.ResumeRecurring(ctx, b.Org, r.ID, r.Version, porttest.Day(11, 18)); err != nil {
		t.Fatal(err)
	}
	if r.Status != storage.RecurringActive || !r.NextOn.Equal(porttest.Day(11, 30)) {
		t.Errorf("resumed on 18 November: %+v", r)
	}
	// A changed content is a new version.
	content.Notes = "Support"
	if r, err = api.UpdateRecurring(ctx, b.Org, r.ID, r.Version, journal.RecurringInvoiceInput{Name: r.Name, Schedule: r.Schedule, Content: content}); err != nil ||
		r.ContentVersion != 2 {
		t.Fatalf("a new content: %+v %v", r, err)
	}

	var first storage.Document // the 5 October draft, still a draft
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		drafts, err := u.Documents().Page(ctx, storage.DocumentFilter{Type: storage.DocumentInvoice, Status: storage.DocumentDraft, Limit: 10})
		for _, x := range drafts {
			if x.Date == "2026-10-05" {
				first = x
			}
		}
		return err
	}); err != nil || first.ID == draft.ID || first.SavedBy != "scheduler:invoices.recurring" {
		t.Fatalf("the 5 October draft: %+v %v", first, err)
	}
	id, doc := r.ID.String(), first.ID.String()
	for _, c := range []struct{ role, stmt, state string }{
		{"fyniq_worker", "INSERT INTO fyniq.document (type) VALUES ('invoice')", "42501"},
		{"fyniq_worker", "SELECT fyniq.create_scheduled_invoice_draft('" + id + "', '2026-10-19')", "FY199"},
		{"fyniq_worker", "SELECT fyniq.create_scheduled_invoice_draft('" + id + "', '2027-03-31')", "FY199"},
		{"fyniq_worker", "UPDATE fyniq.document SET status = 'posted' WHERE id = '" + doc + "'", "FY053"},
		{"fyniq_worker", "UPDATE fyniq.recurring_invoice SET name = 'x' WHERE id = '" + id + "'", "42501"},
		{"fyniq_worker", "UPDATE fyniq.recurring_invoice SET last_on = NULL WHERE id = '" + id + "'", "42501"},
		{"fyniq_api", "SELECT fyniq.create_scheduled_invoice_draft('" + id + "', '2026-11-30')", "42501"},
		{"fyniq_api", "UPDATE fyniq.recurring_invoice_version SET content = '{}' WHERE recurring_invoice_id = '" + id + "'", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.recurring_invoice_version WHERE recurring_invoice_id = '" + id + "'", "42501"},
		{"fyniq_migrator", "UPDATE fyniq.recurring_invoice_version SET content = '{}' WHERE recurring_invoice_id = '" + id + "'", "FY200"},
		{"fyniq_migrator", "DELETE FROM fyniq.recurring_invoice_version WHERE recurring_invoice_id = '" + id + "'", "FY200"},
		// A version's content is an invoice's without its date, rate, deposits, credited invoice or source.
		{"fyniq_api", "INSERT INTO fyniq.recurring_invoice_version (recurring_invoice_id, version, content, edited_by) VALUES ('" + id +
			"', 9, '{\"date\":\"2026-10-01\"}', 'user:x')", "23514"},
		{"fyniq_api", "INSERT INTO fyniq.recurring_invoice_version (recurring_invoice_id, version, content, edited_by) VALUES ('" + id +
			"', 9, '{\"deposits\":[]}', 'user:x')", "23514"},
		// A schedule is weekly or monthly, never both.
		{"fyniq_api", "UPDATE fyniq.recurring_invoice SET every_months = 1, day_of_month = 1 WHERE id = '" + id + "'", "23514"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}
