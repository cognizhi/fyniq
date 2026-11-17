package checks

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The scheduled agents' runs (PRD-001 C.2 #36, AI-67; p1-scheduled-agents task 4.2, design D4): the tick queues each
// slot, and this compares the slots an agent that is on should have run with the runs it has, so a run that failed, was
// skipped or is late, or a slot that never ran, is ⚠ on Checks.

// viewAgents is the Appendix D row whose view opens the agents' runs.
const viewAgents = "AI company secretary: enable, schedule, runs and drafts (rev 0.6)"

// agentLate is how long past its slot a run may start before it is late, and a slot with no run is missed.
const agentLate = 30 * time.Minute

// agentWindow is how far back the watchdog compares slots and runs: the last day, with an hour more.
const agentWindow = 25 * time.Hour

var agentChecks = []Definition{
	{Key: "agents.runs", Ref: "C.2 #36", Area: "AI", Worst: Warning, Title: "Every scheduled agent ran on time",
		Explain: "An agent that is on runs at each slot of its schedule. This flags a run of the last day that failed or was skipped, " +
			"one more than 30 minutes past its slot and not started, one that ran without its AI step, and a slot no run came for. " +
			"The statutory calendar's own reminders keep coming whatever the agent does.",
		Rules: "AI-67, AI-64", Owner: "owner", View: viewAgents, Run: agentRuns},
}

// C.2 #36: the runs of the last day that failed, were skipped or are late, and the slots that never ran (AI-67).
func agentRuns(ctx context.Context, u storage.UnitOfWork, e *Env) (Result, error) {
	var items []Item
	for _, skill := range agents.Default().All() {
		st, err := agents.Enabled(ctx, u, skill, e.Now)
		if err != nil {
			return Result{}, err
		}
		if !st.On {
			continue
		}
		from := e.Now.Add(-agentWindow)
		if st.Since.After(from) {
			from = st.Since
		}
		runs, err := u.AgentRuns().List(ctx, storage.AgentRunQuery{Agent: skill.Name, Limit: 500})
		if err != nil {
			return Result{}, err
		}
		seen := map[string]bool{}
		for _, r := range runs {
			if r.SlotAt.Before(from) {
				continue
			}
			seen[r.Trigger+"@"+r.SlotAt.UTC().Format(time.RFC3339)] = true
			at := r.SlotAt.In(clock.Singapore).Format("2 January 15:04")
			var why string
			switch {
			case r.Status == storage.AgentRunFailed:
				why = "failed: " + r.Reason
			case r.Status == storage.AgentRunSkipped:
				why = "skipped: " + r.Reason
			case r.Status == storage.AgentRunQueued && e.Now.Sub(r.SlotAt) > agentLate:
				why = "late: not started " + strconv.Itoa(int(e.Now.Sub(r.SlotAt).Minutes())) + " minutes after its slot"
			case r.Status == storage.AgentRunDone && r.Report != nil && r.Report.ModelMissed != "":
				why = "ran without its AI step (" + r.Report.ModelMissed + "): its findings and tasks stand, with no AI explanation"
			}
			if why != "" {
				items = append(items, Item{Kind: "agent_run", ID: r.ID.String(), Label: runLabel(r.Trigger),
					Detail: "The " + at + " run (Singapore time) " + why, Version: strconv.Itoa(r.Version)})
			}
		}
		if st.Paused {
			continue // a paused agent queues nothing: no slot is missed
		}
		for _, entry := range agents.Entries {
			spec, err := st.Entry(entry)
			if err != nil {
				return Result{}, err
			}
			for slot := spec.Next(from.Add(-time.Minute)); !slot.IsZero() && e.Now.Sub(slot) > agentLate; slot = spec.Next(slot) {
				if !seen["schedule:"+entry+"@"+slot.UTC().Format(time.RFC3339)] {
					items = append(items, Item{Kind: "agent_slot", ID: skill.Name + ":" + entry + ":" + slot.UTC().Format(time.RFC3339), Label: runLabel("schedule:" + entry),
						Detail: "No run came for the " + slot.In(clock.Singapore).Format("2 January 15:04") + " slot (Singapore time)"})
				}
			}
		}
	}
	if len(items) == 0 {
		return Fine("Every scheduled agent's run of the last day ran on time"), nil
	}
	return Found(Warning, items, fmt.Sprintf("%d of the scheduled agents' runs need a look", len(items)),
		"Open the run to see why, and choose Run now once it is fixed; the statutory calendar's reminders keep coming meanwhile"), nil
}

// runLabel names a run by its trigger, as people read it.
func runLabel(trigger string) string {
	switch trigger {
	case "schedule:monitor":
		return "Deadline and lodgement monitor"
	case "schedule:cross_check":
		return "Full cross-check"
	case "run_now":
		return "Run now"
	}
	return "Event run"
}
