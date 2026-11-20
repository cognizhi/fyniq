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

// ------------------------------------------------------------------ an Owner's grant of the full cap table (FR-EQ-24)

func (u *unitOfWork) CapTableGrants() storage.CapTableGrants { return capTableGrants{u} }

type capTableGrants struct{ u *unitOfWork }

func toCapTableGrant(r sqlcdb.FyniqCapTableGrant) storage.CapTableGrant {
	g := storage.CapTableGrant{ID: r.ID, UserID: r.UserID, Reason: r.Reason, GrantedBy: r.GrantedBy, GrantedAt: r.GrantedAt, RevokedAt: r.RevokedAt}
	if r.RevokedBy != nil {
		g.RevokedBy = *r.RevokedBy
	}
	if r.RevokeReason != nil {
		g.RevokeReason = *r.RevokeReason
	}
	return g
}

func (r capTableGrants) List(ctx context.Context) ([]storage.CapTableGrant, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListCapTableGrants(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.CapTableGrant, 0, len(rows))
	for _, x := range rows {
		out = append(out, toCapTableGrant(x))
	}
	return out, nil
}

func (r capTableGrants) one(row sqlcdb.FyniqCapTableGrant, err error) (storage.CapTableGrant, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.CapTableGrant{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.CapTableGrant{}, mapError(err)
	}
	return toCapTableGrant(row), nil
}

func (r capTableGrants) LiveOf(ctx context.Context, user uuid.UUID) (storage.CapTableGrant, error) {
	if err := r.u.check(); err != nil {
		return storage.CapTableGrant{}, err
	}
	return r.one(r.u.q.LiveCapTableGrantOf(ctx, user))
}

func (r capTableGrants) Get(ctx context.Context, id uuid.UUID) (storage.CapTableGrant, error) {
	if err := r.u.check(); err != nil {
		return storage.CapTableGrant{}, err
	}
	return r.one(r.u.q.GetCapTableGrant(ctx, id))
}

func (r capTableGrants) Grant(ctx context.Context, user uuid.UUID, reason, by string, at time.Time) (storage.CapTableGrant, error) {
	if err := r.u.check(); err != nil {
		return storage.CapTableGrant{}, err
	}
	g, err := r.one(r.u.q.InsertCapTableGrant(ctx, sqlcdb.InsertCapTableGrantParams{UserID: user, Reason: reason, GrantedBy: by, GrantedAt: at}))
	if err != nil {
		return g, err
	}
	return g, r.u.event(ctx, "cap_table_grant.granted", "cap_table_grant", g.ID, nil, map[string][2]any{"user_id": {nil, user.String()}, "reason": {nil, reason}})
}

func (r capTableGrants) Revoke(ctx context.Context, id uuid.UUID, reason, by string, at time.Time) (storage.CapTableGrant, error) {
	if err := r.u.check(); err != nil {
		return storage.CapTableGrant{}, err
	}
	row, err := r.u.q.RevokeCapTableGrant(ctx, sqlcdb.RevokeCapTableGrantParams{ID: id, RevokedBy: &by, RevokedAt: &at, RevokeReason: &reason})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.CapTableGrant{}, &storage.Error{Code: storage.CodeCapTableGrant, Message: "the grant is revoked already"}
	}
	g, err := r.one(row, err)
	if err != nil {
		return g, err
	}
	return g, r.u.event(ctx, "cap_table_grant.revoked", "cap_table_grant", g.ID, nil, map[string][2]any{"revoked": {false, true}, "reason": {nil, reason}})
}
