//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Feedback on the assistant's answers (QA-AI-05; task 6.4, design D13): a person rates an answer of their own
// conversation, up or down, with a reason, once; another's turn, a turn still answering and a second rating are
// refused. An Owner or Administrator reviews it with the question and the answer, and nobody else may; unreviewed
// feedback is not golden, and only what a reviewer marks golden is. Someone else's turn that read payroll is masked to
// the Administrator, who may only dismiss it; the Owner's unmasked read of it is audited (FR-PAY-13, AUD-05).
// req: QA-AI-05
// asvs: V8.2.2, V8.3.1
func TestFeedbackIsReviewedBeforeItFeedsTheEvalSet(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	turn := func(user, question string, classes []string, status string) (string, string) {
		var c storage.AIConversation
		var id uuid.UUID
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			var err error
			if c, err = u.AIConversations().Create(ctx, storage.AIConversation{UserID: uuid.MustParse(user), CreatedAt: e.clock.Now()}); err != nil {
				return err
			}
			tu, err := u.AIConversations().Ask(ctx, storage.AITurn{Conversation: c.ID, UserID: c.UserID, Question: question, AskedAt: e.clock.Now()})
			if err != nil || status == storage.TurnAnswering {
				id = tu.ID
				return err
			}
			at := e.clock.Now().Add(time.Second)
			tu.Status, tu.Answer, tu.Prompt, tu.Classes, tu.AnsweredAt = status, "July's revenue was SGD 48,200.00.", "chat@2", classes, &at
			id = tu.ID
			return u.AIConversations().Finish(ctx, tu, nil)
		})
		e.clock.Advance(time.Minute)
		return c.ID.String(), id.String()
	}
	revenueC, revenue := turn(f.weiling, "What was July's revenue?", []string{"business-figures"}, storage.TurnAnswered)
	payC, pay := turn(f.weiling, "What does the payroll cost?", []string{"business-figures", "payroll"}, storage.TurnAnswered)
	busyC, busy := turn(f.weiling, "And August?", nil, storage.TurnAnswering)
	weiling, owner, admin := f.as(f.weiling, authz.Employee), f.as(f.ahkow, authz.Owner), f.as(f.ahkow, authz.Administrator)
	give := func(as, c, tu string, body map[string]any) response {
		return e.call(http.MethodPost, "/v1/assistant/conversations/"+c+"/turns/"+tu+"/feedback", as, body)
	}

	// A rating needs a reason; it is kept once, waiting for review, and shown with the turn to its person.
	if r := give(weiling, revenueC, revenue, map[string]any{"rating": "down"}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a rating without a reason: %d %s", r.Status, r.Raw)
	}
	r := give(weiling, revenueC, revenue, map[string]any{"rating": "down", "reason": "It used June's figures, not July's."})
	if r.Status != http.StatusCreated || r.str("state") != "waiting" || r.str("question") != "What was July's revenue?" {
		t.Fatalf("giveAIFeedback: %d %s", r.Status, r.Raw)
	}
	given := r.str("id")
	e.clock.Advance(time.Minute)
	e.problemIs(give(weiling, revenueC, revenue, map[string]any{"rating": "up", "reason": "Changed my mind"}), "giveAIFeedback", 409, storage.CodeDuplicate)
	e.problemIs(give(f.as(f.ahkow, authz.Employee), revenueC, revenue, map[string]any{"rating": "up", "reason": "Not mine"}), "giveAIFeedback", 404, CodeNotFound)
	e.problemIs(give(weiling, payC, revenue, map[string]any{"rating": "up", "reason": "Wrong conversation"}), "giveAIFeedback", 404, CodeNotFound)
	e.problemIs(give(weiling, busyC, busy, map[string]any{"rating": "up", "reason": "Too soon"}), "giveAIFeedback", 409, CodeAIFeedbackTurn)
	if r := give(weiling, payC, pay, map[string]any{"rating": "up", "reason": "Clear"}); r.Status != http.StatusCreated {
		t.Fatalf("feedback on the payroll turn: %d %s", r.Status, r.Raw)
	}
	rated := func() map[string]any {
		t.Helper()
		turns := e.call(http.MethodGet, "/v1/assistant/conversations/"+revenueC, weiling, nil).Body["turns"].([]any)
		fb, _ := turns[0].(map[string]any)["feedback"].(map[string]any)
		return fb
	}
	if fb := rated(); fb["rating"] != "down" || fb["state"] != "waiting" || fb["reason"] != "It used June's figures, not July's." {
		t.Errorf("the turn's rating: %v", fb)
	}

	list := func(as, query string) []map[string]any {
		t.Helper()
		r := e.call(http.MethodGet, "/v1/ai/feedback"+query, as, nil)
		if r.Status != http.StatusOK {
			t.Fatalf("listAIFeedback%s: %d %s", query, r.Status, r.Raw)
		}
		var out []map[string]any
		for _, x := range r.Body["items"].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}
	// Unreviewed feedback is not golden; only an Owner or Administrator reviews.
	if golden := list(owner, "?state=golden"); len(golden) != 0 {
		t.Errorf("unreviewed feedback listed as golden: %v", golden)
	}
	for _, role := range []authz.Role{authz.Controller, authz.Employee, authz.DPO} {
		as := f.as(f.ahkow, role)
		e.problemIs(e.call(http.MethodGet, "/v1/ai/feedback", as, nil), "listAIFeedback", 403, CodeForbidden)
		e.problemIs(e.call(http.MethodPost, "/v1/ai/feedback/"+given+"/review", as, map[string]any{"decision": "golden"}), "reviewAIFeedback", 403, CodeForbidden)
	}

	// The Owner sees payroll: both unmasked, and showing the payroll turn's feedback is audited.
	all := list(owner, "?state=waiting")
	if len(all) != 2 || all[0]["question"] != "What does the payroll cost?" || all[0]["masked"] != false || all[1]["reason"] != "It used June's figures, not July's." ||
		all[1]["answer"] != "July's revenue was SGD 48,200.00." {
		t.Errorf("the Owner's list: %v", all)
	}
	payFeedback := all[0]["id"].(string)
	if r := e.call(http.MethodGet, "/v1/audit/events?action=ai_activity.read", owner, nil); r.Status != http.StatusOK || !strings.Contains(r.Raw, payFeedback) {
		t.Errorf("the Owner's read of payroll feedback is not audited: %d %s", r.Status, r.Raw)
	}
	// The Administrator does not see payroll: masked, and it may only be dismissed.
	if mine := list(admin, "?rating=up"); len(mine) != 1 || mine[0]["masked"] != true || mine[0]["question"] != nil || mine[0]["reason"] != nil {
		t.Errorf("the Administrator's view of payroll feedback: %v", mine)
	}
	review := func(as, id, decision string) response {
		return e.call(http.MethodPost, "/v1/ai/feedback/"+id+"/review", as, map[string]any{"decision": decision})
	}
	e.problemIs(review(admin, payFeedback, "golden"), "reviewAIFeedback", 409, CodeAIFeedbackMasked)
	if r := review(admin, payFeedback, "dismissed"); r.Status != http.StatusOK || r.str("state") != "dismissed" || r.str("question") != "" {
		t.Errorf("dismissing masked feedback: %d %s", r.Status, r.Raw)
	}

	// Golden once reviewed, and only then.
	r = review(admin, given, "golden")
	if r.Status != http.StatusOK || r.str("state") != "golden" || r.str("reviewed_by") != "user:"+f.ahkow {
		t.Fatalf("reviewAIFeedback: %d %s", r.Status, r.Raw)
	}
	e.problemIs(review(owner, given, "dismissed"), "reviewAIFeedback", 409, CodeAIFeedbackDone)
	if golden := list(owner, "?state=golden"); len(golden) != 1 || golden[0]["id"] != given || golden[0]["question"] != "What was July's revenue?" {
		t.Errorf("the golden feedback: %v", golden)
	}
	if waiting := list(owner, "?state=waiting"); len(waiting) != 0 {
		t.Errorf("reviewed feedback still waiting: %v", waiting)
	}
	if fb := rated(); fb["state"] != "golden" {
		t.Errorf("the turn's rating after review: %v", fb)
	}
	if r := e.call(http.MethodGet, "/v1/audit/events?action=ai_feedback.reviewed", owner, nil); r.Status != http.StatusOK || !strings.Contains(r.Raw, given) {
		t.Errorf("the review is not audited: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/ai/feedback?page_size=1&cursor=nonsense", owner, nil), "listAIFeedback", 422, CodeCursor)
	if page := e.call(http.MethodGet, "/v1/ai/feedback?page_size=1", owner, nil); page.str("next_cursor") == "" {
		t.Errorf("a page of one has no next cursor: %s", page.Raw)
	} else if next := e.call(http.MethodGet, "/v1/ai/feedback?page_size=1&cursor="+page.str("next_cursor"), owner, nil); !strings.Contains(next.Raw, given) {
		t.Errorf("the second page: %s", next.Raw)
	}
}
