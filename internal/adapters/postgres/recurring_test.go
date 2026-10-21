//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/domain/journal"
	"github.com/cognizhi/fyniq/internal/ports/porttest"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// FR-GL-02, FR-AUT-01, FR-AUT-02 (the founders' decision of rev 0.58, built in rev 0.60): the scheduler makes the
// draft journal of each occurrence of a recurring journal, from its current version, saved by its own principal, and
// nothing more. The worker's role holds no INSERT on documents: its one way to make one is create_scheduled_draft,
// which makes a version-1 draft journal of an active recurring journal's next occurrence, once; it cannot post it,
// change the recurring journal, or rewrite a version.
// req: FR-GL-02
// req: FR-AUT-01
// req: FR-AUT-02
// req: SEC-03
// asvs: V13.2.2
func TestRecurringJournalsMakeDraftsTheSchedulerCannotPost(t *testing.T) {
	ctx := storage.WithAudit(context.Background(), storage.AuditContext{Actor: &storage.Actor{Type: "user", ID: "recurring-author"}})
	b := books(t, "Recurring journals Pte Ltd")
	api := journal.Service{Store: store(t, "fyniq_api")}
	d := saleDraft(t, b, time.Time{}, "250.00")
	r, err := api.CreateRecurring(ctx, b.Org, journal.RecurringInput{Name: "Retainer accrual",
		Schedule: storage.Schedule{EveryMonths: 1, DayOfMonth: 31, StartsOn: porttest.Day(10, 1)},
		Content:  journal.RecurringContent{Memo: d.Memo, Lines: d.Lines, Reverses: true}})
	if err != nil {
		t.Fatal(err)
	}
	if r.NextOn == nil || !r.NextOn.Equal(porttest.Day(10, 31)) || r.Version != 1 || r.ContentVersion != 1 || r.EditedBy != "user:recurring-author" {
		t.Fatalf("a new recurring journal: %+v", r)
	}

	// The scheduler catches up to its day: 31 October and 30 November.
	worker := journal.Service{Store: store(t, "fyniq_worker")}
	run, err := worker.MakeDue(context.Background(), b.Org, porttest.Day(11, 30))
	if err != nil || run.Made != 2 {
		t.Fatalf("the first run: %+v %v", run, err)
	}
	if again, err := worker.MakeDue(context.Background(), b.Org, porttest.Day(11, 30)); err != nil || again.Made != 0 {
		t.Fatalf("a second run the same day: %+v %v", again, err)
	}
	s := store(t, "fyniq_api")
	var draft storage.Document
	if err := s.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		var err error
		if r, err = u.RecurringJournals().Get(ctx, r.ID); err != nil {
			return err
		}
		draft, err = u.Documents().Get(ctx, *r.LastJournalID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !r.LastOn.Equal(porttest.Day(11, 30)) || !r.NextOn.Equal(time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("after the run: last %v, next %v", r.LastOn, r.NextOn)
	}
	saved, err := journal.ReadSaved(draft)
	if err != nil {
		t.Fatal(err)
	}
	if draft.Status != storage.DocumentDraft || draft.Version != 1 || draft.Number != "" || draft.SavedBy != "scheduler:journals.recurring" ||
		saved.Date != "2026-11-30" || saved.ReverseOn != "2026-12-01" || saved.Memo != "Retainer" || len(saved.Lines) != 2 ||
		saved.RecurringJournal == nil || saved.RecurringJournal.ID != r.ID || saved.RecurringJournal.Version != 1 {
		t.Errorf("the November draft: %+v %+v", draft, saved)
	}
	var reverses map[string]any
	if err := json.Unmarshal(draft.Payload, &reverses); err != nil || reverses["reverses"] != nil {
		t.Errorf("the draft keeps the recurring journal's own flag: %s", draft.Payload)
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
		for _, want := range []string{`"actor":{"type":"scheduler","id":"journals.recurring"}`, `"action":"recurring_journal.ran"`,
			`"action":"recurring_journal.created"`} {
			if !strings.Contains(trail.String(), want) {
				t.Errorf("the audit trail lacks %s", want)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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
	// A changed content is a new version; the drafts already made keep theirs.
	d.Memo = "Retainer, revised"
	if r, err = api.UpdateRecurring(ctx, b.Org, r.ID, r.Version, journal.RecurringInput{Name: r.Name, Schedule: r.Schedule,
		Content: journal.RecurringContent{Memo: d.Memo, Lines: d.Lines, Reverses: true}}); err != nil || r.ContentVersion != 2 {
		t.Fatalf("a new content: %+v %v", r, err)
	}

	id, doc := r.ID.String(), draft.ID.String()
	for _, c := range []struct{ role, stmt, state string }{
		// The worker makes documents only through create_scheduled_draft.
		{"fyniq_worker", "INSERT INTO fyniq.document (type) VALUES ('journal')", "42501"},
		{"fyniq_worker", "INSERT INTO fyniq.document_version (document_id, version, payload, created_by) VALUES ('" + doc + "', 2, '{}', 'user:x')", "42501"},
		// Only for an active recurring journal's next occurrence, once.
		{"fyniq_worker", "SELECT fyniq.create_scheduled_draft('" + id + "', '2026-11-30')", "FY057"},
		{"fyniq_worker", "SELECT fyniq.create_scheduled_draft('" + id + "', '2027-03-31')", "FY057"},
		// It cannot post its draft: it cannot say who posted it.
		{"fyniq_worker", "UPDATE fyniq.document SET status = 'posted' WHERE id = '" + doc + "'", "FY053"},
		// Nor change a recurring journal beyond its next occurrence.
		{"fyniq_worker", "UPDATE fyniq.recurring_journal SET name = 'x' WHERE id = '" + id + "'", "42501"},
		{"fyniq_worker", "UPDATE fyniq.recurring_journal SET last_on = NULL WHERE id = '" + id + "'", "42501"},
		// Nobody else makes scheduled drafts.
		{"fyniq_api", "SELECT fyniq.create_scheduled_draft('" + id + "', '2027-02-28')", "42501"},
		// A version never changes or goes.
		{"fyniq_api", "UPDATE fyniq.recurring_journal_version SET content = '{}' WHERE recurring_journal_id = '" + id + "'", "42501"},
		{"fyniq_api", "DELETE FROM fyniq.recurring_journal_version WHERE recurring_journal_id = '" + id + "'", "42501"},
		{"fyniq_migrator", "UPDATE fyniq.recurring_journal_version SET content = '{}' WHERE recurring_journal_id = '" + id + "'", "FY058"},
		{"fyniq_migrator", "DELETE FROM fyniq.recurring_journal_version WHERE recurring_journal_id = '" + id + "'", "FY058"},
		// A version's content is a journal's without its date.
		{"fyniq_api", "INSERT INTO fyniq.recurring_journal_version (recurring_journal_id, version, content, edited_by) VALUES ('" + id +
			"', 9, '{\"date\":\"2026-10-01\"}', 'user:x')", "23514"},
	} {
		err := asOrg(ctx, conn(t, c.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, c.stmt); return err })
		wantState(t, c.role+": "+c.stmt, err, c.state)
	}
}
