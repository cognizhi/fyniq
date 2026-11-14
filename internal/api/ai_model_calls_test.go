//go:build local

package api

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The AI service's model calls go through the API's router (PRD-001 §9.3 rev 0.277; p1-ai-assistant design D4, the
// owner's decision D11 (a) of p1-ai-providers): only with the AI service's service token and a live turn's delegated
// token; for the chat agent's route, as the turn's person; routed for the classes the AI service names and every class
// the turn has read, so it can never send what it read where the policy keeps it from; recorded in the activity log;
// streamed as server-sent events too. The provider keys never leave the API.
// req: AI-P2
// req: ai/proposals The orchestrator holds no write credential
// req: AI-83
// asvs: V8.2.1, V8.3.1
func TestTheAIServicesModelCallsRunTheRoutersRules(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	aiSwitchedOn(e)
	overseas := routable(e, "Calls overseas", "US", []string{"gpt-6"})
	model := &stepModel{caps: full}
	e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
		return model, nil
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures", "personal-data"}, MadeBy: "user:calls",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: overseas, Model: "gpt-6"}}})
		return err
	})
	turn := uuid.New()
	var token string
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(f.ahkow), turn)
		return err
	})
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	ask := func(path, bearer, svc string, classes ...string) response {
		body := map[string]any{"agent": "chat", "classes": classes, "request": map[string]any{"max_tokens": 64,
			"messages": []any{map[string]any{"role": "user", "parts": []any{map[string]any{"kind": "text", "text": "What was revenue?"}}}}}}
		return e.call(http.MethodPost, "/internal/v1/"+path, bearer, body, ServiceTokenHeader, svc)
	}

	// The AI service's identity and a live turn are both needed.
	if r := ask("model-calls", token, "", "business-figures"); r.Status != http.StatusUnauthorized {
		t.Errorf("without the service token: %d %s", r.Status, r.Raw)
	}
	other, _ := e.services.Token("worker")
	if r := ask("model-calls", token, other, "business-figures"); r.Status != http.StatusUnauthorized {
		t.Errorf("another service's token: %d %s", r.Status, r.Raw)
	}
	if r := ask("model-calls", f.as(f.ahkow, authz.Owner), service, "business-figures"); r.Status != http.StatusUnauthorized {
		t.Errorf("a person's own credential instead of the turn's: %d %s", r.Status, r.Raw)
	}

	// Business figures go to the overseas step, as the person, and are recorded.
	r := ask("model-calls", token, service, "business-figures")
	if r.Status != http.StatusOK || r.Body["response"].(map[string]any)["model"] != "gpt-6" || model.calls != 1 {
		t.Fatalf("a call: %d %s", r.Status, r.Raw)
	}
	run := r.str("run")
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		calls, err := u.AICalls().OfRun(ctx, uuid.MustParse(run))
		if err == nil && (len(calls) != 1 || calls[0].Actor != "user:"+f.ahkow || calls[0].Agent != "chat") {
			t.Errorf("the recorded call: %+v", calls)
		}
		return err
	})
	// The stream: text as it comes, then the served call.
	s := ask("model-calls:stream", token, service, "business-figures")
	if s.Status != http.StatusOK || !strings.Contains(s.Raw, "event: text") || !strings.Contains(s.Raw, "event: done") || model.calls != 2 {
		t.Errorf("a streamed call: %d %s", s.Status, s.Raw)
	}

	// Once the turn has read personal data, a call naming only business figures still carries it: nothing goes overseas.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		return u.AIGrants().Widen(ctx, turn, []string{"personal-data"}, e.clock.Now())
	})
	sent := model.calls
	if r := ask("model-calls", token, service, "business-figures"); r.Status != http.StatusConflict || r.str("code") != airoute.CodeNoStep ||
		r.str("instance") == "" || r.str("fix") == "" {
		t.Errorf("personal data overseas: %d %s", r.Status, r.Raw)
	}
	codes, _ := e.api.internal.OpenAPI().Paths["/internal/v1/model-calls"].Post.Extensions["x-fyniq-error-codes"].([]string)
	if !slices.Contains(codes, airoute.CodeNoStep) {
		t.Errorf("the internal contract's codes: %v", codes)
	}
	if model.calls != sent {
		t.Error("personal data went overseas")
	}
	if !strings.Contains(ask("model-calls:stream", token, service, "business-figures").Raw, airoute.CodeNoStep) || model.calls != sent {
		t.Error("a streamed call carrying personal data overseas")
	}

	// Another agent is not the AI service's to call, and the turn's end ends its calls.
	if r := e.call(http.MethodPost, "/internal/v1/model-calls", token, map[string]any{"agent": "company_secretary", "classes": []any{},
		"request": map[string]any{"max_tokens": 1, "messages": []any{}}}, ServiceTokenHeader, service); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("another agent: %d %s", r.Status, r.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error { return e.api.endAIGrant(ctx, u, turn) })
	if r := ask("model-calls", token, service, "business-figures"); r.Status != http.StatusUnauthorized {
		t.Errorf("after the turn: %d %s", r.Status, r.Raw)
	}
}
