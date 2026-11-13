package gcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Cloud Scheduler and Cloud Monitoring, read for scheduler.configured (NFR-OBS-02, CTL-RES-05; p1-operations-dr design D6)
// as the jobs' account, which holds their viewer roles only [live-verify].

// SchedulerAPI and MonitoringAPI are Google's APIs.
const (
	SchedulerAPI  = "https://cloudscheduler.googleapis.com"
	MonitoringAPI = "https://monitoring.googleapis.com"
)

// Scheduling reads the Scheduler's jobs in Region and the alert policies of Project.
type Scheduling struct {
	Scheduler, Monitoring string
	Project, Region       string
	r                     rest
}

var _ ports.Scheduling = (*Scheduling)(nil)

// NewScheduling returns the adapter.
func NewScheduling(scheduler, monitoring, project, region string, tokens oauth2.TokenSource, client *http.Client) *Scheduling {
	return &Scheduling{Scheduler: scheduler, Monitoring: monitoring, Project: project, Region: region, r: rest{tokens: tokens, client: client}}
}

// SchedulerJobs implements ports.Scheduling.
func (s *Scheduling) SchedulerJobs(ctx context.Context) ([]ports.SchedulerJob, error) {
	var out []ports.SchedulerJob
	q := url.Values{"pageSize": {"500"}}
	for page := 0; page < 20; page++ {
		var res struct {
			Jobs []struct {
				Name     string `json:"name"`
				State    string `json:"state"`
				Schedule string `json:"schedule"`
			} `json:"jobs"`
			Next string `json:"nextPageToken"`
		}
		u := fmt.Sprintf("%s/v1/projects/%s/locations/%s/jobs?%s", s.Scheduler, s.Project, s.Region, q.Encode())
		if err := s.r.do(ctx, http.MethodGet, u, "", nil, &res); err != nil {
			return nil, fmt.Errorf("cloud scheduler: %w", err)
		}
		for _, j := range res.Jobs {
			out = append(out, ports.SchedulerJob{Name: j.Name[strings.LastIndexByte(j.Name, '/')+1:], Enabled: j.State == "ENABLED", Schedule: j.Schedule})
		}
		if res.Next == "" {
			return out, nil
		}
		q.Set("pageToken", res.Next)
	}
	return out, nil
}

// AlertPolicies implements ports.Scheduling.
func (s *Scheduling) AlertPolicies(ctx context.Context) ([]ports.AlertPolicy, error) {
	var out []ports.AlertPolicy
	q := url.Values{"pageSize": {"1000"}}
	for page := 0; page < 20; page++ {
		var res struct {
			Policies []struct {
				DisplayName string   `json:"displayName"`
				Enabled     *bool    `json:"enabled"`
				Channels    []string `json:"notificationChannels"`
			} `json:"alertPolicies"`
			Next string `json:"nextPageToken"`
		}
		u := fmt.Sprintf("%s/v3/projects/%s/alertPolicies?%s", s.Monitoring, s.Project, q.Encode())
		if err := s.r.do(ctx, http.MethodGet, u, "", nil, &res); err != nil {
			return nil, fmt.Errorf("cloud monitoring: %w", err)
		}
		for _, p := range res.Policies {
			out = append(out, ports.AlertPolicy{Name: p.DisplayName, Enabled: p.Enabled == nil || *p.Enabled, Channels: len(p.Channels)})
		}
		if res.Next == "" {
			return out, nil
		}
		q.Set("pageToken", res.Next)
	}
	return out, nil
}
