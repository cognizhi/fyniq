//go:build local

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/adapters/postgres"
	"github.com/cognizhi/fyniq/internal/setupcode"
)

// The super-admin console reads the platform's health and the Super admins (FR-ADM-07): a Super admin reads both, the health
// says whether the platform is degraded and why, a second Super admin ends the warning, and nobody else reads either.
// req: FR-ADM-07
// asvs: V8.2.1, V8.3.1
func TestTheSuperAdminConsoleReadsHealthAndSuperAdmins(t *testing.T) {
	ctx := context.Background()
	s := startSessions(t)
	desk := s.browser()
	desk.signIn("founder-a", true)
	code, hash := setupcode.New()
	if err := postgres.SetSetupCode(ctx, s.migrator, hash); err != nil {
		t.Fatal(err)
	}
	if r := desk.call(http.MethodPost, "/v1/setup", "same-origin", map[string]any{"setup_code": code, "display_name": "Mei Ling Tan",
		"organisation": map[string]any{"name": "Cognizhi Pte. Ltd.", "base_currency": "SGD"}}); r.Status != http.StatusCreated {
		t.Fatalf("setup: %d %s", r.Status, r.Raw)
	}
	admins := desk.call(http.MethodGet, "/v1/installation/super-admins", "same-origin", nil)
	items, _ := admins.Body["items"].([]any)
	if admins.Status != 200 || len(items) != 1 || items[0].(map[string]any)["display_name"] != "Mei Ling Tan" || admins.str("warning") == "" {
		t.Fatalf("the Super admins: %d %s", admins.Status, admins.Raw)
	}
	h := desk.call(http.MethodGet, "/v1/installation/health", "same-origin", nil)
	if h.Status != 200 || h.Body["outbox"] == nil || h.Body["jobs"] == nil || h.Body["reasons"] == nil {
		t.Fatalf("the health: %d %s", h.Status, h.Raw)
	}
	// No tick has run in a fresh installation: degraded, and it says why.
	if h.str("status") != "degraded" {
		t.Fatalf("a platform whose tick never ran: %s", h.Raw)
	}
	other := s.browser()
	other.signIn("someone-else", true)
	s.problemIs(other.call(http.MethodGet, "/v1/installation/health", "same-origin", nil), "getPlatformHealth", 403, CodeNotSuperAdmin)
	s.problemIs(other.call(http.MethodGet, "/v1/installation/super-admins", "same-origin", nil), "listSuperAdmins", 403, CodeNotSuperAdmin)
}
