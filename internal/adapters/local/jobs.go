//go:build local

package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// HTTPJobs starts jobs on the local worker's /jobs endpoint: the local stand-in for Cloud Run jobs.
type HTTPJobs struct {
	URL    string
	Tokens ports.TaskTokens
}

var _ ports.JobRunner = HTTPJobs{}

// Start implements ports.JobRunner.
func (j HTTPJobs) Start(ctx context.Context, job string, slot time.Time) (string, error) {
	token, err := j.Tokens.Token("jobs")
	if err != nil {
		return "", err
	}
	body, _ := json.Marshal(map[string]any{"job": job, "slot": slot})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.URL+"/jobs", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("start job %s: %w", job, err)
	}
	defer func() { _ = resp.Body.Close() }()
	name, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("start job %s: the worker answered %d: %s", job, resp.StatusCode, name)
	}
	return string(name), nil
}
