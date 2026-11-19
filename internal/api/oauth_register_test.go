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

	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/mcpgate"
	"github.com/cognizhi/fyniq/internal/ports/storage"

	"github.com/google/uuid"
)

// An AI app registers itself only through Fyniq's own endpoint, and only when every door is open: closed until an Owner turns it on,
// policy-checked (exact redirects, nothing fetched, no impersonation), limited by network, created at the OAuth server as an agent-class
// public client with the default read scope alone, and unusable until an Owner adds it to the apps that may connect; the OAuth server's
// own registration stays off (AI-90, AI-35, AI-34, QA-MCP-01).
// req: AI-90
// req: AI-35
// req: AI-34
// req: QA-MCP-01
// asvs: V10.4.1, V10.4.2, V10.1.2, V16.3.2
func TestAnAIAppRegistersOnlyThroughTheClosedPolicyCheckedEndpoint(t *testing.T) {
	s := startOAuth(t, 0)
	e := s.e
	post := func(body string) (int, map[string]any, http.Header) {
		t.Helper()
		resp, err := http.Post(s.srv.URL+"/v1/oauth/register", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out, resp.Header
	}
	good := `{"client_name":"Acme Books Assistant","redirect_uris":["https://acme.example/oauth/callback"],"token_endpoint_auth_method":"none"}`

	// Closed by default: nothing is created.
	if status, out, _ := post(good); status != 403 || out["code"] != CodeRegClosed || out["error"] != "access_denied" {
		t.Fatalf("closed: %d %v", status, out)
	}
	pending := func() int {
		n, err := e.store.PendingClients(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return len(n)
	}
	if pending() != 0 {
		t.Fatal("a closed endpoint registered a client")
	}
	// An Owner opens it (the setting, in force today).
	y, m, d := e.clock.Now().In(appclock.Singapore).Date()
	e.do(func(ctx context.Context, u storage.UnitOfWork) error {
		_, err := u.Settings().Record(ctx, "mcp.registration", "false", "true", time.Date(y, m, d, 0, 0, 0, 0, time.UTC), 1)
		return err
	})

	status, out, _ := post(good)
	id, _ := out["client_id"].(string)
	if status != 201 || !strings.HasPrefix(id, "mcp-") || out["token_endpoint_auth_method"] != "none" || out["scope"] != "openid offline_access fyniq.read.reports" || out["client_secret"] != nil {
		t.Fatalf("a good registration: %d %v", status, out)
	}
	if pending() != 1 {
		t.Errorf("pending: %d", pending())
	}
	// At the OAuth server it is an agent-class public client for the MCP audience, with the default scope and the exact redirect.
	resp, err := http.Get(s.admin.BaseURL + "/admin/clients/" + id)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var rec struct {
		Audience []string       `json:"audience"`
		Redirect []string       `json:"redirect_uris"`
		Scope    string         `json:"scope"`
		Method   string         `json:"token_endpoint_auth_method"`
		Metadata map[string]any `json:"metadata"`
	}
	_ = json.Unmarshal(raw, &rec)
	if len(rec.Audience) != 1 || rec.Audience[0] != mcpAudience || rec.Scope != "openid offline_access fyniq.read.reports" || rec.Method != "none" ||
		rec.Metadata["class"] != "agent" || len(rec.Redirect) != 1 || rec.Redirect[0] != "https://acme.example/oauth/callback" {
		t.Errorf("the client at the OAuth server: %s", raw)
	}
	// The OAuth server's own registration stays off.
	if r, err := http.Post("http://"+s.public.Host+"/oauth2/register", "application/json", strings.NewReader(good)); err != nil || r.StatusCode < 400 {
		t.Errorf("the OAuth server's own registration: %v %v", r, err)
	}

	// The policy: each of these is refused with the field named, and creates nothing.
	before := pending()
	for name, body := range map[string]string{
		"a wildcard redirect":    `{"client_name":"Acme","redirect_uris":["https://*.acme.example/cb"]}`,
		"a fragment":             `{"client_name":"Acme","redirect_uris":["https://acme.example/cb#x"]}`,
		"an http redirect":       `{"client_name":"Acme","redirect_uris":["http://acme.example/cb"]}`,
		"a private-use scheme":   `{"client_name":"Acme","redirect_uris":["com.acme.app:/cb"]}`,
		"a javascript redirect":  `{"client_name":"Acme","redirect_uris":["javascript:alert(1)"]}`,
		"an IP-literal redirect": `{"client_name":"Acme","redirect_uris":["https://203.0.113.7/cb"]}`,
		"a tunnel host":          `{"client_name":"Acme","redirect_uris":["https://x.ngrok.io/cb"]}`,
		"a secret":               `{"client_name":"Acme","redirect_uris":["https://acme.example/cb"],"token_endpoint_auth_method":"client_secret_basic"}`,
		"a key set to fetch":     `{"client_name":"Acme","redirect_uris":["https://acme.example/cb"],"jwks_uri":"https://evil.example/keys"}`,
		"a software statement":   `{"client_name":"Acme","redirect_uris":["https://acme.example/cb"],"software_statement":"eyJ"}`,
		"a logo to fetch":        `{"client_name":"Acme","redirect_uris":["https://acme.example/cb"],"logo_uri":"https://evil.example/l.png"}`,
		"an impersonating name":  `{"client_name":"Fyn1q Official","redirect_uris":["https://acme.example/cb"]}`,
		"a first-party name":     `{"client_name":"Claude Code","redirect_uris":["https://acme.example/cb"]}`,
		"a brand off its hosts":  `{"client_name":"Claude","redirect_uris":["https://evil.example/cb"]}`,
		"more than the default":  `{"client_name":"Acme","redirect_uris":["https://acme.example/cb"],"scope":"openid fyniq.read.payroll"}`,
		"the implicit grant":     `{"client_name":"Acme","redirect_uris":["https://acme.example/cb"],"grant_types":["implicit"]}`,
		"a field nobody knows":   `{"client_name":"Acme","redirect_uris":["https://acme.example/cb"],"made_up":1}`,
		"not an object":          `[]`,
		"a body over 8 KiB":      `{"client_name":"Acme","redirect_uris":["https://acme.example/cb"],"x":"` + strings.Repeat("a", 9000) + `"}`,
	} {
		status, out, _ := post(body)
		if name == "not an object" { // the contract's own check refuses it first, in the API's usual words
			if status != 422 {
				t.Errorf("%s: %d %v", name, status, out)
			}
			continue
		}
		if status != 400 || (out["code"] != CodeRegPolicy) || out["error"] == nil || out["error_description"] == nil {
			t.Errorf("%s: %d %v", name, status, out)
		}
	}
	if pending() != before {
		t.Errorf("a refused registration created a client: %d pending, was %d", pending(), before)
	}
	// Claude Code's own loopback registration, and a real Claude connector on its host, are accepted.
	if status, out, _ := post(`{"client_name":"Claude Code (fyniq)","redirect_uris":["http://localhost:3118/callback"],"application_type":"native"}`); status != 201 {
		t.Errorf("Claude Code's own: %d %v", status, out)
	}
	if status, out, _ := post(`{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"]}`); status != 201 {
		t.Errorf("Anthropic's connector: %d %v", status, out)
	}

	// The limits: a network registers only so many an hour, and the answer says when to come back.
	var last int
	var header http.Header
	for i := 0; i < 5; i++ {
		last, _, header = post(good)
	}
	if last != 429 || header.Get("Retry-After") == "" {
		t.Errorf("past the hourly limit: %d Retry-After %q", last, header.Get("Retry-After"))
	}
	if _, out, _ := post(good); out["code"] != CodeRegLimit {
		t.Errorf("the limit's code: %v", out)
	}

	// A registration nobody approves lapses: a week on, the next registration finds it expired and its client gone from the OAuth server.
	s.clock.Advance(8 * 24 * time.Hour)
	if status, out, _ := post(good); status != 201 {
		t.Fatalf("after the window: %d %v", status, out)
	}
	if st, _ := e.store.ClientStatus(context.Background(), id); st != storage.MCPClientExpired {
		t.Errorf("the lapsed registration: %q", st)
	}
	if r, err := http.Get(s.admin.BaseURL + "/admin/clients/" + id); err != nil || r.StatusCode != http.StatusNotFound {
		t.Errorf("the lapsed client at the OAuth server: %v %v", r, err)
	}
}

// A registered app is usable only while its registration stands, and only once an Owner adds it to the apps that may connect: pending and
// not listed it is refused; listed, its first admission makes it approved; one that was denied or expired, and one never registered, is
// refused whatever the allowlist says; and an Owner can list only what exists (AI-90, AI-35).
// req: AI-90
// req: AI-35
// asvs: V8.2.2, V4.1.1
func TestARegisteredAppIsUsableOnlyOnceAnOwnerAllowsIt(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ctx := context.Background()
	user := uuid.MustParse(f.ahkow)
	ahkow := f.as(f.ahkow, "owner", "controller")
	y, m, d := e.clock.Now().In(appclock.Singapore).Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	versions := map[string]int{}
	set := func(key, was, value string) {
		t.Helper()
		versions[key]++
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Settings().Record(ctx, key, was, value, from, versions[key])
			return err
		})
	}
	set("features.mcp", "false", "true")
	got := e.call(http.MethodGet, "/v1/me/mcp", ahkow, nil)
	e.call(http.MethodPut, "/v1/me/mcp", ahkow, map[string]any{"opted_in": true}, "If-Match", got.str("etag"))
	gate := mcpgate.Gate{Store: e.store, Clock: e.clock, Clients: e.store}
	admit := func(app string) string {
		t.Helper()
		code, _, err := gate.Admit(ctx, e.books.Org, user, app)
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	register := func(id string) {
		t.Helper()
		refused, err := e.store.Register(ctx, storage.MCPClientRegistration{ID: id, Name: "Acme", RedirectURIs: []string{"https://acme.example/cb"}, Network: "203.0.113.0/24", At: e.clock.Now()},
			storage.MCPClientLimits{PerHour: 100, PerDay: 100, PendingCap: 100})
		if err != nil || refused != "" {
			t.Fatalf("registering %s: %q %v", id, refused, err)
		}
	}
	const pendingApp, deniedApp, expiredApp, unknownApp = "mcp-00000000000000000001", "mcp-00000000000000000002", "mcp-00000000000000000003", "mcp-00000000000000000004"
	for _, id := range []string{pendingApp, deniedApp, expiredApp} {
		register(id)
	}
	set("mcp.clients", "", pendingApp+","+deniedApp+","+expiredApp+","+unknownApp)
	if ok, err := e.store.DecideClient(ctx, deniedApp, storage.MCPClientDenied, e.clock.Now()); err != nil || !ok {
		t.Fatalf("denying: %v %v", ok, err)
	}
	if ok, err := e.store.DecideClient(ctx, deniedApp, storage.MCPClientApproved, e.clock.Now()); err != nil || ok {
		t.Errorf("a denied registration was decided again: %v %v", ok, err)
	}
	e.clock.Advance(8 * 24 * time.Hour)
	if ids, err := workerStore(t).ExpireClients(ctx, e.clock.Now(), 7*24*time.Hour, 30*24*time.Hour); err != nil || len(ids) != 2 {
		t.Fatalf("expiring: %v %v", ids, err) // the pending and the unsettled one; the denied is already decided
	}
	_ = expiredApp

	for app, want := range map[string]string{deniedApp: mcpgate.CodeNotAllowed, unknownApp: mcpgate.CodeNotAllowed, expiredApp: mcpgate.CodeNotAllowed} {
		if got := admit(app); got != want {
			t.Errorf("%s: %q, want %q", app, got, want)
		}
	}
	if got := admit(pendingApp); got != mcpgate.CodeNotAllowed {
		t.Errorf("a registration that expired unapproved: %q", got)
	}
	// A fresh one: pending, not listed: refused; listed: admitted, and approved by that first admission.
	const fresh = "mcp-00000000000000000005"
	register(fresh)
	if got := admit(fresh); got != mcpgate.CodeNotAllowed {
		t.Errorf("registered, not listed: %q", got)
	}
	set("mcp.clients", pendingApp+","+deniedApp+","+expiredApp+","+unknownApp, pendingApp+","+deniedApp+","+expiredApp+","+unknownApp+","+fresh)
	if got := admit(fresh); got != "" {
		t.Errorf("registered and listed: %q", got)
	}
	if st, _ := e.store.ClientStatus(ctx, fresh); st != storage.MCPClientApproved {
		t.Errorf("after its first admission: %q", st)
	}
	if got := admit(fresh); got != "" {
		t.Errorf("an approved app, again: %q", got)
	}
	// Taking it off the list cuts it off at once.
	set("mcp.clients", pendingApp+","+deniedApp+","+expiredApp+","+unknownApp+","+fresh, "")
	if got := admit(fresh); got != mcpgate.CodeNotAllowed {
		t.Errorf("after removal: %q", got)
	}
	// An approved app nobody uses expires, and its registration never changes otherwise.
	e.clock.Advance(31 * 24 * time.Hour)
	if ids, err := workerStore(t).ExpireClients(ctx, e.clock.Now(), 7*24*time.Hour, 30*24*time.Hour); err != nil || len(ids) != 1 || ids[0] != fresh {
		t.Errorf("an approved app never used: %v %v", ids, err)
	}
}
