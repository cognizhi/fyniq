package gcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// scheduler.configured reads the Scheduler's jobs and the alert policies as the jobs' account, every page of them: a
// job's name is the last part of its resource name, a policy with no "enabled" is on (CTL-RES-05).
// req: CTL-RES-05
// req: NFR-OBS-02
func TestSchedulingReadsTheJobsAndThePolicies(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/v1/projects/fyniq-prod/locations/asia-southeast1/jobs" && r.URL.Query().Get("pageToken") == "":
			_, _ = w.Write([]byte(`{"jobs":[{"name":"projects/fyniq-prod/locations/asia-southeast1/jobs/tick","state":"ENABLED","schedule":"*/5 * * * *"}],"nextPageToken":"p2"}`))
		case r.URL.Path == "/v1/projects/fyniq-prod/locations/asia-southeast1/jobs":
			_, _ = w.Write([]byte(`{"jobs":[{"name":"projects/fyniq-prod/locations/asia-southeast1/jobs/old","state":"PAUSED"}]}`))
		case r.URL.Path == "/v3/projects/fyniq-prod/alertPolicies":
			_, _ = w.Write([]byte(`{"alertPolicies":[{"displayName":"Fyniq production: heartbeat tick absent 30 minutes","notificationChannels":["c/1"]},
			  {"displayName":"Fyniq production: a platform job's heartbeat is late","enabled":false}]}`))
		default:
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	s := NewScheduling(srv.URL, srv.URL, "fyniq-prod", "asia-southeast1", oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "jobs-token"}), srv.Client())
	jobs, err := s.SchedulerJobs(context.Background())
	if err != nil || len(jobs) != 2 || jobs[0] != (ports.SchedulerJob{Name: "tick", Enabled: true, Schedule: "*/5 * * * *"}) || jobs[1].Enabled ||
		auth != "Bearer jobs-token" {
		t.Fatalf("the jobs: %+v, %v (%q)", jobs, err, auth)
	}
	policies, err := s.AlertPolicies(context.Background())
	if err != nil || len(policies) != 2 || !policies[0].Enabled || policies[0].Channels != 1 || policies[1].Enabled {
		t.Fatalf("the policies: %+v, %v", policies, err)
	}
}
