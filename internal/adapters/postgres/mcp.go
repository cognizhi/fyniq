package postgres

import (
	"context"

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
