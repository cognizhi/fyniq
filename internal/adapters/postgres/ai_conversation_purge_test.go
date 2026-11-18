//go:build local

package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The purge of the assistant's conversations removes only what passed its retention (§11.9, AI-P8; design D11): the
// conversations whose last question was asked before the cut, and those their people deleted, with their turns, reads
// and feedback; a recent one stays whole. Only the worker runs it; nobody else removes a conversation, the worker
// included, outside it; the API and the AI's role cannot run it; another organisation's are untouched.
// req: data/backup-and-recovery Retention of AI conversations
// req: QA-ENG-05
// asvs: V14.2.7, V8.2.2
func TestAttackTheAIConversationsPurgeTakesOnlyWhatPassedRetention(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Purge A Pte Ltd"), books(t, "Purge B Pte Ltd")
	api, worker := store(t, "fyniq_api"), store(t, "fyniq_worker")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	person, other := founder(t, api, a.Org, 1, now), founder(t, api, b.Org, 2, now)
	conversation := func(org, user uuid.UUID, asked time.Time, deleted bool) uuid.UUID {
		var id uuid.UUID
		if err := api.Do(ctx, org, func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := u.AIConversations().Create(ctx, storage.AIConversation{UserID: user, CreatedAt: asked})
			if err != nil {
				return err
			}
			id = c.ID
			tu, err := u.AIConversations().Ask(ctx, storage.AITurn{Conversation: c.ID, UserID: user, Question: "What was revenue?", AskedAt: asked})
			if err != nil {
				return err
			}
			at := asked.Add(time.Second)
			tu.Status, tu.Answer, tu.AnsweredAt = storage.TurnAnswered, "Answered.", &at
			if err := u.AIConversations().Finish(ctx, tu, []storage.AIToolCall{{Tool: "checks.list", Input: json.RawMessage(`{}`), Outcome: "read"}}); err != nil {
				return err
			}
			if _, err := u.AIConversations().Give(ctx, storage.AIFeedback{Turn: tu.ID, UserID: user, Rating: "up", GivenAt: at}); err != nil {
				return err
			}
			if deleted {
				return u.AIConversations().Delete(ctx, c.ID, at)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	old := conversation(a.Org, person, now.AddDate(-2, 0, 0), false)
	gone := conversation(a.Org, person, now.Add(-time.Hour), true)
	recent := conversation(a.Org, person, now.Add(-time.Hour), false)
	elsewhere := conversation(b.Org, other, now.AddDate(-2, 0, 0), false)

	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, _, err := u.AIConversations().Purge(ctx, now.AddDate(-1, 0, 0))
		return err
	}); err == nil {
		t.Error("the API purged conversations")
	}
	var conversations, turns int
	if err := worker.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		conversations, turns, err = u.AIConversations().Purge(ctx, now.AddDate(-1, 0, 0))
		return err
	}); err != nil || conversations != 2 || turns != 2 {
		t.Fatalf("the purge: %d conversations, %d turns: %v", conversations, turns, err)
	}
	for id, kept := range map[uuid.UUID]bool{old: false, gone: false, recent: true} {
		if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.AIConversations().Get(ctx, id)
			if kept != (err == nil) {
				t.Errorf("conversation %s kept %v: %v", id, kept, err)
			}
			turns, err := u.AIConversations().Turns(ctx, id)
			if err == nil && kept != (len(turns) == 1) {
				t.Errorf("conversation %s's turns: %d", id, len(turns))
			}
			return err
		}); err != nil && err != ports.ErrNotFound {
			t.Fatal(err)
		}
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIConversations().Get(ctx, elsewhere)
		return err
	}); err != nil {
		t.Errorf("another organisation's conversation: %v", err)
	}

	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the worker removing a conversation itself", "fyniq_worker", `DELETE FROM fyniq.ai_conversation`, []string{"42501"}},
		{"the worker setting the purge's flag and removing", "fyniq_worker", `SELECT set_config('fyniq.ai_purge', 'on', true); DELETE FROM fyniq.ai_turn`, []string{"42501"}},
		{"the API running the purge", "fyniq_api", `SELECT * FROM fyniq.ai_purge_conversations(now())`, []string{"42501"}},
		{"the AI running the purge", "fyniq_ai", `SELECT * FROM fyniq.ai_purge_conversations(now())`, []string{"42501"}},
		{"the owner removing a turn outside the purge", "fyniq_migrator", `DELETE FROM fyniq.ai_tool_call`, []string{"FY341"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}
	_, err := conn(t, "fyniq_worker").Exec(ctx, `SELECT * FROM fyniq.ai_purge_conversations(now())`)
	wantState(t, "the purge outside any organisation", err, "FY341")
}
