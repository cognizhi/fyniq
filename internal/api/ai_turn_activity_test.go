//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The assistant's turns in the AI activity log (AI-12, AI-P8; design D11): each member reads their own and the Owner
// everyone's, each turn with the prompt it was told, its runs, the classes it read and its reads, never their input;
// someone else's turn that read payroll is masked to a reader who may not see payroll unmasked, and the Owner's
// unmasked read of it is audited (FR-PAY-13, AUD-05).
// req: AI-12
// req: AI-P8
// asvs: V8.2.2, V16.3.3
func TestTheAssistantsTurnsInTheActivityLog(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	turn := func(user, question string, classes []string) uuid.UUID {
		var id uuid.UUID
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			c, err := u.AIConversations().Create(ctx, storage.AIConversation{UserID: uuid.MustParse(user), CreatedAt: e.clock.Now()})
			if err != nil {
				return err
			}
			tu, err := u.AIConversations().Ask(ctx, storage.AITurn{Conversation: c.ID, UserID: c.UserID, Question: question, AskedAt: e.clock.Now()})
			if err != nil {
				return err
			}
			at := e.clock.Now().Add(time.Second)
			tu.Status, tu.Answer, tu.Prompt, tu.PromptSHA256, tu.Classes, tu.AnsweredAt = storage.TurnAnswered, "Answered.", "chat@2",
				"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", classes, &at
			id = tu.ID
			return u.AIConversations().Finish(ctx, tu, []storage.AIToolCall{{Tool: "metrics.query", Version: 1, Input: json.RawMessage(`{"measure":"revenue"}`),
				Outcome: "read", Facts: 2, OutputSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
		})
		e.clock.Advance(time.Minute)
		return id
	}
	turn(f.ahkow, "What was revenue?", []string{"business-figures"})
	pay := turn(f.weiling, "What does the payroll cost?", []string{"business-figures", "payroll"})
	list := func(as string) []map[string]any {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/ai/activity/turns", as, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("listAITurnActivity: %d %s", r.Status, r.Raw)
		}
		var out []map[string]any
		for _, x := range r.Body["items"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}
	mine := list(f.as(f.ahkow, authz.Controller))
	if len(mine) != 1 || mine[0]["question"] != "What was revenue?" || mine[0]["prompt"] != "chat@2" ||
		mine[0]["tool_calls"].([]any)[0].(map[string]any)["tool"] != "metrics.query" || mine[0]["tool_calls"].([]any)[0].(map[string]any)["input"] != nil {
		t.Errorf("a Controller's own turns: %v", mine)
	}
	everyone := list(f.as(f.ahkow, authz.Owner))
	if len(everyone) != 2 || everyone[0]["question"] != "What does the payroll cost?" || everyone[0]["masked"] != false {
		t.Errorf("the Owner's view: %v", everyone)
	}
	if r := e.call(http.MethodGet, "/v1/audit/events?action=ai_activity.read", f.as(f.weiling, authz.Owner), nil); r.Status != http.StatusOK ||
		!strings.Contains(r.Raw, pay.String()) {
		t.Errorf("the Owner's read of a payroll turn is not audited: %d %s", r.Status, r.Raw)
	}
	// The DPO reads everyone's but may see payroll only masked (Appendix D: Pay runs M).
	dpo := list(f.as(f.ahkow, authz.DPO))
	if len(dpo) != 2 || dpo[0]["masked"] != true || dpo[0]["question"] != nil || dpo[1]["question"] != "What was revenue?" {
		t.Errorf("the DPO's view: %v", dpo)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/ai/activity/turns?person="+f.weiling, f.as(f.ahkow, authz.Controller), nil), "listAITurnActivity", 403, CodeForbidden)
}

// The activity log records, for every read of the knowledge base, which pages and which versions of them an answer used
// (AI-12, AI-51; p1-knowledge-base design D4): "why did the assistant say that" is answered with the page's exact text, which
// the release or the organisation keeps per version. Other reads list none.
// req: AI-12
// asvs: V16.3.3
func TestTheActivityLogShowsTheKnowledgePagesAnAnswerUsed(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.AIConversations().Create(ctx, storage.AIConversation{UserID: uuid.MustParse(f.ahkow), CreatedAt: e.clock.Now()})
		if err != nil {
			return err
		}
		tu, err := u.AIConversations().Ask(ctx, storage.AITurn{Conversation: c.ID, UserID: c.UserID, Question: "How do I approve a claim?", AskedAt: e.clock.Now()})
		if err != nil {
			return err
		}
		at := e.clock.Now().Add(time.Second)
		tu.Status, tu.Answer, tu.Prompt, tu.PromptSHA256, tu.Classes, tu.AnsweredAt = storage.TurnAnswered, "Answered.", "chat@4",
			"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", []string{"know-how"}, &at
		return u.AIConversations().Finish(ctx, tu, []storage.AIToolCall{
			{Tool: "kb.search", Version: 1, Input: json.RawMessage(`{"query":"approve claim"}`), Outcome: "read", OutputSHA256: strings.Repeat("a", 64)},
			{Tool: "kb.read", Version: 1, Input: json.RawMessage(`{"key":"claims.approve-team-claims"}`), Outcome: "read", OutputSHA256: strings.Repeat("b", 64),
				KBPages: []storage.KBPageVersion{{Key: "claims.approve-team-claims", Version: "v0.1.0#7c1ea9b4f2d3"}}},
		})
	})
	r := e.call(http.MethodGet, "/v1/ai/activity/turns", f.as(f.ahkow, authz.Owner), nil)
	if r.Status != http.StatusOK {
		t.Fatalf("listAITurnActivity: %d %s", r.Status, r.Raw)
	}
	calls := r.Body["items"].([]any)[0].(map[string]any)["tool_calls"].([]any)
	search, read := calls[0].(map[string]any), calls[1].(map[string]any)
	if pages := search["kb_pages"].([]any); len(pages) != 0 {
		t.Errorf("a search lists page versions: %v", pages)
	}
	pages := read["kb_pages"].([]any)
	if len(pages) != 1 || pages[0].(map[string]any)["key"] != "claims.approve-team-claims" || pages[0].(map[string]any)["version"] != "v0.1.0#7c1ea9b4f2d3" {
		t.Errorf("the page version kb.read used: %v", read)
	}
}
