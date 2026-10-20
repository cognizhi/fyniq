package postgres

import (
	"context"
	"fmt"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) TeamModes() storage.TeamModes { return teamModes{u} }

type teamModes struct{ u *unitOfWork }

func toTeamMode(r sqlcdb.ListTeamModesRow) storage.TeamMode {
	return storage.TeamMode{ID: r.ID, Version: int(r.Version), Mode: r.Mode, RequestID: r.RequestID, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
}

func toSoDOverride(r sqlcdb.ListSoDOverridesRow) storage.SoDOverride {
	return storage.SoDOverride{ID: r.ID, TeamModeVersion: int(r.TeamModeVersion), UserID: r.UserID, Conflict: r.Conflict, Reason: r.Reason,
		RequestID: r.RequestID, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
}

func (r teamModes) List(ctx context.Context) ([]storage.TeamMode, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListTeamModes(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.TeamMode, len(rows))
	for i, row := range rows {
		out[i] = toTeamMode(row)
	}
	return out, nil
}

func (r teamModes) Add(ctx context.Context, n storage.NewTeamMode) (storage.TeamMode, error) {
	if err := r.u.check(); err != nil {
		return storage.TeamMode{}, err
	}
	if n.Version < 1 || n.Version > 1_000_000 {
		return storage.TeamMode{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("version %d of the team mode", n.Version)}
	}
	row, err := r.u.q.InsertTeamMode(ctx, sqlcdb.InsertTeamModeParams{Version: int32(n.Version), // 1 to 1,000,000, checked above
		Mode: n.Mode, RequestID: n.RequestID, CreatedBy: n.CreatedBy, CreatedAt: n.CreatedAt})
	if err != nil {
		return storage.TeamMode{}, mapError(err)
	}
	m := toTeamMode(sqlcdb.ListTeamModesRow(row))
	if err := r.u.event(ctx, "team_mode.changed", "team_mode", m.ID, &m.Version, map[string][2]any{
		"version": {nil, m.Version}, "mode": {nil, m.Mode}, "request_id": {nil, m.RequestID}, "created_by": {nil, m.CreatedBy}}); err != nil {
		return storage.TeamMode{}, err
	}
	if err := r.u.auditVoids(ctx); err != nil {
		return storage.TeamMode{}, err
	}
	return m, nil
}

func (r teamModes) Overrides(ctx context.Context) ([]storage.SoDOverride, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.ListSoDOverrides(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.SoDOverride, len(rows))
	for i, row := range rows {
		out[i] = toSoDOverride(row)
	}
	return out, nil
}

func (r teamModes) Override(ctx context.Context, n storage.NewSoDOverride) (storage.SoDOverride, error) {
	if err := r.u.check(); err != nil {
		return storage.SoDOverride{}, err
	}
	if n.TeamModeVersion < 1 || n.TeamModeVersion > 1_000_000 {
		return storage.SoDOverride{}, &storage.Error{Code: storage.CodeInvalid, Message: fmt.Sprintf("version %d of the team mode", n.TeamModeVersion)}
	}
	row, err := r.u.q.InsertSoDOverride(ctx, sqlcdb.InsertSoDOverrideParams{TeamModeVersion: int32(n.TeamModeVersion), // checked above
		UserID: n.UserID, Conflict: n.Conflict, Reason: n.Reason, RequestID: n.RequestID, CreatedBy: n.CreatedBy, CreatedAt: n.CreatedAt})
	if err != nil {
		return storage.SoDOverride{}, mapError(err)
	}
	o := toSoDOverride(sqlcdb.ListSoDOverridesRow(row))
	if err := r.u.event(ctx, "sod.overridden", "sod_override", o.ID, nil, map[string][2]any{
		"team_mode_version": {nil, o.TeamModeVersion}, "user_id": {nil, o.UserID}, "conflict": {nil, o.Conflict}, "reason": {nil, o.Reason},
		"request_id": {nil, o.RequestID}, "created_by": {nil, o.CreatedBy}}); err != nil {
		return storage.SoDOverride{}, err
	}
	return o, nil
}
