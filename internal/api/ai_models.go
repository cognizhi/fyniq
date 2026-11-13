package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/cognizhi/fyniq/internal/ai"
	"github.com/cognizhi/fyniq/internal/authn"
	"github.com/cognizhi/fyniq/internal/authz"
	"github.com/cognizhi/fyniq/internal/domain/aimodel"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the models each AI connection's provider lists (AI-80, AI-81, AI-84)
//
// Fyniq ships no list of models: each connection's list is read from its provider with the connection's credential,
// page by page to a cap, when its key is approved, daily at 06:00 SGT, and on Refresh. Only a complete read marks a
// model no longer listed; a failed read keeps the last good list. A connection is switched on only after a complete read.

// ModelReport is a capability, modality or method a provider's list reports of a model, as the provider names it.
type ModelReport string

// Schema implements huma.SchemaProvider.
func (ModelReport) Schema(huma.Registry) *huma.Schema {
	most := 200
	return &huma.Schema{Type: "string", MaxLength: &most, Description: "What the list reports, as the provider names it: a hint, never proof (AI-71).",
		Examples: []any{"image_input"}}
}

// AIModel is a model a connection's provider lists.
type AIModel struct {
	Model         string        `json:"model" doc:"The provider's ID for it." example:"claude-opus-4-6"`
	Name          string        `json:"name,omitempty" doc:"The provider's name for it." example:"Claude Opus 4.6"`
	AliasOf       string        `json:"alias_of,omitempty" doc:"The model an alias points at, when the list says (AI-78)." example:"anthropic/claude-sonnet-5-5"`
	State         string        `json:"state" enum:"new,available,deprecated,no_longer_listed,not_callable" doc:"new: first listed in the last 7 days; deprecated: a shutdown date ahead; no_longer_listed: not in the last complete read, or past its shutdown; not_callable: the provider refused it (AI-81)." example:"available"`
	Shutdown      *time.Time    `json:"shutdown,omitempty" doc:"The provider's retirement or shutdown date (AI-84)." example:"2027-03-31T00:00:00Z"`
	ContextWindow int           `json:"context_window,omitempty" doc:"The context window the list reports: a hint the probe still proves." example:"1000000"`
	MaxOutput     int           `json:"max_output,omitempty" doc:"The most output tokens the list reports." example:"128000"`
	Reports       []ModelReport `json:"reports" nullable:"false" doc:"What the list reports it supports, as the provider names it: hints, never proof (AI-71)."`
	FirstListed   time.Time     `json:"first_listed" example:"2026-11-14T06:00:00Z"`
	LastListed    time.Time     `json:"last_listed" example:"2026-11-14T06:00:00Z"`
}

// AIModelListRead is one read of a connection's model list (AI-84).
type AIModelListRead struct {
	Trigger string    `json:"trigger" enum:"credential,test,enable,daily,not_found,refresh" doc:"Why it was read." example:"daily"`
	Outcome string    `json:"outcome" enum:"complete,partial,failed" doc:"complete: every page to the provider's end; partial: the page cap or a later page stopped it; failed: the first page did." example:"complete"`
	Pages   int       `json:"pages" example:"1"`
	Added   int       `json:"added" doc:"Models it listed for the first time, or again." example:"2"`
	Removed int       `json:"removed" doc:"Models it no longer listed: a complete read only." example:"0"`
	Failure string    `json:"failure,omitempty" doc:"Why it was not complete." example:"the provider answered 401 (authentication_error): the provider refused the credential"`
	ReadBy  string    `json:"read_by" example:"system:ai.models.read"`
	ReadAt  time.Time `json:"read_at" example:"2026-11-14T06:00:00Z"`
}

// AIModelList is a connection's listed models and its latest reads.
type AIModelList struct {
	Models       []AIModel         `json:"models" nullable:"false" doc:"Every model the provider has listed, by ID; none is ever removed, so its history stays."`
	Reads        []AIModelListRead `json:"reads" nullable:"false" doc:"The latest reads, newest first, at most 10."`
	LastComplete *time.Time        `json:"last_complete,omitempty" doc:"When the list was last read whole: switching the connection on needs one." example:"2026-11-14T06:00:00Z"`
}

type (
	aiModelListOutput struct{ Body AIModelList }
)

func (a *API) lister() ai.Lister {
	provider := a.deps.AIProviders
	if provider == nil && a.deps.Connectors.Runtime != nil {
		provider = a.deps.Connectors.Runtime.Model
	}
	if provider == nil {
		provider = func(context.Context, storage.UnitOfWork, storage.AIConnection) (ports.ModelProvider, error) {
			return nil, errors.New("no model provider is configured here")
		}
	}
	return ai.Lister{Store: a.deps.Store, Provider: provider, Clock: a.deps.Clock}
}

func (a *API) modelList(ctx context.Context, u storage.UnitOfWork, raw string) (AIModelList, error) {
	out := AIModelList{Models: []AIModel{}, Reads: []AIModelListRead{}}
	id, err := parseID("connection_id", raw)
	if err != nil {
		return out, err
	}
	if _, err := u.AIConnections().Get(ctx, id); err != nil {
		return out, err
	}
	models, err := u.AIModels().Of(ctx, id)
	if err != nil {
		return out, err
	}
	now := a.deps.Clock.Now()
	for _, m := range models {
		state := aimodel.State(aimodel.Model{ID: m.Model, Shutdown: m.Shutdown, FirstListed: m.FirstListed, Listed: m.Listed, NotCallableAt: m.NotCallableAt}, now)
		reports := []ModelReport{}
		for _, r := range m.Reports {
			reports = append(reports, ModelReport(r))
		}
		out.Models = append(out.Models, AIModel{Model: m.Model, Name: m.Name, AliasOf: m.AliasOf, State: state, Shutdown: m.Shutdown,
			ContextWindow: m.ContextWindow, MaxOutput: m.MaxOutput, Reports: reports, FirstListed: m.FirstListed.UTC(), LastListed: m.LastListed.UTC()})
	}
	reads, err := u.AIModels().Reads(ctx, id, 10)
	if err != nil {
		return out, err
	}
	for _, r := range reads {
		out.Reads = append(out.Reads, AIModelListRead{Trigger: r.Trigger, Outcome: r.Outcome, Pages: r.Pages, Added: r.Added, Removed: r.Removed,
			Failure: r.Failure, ReadBy: r.ReadBy, ReadAt: r.ReadAt.UTC()})
	}
	last, err := u.AIModels().LastComplete(ctx, id)
	switch {
	case err == nil:
		at := last.ReadAt.UTC()
		out.LastComplete = &at
	case !errors.Is(err, ports.ErrNotFound):
		return out, err
	}
	return out, nil
}

// listedWhole is whether a connection's list was read whole: switching it on needs that (AI-80).
func listedWhole(ctx context.Context, u storage.UnitOfWork, c storage.AIConnection) error {
	_, err := u.AIModels().LastComplete(ctx, c.ID)
	if errors.Is(err, ports.ErrNotFound) {
		return problem(aimodel.CodeListUnavailable, "The connection's model list has not been read whole yet (AI_MODEL_LIST_UNAVAILABLE).",
			"Refresh its models from the provider; a credential that cannot list models cannot be switched on")
	}
	return err
}

func registerAIModels(a *API) {
	locks := []string{storage.CodeRetry, storage.CodeLockTimeout}
	register(a, authz.GetAIConnectionModels, huma.Operation{
		OperationID: "getAIConnectionModels", Method: http.MethodGet, Path: "/v1/ai/connections/{connection_id}/models", Tags: []string{"AI"},
		Summary: "List the models an AI connection's provider lists",
		Description: "Every model the connection's provider has listed (AI-80), with its state (AI-81), what the list reports of it as hints, " +
			"and the latest reads of the list with their outcomes (AI-84). Read-only; no side effects.",
	}, append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *AIConnectionPath) (*aiModelListOutput, error) {
		var out AIModelList
		p, _ := authn.From(ctx)
		err := a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
			out, err = a.modelList(ctx, u, in.ConnectionID)
			return err
		})
		return &aiModelListOutput{Body: out}, err
	})

	register(a, authz.RefreshAIConnectionModels, exempt(exempt(huma.Operation{
		OperationID: "refreshAIConnectionModels", Method: http.MethodPost, Path: "/v1/ai/connections/{connection_id}/models/refresh", Tags: []string{"AI"},
		Summary: "Read an AI connection's model list from its provider now",
		Description: "Refresh from provider (AI-80): reads the connection's list now with its credential, page by page to the cap, sending no " +
			"company data, and answers the list as getAIConnectionModels does. A complete read marks the models it does not list no longer " +
			"listed; a partial or failed one keeps the last complete list (AI-84). Side effects: the read, kept with its outcome, and its audit " +
			"event ai_model.listed. On the never-list for the AI and MCP (§9.6).",
	}, "Idempotency-Key", "A refresh reads the provider's list again and keeps that read; repeating it reads again, which harms nothing."),
		"Prefer", "A refresh reads the provider: a preview would be the read itself."),
		append([]string{CodeNotFound}, locks...), func(ctx context.Context, in *AIConnectionPath) (*aiModelListOutput, error) {
			p, _ := authn.From(ctx)
			id, err := parseID("connection_id", in.ConnectionID)
			if err != nil {
				return nil, err
			}
			if _, _, err := a.lister().Read(ctx, p.Organisation, id, aimodel.ByRefresh, uploader(ctx)); err != nil {
				return nil, err
			}
			var out AIModelList
			err = a.deps.Store.Do(ctx, p.Organisation, func(ctx context.Context, u storage.UnitOfWork) (err error) {
				out, err = a.modelList(ctx, u, in.ConnectionID)
				return err
			})
			return &aiModelListOutput{Body: out}, err
		})
}
