package ports

import (
	"context"
	"errors"
	"time"
)

// Task is one delivery of an outbox row to a worker class (ADR-T7).
type Task struct {
	Queue    string        // the worker class: worker, executor or jobs
	Name     string        // unique: a second Enqueue with the same name is a no-op
	Payload  []byte        // a JSON object
	Deadline time.Duration // how long the handler may take (Cloud Tasks: 15 s to 30 min); 0 means 10 min
}

// Queue delivers tasks to their worker class's HTTP handler, with that class's credential, and
// retries a delivery that fails. Production: Cloud Tasks. Local: a push dispatcher with Cloud Tasks'
// semantics (PRD rev 0.23). Enqueue returns once the task is accepted, not once it ran.
type Queue interface {
	Enqueue(ctx context.Context, t Task) error
}

// TaskTokens are the credentials a task carries to its worker class (§12.11): in production an OIDC
// token for the class's service account and audience, which Cloud Tasks mints; locally a signed
// token. A handler accepts only its own class's tokens, so the executor runs nothing another class
// sent (Proposal executor queue isolation).
type TaskTokens interface {
	Token(class string) (string, error)
	Verify(token, class string) error
}

// ErrUnauthorised is returned when a task's credential is missing, forged, expired or for another
// class.
var ErrUnauthorised = errors.New("the task's credential is not for this worker class")
