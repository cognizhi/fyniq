//go:build local

package local

import (
	"context"
	"fmt"
	"sync"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Memory is an in-process queue for tests and `make dev`. River replaces it as the local
// adapter in p0-outbox-background-work (ADR-T7).
type Memory struct {
	mu     sync.Mutex
	queues map[string][]ports.Task
	seen   map[string]bool
}

// NewMemory returns an empty queue.
func NewMemory() *Memory {
	return &Memory{queues: map[string][]ports.Task{}, seen: map[string]bool{}}
}

// Enqueue adds t unless a task with the same name was already enqueued.
func (m *Memory) Enqueue(_ context.Context, t ports.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
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
