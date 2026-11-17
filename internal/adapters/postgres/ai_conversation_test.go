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

// The assistant's conversations only go forward, whoever writes (AI-12, AI-P8; p1-ai-assistant design D11): a
// conversation is deleted once and then left out of its person's list, a turn is answered once with the reads it made,
// feedback is given once, each audited; none of it changes against its course or is removed but by the purge, even by
// the owner of the schema; the worker writes none of it and the AI's role reads none of it (CMP-AI-01); another
// organisation sees none of it.
// req: AI-12
// req: AI-P8
// req: QA-ENG-05
// asvs: V8.2.2, V16.3.3
func TestAttackAIConversationsOnlyGoForward(t *testing.T) {
	ctx := context.Background()
	a, b := books(t, "Conversations A Pte Ltd"), books(t, "Conversations B Pte Ltd")
	api := store(t, "fyniq_api")
	now := clock.System{}.Now().UTC().Truncate(time.Microsecond)
	person := founder(t, api, a.Org, 1, now)
	var conv storage.AIConversation
	var turn storage.AITurn
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		if conv, err = u.AIConversations().Create(ctx, storage.AIConversation{UserID: person, Title: "Revenue", CreatedAt: now}); err != nil {
			return err
		}
		turn, err = u.AIConversations().Ask(ctx, storage.AITurn{Conversation: conv.ID, UserID: person, Question: "What was revenue?",
			Context: json.RawMessage(`{"kind":"report","label":"P&L"}`), AskedAt: now})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	answered := now.Add(time.Second)
	finish := func(status, code string) error {
		return api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			t := turn
			t.Status, t.Code, t.Answer, t.Prompt, t.PromptSHA256, t.AnsweredAt = status, code, "Revenue was SGD 100.00.", "chat@2",
				"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", &answered
			t.Cited, t.Runs = []string{"F00000000000000aa"}, []uuid.UUID{uuid.New()}
			return u.AIConversations().Finish(ctx, t, []storage.AIToolCall{{Tool: "metrics.query", Version: 1, Input: json.RawMessage(`{"measure":"revenue"}`),
				Outcome: "read", Facts: 1}, {Tool: "records.get", Version: 1, Input: json.RawMessage(`{}`), Outcome: "refused", Code: "AI-TOOL-03"}})
		})
	}
	if err := finish(storage.TurnAnswered, ""); err != nil {
		t.Fatal(err)
	}
	if err := finish(storage.TurnAnswered, ""); err == nil {
		t.Error("a turn answered twice")
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		turns, err := u.AIConversations().Turns(ctx, conv.ID)
		if err != nil {
			return err
		}
		calls, err := u.AIConversations().ToolCalls(ctx, turn.ID)
		if err != nil {
			return err
		}
		if len(turns) != 1 || turns[0].Status != storage.TurnAnswered || turns[0].Prompt != "chat@2" || len(calls) != 2 || calls[1].N != 2 ||
			calls[1].Code != "AI-TOOL-03" {
			t.Errorf("the turn: %+v %+v", turns, calls)
		}
		asked, err := u.AIConversations().Asked(ctx, person, now.Add(-time.Minute))
		if err == nil && asked != 1 {
			t.Errorf("asked: %d", asked)
		}
		_, err = u.AIConversations().Give(ctx, storage.AIFeedback{Turn: turn.ID, UserID: person, Rating: "down", Reason: "Wrong month", GivenAt: answered})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIConversations().Give(ctx, storage.AIFeedback{Turn: turn.ID, UserID: person, Rating: "up", GivenAt: answered})
		return err
	}); err == nil {
		t.Error("feedback given twice")
	}
	del := func() error {
		return api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
			return u.AIConversations().Delete(ctx, conv.ID, answered)
		})
	}
	if err := del(); err != nil {
		t.Fatal(err)
	}
	if err := del(); err != ports.ErrNotFound {
		t.Errorf("deleted twice: %v", err)
	}
	if err := api.Do(ctx, a.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		list, err := u.AIConversations().OfUser(ctx, person, uuid.Nil, 50)
		if err == nil && len(list) != 0 {
			t.Errorf("a deleted conversation listed: %+v", list)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}

	for _, x := range []struct {
		what, role, stmt string
		states           []string
	}{
		{"the owner undeleting a conversation", "fyniq_migrator", `UPDATE fyniq.ai_conversation SET deleted_at = NULL`, []string{"FY341"}},
		{"the owner giving it to another", "fyniq_migrator", `UPDATE fyniq.ai_conversation SET user_id = gen_random_uuid()`, []string{"FY341", "23503"}},
		{"the owner rewriting a question", "fyniq_migrator", `UPDATE fyniq.ai_turn SET question = 'Something else'`, []string{"FY341"}},
		{"the owner rewriting an answer", "fyniq_migrator", `UPDATE fyniq.ai_turn SET answer = 'Revenue was SGD 1,000,000.00.'`, []string{"FY341"}},
		{"the owner hiding what a turn read", "fyniq_migrator", `UPDATE fyniq.ai_turn SET classes = '{}'`, []string{"FY341"}},
		{"a class that is none", "fyniq_migrator", `INSERT INTO fyniq.ai_turn (conversation_id, user_id, question, asked_at, classes)
		  SELECT conversation_id, user_id, 'x', now(), '{secrets}' FROM fyniq.ai_turn LIMIT 1`, []string{"23514"}},
		{"the owner changing a tool call", "fyniq_migrator", `UPDATE fyniq.ai_tool_call SET outcome = 'read', code = ''`, []string{"FY341"}},
		{"the owner changing feedback", "fyniq_migrator", `UPDATE fyniq.ai_feedback SET rating = 'up'`, []string{"FY341"}},
		{"the owner removing turns", "fyniq_migrator", `DELETE FROM fyniq.ai_turn`, []string{"FY341", "23503"}},
		{"the owner removing a conversation", "fyniq_migrator", `DELETE FROM fyniq.ai_conversation`, []string{"FY341", "23503"}},
		{"the owner emptying them", "fyniq_migrator", `TRUNCATE fyniq.ai_feedback, fyniq.ai_tool_call, fyniq.ai_turn, fyniq.ai_conversation`, []string{"FY060", "0A000"}},
		{"the API removing a turn", "fyniq_api", `DELETE FROM fyniq.ai_turn`, []string{"42501"}},
		{"the API changing a tool call", "fyniq_api", `UPDATE fyniq.ai_tool_call SET facts = 0`, []string{"42501"}},
		{"the worker asking", "fyniq_worker", `INSERT INTO fyniq.ai_conversation (user_id, title, created_at) VALUES (gen_random_uuid(), 'x', now())`, []string{"42501"}},
		{"the AI reading conversations", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_conversation`, []string{"42501"}},
		{"the AI reading turns", "fyniq_ai", `SELECT count(*) FROM fyniq.ai_turn`, []string{"42501"}},
		{"the AI writing feedback", "fyniq_ai", `INSERT INTO fyniq.ai_feedback (turn_id, user_id, rating, given_at) VALUES (gen_random_uuid(), gen_random_uuid(), 'up', now())`, []string{"42501"}},
	} {
		err := asOrg(ctx, conn(t, x.role), a.Org, func(tx pgx.Tx) error { _, err := tx.Exec(ctx, x.stmt); return err })
		wantState(t, x.what, err, x.states...)
	}
	if err := api.Do(ctx, b.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIConversations().Get(ctx, conv.ID)
		if err != ports.ErrNotFound {
			t.Errorf("another organisation reads the conversation: %v", err)
		}
		turns, err := u.AIConversations().Turns(ctx, conv.ID)
		if err == nil && len(turns) != 0 {
			t.Errorf("another organisation reads its turns: %d", len(turns))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
