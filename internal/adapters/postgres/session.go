package postgres

import (
	"context"
	"github.com/google/uuid"
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
		Provider: n.Provider, Now: n.Now, Expires: n.Expires, StepUpAt: stepUp, UserAgent: n.UserAgent}))
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
		Organisation: row.OrganisationID, Roles: row.Roles, ExpiresAt: row.ExpiresAt, StepUpMethod: row.StepUpMethod}
	if row.StepUpUnix != 0 {
		out.StepUpAt = time.Unix(row.StepUpUnix, 0).UTC()
	}
	return out, nil
}

// EndSession implements storage.Sessions.
func (s *Store) EndSession(ctx context.Context, tokenHash []byte, now time.Time) error {
	return mapError(s.q().EndSession(ctx, sqlcdb.EndSessionParams{TokenHash: tokenHash, Now: now}))
}

// UserSessions implements storage.Sessions.
func (s *Store) UserSessions(ctx context.Context, userID uuid.UUID, now time.Time, idle time.Duration) ([]storage.Device, error) {
	rows, err := s.q().UserSessions(ctx, sqlcdb.UserSessionsParams{UserID: userID, Now: now, IdleSeconds: int64(idle / time.Second)})
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Device, len(rows))
	for i, r := range rows {
		out[i] = storage.Device{TokenHash: r.TokenHash, Provider: r.Provider, SignedInAt: r.CreatedAt, LastSeenAt: r.LastSeenAt,
			ExpiresAt: r.ExpiresAt, StepUpMethod: r.StepUpMethod, UserAgent: r.UserAgent}
	}
	return out, nil
}

// EndUserSession implements storage.Sessions.
func (s *Store) EndUserSession(ctx context.Context, userID uuid.UUID, tokenHash []byte, now time.Time) error {
	ended, err := s.q().EndUserSession(ctx, sqlcdb.EndUserSessionParams{UserID: userID, TokenHash: tokenHash, Now: now})
	if err != nil {
		return mapError(err)
	}
	if !ended {
		return ports.ErrNotFound
	}
	return nil
}
