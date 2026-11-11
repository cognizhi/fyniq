package gcp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// SQLAdminAPI is Cloud SQL's admin API.
const SQLAdminAPI = "https://sqladmin.googleapis.com"

// CloudSQL exports the database of an instance to Cloud Storage (DR-02, DR-04; p1-operations-dr design D1), as the jobs'
// account, which may export and read the instance and nothing else; the instance's own account writes the file
// [live-verify].
type CloudSQL struct {
	Endpoint string
	Project  string
	Instance string
	Database string
	Poll     time.Duration // between reads of the operation; 0 means 10 seconds
	r        rest
}

var _ ports.DatabaseBackup = (*CloudSQL)(nil)

// NewCloudSQL returns the adapter for the instance projects/<project>/instances/<instance> and its database.
func NewCloudSQL(endpoint, project, instance, database string, tokens oauth2.TokenSource, client *http.Client) *CloudSQL {
	return &CloudSQL{Endpoint: endpoint, Project: project, Instance: instance, Database: database, r: rest{tokens: tokens, client: client}}
}

type sqlOperation struct {
	Name   string `json:"name"`
	Status string `json:"status"` // PENDING, RUNNING, DONE
	Error  *struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	} `json:"error"`
}

// Export implements ports.DatabaseBackup: instances.export of the database as SQL to uri, then the operation read until it
// is done.
func (c *CloudSQL) Export(ctx context.Context, uri string) (string, error) {
	body, err := jsonBody(map[string]any{"exportContext": map[string]any{"fileType": "SQL", "uri": uri, "databases": []string{c.Database}}})
	if err != nil {
		return "", err
	}
	var op sqlOperation
	u := fmt.Sprintf("%s/v1/projects/%s/instances/%s/export", c.Endpoint, c.Project, c.Instance)
	if err := c.r.do(ctx, http.MethodPost, u, "application/json", body, &op); err != nil {
		return "", fmt.Errorf("cloud sql export: %w", err)
	}
	return c.wait(ctx, op, "exported "+c.Database+" to "+uri)
}

// wait reads op until it is done, and reports its error, if any.
func (c *CloudSQL) wait(ctx context.Context, op sqlOperation, done string) (string, error) {
	poll := c.Poll
	if poll <= 0 {
		poll = 10 * time.Second
	}
	for op.Status != "DONE" {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("cloud sql operation %s: %w", op.Name, ctx.Err())
		case <-time.After(poll):
		}
		u := fmt.Sprintf("%s/v1/projects/%s/operations/%s", c.Endpoint, c.Project, op.Name)
		if err := c.r.do(ctx, http.MethodGet, u, "", nil, &op); err != nil {
			return "", fmt.Errorf("cloud sql operation: %w", err)
		}
	}
	if op.Error != nil && len(op.Error.Errors) > 0 {
		e := op.Error.Errors[0]
		return "", fmt.Errorf("cloud sql operation %s failed: %s: %s", op.Name, e.Code, e.Message)
	}
	return done, nil
}
