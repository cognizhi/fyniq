package gcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports"
)

// SQLAdminAPI is Cloud SQL's admin API.
const SQLAdminAPI = "https://sqladmin.googleapis.com"

// TelemetryAPI is Google Cloud's Telemetry API, whose OTLP endpoint takes the services' traces into Cloud Trace
// (NFR-OBS-01) [live-verify].
const TelemetryAPI = "https://telemetry.googleapis.com"

// CloudSQL exports the database of an instance to Cloud Storage (DR-02, DR-04; p1-operations-dr design D1), as the jobs'
// account, which may export and read the instance; the instance's own account writes the file. For the monthly restore
// drill it clones the instance at a point in time and deletes the clone (DR-06, design D2): the jobs' account may clone the
// instance, and update and delete only its drill copies [live-verify].
type CloudSQL struct {
	Endpoint string
	Project  string
	Instance string
	Database string
	Poll     time.Duration // between reads of the operation; 0 means 10 seconds
	Clock    ports.Clock   // names a drill copy; nil reads the wall clock
	r        rest
}

var (
	_ ports.DatabaseBackup = (*CloudSQL)(nil)
	_ ports.DatabaseClone  = (*CloudSQL)(nil)
)

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

// drillPrefix is how a drill copy of the instance is named: <instance>-drill-<when it was made, UTC>.
func (c *CloudSQL) drillPrefix() string { return c.Instance + "-drill-" }

// Clone implements ports.DatabaseClone: instances.clone at the point in time into a new instance, then its private
// address once the operation is done.
func (c *CloudSQL) Clone(ctx context.Context, at time.Time) (ports.CloneTarget, error) {
	now := clock.System{}.Now()
	if c.Clock != nil {
		now = c.Clock.Now()
	}
	name := c.drillPrefix() + now.UTC().Format("20060102-150405")
	body, err := jsonBody(map[string]any{"cloneContext": map[string]any{"kind": "sql#cloneContext", "destinationInstanceName": name,
		"pointInTime": at.UTC().Format(time.RFC3339)}})
	if err != nil {
		return ports.CloneTarget{}, err
	}
	var op sqlOperation
	u := fmt.Sprintf("%s/v1/projects/%s/instances/%s/clone", c.Endpoint, c.Project, c.Instance)
	if err := c.r.do(ctx, http.MethodPost, u, "application/json", body, &op); err != nil {
		return ports.CloneTarget{}, fmt.Errorf("cloud sql clone: %w", err)
	}
	target := ports.CloneTarget{Name: name}
	if _, err := c.wait(ctx, op, ""); err != nil {
		return target, err
	}
	var inst struct {
		IPAddresses []struct {
			Type      string `json:"type"`
			IPAddress string `json:"ipAddress"`
		} `json:"ipAddresses"`
	}
	if err := c.r.do(ctx, http.MethodGet, c.instance(name), "", nil, &inst); err != nil {
		return target, fmt.Errorf("cloud sql clone %s: %w", name, err)
	}
	for _, a := range inst.IPAddresses {
		if a.Type == "PRIVATE" {
			target.Host = a.IPAddress
		}
	}
	if target.Host == "" {
		return target, fmt.Errorf("cloud sql clone %s has no private address", name)
	}
	return target, nil
}

// Remove implements ports.DatabaseClone: deletion protection off, then instances.delete, each waited for.
func (c *CloudSQL) Remove(ctx context.Context, name string) error {
	if !strings.HasPrefix(name, c.drillPrefix()) {
		return fmt.Errorf("cloud sql: %q is not a drill copy of %s, so it is not removed", name, c.Instance)
	}
	body, err := jsonBody(map[string]any{"settings": map[string]any{"deletionProtectionEnabled": false}})
	if err != nil {
		return err
	}
	var op sqlOperation
	err = c.r.do(ctx, http.MethodPatch, c.instance(name), "application/json", body, &op)
	if errors.Is(err, errNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cloud sql remove %s: %w", name, err)
	}
	if _, err := c.wait(ctx, op, ""); err != nil {
		return err
	}
	op = sqlOperation{}
	if err := c.r.do(ctx, http.MethodDelete, c.instance(name), "", nil, &op); errors.Is(err, errNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("cloud sql remove %s: %w", name, err)
	}
	_, err = c.wait(ctx, op, "")
	return err
}

func (c *CloudSQL) instance(name string) string {
	return fmt.Sprintf("%s/v1/projects/%s/instances/%s", c.Endpoint, c.Project, name)
}
