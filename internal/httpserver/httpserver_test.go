package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cognizhi/fyniq/internal/version"
)

func get(t *testing.T, s *Server, path string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: body is not JSON: %q", path, rec.Body.String())
	}
	return rec.Code, body
}

// req: NFR-IMG-06
func TestHealthReadyVersion(t *testing.T) {
	s := New(version.Info{Service: "api", Version: "0.2.0", Commit: "abc1234", BuildTime: "2026-10-14T00:00:00Z", Flavour: "local"})
	if code, body := get(t, s, "/healthz"); code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("/healthz: %d %v", code, body)
	}
	if code, body := get(t, s, "/version"); code != http.StatusOK || body["version"] != "0.2.0" || body["commit"] != "abc1234" || body["build_time"] == "" {
		t.Fatalf("/version: %d %v", code, body)
	}
	if code, _ := get(t, s, "/readyz"); code != http.StatusOK {
		t.Fatalf("/readyz with no checks: %d", code)
	}
}

// req: NFR-IMG-06
func TestReadyzReportsAFailingDependency(t *testing.T) {
	s := New(version.Info{Service: "api"})
	s.AddReadiness("database", func(context.Context) error { return errors.New("connection refused") })
	s.AddReadiness("blobstore", func(context.Context) error { return nil })
	code, body := get(t, s, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", code)
	}
	failing, _ := body["failing"].(map[string]any)
	if failing["database"] != "connection refused" || failing["blobstore"] != nil {
		t.Fatalf("failing: %v", failing)
	}
}
