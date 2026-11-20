package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ----------------------------------------------- third-party AI apps (AI-46; p2-api-third-parties task 5)

func (u *unitOfWork) MCPThirdPartyApps() storage.MCPThirdPartyApps { return thirdPartyApps{u} }

type thirdPartyApps struct{ u *unitOfWork }

func toThirdPartyApp(r sqlcdb.FyniqMcpThirdPartyApp) storage.MCPThirdPartyApp {
	return storage.MCPThirdPartyApp{ID: r.ID, ClientID: r.ClientID, Name: r.Name, Publisher: r.Publisher, PublisherURL: deref(r.PublisherUrl), RedirectURIs: r.RedirectUris, Purpose: r.Purpose,
		ProcessingLocation: r.ProcessingLocation, OutsideSingapore: r.OutsideSingapore, InventoryEntry: r.InventoryEntry, RegisteredBy: r.RegisteredBy, RegisteredAt: r.RegisteredAt,
		RemovedAt: r.RemovedAt, RemovedBy: r.RemovedBy}
}

func (r thirdPartyApps) Register(ctx context.Context, a storage.MCPThirdPartyApp) (storage.MCPThirdPartyApp, error) {
	if err := r.u.check(); err != nil {
		return storage.MCPThirdPartyApp{}, err
	}
	row, err := r.u.q.InsertMCPThirdPartyApp(ctx, sqlcdb.InsertMCPThirdPartyAppParams{ID: uuid.Must(uuid.NewV7()), ClientID: a.ClientID, Name: a.Name, Publisher: a.Publisher,
		PublisherUrl: optText(a.PublisherURL), RedirectUris: a.RedirectURIs, Purpose: a.Purpose, ProcessingLocation: a.ProcessingLocation, OutsideSingapore: a.OutsideSingapore,
		InventoryEntry: a.InventoryEntry, RegisteredBy: a.RegisteredBy, At: a.RegisteredAt})
	if err != nil {
		return storage.MCPThirdPartyApp{}, mapError(err)
	}
	out := toThirdPartyApp(row)
	return out, r.u.event(ctx, "mcp_third_party_app.registered", "mcp_third_party_app", out.ID, nil, map[string][2]any{"client_id": {nil, out.ClientID}, "name": {nil, out.Name},
		"publisher": {nil, out.Publisher}, "outside_singapore": {nil, out.OutsideSingapore}, "redirect_uris": {nil, out.RedirectURIs}})
}

func (r thirdPartyApps) List(ctx context.Context) ([]storage.MCPThirdPartyApp, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListMCPThirdPartyApps(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.MCPThirdPartyApp, len(rows))
	for i, row := range rows {
		out[i] = toThirdPartyApp(row)
	}
	return out, nil
}

func (r thirdPartyApps) Get(ctx context.Context, clientID string) (storage.MCPThirdPartyApp, error) {
	if err := r.u.check(); err != nil {
		return storage.MCPThirdPartyApp{}, err
	}
	row, err := r.u.q.GetMCPThirdPartyApp(ctx, clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.MCPThirdPartyApp{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.MCPThirdPartyApp{}, mapError(err)
	}
	return toThirdPartyApp(row), nil
}

func (r thirdPartyApps) Remove(ctx context.Context, clientID string, by uuid.UUID, at time.Time) (storage.MCPThirdPartyApp, error) {
	if err := r.u.check(); err != nil {
		return storage.MCPThirdPartyApp{}, err
	}
	row, err := r.u.q.RemoveMCPThirdPartyApp(ctx, sqlcdb.RemoveMCPThirdPartyAppParams{ClientID: clientID, At: &at, By: &by})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.MCPThirdPartyApp{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.MCPThirdPartyApp{}, mapError(err)
	}
	out := toThirdPartyApp(row)
	return out, r.u.event(ctx, "mcp_third_party_app.removed", "mcp_third_party_app", out.ID, nil, map[string][2]any{"client_id": {nil, out.ClientID}, "removed": {nil, true}})
}
