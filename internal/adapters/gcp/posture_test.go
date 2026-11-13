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

// The posture adapter asks Security Command Center for the active findings of high or critical severity, as the
// jobs' account, page by page, and reads each one's severity, category, resource and when it was first found (SEC-33).
// req: SEC-33
// req: CTL-RES-04
// asvs: V15.2.1
func TestSecurityCenterListsTheActiveFindings(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/projects/fyniq-prod/sources/-/locations/global/findings" || r.Method != http.MethodGet ||
			r.Header.Get("Authorization") != "Bearer jobs-token" {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		q := r.URL.Query()
		asked = append(asked, q.Get("filter")+" page="+q.Get("pageToken"))
		page := map[string]any{"listFindingsResults": []any{map[string]any{"finding": map[string]any{
			"name": "organizations/1/sources/2/locations/global/findings/a", "category": "PUBLIC_BUCKET_ACL", "severity": "HIGH",
			"resourceName": "//storage.googleapis.com/fyniq-exports", "createTime": "2099-01-02T03:04:05Z"}}}, "nextPageToken": "two"}
		if q.Get("pageToken") == "two" {
			page = map[string]any{"listFindingsResults": []any{map[string]any{"finding": map[string]any{
				"name": "organizations/1/sources/2/locations/global/findings/b", "category": "OPEN_FIREWALL", "severity": "CRITICAL",
				"resourceName": "//compute.googleapis.com/fw", "createTime": "2099-01-05T00:00:00Z"}}}}
		}
		_ = json.NewEncoder(w).Encode(page)
	}))
	defer srv.Close()
	scc := NewSecurityCenter(srv.URL, "projects/fyniq-prod", oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "jobs-token"}), srv.Client())
	got, err := scc.Findings(context.Background())
	if err != nil || len(got) != 2 {
		t.Fatalf("findings: %+v, %v", got, err)
	}
	if got[0].Severity != "HIGH" || got[0].Category != "PUBLIC_BUCKET_ACL" || got[0].Resource != "//storage.googleapis.com/fyniq-exports" ||
		!got[0].FirstSeen.Equal(time.Date(2099, 1, 2, 3, 4, 5, 0, time.UTC)) || got[1].Severity != "CRITICAL" {
		t.Errorf("read: %+v", got)
	}
	want := `state="ACTIVE" AND (severity="HIGH" OR severity="CRITICAL")`
	if len(asked) != 2 || asked[0] != want+" page=" || asked[1] != want+" page=two" {
		t.Errorf("asked: %q", asked)
	}
	bad := NewSecurityCenter(srv.URL, "projects/other", nil, srv.Client())
	if _, err := bad.Findings(context.Background()); err == nil || strings.Contains(err.Error(), "unexpected") {
		t.Errorf("a refusal: %v (never the body)", err)
	}
}

// The log adapter reads the Cloud Run services' and jobs' entries since a time, newest first, at most a page, each as
// its text or its JSON (CTL-DP-03).
// req: CTL-DP-03
func TestLoggingReadsTheServicesEntries(t *testing.T) {
	var asked map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/entries:list" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer jobs-token" {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&asked); err != nil {
			http.Error(w, "unexpected", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"entries":[{"textPayload":"listening on :8080"},{"jsonPayload":{"msg":"journal posted"}},{}]}`))
	}))
	defer srv.Close()
	logs := NewLogging(srv.URL, "fyniq-prod", oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "jobs-token"}), srv.Client())
	got, err := logs.Read(context.Background(), time.Date(2099, 1, 2, 3, 0, 0, 0, time.FixedZone("SGT", 8*3600)), 5000)
	if err != nil || len(got) != 2 || got[0] != "listening on :8080" || got[1] != `{"msg":"journal posted"}` {
		t.Fatalf("read: %q, %v", got, err)
	}
	if asked["filter"] != `timestamp>="2099-01-01T19:00:00Z" AND resource.type=("cloud_run_revision" OR "cloud_run_job")` ||
		asked["orderBy"] != "timestamp desc" || asked["pageSize"] != float64(1000) {
		t.Errorf("asked: %v", asked)
	}
	if rn, _ := asked["resourceNames"].([]any); len(rn) != 1 || rn[0] != "projects/fyniq-prod" {
		t.Errorf("asked of: %v", asked["resourceNames"])
	}
}
