//go:build local

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aiconnection"
	"github.com/cognizhi/fyniq/internal/domain/aimodel"
	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
	"github.com/cognizhi/fyniq/internal/domain/approval"
	"github.com/cognizhi/fyniq/internal/notify"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// An AI connection's life is dual controlled (AI-72, golden rule 6, FM-1): an Administrator asks to connect a provider
// with its API key, and does not approve their own request; the Owner approves, and the connection exists, switched off,
// with its key in the secret store and never in an answer or an approval. Changing it, replacing its key and switching
// it on are each asked for and approved the same way; the Administrator switches it off at once. A consumer plan's
// token is refused before anything is stored (AI-73); a connection without its key cannot be switched on.
// req: AI-72
// req: AI-73
// req: AI-79
// req: FM-1
// asvs: V8.2.1, V13.3.1
func TestAIConnectionsAreDualControlled(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner) // the requester holds A as well: FM-1 refuses it
	key := "sk-proj-" + "fixture0123456789abcdef"
	approve := func(asked response) {
		t.Helper()
		if asked.Status != http.StatusAccepted || asked.str("object_type") != "ai_connection" {
			t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
		}
		e.problemIs(f.decide(admin, asked.str("id"), "approve", hashOf(asked)), "approveApproval", 403, approval.CodeOwnRequest)
		if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK || d.str("status") != "approved" {
			t.Fatalf("approving: %d %s", d.Status, d.Raw)
		}
	}
	definition := map[string]any{"name": "OpenAI", "connector": "openai", "auth": "api-key", "billing": map[string]any{"mode": "usage"}}

	// A consumer plan's sign-in token is refused before anything is stored.
	jwt := map[string]any{"name": "OpenAI", "connector": "openai", "auth": "api-key", "billing": map[string]any{"mode": "usage"},
		"credential": "eyJ" + "hbGciOiJIUzI1NiJ9" + "." + "eyJzdWIiOiJ1c2VyIn0" + "." + "c2lnbmF0dXJl"}
	e.problemIs(e.call(http.MethodPost, "/v1/ai/connections", admin, jwt), "createAIConnection", 422, aiconnection.CodeCredential)
	e.problemIs(e.call(http.MethodPost, "/v1/ai/connections", admin, map[string]any{"name": "Mail", "connector": "resend", "auth": "api-key",
		"billing": map[string]any{"mode": "usage"}}), "createAIConnection", 422, aiconnection.CodeProvider)
	e.problemIs(e.call(http.MethodPost, "/v1/ai/connections", admin, map[string]any{"name": "Grok", "connector": "openai-compatible", "preset": "grok",
		"auth": "none", "configuration": map[string]any{"preset": "grok"}, "billing": map[string]any{"mode": "usage"}}), "createAIConnection", 422, aiconnection.CodeAuth)

	create := map[string]any{"credential": key}
	for k, v := range definition {
		create[k] = v
	}
	asked := e.call(http.MethodPost, "/v1/ai/connections", admin, create)
	if strings.Contains(asked.Raw, key) {
		t.Fatal("the approval request shows the key")
	}
	approve(asked)
	list := e.call(http.MethodGet, "/v1/ai/connections", owner, nil)
	items, _ := list.Body["items"].([]any)
	if list.Status != http.StatusOK || len(items) != 1 || strings.Contains(list.Raw, key) {
		t.Fatalf("the list: %d %s", list.Status, list.Raw)
	}
	c := items[0].(map[string]any)
	id := c["id"].(string)
	if c["state"] != "disabled" || c["host"] != "api.openai.com" || c["credential"].(map[string]any)["set"] != true || c["version"].(float64) != 1 ||
		c["created_by"] != "user:"+f.ahkow {
		t.Fatalf("the new connection: %v", c)
	}
	var ref string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		got, err := u.AIConnections().Get(ctx, uuid.MustParse(id))
		ref = got.CredentialRef
		return err
	})
	if v, err := e.api.deps.Connectors.Secrets.Get(context.Background(), ports.SecretRef(ref)); err != nil || v != key {
		t.Fatalf("the key in the secret store: %v", err)
	}
	get := e.call(http.MethodGet, "/v1/ai/connections/"+id, admin, nil)
	if get.Status != http.StatusOK || get.Header.Get("ETag") != `"1-disabled"` {
		t.Fatalf("getAIConnection: %d %v %s", get.Status, get.Header, get.Raw)
	}

	// Changed: a subscription with its quota, and its data handling: processed in the United States, with its inventory row
	// and transfer assessment.
	inventory, transfer := recordedFlow(e, "OpenAI")
	changed := map[string]any{"name": "OpenAI plan", "auth": "api-key", "billing": map[string]any{"mode": "subscription",
		"quota": map[string]any{"amount": 5000000, "unit": "tokens", "window": "month", "fee": "30.00", "currency": "USD"}},
		"data_handling": map[string]any{"inventory_id": inventory, "transfer_id": transfer, "handling": []any{map[string]any{"model": "*",
			"endpoint": "api.openai.com", "feature": "*", "processed_in": []string{"US"}, "stored_in": []string{"US"}, "retention_days": 30,
			"training": "no", "evidence": map[string]any{"kind": "source", "url": "https://platform.openai.com/docs/guides/your-data",
				"title": "Data controls in the OpenAI platform", "checked_on": "2026-10-14"}}}}}
	e.problemIs(e.call(http.MethodPut, "/v1/ai/connections/"+id, admin, changed), "changeAIConnection", 428, CodeIfMatchRequired)
	approve(e.call(http.MethodPut, "/v1/ai/connections/"+id, admin, changed, "If-Match", `"1-disabled"`))

	// Re-keyed: the new key stored now, current once approved.
	next := "sk-proj-" + "fixture9876543210fedcba"
	e.problemIs(e.call(http.MethodPut, "/v1/ai/connections/"+id+"/credential", admin, map[string]any{"value": "sessionKey=" + "abcdef0123456789abcd"},
		"If-Match", `"2-disabled"`), "setAIConnectionCredential", 422, aiconnection.CodeCredential)
	approve(e.call(http.MethodPut, "/v1/ai/connections/"+id+"/credential", admin, map[string]any{"value": next}, "If-Match", `"2-disabled"`))

	// Switched on once its model list is read whole, then off at once.
	e.problemIs(e.call(http.MethodPost, "/v1/ai/connections/"+id+"/enable", admin, nil, "If-Match", `"3-disabled"`), "enableAIConnection", 409,
		aimodel.CodeListUnavailable)
	models := &fakeModels{ids: []string{"gpt-6", "gpt-5"}}
	e.api.deps.AIProviders = models.provider
	if r := e.call(http.MethodPost, "/v1/ai/connections/"+id+"/models/refresh", admin, nil); r.Status != http.StatusOK || r.Body["last_complete"] == nil {
		t.Fatalf("refreshing: %d %s", r.Status, r.Raw)
	}
	approve(e.call(http.MethodPost, "/v1/ai/connections/"+id+"/enable", admin, nil, "If-Match", `"3-disabled"`))
	get = e.call(http.MethodGet, "/v1/ai/connections/"+id, admin, nil)
	if get.str("state") != "enabled" || get.str("name") != "OpenAI plan" || get.Body["billing"].(map[string]any)["quota"].(map[string]any)["fee"] != "30.00" {
		t.Fatalf("on: %s", get.Raw)
	}
	e.problemIs(e.call(http.MethodPost, "/v1/ai/connections/"+id+"/enable", admin, nil, "If-Match", `"4-enabled"`), "enableAIConnection", 409, aiconnection.CodeState)
	off := e.call(http.MethodPost, "/v1/ai/connections/"+id+"/disable", admin, nil, "If-Match", `"4-enabled"`)
	if off.Status != http.StatusOK || off.str("state") != "disabled" || off.Header.Get("ETag") != `"5-disabled"` {
		t.Fatalf("off: %d %s", off.Status, off.Raw)
	}
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		got, err := u.AIConnections().Get(ctx, uuid.MustParse(id))
		if err == nil && (got.CredentialRef == ref || got.CredentialRef == "") {
			t.Errorf("the key was not replaced: %s", got.CredentialRef)
		}
		versions, err2 := u.AIConnections().Versions(ctx, uuid.MustParse(id))
		steps := []string{}
		for _, v := range versions {
			steps = append(steps, v.Step)
			if strings.Contains(string(v.Snapshot), key) || strings.Contains(string(v.Snapshot), next) {
				t.Errorf("version %d holds a key", v.Version)
			}
		}
		if strings.Join(steps, ",") != "created,changed,credential,enabled,disabled" {
			t.Errorf("the versions: %v", steps)
		}
		if err == nil {
			err = err2
		}
		return err
	})
	if strings.Contains(e.log.String(), key) || strings.Contains(e.log.String(), next) {
		t.Error("the log holds a key")
	}

	// Without its key, a connection is not switched on.
	asked = e.call(http.MethodPost, "/v1/ai/connections", admin, map[string]any{"name": "Kimi", "connector": "openai-compatible", "preset": "kimi",
		"auth": "api-key", "configuration": map[string]any{"preset": "kimi"}, "billing": map[string]any{"mode": "usage"}})
	approve(asked)
	list = e.call(http.MethodGet, "/v1/ai/connections", admin, nil)
	var kimi string
	for _, x := range list.Body["items"].([]any) {
		if x.(map[string]any)["name"] == "Kimi" {
			kimi = x.(map[string]any)["id"].(string)
		}
	}
	e.problemIs(e.call(http.MethodPost, "/v1/ai/connections/"+kimi+"/enable", admin, nil, "If-Match", `"1-disabled"`), "enableAIConnection", 409, aiconnection.CodeState)
}

// matrixAIConnection is a connection of the matrix's organisation named name, made once: off with its key, or on.
func matrixAIConnection(e *env, name string, on bool) string {
	e.t.Helper()
	var id string
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		all, err := u.AIConnections().List(ctx)
		if err != nil {
			return err
		}
		for _, c := range all {
			if c.Name == name {
				id = c.ID.String()
				return nil
			}
		}
		now := e.clock.Now()
		approval := uuid.Must(uuid.NewV7())
		entry, err := u.Compliance().SaveInventoryEntry(ctx, storage.InventoryEntry{What: "Prompts to " + name, Why: "The AI assistant", Where: "The provider",
			Who: "The founders", Retention: "None", Transfers: "None", State: "open", SavedBy: "user:matrix", SavedAt: now, Version: 1})
		if err != nil {
			return err
		}
		handling := `{"inventory_id":"` + entry.EntryID.String() + `","handling":[{"model":"*","endpoint":"api.openai.com","feature":"*",` +
			`"processed_in":["SG"],"training":"no","evidence":{"kind":"source","url":"https://example.com/sg","title":"A page","checked_on":"2026-10-01"}}]}`
		c, err := u.AIConnections().Create(ctx, storage.AIConnection{Name: name, Connector: "openai", Auth: "api-key", Configuration: []byte(`{}`),
			Host: "api.openai.com", CredentialRef: "matrix/" + strings.ReplaceAll(name, " ", "-"), CredentialSetAt: &now, Billing: "usage",
			DataHandling: []byte(handling), CreatedBy: "user:matrix", CreatedAt: now}, approval)
		if err != nil {
			return err
		}
		if _, err := u.AIModels().Record(ctx, storage.AIModelListRead{ConnectionID: c.ID, Trigger: "refresh", Outcome: "complete", Pages: 1, Added: 1,
			ReadBy: "user:matrix", ReadAt: now}, []storage.AIModel{{ConnectionID: c.ID, Model: "gpt-6", FirstListed: now, LastListed: now, Listed: true},
			{ConnectionID: c.ID, Model: "gpt-5", FirstListed: now, LastListed: now, Listed: true},
			{ConnectionID: c.ID, Model: "gpt-4", FirstListed: now, LastListed: now, Listed: true}}); err != nil {
			return err
		}
		if _, err := u.AIModels().Probe(ctx, c.ID, "gpt-6", true, false, "answered", now); err != nil {
			return err
		}
		if on {
			c.State, c.ChangedBy, c.ChangedAt = "enabled", "user:matrix", now
			if c, err = u.AIConnections().Step(ctx, c, storage.AIConnectionEnabled, &approval); err != nil {
				return err
			}
		}
		id = c.ID.String()
		return nil
	})
	return id
}

// matrixAIPrice is a new price of gpt-5 on the matrix's connection, waiting for the Owner's review, as a read records
// one that fell by more than half (AI-85).
func matrixAIPrice(e *env) string {
	e.t.Helper()
	connection := uuid.MustParse(matrixAIConnection(e, "Matrix off", false))
	now := e.clock.Now()
	worker := workerStore(e.t)
	digest := strings.Repeat("cd", 32)
	cat, err := worker.RecordAIPriceCatalogue(context.Background(), storage.AIPriceCatalogue{Source: "litellm", URL: "https://example.com/prices.json",
		FetchedAt: now, SHA256: digest, LicenceURL: "https://example.com/LICENSE", LicenceSHA256: digest, Outcome: "rejected",
		Failure: "a fixture of the matrix, never the latest valid download", Document: "ai/price-catalogues/litellm/" + digest + ".json"})
	if err != nil {
		e.t.Fatal(err)
	}
	var p storage.AIPrice
	if err := worker.Do(context.Background(), e.books.Org, func(ctx context.Context, u storage.UnitOfWork) (err error) {
		p, err = u.AIPrices().Record(ctx, storage.AIPrice{ConnectionID: connection, Model: "gpt-5", Source: "litellm", CatalogueID: &cat.ID,
			CatalogueKey: "gpt-5", Match: "exact", Currency: "USD", Status: "pending", Review: "drop", ReviewDetail: "input fell from 2 to 0.5",
			RecordedBy: "system:ai.prices.read", RecordedAt: now, Rates: []storage.AIPriceRate{{Kind: "input", Unit: "per_million_tokens", Amount: "0.5"},
				{Kind: "output", Unit: "per_million_tokens", Amount: "8"}}})
		return err
	}); err != nil {
		e.t.Fatal(err)
	}
	return p.ID.String()
}

// matrixAIOwnerPrice is a new Owner's price of gpt-4 in force on the matrix's connection, written as the database's
// superuser with its triggers held back, as its approved request would leave it (AI-82).
func matrixAIOwnerPrice(e *env) string {
	e.t.Helper()
	connection := matrixAIConnection(e, "Matrix off", false)
	ctx := context.Background()
	c, err := pgx.Connect(ctx, db.URL("fyniq"))
	if err != nil {
		e.t.Fatal(err)
	}
	defer c.Close(ctx)
	tx, err := c.Begin(ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	if _, err := tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		e.t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE fyniq.ai_price SET status = 'superseded' WHERE organisation_id = $1 AND model = 'gpt-4' AND status = 'active'`,
		e.books.Org); err != nil {
		e.t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO fyniq.ai_price (organisation_id, connection_id, model, number, source, match, currency, status,
	  recorded_by, recorded_at, approval_id) SELECT $1, $2, 'gpt-4', coalesce(max(number), 0) + 1, 'owner', 'entered', 'USD', 'active', 'user:matrix',
	  now(), gen_random_uuid() FROM fyniq.ai_price WHERE organisation_id = $1 AND model = 'gpt-4' RETURNING id`, e.books.Org, connection).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// matrixAIConnections are the authorisation matrix's rows of AI connections (AI-72): reading under any right on "AI
// providers and model routes"; asking to create, change, re-key and switch one on under its C, stepped up, as previews;
// switching one off under its C, as a preview. The AI holds no right on the row, so none of it reaches the AI or MCP
// (§9.6).
var matrixAIConnections = map[string]func(e *env, tok string) (response, func() bool){
	"listAIConnections": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/connections", tok, nil), nil
	},
	"getAIConnection": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false), tok, nil), nil
	},
	"createAIConnection": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/connections", steppedUp(e, tok), map[string]any{"name": "Matrix new", "connector": "openai",
			"auth": "api-key", "billing": map[string]any{"mode": "usage"}}, "Prefer", "return=preview"), nil
	},
	"changeAIConnection": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false), steppedUp(e, tok), map[string]any{
			"name": "Matrix renamed", "auth": "api-key", "billing": map[string]any{"mode": "usage"}}, "Prefer", "return=preview", "If-Match", "*"), nil
	},
	"setAIConnectionCredential": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false)+"/credential", steppedUp(e, tok),
			map[string]any{"value": "sk-proj-" + "matrix0123456789abcdef"}, "Prefer", "return=preview", "If-Match", "*"), nil
	},
	"enableAIConnection": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false)+"/enable", steppedUp(e, tok), nil,
			"Prefer", "return=preview", "If-Match", "*"), nil
	},
	"disableAIConnection": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix on", true)+"/disable", tok, nil,
			"Prefer", "return=preview", "If-Match", "*"), nil
	},
	"getAIConnectionModels": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false)+"/models", tok, nil), nil
	},
	"refreshAIConnectionModels": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false)+"/models/refresh", tok, nil), nil
	},
	"probeAIConnectionModel": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false)+"/models/probe", tok,
			map[string]any{"model": "gpt-5"}), nil
	},
	"qualifyAIConnectionModel": func(e *env, tok string) (response, func() bool) {
		was := e.api.deps.AIProviders
		defer func() { e.api.deps.AIProviders = was }()
		e.api.deps.AIProviders = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
			return &evalModel{answers: map[string]string{"single word OK": "OK", "17 plus 25": "42", "READY": "READY"}}, nil
		}
		return e.call(http.MethodPost, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false)+"/models/qualify", tok,
			map[string]any{"model": "gpt-5", "agent": "probe"}), nil
	},
	"allowAIConnectionModel": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false)+"/models/allow", steppedUp(e, tok),
			map[string]any{"model": "gpt-6"}, "Prefer", "return=preview"), nil
	},
	"removeAIConnectionModel": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false)+"/models/remove", tok,
			map[string]any{"model": "gpt-4"}), nil
	},
	"listAIPriceCatalogues": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/price-catalogues", tok, nil), nil
	},
	"priceAIConnectionModel": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/connections/"+matrixAIConnection(e, "Matrix off", false)+"/models/price", steppedUp(e, tok),
			map[string]any{"model": "gpt-5", "currency": "USD", "rates": []any{map[string]any{"kind": "input", "amount": "1"},
				map[string]any{"kind": "output", "amount": "4"}}}, "Prefer", "return=preview"), nil
	},
	"withdrawAIPrice": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/prices/"+matrixAIOwnerPrice(e)+"/withdraw", tok, nil), nil
	},
	"reviewAIPrice": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/ai/prices/"+matrixAIPrice(e)+"/review", tok, map[string]any{"decision": "decline"}), nil
	},
	"getAIBudgets": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/budgets", tok, nil), nil
	},
	"setAIBudget": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/ai/budgets/organisation", tok, map[string]any{"amount": "100"}, "Prefer", "return=preview", "If-Match", "*"), nil
	},
	"listAIActivity": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/activity", tok, nil), nil
	},
	"getAIUsage": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/usage", tok, nil), nil
	},
	"getAIPresets": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/presets", tok, nil), nil
	},
	"getAIRoutes": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/routes", tok, nil), nil
	},
	"getAIRoute": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/routes/chat", tok, nil), nil
	},
	"setAIRoute": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/ai/routes/chat", steppedUp(e, tok), map[string]any{"classes": []string{"business-figures"},
			"steps": []any{map[string]any{"connection_id": matrixRouteConnection(e), "model": "gpt-6", "cache": true}}},
			"Prefer", "return=preview", "If-Match", "*"), nil
	},
	"getAIDataPolicy": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/ai/data-policy", tok, nil), nil
	},
	"changeAIDataPolicy": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPut, "/v1/ai/data-policy", steppedUp(e, tok), map[string]any{"exclude": map[string]any{"business-figures": []string{matrixAIConnection(e, "Matrix on", true)}}}, "Prefer", "return=preview", "If-Match", "*"), nil
	},
}

// recordedFlow records, in the Compliance Center, the data inventory row and the transfer assessment of a flow to a
// provider processing outside Singapore, and returns their IDs (SEC-13, CMP-PD-06).
func recordedFlow(e *env, provider string) (inventory, transfer string) {
	e.t.Helper()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		now := e.clock.Now()
		entry, err := u.Compliance().SaveInventoryEntry(ctx, storage.InventoryEntry{What: "Prompts to " + provider, Why: "The AI assistant",
			Where: provider, Who: "The founders", Retention: "30 days at the provider", Transfers: "United States", State: "open", SavedBy: "user:dpo",
			SavedAt: now, Version: 1})
		if err != nil {
			return err
		}
		assessed := now.Truncate(24 * time.Hour)
		t, err := u.Compliance().SaveTransfer(ctx, storage.Transfer{Recipient: provider, Countries: "United States", Data: "Prompts with business figures",
			Purpose: "The AI assistant", Basis: "contract", AssessedOn: assessed, AssessedBy: "The DPO", Evidence: "The provider's data processing addendum",
			NextReviewOn: assessed.AddDate(1, 0, 0), State: "open", SavedBy: "user:dpo", SavedAt: now, Version: 1})
		inventory, transfer = entry.EntryID.String(), t.TransferID.String()
		return err
	})
	return inventory, transfer
}

// Where the AI may send which data (AI-75, AI-88, CMP-PD-06): a connection is switched on only with its data handling
// declared, its data inventory row, and, when it processes outside Singapore, its transfer assessment. The data-class
// policy starts at its defaults; an Administrator asks to widen it, the Owner approves, and it is the next version; it
// never sends restricted data anywhere, nor personal data overseas on a connection whose transfer is not assessed.
// req: AI-75
// req: AI-88
// req: CMP-PD-06
// req: SEC-13
// asvs: V8.2.1, V14.2.1
func TestWhereTheAIMaySendData(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	approve := func(asked response) {
		t.Helper()
		if asked.Status != http.StatusAccepted {
			t.Fatalf("asking: %d %s", asked.Status, asked.Raw)
		}
		if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
			t.Fatalf("approving: %d %s", d.Status, d.Raw)
		}
	}
	us := func(inventory, transfer string) map[string]any {
		return map[string]any{"inventory_id": inventory, "transfer_id": transfer, "handling": []any{map[string]any{"model": "*", "endpoint": "api.openai.com",
			"feature": "*", "processed_in": []string{"US"}, "training": "no", "evidence": map[string]any{"kind": "source",
				"url": "https://platform.openai.com/docs/guides/your-data", "title": "Data controls", "checked_on": "2026-10-14"}}}}
	}
	connect := func(name string, handling map[string]any) string {
		t.Helper()
		body := map[string]any{"name": name, "connector": "openai", "auth": "api-key", "billing": map[string]any{"mode": "usage"},
			"credential": "sk-proj-" + "fixture0123456789abcdef"}
		if handling != nil {
			body["data_handling"] = handling
		}
		approve(e.call(http.MethodPost, "/v1/ai/connections", admin, body))
		for _, x := range e.call(http.MethodGet, "/v1/ai/connections", admin, nil).Body["items"].([]any) {
			if c := x.(map[string]any); c["name"] == name {
				e.call(http.MethodPost, "/v1/ai/connections/"+c["id"].(string)+"/models/refresh", admin, nil)
				return c["id"].(string)
			}
		}
		t.Fatalf("%s was not created", name)
		return ""
	}
	inventory, transfer := recordedFlow(e, "OpenAI")
	e.api.deps.AIProviders = (&fakeModels{ids: []string{"gpt-6"}}).provider

	// A declaration that is not evidenced is refused before anything is asked.
	bad := us(inventory, transfer)
	bad["handling"].([]any)[0].(map[string]any)["evidence"].(map[string]any)["checked_on"] = "2027-01-01"
	e.problemIs(e.call(http.MethodPost, "/v1/ai/connections", admin, map[string]any{"name": "Undated", "connector": "openai", "auth": "api-key",
		"billing": map[string]any{"mode": "usage"}, "data_handling": bad}), "createAIConnection", 422, aipolicy.CodeDeclaration)

	for name, handling := range map[string]map[string]any{
		"Undeclared":           nil,
		"Not in the inventory": us("", transfer),
		"Overseas, unassessed": us(inventory, ""),
	} {
		id := connect(name, handling)
		e.problemIs(e.call(http.MethodPost, "/v1/ai/connections/"+id+"/enable", admin, nil, "If-Match", `"1-disabled"`), "enableAIConnection", 422,
			aipolicy.CodeDeclaration)
	}
	assessed := connect("Assessed", us(inventory, transfer))
	approve(e.call(http.MethodPost, "/v1/ai/connections/"+assessed+"/enable", admin, nil, "If-Match", `"1-disabled"`))
	if got := e.call(http.MethodGet, "/v1/ai/connections/"+assessed, admin, nil); got.str("state") != "enabled" ||
		got.Body["data_handling"].(map[string]any)["transfer_id"] != transfer {
		t.Fatalf("switched on with its transfer: %s", got.Raw)
	}

	policy := e.call(http.MethodGet, "/v1/ai/data-policy", admin, nil)
	if policy.Status != http.StatusOK || policy.Body["version"].(float64) != 0 || policy.Header.Get("ETag") != `"0"` || policy.str("defaults") == "" {
		t.Fatalf("the default policy: %d %s", policy.Status, policy.Raw)
	}
	unassessed := connect("Unassessed", us(inventory, ""))
	e.problemIs(e.call(http.MethodPut, "/v1/ai/data-policy", admin, map[string]any{"allow": map[string]any{"personal-data": []string{unassessed}}},
		"If-Match", `"0"`), "changeAIDataPolicy", 422, aipolicy.CodePolicy)
	e.problemIs(e.call(http.MethodPut, "/v1/ai/data-policy", admin, map[string]any{"allow": map[string]any{"restricted": []string{assessed}}},
		"If-Match", `"0"`), "changeAIDataPolicy", 422, aipolicy.CodePolicy)
	approve(e.call(http.MethodPut, "/v1/ai/data-policy", admin, map[string]any{"allow": map[string]any{"personal-data": []string{assessed}},
		"exclude": map[string]any{"business-figures": []string{unassessed}}}, "If-Match", `"0"`))
	policy = e.call(http.MethodGet, "/v1/ai/data-policy", admin, nil)
	if policy.Body["version"].(float64) != 1 || policy.Header.Get("ETag") != `"1"` ||
		policy.Body["allow"].(map[string]any)["personal-data"].([]any)[0] != assessed {
		t.Fatalf("the policy's next version: %s", policy.Raw)
	}
	e.problemIs(e.call(http.MethodPut, "/v1/ai/data-policy", admin, map[string]any{}, "If-Match", `"0"`), "changeAIDataPolicy", 412, CodeStale)
}

// fakeModels is a provider whose list is the models it holds, two to a page; refuse fails its first page, and broken its
// second.
type fakeModels struct {
	mu      sync.Mutex
	ids     []string
	refuse  error
	broken  bool
	refused map[string]bool // models the provider answers not found
}

func (f *fakeModels) provider(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
	return f, nil
}
func (f *fakeModels) Capabilities() ports.ModelCapabilities { return ports.ModelCapabilities{} }
func (f *fakeModels) Call(_ context.Context, r ports.ModelRequest) (ports.ModelResponse, error) {
	if f.refused[r.Model] {
		return ports.ModelResponse{}, fmt.Errorf("example answered 404 (not_found_error): %w", ports.ErrModelNotFound)
	}
	return ports.ModelResponse{Model: r.Model, StopReason: ports.StopEnd, Parts: []ports.ModelPart{{Kind: ports.PartText, Text: "ok"}},
		Usage: ports.ModelUsage{Input: 3, Output: 1}}, nil
}
func (f *fakeModels) Stream(ctx context.Context, r ports.ModelRequest, _ func(ports.ModelEvent) error) (ports.ModelResponse, error) {
	return f.Call(ctx, r)
}
func (f *fakeModels) ListModels(_ context.Context, after string) (ports.ModelPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	start := 0
	if after != "" {
		start, _ = strconv.Atoi(after)
		if f.broken {
			return ports.ModelPage{}, errors.New("the provider did not answer the second page")
		}
	} else if f.refuse != nil {
		return ports.ModelPage{}, f.refuse
	}
	page := ports.ModelPage{}
	for i := start; i < len(f.ids) && i < start+2; i++ {
		page.Models = append(page.Models, ports.ListedModel{ID: f.ids[i], Name: strings.ToUpper(f.ids[i])})
	}
	if start+2 < len(f.ids) {
		page.Next = strconv.Itoa(start + 2)
	}
	return page, nil
}

// Model lists come from the provider (AI-80, AI-81, AI-84): approving a key queues a read for the worker, which reads
// the list page by page; Refresh reads it now. Only a complete read marks a model no longer listed; a failed or
// partial read keeps the last complete list and says why; a connection is switched on only after a complete read. The
// daily job reads every connection's list.
// req: AI-80
// req: AI-81
// req: AI-84
func TestModelListsComeFromTheProvider(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	models := &fakeModels{ids: []string{"m1", "m2", "m3"}}
	e.api.deps.AIProviders = models.provider
	asked := e.call(http.MethodPost, "/v1/ai/connections", admin, map[string]any{"name": "Listed", "connector": "openai", "auth": "api-key",
		"billing": map[string]any{"mode": "usage"}, "credential": "sk-proj-" + "fixture0123456789abcdef"})
	if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	id := e.call(http.MethodGet, "/v1/ai/connections", admin, nil).Body["items"].([]any)[0].(map[string]any)["id"].(string)
	list := func() AIModelList {
		t.Helper()
		var out AIModelList
		r := e.call(http.MethodGet, "/v1/ai/connections/"+id+"/models", admin, nil)
		if r.Status != http.StatusOK || json.Unmarshal([]byte(r.Raw), &out) != nil {
			t.Fatalf("getAIConnectionModels: %d %s", r.Status, r.Raw)
		}
		return out
	}
	if l := list(); len(l.Models) != 0 || l.LastComplete != nil {
		t.Fatalf("before any read: %+v", l)
	}

	// The key's approval queued a read; the worker's work reads it.
	lister := ai.Lister{Store: workerStore(t), Provider: models.provider, Clock: e.clock}
	read := 0
	work := lister.Work()
	e.work(map[string]outbox.Work{ai.ReadKind: func(ctx context.Context, org uuid.UUID, row storage.OutboxRow) error {
		read++
		return work(ctx, org, row)
	}, notify.FanoutKind: func(context.Context, uuid.UUID, storage.OutboxRow) error { return nil }})
	l := list()
	if read != 1 || len(l.Models) != 3 || l.Reads[0].Outcome != "complete" || l.Reads[0].Trigger != "credential" || l.Reads[0].Pages != 2 ||
		l.Models[0].State != "new" || l.Models[0].Name != "M1" || l.LastComplete == nil {
		t.Fatalf("the first read (%d queued): %+v", read, l)
	}

	// A failed read keeps the list and says why; a partial one removes nothing; a complete one without m3 removes it.
	models.refuse = errors.New("the provider refused the credential")
	r := e.call(http.MethodPost, "/v1/ai/connections/"+id+"/models/refresh", admin, nil)
	var refreshed AIModelList
	_ = json.Unmarshal([]byte(r.Raw), &refreshed)
	if r.Status != http.StatusOK || refreshed.Reads[0].Outcome != "failed" || !strings.Contains(refreshed.Reads[0].Failure, "refused") ||
		len(refreshed.Models) != 3 || refreshed.Models[2].State != "new" {
		t.Fatalf("a failed read: %d %s", r.Status, r.Raw)
	}
	models.refuse, models.broken, models.ids = nil, true, []string{"m1", "m2", "m4"}
	e.call(http.MethodPost, "/v1/ai/connections/"+id+"/models/refresh", admin, nil)
	if l := list(); l.Reads[0].Outcome != "partial" || l.Reads[0].Pages != 1 || len(l.Models) != 3 || l.Models[2].State != "new" {
		t.Fatalf("a partial read: %+v", l)
	}
	models.broken, models.ids = false, []string{"m1", "m2"}
	e.call(http.MethodPost, "/v1/ai/connections/"+id+"/models/refresh", admin, nil)
	if l := list(); l.Reads[0].Outcome != "complete" || l.Reads[0].Removed != 1 || l.Models[2].State != "no_longer_listed" {
		t.Fatalf("a complete read without m3: %+v", l)
	}

	// The daily job reads it too, and the trail records each read as a system event.
	if err := lister.Daily(context.Background()); err != nil {
		t.Fatal(err)
	}
	if l := list(); l.Reads[0].Trigger != "daily" || l.Reads[0].ReadBy != "system:"+ai.DailyJob {
		t.Fatalf("the daily read: %+v", l.Reads[0])
	}
	var listed int
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		object := uuid.MustParse(id)
		all, err := u.Audit().Find(ctx, storage.AuditQuery{ObjectType: "ai_connection", ObjectID: &object, Action: "ai_model.listed", Limit: 100})
		listed = len(all)
		return err
	})
	if listed != 5 {
		t.Errorf("the trail records %d reads, not 5", listed)
	}
}

// A model joins a connection's allow-list only once its provider lists it, a probe on the connection proved it, and
// the Owner approved the request (AI-72, AI-80); it leaves at once when taken off, and by itself when the provider
// refuses it or a complete read no longer lists it (AI-81). Automatic qualification starts off (AI-81).
// req: AI-72
// req: AI-80
// req: AI-81
// asvs: V8.2.1
func TestModelsJoinTheAllowListProbedAndApproved(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	admin, owner := f.as(f.ahkow, authz.Owner, authz.Administrator), f.as(f.weiling, authz.Owner)
	models := &fakeModels{ids: []string{"m1", "m2", "m3"}, refused: map[string]bool{}}
	e.api.deps.AIProviders = models.provider
	asked := e.call(http.MethodPost, "/v1/ai/connections", admin, map[string]any{"name": "Allowed", "connector": "openai", "auth": "api-key",
		"billing": map[string]any{"mode": "usage"}, "credential": "sk-proj-" + "fixture0123456789abcdef"})
	if d := f.decide(owner, asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving: %d %s", d.Status, d.Raw)
	}
	c := e.call(http.MethodGet, "/v1/ai/connections", admin, nil).Body["items"].([]any)[0].(map[string]any)
	id := c["id"].(string)
	if c["auto_qualify"] != false {
		t.Errorf("automatic qualification starts on: %v", c["auto_qualify"])
	}
	base := "/v1/ai/connections/" + id + "/models/"
	e.call(http.MethodPost, base+"refresh", admin, nil)
	model := func(m string) map[string]any {
		t.Helper()
		for _, x := range e.call(http.MethodGet, "/v1/ai/connections/"+id+"/models", admin, nil).Body["models"].([]any) {
			if x.(map[string]any)["model"] == m {
				return x.(map[string]any)
			}
		}
		t.Fatalf("%s is not listed", m)
		return nil
	}

	e.problemIs(e.call(http.MethodPost, base+"allow", admin, map[string]any{"model": "m1"}), "allowAIConnectionModel", 409, aimodel.CodeNotProbed)
	e.problemIs(e.call(http.MethodPost, base+"probe", admin, map[string]any{"model": "typed-by-hand"}), "probeAIConnectionModel", 404, CodeNotFound)
	if p := e.call(http.MethodPost, base+"probe", admin, map[string]any{"model": "m1"}); p.Status != http.StatusOK ||
		p.Body["probe"].(map[string]any)["ok"] != true || p.Body["allowed"] != false {
		t.Fatalf("probing m1: %d %s", p.Status, p.Raw)
	}
	allow := e.call(http.MethodPost, base+"allow", admin, map[string]any{"model": "m1"})
	e.problemIs(f.decide(admin, allow.str("id"), "approve", hashOf(allow)), "approveApproval", 403, approval.CodeOwnRequest)
	if d := f.decide(owner, allow.str("id"), "approve", hashOf(allow)); d.Status != http.StatusOK {
		t.Fatalf("approving m1: %d %s", d.Status, d.Raw)
	}
	if m := model("m1"); m["allowed"] != true {
		t.Fatalf("m1 after approval: %v", m)
	}
	if r := e.call(http.MethodPost, base+"remove", admin, map[string]any{"model": "m1"}); r.Status != http.StatusOK || r.Body["allowed"] != false {
		t.Fatalf("taking m1 off: %d %s", r.Status, r.Raw)
	}

	// m2 allowed, then refused by the provider: not callable and off the list; m3 allowed, then no longer listed: off.
	for _, m := range []string{"m2", "m3"} {
		e.call(http.MethodPost, base+"probe", admin, map[string]any{"model": m})
		a := e.call(http.MethodPost, base+"allow", admin, map[string]any{"model": m})
		if d := f.decide(owner, a.str("id"), "approve", hashOf(a)); d.Status != http.StatusOK {
			t.Fatalf("approving %s: %d %s", m, d.Status, d.Raw)
		}
	}
	models.refused["m2"] = true
	if p := e.call(http.MethodPost, base+"probe", admin, map[string]any{"model": "m2"}); p.str("state") != "not_callable" || p.Body["allowed"] != false {
		t.Fatalf("m2 refused: %s", p.Raw)
	}
	// A read that lists an allowed model again keeps it allowed (a regression test: the read was refused).
	if r := e.call(http.MethodPost, base+"refresh", admin, nil); r.Status != http.StatusOK || r.Body["reads"].([]any)[0].(map[string]any)["outcome"] != "complete" {
		t.Fatalf("reading the list with m3 allowed: %d %s", r.Status, r.Raw)
	}
	if m := model("m3"); m["allowed"] != true {
		t.Fatalf("m3 listed again: %v", m)
	}
	models.ids = []string{"m1", "m2"}
	e.call(http.MethodPost, base+"refresh", admin, nil)
	if m := model("m3"); m["allowed"] != false || m["state"] != "no_longer_listed" {
		t.Fatalf("m3 no longer listed: %v", m)
	}
	e.problemIs(e.call(http.MethodPost, base+"allow", admin, map[string]any{"model": "m3"}), "allowAIConnectionModel", 409, aimodel.CodeNotProbed)
}
