package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) AIRoutes() storage.AIRoutes { return aiRoutes{u} }

type aiRoutes struct{ u *unitOfWork }

func (r aiRoutes) Agents(ctx context.Context) ([]storage.AIAgent, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIAgents(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIAgent, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.AIAgent{ID: row.ID, Name: row.Name, Description: row.Description, Classes: row.Classes, Line: row.Line})
	}
	return out, nil
}

// withSteps reads the steps of the routes.
func (r aiRoutes) withSteps(ctx context.Context, rows []sqlcdb.FyniqAiRoute) ([]storage.AIRoute, error) {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	steps, err := r.u.q.AIRouteSteps(ctx, ids)
	if err != nil {
		return nil, mapError(err)
	}
	by := map[uuid.UUID][]storage.AIRouteStep{}
	for _, s := range steps {
		by[s.RouteID] = append(by[s.RouteID], storage.AIRouteStep{Position: int(s.Position), ConnectionID: s.ConnectionID, Model: s.Model,
			Effort: s.Effort, Cache: s.Cache})
	}
	out := make([]storage.AIRoute, 0, len(rows))
	for _, row := range rows {
		route := storage.AIRoute{ID: row.ID, Agent: row.Agent, Version: int(row.Version), Classes: row.Classes, Steps: by[row.ID],
			Approval: row.ApprovalID, MadeBy: row.MadeBy, MadeAt: row.MadeAt}
		if row.Preset != nil {
			route.Preset = *row.Preset
		}
		out = append(out, route)
	}
	return out, nil
}

func (r aiRoutes) Current(ctx context.Context) ([]storage.AIRoute, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.CurrentAIRoutes(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	return r.withSteps(ctx, rows)
}

func (r aiRoutes) Versions(ctx context.Context, agent string, limit int) ([]storage.AIRoute, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AIRouteVersions(ctx, sqlcdb.AIRouteVersionsParams{Agent: agent, PageSize: int32(min(max(limit, 1), 1000))}) // #nosec G115 -- 1 to 1,000
	if err != nil {
		return nil, mapError(err)
	}
	return r.withSteps(ctx, rows)
}

func (r aiRoutes) Save(ctx context.Context, route storage.AIRoute) (storage.AIRoute, error) {
	if err := r.u.check(); err != nil {
		return storage.AIRoute{}, err
	}
	row, err := r.u.q.InsertAIRoute(ctx, sqlcdb.InsertAIRouteParams{Agent: route.Agent, Version: int32(min(max(route.Version, 0), 1<<30)), // #nosec G115 -- capped
		Classes: route.Classes, Preset: optional(route.Preset), ApprovalID: route.Approval, MadeBy: route.MadeBy, MadeAt: route.MadeAt})
	if err != nil {
		return storage.AIRoute{}, mapError(err)
	}
	steps := make([]map[string]any, 0, len(route.Steps))
	for i, s := range route.Steps {
		if err := r.u.q.InsertAIRouteStep(ctx, sqlcdb.InsertAIRouteStepParams{RouteID: row.ID, Position: int32(i + 1), ConnectionID: s.ConnectionID, // #nosec G115 -- at most five
			Model: s.Model, Effort: s.Effort, Cache: s.Cache}); err != nil {
			return storage.AIRoute{}, mapError(err)
		}
		steps = append(steps, map[string]any{"connection_id": s.ConnectionID.String(), "model": s.Model, "effort": s.Effort, "cache": s.Cache})
	}
	out, err := r.withSteps(ctx, []sqlcdb.FyniqAiRoute{row})
	if err != nil {
		return storage.AIRoute{}, err
	}
	change := map[string]any{"agent": route.Agent, "classes": route.Classes, "preset": route.Preset, "steps": steps}
	if route.Approval != nil {
		change["approval"] = route.Approval.String()
	}
	version := out[0].Version
	return out[0], r.u.event(ctx, "ai_route.changed", "ai_route", row.ID, &version, map[string][2]any{"route": {nil, change}})
}
