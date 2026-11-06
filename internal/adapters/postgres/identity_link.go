package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// Linked sign-in identities (FR-ADM-08, rev 0.196; migration 000239): global rows, reached through the functions that
// check the session, and each link and removal written to the installation's trail in the same transaction.

// installationUnit runs fn in a unit of work of the installation organisation, whose trail records sign-in changes.
func (s *Store) installationUnit(ctx context.Context, fn func(ctx context.Context, u *unitOfWork) error) error {
	org, err := s.InstallationOrganisation(ctx)
	if err != nil {
		return err
	}
	return s.Do(ctx, org, func(ctx context.Context, uow storage.UnitOfWork) error {
		return fn(ctx, uow.(*unitOfWork))
	})
}

// LinkIdentity implements storage.Sessions.
func (s *Store) LinkIdentity(ctx context.Context, l storage.IdentityLink) (uuid.UUID, error) {
	var who uuid.UUID
	err := s.installationUnit(ctx, func(ctx context.Context, u *unitOfWork) error {
		var err error
		if who, err = u.q.LinkIdentity(ctx, sqlcdb.LinkIdentityParams{SessionHash: l.SessionHash, Issuer: l.Issuer, Subject: l.Subject,
			Provider: l.Provider, Now: l.Now}); err != nil {
			return mapError(err)
		}
		return u.event(as(ctx, who), "identity.linked", "user", who, nil, map[string][2]any{
			"provider": {nil, l.Provider}, "issuer": {nil, l.Issuer}})
	})
	return who, err
}

// UnlinkIdentity implements storage.Sessions.
func (s *Store) UnlinkIdentity(ctx context.Context, sessionHash []byte, issuer, subject string, now time.Time) (uuid.UUID, error) {
	var who uuid.UUID
	err := s.installationUnit(ctx, func(ctx context.Context, u *unitOfWork) error {
		rows, err := u.q.UnlinkIdentity(ctx, sqlcdb.UnlinkIdentityParams{SessionHash: sessionHash, Issuer: issuer, Subject: subject, Now: now})
		if err != nil {
			return mapError(err)
		}
		if len(rows) == 0 {
			return ports.ErrNotFound
		}
		who = rows[0].UserID
		return u.event(as(ctx, who), "identity.unlinked", "user", who, nil, map[string][2]any{
			"provider": {rows[0].Provider, nil}, "issuer": {issuer, nil}})
	})
	return who, err
}

// Identities implements storage.Sessions.
func (s *Store) Identities(ctx context.Context, userID uuid.UUID) ([]storage.Identity, error) {
	rows, err := s.q().UserIdentities(ctx, userID)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Identity, 0, len(rows))
	for _, r := range rows {
		out = append(out, toIdentity(r))
	}
	return out, nil
}

func toIdentity(r sqlcdb.UserIdentitiesRow) storage.Identity {
	i := storage.Identity{Issuer: r.Issuer, Subject: r.Subject, Provider: r.Provider, CreatedAt: r.CreatedAt}
	if r.LinkedUnix != 0 {
		i.LinkedAt = time.Unix(r.LinkedUnix, 0).UTC()
	}
	if r.LastUsedUnix != 0 {
		i.LastUsedAt = time.Unix(r.LastUsedUnix, 0).UTC()
	}
	return i
}
