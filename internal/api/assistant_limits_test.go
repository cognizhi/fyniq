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

	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The assistant's limits (AI-23, design D9): questions a minute, questions in one conversation and reads for one
// question are settings; a turn past a limit is refused in words before it is kept; past 20 earlier turns, the oldest
// are summarised in one message that keeps the facts they cited, which a later answer may cite again (AI-08).
// req: AI-23
// req: AI-08
func TestTheAssistantAnswersWithinItsLimits(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	sg := routable(e, "Limits", "SG", []string{"gpt-6"})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures", "personal-data"}, MadeBy: "user:test",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: "gpt-6"}}})
		return err
	})
	words, _ := json.Marshal(assistant.Structured{Segments: []assistant.Segment{{Text: "The books hold no answer to that yet."}}})
	plain := ports.ModelResponse{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(words)}}, StopReason: ports.StopEnd}
	model := &scripted{}
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return model, nil
	}
	withAIService(e)
	ahkow := f.as(f.ahkow)
	y, m, d := e.clock.Now().Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	set := func(key, from, to string, version int) {
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Settings().Record(ctx, key, from, to, today, version)
			return err
		})
	}
	conversation := func() string {
		return e.call(http.MethodPost, "/v1/assistant/conversations", ahkow, map[string]any{}).str("id")
	}
	ask := func(id, question string) (string, map[string]any) {
		t.Helper()
		kinds, data := events(e.call(http.MethodPost, "/v1/assistant/conversations/"+id+"/turns", ahkow, map[string]any{"question": question}).Raw)
		return kinds[len(kinds)-1], data[len(data)-1]
	}
	turns := func(id string) int {
		return len(e.call(http.MethodGet, "/v1/assistant/conversations/"+id, ahkow, nil).Body["turns"].([]any))
	}

	// One question a minute: the second is refused before it is kept, and a minute later it is answered.
	set(settingAIPerMinute, "20", "1", 1)
	c := conversation()
	model.answers = []ports.ModelResponse{plain, plain}
	if kind, data := ask(c, "What was revenue?"); kind != "answer" || data["status"] != "answered" {
		t.Fatalf("the first question: %s %v", kind, data)
	}
	if kind, data := ask(c, "And costs?"); kind != "error" || data["code"] != CodeAIPerMinute || turns(c) != 1 {
		t.Errorf("a second question in the minute: %s %v", kind, data)
	}
	e.clock.Advance(61 * time.Second)
	if kind, _ := ask(c, "And costs?"); kind != "answer" {
		t.Errorf("a minute later: %s", kind)
	}

	// Two questions in a conversation: the third asks for a new one.
	set(settingAIConversation, "40", "2", 1)
	e.clock.Advance(61 * time.Second)
	if kind, data := ask(c, "And cash?"); kind != "error" || data["code"] != CodeAIConversationLimit || !strings.Contains(data["fix"].(string), "new conversation") {
		t.Errorf("a full conversation: %s %v", kind, data)
	}

	// One read a question: a model asking for two is answered in part, saying so.
	set(settingAIReads, "8", "1", 1)
	model.answers = []ports.ModelResponse{{Parts: []ports.ModelPart{
		{Kind: ports.PartToolCall, CallID: "c1", Tool: "checks_list", Input: json.RawMessage(`{}`)},
		{Kind: ports.PartToolCall, CallID: "c2", Tool: "checks_list", Input: json.RawMessage(`{}`)}}, StopReason: ports.StopToolCall}}
	if kind, data := ask(conversation(), "Everything at once"); kind != "answer" || !strings.Contains(data["answer"].(string), "more than 1 reads") {
		t.Errorf("past the reads: %s %v", kind, data)
	}

	// Past 20 earlier turns the oldest are summarised, their facts kept: the answer cites the first turn's fact again.
	set(settingAIConversation, "2", "40", 2)
	long := conversation()
	earlier := aitool.Fact{ID: "F00000000000000e1", Measure: "revenue", Value: "100.00", Unit: aitool.UnitAmount, Currency: "SGD", From: "2026-07-01",
		To: "2026-07-31", Revision: 1, Source: "the profit and loss's revenue", Lineage: "/v1/reports/statements/pl?from=2026-07&to=2026-07"}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		for i := range 21 {
			tu, err := u.AIConversations().Ask(ctx, storage.AITurn{Conversation: uuid.MustParse(long), UserID: uuid.MustParse(f.ahkow),
				Question: "Question " + string(rune('A'+i)), AskedAt: e.clock.Now().Add(-time.Hour + time.Duration(i)*time.Second)})
			if err != nil {
				return err
			}
			at := tu.AskedAt.Add(time.Second)
			tu.Status, tu.Answer, tu.AnsweredAt = storage.TurnAnswered, "Answered.", &at
			if i == 0 {
				tu.Cited, tu.Facts = []string{earlier.ID}, mustJSON(t, []aitool.Fact{earlier})
			}
			if err := u.AIConversations().Finish(ctx, tu, nil); err != nil {
				return err
			}
		}
		return nil
	})
	again, _ := json.Marshal(assistant.Structured{Segments: []assistant.Segment{{Text: "July's revenue was "},
		{Fact: earlier.ID, Measure: "revenue", From: earlier.From, To: earlier.To, Currency: "SGD"}, {Text: "."}}})
	model.answers = []ports.ModelResponse{{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(again)}}, StopReason: ports.StopEnd}}
	model.requests = nil
	e.clock.Advance(61 * time.Second)
	kind, data := ask(long, "What was July's revenue again?")
	if kind != "answer" || data["answer"] != "July's revenue was SGD 100.00." || data["regenerated"] != false {
		t.Fatalf("an earlier fact cited again: %s %v", kind, data)
	}
	first := model.requests[0].Messages[0].Parts[0].Text
	if !strings.HasPrefix(first, "Earlier in this conversation, summarised") || !strings.Contains(first, "Question A") || !strings.Contains(first, earlier.ID) ||
		strings.Contains(first, "Question B Answered") {
		t.Errorf("the summary: %q", first)
	}
	if n := len(model.requests[0].Messages); n != 2+2*20+1 {
		t.Errorf("%d messages: the summary, 20 turns whole and the question", n)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
