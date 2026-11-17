//go:build local

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// An event queues the agent's run, in the unit of work of the change (AI-62; design D4): a filing recorded, once the
// agent is on and while its events include it, acting for the Owner who turned it on, once per event; with the event
// taken off its list, it queues nothing.
// req: AI-62
func TestAnEventQueuesTheAgentsRun(t *testing.T) {
	e := newEnv(t)
	f := foundersIn(e)
	ahkow := f.as(f.ahkow)
	record := func(key string) string {
		t.Helper()
		d := e.call(http.MethodPost, "/v1/compliance/filings", ahkow, map[string]any{"key": key, "outcome": "not_required", "reason": "Nothing to file"})
		if r := e.call(http.MethodPost, "/v1/compliance/filings/"+d.str("id")+"/record", ahkow, nil, "If-Match", d.Header.Get("ETag")); r.Status != http.StatusOK {
			t.Fatalf("recording %s: %d %s", key, r.Status, r.Raw)
		}
		return d.str("id")
	}
	runs := func() []storage.AgentRun {
		var out []storage.AgentRun
		e.do(func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = u.AgentRuns().List(ctx, storage.AgentRunQuery{Agent: "company-secretary"})
			return err
		})
		return out
	}

	record("ir8a_ais:2026-12-31")
	if got := runs(); len(got) != 0 {
		t.Fatalf("a filing recorded with the agent off queued %d runs", len(got))
	}
	enableSecretary(e, f)
	id := record("form_cs:2026-12-31")
	got := runs()
	if len(got) != 1 || got[0].Trigger != "event:filing_recorded:"+id || got[0].ActsFor.String() != f.ahkow || got[0].Status != storage.AgentRunQueued {
		t.Fatalf("the event's run: %+v", got)
	}

	// Taking the event off the agent's list waits for a second approver; then a filing recorded queues nothing.
	const events = "/v1/organisation/settings/agents.company_secretary.events"
	asked := e.call(http.MethodPatch, events, ahkow, map[string]any{"value": "period_closed,shares_changed"}, "If-Match",
		e.call(http.MethodGet, events, ahkow, nil).Header.Get("ETag"))
	if asked.Status != http.StatusAccepted {
		t.Fatalf("asking to change the events: %d %s", asked.Status, asked.Raw)
	}
	if d := f.decide(f.as(f.weiling), asked.str("id"), "approve", hashOf(asked)); d.Status != http.StatusOK {
		t.Fatalf("approving the events: %d %s", d.Status, d.Raw)
	}
	record("eci:2026-12-31")
	if got := runs(); len(got) != 1 {
		t.Errorf("a filing recorded off the agent's events queued a run: %+v", got)
	}
}
