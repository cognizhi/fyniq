//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The AI's read tools run through the API as the turn's person (AI-01, AI-P1, AI-68, AI-04; p1-ai-assistant design D4,
// D5, D6): each through its public read operation with the turn's own token, so a person who may not read something
// does not read it through the assistant either; each figure comes back as a typed fact, the same fact the operation
// gives; a record keeps only the fields the AI reads; an input the tool does not take is refused with the field; calc
// is the AI service's; only the AI service runs a tool, for a live turn.
// req: AI-P1
// req: AI-01
// req: AI-68
// req: AI-04
// asvs: V8.2.1, V8.2.2
func TestTheReadToolsReadAsThePerson(t *testing.T) {
	e := newEnv(t)
	// Invited while the founders set up, so the staff member keeps the role once they join.
	code := e.call(http.MethodPost, "/v1/members/invitations", e.as(authz.Owner), map[string]any{"display_name": "Staff", "roles": []string{"employee"}}).str("code")
	f := foundersIn(e)
	invoice, _ := e.invoiceDue("2026-10-01", "2026-10-31")
	service, err := e.services.Token(ServiceAI)
	if err != nil {
		t.Fatal(err)
	}
	grant := func(user string) string {
		t.Helper()
		var token string
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			token, err = e.api.issueAIGrant(ctx, u, e.books.Org, uuid.MustParse(user), uuid.New())
			return err
		})
		return token
	}
	owner := grant(f.ahkow)
	staff := e.call(http.MethodPost, "/v1/members", e.principal(authn.Principal{Subject: "staff-" + uuid.NewString(), Client: "web"}),
		map[string]any{"invitation_code": code, "display_name": "Staff"}).str("user_id")
	if staff == "" {
		t.Fatal("the staff member did not join")
	}
	employee := grant(staff)
	tool := func(token, name string, input map[string]any) response {
		t.Helper()
		return e.call(http.MethodPost, "/internal/v1/tool-calls", token, map[string]any{"tool": name, "input": input}, ServiceTokenHeader, service)
	}
	// refused checks an internal operation's refusal directly: problemIs reads the public contract's codes.
	refused := func(r response, code string) {
		t.Helper()
		if r.Status != http.StatusUnprocessableEntity || r.str("code") != code || r.str("instance") == "" {
			t.Errorf("want %s: %d %s", code, r.Status, r.Raw)
		}
	}
	facts := func(r response) map[string]map[string]any {
		t.Helper()
		if r.Status != http.StatusOK {
			t.Fatalf("callAITool: %d %s", r.Status, r.Raw)
		}
		out := map[string]map[string]any{}
		for _, f := range r.Body["facts"].([]any) {
			m := f.(map[string]any)
			out[m["id"].(string)] = m
		}
		return out
	}

	// metrics.query gives the semantic layer's own facts.
	m := tool(owner, "metrics.query", map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-10"})
	direct := e.call(http.MethodPost, "/v1/metrics:query", e.as(authz.Controller), map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-10"})
	want := direct.Body["facts"].([]any)[0].(map[string]any)
	if got := facts(m)[want["id"].(string)]; got == nil || got["value"] != want["value"] || got["unit"] != "amount" || m.Body["version"] != float64(1) {
		t.Errorf("metrics.query: %s, the operation's: %s", m.Raw, direct.Raw)
	}

	// records.get: the invoice's figures are facts, and only the fields the AI reads come back.
	g := tool(owner, "records.get", map[string]any{"kind": "invoice", "id": invoice})
	content, _ := g.Body["content"].(map[string]any)
	total, _ := content["total"].(map[string]any)
	totalFact := facts(g)[total["fact"].(string)]
	if totalFact == nil || totalFact["measure"] != "invoice.total" || totalFact["currency"] != "SGD" || totalFact["member"] != invoice ||
		totalFact["lineage"] != "/v1/invoices/"+invoice || totalFact["value"] == "" {
		t.Errorf("the invoice's total: %s", g.Raw)
	}
	for _, field := range []string{"sends", "notes", "prepared_by", "posted_by", "approval_asked", "links", "ledger_impact"} {
		if _, ok := content[field]; ok {
			t.Errorf("records.get read the invoice's %s", field)
		}
	}
	refused(tool(owner, "records.get", map[string]any{"kind": "credit_note", "id": invoice}), CodeToolRefused)

	// records.search finds it, its amount a fact.
	s := tool(owner, "records.search", map[string]any{"q": "100", "kind": "invoices"})
	if r, _ := s.Body["content"].(map[string]any)["results"].([]any); s.Status != http.StatusOK || len(r) == 0 ||
		r[0].(map[string]any)["amount"].(map[string]any)["fact"] == nil {
		t.Errorf("records.search: %d %s", s.Status, s.Raw)
	}

	// checks.list and checks.explain: the counts are facts; a filter keeps only its checks.
	c := tool(owner, "checks.list", map[string]any{"area": "books"})
	checks, _ := c.Body["content"].(map[string]any)["checks"].([]any)
	if c.Status != http.StatusOK || len(checks) == 0 {
		t.Fatalf("checks.list: %d %s", c.Status, c.Raw)
	}
	for _, ch := range checks {
		if m := ch.(map[string]any); m["area"] != "Books" || m["count"].(map[string]any)["fact"] == nil {
			t.Errorf("checks.list kept %v", m)
		}
	}
	if x := tool(owner, "checks.explain", map[string]any{"key": "books.balance-sheet-balances"}); x.Status != http.StatusOK ||
		x.Body["content"].(map[string]any)["key"] != "books.balance-sheet-balances" {
		t.Errorf("checks.explain: %d %s", x.Status, x.Raw)
	}

	// The person's own rights: an employee reads no reports and no invoices through the assistant either.
	refused(tool(employee, "metrics.query", map[string]any{"measure": "revenue"}), CodeToolRefused)
	if r := tool(employee, "records.get", map[string]any{"kind": "invoice", "id": invoice}); r.Status != http.StatusUnprocessableEntity ||
		strings.Contains(r.Raw, `"total"`) {
		t.Errorf("an employee reading an invoice: %d %s", r.Status, r.Raw)
	}

	// An input the tool does not take, calc, and a caller that is not the AI service.
	refused(tool(owner, "metrics.query", map[string]any{"measure": "revenue", "sql": "SELECT 1"}), CodeToolInput)
	refused(tool(owner, "records.get", map[string]any{"kind": "payroll", "id": invoice}), CodeToolInput)
	refused(tool(owner, "calc", map[string]any{"op": "add", "operands": []any{map[string]any{"constant": "1"}, map[string]any{"constant": "2"}}}), CodeToolElse)
	if r := tool(owner, "records.update", map[string]any{}); r.Status != http.StatusUnprocessableEntity {
		t.Errorf("a tool not the registry's: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/internal/v1/tool-calls", owner, map[string]any{"tool": "metrics.query", "input": map[string]any{"measure": "revenue"}}); r.Status != http.StatusUnauthorized {
		t.Errorf("without the service token: %d", r.Status)
	}
	if r := tool(f.as(f.ahkow, authz.Owner), "metrics.query", map[string]any{"measure": "revenue"}); r.Status != http.StatusUnauthorized {
		t.Errorf("a person's own credential: %d", r.Status)
	}
}
