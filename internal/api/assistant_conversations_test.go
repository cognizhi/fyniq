//go:build local

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// withAIService starts the AI service beside e's API: the real assistant over HTTP, answering for the API with its
// service token and calling the API's internal operations with its own.
func withAIService(e *env) {
	e.t.Helper()
	prompt, err := assistant.DefaultPrompt(assistant.Agent)
	if err != nil {
		e.t.Fatal(err)
	}
	client := &assistant.Client{URL: e.srv.URL + "/internal/v1", ServiceToken: func(context.Context) (string, error) { return e.services.Token(ServiceAI) },
		HTTP: e.srv.Client()}
	srv := httptest.NewServer(assistant.Handler{Assistant: &assistant.Assistant{Backend: client, Prompt: prompt}, Callers: e.services})
	e.t.Cleanup(srv.Close)
	e.api.deps.Assistant = &AssistantService{URL: srv.URL, Token: func(context.Context) (string, error) { return e.services.Token(assistant.Class) },
		HTTP: srv.Client()}
}

// matrixConversations are the authorisation matrix's rows for the assistant's conversations: each as the caller's own.
var matrixConversations = map[string]func(e *env, tok string) (response, func() bool){
	"createAIConversation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/assistant/conversations", steppedUp(e, tok), map[string]any{"title": "Matrix"}), nil
	},
	"listAIConversations": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/assistant/conversations", steppedUp(e, tok), nil), nil
	},
	"getAIConversation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/assistant/conversations/"+matrixConversation(e, tok), steppedUp(e, tok), nil), nil
	},
	"deleteAIConversation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodDelete, "/v1/assistant/conversations/"+matrixConversation(e, tok), steppedUp(e, tok), nil), nil
	},
	"listAITurnActivity": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/activity/turns", steppedUp(e, tok), nil), nil
	},
	"askAIConversation": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/assistant/conversations/"+matrixConversation(e, tok)+"/turns", steppedUp(e, tok),
			map[string]any{"question": "What was revenue?"}), nil
	},
	"giveAIFeedback": func(e *env, tok string) (response, func() bool) {
		c := matrixConversation(e, tok)
		return e.call(http.MethodPost, "/v1/assistant/conversations/"+c+"/turns/"+matrixAnswered(e, c).String()+"/feedback",
			steppedUp(e, tok), map[string]any{"rating": "up", "reason": "Matrix"}), nil
	},
	"listAIFeedback": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/feedback", steppedUp(e, tok), nil), nil
	},
	"reviewAIFeedback": func(e *env, tok string) (response, func() bool) {
		turn := matrixAnswered(e, matrixConversation(e, tok))
		var id uuid.UUID
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			fb, err := u.AIConversations().Give(ctx, storage.AIFeedback{Turn: turn, UserID: uuid.MustParse(matrixApprover(e)), Rating: "up", Reason: "Matrix",
				GivenAt: e.clock.Now()})
			id = fb.ID
			return err
		})
		return e.call(http.MethodPost, "/v1/ai/feedback/"+id.String()+"/review", steppedUp(e, tok), map[string]any{"decision": "dismissed"}), nil
	},
}

// matrixAnswered is an answered turn in a conversation, made directly in the store.
func matrixAnswered(e *env, conversation string) uuid.UUID {
	var id uuid.UUID
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.AIConversations().Get(ctx, uuid.MustParse(conversation))
		if err != nil {
			return err
		}
		t, err := u.AIConversations().Ask(ctx, storage.AITurn{Conversation: c.ID, UserID: c.UserID, Question: "What was revenue?", AskedAt: e.clock.Now()})
		if err != nil {
			return err
		}
		at := e.clock.Now()
		t.Status, t.Answer, t.AnsweredAt = storage.TurnAnswered, "Answered.", &at
		id = t.ID
		return u.AIConversations().Finish(ctx, t, nil)
	})
	return id
}

// matrixConversation is a conversation of the stepped-up principal behind tok, made directly in the store.
func matrixConversation(e *env, tok string) string {
	stepped := steppedUp(e, tok)
	e.tokens.mu.Lock()
	p := e.tokens.m[stepped]
	e.tokens.mu.Unlock()
	user := p.UserID
	if user == uuid.Nil {
		user = uuid.MustParse(matrixApprover(e))
	}
	var id uuid.UUID
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		c, err := u.AIConversations().Create(ctx, storage.AIConversation{UserID: user, Title: "Matrix", CreatedAt: e.clock.Now()})
		id = c.ID
		return err
	})
	return id.String()
}

// events are a server-sent stream's events, as kind and data.
func events(raw string) (kinds []string, data []map[string]any) {
	for _, block := range strings.Split(raw, "\n\n") {
		var kind, body string
		for _, line := range strings.Split(block, "\n") {
			if k, ok := strings.CutPrefix(line, "event: "); ok {
				kind = k
			}
			if d, ok := strings.CutPrefix(line, "data: "); ok {
				body = d
			}
		}
		if kind != "" {
			var m map[string]any
			_ = json.Unmarshal([]byte(body), &m)
			kinds, data = append(kinds, kind), append(data, m)
		}
	}
	return kinds, data
}

// A person's conversations with the assistant (AI-P1, AI-P8, AI-12, FR-UX-17; design D1, D11): theirs alone; a turn
// asked through the API is admitted, answered by the AI service as the person through the read tools, relayed as
// server-sent events and kept with its reads, its figures rendered from their facts; its token ends with it; the AI
// switched off refuses a turn before it is kept, and the AI service missing ends one in words, kept refused; a deleted conversation is
// gone from its person's list; no AI or MCP client reaches any of it (§9.6).
// req: AI-P1
// req: AI-P8
// req: AI-12
// req: FR-UX-17
// asvs: V8.2.1, V8.2.2
func TestAPersonsConversationsWithTheAssistant(t *testing.T) {
	e := newEnv(t)
	kpiBooks(e)
	f := foundersIn(e)
	aiSwitchedOn(e)
	sg := routable(e, "Conversations", "SG", []string{"gpt-6"})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures", "personal-data"}, MadeBy: "user:test",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: "gpt-6"}}})
		return err
	})
	direct := e.call(http.MethodPost, "/v1/metrics:query", e.as(authz.Controller), map[string]any{"measure": "revenue", "from": "2026-11", "to": "2026-11"})
	fact := direct.Body["facts"].([]any)[0].(map[string]any)
	cited, _ := json.Marshal(assistant.Structured{Segments: []assistant.Segment{{Text: "Revenue in November 2026 was "},
		{Fact: fact["id"].(string), Measure: "revenue", From: fact["from"].(string), To: fact["to"].(string), Currency: "SGD"}, {Text: "."}}})
	model := &scripted{answers: []ports.ModelResponse{
		{Parts: []ports.ModelPart{{Kind: ports.PartToolCall, CallID: "c1", Tool: "metrics_query", Input: json.RawMessage(`{"measure":"revenue","from":"2026-11","to":"2026-11"}`)}},
			StopReason: ports.StopToolCall},
		{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(cited)}}, StopReason: ports.StopEnd},
	}}
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return model, nil
	}
	withAIService(e)
	ahkow, weiling := f.as(f.ahkow), f.as(f.weiling)

	c := e.call(http.MethodPost, "/v1/assistant/conversations", ahkow, map[string]any{"title": "Revenue"})
	if c.Status != http.StatusCreated || c.str("title") != "Revenue" {
		t.Fatalf("createAIConversation: %d %s", c.Status, c.Raw)
	}
	path := "/v1/assistant/conversations/" + c.str("id")
	if l := e.call(http.MethodGet, "/v1/assistant/conversations", ahkow, nil); len(l.Body["items"].([]any)) != 1 {
		t.Errorf("their list: %s", l.Raw)
	}
	if l := e.call(http.MethodGet, "/v1/assistant/conversations", weiling, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("another's list: %s", l.Raw)
	}
	e.problemIs(e.call(http.MethodGet, path, weiling, nil), "getAIConversation", 404, CodeNotFound)

	// A turn: asked, answered as the person, relayed, kept.
	ask := func(as string, body map[string]any) ([]string, []map[string]any) {
		t.Helper()
		r := e.call(http.MethodPost, path+"/turns", as, body)
		if r.Status != http.StatusOK || !strings.Contains(r.Header.Get("Content-Type"), "text/event-stream") {
			t.Fatalf("askAIConversation: %d %s", r.Status, r.Raw)
		}
		return events(r.Raw)
	}
	kinds, data := ask(ahkow, map[string]any{"question": "What was revenue in November?", "context": map[string]any{"kind": "report", "label": "Profit and loss"}})
	last := data[len(data)-1]
	if kinds[0] != "status" || kinds[len(kinds)-1] != "answer" || last["status"] != "answered" ||
		!strings.HasPrefix(last["answer"].(string), "Revenue in November 2026 was SGD ") || len(last["cited"].([]any)) != 1 {
		t.Fatalf("the turn's events: %v %v", kinds, last)
	}
	turnID := uuid.MustParse(last["id"].(string))
	g := e.call(http.MethodGet, path, ahkow, nil)
	turns := g.Body["turns"].([]any)
	first := turns[0].(map[string]any)
	if len(turns) != 1 || first["context"].(map[string]any)["label"] != "Profit and loss" || first["segments"].([]any)[1].(map[string]any)["figure"] == nil {
		t.Errorf("getAIConversation: %s", g.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		calls, err := u.AIConversations().ToolCalls(ctx, turnID)
		if err != nil {
			return err
		}
		if len(calls) != 1 || calls[0].Tool != "metrics.query" || calls[0].Outcome != "read" || len(calls[0].OutputSHA256) != 64 {
			t.Errorf("the turn's reads: %+v", calls)
		}
		if _, err := u.AIGrants().Classes(ctx, turnID, e.clock.Now()); err != ports.ErrNotFound {
			t.Errorf("the turn's token after it: %v", err)
		}
		return nil
	})

	// The AI service missing, then the AI switched off: each turn ends in words, kept.
	service := e.api.deps.Assistant
	e.api.deps.Assistant = nil
	if kinds, data = ask(ahkow, map[string]any{"question": "And October?"}); kinds[len(kinds)-1] != "error" || data[len(data)-1]["code"] != CodeAIService {
		t.Errorf("no AI service: %v %v", kinds, data)
	}
	e.api.deps.Assistant = service
	if off := e.call(http.MethodPost, "/v1/ai/settings/switch-off", f.as(f.ahkow, authz.Owner), nil); off.Status != http.StatusOK {
		t.Fatalf("switchOffAI: %d %s", off.Status, off.Raw)
	}
	kinds, data = ask(ahkow, map[string]any{"question": "And October?"})
	if kinds[len(kinds)-1] != "error" || data[len(data)-1]["code"] != airoute.CodeSwitchOff || len(kinds) != 1 {
		t.Errorf("switched off: %v %v", kinds, data)
	}
	g = e.call(http.MethodGet, path, ahkow, nil)
	// The turn without the AI service is kept refused with its code; the AI switched off refused it before keeping one.
	statuses := []string{}
	for _, tu := range g.Body["turns"].([]any) {
		statuses = append(statuses, tu.(map[string]any)["status"].(string)+" "+fmt.Sprint(tu.(map[string]any)["code"]))
	}
	if strings.Join(statuses, ",") != "answered <nil>,refused "+CodeAIService {
		t.Errorf("the turns kept: %v", statuses)
	}

	// Only a person asks: an agent's principal is refused by the AI activity log's AI column.
	agent := e.principal(authn.Principal{Client: "claude-desktop", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner},
		UserID: uuid.MustParse(f.ahkow)})
	if r := e.call(http.MethodPost, "/v1/assistant/conversations", agent, map[string]any{}); r.Status != http.StatusForbidden {
		t.Errorf("an MCP client starting a conversation: %d %s", r.Status, r.Raw)
	}

	// Deleted: gone from the list and not found.
	if r := e.call(http.MethodDelete, path, ahkow, nil); r.Status != http.StatusNoContent {
		t.Fatalf("deleteAIConversation: %d %s", r.Status, r.Raw)
	}
	e.problemIs(e.call(http.MethodGet, path, ahkow, nil), "getAIConversation", 404, CodeNotFound)
	if l := e.call(http.MethodGet, "/v1/assistant/conversations", ahkow, nil); len(l.Body["items"].([]any)) != 0 {
		t.Errorf("a deleted conversation listed: %s", l.Raw)
	}
}
