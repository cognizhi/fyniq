//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/assistant"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// scripted is a model that answers in order, recording what it was sent.
type scripted struct {
	answers  []ports.ModelResponse
	requests []ports.ModelRequest
}

func (m *scripted) Capabilities() ports.ModelCapabilities { return full }
func (m *scripted) Call(_ context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	m.requests = append(m.requests, r)
	a := m.answers[0]
	m.answers = m.answers[1:]
	a.Model, a.Usage = r.Model, ports.ModelUsage{Input: 100, Output: 20}
	return a, nil
}
func (m *scripted) Stream(ctx context.Context, r ports.ModelRequest, _ func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	return m.Call(ctx, r)
}
func (m *scripted) ListModels(context.Context, string) (ports.ModelPage, error) {
	return ports.ModelPage{}, nil
}

// The AI service answers through the API alone (AI-P2, design D3, D4): the real assistant, over HTTP with the AI
// service's token and the turn's, asks the router for the chat agent's model, runs the read tool the model calls as the
// person, gives the model the facts it read, and answers with them; every model call is the person's in the activity
// log. A tool that read personal data widens the turn's classes, so its later calls are routed for them (AI-83).
// req: AI-P2
// req: ai/proposals The orchestrator holds no write credential
// req: AI-83
// asvs: V8.2.1
func TestTheAssistantAnswersThroughTheAPI(t *testing.T) {
	e := newEnv(t)
	kpiBooks(e)
	f := foundersIn(e)
	aiSwitchedOn(e)
	sg := routable(e, "Assistant", "SG", []string{"gpt-6"})
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures", "personal-data"}, MadeBy: "user:assistant",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: "gpt-6"}}})
		return err
	})
	direct := e.call(http.MethodPost, "/v1/metrics:query", e.as(authz.Controller), map[string]any{"measure": "revenue", "from": "2026-11", "to": "2026-11"})
	want := direct.Body["facts"].([]any)[0].(map[string]any)
	cited, _ := json.Marshal(assistant.Structured{Segments: []assistant.Segment{{Text: "Revenue in November 2026 was "},
		{Fact: want["id"].(string), Measure: "revenue", From: want["from"].(string), To: want["to"].(string), Currency: "SGD"}, {Text: "."}}})
	model := &scripted{answers: []ports.ModelResponse{
		{Parts: []ports.ModelPart{{Kind: ports.PartToolCall, CallID: "c1", Tool: "metrics_query", Input: json.RawMessage(`{"measure":"revenue","from":"2026-11","to":"2026-11"}`)}},
			StopReason: ports.StopToolCall},
		{Parts: []ports.ModelPart{{Kind: ports.PartText, Text: string(cited)}}, StopReason: ports.StopEnd},
	}}
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return model, nil
	}
	turn := uuid.New()
	var token string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(f.ahkow), turn)
		return err
	})
	prompt, err := assistant.DefaultPrompt(assistant.Agent)
	if err != nil {
		t.Fatal(err)
	}
	client := &assistant.Client{URL: e.srv.URL + "/internal/v1", ServiceToken: func(context.Context) (string, error) { return e.services.Token(ServiceAI) },
		HTTP: e.srv.Client()}
	out, err := (&assistant.Assistant{Backend: client, Prompt: prompt}).Answer(context.Background(), assistant.Question{Token: token,
		Text: "What was revenue in November?"}, func(assistant.Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if out.Stop != assistant.StopAnswered || len(out.Facts) != 1 || out.Facts[0].ID != want["id"] || out.Facts[0].Value != want["value"] || len(out.Runs) != 2 ||
		out.Grounding.Regenerated || !strings.HasPrefix(out.Text, "Revenue in November 2026 was SGD ") {
		t.Fatalf("the answer: %+v", out)
	}
	first, second := model.requests[0], model.requests[1]
	if first.System != prompt.Text || !slices.ContainsFunc(first.Tools, func(t ports.ModelTool) bool { return t.Name == "metrics_query" }) {
		t.Errorf("the first request: %q tools %d", first.System[:40], len(first.Tools))
	}
	results := second.Messages[len(second.Messages)-1]
	if results.Parts[0].Kind != ports.PartToolResult || !strings.Contains(results.Parts[0].Text, want["id"].(string)) {
		t.Errorf("the tool's result as the model read it: %+v", results)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		for _, run := range out.Runs {
			calls, err := u.AICalls().OfRun(ctx, uuid.MustParse(run))
			if err != nil {
				return err
			}
			if len(calls) != 1 || calls[0].Actor != "user:"+f.ahkow || calls[0].Agent != "chat" {
				t.Errorf("the run %s: %+v", run, calls)
			}
		}
		return nil
	})

	// A record's read widens the turn's classes to personal data.
	contact := e.call(http.MethodPost, "/v1/contacts", e.as(authz.Accountant), map[string]any{"legal_name": "Tan Ah Kow", "individual": true, "customer": true})
	if _, err := client.CallTool(context.Background(), token, "records.get", json.RawMessage(`{"kind":"contact","id":"`+contact.str("id")+`"}`)); err != nil {
		t.Fatal(err)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		classes, err := u.AIGrants().Classes(ctx, turn, e.clock.Now())
		if err == nil && !slices.Contains(classes, "personal-data") {
			t.Errorf("the turn's classes after reading a contact: %v", classes)
		}
		return err
	})

	// Without the AI service's own token, the API refuses the client in its words.
	bad := &assistant.Client{URL: e.srv.URL + "/internal/v1", ServiceToken: func(context.Context) (string, error) { return "forged", nil }, HTTP: e.srv.Client()}
	if _, err := bad.CallTool(context.Background(), token, "checks.list", nil); err == nil || !strings.Contains(err.Error(), CodeUnauthenticated) {
		t.Errorf("a forged service token: %v", err)
	}
}
