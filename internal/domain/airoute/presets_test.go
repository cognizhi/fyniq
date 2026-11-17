package airoute

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The assistant goes live on preset A (AI-05, ADR-T2 option A; p1-ai-assistant task 9.1): its chat agent, which reads
// personal data and, when the organisation allows it, payroll, is routed to Claude on Agent Platform in Singapore with
// the service's own account, on the newest model of its line that passed its evals there; never to the global
// endpoint, even when that has a newer model. Preset B leaves personal data and payroll with A; C picks A for it.
// req: AI-05
// req: AI-26
// req: AI-75
func TestTheAssistantGoesLiveOnPresetA(t *testing.T) {
	assistant := Agent{ID: "chat", Name: "Assistant", Classes: []string{"know-how", "business-figures", "personal-data", "payroll"}, Line: LineMain}
	day := func(d int) *time.Time { at := time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC); return &at }
	sg, glob := uuid.New(), uuid.New()
	cands := []Candidate{
		{Connection: sg, Name: "Claude in Singapore", Connector: "anthropic", Auth: "workload-identity", Location: "asia-southeast1", Models: []CandidateModel{
			{ID: "claude-opus-4-6", Created: day(1), Qualified: []string{"chat"}},
			{ID: "claude-opus-4-7", Created: day(10), Qualified: []string{"chat"}},
			{ID: "claude-opus-4-8", Created: day(12)}, // not qualified for chat: never pinned
		}},
		{Connection: glob, Name: "Claude, global", Connector: "anthropic", Auth: "workload-identity", Location: "global", Models: []CandidateModel{
			{ID: "claude-opus-5-0", Created: day(14), Qualified: []string{"chat"}},
		}},
	}
	a, _ := PresetOf("A")
	p := Apply(a, assistant, cands)
	if p.Why != "" || !slices.Equal(p.Route.Classes, assistant.Classes) || len(p.Route.Steps) == 0 {
		t.Fatalf("preset A for the assistant: %+v", p)
	}
	for _, s := range p.Route.Steps {
		if s.Connection != sg {
			t.Errorf("a step outside Singapore: %+v", s)
		}
	}
	if p.Route.Steps[0].Model != "claude-opus-4-7" {
		t.Errorf("the first step pins %s, not the newest model that passed the assistant's evals in Singapore", p.Route.Steps[0].Model)
	}
	b, _ := PresetOf("B")
	if p := Apply(b, assistant, cands); slices.Contains(p.Route.Classes, "personal-data") || slices.Contains(p.Route.Classes, "payroll") || len(p.Notes) < 2 {
		t.Errorf("preset B carries the assistant's personal data or payroll: %+v", p)
	}
	c, _ := PresetOf("C")
	if p := Apply(c, assistant, cands); len(p.Route.Steps) == 0 || p.Route.Steps[0].Connection != sg {
		t.Errorf("preset C for the assistant: %+v", p)
	}
}
