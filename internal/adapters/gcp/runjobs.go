package gcp

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// RunAPI is the Cloud Run Admin API.
const RunAPI = "https://run.googleapis.com"

// RunJobs is the JobRunner port on Cloud Run jobs (ADR-T7): the jobs image runs one platform job's slot
// as `jobs run-slot <job> <slot>`, started by overriding the Cloud Run job's arguments. Start returns
// once the execution is launched; the job reports its own completion to its slot and heartbeat.
type RunJobs struct {
	Endpoint string
	Job      string // projects/…/locations/…/jobs/fyniq-jobs
	r        rest
}

var _ ports.JobRunner = (*RunJobs)(nil)

// NewRunJobs returns the adapter.
func NewRunJobs(endpoint, job string, tokens oauth2.TokenSource, client *http.Client) *RunJobs {
	return &RunJobs{Endpoint: strings.TrimSuffix(endpoint, "/"), Job: job, r: rest{tokens: tokens, client: client}}
}

// Start implements ports.JobRunner.
func (j *RunJobs) Start(ctx context.Context, job string, slot time.Time) (string, error) {
	body, err := jsonBody(map[string]any{"overrides": map[string]any{"taskCount": 1,
		"containerOverrides": []map[string]any{{"args": []string{"run-slot", job, slot.UTC().Format(time.RFC3339)}}}}})
	if err != nil {
		return "", err
	}
	var op struct {
		Name     string `json:"name"`
		Metadata struct {
			Name string `json:"name"` // the execution
		} `json:"metadata"`
	}
	if err := j.r.do(ctx, http.MethodPost, fmt.Sprintf("%s/v2/%s:run", j.Endpoint, j.Job), "application/json", body, &op); err != nil {
		return "", fmt.Errorf("start job %s: %w", job, err)
	}
	return cmp.Or(op.Metadata.Name, op.Name), nil
}
