//go:build local

package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/authz"
	appclock "github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/mcpgate"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The MCP gate reads the organisation's switch, its allowlist and the person's opt-in from the database, in that order, and
// admits only when all three hold: closed by default, each level refusing with its own code, an app that is not listed never
// admitted, and a person's opt-out, a removal from the allowlist and the switch off each stop the next admission at once
// (AI-35, AI-38, CTL-MCP-03).
// req: AI-35
// req: AI-38
// req: CTL-MCP-03
// asvs: V8.2.2, V4.1.1
func TestTheMCPGateAdmitsOnlyWhenTheOrganisationTheAppAndThePersonAllow(t *testing.T) {
	f := newFounders(t)
	e := f.e
	user := uuid.MustParse(f.ahkow)
	ahkow := f.as(f.ahkow, authz.Owner, authz.Controller)
	gate := mcpgate.Gate{Store: e.store, Clock: e.clock}
	admit := func(app string) string {
		t.Helper()
		code, _, err := gate.Admit(context.Background(), e.books.Org, user, app)
		if err != nil {
			t.Fatal(err)
		}
		return code
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
	optIn := func(in bool) {
		t.Helper()
		got := e.call(http.MethodGet, "/v1/me/mcp", ahkow, nil)
		if r := e.call(http.MethodPut, "/v1/me/mcp", ahkow, map[string]any{"opted_in": in}, "If-Match", got.str("etag")); r.Status != 200 {
			t.Fatalf("opting: %d %s", r.Status, r.Raw)
		}
	}

	if got := admit("claude-code"); got != mcpgate.CodeOrganisationOff {
		t.Fatalf("a new organisation: %q", got)
	}
	set("features.mcp", "false", "true")
	if got := admit("claude-code"); got != mcpgate.CodeNotAllowed {
		t.Fatalf("on, with no app listed: %q", got)
	}
	set("mcp.clients", "", "claude-desktop")
	if got := admit("claude-code"); got != mcpgate.CodeNotAllowed {
		t.Fatalf("another app listed: %q", got)
	}
	set("mcp.clients", "claude-desktop", "claude-code,claude-desktop")
	if got := admit("claude-code"); got != mcpgate.CodeNotOptedIn {
		t.Fatalf("listed, the person has not chosen: %q", got)
	}
	optIn(true)
	if got := admit("claude-code"); got != "" {
		t.Fatalf("every level allows: %q", got)
	}
	for _, rogue := range []string{"", "rogue-app", "claude-code,claude-desktop", " claude-code"} {
		if got := admit(rogue); got == "" {
			t.Errorf("%q was admitted", rogue)
		}
	}
	// Someone else is not admitted by this person's choice.
	if code, _, err := gate.Admit(context.Background(), e.books.Org, uuid.MustParse(f.weiling), "claude-code"); err != nil || code != mcpgate.CodeNotOptedIn {
		t.Errorf("another person: %q %v", code, err)
	}
	optIn(false)
	if got := admit("claude-code"); got != mcpgate.CodeNotOptedIn {
		t.Errorf("after opting out: %q", got)
	}
	optIn(true)
	set("mcp.clients", "claude-code,claude-desktop", "claude-desktop")
	if got := admit("claude-code"); got != mcpgate.CodeNotAllowed {
		t.Errorf("after the app was taken off: %q", got)
	}
	set("mcp.clients", "claude-desktop", "claude-code")
	set("features.mcp", "true", "false")
	if got := admit("claude-code"); got != mcpgate.CodeOrganisationOff {
		t.Errorf("after the switch was turned off: %q", got)
	}
	if got := admit("claude-code"); got == "" {
		t.Error("the gate admitted with the switch off")
	}
}
