//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aiconnection"
	"github.com/cognizhi/fyniq/internal/domain/approval"
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

	// Changed: a subscription with its quota, approved.
	changed := map[string]any{"name": "OpenAI plan", "auth": "api-key", "billing": map[string]any{"mode": "subscription",
		"quota": map[string]any{"amount": 5000000, "unit": "tokens", "window": "month", "fee": "30.00", "currency": "USD"}}}
	e.problemIs(e.call(http.MethodPut, "/v1/ai/connections/"+id, admin, changed), "changeAIConnection", 428, CodeIfMatchRequired)
	approve(e.call(http.MethodPut, "/v1/ai/connections/"+id, admin, changed, "If-Match", `"1-disabled"`))

	// Re-keyed: the new key stored now, current once approved.
	next := "sk-proj-" + "fixture9876543210fedcba"
	e.problemIs(e.call(http.MethodPut, "/v1/ai/connections/"+id+"/credential", admin, map[string]any{"value": "sessionKey=" + "abcdef0123456789abcd"},
		"If-Match", `"2-disabled"`), "setAIConnectionCredential", 422, aiconnection.CodeCredential)
	approve(e.call(http.MethodPut, "/v1/ai/connections/"+id+"/credential", admin, map[string]any{"value": next}, "If-Match", `"2-disabled"`))

	// Switched on, then off at once.
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
		c, err := u.AIConnections().Create(ctx, storage.AIConnection{Name: name, Connector: "openai", Auth: "api-key", Configuration: []byte(`{}`),
			Host: "api.openai.com", CredentialRef: "matrix/" + strings.ReplaceAll(name, " ", "-"), CredentialSetAt: &now, Billing: "usage", CreatedBy: "user:matrix", CreatedAt: now}, approval)
		if err != nil {
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
}
