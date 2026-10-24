//go:build local

package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/local"
	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/doctemplate"
	"github.com/cognizhi/fyniq/internal/domain/invoice"
	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/domain/money"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
	"github.com/cognizhi/fyniq/internal/render"
	"github.com/cognizhi/fyniq/internal/salesmail"
)

// FR-AR-07, FM-3 (the owner's decisions of 24 Oct 2026; the threat model docs/security/threat-models/scheduled-invoices.md):
// a recurring invoice set to issue and send makes drafts until its content is approved: its 1 October draft stays a
// draft. Approved, the scheduler issues the 1 November draft as itself (INV-0001), lists it for the weekly review, and
// emails it to Acme's billing email with its PDF as evidence, an online link and the send; a second run sends nothing
// more. A change of content withdraws the approval: the 1 December draft stays a draft. The database lets the worker
// post, number, attach, link, send and list for review only such an invoice (FY201), and queue invoice email only with
// such a send (FY202): a person's draft and the unapproved draft are refused, whatever it tries.
// req: FR-AR-07
// req: FM-3
// req: FR-AUT-02
// req: SEC-03
// asvs: V13.2.2
func TestTheSchedulerIssuesOnlyApprovedRecurringInvoices(t *testing.T) {
	author := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "author"}})
	approver := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "approver"}})
	s, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	b := newSalesBooks(t, author, s, "Scheduled invoices Pte Ltd")
	sgd := money.MustLookup("SGD")
	if err := s.Do(author, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.Contacts().Get(ctx, b.Customer.ID)
		if err != nil {
			return err
		}
		c.BillingEmail = "ap@acme.example"
		_, err = u.Contacts().Update(ctx, c.ID, c.Version, c.ContactDetails, c.Active)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	content := invoice.Draft{CustomerID: b.Customer.ID, Currency: sgd, Notes: "Retainer", Lines: []invoice.Line{invoiceLine(t, b, "1", "800.00", "", "SR", "SGD")}}
	api := journal.Invoices{Store: s}
	r, err := api.CreateRecurring(author, b.Org, journal.RecurringInvoiceInput{Name: "Retainer, Acme", Mode: storage.RecurringIssueAndSend,
		Schedule: storage.Schedule{EveryMonths: 1, DayOfMonth: 1, StartsOn: porttest.Day(10, 1)}, Content: content})
	if err != nil || r.Mode != storage.RecurringIssueAndSend || r.IssuesAndSends() {
		t.Fatalf("a recurring invoice to issue and send: %+v %v", r, err)
	}
	scheduler := journal.Invoices{Store: worker}
	run, err := scheduler.MakeDue(context.Background(), b.Org, porttest.Day(10, 15))
	if err != nil || run.Made != 1 || run.Issued != 0 {
		t.Fatalf("before the approval: %+v %v", run, err)
	}
	october := lastInvoiceOf(t, s, b.Org, r.ID)
	if october.Status != storage.DocumentDraft {
		t.Fatalf("the unapproved 1 October draft: %+v", october)
	}

	// Approved by another person, the next occurrence is issued as the scheduler and listed for the weekly review.
	if err := s.Do(approver, b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		r, err = u.RecurringInvoices().Approve(ctx, r.ID, 1)
		return err
	}); err != nil || !r.IssuesAndSends() || r.ApprovedBy != "user:approver" {
		t.Fatalf("approving it: %+v %v", r, err)
	}
	if run, err = scheduler.MakeDue(context.Background(), b.Org, porttest.Day(11, 1)); err != nil || run.Made != 1 || run.Issued != 1 {
		t.Fatalf("after the approval: %+v %v", run, err)
	}
	november := lastInvoiceOf(t, s, b.Org, r.ID)
	if november.Status != storage.DocumentPosted || november.Number != "INV-0001" || november.PostedBy != "scheduler:invoices.recurring" ||
		november.SavedBy != "scheduler:invoices.recurring" {
		t.Fatalf("the 1 November invoice: %+v", november)
	}
	if err := s.Do(author, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		entries, err := u.ReviewLog().List(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, e := range entries {
			found = found || (e.Kind == storage.ReviewScheduledIssue && e.ObjectKey == november.ID.String() && strings.Contains(e.Summary, "INV-0001"))
		}
		if !found {
			t.Errorf("the weekly review lacks INV-0001: %+v", entries)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The scheduler emails it, once, and only for ScheduledSendFor: one older waits for a person.
	unsent := func(s storage.Store) (ids []uuid.UUID) {
		if err := s.Do(context.Background(), b.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			ids, err = u.RecurringInvoices().Unsent(ctx, 10)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	later := store(t, "fyniq_worker", postgres.WithClock(clock.NewFrozen(november.PostedAt.Add(storage.ScheduledSendFor+time.Minute))))
	if now, then := unsent(worker), unsent(later); len(now) != 1 || now[0] != november.ID || len(then) != 0 {
		t.Fatalf("the invoices to send: now %v, after the window %v", now, then)
	}
	blobs, err := local.NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pdf := []byte("%PDF-1.7 INV-0001")
	sum := sha256.Sum256(pdf)
	frozen := clock.NewFrozen(time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC))
	sender := salesmail.Sender{Store: worker, Staging: blobs, Outbox: outbox.Dispatcher{Store: worker, Queue: local.NewMemory(), Clock: frozen},
		Origin: "https://fyniq.test", Clock: frozen,
		Render: func(context.Context, string, doctemplate.Content, render.Data, time.Time) (render.Response, error) {
			return render.Response{PDF: pdf, SHA256: hex.EncodeToString(sum[:])}, nil
		}}
	if n, err := sender.SendScheduled(context.Background(), b.Org); err != nil || n != 1 {
		t.Fatalf("sending it: %d %v", n, err)
	}
	if n, err := sender.SendScheduled(context.Background(), b.Org); err != nil || n != 0 {
		t.Fatalf("a second run: %d %v", n, err)
	}
	if err := s.Do(author, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		sends, err := u.DocumentSends().OfDocument(ctx, november.ID)
		if err != nil || len(sends) != 1 || sends[0].Recipient != "ap@acme.example" || sends[0].SentBy != "scheduler:invoices.recurring" {
			t.Errorf("its send: %+v %v", sends, err)
		}
		links, err := u.DocumentLinks().OfDocument(ctx, november.ID)
		if err != nil || len(links) != 1 {
			t.Errorf("its link: %+v %v", links, err)
		}
		files, err := u.Evidence().OfDocument(ctx, november.ID, uuid.Nil, 10)
		if err != nil || len(files) != 1 || files[0].Source != storage.SourceGenerated || files[0].SHA256 != hex.EncodeToString(sum[:]) {
			t.Errorf("its PDF: %+v %v", files, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A change of content withdraws the approval: the next draft stays a draft.
	content.Notes = "Retainer, revised"
	if r, err = api.UpdateRecurring(author, b.Org, r.ID, r.Version, journal.RecurringInvoiceInput{Name: r.Name, Schedule: r.Schedule, Content: content,
		Mode: storage.RecurringIssueAndSend}); err != nil || r.IssuesAndSends() {
		t.Fatalf("a new content: %+v %v", r, err)
	}
	if run, err = scheduler.MakeDue(context.Background(), b.Org, porttest.Day(12, 1)); err != nil || run.Made != 1 || run.Issued != 0 {
		t.Fatalf("after the change: %+v %v", run, err)
	}
	december := lastInvoiceOf(t, s, b.Org, r.ID)

	// A person's draft, and the unapproved December draft, are beyond the worker whatever it tries.
	person, err := api.Create(author, b.Org, invoice.Draft{CustomerID: b.Customer.ID, Date: porttest.Day(11, 2), Currency: sgd,
		Lines: []invoice.Line{invoiceLine(t, b, "1", "10.00", "", "SR", "SGD")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range []storage.Document{person, december} {
		id := doc.ID.String()
		for _, c := range []struct{ stmt, state string }{
			{"UPDATE fyniq.document SET status = 'posted', posted_by = 'scheduler:invoices.recurring', posted_at = now() WHERE id = '" + id + "'", "FY201"},
			{"INSERT INTO fyniq.document_number (id, document_type, number) VALUES ('" + id + "', 'invoice', 'INV-9999')", "FY201"},
			{"INSERT INTO fyniq.evidence (document_id, evidence_type, file_name, media_type, size_bytes, sha256, original_sha256, source, data_class, staging_key, uploaded_by) " +
				"VALUES ('" + id + "', 'Invoice as sent', 'x.pdf', 'application/pdf', 1, repeat('ab', 32), repeat('ab', 32), 'generated', 'general', 'staging/x', 'scheduler:invoices.recurring')", "FY201"},
			{"INSERT INTO fyniq.review_log_entry (kind, object_type, object_key, summary, logged_at) VALUES ('scheduled_issue', 'invoice', '" + id + "', 'x', now())", "FY201"},
		} {
			err := asOrg(context.Background(), conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(context.Background(), c.stmt); return err })
			wantState(t, "fyniq_worker: "+c.stmt, err, c.state)
		}
	}
	for _, c := range []struct{ stmt, state string }{
		// Invoice email with no send of an approved recurring invoice, refused at commit.
		{"INSERT INTO fyniq.email_message (purpose, recipient, subject) VALUES ('invoice', 'victim@example.com', 'Pay now')", "FY202"},
		// The worker lists nothing else for review.
		{"INSERT INTO fyniq.review_log_entry (kind, object_type, object_key, summary, logged_at) VALUES ('self_approved', 'invoice', '" +
			november.ID.String() + "', 'x', now())", "FY201"},
		// Nor numbers another document type.
		{"INSERT INTO fyniq.document_sequence (document_type, prefix, digits, gap_free, next_number) VALUES ('bill', 'X-', 4, true, 1)", "FY201"},
	} {
		err := asOrg(context.Background(), conn(t, "fyniq_worker"), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(context.Background(), c.stmt); return err })
		wantState(t, "fyniq_worker: "+c.stmt, err, c.state, "42501")
	}
}

// lastInvoiceOf is the last draft a recurring invoice made, as it stands.
func lastInvoiceOf(t *testing.T, s storage.Store, org, id uuid.UUID) storage.Document {
	t.Helper()
	var doc storage.Document
	if err := s.Do(context.Background(), org, func(ctx context.Context, u storage.UnitOfWork) error {
		r, err := u.RecurringInvoices().Get(ctx, id)
		if err != nil {
			return err
		}
		doc, err = u.Documents().Get(ctx, *r.LastInvoiceID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return doc
}
