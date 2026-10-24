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

// documentLinks keeps the online links to quotes and invoices (FR-AR-06): only a token's hash, never the token; a link
// changes only in its first view and its revocation, each once (FY197).
type documentLinks struct{ u *unitOfWork }

func (u *unitOfWork) DocumentLinks() storage.DocumentLinks { return documentLinks{u} }

func toDocumentLink(r sqlcdb.GetDocumentLinkRow) storage.DocumentLink {
	return storage.DocumentLink{ID: r.ID, DocumentID: r.DocumentID, DocumentVersion: int(r.DocumentVersion), MessageID: r.MessageID, ExpiresAt: r.ExpiresAt,
		CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, FirstViewedAt: r.FirstViewedAt, RevokedAt: r.RevokedAt, RevokedBy: str(r.RevokedBy)}
}

func (d documentLinks) Create(ctx context.Context, n storage.NewDocumentLink) (storage.DocumentLink, error) {
	if err := d.u.check(); err != nil {
		return storage.DocumentLink{}, err
	}
	row, err := d.u.q.InsertDocumentLink(ctx, sqlcdb.InsertDocumentLinkParams{DocumentID: n.DocumentID, DocumentVersion: int32(n.DocumentVersion), // #nosec G115 -- a version is small
		MessageID: n.MessageID, TokenHash: n.TokenHash, ExpiresAt: n.ExpiresAt, CreatedBy: d.u.actor(ctx)})
	if err != nil {
		return storage.DocumentLink{}, mapError(err)
	}
	out := toDocumentLink(sqlcdb.GetDocumentLinkRow(row))
	changes := map[string][2]any{"document_id": {nil, n.DocumentID.String()}, "document_version": {nil, n.DocumentVersion},
		"message_id": {nil, n.MessageID.String()}, "expires_at": {nil, out.ExpiresAt.UTC().Format(time.RFC3339)}} // never the token or its hash
	if err := d.u.event(ctx, "document_link.created", "document_link", out.ID, nil, changes); err != nil {
		return storage.DocumentLink{}, err
	}
	return out, nil
}

func (d documentLinks) Get(ctx context.Context, id uuid.UUID) (storage.DocumentLink, error) {
	if err := d.u.check(); err != nil {
		return storage.DocumentLink{}, err
	}
	row, err := d.u.q.GetDocumentLink(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.DocumentLink{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.DocumentLink{}, mapError(err)
	}
	return toDocumentLink(row), nil
}

func (d documentLinks) OfDocument(ctx context.Context, documentID uuid.UUID) ([]storage.DocumentLink, error) {
	if err := d.u.check(); err != nil {
		return nil, err
	}
	rows, err := d.u.q.DocumentLinksOf(ctx, documentID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.DocumentLink, len(rows))
	for i, r := range rows {
		out[i] = toDocumentLink(sqlcdb.GetDocumentLinkRow(r))
	}
	return out, nil
}

func (d documentLinks) Viewed(ctx context.Context, id uuid.UUID, at time.Time) (storage.DocumentLink, bool, error) {
	if err := d.u.check(); err != nil {
		return storage.DocumentLink{}, false, err
	}
	row, err := d.u.q.ViewDocumentLink(ctx, sqlcdb.ViewDocumentLinkParams{ID: id, At: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		l, err := d.Get(ctx, id)
		return l, false, err
	}
	if err != nil {
		return storage.DocumentLink{}, false, mapError(err)
	}
	out := toDocumentLink(sqlcdb.GetDocumentLinkRow(row))
	if err := d.u.event(ctx, "document_link.viewed", "document_link", id, nil,
		map[string][2]any{"first_viewed_at": {nil, at.UTC().Format(time.RFC3339)}}); err != nil {
		return storage.DocumentLink{}, false, err
	}
	return out, true, nil
}

func (d documentLinks) Revoke(ctx context.Context, id uuid.UUID) (storage.DocumentLink, error) {
	if err := d.u.check(); err != nil {
		return storage.DocumentLink{}, err
	}
	by := d.u.actor(ctx)
	row, err := d.u.q.RevokeDocumentLink(ctx, sqlcdb.RevokeDocumentLinkParams{ID: id, RevokedBy: &by})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := d.Get(ctx, id); err != nil {
			return storage.DocumentLink{}, err
		}
		return storage.DocumentLink{}, &storage.Error{Code: storage.CodeLink, Message: "the link was revoked already"}
	}
	if err != nil {
		return storage.DocumentLink{}, mapError(err)
	}
	out := toDocumentLink(sqlcdb.GetDocumentLinkRow(row))
	if err := d.u.event(ctx, "document_link.revoked", "document_link", id, nil,
		map[string][2]any{"revoked_at": {nil, out.RevokedAt.UTC().Format(time.RFC3339)}, "revoked_by": {nil, by}}); err != nil {
		return storage.DocumentLink{}, err
	}
	return out, nil
}

// LinkRoute implements storage.LinkRoutes through document_link_route, which reads the registry of hashes before any
// tenant is set.
func (s *Store) LinkRoute(ctx context.Context, tokenHash string) (uuid.UUID, uuid.UUID, error) {
	row, err := s.q().DocumentLinkRoute(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, uuid.Nil, ports.ErrNotFound
	}
	if err != nil {
		return uuid.Nil, uuid.Nil, mapError(err)
	}
	return row.OrganisationID, row.LinkID, nil
}
