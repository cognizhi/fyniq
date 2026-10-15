package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/app"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/version"
)

// infrastructure are the routes outside the public API: probes and version (NFR-IMG-06), the
// Scheduler's tick (§12.11, its own credential) and the API's document itself.
var infrastructure = []string{
	"GET /healthz", "GET /readyz", "GET /version", "POST /internal/tick",
	"GET /v1/openapi.json", "GET /v1/openapi.yaml", "GET /v1/openapi-3.0.json", "GET /v1/openapi-3.0.yaml",
	// The OAuth server's login and consent pages (ADR-T3): browser steps of the authorisation flow, not
	// API operations.
	"GET /auth/login", "GET /auth/consent", "POST /auth/consent",
	// Signing in to the web app (SEC-05, SEC-08): browser navigations to a provider and back, and the
	// sign-out form; and in the local build the dev sign-in page that stands in for the provider (§12.8).
	"GET /auth/signin", "GET /auth/callback", "POST /auth/signout", "GET /auth/dev/authorize", "POST /auth/dev/authorize",
}

// Every route the API serves is a documented operation or named infrastructure: no private
// endpoints (the server half of QA-API-03).
// req: QA-API-03
func TestEveryRouteIsDocumented(t *testing.T) {
	t.Setenv("FYNIQ_TICK_INTERVAL", "off")
	t.Setenv("FYNIQ_OAUTH_ADMIN_URL", "http://127.0.0.1:9") // registers the login and consent pages in the local build
	t.Setenv("FYNIQ_DATA_DIR", t.TempDir())
	s := httpserver.New(version.Info{Service: "api"})
	if err := routes(s, app.Deps{Clock: clock.NewFrozen(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/openapi.json", nil))
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil || len(doc.Paths) == 0 {
		t.Fatalf("the document at /v1/openapi.json: %v (%d)", err, rec.Code)
	}
	documented := map[string]bool{}
	for path, methods := range doc.Paths {
		for m := range methods {
			documented[strings.ToUpper(m)+" "+path] = true
		}
	}
	served := s.Routes()
	for _, r := range served {
		if !documented[r] && !slices.Contains(infrastructure, r) {
			t.Errorf("%s is served but is neither in the API's document nor named infrastructure", r)
		}
	}
	for op := range documented {
		if !slices.Contains(served, op) {
			t.Errorf("%s is documented but not served", op)
		}
	}
	if len(served) < len(documented)+4 {
		t.Fatalf("only %d routes: %v", len(served), served)
	}
}
