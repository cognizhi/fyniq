package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/domain/aiprice"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) AICalls() storage.AICalls { return aiCalls{u} }

type aiCalls struct{ u *unitOfWork }

func (r aiCalls) Record(ctx context.Context, c storage.AICall) (storage.AICall, error) {
	if err := r.u.check(); err != nil {
		return storage.AICall{}, err
	}
	var fellBack *int32
	if c.FellBackFrom > 0 {
		v := int32(min(c.FellBackFrom, 5)) // #nosec G115 -- a route has at most five steps
		fellBack = &v
	}
	detail := c.Detail
	if len(detail) > 500 {
		detail = detail[:497] + "…"
	}
	id, err := r.u.q.RecordAICall(ctx, sqlcdb.RecordAICallParams{Run: c.Run, Agent: c.Agent, RouteVersion: int32(min(max(c.RouteVersion, 0), 1<<30)), // #nosec G115 -- capped
		Step: int32(min(max(c.Step, 0), 5)), FellBackFrom: fellBack, Connection: c.ConnectionID, ModelRequested: c.ModelRequested, // #nosec G115 -- capped
		ModelReported: optional(c.ModelReported), VersionReported: optional(c.VersionReported), Actor: c.Actor, Classes: c.Classes,
		Outcome: c.Outcome, Detail: optional(detail), Input: c.Input, Output: c.Output, CacheRead: c.CacheRead, CacheWrite: c.CacheWrite,
		Reasoning: c.Reasoning, Cost: optional(c.Cost), Currency: optional(c.Currency), Price: c.Price, CostBase: optional(c.CostBase),
		LatencyMs: int32(min(max(c.LatencyMS, 0), 1<<30)), StartedAt: c.StartedAt}) // #nosec G115 -- capped
	if err != nil {
		return storage.AICall{}, mapError(err)
	}
	c.ID, c.Detail = id, detail
	return c, nil
}

func (r aiCalls) OfRun(ctx context.Context, run uuid.UUID) ([]storage.AICall, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AICallsOfRun(ctx, run)
	if err != nil {
		return nil, mapError(err)
	}
	return toAICalls(rows), nil
}

func (r aiCalls) Page(ctx context.Context, q storage.AICallQuery) ([]storage.AICall, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	var before *uuid.UUID
	if q.BeforeAt != nil {
		before = &q.BeforeID
	}
	rows, err := r.u.q.AICallsPage(ctx, sqlcdb.AICallsPageParams{Actor: optional(q.Actor), Agent: optional(q.Agent), Run: q.Run, FromAt: q.From,
		ToAt: q.To, BeforeAt: q.BeforeAt, BeforeID: before, PageSize: int32(min(max(q.Limit, 1), 1000))}) // #nosec G115 -- capped
	if err != nil {
		return nil, mapError(err)
	}
	all := make([]sqlcdb.AICallsOfRunRow, 0, len(rows))
	for _, row := range rows {
		all = append(all, sqlcdb.AICallsOfRunRow(row))
	}
	return toAICalls(all), nil
}

// toAICalls are the attempts as rows of ai_call give them.
func toAICalls(rows []sqlcdb.AICallsOfRunRow) []storage.AICall {
	out := make([]storage.AICall, 0, len(rows))
	for _, row := range rows {
		c := storage.AICall{ID: row.ID, Run: row.RunID, Agent: row.Agent, RouteVersion: int(row.RouteVersion), Step: int(row.Step),
			ConnectionID: row.ConnectionID, ModelRequested: row.ModelRequested, Actor: row.Actor, Classes: row.Classes, Outcome: row.Outcome,
			Input: row.InputTokens, Output: row.OutputTokens, CacheRead: row.CacheReadTokens, CacheWrite: row.CacheWriteTokens,
			Reasoning: row.ReasoningTokens, Price: row.PriceID, LatencyMS: int(row.LatencyMs), StartedAt: row.StartedAt, RecordedAt: row.RecordedAt}
		if row.FellBackFrom != nil {
			c.FellBackFrom = int(*row.FellBackFrom)
		}
		for _, s := range []struct {
			to   *string
			from *string
		}{{&c.ModelReported, row.ModelReported}, {&c.VersionReported, row.VersionReported}, {&c.Detail, row.Detail}, {&c.Currency, row.Currency}} {
			if s.from != nil {
				*s.to = *s.from
			}
		}
		if row.Cost != "" {
			c.Cost = aiprice.Trim(row.Cost)
		}
		if row.CostBase != "" {
			c.CostBase = aiprice.Trim(row.CostBase)
		}
		out = append(out, c)
	}
	return out
}

func (r aiCalls) Usage(ctx context.Context, from, to time.Time) ([]storage.AIUsageRow, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.AICallUsage(ctx, sqlcdb.AICallUsageParams{FromAt: from, ToAt: to})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.AIUsageRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.AIUsageRow{ConnectionID: row.ConnectionID, Model: row.ModelRequested, Agent: row.Agent, Actor: row.Actor,
			Requests: row.Requests, Served: row.Served, Fallbacks: row.Fallbacks, Errors: row.Errors, Skipped: row.Skipped, Input: row.InputTokens,
			Output: row.OutputTokens, CacheRead: row.CacheReadTokens, CacheWrite: row.CacheWriteTokens, Reasoning: row.ReasoningTokens,
			CostBase: aiprice.Trim(row.CostBase), Unpriced: row.Unpriced})
	}
	return out, nil
}
