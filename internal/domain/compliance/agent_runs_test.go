package compliance

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// The nightly check of CTL-AI-09 (AI-61, AI-66; p1-scheduled-agents design D11): a run that ended is an exception when its
// model step called a read tool its skill does not name, or it proposed a task type its skill does not output or under
// another agent's name; a run of a skill this release does not pin is counted and not checked.
// req: CTL-AI-09
// asvs: V8.2.1, V16.3.3
func TestEveryAgentRunStaysInsideItsSkill(t *testing.T) {
	skills := agents.Default()
	skill, ok := skills.Get("company-secretary")
	if !ok {
		t.Fatal("the release has no company secretary skill")
	}
	at := time.Date(2026, 11, 16, 23, 1, 0, 0, time.UTC)
	run := func(tools ...string) storage.AgentRun {
		return storage.AgentRun{ID: uuid.New(), Agent: skill.Name, SkillVersion: skill.Version, SkillSHA256: skill.SHA256, Status: storage.AgentRunDone,
			FinishedAt: &at, Report: &storage.AgentReport{ModelUsed: true, Tools: tools}}
	}
	right := run("checks.list", "packs.get")
	wrongTool := run("checks.list", "records.delete")
	wrongOutput, wrongAgent, older := run(), run(), run("anything")
	older.SkillSHA256 = "00" + skill.SHA256[2:]
	proposals := map[uuid.UUID][]storage.AIProposal{
		right.ID:       {{TaskType: "sec.create_task", Agent: skill.Agent()}, {TaskType: "sec.draft_reminder", Agent: skill.Agent()}},
		wrongOutput.ID: {{TaskType: "fin.post_journal", Agent: skill.Agent()}},
		wrongAgent.ID:  {{TaskType: "sec.create_task", Agent: "chat"}},
	}
	found, other := agentRunFindings(skills, []storage.AgentRun{right, wrongTool, wrongOutput, wrongAgent, older}, proposals)
	want := map[string]string{
		wrongTool.ID.String():   "Its model step called records.delete, which its skill does not name",
		wrongOutput.ID.String(): "It proposed fin.post_journal, which its skill does not output",
		wrongAgent.ID.String():  "Its proposal of sec.create_task names the agent chat",
	}
	if len(found) != len(want) || other != 1 {
		t.Fatalf("found %+v, %d not checked", found, other)
	}
	for _, f := range found {
		if want[f.ID] != f.Detail || f.Kind != "agent_run" {
			t.Errorf("finding %+v, want %q", f, want[f.ID])
		}
	}
}
