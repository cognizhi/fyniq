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

// portalLinks keeps the customer portal's links (FR-AR-14): only a token's hash, never the token; a link changes only in its first
// opening and its revocation, each once (FYP10).
type portalLinks struct{ u *unitOfWork }

func (u *unitOfWork) PortalLinks() storage.PortalLinks { return portalLinks{u} }

func toPortalLink(r sqlcdb.GetPortalLinkRow) storage.PortalLink {
	return storage.PortalLink{ID: r.ID, ContactID: r.ContactID, Label: str(r.Label), ExpiresAt: r.ExpiresAt, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
		FirstOpenedAt: r.FirstOpenedAt, RevokedAt: r.RevokedAt, RevokedBy: str(r.RevokedBy)}
}

func (p portalLinks) Create(ctx context.Context, n storage.NewPortalLink) (storage.PortalLink, error) {
	if err := p.u.check(); err != nil {
		return storage.PortalLink{}, err
	}
	row, err := p.u.q.InsertPortalLink(ctx, sqlcdb.InsertPortalLinkParams{ContactID: n.ContactID, Label: opt(n.Label), TokenHash: n.TokenHash,
		ExpiresAt: n.ExpiresAt, CreatedBy: p.u.actor(ctx), CreatedAt: n.At})
	if err != nil {
		return storage.PortalLink{}, mapError(err)
	}
	out := toPortalLink(sqlcdb.GetPortalLinkRow(row))
	changes := map[string][2]any{"contact_id": {nil, n.ContactID.String()}, "label": {nil, nilIfEmpty(n.Label)},
		"expires_at": {nil, out.ExpiresAt.UTC().Format(time.RFC3339)}} // never the token or its hash
	if err := p.u.event(ctx, "portal_link.created", "portal_link", out.ID, nil, changes); err != nil {
		return storage.PortalLink{}, err
	}
	return out, nil
}

func (p portalLinks) Get(ctx context.Context, id uuid.UUID) (storage.PortalLink, error) {
	if err := p.u.check(); err != nil {
		return storage.PortalLink{}, err
	}
	row, err := p.u.q.GetPortalLink(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.PortalLink{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.PortalLink{}, mapError(err)
	}
	return toPortalLink(row), nil
}

func (p portalLinks) OfContact(ctx context.Context, contact uuid.UUID) ([]storage.PortalLink, error) {
	if err := p.u.check(); err != nil {
		return nil, err
	}
	rows, err := p.u.q.PortalLinksOfContact(ctx, contact)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.PortalLink, len(rows))
	for i, r := range rows {
		out[i] = toPortalLink(sqlcdb.GetPortalLinkRow(r))
	}
	return out, nil
}

func (p portalLinks) Opened(ctx context.Context, id uuid.UUID, at time.Time) (storage.PortalLink, bool, error) {
	if err := p.u.check(); err != nil {
		return storage.PortalLink{}, false, err
	}
	row, err := p.u.q.OpenPortalLink(ctx, sqlcdb.OpenPortalLinkParams{ID: id, At: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		l, err := p.Get(ctx, id)
		return l, false, err
	}
	if err != nil {
		return storage.PortalLink{}, false, mapError(err)
	}
	out := toPortalLink(sqlcdb.GetPortalLinkRow(row))
	if err := p.u.event(ctx, "portal_link.opened", "portal_link", id, nil,
		map[string][2]any{"first_opened_at": {nil, at.UTC().Format(time.RFC3339)}}); err != nil {
		return storage.PortalLink{}, false, err
	}
	return out, true, nil
}

func (p portalLinks) Revoke(ctx context.Context, id uuid.UUID) (storage.PortalLink, error) {
	if err := p.u.check(); err != nil {
		return storage.PortalLink{}, err
	}
	by := p.u.actor(ctx)
	row, err := p.u.q.RevokePortalLink(ctx, sqlcdb.RevokePortalLinkParams{ID: id, RevokedBy: &by})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := p.Get(ctx, id); err != nil {
			return storage.PortalLink{}, err
		}
		return storage.PortalLink{}, &storage.Error{Code: storage.CodeLink, Message: "the link was revoked already"}
	}
	if err != nil {
		return storage.PortalLink{}, mapError(err)
	}
	out := toPortalLink(sqlcdb.GetPortalLinkRow(row))
	if err := p.u.event(ctx, "portal_link.revoked", "portal_link", id, nil,
		map[string][2]any{"revoked_at": {nil, out.RevokedAt.UTC().Format(time.RFC3339)}, "revoked_by": {nil, by}}); err != nil {
		return storage.PortalLink{}, err
	}
	return out, nil
}

// PortalRoute implements storage.PortalRoutes through portal_link_route, which reads the registry of hashes before any tenant
// is set.
func (s *Store) PortalRoute(ctx context.Context, tokenHash string) (uuid.UUID, uuid.UUID, error) {
	row, err := s.q().PortalLinkRoute(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, ports.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, mapError(err)
	}
	return row.OrganisationID, row.LinkID, nil
}
