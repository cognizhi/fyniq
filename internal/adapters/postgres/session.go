package postgres

import (
	"context"
	"time"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

var _ storage.Sessions = (*Store)(nil)

// StartSession implements storage.Sessions.
func (s *Store) StartSession(ctx context.Context, n storage.NewSession) error {
	var stepUp *time.Time
	if !n.StepUpAt.IsZero() {
		stepUp = &n.StepUpAt
	}
	return mapError(s.q().StartSession(ctx, sqlcdb.StartSessionParams{TokenHash: n.TokenHash, Issuer: n.Issuer, Subject: n.Subject,
		Provider: n.Provider, Now: n.Now, Expires: n.Expires, StepUpAt: stepUp}))
}

// Session implements storage.Sessions.
func (s *Store) Session(ctx context.Context, tokenHash []byte, now time.Time, idle time.Duration) (storage.Session, error) {
	row, err := s.q().SessionFor(ctx, sqlcdb.SessionForParams{TokenHash: tokenHash, Now: now, IdleSeconds: int64(idle / time.Second)})
	if err != nil {
		return storage.Session{}, mapError(err)
	}
	if row.Issuer == "" {
		return storage.Session{}, ports.ErrNotFound
	}
	out := storage.Session{Issuer: row.Issuer, Subject: row.Subject, Provider: row.Provider, UserID: row.UserID,
		Organisation: row.OrganisationID, Roles: row.Roles, ExpiresAt: row.ExpiresAt}
	if row.StepUpUnix != 0 {
		out.StepUpAt = time.Unix(row.StepUpUnix, 0).UTC()
	}
	return out, nil
}

// EndSession implements storage.Sessions.
func (s *Store) EndSession(ctx context.Context, tokenHash []byte, now time.Time) error {
	return mapError(s.q().EndSession(ctx, sqlcdb.EndSessionParams{TokenHash: tokenHash, Now: now}))
}
