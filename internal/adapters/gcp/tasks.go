package gcp

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/cognizhi/fyniq/internal/ports"
)

// TasksAPI is Cloud Tasks' REST API.
const TasksAPI = "https://cloudtasks.googleapis.com"

// TaskClass is where one worker class's tasks go (§12.11): its queue, its handler and the identity
// Cloud Tasks signs the request as, for the audience the handler checks.
type TaskClass struct {
	Queue          string `json:"queue"`           // projects/…/locations/…/queues/worker
	URL            string `json:"url"`             // the class's service, e.g. https://worker-….run.app
	ServiceAccount string `json:"service_account"` // the invoker Cloud Tasks signs the OIDC token as
	Audience       string `json:"audience"`        // the token's audience: what the handler checks
}

// Tasks is the Queue port on Cloud Tasks (ADR-T7): each outbox row becomes an HTTP task to its worker
// class's /tasks endpoint, carrying an OIDC token Cloud Tasks mints for the class's invoker account. The
// task is named after the row, so a second Enqueue of the same row is refused by Cloud Tasks and taken
// as done (the outbox's at-most-once delivery per name).
type Tasks struct {
	Endpoint string
	Classes  map[string]TaskClass
	r        rest
}

var _ ports.Queue = (*Tasks)(nil)

// NewTasks returns the adapter.
func NewTasks(endpoint string, classes map[string]TaskClass, tokens oauth2.TokenSource, client *http.Client) *Tasks {
	return &Tasks{Endpoint: strings.TrimSuffix(endpoint, "/"), Classes: classes, r: rest{tokens: tokens, client: client}}
}

var taskName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,500}$`)

// Enqueue implements ports.Queue.
func (q *Tasks) Enqueue(ctx context.Context, t ports.Task) error {
	c, ok := q.Classes[t.Queue]
	if !ok {
		return fmt.Errorf("no worker class %q to send task %s to", t.Queue, t.Name)
	}
	if !taskName.MatchString(t.Name) {
		return fmt.Errorf("task name %q is not a Cloud Tasks name (letters, digits, hyphens and underscores)", t.Name)
	}
	deadline := cmp.Or(t.Deadline, 10*time.Minute)
	if deadline < 15*time.Second || deadline > 30*time.Minute {
		return fmt.Errorf("task %s: a deadline of %s is outside Cloud Tasks' 15 s to 30 min", t.Name, deadline)
	}
	body, err := jsonBody(map[string]any{"task": map[string]any{
		"name":             c.Queue + "/tasks/" + t.Name,
		"dispatchDeadline": fmt.Sprintf("%ds", int(deadline/time.Second)),
		"httpRequest": map[string]any{
			"httpMethod": "POST", "url": strings.TrimSuffix(c.URL, "/") + "/tasks",
			"headers": map[string]string{"Content-Type": "application/json"}, "body": base64.StdEncoding.EncodeToString(t.Payload),
			"oidcToken": map[string]string{"serviceAccountEmail": c.ServiceAccount, "audience": c.Audience},
		},
	}})
	if err != nil {
		return err
	}
	err = q.r.do(ctx, http.MethodPost, fmt.Sprintf("%s/v2/%s/tasks", q.Endpoint, c.Queue), "application/json", body, nil)
	if errors.Is(err, errConflict) {
		return nil // the name is taken: this row's task exists already
	}
	return err
}
