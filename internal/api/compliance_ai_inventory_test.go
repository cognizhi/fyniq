//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The AI inventory (CMP-AI-02), beside the AI assistant's impact assessment in the Compliance Center: each connection
// with its endpoint, where prompts are processed, whether the provider trains on them, its allowed models and the
// classes the routes send it; each agent's route; the data categories; the prompts and tools, none yet. The assessment
// is current once approved after the AI's last change, and needs reviewing again after a later route change.
// req: CMP-AI-02
// req: CMP-PD-08
func TestTheAIInventory(t *testing.T) {
	e := newEnv(t)
	dpo := e.personAs(uuid.New(), authz.DPO)
	sg := routable(e, "Claude in Singapore", "SG", []string{"gpt-6"})
	us := routable(e, "OpenAI in the US", "US", []string{"gpt-5"})
	route := func(version int, at time.Time) {
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: version, Classes: []string{"business-figures", "personal-data"},
				MadeBy: "user:inventory", MadeAt: at, Steps: []storage.AIRouteStep{{Position: 1, ConnectionID: sg, Model: "gpt-6"}}})
			return err
		})
	}
	route(1, e.clock.Now().Add(-time.Hour))
	get := func() response { return e.call(http.MethodGet, "/v1/compliance/ai-inventory", dpo, nil) }

	inv := get()
	conns, _ := inv.Body["connections"].([]any)
	if inv.Status != http.StatusOK || len(conns) != 2 {
		t.Fatalf("getAIInventory: %d %s", inv.Status, inv.Raw)
	}
	first, second := conns[0].(map[string]any), conns[1].(map[string]any)
	if first["name"] != "Claude in Singapore" || first["id"] != sg.String() || first["endpoint"] != "api.openai.com" || first["state"] != "enabled" ||
		first["processed_in"].([]any)[0] != "SG" || first["training"] != "no" || first["models"].([]any)[0] != "gpt-6" ||
		len(first["classes"].([]any)) != 2 || first["asked_by"] != "user:routes" {
		t.Errorf("the Singapore connection: %v", first)
	}
	if second["id"] != us.String() || second["processed_in"].([]any)[0] != "US" || len(second["classes"].([]any)) != 0 {
		t.Errorf("the US connection: %v", second)
	}
	routes, _ := inv.Body["routes"].([]any)
	if len(routes) != 1 || routes[0].(map[string]any)["agent"] != "chat" ||
		routes[0].(map[string]any)["steps"].([]any)[0].(map[string]any)["connection"] != "Claude in Singapore" ||
		len(inv.Body["classes"].([]any)) != 2 || len(inv.Body["prompts"].([]any)) != 0 || len(inv.Body["tools"].([]any)) != 0 {
		t.Errorf("the routes and categories: %s", inv.Raw)
	}
	if d := inv.Body["dpia"].(map[string]any); d["key"] != "ai-assistant" || d["current"] != false || d["approved"] != nil {
		t.Errorf("the assessment before its approval: %v", d)
	}

	// Approved after the last change, it is current; a later route change needs it reviewed again.
	if a := e.call(http.MethodPost, "/v1/compliance/dpias/ai-assistant/approvals", dpo, nil, "If-Match", `"v0"`, "Idempotency-Key", "ai-dpia-1"); a.Status != 201 {
		t.Fatalf("approving the AI assessment: %d %s", a.Status, a.Raw)
	}
	if d := get().Body["dpia"].(map[string]any); d["current"] != true {
		t.Errorf("the assessment once approved: %v", d)
	}
	route(2, e.clock.Now().Add(time.Hour))
	if d := get().Body["dpia"].(map[string]any); d["current"] != false || d["approved"] == nil {
		t.Errorf("the assessment after a later change: %v", d)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/compliance/ai-inventory", e.as(authz.Employee), nil), "getAIInventory", 403, CodeForbidden)
}
