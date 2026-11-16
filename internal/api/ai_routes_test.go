//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// routable is an enabled connection whose data is processed in country, with models listed, probed and allowed, and
// priced, as each step of a route needs (AI-72, AI-75, AI-82); unpriced models are allowed but have no price.
func routable(e *env, name, country string, priced []string, unpriced ...string) uuid.UUID {
	e.t.Helper()
	return routableOn(e, storage.AIConnection{Name: name, Connector: "openai", Auth: "api-key", Configuration: []byte(`{}`), Host: "api.openai.com"},
		country, priced, unpriced...)
}

// routableOn is routable for a connection to c's provider, with c's configuration.
func routableOn(e *env, c storage.AIConnection, country string, priced []string, unpriced ...string) uuid.UUID {
	e.t.Helper()
	name := c.Name
	now := e.clock.Now()
	approval := uuid.Must(uuid.NewV7())
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		entry, err := u.Compliance().SaveInventoryEntry(ctx, storage.InventoryEntry{What: "Prompts to " + name, Why: "The AI assistant", Where: "The provider",
			Who: "The founders", Retention: "None", Transfers: "None", State: "open", SavedBy: "user:routes", SavedAt: now, Version: 1})
		if err != nil {
			return err
		}
		handling := `{"inventory_id":"` + entry.EntryID.String() + `","handling":[{"model":"*","endpoint":"api.openai.com","feature":"*",` +
			`"processed_in":["` + country + `"],"training":"no","evidence":{"kind":"source","url":"https://example.com/terms","title":"Terms","checked_on":"2026-10-01"}}]}`
		c.Billing, c.DataHandling, c.CreatedBy, c.CreatedAt = "usage", []byte(handling), "user:routes", now
		if c.Auth == "api-key" {
			c.CredentialRef, c.CredentialSetAt = "routes/"+strings.ReplaceAll(name, " ", "-"), &now
		}
		if c, err = u.AIConnections().Create(ctx, c, approval); err != nil {
			return err
		}
		var models []storage.AIModel
		for _, m := range append(append([]string{}, priced...), unpriced...) {
			models = append(models, storage.AIModel{ConnectionID: c.ID, Model: m, FirstListed: now, LastListed: now, Listed: true})
		}
		if _, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: c.ID, Trigger: "refresh", Outcome: "complete", Pages: 1,
			Added: len(models), ReadBy: "user:routes", ReadAt: now}, models); err != nil {
			return err
		}
		for _, m := range models {
			if _, err := u.AIModels().Probe(ctx, c.ID, m.Model, true, false, "answered", now); err != nil {
				return err
			}
			if _, err := u.AIModels().Allow(ctx, c.ID, m.Model, true, &approval); err != nil {
				return err
			}
		}
		c.State, c.ChangedBy, c.ChangedAt = "enabled", "user:routes", now
		_, err = u.AIConnections().Step(ctx, c, storage.AIConnectionEnabled, &approval)
		return err
	})
	worker := workerStore(e.t)
	digest := strings.Repeat("ef", 32)
	cat, err := worker.RecordAIPriceCatalogue(context.Background(), storage.AIPriceCatalogue{Source: "litellm", URL: "https://example.com/prices.json",
		FetchedAt: now, SHA256: digest, LicenceURL: "https://example.com/LICENSE", LicenceSHA256: digest, Outcome: "rejected",
		Failure: "a fixture of the routes, never the latest valid download", Document: "ai/price-catalogues/litellm/" + digest + ".json"})
	if err != nil {
		e.t.Fatal(err)
	}
	if err := worker.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		for _, m := range priced {
			if _, err := u.AIPrices().Record(ctx, storage.AIPrice{ConnectionID: c.ID, Model: m, Source: "litellm", CatalogueID: &cat.ID, CatalogueKey: m,
				Match: "exact", Currency: "USD", Status: "active", RecordedBy: "system:ai.prices.read", RecordedAt: now,
				Rates: []storage.AIPriceRate{{Kind: "input", Unit: "per_million_tokens", Amount: "2"}, {Kind: "output", Unit: "per_million_tokens", Amount: "8"}}}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	return c.ID
}

// Every agent runs on a route set per agent (AI-74, AI-26): its first route, and any version that names a connection
// or model, or a class, the one in force did not, waits for a second approver; one that only narrows is saved at once.
// Each step names a connection that is on and a model on its allow-list, priced where the connection bills by usage,
// that the data-class policy lets every class go to; a class the agent never handles, a stale version or an unknown
// agent is refused. A step whose model later leaves the allow-list shows that it cannot serve.
// req: AI-74
// req: AI-26
// req: AI-82
func TestEveryAgentRunsOnItsRoute(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	sg := routable(e, "Singapore", "SG", []string{"gpt-6"}, "gpt-5")
	us := routable(e, "Overseas", "US", []string{"gpt-6"})
	list := e.call(http.MethodGet, "/v1/ai/routes", admin, nil)
	agents, _ := list.Body["agents"].([]any)
	if list.Status != http.StatusOK || len(agents) < 8 || agents[0].(map[string]any)["agent"] != "chat" || agents[0].(map[string]any)["route"] != nil {
		t.Fatalf("getAIRoutes: %d %s", list.Status, list.Raw)
	}
	step := func(conn uuid.UUID, model string) map[string]any {
		return map[string]any{"connection_id": conn.String(), "model": model, "cache": true}
	}
	set := func(as, agent, match string, classes []string, steps ...map[string]any) response {
		return e.call(http.MethodPut, "/v1/ai/routes/"+agent, steppedUp(e, as), map[string]any{"classes": classes, "steps": steps}, "If-Match", match)
	}

	// The first route waits for a second approver.
	first := set(admin, "chat", `"0"`, []string{"business-figures", "personal-data"}, step(sg, "gpt-6"))
	approvalBody, _ := first.Body["approval"].(map[string]any)
	if first.Status != http.StatusAccepted || approvalBody == nil || approvalBody["operation"] != "ai_route.change" || first.Body["route"] != nil {
		t.Fatalf("the first route: %d %s", first.Status, first.Raw)
	}
	asked := response{Status: http.StatusAccepted, Body: approvalBody}
	if d := f.decide(admin, asked.str("id"), "approve", hashOf(asked)); d.Status == http.StatusOK {
		t.Errorf("the requester approving their own route: %s", d.Raw)
	}
	if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("the second approver: %d %s", d.Status, d.Raw)
	}
	got := e.call(http.MethodGet, "/v1/ai/routes/chat", admin, nil)
	route, _ := got.Body["route"].(map[string]any)
	if got.Status != http.StatusOK || got.Header.Get("ETag") != `"1"` || route == nil || route["approval"] != asked.str("id") ||
		route["steps"].([]any)[0].(map[string]any)["serves"] != true {
		t.Fatalf("the route in force: %d %s", got.Status, got.Raw)
	}

	// Narrowing is saved at once; a stale version, a widening step or class each go their own way.
	narrow := set(admin, "chat", `"1"`, []string{"business-figures"}, map[string]any{"connection_id": sg.String(), "model": "gpt-6", "effort": "high", "cache": false})
	if r, _ := narrow.Body["route"].(map[string]any); narrow.Status != http.StatusOK || r == nil || r["version"] != float64(2) || narrow.Body["approval"] != nil {
		t.Fatalf("narrowing: %d %s", narrow.Status, narrow.Raw)
	}
	e.problemIs(set(admin, "chat", `"1"`, []string{"business-figures"}, step(sg, "gpt-6")), "setAIRoute", 412, CodeStale)
	if wide := set(admin, "chat", `"2"`, []string{"business-figures"}, step(sg, "gpt-6"), step(us, "gpt-6")); wide.Status != http.StatusAccepted {
		t.Errorf("a fallback on another connection: %d %s", wide.Status, wide.Raw)
	}
	e.problemIs(set(admin, "chat", `"2"`, []string{"business-figures", "personal-data"}, step(us, "gpt-6")), "setAIRoute", 422, "AI-ROUTE-02")
	e.problemIs(set(admin, "chat", `"2"`, []string{"business-figures"}, step(sg, "gpt-5")), "setAIRoute", 409, "AI-PRICE-01")
	e.problemIs(set(admin, "chat", `"2"`, []string{"business-figures"}, step(sg, "gpt-4")), "setAIRoute", 422, "AI-ROUTE-02")
	e.problemIs(set(admin, "report_commentary", `"0"`, []string{"personal-data"}, step(sg, "gpt-6")), "setAIRoute", 422, "AI-ROUTE-01")
	e.problemIs(set(admin, "dreamer", `"0"`, []string{"know-how"}, step(sg, "gpt-6")), "setAIRoute", 404, CodeNotFound)
	if r := e.call(http.MethodPut, "/v1/ai/routes/chat", steppedUp(e, admin), map[string]any{"classes": []string{"business-figures"},
		"steps": []any{map[string]any{"connection_id": sg.String(), "model": "gpt-6", "effort": "low", "cache": true}}},
		"If-Match", `"2"`, "Prefer", "return=preview"); r.Status != http.StatusOK || r.Body["route"] == nil {
		t.Errorf("a preview: %d %s", r.Status, r.Raw)
	}
	if v := e.call(http.MethodGet, "/v1/ai/routes/chat", admin, nil); v.Header.Get("ETag") != `"2"` {
		t.Errorf("a preview saved a version: %s", v.Header.Get("ETag"))
	}

	// A model that leaves the allow-list leaves the route by itself: the step shows it cannot serve.
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIModels().Allow(ctx, sg, "gpt-6", false, nil)
		return err
	})
	after := e.call(http.MethodGet, "/v1/ai/routes/chat", admin, nil)
	s := after.Body["route"].(map[string]any)["steps"].([]any)[0].(map[string]any)
	if s["serves"] != false || !strings.Contains(s["why"].(string), "allow-list") || len(after.Body["versions"].([]any)) != 2 {
		t.Errorf("a step that cannot serve: %s", after.Raw)
	}
	var changed int
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.Audit().Find(ctx, storage.AuditQuery{ObjectType: "ai_route", Action: "ai_route.changed", Limit: 10})
		changed = len(all)
		return err
	})
	if changed != 2 {
		t.Errorf("the trail records %d route changes", changed)
	}
}

// matrixRouteConnection is the matrix's routable connection, made once.
func matrixRouteConnection(e *env) string {
	e.t.Helper()
	var id string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.AIConnections().List(ctx)
		for _, c := range all {
			if c.Name == "Matrix routes" {
				id = c.ID.String()
			}
		}
		return err
	})
	if id == "" {
		id = routable(e, "Matrix routes", "SG", []string{"gpt-6"}).String()
	}
	return id
}

// The presets give each agent the route that pins the newest allowed model of its lines (AI-26): A in Singapore with
// every class the agent handles, B on the global endpoint without personal data or payroll, C by the agent's classes;
// the route is set from it as any other, naming its preset.
// req: AI-26
// req: AI-74
func TestPresetsGiveEachAgentItsRoute(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	claude := func(name, location string) storage.AIConnection {
		return storage.AIConnection{Name: name, Connector: "anthropic", Auth: "workload-identity",
			Configuration: []byte(`{"endpoint":"agent-platform","project":"fyniq-test","location":"` + location + `"}`), Host: location + "-aiplatform.googleapis.com"}
	}
	sg := routableOn(e, claude("Claude in Singapore", "asia-southeast1"), "SG", []string{"claude-opus-4-7", "claude-sonnet-4-6"})
	global := routableOn(e, claude("Claude global", "global"), "US", []string{"claude-opus-5-5", "claude-haiku-5-5"})
	r := e.call(http.MethodGet, "/v1/ai/presets", admin, nil)
	if r.Status != http.StatusOK {
		t.Fatalf("getAIPresets: %d %s", r.Status, r.Raw)
	}
	agentOf := func(preset, agent string) map[string]any {
		for _, p := range r.Body["presets"].([]any) {
			if p.(map[string]any)["id"] != preset {
				continue
			}
			for _, a := range p.(map[string]any)["agents"].([]any) {
				if a.(map[string]any)["agent"] == agent {
					return a.(map[string]any)
				}
			}
		}
		t.Fatalf("no %s for %s", preset, agent)
		return nil
	}
	steps := func(a map[string]any) string {
		var out []string
		for _, s := range a["route"].(map[string]any)["steps"].([]any) {
			id := s.(map[string]any)["connection_id"].(string)
			where := map[string]string{sg.String(): "sg", global.String(): "global"}[id]
			out = append(out, s.(map[string]any)["model"].(string)+"@"+where)
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct{ preset, agent, steps, why string }{
		{"A", "chat", "claude-opus-4-7@sg,claude-sonnet-4-6@sg", ""},
		{"A", "bank_categorisation", "claude-sonnet-4-6@sg,claude-opus-4-7@sg", ""},
		{"B", "chat", "claude-opus-5-5@global,claude-haiku-5-5@global", ""},
		{"C", "chat", "claude-opus-4-7@sg,claude-sonnet-4-6@sg", ""},
		{"C", "report_commentary", "claude-opus-5-5@global,claude-haiku-5-5@global", ""},
	} {
		a := agentOf(tc.preset, tc.agent)
		if got := steps(a); got != tc.steps || a["why"] != nil {
			t.Errorf("%s for %s: %s %v", tc.preset, tc.agent, got, a["why"])
		}
	}
	if b := agentOf("B", "chat"); len(b["notes"].([]any)) != 2 || len(b["route"].(map[string]any)["classes"].([]any)) != 2 {
		t.Errorf("B leaves personal data and payroll in Singapore: %v", b)
	}
	if kb := agentOf("B", "kb_compiler"); kb["route"] == nil {
		t.Errorf("the compiler under B: %v", kb)
	}

	// The route is set from the preset as any other: the agent's first route waits for a second approver.
	a := agentOf("A", "chat")
	asked := e.call(http.MethodPut, "/v1/ai/routes/chat", steppedUp(e, admin), a["route"], "If-Match", `"0"`)
	approvalBody, _ := asked.Body["approval"].(map[string]any)
	if asked.Status != http.StatusAccepted || approvalBody == nil {
		t.Fatalf("setting A: %d %s", asked.Status, asked.Raw)
	}
	req := response{Status: http.StatusAccepted, Body: approvalBody}
	if d := f.decide(owner, req.str("id"), "approve", hashOf(req)); d.Status != http.StatusOK {
		t.Fatalf("approving A: %d %s", d.Status, d.Raw)
	}
	if got := e.call(http.MethodGet, "/v1/ai/routes/chat", admin, nil); got.Body["route"].(map[string]any)["preset"] != "A" {
		t.Errorf("the route's preset: %s", got.Raw)
	}
}
