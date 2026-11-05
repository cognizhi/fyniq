package postgres

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/cognizhi/fyniq/internal/adapters/postgres/sqlcdb"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/ports/storage"
)

func (u *unitOfWork) Dimensions() storage.Dimensions { return dimensions{u} }

type dimensions struct{ u *unitOfWork }

func toDimension(r sqlcdb.DimensionRow) storage.Dimension {
	d := storage.Dimension{ID: r.ID, Kind: r.Kind, Name: r.Name, Active: r.IsActive, Version: int(r.Version), CreatedAt: r.CreatedAt}
	if r.Position != nil {
		d.Place = int(*r.Position)
	}
	return d
}

func toDimensionValue(r sqlcdb.DimensionValueRow) storage.DimensionValue {
	return storage.DimensionValue{ID: r.ID, DimensionID: r.DimensionID, Code: r.Code, Name: r.Name, Active: r.IsActive, Version: int(r.Version),
		CreatedAt: r.CreatedAt}
}

func (r dimensions) List(ctx context.Context) ([]storage.Dimension, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.Dimensions(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.Dimension, 0, len(rows))
	for _, x := range rows {
		out = append(out, toDimension(sqlcdb.DimensionRow(x)))
	}
	return out, nil
}

func (r dimensions) Get(ctx context.Context, id uuid.UUID) (storage.Dimension, error) {
	if err := r.u.check(); err != nil {
		return storage.Dimension{}, err
	}
	row, err := r.u.q.Dimension(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Dimension{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.Dimension{}, mapError(err)
	}
	return toDimension(row), nil
}

func (r dimensions) Create(ctx context.Context, n storage.NewDimension) (storage.Dimension, error) {
	if err := r.u.check(); err != nil {
		return storage.Dimension{}, err
	}
	var place *int16
	if n.Place != 0 {
		p := int16(-1) // outside 1 to 8, which the table's check refuses
		if n.Place > 0 && n.Place <= 8 {
			p = int16(n.Place) // 1 to 8, checked here
		}
		place = &p
	}
	row, err := r.u.q.InsertDimension(ctx, sqlcdb.InsertDimensionParams{Kind: n.Kind, Position: place, Name: n.Name})
	if err != nil {
		return storage.Dimension{}, mapError(err)
	}
	out := toDimension(sqlcdb.DimensionRow(row))
	changes := map[string][2]any{"kind": {nil, out.Kind}, "name": {nil, out.Name}}
	if out.Place != 0 {
		changes["place"] = [2]any{nil, out.Place}
	}
	return out, r.u.event(ctx, "dimension.created", "dimension", out.ID, &out.Version, changes)
}

func (r dimensions) Change(ctx context.Context, id uuid.UUID, version int, c storage.DimensionChange) (storage.Dimension, error) {
	before, err := r.Get(ctx, id)
	if err != nil {
		return before, err
	}
	row, err := r.u.q.UpdateDimension(ctx, sqlcdb.UpdateDimensionParams{Name: c.Name, IsActive: c.Active, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.Dimension{}, &storage.Error{Code: storage.CodeVersion, Message: "the dimension changed since it was read"}
	}
	if err != nil {
		return storage.Dimension{}, mapError(err)
	}
	after := toDimension(sqlcdb.DimensionRow(row))
	return after, r.u.event(ctx, "dimension.changed", "dimension", id, &after.Version, changed(before.Name, after.Name, before.Active, after.Active))
}

// changed is what a change of a dimension or a value did to its name and use, as the audit trail records it.
func changed(name, newName string, active, nowActive bool) map[string][2]any {
	out := map[string][2]any{}
	if name != newName {
		out["name"] = [2]any{name, newName}
	}
	if active != nowActive {
		out["active"] = [2]any{active, nowActive}
	}
	return out
}

func (r dimensions) Values(ctx context.Context, dimension *uuid.UUID) ([]storage.DimensionValue, error) {
	if err := r.u.check(); err != nil {
		return nil, err
	}
	rows, err := r.u.q.DimensionValues(ctx, dimension)
	if err != nil {
		return nil, mapError(err)
	}
	out := make([]storage.DimensionValue, 0, len(rows))
	for _, x := range rows {
		out = append(out, toDimensionValue(sqlcdb.DimensionValueRow(x)))
	}
	return out, nil
}

func (r dimensions) GetValue(ctx context.Context, id uuid.UUID) (storage.DimensionValue, error) {
	if err := r.u.check(); err != nil {
		return storage.DimensionValue{}, err
	}
	row, err := r.u.q.DimensionValue(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.DimensionValue{}, ports.ErrNotFound
	}
	if err != nil {
		return storage.DimensionValue{}, mapError(err)
	}
	return toDimensionValue(row), nil
}

// CreateValue adds a value; its audit event is its dimension's, as the database requires (AUD-08), and names the value.
func (r dimensions) CreateValue(ctx context.Context, n storage.NewDimensionValue) (storage.DimensionValue, error) {
	if err := r.u.check(); err != nil {
		return storage.DimensionValue{}, err
	}
	row, err := r.u.q.InsertDimensionValue(ctx, sqlcdb.InsertDimensionValueParams{DimensionID: n.DimensionID, Code: n.Code, Name: n.Name})
	if err != nil {
		return storage.DimensionValue{}, mapError(err)
	}
	out := toDimensionValue(sqlcdb.DimensionValueRow(row))
	return out, r.u.event(ctx, "dimension.value_added", "dimension", out.DimensionID, nil, map[string][2]any{
		"value_id": {nil, out.ID.String()}, "code": {nil, out.Code}, "name": {nil, out.Name}})
}

// ChangeValue changes a value at version; its audit event is its dimension's, naming the value and its version.
func (r dimensions) ChangeValue(ctx context.Context, id uuid.UUID, version int, c storage.DimensionChange) (storage.DimensionValue, error) {
	before, err := r.GetValue(ctx, id)
	if err != nil {
		return before, err
	}
	row, err := r.u.q.UpdateDimensionValue(ctx, sqlcdb.UpdateDimensionValueParams{Name: c.Name, IsActive: c.Active, ID: id, Version: small(version)})
	if errors.Is(err, pgx.ErrNoRows) {
		return storage.DimensionValue{}, &storage.Error{Code: storage.CodeVersion, Message: "the dimension value " + before.Code + " changed since it was read"}
	}
	if err != nil {
		return storage.DimensionValue{}, mapError(err)
	}
	after := toDimensionValue(sqlcdb.DimensionValueRow(row))
	changes := changed(before.Name, after.Name, before.Active, after.Active)
	changes["value_id"] = [2]any{after.ID.String(), after.ID.String()}
	changes["version"] = [2]any{before.Version, after.Version}
	return after, r.u.event(ctx, "dimension.value_changed", "dimension", after.DimensionID, nil, changes)
}
