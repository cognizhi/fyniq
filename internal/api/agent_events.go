package api

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The scheduled agents' event triggers (PRD-001 AI-62; p1-scheduled-agents task 4.3, design D4).
const (
	EventPeriodClosed   = "period_closed"   // a period's close attested
	EventSharesChanged  = "shares_changed"  // a share issue, transfer or conversion effective
	EventFilingRecorded = "filing_recorded" // a statutory calendar's filing recorded
)

// agentEvent queues, in the unit of work of the change, the run each agent that is running owes the event: one whose
// events include kind, acting for the Owner who turned it on, once per event's key. A change that rolls back queues
// nothing; the deadline windows are the daily monitor's, whose inputs include the day.
func (a *API) agentEvent(ctx context.Context, u storage.UnitOfWork, org uuid.UUID, kind, key string) error {
	if a.deps.Outbox == nil {
		return nil
	}
	now := a.deps.Clock.Now()
	s := agents.Scheduler{Store: a.deps.Store, Dispatcher: *a.deps.Outbox, Clock: a.deps.Clock, Skills: agents.Default()}
	for _, skill := range agents.Default().All() {
		st, err := agents.Enabled(ctx, u, skill, now)
		if err != nil {
			return err
		}
		if !st.Running() || !slices.Contains(strings.Split(st.Value("events"), ","), kind) {
			continue
		}
		if _, err := s.Queue(ctx, u, org, skill, st.ActsFor, "event:"+kind+":"+key, now.Truncate(time.Minute), nil); err != nil {
			return err
		}
	}
	return nil
}
