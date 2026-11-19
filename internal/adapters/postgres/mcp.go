package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) MCPOptIns() storage.MCPOptIns { return mcpOptIns{u} }

type mcpOptIns struct{ u *unitOfWork }

func (r mcpOptIns) Set(ctx context.Context, o storage.MCPOptIn) (storage.MCPOptIn, error) {
	if err := r.u.check(); err != nil {
		return storage.MCPOptIn{}, err
	}
	row, err := r.u.q.InsertMCPUserOptIn(ctx, sqlcdb.InsertMCPUserOptInParams{UserID: o.UserID, OptedIn: o.OptedIn, SetAt: r.u.store.clock.Now()})
	if err != nil {
		return storage.MCPOptIn{}, mapError(err)
	}
	out := storage.MCPOptIn{UserID: row.UserID, OptedIn: row.OptedIn, SetAt: row.SetAt}
	if err := r.u.event(ctx, "mcp_user_optin.set", "mcp_user_optin", row.ID, nil, map[string][2]any{"opted_in": {nil, out.OptedIn}}); err != nil {
		return storage.MCPOptIn{}, err
	}
	return out, nil
}

func (r mcpOptIns) Latest(ctx context.Context, user uuid.UUID) (storage.MCPOptIn, bool, error) {
	if err := r.u.check(); err != nil {
		return storage.MCPOptIn{}, false, err
	}
	rows, err := r.u.q.LatestMCPUserOptIn(ctx, user)
	if err != nil {
		return storage.MCPOptIn{}, false, mapError(err)
	}
	if len(rows) == 0 {
		return storage.MCPOptIn{}, false, nil
	}
	return storage.MCPOptIn{UserID: rows[0].UserID, OptedIn: rows[0].OptedIn, SetAt: rows[0].SetAt}, true, nil
}

func (u *unitOfWork) MCPCalls() storage.MCPCalls { return mcpCalls{u} }

type mcpCalls struct{ u *unitOfWork }

func (r mcpCalls) Record(ctx context.Context, c storage.MCPCall) (storage.MCPCall, error) {
	if err := r.u.check(); err != nil {
		return storage.MCPCall{}, err
	}
	id, err := r.u.q.InsertMCPCall(ctx, sqlcdb.InsertMCPCallParams{UserID: c.UserID, App: c.App, AppVersion: c.AppVersion, TokenID: c.TokenID, Tool: c.Tool,
		InputSha256: c.InputSHA256, OutputSha256: c.OutputSHA, Facts: int32(c.Facts), DurationMs: int32(c.DurationMS), Outcome: c.Outcome, Code: c.Code, // #nosec G115 -- bounded counts
		Network: c.Network, At: c.At})
	if err != nil {
		return storage.MCPCall{}, mapError(err)
	}
	c.ID = id
	return c, nil
}

func (r mcpCalls) Count(ctx context.Context, user uuid.UUID, app string, since, until time.Time) (int, time.Time, error) {
	if err := r.u.check(); err != nil {
		return 0, time.Time{}, err
	}
	row, err := r.u.q.CountMCPCalls(ctx, sqlcdb.CountMCPCallsParams{UserID: user, App: app, Since: since, Until: until})
	if err != nil {
		return 0, time.Time{}, mapError(err)
	}
	if row.Calls == 0 {
		return 0, time.Time{}, nil
	}
	return int(row.Calls), row.Oldest, nil
}

func (r mcpCalls) List(ctx context.Context, user uuid.UUID, limit int) ([]storage.MCPCall, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListMCPCalls(ctx, sqlcdb.ListMCPCallsParams{UserID: user, RowLimit: int32(min(max(limit, 1), 500))}) // #nosec G115 -- capped
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MCPCall, len(rows))
	for i, x := range rows {
		out[i] = storage.MCPCall{ID: x.ID, UserID: x.UserID, App: x.App, AppVersion: x.AppVersion, TokenID: x.TokenID, Tool: x.Tool, InputSHA256: x.InputSha256,
			OutputSHA: x.OutputSha256, Facts: int(x.Facts), DurationMS: int(x.DurationMs), Outcome: x.Outcome, Code: x.Code, Network: x.Network, At: x.At}
	}
	return out, nil
}

func (r mcpCalls) FirstSeen(ctx context.Context, user uuid.UUID, kind, value string, at time.Time) (bool, error) {
	if err := r.u.check(); err != nil {
		return false, err
	}
	first, err := r.u.q.MarkMCPSeen(ctx, sqlcdb.MarkMCPSeenParams{UserID: user, Kind: kind, Value: value, FirstSeen: at})
	return first, mapError(err)
}
