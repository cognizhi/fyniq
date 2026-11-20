//go:build local

package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// A third-party AI app is registered by hand and never automatically (AI-46, rev 0.611; AI-90, AI-35, AI-34, QA-MCP-01): off until an Owner turns on AI apps through MCP and
// third-party registration, an Administrator's step-up alone registers one, the policy that checks a self-registering app checks the name and the redirects, nothing the
// Administrator names is fetched, the app is an agent-class public client with the default read scope alone and cannot connect until an Owner approves it, it makes a data
// inventory entry (a transfer to assess when it is outside Singapore) and appears in the access review, and removing it takes it out of the OAuth server at once.
// req: AI-46
// req: AI-90
// req: AI-35
// req: AI-34
// req: QA-MCP-01
// req: FR-CTL-06
// asvs: V10.4.1, V10.4.2, V8.2.1, V16.3.2
func TestAnAdministratorRegistersAThirdPartyAIAppByHandAndAnOwnerApprovesIt(t *testing.T) {
	s := startOAuth(t, 0)
	e := s.e
	user := matrixApprover(e)
	// The stack's API takes the dev tokens by their prefix, the others going to the OAuth server.
	dev := func(p authn.Principal) string {
		e.tokens.mu.Lock()
		defer e.tokens.mu.Unlock()
		tok := authn.DevPrefix + uuid.NewString()
		if p.Subject == "" {
			p.Subject = "user-" + tok[len(tok)-8:]
		}
		p.Via, p.Issuer = authn.ViaSession, "urn:fyniq:test"
		e.tokens.m[tok] = p
		return tok
	}
	person := func(class string, steppedUp bool, roles ...authz.Role) string {
		p := authn.Principal{Subject: "person-" + uuid.NewString(), UserID: uuid.MustParse(user), Client: "web", Class: class, Organisation: e.books.Org, Roles: roles}
		if steppedUp {
			p.StepUpAt = s.clock.Now()
		}
		return dev(p)
	}
	admin, adminNoStepUp := person(authn.ClassUser, true, authz.Administrator), person(authn.ClassUser, false, authz.Administrator)
	owner := person(authn.ClassUser, true, authz.Owner)
	call := func(method, path, tok string, body any) (int, map[string]any) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rd = strings.NewReader(string(raw))
		}
		req, _ := http.NewRequest(method, s.srv.URL+path, rd)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}
	good := map[string]any{"name": "Acme Books Assistant", "publisher": "Acme Software Pte Ltd", "publisher_url": "https://acme.example", "redirect_uris": []string{"https://acme.example/oauth/callback"},
		"purpose": "Ask questions about the management accounts", "processing_location": "United States", "outside_singapore": true}
	path := "/v1/connected-apps/third-party-apps"

	// Off by default: nothing is created, whoever asks.
	if status, out := call(http.MethodPost, path, admin, good); status != 403 || out["code"] != CodeThirdPartyOff {
		t.Fatalf("off by default: %d %v", status, out)
	}
	y, m, d := e.clock.Now().In(appclock.Singapore).Date()
	on := func(key string) {
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Settings().Record(ctx, key, "false", "true", time.Date(y, m, d, 0, 0, 0, 0, time.UTC), 1)
			return err
		})
	}
	on("mcp.third_party")
	if status, out := call(http.MethodPost, path, admin, good); status != 403 || out["code"] != CodeThirdPartyOff {
		t.Fatalf("AI apps through MCP still off: %d %v", status, out)
	}
	on("features.mcp")

	// Who may not: an Owner (A, not C), an Accountant, an Administrator without a recent second factor, the AI, an integration client with every scope.
	for name, tok := range map[string]string{
		"an Owner":                     owner,
		"an Accountant":                person(authn.ClassUser, true, authz.Accountant),
		"an Administrator, no step-up": adminNoStepUp,
		"the AI":                       dev(authn.Principal{Client: "assistant", Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Administrator}}),
		"an integration client": dev(authn.Principal{Via: authn.ViaToken, Subject: "erp", Client: "erp", Class: authn.ClassIntegration, UserID: uuid.MustParse(user), Organisation: e.books.Org,
			Roles: []authz.Role{authz.Administrator}, Scoped: true, Scopes: authz.ClientScopes()}),
	} {
		if status, out := call(http.MethodPost, path, tok, good); status != 403 {
			t.Errorf("%s registered an app: %d %v", name, status, out)
		}
	}

	// The policy of AI-90 checks the name and the redirects; nothing the Administrator names is fetched; each refusal creates nothing.
	bad := func(over map[string]any) map[string]any {
		b := map[string]any{}
		for k, v := range good {
			b[k] = v
		}
		for k, v := range over {
			b[k] = v
		}
		return b
	}
	for name, body := range map[string]map[string]any{
		"a wildcard redirect":      bad(map[string]any{"redirect_uris": []string{"https://*.acme.example/cb"}}),
		"an http redirect":         bad(map[string]any{"redirect_uris": []string{"http://acme.example/cb"}}),
		"a fragment":               bad(map[string]any{"redirect_uris": []string{"https://acme.example/cb#x"}}),
		"a tunnel host":            bad(map[string]any{"redirect_uris": []string{"https://x.ngrok.io/cb"}}),
		"an impersonating name":    bad(map[string]any{"name": "Fyn1q Official"}),
		"a first-party name":       bad(map[string]any{"name": "Claude Code"}),
		"a metadata document":      bad(map[string]any{"client_id_metadata_document_uri": "https://evil.example/client.json"}),
		"a key set to fetch":       bad(map[string]any{"jwks_uri": "https://evil.example/keys"}),
		"a publisher over http":    bad(map[string]any{"publisher_url": "http://acme.example"}),
		"a publisher with a login": bad(map[string]any{"publisher_url": "https://user:pw@acme.example"}),
		"an empty publisher":       bad(map[string]any{"publisher": "  "}),
	} {
		if status, out := call(http.MethodPost, path, admin, body); status < 400 || status >= 500 {
			t.Errorf("%s: %d %v", name, status, out)
		}
	}
	if status, out := call(http.MethodGet, path, admin, nil); status != 200 || len(out["apps"].([]any)) != 0 {
		t.Fatalf("a refused registration left a record: %d %v", status, out)
	}

	// A good one: pending, an agent-class public client with the default read scope alone, and the OAuth server's record agrees.
	status, made := call(http.MethodPost, path, admin, good)
	id, _ := made["client_id"].(string)
	if status != 201 || !strings.HasPrefix(id, "mcp-") || made["status"] != "pending" || made["outside_singapore"] != true || made["publisher"] != "Acme Software Pte Ltd" {
		t.Fatalf("a good registration: %d %v", status, made)
	}
	resp, err := http.Get(s.admin.BaseURL + "/admin/clients/" + id)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var rec struct {
		Scope    string         `json:"scope"`
		Method   string         `json:"token_endpoint_auth_method"`
		Metadata map[string]any `json:"metadata"`
	}
	_ = json.Unmarshal(raw, &rec)
	if rec.Scope != "openid offline_access fyniq.read.reports" || rec.Method != "none" || rec.Metadata["class"] != "agent" {
		t.Errorf("the client at the OAuth server: %s", raw)
	}
	if st, _ := e.store.ClientStatus(context.Background(), id); st != storage.MCPClientPending {
		t.Errorf("the registration's status = %q, want pending: it cannot connect until an Owner approves it", st)
	}

	// The data inventory entry made with it names the transfer; the Owner sees it waiting, with who publishes it; the access review lists it.
	var inventory []storage.InventoryEntry
	e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
		inventory, err = u.Compliance().InventoryEntries(ctx)
		return err
	})
	found := false
	for _, en := range inventory {
		if strings.Contains(en.What, id) && en.NeedsDPIA && strings.Contains(en.Transfers, "outside Singapore") {
			found = true
		}
	}
	if !found {
		t.Errorf("no data inventory entry for %s among %d", id, len(inventory))
	}
	if status, out := call(http.MethodGet, "/v1/connected-apps/registrations", owner, nil); status != 200 || !strings.Contains(toJSON(out), id) || !strings.Contains(toJSON(out), "Acme Software Pte Ltd") {
		t.Errorf("the Owner's waiting registrations: %d %v", status, out)
	}
	if status, out := call(http.MethodGet, "/v1/access-review", owner, nil); status != 200 || !strings.Contains(toJSON(out["ai_apps"]), id) {
		t.Errorf("the access review: %d %v", status, out["ai_apps"])
	}

	// Removing it narrows and happens at once: gone from the OAuth server, removed in the record, and a second removal finds nothing.
	if status, out := call(http.MethodDelete, path+"/"+id, admin, nil); status != 204 {
		t.Fatalf("removing it: %d %v", status, out)
	}
	if r, err := http.Get(s.admin.BaseURL + "/admin/clients/" + id); err != nil || r.StatusCode != 404 {
		t.Errorf("the client is still at the OAuth server: %v %v", r, err)
	}
	if _, out := call(http.MethodGet, path, admin, nil); !strings.Contains(toJSON(out), `"removed"`) {
		t.Errorf("the record does not say it is removed: %v", out)
	}
	if status, out := call(http.MethodDelete, path+"/"+id, admin, nil); status != 404 || out["code"] != CodeThirdPartyApp {
		t.Errorf("removing it again: %d %v", status, out)
	}
	if status, out := call(http.MethodGet, "/v1/access-review", owner, nil); status != 200 || strings.Contains(toJSON(out["ai_apps"]), id) {
		t.Errorf("a removed app is still in the access review: %v", out["ai_apps"])
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func init() {
	// The matrix has no MCP server on, so an Administrator's registration is answered past the policy, by the deployment's switch.
	pastPolicy["registerMCPThirdPartyApp"] = CodeRegClosed
	pastPolicy["removeMCPThirdPartyApp"] = CodeThirdPartyApp
}

var matrixMCPThirdParty = map[string]func(e *env, tok string) (response, func() bool){
	"listMCPThirdPartyApps": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodGet, "/v1/connected-apps/third-party-apps", tok, nil), nil
	},
	"registerMCPThirdPartyApp": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodPost, "/v1/connected-apps/third-party-apps", steppedUp(e, tok), map[string]any{"name": "Matrix App", "publisher": "Matrix", "redirect_uris": []string{"https://matrix.example/cb"},
			"purpose": "A matrix row", "processing_location": "Singapore", "outside_singapore": false}), nil
	},
	"removeMCPThirdPartyApp": func(e *env, tok string) (response, func() bool) {
		return e.call(http.MethodDelete, "/v1/connected-apps/third-party-apps/mcp-00000000000000000000", steppedUp(e, tok), nil), nil
	},
}
