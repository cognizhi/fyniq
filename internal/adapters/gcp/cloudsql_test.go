package gcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
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
