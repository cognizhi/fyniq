//go:build local

package api

import (
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
)

// FR-INT-15 (p2-api-third-parties task 3): the semantic metrics endpoint is open to any authorised client, not only a person or the assistant: an integration client acting for an
// Accountant with fyniq.read.reports reads a measure; the same client with only fyniq.read.sales is refused, and an agent-class token with the reports scope reads (agents read).
// req: FR-INT-15
// req: FR-INT-02
// asvs: V4.1.1
func TestAnIntegrationClientWithTheReportsScopeAsksTheSemanticLayer(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-10"}
	client := func(class string, scopes ...string) string {
		return e.principal(authn.Principal{Via: authn.ViaToken, Subject: "client-" + class, Client: "client-" + class, Class: class, Organisation: e.books.Org,
			Roles: []authz.Role{authz.Accountant}, Scopes: scopes, Scoped: true})
	}
	if r := e.call(http.MethodPost, "/v1/metrics:query", client(authn.ClassIntegration, "fyniq.read.reports"), body); r.Status != http.StatusOK {
		t.Errorf("an integration client with the reports scope: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/metrics:query", client(authn.ClassIntegration, "fyniq.read.sales"), body); r.Status != http.StatusForbidden {
		t.Errorf("an integration client without it: %d %s", r.Status, r.Raw)
	}
	if r := e.call(http.MethodPost, "/v1/metrics:query", client(authn.ClassAgent, "fyniq.read.reports"), body); r.Status != http.StatusOK {
		t.Errorf("an agent with the reports scope: %d %s", r.Status, r.Raw)
	}
}
