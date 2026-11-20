package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

// ------------------------------------------------------------------ the arranged dashboards (FR-VIZ-09, FR-KPI-05, rev 0.540)

func (u *unitOfWork) DashboardLayouts() storage.DashboardLayouts { return dashboardLayouts{u} }

type dashboardLayouts struct{ u *unitOfWork }

func toDashboardLayout(r sqlcdb.FyniqDashboardLayout) storage.DashboardLayout {
	d := storage.DashboardLayout{ID: r.ID, Scope: r.Scope, Tiles: json.RawMessage(r.Tiles), Version: int(r.Version), UpdatedBy: r.UpdatedBy, UpdatedAt: r.UpdatedAt}
	if r.Role != nil {
		d.Role = *r.Role
	}
	if r.UserID != nil {
		d.UserID = *r.UserID
	}
	return d
}

func (r dashboardLayouts) OfRole(ctx context.Context, role string) (storage.DashboardLayout, error) {
	if err := r.u.check(); err != nil {
		return storage.DashboardLayout{}, err
	}
	row, err := r.u.q.DashboardLayoutOfRole(ctx, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.DashboardLayout{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.DashboardLayout{}, mapError(err)
	}
	return toDashboardLayout(row), nil
}

func (r dashboardLayouts) OfUser(ctx context.Context, user uuid.UUID) (storage.DashboardLayout, error) {
	if err := r.u.check(); err != nil {
		return storage.DashboardLayout{}, err
	}
	row, err := r.u.q.DashboardLayoutOfUser(ctx, &user)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.DashboardLayout{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.DashboardLayout{}, mapError(err)
	}
	return toDashboardLayout(row), nil
}

func (r dashboardLayouts) save(ctx context.Context, scope string, role *string, user *uuid.UUID, before func() (storage.DashboardLayout, error), tiles json.RawMessage,
	by string, at time.Time, version int) (storage.DashboardLayout, error) {
	if version == 0 {
		row, err := r.u.q.InsertDashboardLayout(ctx, sqlcdb.InsertDashboardLayoutParams{Scope: scope, Role: role, UserID: user, Tiles: tiles, UpdatedBy: by, UpdatedAt: at})
		if err != nil {
			return storage.DashboardLayout{}, mapError(err)
		}
		out := toDashboardLayout(row)
		v := out.Version
		return out, r.u.event(ctx, "dashboard_layout.saved", "dashboard_layout", out.ID, &v, map[string][2]any{"scope": {nil, scope}, "tiles": {nil, string(tiles)}})
	}
	cur, err := before()
	if errors.Is(err, ports.ErrNotFound) {
		return storage.DashboardLayout{}, &storage.Error{Code: storage.CodeVersion, Message: "the layout does not exist at that version"}
	}
	if err != nil {
		return storage.DashboardLayout{}, err
	}
	row, err := r.u.q.UpdateDashboardLayout(ctx, sqlcdb.UpdateDashboardLayoutParams{ID: cur.ID, Version: int32(min(max(version, 1), 1<<30)), Tiles: tiles, UpdatedBy: by, UpdatedAt: at}) // #nosec G115 -- bounded
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.DashboardLayout{}, &storage.Error{Code: storage.CodeVersion, Message: "the layout changed since it was read"}
	}
	if err != nil {
		return storage.DashboardLayout{}, mapError(err)
	}
	out := toDashboardLayout(row)
	v := out.Version
	return out, r.u.event(ctx, "dashboard_layout.changed", "dashboard_layout", out.ID, &v, map[string][2]any{"tiles": {string(cur.Tiles), string(tiles)}})
}

func (r dashboardLayouts) SaveRole(ctx context.Context, role string, tiles json.RawMessage, by string, at time.Time, version int) (storage.DashboardLayout, error) {
	if err := r.u.check(); err != nil {
		return storage.DashboardLayout{}, err
	}
	return r.save(ctx, storage.DashboardRole, &role, nil, func() (storage.DashboardLayout, error) { return r.OfRole(ctx, role) }, tiles, by, at, version)
}

func (r dashboardLayouts) SaveUser(ctx context.Context, user uuid.UUID, tiles json.RawMessage, by string, at time.Time, version int) (storage.DashboardLayout, error) {
	if err := r.u.check(); err != nil {
		return storage.DashboardLayout{}, err
	}
	return r.save(ctx, storage.DashboardUser, nil, &user, func() (storage.DashboardLayout, error) { return r.OfUser(ctx, user) }, tiles, by, at, version)
}
