//go:build local

package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/outbox"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The tick is the agents' watchdog (AI-67, C.2 #36; design D4): with the agent on, a run more than 30 minutes past its
// slot and not started, a run that failed, and a slot no run came for are each ⚠ on Checks, while a slot not yet due is
// not.
// req: AI-67
// req: C.2 #36
func TestTheTickWatchesTheAgentsRuns(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	ctx := context.Background()
	s := agents.Scheduler{Store: workerStore(t), Dispatcher: outbox.Dispatcher{Store: workerStore(t), Queue: e.queue, Clock: e.clock}, Clock: e.clock,
		Skills: agents.Default()}
	ahkow := f.as(f.ahkow)
	watch := func() map[string]any {
		t.Helper()
		e.runCatalogue()
		c := checkNamed(e.call(http.MethodGet, "/v1/checks", ahkow, nil), "agents.runs")
		if c == nil {
			t.Fatal("no check agents.runs")
		}
		return c
	}
	details := func(c map[string]any) string {
		var out []string
		items, _ := c["items"].([]any)
		for _, it := range items {
			d, _ := it.(map[string]any)["detail"].(string)
			out = append(out, d)
		}
		return strings.Join(out, " | ")
	}

	enableSecretary(e, f)
	nextAt(e, -1, 6, 55)
	if n, err := s.ScheduleIn(ctx, e.books.Org); err != nil || n != 1 {
		t.Fatalf("the monitor's slot: %d %v", n, err)
	}
	if c := watch(); c["status"] != "ok" || c["ref"] != "C.2 #36" {
		t.Fatalf("a slot not yet due: %v", c)
	}

	// 45 minutes past its slot and not started: late.
	e.clock.Advance(50 * time.Minute)
	if c := watch(); c["status"] != "warning" || !strings.Contains(details(c), "late: not started 45 minutes after its slot") {
		t.Fatalf("a late run: %v", c)
	}

	// Failed as it ran.
	if err := workerStore(t).Do(ctx, e.books.Org, func(ctx context.Context, u storage.UnitOfWork) error {
		list, err := u.AgentRuns().List(ctx, storage.AgentRunQuery{Agent: "company-secretary"})
		if err != nil {
			return err
		}
		run, err := u.AgentRuns().Start(ctx, list[0].ID, list[0].Version, e.clock.Now())
		if err != nil {
			return err
		}
		_, err = u.AgentRuns().Fail(ctx, run.ID, run.Version, "the model step did not finish", e.clock.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if c := watch(); c["status"] != "warning" || !strings.Contains(details(c), "failed: the model step did not finish") {
		t.Fatalf("a failed run: %v", c)
	}

	// A day later, with no tick to queue it, the next 07:00 never ran.
	nextAt(e, -1, 7, 45)
	if c := watch(); c["status"] != "warning" || !strings.Contains(details(c), "No run came for the") {
		t.Fatalf("a missed slot: %v", details(c))
	}
}
