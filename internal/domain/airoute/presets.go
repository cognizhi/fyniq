package airoute

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/domain/aipolicy"
)

// Preset is one of ADR-T2's options as a route preset (AI-26, rev 0.12 and 0.13): it names a provider, a region and a
// model line for each agent, never a model ID. Applying it pins the newest allowed, callable model of each line from the
// connection's discovered list that has passed the agent's evals there (AI-78).
type Preset struct {
	ID          string
	Name        string
	Description string
	// Targets are where its steps go, in order; none for C, which takes A's or B's by the agent's classes.
	Targets []Target
	// Sensitive is whether it may carry personal data and payroll: in-Singapore processing only (AI-75's default).
	Sensitive bool
}

// Target is a provider and region a preset routes to, and the prefix of the model IDs of each line it pins.
type Target struct {
	Connector string
	Auth      string
	Location  string
	Lines     map[string]string // LineMain and LineClassification, to the prefix of the model IDs of that line
}

// The targets of ADR-T2's options A and B (§12.5): Claude on Agent Platform with the service's own account, in
// Singapore or on its global endpoint. Option A's classification line is Sonnet and B's Haiku; the main line Opus.
var (
	singapore = Target{Connector: "anthropic", Auth: "workload-identity", Location: "asia-southeast1",
		Lines: map[string]string{LineMain: "claude-opus-", LineClassification: "claude-sonnet-"}}
	global = Target{Connector: "anthropic", Auth: "workload-identity", Location: "global",
		Lines: map[string]string{LineMain: "claude-opus-", LineClassification: "claude-haiku-"}}
)

// Presets are ADR-T2's options A, B and C (§12.5, AI-26): A stays the default at go-live, and for personal data and
// payroll afterwards.
var Presets = []Preset{
	{ID: "A", Name: "Singapore only", Description: "Claude on Agent Platform in Singapore (asia-southeast1) for every agent: the default at go-live, " +
		"and always for personal data and payroll.", Targets: []Target{singapore}, Sensitive: true},
	{ID: "B", Name: "Global", Description: "Claude on Agent Platform's global endpoint: newer models, processed outside Singapore once the PDPA " +
		"transfer assessment is recorded; never personal data or payroll.", Targets: []Target{global}},
	{ID: "C", Name: "By data class", Description: "Singapore only for an agent that handles personal data or payroll; the global endpoint for the others."},
}

// PresetOf is the preset named id.
func PresetOf(id string) (Preset, bool) {
	i := slices.IndexFunc(Presets, func(p Preset) bool { return p.ID == id })
	if i < 0 {
		return Preset{}, false
	}
	return Presets[i], true
}

// Candidate is a connection a preset may route to: what it connects to, where, and its allowed, callable models.
type Candidate struct {
	Connection uuid.UUID
	Name       string
	Connector  string
	Auth       string
	Location   string
	Models     []CandidateModel
}

// CandidateModel is an allowed, callable model of a candidate, as its provider listed it, and the agents whose evals it
// passed there and has not gone out of date for (AI-78).
type CandidateModel struct {
	ID        string
	AliasOf   string
	Created   *time.Time
	Qualified []string
}

// Proposal is what a preset gives an agent: its route, or why it gives none, and what it leaves out.
type Proposal struct {
	Route Route
	Why   string
	Notes []string
}

// Apply is the route preset p gives agent a over the organisation's connections (AI-26, AI-74): the agent's classes,
// without personal data and payroll unless the preset keeps them in Singapore; then, for each target in order and each
// connection on it, the newest model of the agent's line and then of the other line, at most MaxSteps steps.
func Apply(p Preset, a Agent, cs []Candidate) Proposal {
	sensitive := slices.ContainsFunc(a.Classes, func(c string) bool { return slices.Contains(aipolicy.Sensitive, c) })
	if p.ID == "C" {
		if sensitive {
			p = Presets[0]
		} else {
			p = Presets[1]
		}
	}
	var out Proposal
	for _, c := range a.Classes {
		if !p.Sensitive && slices.Contains(aipolicy.Sensitive, c) {
			out.Notes = append(out.Notes, c+" stays with preset A, in Singapore (AI-75)")
			continue
		}
		out.Route.Classes = append(out.Route.Classes, c)
	}
	if len(out.Route.Classes) == 0 {
		out.Why = a.Name + " handles only personal data or payroll, which preset " + p.ID + " never carries"
		return out
	}
	lines := []string{a.Line, LineMain}
	if a.Line == LineMain {
		lines[1] = LineClassification
	}
	sorted := slices.Clone(cs)
	slices.SortFunc(sorted, func(x, y Candidate) int {
		return cmp.Or(cmp.Compare(x.Name, y.Name), cmp.Compare(x.Connection.String(), y.Connection.String()))
	})
	for _, t := range p.Targets {
		for _, c := range sorted {
			if c.Connector != t.Connector || c.Auth != t.Auth || c.Location != t.Location {
				continue
			}
			for _, line := range lines {
				m, ok := newest(qualified(c.Models, a.ID), t.Lines[line])
				if !ok || len(out.Route.Steps) == MaxSteps ||
					slices.ContainsFunc(out.Route.Steps, func(s Step) bool { return s.Connection == c.Connection && s.Model == m }) {
					continue
				}
				out.Route.Steps = append(out.Route.Steps, Step{Connection: c.Connection, Model: m, Cache: true})
			}
		}
	}
	if len(out.Route.Steps) == 0 {
		where := make([]string, 0, len(p.Targets))
		for _, t := range p.Targets {
			where = append(where, t.Connector+" with "+t.Auth+" in "+t.Location)
		}
		out.Why = "no connection that is on (" + strings.Join(where, ", ") + ") has an allowed model of the lines preset " + p.ID + " pins that passed " + a.Name + "'s evals"
	}
	return out
}

// newest is the newest model whose ID starts with prefix: a dated model before a moving alias (AI-78), then the latest
// the provider created, then the greatest ID.
// qualified are the models qualified for agent.
func qualified(ms []CandidateModel, agent string) []CandidateModel {
	var out []CandidateModel
	for _, m := range ms {
		if slices.Contains(m.Qualified, agent) {
			out = append(out, m)
		}
	}
	return out
}

func newest(ms []CandidateModel, prefix string) (string, bool) {
	var best *CandidateModel
	better := func(m, than CandidateModel) bool {
		if (m.AliasOf == "") != (than.AliasOf == "") {
			return m.AliasOf == ""
		}
		switch {
		case m.Created != nil && than.Created == nil:
			return true
		case m.Created == nil && than.Created != nil:
			return false
		case m.Created != nil && !m.Created.Equal(*than.Created):
			return m.Created.After(*than.Created)
		}
		return m.ID > than.ID
	}
	for i := range ms {
		if prefix == "" || !strings.HasPrefix(ms[i].ID, prefix) {
			continue
		}
		if best == nil || better(ms[i], *best) {
			best = &ms[i]
		}
	}
	if best == nil {
		return "", false
	}
	return best.ID, true
}
