//go:build local

package local

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/cognizhi/fyniq/internal/ports"
)

// Push delivers tasks the way Cloud Tasks does (PRD rev 0.23): an HTTP POST to the worker class's
// /tasks endpoint, with that class's token, the task's name and retry count in headers, a deadline
// per attempt, and retries with backoff on any answer other than 2xx. A task name is accepted once.
// Undelivered work is not lost when this process stops: its outbox row stays dispatched and the
// tick sends it again.
type Push struct {
	targets  map[string]string // class → base URL
	tokens   ports.TaskTokens
	client   *http.Client
	attempts int
	backoff  time.Duration

	mu   sync.Mutex
	seen map[string]bool
	wg   sync.WaitGroup
}

var _ ports.Queue = (*Push)(nil)

// NewPush delivers to targets (worker class → base URL such as http://localhost:8084), retrying a
// delivery up to attempts times with backoff doubling from base.
func NewPush(targets map[string]string, tokens ports.TaskTokens, attempts int, base time.Duration) *Push {
	return &Push{targets: targets, tokens: tokens, client: &http.Client{}, attempts: max(attempts, 1), backoff: base, seen: map[string]bool{}}
}

// Enqueue implements ports.Queue: it accepts the task and delivers it in the background.
func (p *Push) Enqueue(_ context.Context, t ports.Task) error {
	url, ok := p.targets[t.Queue]
	if !ok {
		return fmt.Errorf("no worker class %q to deliver task %s to", t.Queue, t.Name)
	}
	token, err := p.tokens.Token(t.Queue)
	if err != nil {
		return err
	}
	p.mu.Lock()
	if p.seen[t.Name] {
		p.mu.Unlock()
		return nil // Cloud Tasks refuses a name it has seen; the outbox needs only that it is not run twice
	}
	p.seen[t.Name] = true
	p.mu.Unlock()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.deliver(url+"/tasks", token, t)
	}()
	return nil
}

func (p *Push) deliver(url, token string, t ports.Task) {
	deadline := cmp.Or(t.Deadline, 10*time.Minute)
	wait := p.backoff
	for attempt := range p.attempts {
		if attempt > 0 {
			time.Sleep(wait)
			wait *= 2
		}
		ctx, cancel := context.WithTimeout(context.Background(), deadline)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(t.Payload))
		if err != nil {
			cancel()
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-CloudTasks-TaskName", t.Name)
		req.Header.Set("X-CloudTasks-QueueName", t.Queue)
		req.Header.Set("X-CloudTasks-TaskRetryCount", strconv.Itoa(attempt))
		resp, err := p.client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
		}
		cancel()
		if err == nil && resp.StatusCode/100 == 2 {
			return
		}
	}
}

// Wait blocks until every accepted task has finished its deliveries (tests, and shutdown).
func (p *Push) Wait() { p.wg.Wait() }
