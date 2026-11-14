package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) AIGrants() storage.AIGrants { return aiGrants{u} }

type aiGrants struct{ u *unitOfWork }

func (r aiGrants) Issue(ctx context.Context, g storage.AIGrant) (storage.AIGrant, error) {
	if err := r.u.check(); err != nil {
		return storage.AIGrant{}, err
	}
	row, err := r.u.q.InsertAIGrant(ctx, sqlcdb.InsertAIGrantParams{TokenHash: g.TokenHash, UserID: g.UserID, TurnID: g.Turn, IssuedAt: g.IssuedAt,
		ExpiresAt: g.ExpiresAt})
	if err != nil {
		return storage.AIGrant{}, mapError(err)
	}
	return storage.AIGrant{ID: row.ID, TokenHash: row.TokenHash, UserID: row.UserID, Turn: row.TurnID, IssuedAt: row.IssuedAt, ExpiresAt: row.ExpiresAt,
		EndedAt: row.EndedAt}, nil
}

func (r aiGrants) End(ctx context.Context, turn uuid.UUID, now time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	n, err := r.u.q.EndAIGrant(ctx, sqlcdb.EndAIGrantParams{EndedAt: &now, TurnID: turn})
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return nil
}

func (r aiGrants) Classes(ctx context.Context, turn uuid.UUID, now time.Time) ([]string, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	classes, err := r.u.q.AIGrantClasses(ctx, sqlcdb.AIGrantClassesParams{TurnID: turn, Now: now})
	if err != nil {
		return nil, mapError(err)
	}
	return classes, nil
}

func (r aiGrants) Widen(ctx context.Context, turn uuid.UUID, classes []string, now time.Time) error {
	if err := r.u.check(); err != nil {
		return err
	}
	if classes == nil {
		classes = []string{}
	}
	n, err := r.u.q.WidenAIGrant(ctx, sqlcdb.WidenAIGrantParams{TurnID: turn, Classes: classes, Now: now})
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return ports.ErrNotFound
	}
	return nil
}

// AIGrant implements storage.AIGrantLookup.
func (s *Store) AIGrant(ctx context.Context, org uuid.UUID, tokenHash []byte, now time.Time) (storage.AIGrantHolder, error) {
	row, err := s.q().AIGrantFor(ctx, sqlcdb.AIGrantForParams{Org: org, TokenHash: tokenHash, Now: now})
	if err != nil {
		return storage.AIGrantHolder{}, mapError(err)
	}
	if row.UserID == uuid.Nil {
		return storage.AIGrantHolder{}, ports.ErrNotFound
	}
	return storage.AIGrantHolder{UserID: row.UserID, Turn: row.TurnID, Roles: row.Roles, ExpiresAt: row.ExpiresAt}, nil
}

var _ storage.AIGrantLookup = (*Store)(nil)
