//go:build local

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/mcpgate"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// An MCP tool call passes every gate in the API or is refused, in the order the design gives: the MCP service's own service
// token and the delegated token of an agent for a person (never the AI service's token, a turn's grant, a person's session or an
// integration); a tool the manifest exposes; every scope this tool and this input need, read from what the tool reads (the
// reports scope cannot reach an invoice, a bill or a journal, nor a contact or an expense claim); and the organisation's switch,
// the allowlist and the person's opt-in, asked again on every call, so each stops the next call at once (AI-16, AI-34, AI-35,
// AI-36, AI-37, CTL-MCP-01, CTL-MCP-02).
// req: AI-16
// req: AI-34
// req: AI-35
// req: AI-36
// req: AI-37
// req: CTL-MCP-02
// req: QA-MCP-01
// asvs: V8.2.1, V8.2.2, V10.3.2, V4.1.1
func TestAnMCPToolCallPassesEveryGateOrIsRefused(t *testing.T) {
	f := newFounders(t)
	e := f.e
	ahkow := f.as(f.ahkow, authz.Owner, authz.Controller)
	invoice, _ := e.invoiceDue("2026-10-01", "2026-10-31")
	service, err := e.services.Token(ServiceMCP)
	if err != nil {
		t.Fatal(err)
	}
	user := uuid.MustParse(f.ahkow)
	delegated := func(scopes ...string) string {
		return e.principal(authn.Principal{Via: authn.ViaToken, Subject: user.String(), UserID: user, Client: "mcp-delegation", Class: authn.ClassAgent,
			Organisation: e.books.Org, Roles: []authz.Role{authz.Owner, authz.Controller}, Scopes: scopes})
	}
	call := func(token, app, tool string, input map[string]any) response {
		t.Helper()
		return e.call(http.MethodPost, "/internal/v1/mcp/tool-calls", token, map[string]any{"app": app, "tool": tool, "input": input}, ServiceTokenHeader, service)
	}
	code := func(r response, status int, want string) {
		t.Helper()
		if r.Status != status || r.str("code") != want {
			t.Errorf("want %d %s: %d %s", status, want, r.Status, r.Raw)
		}
	}
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
	revenue := map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-10"}
	reports := delegated("fyniq.read.reports")

	// Closed by default: nothing runs until the organisation, the app and the person all allow it.
	code(call(reports, "claude-code", "metrics.query", revenue), 403, mcpgate.CodeOrganisationOff)
	set("features.mcp", "false", "true")
	code(call(reports, "claude-code", "metrics.query", revenue), 403, mcpgate.CodeNotAllowed)
	set("mcp.clients", "", "claude-code")
	code(call(reports, "claude-code", "metrics.query", revenue), 403, mcpgate.CodeNotOptedIn)
	got := e.call(http.MethodGet, "/v1/me/mcp", ahkow, nil)
	if r := e.call(http.MethodPut, "/v1/me/mcp", ahkow, map[string]any{"opted_in": true}, "If-Match", got.str("etag")); r.Status != 200 {
		t.Fatalf("opting in: %d %s", r.Status, r.Raw)
	}
	code(call(reports, "claude-desktop", "metrics.query", revenue), 403, mcpgate.CodeNotAllowed)

	// Every gate open: the figure is the semantic layer's own, as a typed fact.
	ok := call(reports, "claude-code", "metrics.query", revenue)
	direct := e.call(http.MethodPost, "/v1/metrics:query", e.as(authz.Controller), map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-10"})
	want := direct.Body["facts"].([]any)[0].(map[string]any)
	found := false
	if ok.Status == 200 {
		for _, fa := range ok.Body["facts"].([]any) {
			if fa.(map[string]any)["id"] == want["id"] && fa.(map[string]any)["value"] == want["value"] {
				found = true
			}
		}
	}
	if !found || ok.Body["tool"] != "metrics.query" {
		t.Fatalf("the open gates: %d %s, the operation's: %s", ok.Status, ok.Raw, direct.Raw)
	}

	// A scope the tool does not need is no help, and one it needs, read from its input, is: reports cannot reach an invoice.
	sales := call(reports, "claude-code", "records.get", map[string]any{"kind": "invoice", "id": invoice})
	code(sales, 403, CodeMCPScope)
	if sales.Header.Get(headerNeedScopes) != "fyniq.read.sales" {
		t.Errorf("the scope the call needs: %q", sales.Header.Get(headerNeedScopes))
	}
	code(call(reports, "claude-code", "metrics.query", map[string]any{"measure": "revenue", "by": "customer", "from": "2026-10", "to": "2026-10"}), 403, CodeMCPScope)
	if r := call(delegated("fyniq.read.sales"), "claude-code", "records.get", map[string]any{"kind": "invoice", "id": invoice}); r.Status != 200 || r.Body["facts"] == nil {
		t.Errorf("a sales token reads an invoice: %d %s", r.Status, r.Raw)
	}
	code(call(delegated("fyniq.read.sales"), "claude-code", "records.get", map[string]any{"kind": "bill", "id": invoice}), 403, CodeMCPScope)
	for _, in := range []map[string]any{{"kind": "contact", "id": invoice}, {"kind": "expense_claim", "id": invoice}} {
		code(call(delegated("fyniq.read.sales", "fyniq.read.purchases", "fyniq.read.accounting", "fyniq.read.reports"), "claude-code", "records.get", in), 403, CodeMCPScope)
	}
	code(call(delegated("fyniq.read.sales"), "claude-code", "records.search", map[string]any{"q": "acme"}), 403, CodeMCPScope) // no kind: every kind
	code(call(reports, "claude-code", "calc", map[string]any{"op": "add", "operands": []any{}}), 403, CodeMCPScope)
	if r := call(delegated("fyniq.read.payroll", "fyniq.read.equity"), "claude-code", "metrics.query", revenue); r.Status != 403 {
		t.Errorf("payroll and equity scopes: %d", r.Status)
	}
	if r := call(reports, "claude-code", "ledger.post", map[string]any{}); r.Status < 400 || r.Status >= 500 {
		t.Errorf("a tool that is not one: %d %s", r.Status, r.Raw)
	}

	// Who may call: the MCP service alone, with a delegated agent token. Nobody else's token opens it.
	aiService, _ := e.services.Token(ServiceAI)
	body := map[string]any{"app": "claude-code", "tool": "metrics.query", "input": revenue}
	for name, r := range map[string]response{
		"no service token":       e.call(http.MethodPost, "/internal/v1/mcp/tool-calls", reports, body),
		"the AI service's token": e.call(http.MethodPost, "/internal/v1/mcp/tool-calls", reports, body, ServiceTokenHeader, aiService),
		"a person's own session": e.call(http.MethodPost, "/internal/v1/mcp/tool-calls", ahkow, body, ServiceTokenHeader, service),
		"an integration client": e.call(http.MethodPost, "/internal/v1/mcp/tool-calls", e.principal(authn.Principal{Via: authn.ViaToken, Subject: "c1", Client: "c1",
			Class: authn.ClassIntegration, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}, Scoped: true, Scopes: []string{"fyniq.read.reports"}}), body, ServiceTokenHeader, service),
		"an agent token with no person": e.call(http.MethodPost, "/internal/v1/mcp/tool-calls", e.principal(authn.Principal{Via: authn.ViaToken, Subject: "a1", Client: "a1",
			Class: authn.ClassAgent, Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}, Scopes: []string{"fyniq.read.reports"}}), body, ServiceTokenHeader, service),
	} {
		if r.Status != http.StatusUnauthorized && r.Status != http.StatusForbidden {
			t.Errorf("%s: %d %s", name, r.Status, r.Raw)
		}
	}
	// And the AI service's own operations refuse the MCP service's token.
	if r := e.call(http.MethodPost, "/internal/v1/tool-calls", reports, map[string]any{"tool": "metrics.query", "input": revenue}, ServiceTokenHeader, service); r.Status != http.StatusUnauthorized {
		t.Errorf("the MCP service's token at the AI service's operation: %d %s", r.Status, r.Raw)
	}
	// The public API refuses the delegated token's writes (CTL-MCP-01).
	if r := e.call(http.MethodPost, "/v1/contacts", reports, map[string]any{"legal_name": "Sneaky", "customer": true}); r.Status != http.StatusForbidden {
		t.Errorf("an agent token wrote: %d %s", r.Status, r.Raw)
	}

	// Each gate stops the next call at once: the person's opt-out, the app's removal and the organisation's switch.
	cur := e.call(http.MethodGet, "/v1/me/mcp", ahkow, nil)
	e.call(http.MethodPut, "/v1/me/mcp", ahkow, map[string]any{"opted_in": false}, "If-Match", cur.str("etag"))
	code(call(reports, "claude-code", "metrics.query", revenue), 403, mcpgate.CodeNotOptedIn)
	cur = e.call(http.MethodGet, "/v1/me/mcp", ahkow, nil)
	e.call(http.MethodPut, "/v1/me/mcp", ahkow, map[string]any{"opted_in": true}, "If-Match", cur.str("etag"))
	if r := call(reports, "claude-code", "metrics.query", revenue); r.Status != 200 {
		t.Fatalf("opted back in: %d %s", r.Status, r.Raw)
	}
	set("mcp.clients", "claude-code", "claude-desktop")
	code(call(reports, "claude-code", "metrics.query", revenue), 403, mcpgate.CodeNotAllowed)
	set("mcp.clients", "claude-desktop", "claude-code")
	set("features.mcp", "true", "false")
	code(call(reports, "claude-code", "metrics.query", revenue), 403, mcpgate.CodeOrganisationOff)
	if strings.Contains(ok.Raw, "evil") {
		t.Error("unexpected text in a result")
	}
}

// The output filter masks an NRIC or a bank number in every string of a result before it leaves, whatever the tool or the
// field, and leaves figures, IDs and keys alone (AI-41, AI-20).
// req: AI-41
// req: AI-20
// asvs: V14.2.1, V8.3.1
func TestAResultIsFilteredBeforeItLeaves(t *testing.T) {
	in := map[string]any{"reference": "Payment from S1234567D", "note": []any{"account 123456789012", map[string]any{"deep": "F1234567N"}},
		"id": "0189a3c2-7b1e-7c41-9d2e-5f8a1b3c4d40", "total": "1234.50", "S1234567D": "a key is not text"}
	out := filterJSON(in).(map[string]any)
	flat := toString(out)
	for _, leak := range []string{"S1234567D\"", "123456789012", "F1234567N"} {
		if strings.Contains(strings.Replace(flat, `"S1234567D":`, "", 1), leak) {
			t.Errorf("%s was not masked: %s", leak, flat)
		}
	}
	if out["id"] != "0189a3c2-7b1e-7c41-9d2e-5f8a1b3c4d40" || out["total"] != "1234.50" {
		t.Errorf("an ID or a figure was changed: %v", out)
	}
}

func toString(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
