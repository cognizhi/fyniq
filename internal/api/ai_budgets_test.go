//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aibudget"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// pricedInBase prices a connection's model in the organisation's base currency, as a read would: 2 per million input
// tokens and 8 per million output.
func pricedInBase(e *env, connection uuid.UUID, model string) {
	e.t.Helper()
	worker := workerStore(e.t)
	digest := strings.Repeat("ab", 32)
	cat, err := worker.RecordAIPriceCatalogue(context.Background(), storage.AIPriceCatalogue{Source: "litellm", URL: "https://example.com/prices.json",
		FetchedAt: e.clock.Now(), SHA256: digest, LicenceURL: "https://example.com/LICENSE", LicenceSHA256: digest, Outcome: "rejected",
		Failure: "a fixture of the budgets, never the latest valid download", Document: "ai/price-catalogues/litellm/" + digest + ".json"})
	if err != nil {
		e.t.Fatal(err)
	}
	if err := worker.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIPrices().Record(ctx, storage.AIPrice{ConnectionID: connection, Model: model, Source: "litellm", CatalogueID: &cat.ID, CatalogueKey: model,
			Match: "exact", Currency: "SGD", Status: "active", RecordedBy: "system:ai.prices.read", RecordedAt: e.clock.Now(),
			Rates: []storage.AIPriceRate{{Kind: "input", Unit: "per_million_tokens", Amount: "2"}, {Kind: "output", Unit: "per_million_tokens", Amount: "8"}}})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
}

// Budgets admit only what they cover (AI-77, AI-86, AI-13): the Owner sets a monthly budget in the base currency; the
// router reserves each attempt's most against every budget that applies, settles it to the attempt's exact cost, and
// alerts the Owners once when a budget reaches 80%; a call the agent's budget cannot cover stops, while a connection's
// spent budget leaves the next step to serve; a probe reserves like any call. The use shows on the budgets and in the
// month's usage.
// req: AI-77
// req: AI-86
// req: AI-13
func TestBudgetsAdmitOnlyWhatTheyCover(t *testing.T) {
	e := newEnv(t)
	aiSwitchedOn(e)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	adminOnly := f.as(f.ahkow, authz.Administrator)
	sg := routable(e, "Budget primary", "SG", nil, "gpt-6")
	fb := routable(e, "Budget fallback", "SG", nil, "gpt-6")
	pricedInBase(e, sg, "gpt-6")
	pricedInBase(e, fb, "gpt-6")
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures"}, MadeBy: "user:budgets",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: "gpt-6"}, {ConnectionID: fb, Model: "gpt-6"}}})
		return err
	})
	models := map[uuid.UUID]*stepModel{sg: {caps: full}, fb: {caps: full}}
	router := ai.Router{Store: e.api.deps.Store, Clock: e.clock, Outbox: e.api.deps.Outbox,
		Provider: func(_ context.Context, _ storage.UnitOfWork, c storage.AIConnection) (ports.ModelProvider, error) {
			return models[c.ID], nil
		}}
	ask := ai.Call{Agent: "chat", Classes: []string{"business-figures"}, Actor: "user:" + f.ahkow, Request: ports.ModelRequest{MaxTokens: 1024,
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "What was revenue?"}}}}}}
	set := func(as, key, match string, amount any) response {
		return e.call(http.MethodPut, "/v1/ai/budgets/"+key, as, map[string]any{"amount": amount}, "If-Match", match)
	}

	e.problemIs(set(owner, "team", `"0"`, "5"), "setAIBudget", 422, "AI-BUDGET-01")
	e.problemIs(set(owner, "agent:dreamer", `"0"`, "5"), "setAIBudget", 404, CodeNotFound)
	if r := set(adminOnly, "agent:chat", `"0"`, "0.01"); r.Status != http.StatusForbidden {
		t.Errorf("an Administrator setting a budget: %d %s", r.Status, r.Raw)
	}
	if r := set(owner, "agent:chat", `"0"`, "0.01"); r.Status != http.StatusOK || r.str("amount") != "0.01" || r.Header.Get("ETag") != `"1"` {
		t.Fatalf("the agent's budget: %d %s", r.Status, r.Raw)
	}
	e.problemIs(set(owner, "agent:chat", `"0"`, "0.02"), "setAIBudget", 412, CodeStale)

	// The first call reserves its most, 7 input tokens at 2 and 1,024 output at 8 per million: 0.008206 of 0.01, so the
	// Owners are alerted; it settles to its cost, 1,000 at 2 and 500 at 8.
	ctx := context.Background()
	res, err := router.Run(ctx, e.books.Org, ask)
	if err != nil || res.Attempts[0].CostBase != "0.006" {
		t.Fatalf("the first call: %v %+v", err, res.Attempts)
	}
	key := uuid.NewSHA1(uuid.NameSpaceOID, []byte(e.books.Org.String()+" ai-budget agent chat "+aibudget.Month(e.clock.Now())))
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		yes, err := u.Notifications().Emitted(ctx, notify.AIBudgetType, key)
		if err == nil && !yes {
			t.Error("no alert at 80%")
		}
		return err
	})
	// The second could take it past 0.01: it is not sent.
	sent := models[sg].calls
	res, err = router.Run(ctx, e.books.Org, ask)
	var be *aibudget.Error
	if !errors.As(err, &be) || be.Code != aibudget.CodeSpent || models[sg].calls != sent || res.Attempts[0].Outcome != "stopped" {
		t.Fatalf("a call past the budget: %v %+v", err, res.Attempts)
	}
	got := e.call(http.MethodGet, "/v1/ai/budgets", admin, nil)
	budgets, _ := got.Body["budgets"].([]any)
	if got.Status != http.StatusOK || got.str("currency") != "SGD" || len(budgets) != 1 || budgets[0].(map[string]any)["used"] != "0.006" ||
		budgets[0].(map[string]any)["alerted"] != true {
		t.Fatalf("getAIBudgets: %d %s", got.Status, got.Raw)
	}

	// With the agent's budget raised, a connection's spent budget leaves the next step to serve.
	if r := set(owner, "agent:chat", `"1"`, "1"); r.Status != http.StatusOK {
		t.Fatalf("raising the budget: %d %s", r.Status, r.Raw)
	}
	if r := set(owner, "connection:"+sg.String(), `"0"`, "0.001"); r.Status != http.StatusOK {
		t.Fatalf("the connection's budget: %d %s", r.Status, r.Raw)
	}
	res, err = router.Run(ctx, e.books.Org, ask)
	if err != nil || res.Connection != fb || res.Attempts[0].Outcome != "skipped" || !strings.Contains(res.Attempts[0].Detail, "connection's AI budget") {
		t.Fatalf("a connection's spent budget: %v %+v", err, res.Attempts)
	}
	if r := set(owner, "connection:"+sg.String(), `"1"`, nil); r.Status != http.StatusOK || r.Body["amount"] != nil {
		t.Errorf("removing a budget: %d %s", r.Status, r.Raw)
	}

	// The month's use (AI-77): two served, one of them by the fallback, one skipped, one stopped before sending, at
	// 0.006 each.
	u := e.call(http.MethodGet, "/v1/ai/usage", admin, nil)
	total, _ := u.Body["total"].(map[string]any)
	if u.Status != http.StatusOK || total["requests"] != float64(2) || total["served"] != float64(2) || total["fallbacks"] != float64(1) ||
		total["skipped"] != float64(1) || total["cost"] != "0.012" || total["input_tokens"] != float64(2000) || len(u.Body["by_connection"].([]any)) != 2 ||
		u.Body["by_agent"].([]any)[0].(map[string]any)["agent"] != "chat" || u.Body["by_person"].([]any)[0].(map[string]any)["person"] != "user:"+f.ahkow {
		t.Errorf("getAIUsage: %d %s", u.Status, u.Raw)
	}
	e.problemIs(e.call(http.MethodGet, "/v1/ai/usage?month=2026-13", admin, nil), "getAIUsage", 422, CodeInvalid)

	// A probe reserves like any call: with the organisation's budget all but spent, it is not sent (AI-86).
	e.api.deps.AIProviders = func(_ context.Context, _ storage.UnitOfWork, c storage.AIConnection) (ports.ModelProvider, error) {
		return models[c.ID], nil
	}
	probe := func() response {
		return e.call(http.MethodPost, "/v1/ai/connections/"+sg.String()+"/models/probe", admin, map[string]any{"model": "gpt-6"})
	}
	if r := probe(); r.Status != http.StatusOK || r.Body["probe"].(map[string]any)["ok"] != true {
		t.Fatalf("a probe: %d %s", r.Status, r.Raw)
	}
	if r := set(owner, "organisation", `"0"`, "0.0001"); r.Status != http.StatusOK {
		t.Fatalf("the organisation's budget: %d %s", r.Status, r.Raw)
	}
	before := models[sg].calls
	e.problemIs(probe(), "probeAIConnectionModel", 409, aibudget.CodeSpent)
	if models[sg].calls != before {
		t.Error("a probe past the budget was sent")
	}
	if u := e.call(http.MethodGet, "/v1/ai/usage", admin, nil); !strings.Contains(u.Raw, `"agent":"probe"`) {
		t.Errorf("the probe in the usage: %s", u.Raw)
	}

	// A subscription shows its quota used in its window and when it resets (AI-73).
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIConnections().Create(ctx, storage.AIConnection{Name: "Kimi plan", Connector: "openai-compatible", Preset: "kimi", Auth: "api-key",
			Configuration: []byte(`{}`), Host: "api.moonshot.ai", Billing: "subscription",
			Quota:     []byte(`{"amount":1000000,"unit":"tokens","window":"month","fee":"19.00","currency":"USD"}`),
			CreatedBy: "user:budgets", CreatedAt: e.clock.Now()}, uuid.Must(uuid.NewV7()))
		return err
	})
	u = e.call(http.MethodGet, "/v1/ai/usage", admin, nil)
	var quota map[string]any
	for _, c := range u.Body["by_connection"].([]any) {
		if c.(map[string]any)["name"] == "Kimi plan" {
			quota, _ = c.(map[string]any)["quota"].(map[string]any)
		}
	}
	if quota == nil || quota["amount"] != float64(1000000) || quota["used"] != float64(0) || quota["resets"] == nil {
		t.Errorf("a subscription's quota: %s", u.Raw)
	}
}
