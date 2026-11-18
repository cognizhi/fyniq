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
	"github.com/cognizhi/fyniq/internal/domain/aiconnection"
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

// Token budgets (AI-13): the organisation's and a person's month of tokens bound every call, whatever it costs. The
// Owner sets them as tokens:organisation and tokens:person:<id>; a call whose most tokens they cannot cover is not sent,
// its person's reaching 80% raises the alert, and the budgets show what the month used in tokens.
// req: AI-13
// req: AI-86
func TestTokenBudgetsBoundEveryCall(t *testing.T) {
	e := newEnv(t)
	aiSwitchedOn(e)
	f := foundersIn(e)
	owner := f.as(f.weiling, authz.Owner)
	sg := routable(e, "Tokens primary", "SG", nil, "gpt-6")
	pricedInBase(e, sg, "gpt-6")
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.AIRoutes().Save(ctx, storage.AIRoute{Agent: "chat", Version: 1, Classes: []string{"business-figures"}, MadeBy: "user:tokens",
			MadeAt: e.clock.Now(), Steps: []storage.AIRouteStep{{ConnectionID: sg, Model: "gpt-6"}}})
		return err
	})
	model := &stepModel{caps: full}
	router := ai.Router{Store: e.api.deps.Store, Clock: e.clock, Outbox: e.api.deps.Outbox,
		Provider: func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
			return model, nil
		}}
	ask := ai.Call{Agent: "chat", Classes: []string{"business-figures"}, Actor: "user:" + f.ahkow, Request: ports.ModelRequest{MaxTokens: 1024,
		Messages: []ports.ModelMessage{{Role: ports.RoleUser, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "What was revenue?"}}}}}}
	set := func(key, match string, amount any) response {
		return e.call(http.MethodPut, "/v1/ai/budgets/"+key, owner, map[string]any{"amount": amount}, "If-Match", match)
	}
	ctx := context.Background()
	spent := func(err error) bool {
		var be *aibudget.Error
		return errors.As(err, &be) && be.Code == aibudget.CodeSpent
	}

	e.problemIs(set("tokens:agent:chat", `"0"`, "1000"), "setAIBudget", 422, aibudget.CodeBudget)
	e.problemIs(set("tokens:organisation", `"0"`, "1.5"), "setAIBudget", 422, aibudget.CodeBudget)
	if r := set("tokens:organisation", `"0"`, "1000"); r.Status != http.StatusOK || r.str("unit") != "tokens" || r.str("key") != "tokens:organisation" {
		t.Fatalf("the organisation's tokens: %d %s", r.Status, r.Raw)
	}
	// A call that could use 1,024 tokens of output alone is not sent against 1,000.
	if _, err := router.Run(ctx, e.books.Org, ask); !spent(err) || model.calls != 0 {
		t.Fatalf("past the organisation's tokens: %v", err)
	}
	if r := set("tokens:organisation", `"1"`, "1000000"); r.Status != http.StatusOK {
		t.Fatalf("raising it: %d %s", r.Status, r.Raw)
	}
	if r := set("tokens:person:"+f.ahkow, `"0"`, "3000"); r.Status != http.StatusOK || r.str("subject") != "user:"+f.ahkow {
		t.Fatalf("a person's tokens: %d %s", r.Status, r.Raw)
	}
	// Each answer uses 1,500 tokens (1,000 in, 500 out), and a call reserves a little over 1,024 (its prompt and all its
	// output): the second call's reservation reaches 80% of the person's 3,000, and a third could not be covered.
	for i := range 2 {
		if _, err := router.Run(ctx, e.books.Org, ask); err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
	}
	if _, err := router.Run(ctx, e.books.Org, ask); !spent(err) {
		t.Errorf("past the person's tokens: %v", err)
	}
	key := uuid.NewSHA1(uuid.NameSpaceOID, []byte(e.books.Org.String()+" ai-budget tokens_person user:"+f.ahkow+" "+aibudget.Month(e.clock.Now())))
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		yes, err := u.Notifications().Emitted(ctx, notify.AIBudgetType, key)
		if err == nil && !yes {
			t.Error("no alert at 80% of the person's tokens")
		}
		return err
	})
	got := e.call(http.MethodGet, "/v1/ai/budgets", owner, nil)
	for _, b := range got.Body["budgets"].([]any) {
		b := b.(map[string]any)
		if b["key"] == "tokens:person:"+f.ahkow && (b["used"] != "3000" || b["unit"] != "tokens" || b["alerted"] != true) {
			t.Errorf("the person's tokens: %v", b)
		}
	}
}

// A plan that draws its quota at different rates per model (AI-73): the connection keeps each model's rate with the
// plan's published terms it comes from, and the month's usage meters each model at its rate, rounded down; rates
// without their terms are refused.
// req: AI-73
// req: AI-77
func TestAPlansQuotaDrawsAtEachModelsRate(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Administrator), f.as(f.weiling, authz.Owner)
	quota := map[string]any{"amount": 300, "unit": "requests", "window": "month", "fee": "19.00", "currency": "USD",
		"rates": []any{map[string]any{"model": "kimi-big", "rate": "10"}, map[string]any{"model": "kimi-small", "rate": "0.33"}}}
	create := func(q map[string]any) response {
		return e.call(http.MethodPost, "/v1/ai/connections", admin, map[string]any{"name": "Kimi plan", "connector": "openai-compatible", "preset": "kimi",
			"auth": "api-key", "configuration": map[string]any{"preset": "kimi"}, "credential": "not-a-real-key-for-tests", "billing": map[string]any{"mode": "subscription", "quota": q}})
	}
	e.problemIs(create(quota), "createAIConnection", 422, aiconnection.CodeBilling)
	quota["terms"] = map[string]any{"url": "https://example.com/plan-terms", "title": "The plan's terms", "checked_on": "2026-10-01"}
	asked := create(quota)
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
	}
	if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	list := e.call(http.MethodGet, "/v1/ai/connections", owner, nil)
	c := list.Body["items"].([]any)[0].(map[string]any)
	q := c["billing"].(map[string]any)["quota"].(map[string]any)
	if q["terms"].(map[string]any)["title"] != "The plan's terms" || len(q["rates"].([]any)) != 2 {
		t.Fatalf("the plan: %s", list.Raw)
	}
	id := uuid.MustParse(c["id"].(string))

	// Two requests of the big model (20), three of the small one (0.99) and four of another (4): 24.99, so 24 drawn.
	now := e.clock.Now()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		var models []storage.AIModel
		for _, m := range []string{"kimi-big", "kimi-small", "kimi-plain"} {
			models = append(models, storage.AIModel{ConnectionID: id, Model: m, FirstListed: now, LastListed: now, Listed: true})
		}
		if _, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: id, Trigger: "refresh", Outcome: "complete", Pages: 1, ReadBy: "user:plans",
			ReadAt: now}, models); err != nil {
			return err
		}
		for m, n := range map[string]int{"kimi-big": 2, "kimi-small": 3, "kimi-plain": 4} {
			for range n {
				if _, err := u.AICalls().Record(ctx, storage.AICall{Run: uuid.Must(uuid.NewV7()), Agent: "chat", RouteVersion: 1, Step: 1, ConnectionID: id,
					ModelRequested: m, ModelReported: m, Actor: "user:plans", Classes: []string{"business-figures"}, Outcome: "ok", Input: 10, Output: 5,
					StartedAt: now}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	u := e.call(http.MethodGet, "/v1/ai/usage", admin, nil)
	var use map[string]any
	for _, x := range u.Body["by_connection"].([]any) {
		if x.(map[string]any)["connection_id"] == id.String() {
			use, _ = x.(map[string]any)["quota"].(map[string]any)
		}
	}
	if use == nil || use["used"] != float64(24) || use["unit"] != "requests" {
		t.Errorf("the plan's use: %s", u.Raw)
	}
}
