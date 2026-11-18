package gcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// The export asks Cloud SQL to export the database as SQL to the bucket, as the jobs' account, and waits for its
// operation; a failed operation is an error with Cloud SQL's reason (DR-02, DR-04).
// req: DR-02
// req: DR-04
func TestCloudSQLExportsAndWaits(t *testing.T) {
	var asked map[string]any
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer jobs-token" {
			http.Error(w, "unexpected", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/fyniq-prod/instances/fyniq-production/export":
			_ = json.NewDecoder(r.Body).Decode(&asked)
			_, _ = w.Write([]byte(`{"name":"op-1","status":"PENDING"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/fyniq-prod/operations/op-1":
			reads++
			if reads < 2 {
				_, _ = w.Write([]byte(`{"name":"op-1","status":"RUNNING"}`))
				return
			}
			_, _ = w.Write([]byte(`{"name":"op-1","status":"DONE"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/fyniq-prod/instances/broken/export":
			_, _ = w.Write([]byte(`{"name":"op-2","status":"DONE","error":{"errors":[{"code":"ERROR_RDBMS","message":"the bucket refused the write"}]}}`))
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	tokens := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "jobs-token"})
	c := NewCloudSQL(srv.URL, "fyniq-prod", "fyniq-production", "fyniq", tokens, srv.Client())
	c.Poll = time.Millisecond
	what, err := c.Export(context.Background(), "gs://fyniq-production-db-weekly/weekly/fyniq-2099-02-07.sql.gz")
	if err != nil || reads != 2 || !strings.Contains(what, "fyniq-2099-02-07") {
		t.Fatalf("the export: %q %v after %d reads", what, err, reads)
	}
	ctx, _ := asked["exportContext"].(map[string]any)
	if ctx["fileType"] != "SQL" || ctx["uri"] != "gs://fyniq-production-db-weekly/weekly/fyniq-2099-02-07.sql.gz" {
		t.Errorf("asked: %v", asked)
	}
	if dbs, _ := ctx["databases"].([]any); len(dbs) != 1 || dbs[0] != "fyniq" {
		t.Errorf("the databases: %v", ctx["databases"])
	}
	broken := NewCloudSQL(srv.URL, "fyniq-prod", "broken", "fyniq", tokens, srv.Client())
	if _, err := broken.Export(context.Background(), "gs://b/x.sql.gz"); err == nil || !strings.Contains(err.Error(), "the bucket refused the write") {
		t.Errorf("a failed operation: %v", err)
	}
}

// The monthly drill's copy: Cloud SQL clones the instance at the point in time into an instance named for the drill, and
// the copy's private address is where the drill connects; removing it turns its deletion protection off and deletes it,
// a copy already gone is not an error, and no instance but a drill copy is ever removed (DR-06, CTL-RES-02).
// req: DR-06
// req: CTL-RES-02
// asvs: V8.2.1
func TestCloudSQLClonesAtAPointInTimeAndRemovesOnlyItsCopies(t *testing.T) {
	var asked, patched map[string]any
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/fyniq-prod/instances/fyniq-production/clone":
			_ = json.NewDecoder(r.Body).Decode(&asked)
			_, _ = w.Write([]byte(`{"name":"op-c","status":"RUNNING"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/projects/fyniq-prod/operations/"):
			_, _ = w.Write([]byte(`{"name":"op","status":"DONE"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/projects/fyniq-prod/instances/fyniq-production-drill-20990301-020000":
			_, _ = w.Write([]byte(`{"ipAddresses":[{"type":"OUTGOING","ipAddress":"34.1.2.3"},{"type":"PRIVATE","ipAddress":"10.20.0.7"}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/v1/projects/fyniq-prod/instances/fyniq-production-drill-20990301-020000":
			_ = json.NewDecoder(r.Body).Decode(&patched)
			_, _ = w.Write([]byte(`{"name":"op-p","status":"PENDING"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/projects/fyniq-prod/instances/fyniq-production-drill-20990301-020000":
			_, _ = w.Write([]byte(`{"name":"op-d","status":"PENDING"}`))
		case r.URL.Path == "/v1/projects/fyniq-prod/instances/fyniq-production-drill-20990201-020000":
			http.Error(w, `{"error":{"code":404}}`, http.StatusNotFound)
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	c := NewCloudSQL(srv.URL, "fyniq-prod", "fyniq-production", "fyniq", nil, srv.Client())
	c.Poll, c.Clock = time.Millisecond, clock.NewFrozen(time.Date(2099, 3, 1, 2, 0, 0, 0, time.UTC))
	at := time.Date(2099, 3, 1, 1, 0, 0, 0, time.UTC)
	got, err := c.Clone(context.Background(), at)
	if err != nil || got != (ports.CloneTarget{Name: "fyniq-production-drill-20990301-020000", Host: "10.20.0.7"}) {
		t.Fatalf("Clone = %+v, %v", got, err)
	}
	cc, _ := asked["cloneContext"].(map[string]any)
	if cc["destinationInstanceName"] != got.Name || cc["pointInTime"] != "2099-03-01T01:00:00Z" {
		t.Errorf("asked: %v", asked)
	}
	if err := c.Remove(context.Background(), got.Name); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if s, _ := patched["settings"].(map[string]any); s["deletionProtectionEnabled"] != false {
		t.Errorf("patched: %v", patched)
	}
	if !slices.Contains(calls, "DELETE /v1/projects/fyniq-prod/instances/fyniq-production-drill-20990301-020000") {
		t.Errorf("not deleted: %v", calls)
	}
	if err := c.Remove(context.Background(), "fyniq-production-drill-20990201-020000"); err != nil {
		t.Errorf("a copy already gone: %v", err)
	}
	before := len(calls)
	for _, name := range []string{"fyniq-production", "fyniq-staging-drill-1", "other"} {
		if err := c.Remove(context.Background(), name); err == nil || !strings.Contains(err.Error(), "not a drill copy") {
			t.Errorf("Remove(%q): %v", name, err)
		}
	}
	if len(calls) != before {
		t.Errorf("a refused removal reached Cloud SQL: %v", calls[before:])
	}
}
