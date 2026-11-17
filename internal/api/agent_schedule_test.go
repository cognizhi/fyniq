//go:build local

package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// enableSecretary turns the company secretary's assistant on, as the first founder asks and the second approves (AI-63).
func enableSecretary(e *env, f founders) {
	e.t.Helper()
	const path = "/v1/organisation/settings/agents.company_secretary.enabled"
	ahkow := f.as(f.ahkow)
	asked := e.call(http.MethodPatch, path, ahkow, map[string]any{"value": "true"}, "If-Match", e.call(http.MethodGet, path, ahkow, nil).Header.Get("ETag"))
	if asked.Status != http.StatusAccepted {
		e.t.Fatalf("asking to turn the assistant on: %d %s", asked.Status, asked.Raw)
	}
	if d := f.decide(f.as(f.weiling), asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		e.t.Fatalf("approving it: %d %s", d.Status, d.Raw)
	}
}

// nextAt moves e's clock to the next h:m in Singapore time, at least a minute ahead.
func nextAt(e *env, weekday time.Weekday, h, m int) {
	now := e.clock.Now().In(clock.Singapore)
	t := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, clock.Singapore)
	for !t.After(now.Add(time.Minute)) || (weekday >= 0 && t.Weekday() != weekday) {
		t = t.AddDate(0, 0, 1)
	}
	e.clock.Set(t)
}

// scheduled are the tasks e's queue holds for the worker with a time to run at, by it.
func scheduled(e *env) []time.Time {
	var out []time.Time
	for {
		task, err := e.queue.Next(context.Background(), string(storage.ClassWorker))
		if errors.Is(err, ports.ErrNotFound) {
			return out
		}
		if !task.At.IsZero() {
			out = append(out, task.At)
		}
	}
}

// A scheduled agent's slots are queued from the database, each once and delivered at its time (AI-62; design D4): only
// once the agent is turned on, by the Owner with a second approver, and not while it is paused; each run acts for the
// Owner who asked to turn it on (AI-63). Turning it on is the Owner's on its own row; its schedule is C there.
// req: AI-62
// req: AI-63
// req: ai/scheduled-agents Recommended default schedule
// asvs: V8.2.1
func TestAnAgentsSlotsAreQueuedOnceAtTheirTime(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	ctx := context.Background()
	s := agents.Scheduler{Store: workerStore(t), Dispatcher: outbox.Dispatcher{Store: workerStore(t), Queue: e.queue, Clock: e.clock}, Clock: e.clock,
		Skills: agents.Default()}
	runs := func() []storage.AgentRun {
		var out []storage.AgentRun
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AgentRuns().List(ctx, storage.AgentRunQuery{Agent: "company-secretary"})
			return err
		})
		return out
	}

	// Off, nothing is queued; only the Owner turns it on, and only with a second approver.
	nextAt(e, -1, 6, 55)
	if n, err := s.ScheduleIn(ctx, e.books.Org); err != nil || n != 0 {
		t.Fatalf("off: %d %v", n, err)
	}
	const enabled = "/v1/organisation/settings/agents.company_secretary.enabled"
	e.problemIs(e.call(http.MethodPatch, enabled, e.as(authz.Administrator), map[string]any{"value": "true"}, "If-Match", `"0"`),
		"updateSetting", 403, CodeForbidden)
	enableSecretary(e, f)

	// On, the day's 07:00 monitor is queued once, at 07:00, acting for the founder who asked.
	nextAt(e, -1, 6, 55)
	if n, err := s.ScheduleIn(ctx, e.books.Org); err != nil || n != 1 {
		t.Fatalf("the monitor's slot: %d %v", n, err)
	}
	if n, err := s.ScheduleIn(ctx, e.books.Org); err != nil || n != 0 {
		t.Fatalf("the same slot again: %d %v", n, err)
	}
	queued := runs()
	slot := queued[0].SlotAt.In(clock.Singapore)
	if len(queued) != 1 || queued[0].Trigger != "schedule:monitor" || slot.Hour() != 7 || slot.Minute() != 0 || queued[0].ActsFor.String() != f.ahkow ||
		queued[0].Status != storage.AgentRunQueued {
		t.Fatalf("the run queued: %+v", queued)
	}
	if at := scheduled(e); !slices.ContainsFunc(at, func(t time.Time) bool { return t.Equal(queued[0].SlotAt) }) {
		t.Errorf("the run's task is not delivered at its slot: %v", at)
	}

	// On a Monday, the day's monitor and the weekly cross-check at 08:00.
	nextAt(e, time.Monday, 7, 57)
	if n, err := s.ScheduleIn(ctx, e.books.Org); err != nil || n != 2 {
		t.Fatalf("Monday's slots: %d %v", n, err)
	}

	// Paused, nothing more is queued; pausing takes effect at once, with no second approver.
	const paused = "/v1/organisation/settings/agents.company_secretary.paused"
	ahkow := f.as(f.ahkow)
	if r := e.call(http.MethodPatch, paused, ahkow, map[string]any{"value": "true"}, "If-Match", e.call(http.MethodGet, paused, ahkow, nil).Header.Get("ETag")); r.Status != http.StatusOK {
		t.Fatalf("pausing: %d %s", r.Status, r.Raw)
	}
	nextAt(e, -1, 6, 55)
	if n, err := s.ScheduleIn(ctx, e.books.Org); err != nil || n != 0 {
		t.Fatalf("paused: %d %v", n, err)
	}
	if got := len(runs()); got != 3 {
		t.Errorf("runs queued in all: %d", got)
	}
}
