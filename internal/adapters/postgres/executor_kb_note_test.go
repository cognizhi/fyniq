//go:build local

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The executor's grant for kb.propose_page is the narrowest the command needs (migration 001105): it adds a general NOTE under a key that begins
// "proposed." and reads notes, and nothing more: no policy or decision, no other data class, no key of a person's, no change or removal of a note, and
// nothing of the compiled pages, their versions, snapshots or findings.
// req: AI-59
// req: AI-10
// asvs: V8.2.2, V8.3.1
func TestAttackTheExecutorAddsAProposedNoteAndNothingMore(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Executor notes Pte Ltd")
	api, exec := store(t, "fyniq_api"), store(t, "fyniq_executor")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	person := founder(t, api, b.Org, 1, now)
	note := func(key, kind, class string) storage.KBNote {
		return storage.KBNote{Key: key, Kind: kind, Title: "How we file claims", Body: "File within two weeks.", DataClass: class, UpdatedBy: person, UpdatedAt: now}
	}
	do := func(f func(k storage.OrganisationKnowledge) error) error {
		return exec.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error { return f(u.OrganisationKnowledge()) })
	}
	if err := do(func(k storage.OrganisationKnowledge) error {
		_, err := k.SaveNote(ctx, note("proposed.how-we-file-claims", "note", "general"), 0)
		return err
	}); err != nil {
		t.Fatalf("what the executor may do: %v", err)
	}
	for name, n := range map[string]storage.KBNote{
		"a policy":        note("proposed.a-policy", "policy", "general"),
		"a decision":      note("proposed.a-decision", "decision", "general"),
		"a personal note": note("proposed.personal", "note", "personal"),
		"a payroll note":  note("proposed.payroll", "note", "payroll"),
		"a person's key":  note("expense-policy", "note", "general"),
		"a near key":      note("proposedx.how", "note", "general"),
	} {
		if err := do(func(k storage.OrganisationKnowledge) error { _, err := k.SaveNote(ctx, n, 0); return err }); err == nil {
			t.Errorf("the executor wrote %s", name)
		}
	}
	// It changes no note, and removes none; it holds nothing on the pages.
	if err := do(func(k storage.OrganisationKnowledge) error {
		n, err := k.Note(ctx, "proposed.how-we-file-claims")
		if err != nil {
			return err
		}
		n.Body = "Changed"
		_, err = k.SaveNote(ctx, n, n.Version)
		return err
	}); err == nil {
		t.Error("the executor changed a note")
	}
	su := conn(t, "fyniq_executor")
	for _, q := range []string{`DELETE FROM fyniq.kb_note`, `SELECT 1 FROM fyniq.kb_page_version LIMIT 1`, `INSERT INTO fyniq.kb_page_version (key) VALUES ('x')`, `SELECT 1 FROM fyniq.kb_snapshot LIMIT 1`} {
		if _, err := su.Exec(ctx, q); err == nil {
			t.Errorf("the executor ran %s", q)
		}
	}
}
