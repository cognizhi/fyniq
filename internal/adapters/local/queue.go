//go:build local

package local

import (
	"context"
	"fmt"
	"sync"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Memory is an in-process queue that holds tasks instead of delivering them: the test double of
// the Queue port. Local mode delivers with Push.
type Memory struct {
	mu      sync.Mutex
	classes map[string]bool
	queues  map[string][]ports.Task
	seen    map[string]bool
}

// NewMemory returns an empty queue for the given worker classes (worker, executor and jobs by
// default).
func NewMemory(classes ...string) *Memory {
	if len(classes) == 0 {
		classes = []string{"worker", "executor", "jobs"}
	}
	m := &Memory{classes: map[string]bool{}, queues: map[string][]ports.Task{}, seen: map[string]bool{}}
	for _, c := range classes {
		m.classes[c] = true
	}
	return m
}

// Enqueue adds t unless a task with the same name was already enqueued.
func (m *Memory) Enqueue(_ context.Context, t ports.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.classes[t.Queue] {
		return fmt.Errorf("no worker class %q", t.Queue)
	}
	if m.seen[t.Name] {
		return nil
	}
	m.seen[t.Name] = true
	m.queues[t.Queue] = append(m.queues[t.Queue], t)
	return nil
}

// Next removes and returns the oldest task on queue.
func (m *Memory) Next(_ context.Context, queue string) (ports.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.queues[queue]
	if len(q) == 0 {
		return ports.Task{}, fmt.Errorf("queue %s: %w", queue, ports.ErrNotFound)
	}
	m.queues[queue] = q[1:]
	return q[0], nil
}

var _ ports.Queue = (*Memory)(nil)
