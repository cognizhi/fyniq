//go:build local

package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The company secretary assistant's reminders are the executor's alone to queue (Appendix E sec.draft_reminder; design
// D8, migration 000388): the executor queues that purpose and no other, and no other role queues it, so a reminder the AI
// drafted leaves only once a person approved it.
// req: ai/task-catalogue sec.draft_reminder Draft a reminder about an obligation
// req: QA-ENG-05
// asvs: V8.2.2
func TestAttackOnlyAnApprovedProposalSendsAReminder(t *testing.T) {
	ctx := context.Background()
	b := books(t, "Attack reminders Pte Ltd")
	if err := store(t, "fyniq_executor").Do(signedIn(), b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.EmailMessages().Queue(ctx, "secretary_reminder", "secretary@example.test", "The annual return is due")
		return err
	}); err != nil {
		t.Fatalf("the executor queueing a reminder: %v", err)
	}
	insert := func(purpose string) string {
		return `INSERT INTO fyniq.email_message (purpose, recipient, subject) VALUES ('` + purpose + `', 'someone@example.test', 'x')`
	}
	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the executor queueing a notification", "fyniq_executor", insert("notification"), []string{"42501"}},
		{"the executor queueing a test message", "fyniq_executor", insert("test"), []string{"42501"}},
		{"the API queueing a reminder", "fyniq_api", insert("secretary_reminder"), []string{"42501"}},
		{"the worker queueing a reminder", "fyniq_worker", insert("secretary_reminder"), []string{"42501"}},
		{"the AI queueing a reminder", "fyniq_ai", insert("secretary_reminder"), []string{"42501"}},
		{"the executor changing a message's outcome", "fyniq_executor", `UPDATE fyniq.email_message SET status = 'accepted'`, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), b.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}
}
