package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aimodel"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/domain/airoute"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the route presets (AI-26)
//
// ADR-T2's options A (Singapore only), B (global) and C (by data class) as presets over the organisation's connections:
// each names a provider, a region and a model line, and gives each agent the route that pins the newest allowed model of
// its lines. The route is set as any other, with PUT /v1/ai/routes/{agent}.

// PresetNote is what a preset leaves out of an agent's route, and why.
type PresetNote string

// Schema implements huma.SchemaProvider.
func (PresetNote) Schema(huma.Registry) *huma.Schema {
	most := 300
	return &huma.Schema{Type: "string", MaxLength: &most, Description: "What the preset leaves out of the route, and why.",
		Examples: []any{"personal-data stays with preset A, in Singapore (AI-75)"}}
}

// AIPresetTarget is a provider and region a preset routes to, and the model lines it pins.
type AIPresetTarget struct {
	Connector          string `json:"connector" example:"anthropic"`
	Auth               string `json:"auth" example:"workload-identity"`
	Location           string `json:"location" example:"asia-southeast1"`
	MainLine           string `json:"main_line" doc:"The prefix of the model IDs it pins for most agents." example:"claude-opus-"`
	ClassificationLine string `json:"classification_line" doc:"The prefix it pins for high-volume classification." example:"claude-sonnet-"`
}

// AIPresetAgent is the route a preset gives an agent, ready for PUT /v1/ai/routes/{agent}, or why it gives none.
type AIPresetAgent struct {
	Agent string       `json:"agent" example:"chat"`
	Route *AIRouteSet  `json:"route,omitempty" doc:"The route, as setAIRoute takes it."`
	Why   string       `json:"why,omitempty" doc:"Why the preset gives the agent no route it could set now." example:"no connection that is on has an allowed model of the lines preset A pins"`
	Notes []PresetNote `json:"notes" nullable:"false"`
}

// AIPreset is one of ADR-T2's options as a preset.
type AIPreset struct {
	ID          string           `json:"id" enum:"A,B,C" example:"A"`
	Name        string           `json:"name" example:"Singapore only"`
	Description string           `json:"description" example:"Claude on Agent Platform in Singapore (asia-southeast1) for every agent."`
	Targets     []AIPresetTarget `json:"targets" nullable:"false" doc:"Where its steps go, in order; none for C, which takes A's or B's by the agent's classes."`
	Agents      []AIPresetAgent  `json:"agents" nullable:"false"`
}

// AIPresetList is every preset and what it gives each agent.
type AIPresetList struct {
	Presets []AIPreset `json:"presets" nullable:"false"`
}

type aiPresetListOutput struct{ Body AIPresetList }

// candidates are the organisation's connections that are on, as a preset sees them: each with its location and the
// models that could serve now (allowed, callable, and priced where it bills by usage).
func candidates(s routeScene) []airoute.Candidate {
	var out []airoute.Candidate
	for _, c := range s.connections {
		if c.State != "enabled" {
			continue
		}
		var config struct {
			Location string `json:"location"`
		}
		_ = json.Unmarshal(c.Configuration, &config)
		if config.Location == "" {
			config.Location = "asia-southeast1" // the Agent Platform connectors' default
		}
		cand := airoute.Candidate{Connection: c.ID, Name: c.Name, Connector: c.Connector, Auth: c.Auth, Location: config.Location}
		for _, m := range s.models[c.ID] {
			callable := m.Allowed && aimodel.Callable(aimodel.Model{ID: m.Model, Shutdown: m.Shutdown, Listed: m.Listed, NotCallableAt: m.NotCallableAt}, s.now)
			if callable && aiprice.Serves(c.Billing, c.SelfHosted, s.priced[c.ID][m.Model], m.Model) == nil {
				cand.Models = append(cand.Models, airoute.CandidateModel{ID: m.Model, AliasOf: m.AliasOf, Created: m.Created})
			}
		}
		out = append(out, cand)
	}
	return out
}

func registerAIPresets(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetAIPresets, huma.Operation{
		OperationID: "getAIPresets", Method: http.MethodGet, Path: "/v1/ai/presets", Tags: []string{"AI"},
		Summary: "Get the route presets",
		Description: "ADR-T2's options as route presets (AI-26): A Singapore only, the default at go-live and always for personal data and " +
			"payroll; B the global endpoint; C by data class. Each names a provider, a region and a model line, never a model ID, and gives " +
			"each agent the route that pins the newest allowed model of its lines from the connections' discovered lists, with what it leaves " +
			"out, or why it gives none. A route is set from it with setAIRoute, as any other. Read-only; no side effects.",
	}, locks, func(ctx context.Context, _ *struct{}) (*aiPresetListOutput, error) {
		p, _ := authn.From(ctx)
		out := AIPresetList{Presets: []AIPreset{}}
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) error {
			agents, err := u.AIRoutes().Agents(ctx)
			if err != nil {
				return err
			}
			scene, err := a.routeScene(ctx, u)
			if err != nil {
				return err
			}
			cands := candidates(scene)
			for _, preset := range airoute.Presets {
				item := AIPreset{ID: preset.ID, Name: preset.Name, Description: preset.Description, Targets: []AIPresetTarget{}, Agents: []AIPresetAgent{}}
				for _, t := range preset.Targets {
					item.Targets = append(item.Targets, AIPresetTarget{Connector: t.Connector, Auth: t.Auth, Location: t.Location,
						MainLine: t.Lines[airoute.LineMain], ClassificationLine: t.Lines[airoute.LineClassification]})
				}
				for _, ag := range agents {
					proposal := airoute.Apply(preset, airoute.Agent{ID: ag.ID, Name: ag.Name, Classes: ag.Classes, Line: ag.Line}, cands)
					pa := AIPresetAgent{Agent: ag.ID, Why: proposal.Why, Notes: []PresetNote{}}
					for _, n := range proposal.Notes {
						pa.Notes = append(pa.Notes, PresetNote(n))
					}
					if proposal.Why == "" {
						set := AIRouteSet{Preset: preset.ID, Classes: []RouteClass{}, Steps: []AIRouteStepInput{}}
						for _, c := range proposal.Route.Classes {
							set.Classes = append(set.Classes, RouteClass(c))
						}
						for _, s := range proposal.Route.Steps {
							set.Steps = append(set.Steps, AIRouteStepInput{ConnectionID: s.Connection.String(), Model: s.Model, Effort: s.Effort, Cache: s.Cache})
						}
						for i, s := range proposal.Route.Steps {
							if err := scene.serves(i, s, proposal.Route.Classes); err != nil {
								pa.Why = "the preset's route could not be set now: " + problemMessage(err)
							}
						}
						pa.Route = &set
					}
					item.Agents = append(item.Agents, pa)
				}
				out.Presets = append(out.Presets, item)
			}
			return nil
		})
		return &aiPresetListOutput{Body: out}, err
	})
}
