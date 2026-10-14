package ports

import "context"

// Task is one unit of background work. p0-outbox-background-work grows this port into the
// outbox's TaskQueue (ADR-T7).
type Task struct {
	Queue   string
	Name    string // unique per task: a second Enqueue with the same name is a no-op
	Payload []byte
}

// Queue dispatches tasks to a worker class. Production: Cloud Tasks. Local: in memory, then River.
type Queue interface {
	Enqueue(ctx context.Context, t Task) error
	// Next returns the next task on a queue, or ErrNotFound when it is empty.
	Next(ctx context.Context, queue string) (Task, error)
}
