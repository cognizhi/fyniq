//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/domain/security"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// What an AI app does that is worth a look is an alert to the Owners and the DPO, once per rule and person in the window (AI-45,
// SEC-31): an app or a network a person has not used before, scopes refused again and again, and a volume spike. It is an audit
// event of the platform and the log entry Cloud Monitoring alerts on; no token, argument or result is in it.
// req: AI-45
// req: SEC-31
// req: QA-MCP-01
// asvs: V16.3.1, V16.3.2, V16.4.1
func TestAnAIAppsUnusualUseRaisesAnAlertOncePerWindow(t *testing.T) {
	f := newFounders(t)
	e := f.e
	e.api.deps.AlertThresholds = security.Thresholds{DeviceDays: 90, TravelNearHours: 2, TravelFarHours: 6, ExportsPerHour: 20, RefusalsPer10: 20,
		AlertWindowMins: 60, MCPCallsPer5: 4}
	service, err := e.services.Token(ServiceMCP)
	if err != nil {
		t.Fatal(err)
	}
	user := uuid.MustParse(f.ahkow)
	token := e.principal(authn.Principal{Via: authn.ViaToken, Subject: user.String(), UserID: user, Client: "mcp-delegation", Class: authn.ClassAgent,
		Organisation: e.books.Org, Roles: []authz.Role{authz.Owner}, Scopes: []string{"fyniq.read.reports"}})
	y, m, d := e.clock.Now().In(appclock.Singapore).Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	for i, kv := range [][2]string{{"features.mcp", "true"}, {"mcp.clients", "claude-code"}, {"mcp.requests_per_minute", "600"}} {
		was := map[string]string{"features.mcp": "false", "mcp.clients": "", "mcp.requests_per_minute": "60"}[kv[0]]
		e.do(func(ctx context.Context, u storage.UnitOfWork) error {
			_, err := u.Settings().Record(ctx, kv[0], was, kv[1], from, i+1)
			return err
		})
	}
	got := e.call(http.MethodGet, "/v1/me/mcp", f.as(f.ahkow), nil)
	if r := e.call(http.MethodPut, "/v1/me/mcp", f.as(f.ahkow), map[string]any{"opted_in": true}, "If-Match", got.str("etag")); r.Status != 200 {
		t.Fatalf("opting in: %d %s", r.Status, r.Raw)
	}
	call := func(tool string, input map[string]any, ip string) response {
		return e.call(http.MethodPost, "/internal/v1/mcp/tool-calls", token,
			map[string]any{"app": "claude-code", "tool": tool, "input": input, "ip": ip}, ServiceTokenHeader, service)
	}
	revenue := map[string]any{"measure": "revenue", "from": "2026-10", "to": "2026-10"}
	if r := call("metrics.query", revenue, "203.0.113.7"); r.Status != 200 {
		t.Fatalf("the first call: %d %s", r.Status, r.Raw)
	}
	// The first use of an app, and of a network, is an alert each, and only the first time.
	if n, nn := alertsOf(t, e, security.RuleMCPNewApp), alertsOf(t, e, security.RuleMCPNewNetwork); n != 1 || nn != 1 {
		t.Fatalf("after the first call: %d new-app and %d new-network alerts, want 1 and 1", n, nn)
	}
	call("metrics.query", revenue, "203.0.113.9") // the same /24: neither is new
	if n, nn := alertsOf(t, e, security.RuleMCPNewApp), alertsOf(t, e, security.RuleMCPNewNetwork); n != 1 || nn != 1 {
		t.Errorf("a second call: %d and %d alerts, want 1 and 1", n, nn)
	}
	call("metrics.query", revenue, "198.51.100.4")                 // another network
	if nn := alertsOf(t, e, security.RuleMCPNewNetwork); nn != 1 { // one alert per rule and person in the window, whatever the networks
		t.Errorf("another network: %d alerts", nn)
	}
	// A volume spike: more than the threshold's calls in 5 minutes, one alert for the window however many follow.
	for range 4 {
		call("metrics.query", revenue, "203.0.113.7")
	}
	if n := alertsOf(t, e, security.RuleMCPSpike); n != 1 {
		t.Errorf("a spike: %d alerts, want 1", n)
	}
	call("metrics.query", revenue, "203.0.113.7")
	if n := alertsOf(t, e, security.RuleMCPSpike); n != 1 {
		t.Errorf("the spike goes on: %d alerts, want still 1", n)
	}
	// Scopes refused again and again.
	for range 3 {
		if r := call("records.get", map[string]any{"kind": "invoice", "id": uuid.NewString()}, "203.0.113.7"); r.Status != http.StatusForbidden {
			t.Fatalf("a call without the scope: %d %s", r.Status, r.Raw)
		}
	}
	if n := alertsOf(t, e, security.RuleMCPDenied); n != 1 {
		t.Errorf("repeated denial: %d alerts, want 1", n)
	}
	// The alert names no token and no argument.
	if trail := auditTrail(t, e); strings.Contains(trail, "ory_at_") || strings.Contains(trail, "Bearer ") {
		t.Error("an alert kept a credential")
	}
}
