package agents_test

import (
	"encoding/json"
	"flag"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cognizhi/fyniq/internal/agents"
	"github.com/cognizhi/fyniq/internal/aitool"
	"github.com/cognizhi/fyniq/internal/proposal"
)

var update = flag.Bool("update", false, "rewrite agents/agents.lock.json from the skills as they are, each changed one with a new version")

const lockPath = "../../agents/agents.lock.json"

const skill = `---
name: test-agent
description: A fixture of the tests.
version: 1
schema: agent-skill@1
tools: [checks.list, kb.read]
outputs: [run_report, sec.create_task]
permission_profile: compliance-read
---

# Test agent

Check, explain, draft.
`

// The release's skills load (AI-61): the company secretary's, with its tools within its profile and its outputs its
// report and catalogue task types. A run offers the model only the tools the release's registry has, and proposes only
// the task types the catalogue has: the secretary's document waits for its change. The knowledge base's tools arrived with p1-knowledge-base.
// req: AI-61
// req: CTL-AI-09
func TestTheReleasesSkillsLoad(t *testing.T) {
	s, ok := agents.Default().Get("company-secretary")
	if !ok || s.Version < 1 || s.Agent() != "company_secretary" || !strings.Contains(s.Instructions, "Rules that never bend") || len(s.SHA256) != 64 {
		t.Fatalf("the company secretary's skill: %+v", s)
	}
	if offered := s.Offered(aitool.Default()); !slices.Contains(offered, "kb.read") || !slices.Contains(offered, "kb.search") || !slices.Contains(offered, "checks.list") {
		t.Errorf("the tools a run offers: %v", offered)
	}
	if got := s.Proposes(proposal.Default()); slices.Contains(got, agents.RunReport) {
		t.Errorf("the task types a run may propose: %v", got)
	}
	if _, ok := agents.Default().Get("people-assistant"); ok {
		t.Error("Nemura's people assistant runs in Fyniq")
	}
}

// A skill that could act beyond its bounds is refused as it loads (AI-61, AI-66, CTL-AI-09): one naming a write as a
// tool, a tool beyond its permission profile, an output that is neither its report nor a task type, a task type on the
// never-list, no report, an unknown profile, another directory's name, another schema, no instructions, or a field the
// format does not have.
// req: AI-61
// req: AI-66
// req: CTL-AI-09
// asvs: V8.2.1
func TestASkillBeyondItsBoundsIsRefused(t *testing.T) {
	load := func(text string) error {
		_, err := agents.Load(fstest.MapFS{"test-agent/SKILL.md": {Data: []byte(text)}}, []string{"test-agent"})
		return err
	}
	if err := load(skill); err != nil {
		t.Fatalf("the fixture: %v", err)
	}
	for name, c := range map[string][2]string{
		"a write as a tool":         {"tools: [checks.list, kb.read]", "tools: [checks.list, records.update]"},
		"a tool beyond its profile": {"tools: [checks.list, kb.read]", "tools: [checks.list, people.get]"},
		"an output that is no type": {"outputs: [run_report, sec.create_task]", "outputs: [run_report, email]"},
		"a never-listed task type":  {"outputs: [run_report, sec.create_task]", "outputs: [run_report, sec.file_annual_return]"},
		"approving as an output":    {"outputs: [run_report, sec.create_task]", "outputs: [run_report, ap.approve_bill]"},
		"no report":                 {"outputs: [run_report, sec.create_task]", "outputs: [sec.create_task]"},
		"an unknown profile":        {"permission_profile: compliance-read", "permission_profile: everything"},
		"another directory's name":  {"name: test-agent", "name: other-agent"},
		"another schema":            {"schema: agent-skill@1", "schema: agent-skill@2"},
		"no version":                {"version: 1", "version: 0"},
		"no instructions":           {"# Test agent\n\nCheck, explain, draft.\n", ""},
		"a field the format lacks":  {"version: 1", "version: 1\nauto_approve: true"},
	} {
		if err := load(strings.Replace(skill, c[0], c[1], 1)); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	if _, err := agents.Load(fstest.MapFS{}, []string{"test-agent"}); err == nil {
		t.Error("a missing skill loaded")
	}
}

// Each skill the release ships is pinned in agents/agents.lock.json (AI-61, CMP-AI-03): a change made without a new
// version, or a version going back, fails.
// req: AI-61
// req: CTL-AI-09
func TestEveryChangeToASkillIsANewVersion(t *testing.T) {
	type pin struct {
		Version int    `json:"version"`
		SHA256  string `json:"sha256"`
	}
	pinned := map[string]pin{}
	if b, err := os.ReadFile(lockPath); err == nil {
		if err := json.Unmarshal(b, &pinned); err != nil {
			t.Fatal(err)
		}
	} else if !*update {
		t.Fatal(err)
	}
	now := map[string]pin{}
	for _, s := range agents.Default().All() {
		now[s.Name] = pin{s.Version, s.SHA256}
		was, ok := pinned[s.Name]
		switch {
		case ok && was.Version == s.Version && was.SHA256 != s.SHA256:
			t.Errorf("%s changed without a new version: raise its version from %d", s.Name, s.Version)
		case ok && s.Version < was.Version:
			t.Errorf("%s went back from version %d to %d", s.Name, was.Version, s.Version)
		}
	}
	if t.Failed() || maps.Equal(pinned, now) {
		return
	}
	if !*update {
		t.Fatalf("agents/agents.lock.json is not the skills as they are: run go test ./internal/agents -run %s -update", t.Name())
	}
	b, _ := json.MarshalIndent(now, "", "  ")
	if err := os.WriteFile(lockPath, append(b, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
