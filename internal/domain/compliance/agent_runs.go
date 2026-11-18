package compliance

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// agentRuns (CTL-AI-09, AI-61, AI-66; p1-scheduled-agents design D11): every scheduled agent's run that ended in the window
// stayed inside its pinned skill. Each read tool its model step called is one of its skill's, and each proposal it made
// is of a task type its skill outputs, by its own agent. Its tasks are its findings' by construction (the database ties a
// run's task to the run, raised by the worker). A run of a skill this release does not pin, such as one finished before
// an upgrade, is counted and not checked (target 0).
func agentRuns(ctx context.Context, u storage.UnitOfWork, since time.Time) ([]Finding, string, error) {
	runs, err := u.AgentRuns().List(ctx, storage.AgentRunQuery{FinishedSince: &since, Limit: 500})
	if err != nil {
		return nil, "", err
	}
	proposals := map[uuid.UUID][]storage.AIProposal{}
	for _, r := range runs {
		id := r.ID
		if proposals[id], err = u.AIProposals().List(ctx, storage.AIProposalQuery{Run: &id, Limit: 100}); err != nil {
			return nil, "", err
		}
	}
	found, other := agentRunFindings(agents.Default(), runs, proposals)
	read := fmt.Sprintf("%d run(s) of the scheduled agents that ended", len(runs)-other)
	if other > 0 {
		read += fmt.Sprintf(", and %d of a skill this release does not pin, not checked", other)
	}
	return found, read, nil
}

// agentRunFindings are the runs that went beyond their skill, with how many runs were of a skill skills does not pin.
func agentRunFindings(skills *agents.Skills, runs []storage.AgentRun, proposals map[uuid.UUID][]storage.AIProposal) ([]Finding, int) {
	var out []Finding
	other := 0
	for _, r := range runs {
		skill, ok := skills.Get(r.Agent)
		if !ok || skill.Version != r.SkillVersion || skill.SHA256 != r.SkillSHA256 {
			other++
			continue
		}
		at := ""
		if r.FinishedAt != nil {
			at = day(*r.FinishedAt)
		}
		offered := skill.Offered(aitool.Default())
		if r.Report != nil {
			for _, tool := range r.Report.Tools {
				if !slices.Contains(offered, tool) {
					out = append(out, Finding{Kind: "agent_run", ID: r.ID.String(), At: at, Detail: "Its model step called " + tool + ", which its skill does not name"})
				}
			}
		}
		for _, p := range proposals[r.ID] {
			switch {
			case !slices.Contains(skill.Outputs, p.TaskType):
				out = append(out, Finding{Kind: "agent_run", ID: r.ID.String(), At: at, Detail: "It proposed " + p.TaskType + ", which its skill does not output"})
			case p.Agent != skill.Agent():
				out = append(out, Finding{Kind: "agent_run", ID: r.ID.String(), At: at, Detail: "Its proposal of " + p.TaskType + " names the agent " + p.Agent})
			}
		}
	}
	return out, other
}
